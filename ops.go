// ops.go — de vier bewerkingen op een doel (kaart, stick of image-bestand):
// schrijven, verifiëren, en het config-venster lezen of herschrijven.
//
// Alles gebeurt IN DIT PROCES. Dat kan omdat de rechten niet van root komen
// maar van authopen (internal/priv): root zijn is op macOS namelijk niet
// genoeg — zelfs Apple's dd krijgt als uid 0 "Operation not permitted" op
// /dev/rdiskN, want TCC kijkt naar het proces en niet naar de gebruiker
// (gemeten 06-08). authopen is Apple's eigen helper die dat mag, en die geeft
// een gewone seekbare file descriptor terug.
//
// Winst van die keuze, behalve dat het werkt: geen tweede proces, dus live
// voortgang in plaats van een voortgangsbestand, en fouten die gewoon Go-fouten
// zijn.
//
// Elke device-bewerking begint met unmounten — dat mag een gewone gebruiker
// voor verwisselbare media, en macOS weigert anders te schrijven.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xinix00/hop-imager/internal/cfgwin"
	"github.com/xinix00/hop-imager/internal/disk"
	"github.com/xinix00/hop-imager/internal/priv"
)

// sectorSize is de uitlijning die een rauw device eist.
const sectorSize = 512

// isDevice zegt of het pad een rauw blok-device is (en dus unmount plus
// authopen vraagt) of een gewoon image-bestand.
func isDevice(path string) bool { return strings.HasPrefix(path, "/dev/") }

// keepEligible: alleen een kaart of stick draagt de config van een node, dus
// alleen daar is er iets te bewaren. Een variabele en geen directe aanroep,
// zodat de test de hele beweging op een bestand kan naspelen.
var keepEligible = isDevice

// target is een geopend doel plus wat er bij het sluiten nog moet gebeuren.
// Dat laatste bestaat om Windows: daar moeten de volume-sloten vastgehouden
// worden zolang er geschreven wordt, en pas ná het sluiten van het device weer
// los (internal/disk.Prepare).
type target struct {
	*os.File
	release func()
}

func (t *target) Close() error {
	err := t.File.Close()
	if t.release != nil {
		t.release()
	}
	return err
}

// openTarget opent een doel: een device (met de voorbereiding die dat platform
// eist) of een gewoon image-bestand.
func openTarget(path string, write bool) (*target, error) {
	if !isDevice(path) {
		flag := os.O_RDONLY
		if write {
			flag = os.O_RDWR
		}
		f, err := os.OpenFile(path, flag, 0)
		if err != nil {
			return nil, err
		}
		return &target{File: f}, nil
	}
	release, err := disk.Prepare(path)
	if err != nil {
		return nil, err
	}
	f, err := priv.OpenDevice(path, write)
	if err != nil {
		release()
		return nil, err
	}
	return &target{File: f, release: release}, nil
}

// burnResult is wat één schrijfbeweging heeft opgeleverd: is het image
// byte-exact geland, en is de config die er stond teruggezet.
type burnResult struct {
	MismatchAt int64    // -1 = het image staat er byte-voor-byte op
	Kept       bool     // de config van de kaart is teruggezet
	KeptNode   string   // hopos.node uit die config (voor de melding)
	Backup     string   // pad van de veiligheidskopie
	NewKeys    []string // sleutels die de NIEUWE default heeft en de bewaarde config niet
	Warning    string   // een mismatch die de gebruiker moet weten
}

// opWrite schrijft het image naar het doel, verifieert het meteen op DEZELFDE
// open descriptor, en zet (met keepConfig) de config terug die er al stond.
//
// WAAROM DE HELE BEWEGING IN ÉÉN OPEN ZIT: het is één autorisatie in plaats van
// drie, en de verificatie klopt alleen zó. Zodra het device dichtgaat ziet macOS
// een verse FAT-partitie, mount hem en schrijft er zijn eigen metadata in
// (.Spotlight-V100, .fseventsd, de dirty-bit) — een verificatie daarná
// vergelijkt met bytes die het systeem net heeft veranderd (Dereks "verify
// FAILED op twee kaarten", 06-08, terwijl beide flashes goed waren).
//
// De volgorde is niet vrij:
//
//  1. lezen wat er staat  — de config van de node zoals hij nu is
//  2. back-up naar schijf — vóór er één byte overheen gaat
//  3. schrijven
//  4. VERIFIËREN          — nog steeds byte-identiek aan het image
//  5. de config terugzetten
//
// Stap 5 ná stap 4, want daarna verschilt de kaart legitiem van het image (jouw
// config staat in het venster) en zou de verificatie onzin melden.
func opWrite(imagePath, target string, keepConfig bool, phase func(string), prog func(done, total int64)) (res burnResult, err error) {
	res.MismatchAt = -1
	img, err := os.Open(imagePath)
	if err != nil {
		return res, err
	}
	defer img.Close()
	st, err := img.Stat()
	if err != nil {
		return res, err
	}
	out, err := openTarget(target, true)
	if err != nil {
		return res, err
	}
	defer out.Close()

	// 1+2. De config die er nu staat, en meteen een kopie op schijf. Alleen op
	// een device: een image-bestand naar een image-bestand kopiëren is geen
	// bestaand geval.
	var kept []byte
	if keepConfig && keepEligible(target) {
		phase("reading the config that is on the card")
		if w, ferr := cfgwin.Find(out.File); ferr == nil {
			if c, cerr := cfgwin.Content(out.File, w); cerr == nil && len(c) > 0 {
				kept = c
				res.KeptNode = cfgValue(c, "hopos.node")
				res.Backup, _ = saveKept(c, res.KeptNode)
			}
		}
	}

	// 3. Schrijven. De fd staat nog op 0: Find en Content lezen met ReadAt en
	// verzetten de offset niet.
	phase("writing the image")
	buf := make([]byte, 4<<20)
	var done int64
	for {
		n, rerr := img.Read(buf)
		if n > 0 {
			// Een raw device neemt alleen HELE SECTOREN aan (macOS geeft
			// anders EINVAL, Windows ERROR_INVALID_PARAMETER). De staart van
			// een image is zelden een 512-voud, dus die padden we met nullen —
			// een paar bytes voorbij het image-einde raakt niemand, en de
			// verificatie vergelijkt toch precies de image-lengte.
			w := n
			if isDevice(target) {
				if rest := w % sectorSize; rest != 0 {
					pad := sectorSize - rest
					for i := w; i < w+pad; i++ {
						buf[i] = 0
					}
					w += pad
				}
			}
			if _, werr := out.Write(buf[:w]); werr != nil {
				return res, werr
			}
			done += int64(n)
			prog(done, st.Size())
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return res, rerr
		}
	}
	// Tolerante sync: een raw device kan geen fsync (zie cfgwin.Sync).
	if err := cfgwin.Sync(out.File); err != nil {
		return res, err
	}

	// 4. Verifiëren.
	phase("verifying")
	at, err := compare(img, out.File, st.Size(), prog)
	if err != nil {
		return res, err
	}
	res.MismatchAt = at
	if at >= 0 || len(kept) == 0 {
		return res, nil
	}

	// 5. De bewaarde config terug in het VERSE venster. Het venster zoeken we
	// in het IMAGE-bestand en niet op de kaart: dat is een lokale schijf (dus
	// snel) en de kaart is net byte-identiek bevonden, dus de coördinaten
	// kloppen per definitie.
	phase("putting your config back")
	w, err := cfgwin.Find(img)
	if err != nil {
		res.Warning = "this image has no config window, so your config could not be put back: " + err.Error()
		return res, nil
	}
	if def, derr := cfgwin.Content(img, w); derr == nil {
		res.NewKeys = missingKeys(kept, def)
		// Een desktop-config op een headless image zaait een display-job die
		// het glas nooit krijgt: in een headless build zit geen grant-provider.
		// Stille mismatch, dus hardop zeggen.
		if isDesktop(kept) && !isDesktop(def) {
			res.Warning = "your config seeds a desktop, but this is a headless image — the display job will never get the framebuffer. Switch to the desktop image, or drop those jobs in CONFIGURE."
		}
	}
	if err := cfgwin.Replace(out.File, w, kept); err != nil {
		res.Warning = "your config did not fit the new window (" + err.Error() + "), so the card carries the image default. Your config is safe in " + res.Backup
		return res, nil
	}
	res.Kept = true
	return res, nil
}

// saveKept legt de gelezen config op schijf vóór er iets overheen gaat.
//
// Dit is het vangnet dat de hele feature vertrouwd maakt: in zo'n config zit je
// API-sleutel en je S3-secret, en een flash die halverwege stukloopt mag die
// nooit opeten. Mode 0600 — het zijn geheimen in platte tekst, en de melding
// noemt het bestand bij naam zodat dat geen verrassing is.
func saveKept(cfg []byte, node string) (string, error) {
	dir, err := keptDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if node == "" {
		node = "unnamed"
	}
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, node)
	p := filepath.Join(dir, safe+"-"+time.Now().Format("20060102-150405")+".cfg")
	return p, os.WriteFile(p, cfg, 0600)
}

// keptDir is waar de back-ups landen. Een variabele omdat os.UserConfigDir op
// macOS geen omgevingsvariabele leest, en een test nooit in de echte config-map
// van de gebruiker hoort te schrijven.
var keptDir = func() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "hop-imager", "kept"), nil
}

// cfgValue leest één sleutel uit een config (eerste voorkomen, commentaar
// overgeslagen) — zelfde regels als fw/bootcfg in HopOS zelf.
func cfgValue(cfg []byte, key string) string {
	for _, line := range strings.Split(string(cfg), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// missingKeys zegt welke sleutels de nieuwe default zét en de bewaarde config
// niet heeft. BEWUST GEEN MERGE: een config is een beslissing van de gebruiker,
// en die stil samenvoegen verandert beslissingen — bij herhaalbare regels
// (hopos.init[], hopos.apps[]) is "aanvullen of vervangen?" ook niet te weten.
// Dus houden we letterlijk wat er stond en zeggen we wat je mist.
func missingKeys(kept, def []byte) []string {
	have := map[string]bool{}
	for _, line := range strings.Split(string(kept), "\n") {
		if k := keyOf(line); k != "" {
			have[k] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(def), "\n") {
		k := keyOf(line)
		if k == "" || have[k] || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

func keyOf(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return ""
	}
	k, _, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(k)
}

// compare vergelijkt n bytes van het image met het doel en geeft de eerste
// afwijkende byte terug (-1 = gelijk).
//
// De reads naar het doel zijn UITGELIJND en een veelvoud van 512: een rauw
// character device weigert al het andere met EINVAL (zie cfgwin.readAt). Het
// image is een gewoon bestand en mag alles.
func compare(img, dev *os.File, n int64, prog func(done, total int64)) (int64, error) {
	const chunk = 4 << 20
	ibuf := make([]byte, chunk)
	dbuf := make([]byte, chunk)
	var off int64
	for off < n {
		want := int64(chunk)
		if n-off < want {
			want = n - off
		}
		if _, err := img.ReadAt(ibuf[:want], off); err != nil && err != io.EOF {
			return 0, fmt.Errorf("read image at %d: %v", off, err)
		}
		rd := (want + sectorSize - 1) / sectorSize * sectorSize
		if rd > chunk {
			rd = chunk
		}
		if _, err := dev.ReadAt(dbuf[:rd], off); err != nil && err != io.EOF {
			return 0, fmt.Errorf("read back at %d: %v", off, err)
		}
		if !bytes.Equal(ibuf[:want], dbuf[:want]) {
			for i := int64(0); i < want; i++ {
				if ibuf[i] != dbuf[i] {
					return off + i, nil
				}
			}
		}
		off += want
		if prog != nil {
			prog(off, n)
		}
	}
	return -1, nil
}

// opCfgRead geeft de configinhoud uit het venster van het doel.
func opCfgRead(target string) ([]byte, cfgwin.Window, error) {
	f, err := openTarget(target, false)
	if err != nil {
		return nil, cfgwin.Window{}, err
	}
	defer f.Close()
	w, err := cfgwin.Find(f.File)
	if err != nil {
		return nil, cfgwin.Window{}, err
	}
	b, err := cfgwin.Content(f.File, w)
	return b, w, err
}

// opCfgWrite herschrijft het venster (en werkt de FIP-checksums bij als het
// venster in een Sophgo-monitor ligt — de LicheeRV).
func opCfgWrite(target string, content []byte) (cfgwin.Window, error) {
	f, err := openTarget(target, true)
	if err != nil {
		return cfgwin.Window{}, err
	}
	defer f.Close()
	w, err := cfgwin.Find(f.File)
	if err != nil {
		return cfgwin.Window{}, err
	}
	return w, cfgwin.Replace(f.File, w, content)
}

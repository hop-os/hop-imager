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
	"strings"

	"github.com/xinix00/hop-imager/internal/cfgwin"
	"github.com/xinix00/hop-imager/internal/disk"
	"github.com/xinix00/hop-imager/internal/priv"
)

// sectorSize is de uitlijning die een rauw device eist.
const sectorSize = 512

// isDevice zegt of het pad een rauw blok-device is (en dus unmount plus
// authopen vraagt) of een gewoon image-bestand.
func isDevice(path string) bool { return strings.HasPrefix(path, "/dev/") }

// openTarget opent een doel: een device via authopen, een bestand gewoon.
func openTarget(path string, write bool) (*os.File, error) {
	if !isDevice(path) {
		flag := os.O_RDONLY
		if write {
			flag = os.O_RDWR
		}
		return os.OpenFile(path, flag, 0)
	}
	if err := disk.UnmountPath(path); err != nil {
		return nil, err
	}
	return priv.OpenDevice(path, write)
}

// opWrite schrijft het image naar het doel en verifieert het meteen daarna op
// DEZELFDE open descriptor.
//
// WAAROM DIRECT EN NIET LATER (en dit was Dereks "verify FAILED op twee
// kaarten", 06-08): zodra de write klaar is en het device dichtgaat, ziet macOS
// een verse FAT-partitie, mount hem automatisch en schrijft er zijn eigen
// metadata in (.Spotlight-V100, .fseventsd, de dirty-bit). Een verificatie
// dáárna vergelijkt dus image-bytes met kaart-bytes die het systeem net heeft
// aangepast — en meldt terecht een verschil dat niets met de flash te maken
// heeft. Meteen verifiëren geeft het antwoord dat je wilt weten: is er goed
// geschreven?
//
// mismatchAt is de eerste byte die verschilt, of -1 als alles klopt.
func opWrite(imagePath, target string, prog func(done, total int64)) (mismatchAt int64, err error) {
	img, err := os.Open(imagePath)
	if err != nil {
		return 0, err
	}
	defer img.Close()
	st, err := img.Stat()
	if err != nil {
		return 0, err
	}
	out, err := openTarget(target, true)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	buf := make([]byte, 4<<20)
	var done int64
	for {
		n, rerr := img.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return 0, werr
			}
			done += int64(n)
			prog(done, st.Size())
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return 0, rerr
		}
	}
	// Tolerante sync: een raw device kan geen fsync (zie cfgwin.Sync).
	if err := cfgwin.Sync(out); err != nil {
		return 0, err
	}
	return compare(img, out, st.Size(), prog)
}

// opCheck is de losse verificatie: het doel terugleren en vergelijken. Let op
// het verschil met de verificatie ín opWrite — hier kan het systeem de kaart
// intussen hebben aangeraakt (zie de uitleg daar), dus geeft deze functie de
// EERSTE afwijkende byte terug in plaats van alleen een oordeel. Ligt die in de
// eerste megabytes, dan is het bijna altijd macOS-metadata en niet je flash.
func opCheck(imagePath, target string, prog func(done, total int64)) (mismatchAt int64, err error) {
	img, err := os.Open(imagePath)
	if err != nil {
		return 0, err
	}
	defer img.Close()
	st, err := img.Stat()
	if err != nil {
		return 0, err
	}
	dev, err := openTarget(target, false)
	if err != nil {
		return 0, err
	}
	defer dev.Close()
	return compare(img, dev, st.Size(), prog)
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
	w, err := cfgwin.Find(f)
	if err != nil {
		return nil, cfgwin.Window{}, err
	}
	b, err := cfgwin.Content(f, w)
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
	w, err := cfgwin.Find(f)
	if err != nil {
		return cfgwin.Window{}, err
	}
	return w, cfgwin.Replace(f, w, content)
}

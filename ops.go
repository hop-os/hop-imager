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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/xinix00/hop-imager/internal/cfgwin"
	"github.com/xinix00/hop-imager/internal/disk"
	"github.com/xinix00/hop-imager/internal/priv"
)

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

// opWrite schrijft het image naar het doel en meldt voortgang per blok.
func opWrite(imagePath, target string, prog func(done, total int64)) error {
	img, err := os.Open(imagePath)
	if err != nil {
		return err
	}
	defer img.Close()
	st, err := img.Stat()
	if err != nil {
		return err
	}
	out, err := openTarget(target, true)
	if err != nil {
		return err
	}
	defer out.Close()
	buf := make([]byte, 4<<20)
	var done int64
	for {
		n, rerr := img.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			prog(done, st.Size())
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	// Tolerante sync: een raw device kan geen fsync (zie cfgwin.Sync).
	return cfgwin.Sync(out)
}

// opCheck leest het doel terug en vergelijkt met het image, byte voor byte
// (sha256 over precies de image-lengte).
func opCheck(imagePath, target string, prog func(done, total int64)) (bool, string, error) {
	img, err := os.Open(imagePath)
	if err != nil {
		return false, "", err
	}
	defer img.Close()
	st, err := img.Stat()
	if err != nil {
		return false, "", err
	}
	want, err := shaOf(img, st.Size(), nil)
	if err != nil {
		return false, "", err
	}
	dev, err := openTarget(target, false)
	if err != nil {
		return false, "", err
	}
	defer dev.Close()
	got, err := shaOf(dev, st.Size(), prog)
	if err != nil {
		return false, "", err
	}
	return want == got, want, nil
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

func shaOf(r io.Reader, n int64, prog func(done, total int64)) (string, error) {
	h := sha256.New()
	buf := make([]byte, 4<<20)
	var done int64
	for done < n {
		want := int64(len(buf))
		if n-done < want {
			want = n - done
		}
		m, err := io.ReadFull(r, buf[:want])
		if m > 0 {
			h.Write(buf[:m])
			done += int64(m)
			if prog != nil {
				prog(done, n)
			}
		}
		if err != nil {
			return "", fmt.Errorf("read back at %d bytes: %v", done, err)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

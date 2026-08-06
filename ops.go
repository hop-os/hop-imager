// ops.go — de bewerkingen die rechten op het rauwe device nodig hebben:
// schrijven, verifiëren en het config-venster lezen/herschrijven. Eén
// implementatie, twee aanroepers: de GUI zelf als die al root is, en de
// verhoogde worker (dezelfde binary met -op, zie internal/priv) als dat niet
// zo is. De worker praat via stdout en een voortgangsbestand — daardoor is er
// geen tweede code-pad en dus ook geen tweede verzameling fouten.
//
// Elke device-bewerking begint met unmounten: macOS weigert raw writes naar
// een schijf met gemounte volumes, en raw is precies wat we willen (het werkt
// op élk board, ook als de partitie niet mount of de config in de kernel zit).
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/xinix00/hop-imager/internal/cfgwin"
	"github.com/xinix00/hop-imager/internal/disk"
	"github.com/xinix00/hop-imager/internal/priv"
)

// isDevice zegt of het pad een rauw blok-device is (en dus unmount + rechten
// vraagt) of een gewoon image-bestand.
func isDevice(path string) bool { return strings.HasPrefix(path, "/dev/") }

// runOp is de worker-dispatcher: dezelfde argumenten die priv.Escalate
// doorgeeft, en dezelfde functie die de GUI aanroept als ze al root is. De
// teruggegeven string is wat de aanroeper als resultaat ziet (stdout van de
// worker).
func runOp(args []string) (string, error) {
	fs := flag.NewFlagSet("op", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	op := fs.String("op", "", "write|check|cfgread|cfgwrite")
	image := fs.String("image", "", "image file")
	dev := fs.String("dev", "", "target device or image file")
	cfgFile := fs.String("cfg", "", "config file to write")
	progress := fs.String("progress", "", "progress file")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	switch *op {
	case "write":
		return "", opWrite(*image, *dev, *progress)
	case "check":
		return opCheck(*image, *dev, *progress)
	case "cfgread":
		b, err := opCfgRead(*dev)
		return string(b), err
	case "cfgwrite":
		b, err := os.ReadFile(*cfgFile)
		if err != nil {
			return "", err
		}
		return opCfgWrite(*dev, b)
	}
	return "", fmt.Errorf("unknown -op %q", *op)
}

func opWrite(imagePath, devPath, progressFile string) error {
	img, err := os.Open(imagePath)
	if err != nil {
		return err
	}
	defer img.Close()
	st, err := img.Stat()
	if err != nil {
		return err
	}
	if isDevice(devPath) {
		if err := disk.UnmountPath(devPath); err != nil {
			return err
		}
	}
	out, err := os.OpenFile(devPath, os.O_WRONLY, 0)
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
			priv.WriteProgress(progressFile, done, st.Size())
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return out.Sync()
}

// opCheck leest de kaart terug en vergelijkt met het image, byte voor byte
// (sha256 over precies de image-lengte). Het antwoord is één regel, want dat
// is wat door de escalatie heen komt.
func opCheck(imagePath, devPath, progressFile string) (string, error) {
	img, err := os.Open(imagePath)
	if err != nil {
		return "", err
	}
	defer img.Close()
	st, err := img.Stat()
	if err != nil {
		return "", err
	}
	want, err := shaOf(img, st.Size(), nil)
	if err != nil {
		return "", err
	}
	if isDevice(devPath) {
		if err := disk.UnmountPath(devPath); err != nil {
			return "", err
		}
	}
	d, err := os.Open(devPath)
	if err != nil {
		return "", err
	}
	defer d.Close()
	got, err := shaOf(d, st.Size(), func(done, total int64) {
		priv.WriteProgress(progressFile, done, total)
	})
	if err != nil {
		return "", err
	}
	if want == got {
		return "MATCH " + want, nil
	}
	return "MISMATCH " + want + " " + got, nil
}

func opCfgRead(target string) ([]byte, error) {
	if isDevice(target) {
		if err := disk.UnmountPath(target); err != nil {
			return nil, err
		}
	}
	f, err := os.Open(target)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	w, err := cfgwin.Find(f)
	if err != nil {
		return nil, err
	}
	return cfgwin.Content(f, w)
}

// opCfgWrite herschrijft het venster en geeft een regel terug die de GUI kan
// tonen — inclusief of het om een FIP-image ging (daar zijn checksums
// bijgewerkt).
func opCfgWrite(target string, content []byte) (string, error) {
	if isDevice(target) {
		if err := disk.UnmountPath(target); err != nil {
			return "", err
		}
	}
	f, err := os.OpenFile(target, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	w, err := cfgwin.Find(f)
	if err != nil {
		return "", err
	}
	if err := cfgwin.Replace(f, w, content); err != nil {
		return "", err
	}
	return fmt.Sprintf("WRITTEN %d bytes into the %d KB window at %#x", len(content), w.Size>>10, w.Off), nil
}

func shaOf(r io.Reader, n int64, progress func(done, total int64)) (string, error) {
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
			if progress != nil {
				progress(done, n)
			}
		}
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Package cfgwin leest en herschrijft het config-venster van HopOS-images:
// een magic-kopregel (`#HOPCFG1 window=N len=%010d`), de config, en
// '#'-commentregels als padding tot een vaste maat. FORMAATCONTRACT met
// hop-os image/hopcfg/main.go en image/mkcard (-cfgwindow) — de uitleg en het
// waarom staan in de kop van hopcfg; wijzigt het formaat, wijzig dan alle
// drie.
//
// Werkt op een .img-bestand én op een raw device (/dev/rdiskN,
// \\.\PhysicalDriveN): de scan zoekt op elke byte-offset (de LicheeRV-cfg zit
// ín de kernel en ligt niet uitgelijnd), schrijven gaat via read-modify-write
// van het omliggende sector-uitgelijnde bereik. Ligt het venster in de
// MONITOR-payload van een Sophgo-FIP (LicheeRV), dan worden MONITOR_CKSUM en
// PARAM2_CKSUM na de patch bijgewerkt — anders weigert de FSBL de kernel.
package cfgwin

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"syscall"
)

const (
	magic       = "#HOPCFG1 window="
	scanLimit   = 256 << 20
	scanChunk   = 4 << 20
	sectorAlign = 512
)

// Window is één gevonden config-venster in een image of op een device.
type Window struct {
	Off        int64 // byte-offset van de kopregel
	Size       int   // totale venstergrootte (kopregel + config + padding)
	ContentLen int   // lengte van de echte config
	HeaderLen  int   // lengte van de kopregel
}

// Find zoekt hét config-venster. Eén hit is het venster; nul of meer dan één
// is een fout die zichzelf uitlegt.
func Find(f *os.File) (Window, error) {
	hits, err := scan(f, []byte(magic), func(b []byte) bool {
		_, _, _, ok := parseHeader(b)
		return ok
	})
	if err != nil {
		return Window{}, err
	}
	switch len(hits) {
	case 0:
		return Window{}, fmt.Errorf("no config window found — is this a HopOS image built with a config window?")
	case 1:
	default:
		return Window{}, fmt.Errorf("%d config windows found at offsets %v — refusing to patch", len(hits), hits)
	}
	head, err := readAt(f, hits[0], sectorAlign)
	if err != nil {
		return Window{}, err
	}
	win, clen, hlen, _ := parseHeader(head)
	return Window{Off: hits[0], Size: win, ContentLen: clen, HeaderLen: hlen}, nil
}

// Content leest de echte config (zonder kopregel en padding) uit het venster.
func Content(f *os.File, w Window) ([]byte, error) {
	return readAt(f, w.Off+int64(w.HeaderLen), w.ContentLen)
}

// Replace herschrijft het venster in-place met nieuwe config-inhoud en werkt
// de FIP-checksums bij als het venster in een Sophgo-monitor ligt.
func Replace(f *os.File, w Window, content []byte) error {
	buf, err := makeWindow(Strip(content), w.Size)
	if err != nil {
		return err
	}
	if err := writeAligned(f, w.Off, buf); err != nil {
		return err
	}
	if err := fipFix(f, w.Off, w.Size); err != nil {
		return err
	}
	return Sync(f)
}

// Sync dwingt de bytes naar het medium en TOLEREERT dat een rauw device dat
// niet kan. /dev/rdiskN is een character device; fsync geeft daar ENOTTY
// ("inappropriate ioctl for device") — gemeten ná een geslaagde flash op
// Dereks Mac, 06-08. Dat is geen mislukking maar "niet van toepassing":
// schrijven naar het raw device gaat ongebufferd naar de kaart, de sync is de
// gordel voor gewone bestanden. Een fout hier als fout melden zou dus een
// geslaagde flash er als mislukt laten uitzien.
func Sync(f *os.File) error {
	err := f.Sync()
	if err == nil {
		return nil
	}
	var e syscall.Errno
	if errors.As(err, &e) {
		switch e {
		case syscall.ENOTTY, syscall.EINVAL, syscall.ENODEV, syscall.ENOTSUP:
			return nil
		}
	}
	return err
}

// Strip haalt een eventueel bestaand venster van een config af (idempotentie:
// een al-gepadde config aanbieden is geen fout).
func Strip(b []byte) []byte {
	if _, clen, hlen, ok := parseHeader(b); ok && hlen+clen <= len(b) {
		return b[hlen : hlen+clen]
	}
	return b
}

func makeWindow(content []byte, win int) ([]byte, error) {
	if win%sectorAlign != 0 {
		return nil, fmt.Errorf("window %d is not a multiple of %d", win, sectorAlign)
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		content = append(append([]byte{}, content...), '\n')
	}
	header := fmt.Sprintf("%s%d len=%010d\n", magic, win, len(content))
	if len(header)+len(content) > win {
		return nil, fmt.Errorf("config of %d bytes does not fit the %d-byte window on this image", len(content), win)
	}
	buf := make([]byte, 0, win)
	buf = append(buf, header...)
	buf = append(buf, content...)
	for pad := win - len(buf); pad > 0; pad = win - len(buf) {
		n := 65
		if pad < n {
			n = pad
		}
		line := bytes.Repeat([]byte{'#'}, n)
		line[n-1] = '\n'
		buf = append(buf, line...)
	}
	return buf, nil
}

func parseHeader(b []byte) (win, clen, hlen int, ok bool) {
	if !bytes.HasPrefix(b, []byte(magic)) {
		return 0, 0, 0, false
	}
	rest := b[len(magic):]
	sp := bytes.IndexByte(rest, ' ')
	if sp < 1 {
		return 0, 0, 0, false
	}
	win, err := strconv.Atoi(string(rest[:sp]))
	if err != nil || !bytes.HasPrefix(rest[sp:], []byte(" len=")) {
		return 0, 0, 0, false
	}
	digits := rest[sp+5:]
	if len(digits) < 11 || digits[10] != '\n' {
		return 0, 0, 0, false
	}
	clen, err = strconv.Atoi(string(digits[:10]))
	if err != nil {
		return 0, 0, 0, false
	}
	hlen = len(magic) + sp + 5 + 11
	if win < hlen+clen || win%sectorAlign != 0 {
		return 0, 0, 0, false
	}
	return win, clen, hlen, true
}

// scan zoekt een needle op élke byte-offset; verify mag kandidaten afkeuren.
func scan(f *os.File, needle []byte, verify func([]byte) bool) ([]int64, error) {
	var hits []int64
	buf := make([]byte, scanChunk+sectorAlign) // staart-overlap: kop < 512
	var off int64
	for off < scanLimit {
		n, err := f.ReadAt(buf, off)
		if n > 0 {
			// i blijft ónder scanChunk: de staart is er alleen zodat een
			// vondst op de chunkgrens volledig te toetsen is.
			for i := 0; i < scanChunk && i < n; {
				k := bytes.Index(buf[i:n], needle)
				if k < 0 || i+k >= scanChunk {
					break
				}
				i += k
				if verify == nil || verify(buf[i:n]) {
					hits = append(hits, off+int64(i))
				}
				i++
			}
		}
		if err == io.EOF || n < len(buf) {
			break
		}
		if err != nil {
			return nil, err
		}
		off += scanChunk
	}
	return hits, nil
}

// readAt leest n bytes vanaf off, óók als dat niet sector-uitgelijnd is.
//
// EEN RAUW DEVICE EIST UITLIJNING: /dev/rdiskN is een character device en
// accepteert alleen reads van een veelvoud van 512 bytes op een 512-uitgelijnd
// offset. Doe je dat niet, dan krijg je EINVAL — "invalid argument" — en dat is
// exact de fout waar het lezen van het config-venster op stukliep (gemeten op
// Dereks Mac, 06-08: het venster begint uitgelijnd, maar de config erbinnen
// begint ná de kopregel en is zelden een veelvoud van 512).
//
// Dus: lees het omliggende uitgelijnde bereik en snijd eruit wat gevraagd is.
func readAt(f *os.File, off int64, n int) ([]byte, error) {
	lo := off &^ (sectorAlign - 1)
	hi := (off + int64(n) + sectorAlign - 1) &^ (sectorAlign - 1)
	buf := make([]byte, hi-lo)
	got, err := f.ReadAt(buf, lo)
	if err != nil && got < int(off-lo)+n {
		return nil, err
	}
	return buf[off-lo : off-lo+int64(n)], nil
}

// writeAligned schrijft data op een willekeurige offset via read-modify-write
// van het omliggende sector-bereik — raw devices accepteren alleen
// sector-uitgelijnde I/O.
func writeAligned(f *os.File, off int64, data []byte) error {
	lo := off &^ (sectorAlign - 1)
	hi := (off + int64(len(data)) + sectorAlign - 1) &^ (sectorAlign - 1)
	buf, err := readAt(f, lo, int(hi-lo))
	if err != nil {
		return err
	}
	copy(buf[off-lo:], data)
	_, err = f.WriteAt(buf, lo)
	return err
}

// --- Sophgo-FIP (LicheeRV): checksums over de monitor-payload ---------------

func crcHqx(b []byte) uint16 {
	var crc uint16
	for _, c := range b {
		crc ^= uint16(c) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func fipCksum(b []byte) []byte {
	crc := crcHqx(b)
	return []byte{byte(crc), byte(crc >> 8), 0xFE, 0xCA}
}

// fipFix herstelt MONITOR_CKSUM (param2+48) en PARAM2_CKSUM (param2+8) als
// het venster binnen de monitor-payload ligt. Geen FIP in het image: no-op.
func fipFix(f *os.File, winOff int64, winLen int) error {
	p2s, err := scan(f, []byte("CVLD02\n\x00"), func(b []byte) bool {
		if len(b) < 64 {
			return false
		}
		ra := le32(b[60:64])
		return ra == 0 || (ra >= 0x8000_0000 && ra < 0x9000_0000)
	})
	if err != nil || len(p2s) == 0 {
		return err
	}
	if len(p2s) > 1 {
		return fmt.Errorf("fip: %d param2 blocks found at %v, expected 1 — checksum fix refused", len(p2s), p2s)
	}
	p2 := p2s[0]
	param2, err := readAt(f, p2, 4096)
	if err != nil {
		return err
	}
	fips, err := scan(f, []byte("CVBL01\n\x00"), nil)
	if err != nil {
		return err
	}
	var fipStart int64 = -1
	for _, s := range fips {
		if s <= p2 && s > fipStart {
			fipStart = s
		}
	}
	if fipStart < 0 {
		return fmt.Errorf("fip: param2 (CVLD02) at %#x but no fip header (CVBL01) before it", p2)
	}
	monOff := fipStart + int64(le32(param2[52:56]))
	monSize := int(le32(param2[56:60]))
	if winOff < monOff || winOff+int64(winLen) > monOff+int64(monSize) {
		return nil // venster ligt buiten de monitor — geen checksum-werk
	}
	monitor, err := readAt(f, monOff, monSize)
	if err != nil {
		return err
	}
	copy(param2[48:52], fipCksum(monitor))
	// PARAM2_CKSUM dekt param2 vanaf byte 12 — mét de verse MONITOR_CKSUM.
	copy(param2[8:12], fipCksum(param2[12:]))
	return writeAligned(f, p2+8, param2[8:52])
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

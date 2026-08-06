//go:build darwin

package disk

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// List geeft de schijven waar een kaart-image naartoe mag. Dat is precies:
// een héle fysieke schijf die verwisselbaar of extern is, en niet de schijf
// waar dit systeem van boot.
//
// WAAROM NIET `diskutil list external physical` (mijn eerste poging, en het
// koste Derek een lege lijst met de kaart al in de Mac): een INGEBOUWDE
// SD-lezer meldt `Device Location: Internal`. De kaart in een MacBook is dus
// "internal" en viel weg. Het veld dat een kaart van een systeemschijf
// scheidt is `Removable Media` — Removable tegenover Fixed (gemeten 06-08:
// disk4 "Built In SDXC Reader" Removable, disk0 "APPLE SSD" Fixed).
//
// Eén `diskutil info -all` levert alles in één procesaanroep, wat het ook
// geschikt maakt om elke paar seconden te herhalen (de UI doet dat: een kaart
// insteken hoort genoeg te zijn).
func List() ([]Device, error) {
	out, err := exec.Command("diskutil", "info", "-all").Output()
	if err != nil {
		return nil, fmt.Errorf("diskutil info -all: %v", err)
	}
	boot := bootDisk()
	var devs []Device
	for _, block := range strings.Split(string(out), "**********") {
		f := fields(block)
		id := f["Device Identifier"]
		if id == "" || f["Whole"] != "Yes" || f["Virtual"] == "Yes" {
			continue // partitie, container of een virtuele schijf
		}
		if id == boot {
			continue // nooit de schijf waar we van draaien
		}
		removable := strings.Contains(f["Removable Media"], "Removable")
		external := strings.EqualFold(f["Device Location"], "External")
		if !removable && !external {
			continue
		}
		size := bytesOf(f["Disk Size"])
		name := f["Device / Media Name"]
		if name == "" {
			name = f["Media Name"]
		}
		devs = append(devs, Device{
			ID:      id,
			RawPath: "/dev/r" + id,
			Desc:    fmt.Sprintf("%s — %s (%s)", id, name, humanSize(size)),
			Size:    size,
		})
	}
	return devs, nil
}

// IDs is de goedkope variant voor de auto-verversing: alleen de namen van de
// kandidaat-schijven, om te zien óf er iets veranderd is.
func IDs() []string {
	devs, err := List()
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(devs))
	for _, d := range devs {
		ids = append(ids, d.Desc)
	}
	return ids
}

// fields ontleedt één diskutil-blok: "  Sleutel:   waarde" per regel.
func fields(block string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(block, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k != "" && v != "" {
			m[k] = v
		}
	}
	return m
}

// bootDisk is de héle schijf waar de systeemvolume op staat — die mag nooit in
// de lijst komen.
func bootDisk() string {
	out, err := exec.Command("diskutil", "info", "/").Output()
	if err != nil {
		return ""
	}
	return fields(string(out))["Part of Whole"]
}

// bytesOf haalt het byte-getal uit "8.0 GB (7994343424 Bytes) (exactly …)".
func bytesOf(s string) int64 {
	i := strings.Index(s, "(")
	if i < 0 {
		return 0
	}
	f := strings.Fields(s[i+1:])
	if len(f) == 0 {
		return 0
	}
	n, _ := strconv.ParseInt(f[0], 10, 64)
	return n
}

// UnmountPath is Unmount voor een worker die alleen het rauwe pad kent
// (/dev/rdisk4 of /dev/disk4 → diskutil unmountDisk disk4).
func UnmountPath(raw string) error {
	id := strings.TrimPrefix(strings.TrimPrefix(raw, "/dev/"), "r")
	if !strings.HasPrefix(id, "disk") {
		return fmt.Errorf("not a disk device: %s", raw)
	}
	return Unmount(Device{ID: id})
}

// Prepare maakt een doel klaar voor raw I/O en geeft terug wat er ná het
// sluiten moet gebeuren. Op macOS is dat simpel — ontkoppelen en klaar; op
// Windows moeten de volume-sloten vastgehouden worden zolang je schrijft, en
// daar is deze vorm voor.
func Prepare(raw string) (func(), error) {
	if err := UnmountPath(raw); err != nil {
		return nil, err
	}
	return func() {}, nil
}

// Unmount koppelt alle volumes van de schijf los — verplicht vóór een raw
// write: macOS weigert te schrijven naar een disk met gemounte volumes.
func Unmount(d Device) error {
	if out, err := exec.Command("diskutil", "unmountDisk", d.ID).CombinedOutput(); err != nil {
		return fmt.Errorf("diskutil unmountDisk %s: %s", d.ID, strings.TrimSpace(string(out)))
	}
	return nil
}

// Eject werpt de kaart uit na het schrijven — dan is eruit trekken altijd veilig.
func Eject(d Device) error {
	if out, err := exec.Command("diskutil", "eject", d.ID).CombinedOutput(); err != nil {
		return fmt.Errorf("diskutil eject %s: %s", d.ID, strings.TrimSpace(string(out)))
	}
	return nil
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

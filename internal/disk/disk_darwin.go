//go:build darwin

package disk

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// List geeft de externe, fysieke schijven — precies wat `diskutil list
// external physical` als /dev/diskN meldt. Interne schijven blijven bewust
// buiten beeld: dit is een flash-tool, en de systeemschijf hoort nooit in een
// keuzelijst te staan.
func List() ([]Device, error) {
	out, err := exec.Command("diskutil", "list", "external", "physical").Output()
	if err != nil {
		return nil, fmt.Errorf("diskutil list: %v", err)
	}
	var devs []Device
	re := regexp.MustCompile(`(?m)^/dev/(disk\d+) \(external, physical\):`)
	for _, m := range re.FindAllStringSubmatch(string(out), -1) {
		id := m[1]
		name, size := info(id)
		desc := fmt.Sprintf("%s — %s (%s)", id, name, humanSize(size))
		devs = append(devs, Device{ID: id, RawPath: "/dev/r" + id, Desc: desc, Size: size})
	}
	return devs, nil
}

// info leest medianaam en grootte uit `diskutil info` — regelgebaseerd, want
// diskutil heeft geen JSON en een plist-parser is dit niet waard.
func info(id string) (string, int64) {
	out, err := exec.Command("diskutil", "info", id).Output()
	if err != nil {
		return "?", 0
	}
	name, size := "?", int64(0)
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "Device / Media Name", "Media Name":
			name = strings.TrimSpace(v)
		case "Disk Size", "Total Size":
			// "63.9 GB (63864569856 Bytes) (exactly ...)"
			if i := strings.Index(v, "("); i >= 0 {
				f := strings.Fields(v[i+1:])
				if len(f) > 0 {
					size, _ = strconv.ParseInt(f[0], 10, 64)
				}
			}
		}
	}
	return name, size
}

// UnmountPath is Unmount voor een worker die alleen het rauwe pad kent
// (/dev/rdisk4 of /dev/disk4 → diskutil unmountDisk disk4).
func UnmountPath(raw string) error {
	id := strings.TrimPrefix(strings.TrimPrefix(raw, "/dev/"), "r")
	if id == "" || !strings.HasPrefix(id, "disk") {
		return fmt.Errorf("not a disk device: %s", raw)
	}
	return Unmount(Device{ID: id})
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

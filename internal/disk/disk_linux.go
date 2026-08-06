//go:build linux

package disk

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// List geeft de schijven waar een kaart-image naartoe mag: hele schijven die
// verwisselbaar zijn of aan USB hangen, nooit de schijf met de root erop.
//
// lsblk levert dat in JSON, dus geen /sys-gewandel: één aanroep met de velden
// die de keuze bepalen. Zelfde criteria als op macOS (verwisselbaar óf extern,
// minus de systeemschijf) — alleen de bron van de waarheid verschilt.
func List() ([]Device, error) {
	out, err := exec.Command("lsblk", "-J", "-b", "-o", "NAME,SIZE,MODEL,VENDOR,RM,TYPE,TRAN,MOUNTPOINT").Output()
	if err != nil {
		return nil, fmt.Errorf("lsblk: %v", err)
	}
	var tree struct {
		Blockdevices []lsblkNode `json:"blockdevices"`
	}
	if err := json.Unmarshal(out, &tree); err != nil {
		return nil, fmt.Errorf("lsblk json: %v", err)
	}
	var devs []Device
	for _, n := range tree.Blockdevices {
		if n.Type != "disk" || n.holdsRoot() {
			continue
		}
		if !n.RM && !strings.EqualFold(n.Tran, "usb") {
			continue
		}
		name := strings.TrimSpace(n.Vendor + " " + n.Model)
		if name == "" {
			name = n.Name
		}
		devs = append(devs, Device{
			ID:      n.Name,
			RawPath: "/dev/" + n.Name,
			Desc:    fmt.Sprintf("%s — %s (%s)", n.Name, name, humanSize(n.Size)),
			Size:    n.Size,
		})
	}
	return devs, nil
}

type lsblkNode struct {
	Name       string      `json:"name"`
	Size       int64       `json:"size"`
	Model      string      `json:"model"`
	Vendor     string      `json:"vendor"`
	RM         bool        `json:"rm"`
	Type       string      `json:"type"`
	Tran       string      `json:"tran"`
	Mountpoint string      `json:"mountpoint"`
	Children   []lsblkNode `json:"children"`
}

// holdsRoot: staat er ergens onder deze schijf de root of /boot gemount, dan is
// het de systeemschijf en hoort hij niet in een flash-lijst.
func (n lsblkNode) holdsRoot() bool {
	if n.Mountpoint == "/" || n.Mountpoint == "/boot" || strings.HasPrefix(n.Mountpoint, "/boot/") {
		return true
	}
	for _, c := range n.Children {
		if c.holdsRoot() {
			return true
		}
	}
	return false
}

// IDs is de goedkope variant voor de auto-verversing.
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

// Unmount koppelt elke partitie van de schijf los. udisksctl eerst: dat mag een
// gewone gebruiker via polkit, en het is de weg die de desktop zelf gebruikt.
// Lukt dat niet, dan umount (root of een fstab-regel met user).
func Unmount(d Device) error {
	parts, err := partitions(d.ID)
	if err != nil {
		return err
	}
	var last error
	for _, p := range parts {
		if exec.Command("udisksctl", "unmount", "-b", p).Run() == nil {
			continue
		}
		if out, err := exec.Command("umount", p).CombinedOutput(); err != nil {
			msg := strings.TrimSpace(string(out))
			// Niet gemount is precies wat we willen; dat is geen fout.
			if !strings.Contains(msg, "not mounted") && !strings.Contains(msg, "niet aangekoppeld") {
				last = fmt.Errorf("umount %s: %s", p, msg)
			}
		}
	}
	return last
}

func UnmountPath(raw string) error {
	id := strings.TrimPrefix(raw, "/dev/")
	if id == "" {
		return fmt.Errorf("not a disk device: %s", raw)
	}
	return Unmount(Device{ID: id})
}

// Eject zet de schijf uit als udisks dat kan; anders is unmounten genoeg (op
// Linux is een kaart eruit trekken na unmount veilig).
func Eject(d Device) error {
	if err := Unmount(d); err != nil {
		return err
	}
	exec.Command("udisksctl", "power-off", "-b", d.RawPath).Run()
	return nil
}

func partitions(id string) ([]string, error) {
	out, err := exec.Command("lsblk", "-J", "-o", "NAME,TYPE", "/dev/"+id).Output()
	if err != nil {
		return nil, fmt.Errorf("lsblk %s: %v", id, err)
	}
	var tree struct {
		Blockdevices []lsblkNode `json:"blockdevices"`
	}
	if err := json.Unmarshal(out, &tree); err != nil {
		return nil, err
	}
	var parts []string
	for _, n := range tree.Blockdevices {
		for _, c := range n.Children {
			if c.Type == "part" {
				parts = append(parts, "/dev/"+c.Name)
			}
		}
	}
	return parts, nil
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

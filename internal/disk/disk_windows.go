//go:build windows

package disk

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

// List geeft de fysieke schijven waar een kaart-image naartoe mag: aan USB of
// als verwisselbaar medium gemeld, en nooit de schijf waar Windows zelf op
// staat. De bron is WMI (Win32_DiskDrive), want dat mag een gewone gebruiker
// lezen — je hebt pas beheerdersrechten nodig om te SCHRIJVEN.
func List() ([]Device, error) {
	out, err := ps(`Get-CimInstance -ClassName Win32_DiskDrive |
		Select-Object Index,DeviceID,Model,Size,InterfaceType,MediaType |
		ConvertTo-Json -Compress -AsArray`)
	if err != nil {
		return nil, err
	}
	var drives []struct {
		Index         int
		DeviceID      string
		Model         string
		Size          json.Number
		InterfaceType string
		MediaType     string
	}
	if err := json.Unmarshal([]byte(out), &drives); err != nil {
		return nil, fmt.Errorf("Win32_DiskDrive json: %v", err)
	}
	sys, _ := systemDisk()
	var devs []Device
	for _, d := range drives {
		if d.Index == sys {
			continue // nooit de schijf waar Windows van draait
		}
		usb := strings.EqualFold(d.InterfaceType, "USB")
		removable := strings.Contains(strings.ToLower(d.MediaType), "removable")
		if !usb && !removable {
			continue
		}
		size, _ := strconv.ParseInt(d.Size.String(), 10, 64)
		name := strings.TrimSpace(d.Model)
		if name == "" {
			name = d.DeviceID
		}
		devs = append(devs, Device{
			ID: strconv.Itoa(d.Index),
			// Het rauwe pad van een fysieke schijf. Let op de dubbele
			// backslashes: dit is de Win32-devicenaam, geen bestandspad.
			RawPath: fmt.Sprintf(`\\.\PhysicalDrive%d`, d.Index),
			Desc:    fmt.Sprintf("disk %d — %s (%s)", d.Index, name, humanSize(size)),
			Size:    size,
		})
	}
	return devs, nil
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

// systemDisk is het schijfnummer waar de Windows-partitie op staat.
func systemDisk() (int, error) {
	out, err := ps(`(Get-Partition -DriveLetter $env:SystemDrive.Trim(':')).DiskNumber`)
	if err != nil {
		return -1, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return -1, err
	}
	return n, nil
}

// Unmount is op Windows GEEN losse stap die je vooraf doet en daarna vergeet.
//
// Windows remount een volume zodra het de kans krijgt, en dan schrijft de
// filesystem-driver over je image heen (of hij weigert je writes). De enige
// werkende manier is: elk volume van de schijf openen, LOCK_VOLUME en
// DISMOUNT_VOLUME sturen, en die handles OPEN HOUDEN zolang je schrijft. Zie
// Lock hieronder; deze functie bestaat alleen zodat het contract met de andere
// platforms klopt en doet dus niets blijvends.
func Unmount(d Device) error {
	release, err := Lock(d.ID)
	if err != nil {
		return err
	}
	release()
	return nil
}

func UnmountPath(raw string) error {
	return Unmount(Device{ID: diskNumberOf(raw)})
}

// Prepare is op Windows het zware werk: de volumes moeten GELOCKT EN
// ONTKOPPELD blijven zolang er geschreven wordt, dus geeft hij de sloten pas
// vrij als de aanroeper klaar is.
func Prepare(raw string) (func(), error) {
	return Lock(diskNumberOf(raw))
}

// Lock ontkoppelt elk volume van deze schijf en houdt de sloten vast tot de
// teruggegeven functie wordt aangeroepen. ZONDER DIT is een raw write naar
// \\.\PhysicalDriveN op Windows onbetrouwbaar: het volume kan tussentijds
// terugkomen.
func Lock(id string) (release func(), err error) {
	letters, err := volumeLetters(id)
	if err != nil {
		return nil, err
	}
	var handles []windows.Handle
	closeAll := func() {
		for _, h := range handles {
			windows.CloseHandle(h)
		}
	}
	for _, l := range letters {
		h, err := openVolume(l)
		if err != nil {
			// Een volume dat we niet kunnen openen is een volume dat we niet
			// mogen overschrijven: dan stoppen we, in plaats van half te
			// ontkoppelen en toch te schrijven.
			closeAll()
			return nil, fmt.Errorf("volume %s: %v (writing needs administrator rights)", l, err)
		}
		handles = append(handles, h)
		if err := ioctl(h, fsctlLockVolume); err != nil {
			closeAll()
			return nil, fmt.Errorf("lock %s: %v (something still has files open there)", l, err)
		}
		if err := ioctl(h, fsctlDismountVolume); err != nil {
			closeAll()
			return nil, fmt.Errorf("dismount %s: %v", l, err)
		}
	}
	return func() {
		closeAll()
		// Windows de partitietabel opnieuw laten lezen, zodat de verse kaart
		// meteen klopt in de Verkenner.
		if h, err := openPhysical(id); err == nil {
			ioctl(h, ioctlDiskUpdateProperties)
			windows.CloseHandle(h)
		}
	}, nil
}

// Eject werpt de schijf uit (zelfde IOCTL die "veilig verwijderen" gebruikt).
func Eject(d Device) error {
	h, err := openPhysical(d.ID)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	if err := ioctl(h, ioctlStorageEjectMedia); err != nil {
		// Een kaartlezer die niet uitwerpt is geen fout: na ontkoppelen is de
		// kaart eruit halen veilig.
		return nil
	}
	return nil
}

// De IOCTL's die dit pad nodig heeft (winioctl.h).
const (
	fsctlLockVolume           = 0x00090018
	fsctlDismountVolume       = 0x00090020
	ioctlDiskUpdateProperties = 0x00070140
	ioctlStorageEjectMedia    = 0x002D4808
)

func ioctl(h windows.Handle, code uint32) error {
	var ret uint32
	return windows.DeviceIoControl(h, code, nil, 0, nil, 0, &ret, nil)
}

func openVolume(letter string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(`\\.\` + letter + `:`)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
}

func openPhysical(id string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(`\\.\PhysicalDrive` + id)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
}

// volumeLetters geeft de driveletters die op deze fysieke schijf staan.
func volumeLetters(id string) ([]string, error) {
	out, err := ps(fmt.Sprintf(`Get-Partition -DiskNumber %s -ErrorAction SilentlyContinue |
		Where-Object DriveLetter |
		ForEach-Object { $_.DriveLetter }`, id))
	if err != nil {
		return nil, err
	}
	var letters []string
	for _, l := range strings.Fields(out) {
		if l = strings.TrimSpace(l); l != "" {
			letters = append(letters, l)
		}
	}
	return letters, nil
}

func diskNumberOf(raw string) string {
	if i := strings.LastIndex(strings.ToLower(raw), "physicaldrive"); i >= 0 {
		return raw[i+len("physicaldrive"):]
	}
	return raw
}

// ps draait één PowerShell-opdracht zonder profiel (snel, en niet afhankelijk
// van wat de gebruiker in zijn profiel heeft staan).
func ps(script string) (string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("powershell: %v", err)
	}
	return string(out), nil
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

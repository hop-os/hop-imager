//go:build !darwin

package disk

import "fmt"

// Windows (\\.\PhysicalDriveN + UAC) en Linux (lsblk + udisks/sudo) komen als
// de tool naar zijn eigen repo verhuist — de UI en het schrijfpad zijn er al
// op voorbereid (alles loopt via Device.RawPath).
func List() ([]Device, error) {
	return nil, fmt.Errorf("device listing is not implemented on this OS yet (macOS first)")
}

func IDs() []string                { return nil }
func Unmount(d Device) error       { return nil }
func Eject(d Device) error         { return nil }
func UnmountPath(raw string) error { return nil }

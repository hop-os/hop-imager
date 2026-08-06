//go:build !darwin && !linux && !windows

package disk

import "fmt"

// Windows is de laatste die nog wacht: \\.\PhysicalDriveN openen vraagt naast
// UAC ook een expliciete FSCTL_LOCK_VOLUME/DISMOUNT_VOLUME per volume, en dat
// wil ik geschreven hebben met een Windows-machine ernaast in plaats van op
// goed geluk. Tot dan doet de Windows-build FIND en het bewerken van een
// image-BESTAND; een kaart schrijven is er nog niet. De UI en alle
// bewerkingen zijn er al op voorbereid: alles loopt via Device.RawPath.
func List() ([]Device, error) {
	return nil, fmt.Errorf("device listing is not implemented on this OS yet (macOS first)")
}

func IDs() []string                      { return nil }
func Prepare(raw string) (func(), error) { return func() {}, nil }
func Unmount(d Device) error             { return nil }
func Eject(d Device) error               { return nil }
func UnmountPath(raw string) error       { return nil }

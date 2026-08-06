//go:build !darwin && !windows

package priv

import "os"

// Op Linux is een verhoogd proces genoeg: daar is os.OpenFile het hele verhaal
// (root of lidmaatschap van de disk-groep). macOS heeft er een helper voor
// nodig (open_darwin.go) en Windows een UAC-elevatie plus volume-locks
// (open_windows.go).
func OpenDevice(path string, write bool) (*os.File, error) {
	flag := os.O_RDONLY
	if write {
		flag = os.O_RDWR
	}
	return os.OpenFile(path, flag, 0)
}

// NeedsElevation/Elevate: alleen Windows kan zichzelf verhogen. Op Linux geldt
// de gewone regel — start met sudo of zit in de disk-groep — en op macOS regelt
// authopen het per bewerking.
func NeedsElevation() bool { return false }
func Elevate() error       { return nil }

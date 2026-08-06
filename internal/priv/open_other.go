//go:build !darwin

package priv

import "os"

// Op Windows (\\.\PhysicalDriveN na UAC-elevatie) en Linux (root of een
// udisks-fd) is een verhoogd proces genoeg: daar is os.OpenFile het hele
// verhaal. macOS heeft er een helper voor nodig, zie open_darwin.go.
func OpenDevice(path string, write bool) (*os.File, error) {
	flag := os.O_RDONLY
	if write {
		flag = os.O_RDWR
	}
	return os.OpenFile(path, flag, 0)
}

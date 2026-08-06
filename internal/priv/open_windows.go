//go:build windows

package priv

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// OpenDevice opent een fysieke schijf (\\.\PhysicalDriveN). Op Windows is
// SCHRIJVEN het deel dat rechten vraagt: lezen mag een gewone gebruiker, maar
// een raw write naar een fysieke schijf vraagt beheerdersrechten — en de
// volumes van die schijf moeten bovendien gelockt en ontkoppeld zijn (dat doet
// internal/disk.Lock, en die sloten moeten open blijven tijdens het schrijven).
//
// Go's os.OpenFile gebruikt onder water CreateFile met FILE_SHARE_READ|WRITE,
// wat precies is wat hier nodig is; de dubbele backslashes in het pad maken het
// een Win32-devicenaam en geen bestandspad.
func OpenDevice(path string, write bool) (*os.File, error) {
	flag := os.O_RDONLY
	if write {
		flag = os.O_RDWR
		if !IsElevated() {
			return nil, fmt.Errorf("writing to %s needs administrator rights — restart the app as administrator", path)
		}
	}
	f, err := os.OpenFile(path, flag, 0)
	if err != nil && !IsElevated() {
		return nil, fmt.Errorf("%v — this needs administrator rights", err)
	}
	return f, err
}

// IsElevated zegt of dit proces beheerdersrechten heeft.
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// NeedsElevation: op Windows moet je verhoogd draaien om te kunnen schrijven, en
// dat kan een proces niet zelf worden — het moet opnieuw starten.
func NeedsElevation() bool { return !IsElevated() }

// Elevate start dezelfde applicatie opnieuw met de UAC-prompt ("Als
// administrator uitvoeren"). De aanroeper stopt daarna zichzelf: één venster,
// nu met de rechten die schrijven vraagt.
func Elevate() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	cwd, _ := windows.UTF16PtrFromString(filepathDir(exe))
	return windows.ShellExecute(0, verb, file, nil, cwd, windows.SW_NORMAL)
}

// filepathDir zonder path/filepath te importeren voor één regel.
func filepathDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '\\' || p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}

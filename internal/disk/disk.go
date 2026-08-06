// Package disk somt verwisselbare schijven op en ontkoppelt ze vóór het
// schrijven. Per OS een eigen implementatie; macOS eerst (diskutil), Windows
// en Linux volgen in de eigen repo.
package disk

// Device is één kandidaat-doelschijf.
type Device struct {
	ID      string // "disk4"
	RawPath string // "/dev/rdisk4" — het raw device waarop geschreven wordt
	Desc    string // mensleesbaar: naam + grootte
	Size    int64  // bytes
}

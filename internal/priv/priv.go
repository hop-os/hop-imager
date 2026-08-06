// Package priv regelt de rechten voor rauwe schijftoegang. Op macOS is dat
// authopen (open_darwin.go); elders is een verhoogd proces genoeg
// (open_other.go). Zie de uitleg in open_darwin.go voor waarom root op macOS
// niet volstaat.
package priv

import "os"

// IsRoot is nog steeds nuttig als diagnose: draait de tool per ongeluk als
// root (sudo), dan is dat geen fout maar wel het vermelden waard — de tool
// heeft het niet nodig.
func IsRoot() bool { return os.Geteuid() == 0 }

//go:build !darwin

package priv

import "fmt"

// Windows (UAC via een verhoogde helper) en Linux (pkexec/sudo -A) volgen als
// de tool naar zijn eigen repo verhuist; het worker-protocol is er al op
// ontworpen — alleen deze functie verschilt per OS.
func Escalate(args []string) (string, error) {
	return "", fmt.Errorf("privilege escalation is not implemented on this OS yet (macOS first) — run the tool as root")
}

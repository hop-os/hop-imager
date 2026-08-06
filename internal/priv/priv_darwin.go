//go:build darwin

package priv

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Escalate start dezelfde binary als root met args en geeft zijn stdout terug.
// De systeem-wachtwoordprompt komt van osascript's `with administrator
// privileges` — hetzelfde dialoog dat elke Mac-app gebruikt om even root te
// mogen zijn.
func Escalate(args []string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(args)+1)
	for _, a := range append([]string{exe}, args...) {
		parts = append(parts, shellQuote(a))
	}
	// stderr mee naar stdout: een fout van de worker moet in de melding staan,
	// niet in een venster dat niemand ziet.
	cmd := strings.Join(parts, " ") + " 2>&1"
	script := fmt.Sprintf("do shell script %s with administrator privileges", appleQuote(cmd))
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "-128") { // gebruiker klikte Annuleer
			return "", fmt.Errorf("cancelled — this needs administrator rights")
		}
		return "", fmt.Errorf("%s", msg)
	}
	return string(out), nil
}

// shellQuote maakt van één argument een veilig shell-woord.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// appleQuote maakt van de commandoregel één AppleScript-string.
func appleQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

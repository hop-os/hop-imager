// Package priv escaleert naar root wanneer een bewerking dat nodig heeft:
// dezelfde binary wordt als root opnieuw gestart met een -op-vlag, en die
// worker doet het echte werk (unmounten, raw schrijven, het config-venster
// lezen of herschrijven).
//
// WAAROM ZO en niet "start de tool maar met sudo" (Derek 06-08: "gewoon
// escaleren naar sudo, unmounten en writen, dat werkt altijd het beste"):
//
//   - de GUI blijft een gewoon gebruikersproces (voorkeuren, vensterserver,
//     Finder-integratie) en alleen de paar seconden die het écht nodig hebben
//     lopen als root;
//   - het is één pad voor élk board: raw op /dev/rdiskN werkt ook als het OS
//     de partitie niet mount (de Radxa) of als de config in de kernel zit (de
//     LicheeRV) — dus is de configbewerking overal hetzelfde;
//   - de gebruiker ziet de vertrouwde systeem-wachtwoordprompt in plaats van
//     een instructie in een logvenster.
//
// Voortgang gaat via een bestand: de worker schrijft "done total" en de GUI
// leest dat elke 150ms. Simpeler dan een socket, en het werkt door de
// osascript-escalatie heen (die geeft stdout pas aan het eind terug).
package priv

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// IsRoot zegt of dit proces het werk zelf mag doen.
func IsRoot() bool { return os.Geteuid() == 0 }

// Progress leest het voortgangsbestand van de worker (0 als er nog niets staat).
func Progress(path string) (done, total int64) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0
	}
	f := strings.Fields(string(b))
	if len(f) != 2 {
		return 0, 0
	}
	done, _ = strconv.ParseInt(f[0], 10, 64)
	total, _ = strconv.ParseInt(f[1], 10, 64)
	return done, total
}

// WriteProgress is de worker-kant van Progress.
func WriteProgress(path string, done, total int64) {
	if path == "" {
		return
	}
	os.WriteFile(path, []byte(fmt.Sprintf("%d %d", done, total)), 0644)
}

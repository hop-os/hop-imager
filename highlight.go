// highlight.go — kleuren voor een hopos.cfg, en alleen voor wat dat formaat
// ÉCHT kan. Geen parser-bibliotheek, geen taalgrammatica: de config is
// regel-gebaseerd (`key=value`, `#` is commentaar — zie fw/bootcfg in HopOS), en
// dat is met een handvol regels te lezen.
//
// Getekend met widget.TextGrid, dat Fyne zelf al meebrengt "for a text editor,
// code preview or terminal emulator" — dus per teken een kleur zonder één extra
// afhankelijkheid.
//
// Wat de kleuren betekenen (palet van gethop.org, zie theme.go):
//
//	fosforgroen  sectiekop (`# --- Identity ---`) — waar een blok begint
//	grijs        commentaar, en de tekens die alleen structuur zijn (= { } : ,)
//	blauw        de sleutel
//	wit          de waarde
//	koper        de waarde van een GEHEIM (apikey, s3.secret, s3.key) én getallen
//	leafgroen    strings binnen een JSON-jobregel
//
// Die koperen geheimen zijn geen versiering: een config die open op je scherm
// staat hoort te laten zien waar de sleutels staan.
package main

import (
	"strings"

	"fyne.io/fyne/v2/widget"
)

// De stijlen, één keer gemaakt: TextGrid houdt een pointer per cel vast.
var (
	stSection = &widget.CustomTextGridStyle{FGColor: hopPhosphor}
	stComment = &widget.CustomTextGridStyle{FGColor: hopMuted}
	stKey     = &widget.CustomTextGridStyle{FGColor: hopWave}
	stPunct   = &widget.CustomTextGridStyle{FGColor: hopMuted}
	stValue   = &widget.CustomTextGridStyle{FGColor: hopText}
	stSecret  = &widget.CustomTextGridStyle{FGColor: hopCopper}
	stString  = &widget.CustomTextGridStyle{FGColor: hopLeaf}
	stNumber  = &widget.CustomTextGridStyle{FGColor: hopCopper}
)

// secretKeys zijn de sleutels waarvan de WAARDE een geheim is. Precies deze
// drie, uit docs/config.md — niet gokken op namen die "key" bevatten, want
// hopos.s3.pathstyle en hopos.apps[] zijn dat niet.
var secretKeys = map[string]bool{
	"hopos.apikey":    true,
	"hopos.s3.secret": true,
	"hopos.s3.key":    true,
}

// highlight zet de tekst in het grid en kleurt hem per regel.
func highlight(tg *widget.TextGrid, text string) {
	tg.SetText(text)
	for row, line := range strings.Split(text, "\n") {
		paintLine(tg, row, line)
	}
	tg.Refresh()
}

func paintLine(tg *widget.TextGrid, row int, line string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}
	n := len([]rune(line))

	// Commentaar. Een sectiekop (`# --- … ---`) krijgt fosforgroen: dat zijn de
	// ankers waarmee je door zo'n config heen leest.
	if strings.HasPrefix(trimmed, "#") {
		st := stComment
		if strings.Contains(trimmed, "---") {
			st = stSection
		}
		tg.SetStyleRange(row, 0, row, n-1, st)
		return
	}

	eq := strings.IndexRune(line, '=')
	if eq < 0 {
		tg.SetStyleRange(row, 0, row, n-1, stValue)
		return
	}
	key := strings.TrimSpace(line[:eq])
	eqCol := len([]rune(line[:eq]))

	tg.SetStyleRange(row, 0, row, eqCol-1, stKey)
	tg.SetStyle(row, eqCol, stPunct)
	if eqCol+1 > n-1 {
		return // sleutel zonder waarde
	}

	value := line[eq+1:]
	switch {
	case secretKeys[key]:
		tg.SetStyleRange(row, eqCol+1, row, n-1, stSecret)
	case strings.HasPrefix(strings.TrimSpace(value), "{"):
		paintJSON(tg, row, eqCol+1, value)
	default:
		tg.SetStyleRange(row, eqCol+1, row, n-1, stValue)
	}
}

// paintJSON kleurt een jobregel (hopos.init[]/hopos.apps[]): strings groen,
// getallen koper, en de haken en dubbele punten grijs zodat de inhoud opvalt en
// niet de interpunctie. Geen JSON-parser: één pass over de tekens is genoeg voor
// compacte JSON op één regel, en een onafgesloten string kleurt dan gewoon tot
// het eind — dat is precies de fout die je wil zien.
func paintJSON(tg *widget.TextGrid, row, col int, value string) {
	inString := false
	escaped := false
	for i, r := range []rune(value) {
		c := col + i
		switch {
		case escaped:
			escaped = false
			tg.SetStyle(row, c, stString)
		case r == '\\' && inString:
			escaped = true
			tg.SetStyle(row, c, stString)
		case r == '"':
			inString = !inString
			tg.SetStyle(row, c, stString)
		case inString:
			tg.SetStyle(row, c, stString)
		case r >= '0' && r <= '9':
			tg.SetStyle(row, c, stNumber)
		case strings.ContainsRune("{}[]:,", r):
			tg.SetStyle(row, c, stPunct)
		default:
			tg.SetStyle(row, c, stValue)
		}
	}
}

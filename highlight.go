// highlight.go — kleuren voor een hopos.cfg, en alleen voor wat dat formaat
// ÉCHT kan. Geen parser-bibliotheek, geen taalgrammatica: de config is
// regel-gebaseerd (`key=value`, `#` is commentaar — zie fw/bootcfg in HopOS), en
// dat is met een handvol regels te lezen.
//
// Dit bestand maakt geen plaatje maar een lijst SEGMENTEN: stukjes tekst met
// een kleurnaam. Die gaan in een widget.RichText die precies onder het
// tekstveld ligt (editor.go), zodat de kleuren er zijn terwijl je typt.
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

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// De kleurnamen. Een fyne.ThemeColorName is gewoon een string, dus we mogen er
// zelf verzinnen zolang het editor-thema ze beantwoordt (theme.go).
const (
	cSection fyne.ThemeColorName = "hopSection"
	cComment fyne.ThemeColorName = "hopComment"
	cKey     fyne.ThemeColorName = "hopKey"
	cPunct   fyne.ThemeColorName = "hopPunct"
	cValue   fyne.ThemeColorName = "hopValue"
	cSecret  fyne.ThemeColorName = "hopSecret"
	cString  fyne.ThemeColorName = "hopString"
	cNumber  fyne.ThemeColorName = "hopNumber"
)

// secretKeys zijn de sleutels waarvan de WAARDE een geheim is. Precies deze
// drie, uit docs/config.md — niet gokken op namen die "key" bevatten, want
// hopos.s3.pathstyle en hopos.apps[] zijn dat niet.
var secretKeys = map[string]bool{
	"hopos.apikey":    true,
	"hopos.s3.secret": true,
	"hopos.s3.key":    true,
}

// run is een stuk tekst van één kleur.
type run struct {
	text  string
	color fyne.ThemeColorName
}

var monoStyle = fyne.TextStyle{Monospace: true}

// segmentsFor maakt de gekleurde laag voor een hele config.
//
// Elke regel eindigt met een segment dat NIET inline is: dat is wat RichText
// als regeleinde leest. Een lege regel is dus één leeg segment — en die telt
// mee, anders lopen de kleuren en de letters uit de pas.
func segmentsFor(text string) []widget.RichTextSegment {
	lines := strings.Split(text, "\n")
	out := make([]widget.RichTextSegment, 0, len(lines)*3)
	for _, line := range lines {
		runs := runsFor(line)
		for i, r := range runs {
			out = append(out, &widget.TextSegment{
				Text: r.text,
				Style: widget.RichTextStyle{
					TextStyle: monoStyle,
					ColorName: r.color,
					Inline:    i < len(runs)-1,
				},
			})
		}
	}
	return out
}

func runsFor(line string) []run {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return []run{{text: line, color: cValue}}
	}

	// Commentaar. Een sectiekop (`# --- … ---`) krijgt fosforgroen: dat zijn de
	// ankers waarmee je door zo'n config heen leest.
	if strings.HasPrefix(trimmed, "#") {
		c := cComment
		if strings.Contains(trimmed, "---") {
			c = cSection
		}
		return []run{{text: line, color: c}}
	}

	eq := strings.IndexRune(line, '=')
	if eq < 0 {
		return []run{{text: line, color: cValue}}
	}
	key, value := strings.TrimSpace(line[:eq]), line[eq+1:]
	runs := []run{{text: line[:eq], color: cKey}, {text: "=", color: cPunct}}
	switch {
	case value == "":
	case secretKeys[key]:
		runs = append(runs, run{text: value, color: cSecret})
	case strings.HasPrefix(strings.TrimSpace(value), "{"):
		runs = append(runs, jsonRuns(value)...)
	default:
		runs = append(runs, run{text: value, color: cValue})
	}
	return runs
}

// jsonRuns kleurt een jobregel (hopos.init[]/hopos.apps[]): strings groen,
// getallen koper, en de haken en dubbele punten grijs zodat de inhoud opvalt en
// niet de interpunctie. Geen JSON-parser: één pass over de tekens is genoeg voor
// compacte JSON op één regel, en een onafgesloten string kleurt dan gewoon tot
// het eind — dat is precies de fout die je wil zien.
//
// Tekens van dezelfde kleur worden samengevoegd: elk segment is straks een
// canvas-object, en per teken één object maakt een grote config traag.
func jsonRuns(value string) []run {
	var out []run
	var b strings.Builder
	cur := cValue
	flush := func() {
		if b.Len() > 0 {
			out = append(out, run{text: b.String(), color: cur})
			b.Reset()
		}
	}
	inString, escaped := false, false
	for _, r := range value {
		var c fyne.ThemeColorName
		switch {
		case escaped:
			escaped, c = false, cString
		case r == '\\' && inString:
			escaped, c = true, cString
		case r == '"':
			inString, c = !inString, cString
		case inString:
			c = cString
		case r >= '0' && r <= '9':
			c = cNumber
		case strings.ContainsRune("{}[]:,", r):
			c = cPunct
		default:
			c = cValue
		}
		if c != cur {
			flush()
			cur = c
		}
		b.WriteRune(r)
	}
	flush()
	return out
}

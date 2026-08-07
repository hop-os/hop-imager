// edittest — WEGGOOIWERK. Meetinstrument voor één vraag: kun je een gekleurde
// laag precies onder een widget.Entry leggen, zodat je kunt TYPEN in tekst die
// kleur heeft?
//
// Eerste poging was een widget.TextGrid eronder. Dat liep uit de pas: het grid
// zet elke regel op een vaste celhoogte, de Entry meet zijn regels via
// RichText, en over ~48 regels scheelde dat een volle regel.
//
// Tweede poging (dit bestand): de gekleurde laag is zélf een widget.RichText —
// hetzelfde ding dat de Entry intern gebruikt. Zelfde meetlat, dus per
// constructie dezelfde regelhoogte. De Entry tekent hier in half-doorzichtig
// rood over de gekleurde tekst, dus elke afwijking is dubbel beeld.
package main

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Eigen kleurnamen: een ThemeColorName is gewoon een string, dus we mogen er
// zelf verzinnen zolang ons thema ze beantwoordt.
const (
	colKey fyne.ThemeColorName = "hopKey"
	colVal fyne.ThemeColorName = "hopVal"
)

type overlayTheme struct {
	fyne.Theme
	fg color.Color // de kleur van de Entry-tekst zelf
}

func (t overlayTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameForeground:
		return t.fg
	case theme.ColorNameInputBackground, theme.ColorNameInputBorder:
		return color.Transparent // anders dekt de Entry de laag eronder af
	case colKey:
		return color.NRGBA{R: 0x6B, G: 0xA6, B: 0xFF, A: 0xFF}
	case colVal:
		return color.NRGBA{R: 0x46, G: 0xD9, B: 0x79, A: 0xFF}
	}
	return t.Theme.Color(n, v)
}

func (t overlayTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameLineSpacing:
		return 0
	}
	return t.Theme.Size(n)
}

func segments(text string) []widget.RichTextSegment {
	mono := fyne.TextStyle{Monospace: true}
	var out []widget.RichTextSegment
	add := func(s string, c fyne.ThemeColorName, inline bool) {
		out = append(out, &widget.TextSegment{
			Text:  s,
			Style: widget.RichTextStyle{TextStyle: mono, ColorName: c, Inline: inline},
		})
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "{") { // ergste geval: één segment per teken
			r := []rune(line)
			for i, c := range r {
				name := colKey
				if i%2 == 0 {
					name = colVal
				}
				add(string(c), name, i < len(r)-1)
			}
			continue
		}
		if eq := strings.IndexRune(line, '='); eq > 0 {
			add(line[:eq], colKey, true)
			add(line[eq:], colVal, false)
			continue
		}
		add(line, "", false)
	}
	return out
}

func main() {
	a := app.New()
	w := a.NewWindow("edittest")

	var lines []string
	for i := 0; i < 12; i++ {
		lines = append(lines,
			"hopos.node=hop-1",
			"",
			"# --- Identity ------------------------------------------",
			"hopos.init[]={\"name\":\"welcome\",\"count\":1}",
		)
	}
	text := strings.Join(lines, "\n")

	rich := widget.NewRichText(segments(text)...)
	rich.Wrapping = fyne.TextWrapOff
	rich.Scroll = fyne.ScrollNone

	entry := widget.NewMultiLineEntry()
	entry.TextStyle = fyne.TextStyle{Monospace: true}
	entry.Wrapping = fyne.TextWrapOff
	entry.Scroll = fyne.ScrollNone
	entry.SetText(text)

	base := theme.DefaultTheme()
	richTh := overlayTheme{Theme: base, fg: color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}}
	entryTh := overlayTheme{Theme: base, fg: color.NRGBA{R: 0xFF, G: 0x40, B: 0x40, A: 0xA0}}

	w.SetContent(container.NewScroll(container.NewStack(
		container.NewThemeOverride(rich, richTh),
		container.NewThemeOverride(entry, entryTh),
	)))
	w.Resize(fyne.NewSize(820, 700))
	w.ShowAndRun()
}

// editor.go — het config-tekstveld dat kleur houdt terwijl je typt.
//
// ER IS GEEN LEESSTAND MEER (Derek, 07-08: "je wil het altijd editten, er is
// geen VIEW mode"). Eerst wisselde een knop tussen een gekleurd TextGrid en een
// kaal tekstveld; dat is precies verkeerd om, want je opent CONFIGURE nooit om
// te kíjken.
//
// HOE HET WERKT — twee lagen die exact op elkaar liggen:
//
//	onder   widget.RichText met de gekleurde segmenten (highlight.go)
//	boven   widget.Entry met ONZICHTBARE letters (voorgrondkleur transparant)
//
// De Entry doet dus al het echte werk — cursor, selectie, slepen, plakken,
// ongedaan maken — en de kleuren komen van de laag eronder. Zelf een editor
// bouwen op een TextGrid zou al die dingen opnieuw (en slechter) betekenen.
//
// WAAROM RichText EN GEEN TextGrid ALS ONDERLAAG: een Entry meet zijn regels
// via RichText, en een TextGrid rekent met een vaste celhoogte. Gemeten 07-08
// met tools/edittest: over 48 regels liepen die een volle regel uit elkaar. Met
// RichText onder RichText is de regelhoogte per constructie gelijk — in
// diezelfde meting pixel-op-pixel, ook met één segment per teken.
//
// Eén ding moet daarvoor uit het thema (editorTheme in theme.go): regelafstand
// nul, want die kent alleen de RichText-kant. De invoerrand blijft juist staan —
// daar is de breedte van de cursor van gemaakt.
//
// OPNIEUW KLEUREN GEBEURT BIJ ELKE AANSLAG, zonder uitstel. Dat mag: een config
// van 7 KB opnieuw in segmenten hakken kost ~0,2 ms en de RichText verversen
// ~0,6 ms (gemeten 07-08). Een uitgestelde variant stond hier eerst, maar die
// was juist zichtbaar traag — de letters van de Entry zijn onzichtbaar, dus je
// ziet wat je typt pas als de laag eronder bijwerkt.
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type cfgEditor struct {
	entry *widget.Entry
	rich  *widget.RichText
	view  *container.Scroll
	obj   fyne.CanvasObject
}

func newCfgEditor() *cfgEditor {
	e := &cfgEditor{}

	e.rich = widget.NewRichText()
	e.rich.Wrapping = fyne.TextWrapOff
	e.rich.Scroll = fyne.ScrollNone

	e.entry = widget.NewMultiLineEntry()
	e.entry.TextStyle = monoStyle
	e.entry.Wrapping = fyne.TextWrapOff
	// Geen eigen scroller: dan is de Entry precies zo groot als zijn tekst en
	// schuiven beide lagen samen in ÉÉN scroller eromheen. Met twee scrollers
	// zouden de kleuren achterblijven zodra je scrolt.
	e.entry.Scroll = fyne.ScrollNone
	e.entry.SetPlaceHolder("load a config from a card or an image file")
	e.entry.OnChanged = func(string) { e.repaint() }
	e.entry.OnCursorChanged = func() { e.followCursor() }

	th := editorTheme{}
	layers := container.NewStack(
		container.NewThemeOverride(e.rich, th),
		container.NewThemeOverride(e.entry, th),
	)
	e.view = container.NewScroll(layers)

	// Het paneel eronder staat BUITEN de scroller: anders zou het meeschuiven en
	// bij een korte config maar een stukje van het vlak vullen.
	bg := canvas.NewRectangle(hopPanel)
	e.obj = container.NewStack(bg, e.view)
	return e
}

func (e *cfgEditor) Text() string { return e.entry.Text }

func (e *cfgEditor) SetText(s string) {
	e.entry.SetText(s) // dit vuurt OnChanged, dus de kleuren volgen vanzelf
	e.view.ScrollToTop()
}

func (e *cfgEditor) repaint() {
	e.rich.Segments = segmentsFor(e.entry.Text)
	e.rich.Refresh()
}

// followCursor schuift de cursor in beeld. Dat doet de Entry normaal zelf met
// zijn eigen scroller, en die hebben we juist weggehaald om de twee lagen samen
// te laten schuiven — dus doen we het hier.
//
// De regelhoogte is hier een schatting (de hoogte van een 'M'); dat is precies
// genoeg om te bepalen of de cursor boven of onder het venster ligt, en een
// regel te veel of te weinig marge merkt niemand.
func (e *cfgEditor) followCursor() {
	// De Entry verzet zijn cursor al tijdens het bouwen (een ThemeOverride laat
	// hem verversen), dus dit kan vóór de scroller bestaan.
	if e.view == nil {
		return
	}
	line := fyne.MeasureText("M", fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameText), monoStyle).Height
	if line <= 0 {
		return
	}
	y := float32(e.entry.CursorRow) * line
	top, height := e.view.Offset.Y, e.view.Size().Height
	switch {
	case y < top:
		e.view.Offset.Y = y
	case y+2*line > top+height:
		e.view.Offset.Y = y + 2*line - height
	default:
		return
	}
	if e.view.Offset.Y < 0 {
		e.view.Offset.Y = 0
	}
	e.view.Refresh()
}

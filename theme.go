// theme.go — de HopOS-huisstijl als Fyne-thema, 1:1 het palet van gethop.org
// (style.css: --bg/--panel/--line/--text/--muted/--phosphor/--leaf/--wave/
// --copper/--fault): donker terminal-blauw met fosforgroen als accent. De
// tool ziet er zo uit als de site en als een node-console — één merk.
package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

var (
	hopBG       = color.NRGBA{R: 0x0B, G: 0x0F, B: 0x14, A: 0xFF} // --bg
	hopPanel    = color.NRGBA{R: 0x10, G: 0x16, B: 0x1D, A: 0xFF} // --panel
	hopLine     = color.NRGBA{R: 0x1D, G: 0x27, B: 0x33, A: 0xFF} // --line
	hopText     = color.NRGBA{R: 0xCD, G: 0xD6, B: 0xE0, A: 0xFF} // --text
	hopMuted    = color.NRGBA{R: 0x7F, G: 0x8B, B: 0x9B, A: 0xFF} // --muted
	hopPhosphor = color.NRGBA{R: 0x46, G: 0xD9, B: 0x79, A: 0xFF} // --phosphor
	hopLeaf     = color.NRGBA{R: 0x56, G: 0xC8, B: 0x8B, A: 0xFF} // --leaf
	hopWave     = color.NRGBA{R: 0x6B, G: 0xA6, B: 0xFF, A: 0xFF} // --wave
	hopCopper   = color.NRGBA{R: 0xE0, G: 0x9A, B: 0x63, A: 0xFF} // --copper
	hopFault    = color.NRGBA{R: 0xE0, G: 0x65, B: 0x4F, A: 0xFF} // --fault
	hopDeep     = color.NRGBA{R: 0x07, G: 0x10, B: 0x19, A: 0xFF} // tekst óp fosfor

	// fadedMuted is muted maar halfdoorzichtig: voor tekst die er mag zijn zonder
	// mee te praten — de stapnummers op het beginscherm, en de regel die de
	// volgorde uitlegt.
	fadedMuted = color.NRGBA{R: 0x7F, G: 0x8B, B: 0x9B, A: 0x66}
	// watermarkMuted is nóg lichter: voor het grote stapnummer in de hoek van een
	// kaart. Groot mag alleen als het licht genoeg is om decor te blijven.
	watermarkMuted = color.NRGBA{R: 0x7F, G: 0x8B, B: 0x9B, A: 0x3D}
	// transparent maakt een onzichtbare rechthoek die alleen ruimte inneemt.
	transparent = color.NRGBA{}
)

// De tag-kleuren volgen het palet: leaf = er komt beeld uit, wave = op
// afstand (API/console), copper = let op.
var (
	colDesktop  = hopLeaf
	colHeadless = hopWave
	colWarn     = hopCopper
)

// De kanaal-kleuren: fosfor = de gewone weg (stable), koper = let op, dit is
// een voorproefje (beta). Dezelfde koperkleur als een waarschuwing, want dat
// is wat een prerelease is — geen fout, wel iets om te weten.
var (
	colStable = hopPhosphor
	colBeta   = hopCopper
)

// hopTheme dwingt de huisstijl af, licht óf donker systeemthema — de site is
// ook altijd donker. Alles wat hier niet genoemd wordt valt terug op het
// donkere standaardthema.
type hopTheme struct{}

func (hopTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return hopBG
	case theme.ColorNameForeground:
		return hopText
	case theme.ColorNamePrimary, theme.ColorNameHyperlink, theme.ColorNameFocus:
		return hopPhosphor
	case theme.ColorNameForegroundOnPrimary:
		return hopDeep
	case theme.ColorNameButton, theme.ColorNameInputBackground, theme.ColorNameOverlayBackground, theme.ColorNameMenuBackground:
		return hopPanel
	case theme.ColorNameSeparator, theme.ColorNameInputBorder, theme.ColorNameShadow:
		return hopLine
	case theme.ColorNamePlaceHolder, theme.ColorNameScrollBar:
		return hopMuted
	case theme.ColorNameDisabled:
		return color.NRGBA{R: 0x4A, G: 0x55, B: 0x63, A: 0xFF} // tussen line en muted
	case theme.ColorNameError:
		return hopFault
	case theme.ColorNameSuccess:
		return hopLeaf
	case theme.ColorNameWarning:
		return hopCopper
	case theme.ColorNameHover:
		// DOORSCHIJNEND, geen dekkende kleur: hover wordt over de knopkleur
		// heen geverfd, en een dekkende laag maakte de groene (actieve)
		// knoppen onleesbaar (Derek 06-08). Zo blijft groen gewoon groen,
		// een tikje lichter.
		return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x14}
	case theme.ColorNameSelection:
		return color.NRGBA{R: 0x0D, G: 0x1A, B: 0x2B, A: 0xFF}
	}
	return theme.DefaultTheme().Color(n, theme.VariantDark)
}

// editorTheme is wat de twee lagen van de config-editor te zien krijgen
// (editor.go). Het beantwoordt de kleurnamen die de highlighter verzint, maakt
// de letters van het tekstveld onzichtbaar — de kleuren komen van de laag
// eronder — en haalt de twee maten weg die de lagen uit de pas zouden laten
// lopen.
type editorTheme struct{ hopTheme }

func (t editorTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameForeground:
		// De Entry tekent zijn eigen letters niet: die staan al gekleurd in de
		// RichText eronder. Zou hij dat wel doen, dan zag je alles dubbel.
		return color.Transparent
	case theme.ColorNameInputBackground, theme.ColorNameInputBorder:
		return color.Transparent // anders dekt het tekstveld de kleuren af
	case theme.ColorNameSelection:
		// Doorschijnend: een dekkend selectieblok zou de gekleurde tekst
		// verbergen op precies het moment dat je hem aanwijst.
		return color.NRGBA{R: 0x46, G: 0xD9, B: 0x79, A: 0x4D}
	case cSection:
		return hopPhosphor
	case cComment, cPunct:
		return hopMuted
	case cKey:
		return hopWave
	case cValue:
		return hopText
	case cSecret, cNumber:
		return hopCopper
	case cString:
		return hopLeaf
	}
	return t.hopTheme.Color(n, v)
}

func (t editorTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameLineSpacing:
		// De gekleurde laag en het tekstveld tellen allebei regelhoogtes op;
		// regelafstand zou daar bij één van de twee bij komen en ze uit elkaar
		// laten lopen. Nul is hier dus geen smaak maar een eis.
		//
		// SizeNameInputBorder blijft WEL staan, hoe verleidelijk ook: daar is de
		// breedte van de cursor van gemaakt (entry.go: Resize(inputBorder,
		// lineHeight)). Op nul is je cursor onzichtbaar — precies wat er 07-08
		// gebeurde. Voor de uitlijning maakt hij niets uit: de Entry schuift zijn
		// tekstlaag er met inputBorder in, en die laag trekt hem er weer af.
		return 0
	}
	return t.hopTheme.Size(n)
}

func (hopTheme) Font(s fyne.TextStyle) fyne.Resource     { return theme.DefaultTheme().Font(s) }
func (hopTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }
func (hopTheme) Size(n fyne.ThemeSizeName) float32       { return theme.DefaultTheme().Size(n) }

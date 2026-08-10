// home.go — het beginscherm en de navigatie: drie knoppen, en een weg terug.
//
// WAAROM GEEN TABS MEER (Derek, 07-08: "ik vind het nu zo moeilijk en allemaal
// knoppen in your face"):
//
// De drie dingen die deze tool doet gebeuren nooit tegelijk. Je brandt een
// kaart, óf je past een config aan, óf je zoekt een node op het netwerk. Drie
// tabs met alles altijd zichtbaar dwong de gebruiker bij elke start opnieuw te
// kiezen wat hij niet ging doen — en de smaak-, board-, doel- en
// bewaar-de-config-knoppen stonden er allemaal meteen bij.
//
// Dus: eerst kiezen WAT je doet (met één regel uitleg per keuze, in de app en
// niet alleen in de README), en dan pas het gereedschap voor dat werk. De weg
// terug is een knop links boven, of Escape.
//
// De cijfers 1-2-3 staan er als WATERMERK, en dat is precies de juiste zwaarte:
// burn → configure → find is de levensloop van een node, maar stap 2 en 3 zijn
// geen verplicht vervolg. Een dikke badge zou "wizard" lezen; een lichte hoek
// leest "volgorde".
package main

import (
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// De namen van de schermen. Eén string per scherm, ook als sleutel in u.screens.
const (
	scrHome      = "home"
	scrBurn      = "burn"
	scrConfigure = "configure"
	scrFind      = "find"
)

// Wat elke sectie doet, in één regel. Deze tekst staat op de knop op het
// beginscherm ÉN in de kop van de sectie zelf: zodra je binnen bent is de
// knoptekst weg, en dan hoort er nog te staan waar je bent.
const (
	descBurn      = "Write a HopOS release — stable or beta — to a card or USB stick, and read it back to prove it landed."
	descConfigure = "Change the config of a card you already burned: its name, its keys, the jobs it runs."
	descFind      = "Find the HopOS nodes that are running on your network, and open their screen."
)

// bigCard is een knop met een kop, een regel uitleg en een licht stapnummer.
//
// Gebouwd als Stack(knop, inhoud): een widget.Button kan geen opgemaakte inhoud
// dragen, maar canvas-tekst is niet aanklikbaar — dus vangt de knop eronder de
// tik, en krijgen we de hover-highlight van het thema er gratis bij.
func bigCard(step int, title, desc string, tap func()) fyne.CanvasObject {
	btn := widget.NewButton("", tap)

	// Kop en uitleg staan VERTICAAL gecentreerd maar links uitgelijnd (Derek
	// 07-08): drie kaarten met hun tekst op dezelfde linkerlijn lees je als één
	// lijst; horizontaal centreren maakt van elke kaart een eigen postertje en je
	// oog moet elke regel opnieuw zoeken.
	ttl := canvas.NewText(title, hopPhosphor)
	ttl.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	ttl.TextSize = 19

	sub := canvas.NewText(desc, hopMuted)
	sub.TextSize = 12

	// Het stapnummer als WATERMERK: dik, groot, rechts in de kaart, en het breekt
	// aan de ONDERKANT uit (Derek 07-08). Dat afsnijden is het effect: een cijfer
	// dat netjes binnen de kaart past leest als een label dat iets beweert, een
	// afgesneden cijfer leest als decor. Alleen naar onderen, niet naar rechts —
	// een cijfer dat ook zijwaarts wegvalt is geen ontwerp maar een ongeluk.
	// Groot mag daarom, zolang het licht genoeg blijft om onder de tekst door te
	// lopen zonder ermee te vechten. Vet schreefloos, geen monospace: dat is de
	// dikste letter die we hebben.
	num := canvas.NewText(strconv.Itoa(step), watermarkMuted)
	num.TextStyle = fyne.TextStyle{Bold: true}
	num.TextSize = 96
	// Rechts uitgelijnd, en cornerHang geeft het de volle breedte: dan hangt het
	// cijfer STRAK tegen de rechterrand in plaats van op een marge die van de
	// gemeten tekstbreedte afhangt (Derek 07-08).
	num.Alignment = fyne.TextAlignTrailing

	// Een doorzichtige rechthoek zet de hoogte: de knop is leeg en zou anders
	// om zijn eigen (lege) tekst heen krimpen.
	spacer := canvas.NewRectangle(transparent)
	spacer.SetMinSize(fyne.NewSize(0, 112))

	// Het afsnijden doet een Scroll met ScrollNone: dat is in Fyne het enige ding
	// dat zijn inhoud écht op zijn eigen maat afkapt. Zonder die kap zou het
	// cijfer gewoon over de kaart eronder heen getekend worden — een gewone
	// container knipt niet.
	clip := container.NewScroll(container.New(&cornerHang{obj: num, dy: 30}, num))
	clip.Direction = container.ScrollNone

	body := container.NewVBox(layout.NewSpacer(), ttl, sub, layout.NewSpacer())
	return container.NewStack(btn, spacer, clip, container.NewPadded(body))
}

// cornerHang legt één object tegen de onderrand aan over de VOLLE breedte, en
// schuift het dy naar beneden — dus onder de rand uit, waar de Scroll eromheen
// het afsnijdt. De volle breedte is de truc: het object lijnt zichzelf rechts
// uit (num.Alignment), zodat het strak tegen de rand staat en niet op een marge
// die van de gemeten tekstbreedte afhangt.
//
// MinSize is nul: het cijfer mag de kaart niet groter maken — het hangt eraan,
// het vult niets.
type cornerHang struct {
	obj fyne.CanvasObject
	dy  float32
}

func (c *cornerHang) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.Size{} }

func (c *cornerHang) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	h := c.obj.MinSize().Height
	c.obj.Resize(fyne.NewSize(size.Width, h))
	c.obj.Move(fyne.NewPos(0, size.Height-h+c.dy))
}

// homeScreen: het logo, en de drie dingen die deze tool doet.
func (u *ui) homeScreen() fyne.CanvasObject {
	ver := canvas.NewText("imager "+version, hopMuted)
	ver.TextStyle = fyne.TextStyle{Monospace: true}
	ver.TextSize = 12

	// Geen pijltjes: Fyne's standaardfont heeft geen "→" en tekende er een
	// vierkantje voor (gemeten 07-08). Punten doen hetzelfde werk en bestaan.
	order := canvas.NewText("1 · 2 · 3 is the usual order for a new node, but each one stands on its own.", fadedMuted)
	order.TextSize = 11

	// De spacers duwen de drie kaarten naar het midden van de ruimte die
	// overblijft: zonder hen plakken ze onder het logo en staat de onderste helft
	// van het venster leeg.
	return container.NewVBox(
		logoArt(),
		container.NewCenter(ver),
		widget.NewSeparator(),
		layout.NewSpacer(),
		bigCard(1, "BURN", descBurn, func() { u.show(scrBurn) }),
		bigCard(2, "CONFIGURE", descConfigure, func() { u.show(scrConfigure) }),
		bigCard(3, "FIND", descFind, func() { u.show(scrFind) }),
		container.NewCenter(order),
		layout.NewSpacer(),
	)
}

// sectionScreen zet een sectie in zijn eigen scherm: terug-knop, waar je bent,
// wat het doet — en dan het gereedschap. extra hangt rechts in de kop (BURN zet
// daar zijn release-verversing; die hoort bij BURN en niet bij de hele app).
func (u *ui) sectionScreen(step int, title, desc string, extra, body fyne.CanvasObject) fyne.CanvasObject {
	back := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() { u.show(scrHome) })
	back.Importance = widget.LowImportance

	// Geen merkje in de kop. Ik had hier de bunny-oren staan (zoals het app-icoon
	// op kleine maten doet), maar in een kopregel náást een titel zijn ze ruis:
	// het logo hoort op het beginscherm, niet in elke stap (Derek 07-08).
	ttl := canvas.NewText(strconv.Itoa(step)+".  "+title, hopText)
	ttl.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	ttl.TextSize = 16

	sub := canvas.NewText(desc, hopMuted)
	sub.TextSize = 11

	head := container.NewBorder(nil, nil,
		container.NewHBox(back), extra,
		container.NewVBox(ttl, sub),
	)
	return container.NewBorder(
		container.NewVBox(head, widget.NewSeparator()),
		nil, nil, nil,
		body,
	)
}

// show wisselt van scherm. Moet van de UI-thread komen (of via fyne.Do).
func (u *ui) show(name string) {
	for k, s := range u.screens {
		if k == name {
			s.Show()
		} else {
			s.Hide()
		}
	}
	u.current = name

	// Naar CONFIGURE gaan met een doel geselecteerd = de config laden. Zo blijft
	// "kiezen is zien" waar, zonder dat er een rechten-dialoog uit de lucht valt
	// terwijl je op een ander scherm staat.
	if name == scrConfigure && u.pendingCfg != "" && u.pendingCfg != u.cfgTarget {
		target := u.pendingCfg
		u.run(func() { u.loadConfig(target) })
	}
}

func (u *ui) onConfigureTab() bool { return u.current == scrConfigure }

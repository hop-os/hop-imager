// hop-imager — één tool voor de hele levensloop van een HopOS-node:
//
//	BURN       een gekozen release naar een kaart schrijven en verifiëren
//	CONFIGURE  de node-config aanpassen — altijd achteraf, op de kaart zelf
//	FIND       draaiende nodes op het netwerk vinden ("waar staat dat ding?")
//
// Die drie zijn Dereks indeling (06-08) en ze staan niet voor niets in die
// volgorde: configureren hoort ná het flashen, niet tijdens. Ze zitten elk in
// hun eigen scherm, met een beginscherm dat de keuze maakt — zie home.go. De
// config-bewerking gaat altijd RAW via het #HOPCFG1-venster
// (internal/cfgwin, formaatcontract met hop-os image/hopcfg) — dat werkt op
// élk board, ook als het OS de partitie niet mount (Radxa) of als de config in
// de kernel zit (LicheeRV). De rechten daarvoor komen van authopen
// (internal/priv): op macOS is root NIET genoeg voor rauwe schijftoegang —
// zelfs Apple's dd krijgt als uid 0 "Operation not permitted", want TCC kijkt
// naar het proces. Dus geen sudo en geen verhoogde worker: één
// autorisatie-dialoog per bewerking, en al het werk in dit proces (dus ook
// live voortgang).
//
// FIND leunt op de console-poort die élke meegeleverde config aanzet
// (hopos.console=5555): één TCP-connect levert de identificatie én de naam,
// architectuur en staat van de node — zie internal/find.
//
// Eigen repo en eigen module, náást HopOS zelf: Fyne (cgo, OpenGL) hoort niet
// in de metal-module, en deze tool heeft zijn eigen ritme. macOS eerst;
// Windows/Linux = internal/disk en internal/priv invullen, de rest is
// platform-neutraal.
//
//	go build . && ./hop-imager
//	./hop-imager -scan            (dezelfde zoeker zonder GUI)
//	./hop-imager -releases        (welke versies ziet de tool, en welke kiest hij?)
//	./hop-imager -disks           (wat de tool als doel ziet)
//	./hop-imager -probe /dev/rX   (kan ik dit device lezen? zit er een venster in?)
package main

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/xinix00/hop-imager/internal/cfgwin"
	"github.com/xinix00/hop-imager/internal/disk"
	"github.com/xinix00/hop-imager/internal/find"
	"github.com/xinix00/hop-imager/internal/priv"
	"github.com/xinix00/hop-imager/internal/release"
)

// iconPNG is de bunny uit tools/mkicon — hetzelfde beeld als het app-icoon, ook
// als de tool als losse binary draait (Linux, Windows) en er geen bundle is.
//
//go:embed icon.png
var iconPNG []byte

// version wordt bij het bouwen ingevuld (-ldflags "-X main.version=…"); zie
// make-app.sh en .github/workflows/release.yml.
var version = "dev"

// bunny: Dereks origineel (2026-07-11) — hetzelfde logo dat elke node bij het
// booten toont (metal/cmd/hopos/main.go), dus de tool en het glas matchen.
const bunny = `   (\(\
   ( -.-)     HopOS
   o_(")(")   --------------
              the Go-only OS`

// boardRow is één board uit de release: dezelfde kaart in (maximaal) drie
// smaken. De knoppen tonen boards; de smaak-keuze erboven kiest het asset.
type boardRow struct {
	id       string
	display  string
	byFlavor map[string]release.Asset // "desktop" / "headless" / "media"
}

// boardNames maakt van de asset-id's mensennamen; onbekende id's tonen de id
// zelf, zodat een nieuw board in de release nooit onzichtbaar is.
var boardNames = map[string]string{
	"rpi5":        "Raspberry Pi 5",
	"rpi4":        "Raspberry Pi 4",
	"radxa-zero3": "Radxa Zero 3E",
	"o6n":         "Radxa Orion O6N",
	"altra":       "Ampere Altra",
	"licheerv":    "LicheeRV Nano",
	"m4":          "Mac mini M4",
	"uefi":        "UEFI · USB stick",
}

var boardOrder = []string{"rpi5", "rpi4", "radxa-zero3", "o6n", "altra", "licheerv", "m4", "uefi"}

type ui struct {
	win fyne.Window

	// BURN
	releases   []release.Release
	relRow     *fyne.Container // de regel bovenin, herschikt als de pil verschijnt
	relChan    *tagPill        // stable of beta, gekleurd
	relSel     *widget.Select  // het versienummer erachter — en de keuze
	relTag     string
	flavorBtns map[string]*widget.Button // "desktop" / "headless" / "media"
	flavorVal  string
	boardBox   *fyne.Container
	boards     []boardRow
	selBoard   string
	imageLbl   *widget.Label
	imageTags  *fyne.Container
	imagePath  string
	keepCfg    *widget.Check

	// CONFIGURE
	cfgEdit   *cfgEditor // gekleurd tekstveld (editor.go) — altijd bewerkbaar
	cfgLbl    *widget.Label
	cfgTags   *fyne.Container
	cfgTarget string // /dev/rdiskN of een image-pad
	cfgSave   *widget.Button

	// FIND
	nodes    []find.Node
	nodeList *widget.List
	scanBtn  *widget.Button
	scanning bool
	cancel   func()

	// Eén voortgangsbalk PER SCHERM (progress.go): een gedeelde balk liet een
	// flash op BURN eruitzien als een geschreven config en een klare scan.
	burnProg *prog
	findProg *prog
	cfgBusy  *widget.ProgressBarInfinite

	// gedeeld — navigatie: één scherm zichtbaar, de rest verborgen (home.go).
	screens    map[string]fyne.CanvasObject
	current    string
	pendingCfg string // doel waarvan de config klaarstaat om te laden
	cardSels   []*widget.Select
	devices    map[string]disk.Device
	status     *widget.Label // de volledige console (groeit in de BURN-tab)
	lastLbl    *widget.Label // één regel, altijd zichtbaar onder elke tab
	logScroll  *container.Scroll
	logLines   []string
}

// --- tags: outline-pills ("mooi cirkeltje er omheen", Derek 06-08) -----------

type tagPill struct {
	wrap *fyne.Container
	txt  *canvas.Text
	box  *canvas.Rectangle
}

func newTagPill() *tagPill {
	t := canvas.NewText("", hopMuted)
	t.TextSize = 11
	t.TextStyle = fyne.TextStyle{Bold: true}
	r := canvas.NewRectangle(color.Transparent)
	r.StrokeWidth = 1.5
	r.CornerRadius = 10
	p := &tagPill{txt: t, box: r}
	p.wrap = container.NewCenter(container.NewStack(r, container.NewCenter(t)))
	return p
}

func (p *tagPill) set(text string, c color.Color) {
	p.txt.Text, p.txt.Color, p.box.StrokeColor = text, c, c
	w := fyne.MeasureText(text, p.txt.TextSize, p.txt.TextStyle).Width
	p.box.SetMinSize(fyne.NewSize(w+20, 21))
	p.txt.Refresh()
	p.box.Refresh()
	p.wrap.Refresh()
}

func pill(text string, c color.Color) fyne.CanvasObject {
	p := newTagPill()
	p.set(text, c)
	return p.wrap
}

// section is een veldlabel in site-stijl: muted caps, monospace — zoals de
// kopjes op gethop.org.
func section(text string) fyne.CanvasObject {
	t := canvas.NewText(strings.ToUpper(text)+":", hopMuted)
	t.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	t.TextSize = 12
	return container.NewPadded(t)
}

// logoArt tekent de bunny in fosforgroen. Per regel een canvas.Text en dan het
// blók centreren: een gecentreerd multiline-label centreert elke regel
// afzonderlijk en dat sloopt ASCII-art (gemeten op Dereks scherm, 06-08).
func logoArt() fyne.CanvasObject {
	box := container.NewVBox()
	for _, l := range strings.Split(bunny, "\n") {
		t := canvas.NewText(l, hopPhosphor)
		t.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
		t.TextSize = 15
		box.Add(t)
	}
	return container.NewCenter(box)
}

func main() {
	// -disks: wat de tool als doelkaart ziet. Diagnose ("waarom staat mijn
	// kaart er niet bij?") én het meetinstrument voor internal/disk.
	if slices.Contains(os.Args[1:], "-disks") {
		devs, err := disk.List()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, d := range devs {
			fmt.Printf("%-40s  %s  %d bytes\n", d.Desc, d.RawPath, d.Size)
		}
		fmt.Fprintf(os.Stderr, "%d card(s)/stick(s)\n", len(devs))
		return
	}

	// -releases: welke versies de tool ziet, in welk kanaal ze zitten en welke
	// hij uit zichzelf laat zien (de ster). Diagnose ("waarom staat die beta er
	// niet bij?") én het meetinstrument voor internal/release.
	if slices.Contains(os.Args[1:], "-releases") {
		rels, err := release.List(release.Repo)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		rels = release.WithImages(rels)
		def, _ := release.Pick(rels)
		for _, r := range rels {
			star := " "
			if r.Tag == def.Tag {
				star = "*"
			}
			fmt.Printf("%s %-8s %-18s %d image(s)\n", star, r.Channel(), r.Tag, len(release.Images(r.Assets)))
		}
		fmt.Fprintf(os.Stderr, "%d version(s) with card images\n", len(rels))
		return
	}

	// -probe <device>: kan de tool dit device überhaupt lezen, en zit er een
	// config-venster in? Het meetinstrument voor de rechten-weg (authopen).
	if i := slices.Index(os.Args[1:], "-probe"); i >= 0 && len(os.Args) > i+2 {
		dev := os.Args[i+2]
		f, err := priv.OpenDevice(dev, false)
		if err != nil {
			fmt.Fprintln(os.Stderr, "open:", err)
			os.Exit(1)
		}
		defer f.Close()
		w, err := cfgwin.Find(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "window:", err)
			os.Exit(1)
		}
		c, _ := cfgwin.Content(f, w)
		fmt.Printf("window at %#x, %d KB, config %d bytes\n", w.Off, w.Size>>10, w.ContentLen)
		fmt.Println(strings.SplitN(string(c), "\n", 2)[0])
		return
	}

	// -scan: dezelfde zoeker zonder GUI. Handig in een script ("waar staan
	// mijn nodes?") en het is het meetinstrument voor de FIND-tab zelf.
	if slices.Contains(os.Args[1:], "-scan") {
		hosts := find.Subnets()
		fmt.Fprintf(os.Stderr, "scanning %d addresses on tcp/%d…\n", len(hosts), find.DefaultPort)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		st := find.Scan(ctx, hosts, find.DefaultPort, func(nd find.Node) {
			fmt.Printf("%-15s  %-16s  %-8s  %-22s  api-open=%v\n", nd.IP, nd.Name, nd.Arch, nd.State, nd.InsecND)
		}, nil)
		fmt.Fprintf(os.Stderr, "%d node(s) found\n", st.Found)
		return
	}

	a := app.NewWithID("org.gethop.imager")
	a.Settings().SetTheme(hopTheme{})
	a.SetIcon(fyne.NewStaticResource("icon.png", iconPNG))
	u := &ui{win: a.NewWindow("HopOS Imager"), devices: map[string]disk.Device{}}

	u.burnProg, u.findProg = newProg(), newProg()
	u.cfgBusy = widget.NewProgressBarInfinite()
	u.cfgBusy.Hide()
	u.status = widget.NewLabel("")
	u.status.Wrapping = fyne.TextWrapWord
	u.status.TextStyle = fyne.TextStyle{Monospace: true}
	u.logScroll = container.NewVScroll(u.status)
	// Geen vaste hoogte: de console groeit mee in de BURN-tab (daar is hij het
	// hoofdvenster van wat er gebeurt). Zie burnTab.
	u.lastLbl = widget.NewLabel("")
	u.lastLbl.Truncation = fyne.TextTruncateEllipsis

	u.screens = map[string]fyne.CanvasObject{
		scrHome:      u.homeScreen(),
		scrBurn:      u.sectionScreen(1, "BURN", descBurn, nil, u.burnTab()),
		scrConfigure: u.sectionScreen(2, "CONFIGURE", descConfigure, nil, u.configureTab()),
		scrFind:      u.sectionScreen(3, "FIND", descFind, nil, u.findTab()),
	}
	stack := container.NewStack()
	for _, name := range []string{scrHome, scrBurn, scrConfigure, scrFind} {
		stack.Add(u.screens[name])
	}
	u.show(scrHome)

	// De laatste regel status blijft onder élk scherm staan: waar je ook bent,
	// wat de tool net deed hoort zichtbaar te zijn.
	u.win.SetContent(container.NewBorder(
		nil,
		container.NewVBox(widget.NewSeparator(), u.lastLbl),
		nil, nil,
		container.NewPadded(stack),
	))
	// Escape = terug naar het beginscherm. Kost niets: een scherm wordt verborgen,
	// niet weggegooid — je keuzes en de console staan er nog als je terugkomt.
	u.win.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if ev.Name == fyne.KeyEscape && u.current != scrHome {
			u.show(scrHome)
		}
	})
	u.win.Resize(fyne.NewSize(820, 780))

	u.log("Welcome. Loading the release list…")
	u.fetchReleases()
	u.refreshDevices()
	u.watchDevices()
	u.demoMode()
	u.win.ShowAndRun()
}

// demoMode zet een beginstand voor demo's en de README-screenshots:
// HOPIMAGER_TAB kiest het scherm, HOPIMAGER_IMAGE zet een image klaar (en dus
// ook de config die CONFIGURE toont), en op FIND begint de scan meteen. Puur een
// startstand — er zit geen gedrag achter dat de tool anders niet heeft.
func (u *ui) demoMode() {
	if p := os.Getenv("HOPIMAGER_IMAGE"); p != "" {
		u.pendingCfg = p
		u.run(func() { u.setImage(p) })
	}
	tab := strings.ToLower(os.Getenv("HOPIMAGER_TAB"))
	if _, ok := u.screens[tab]; !ok {
		return
	}
	u.show(tab)
	if tab == scrFind {
		u.run(func() { time.Sleep(300 * time.Millisecond); u.scanNetwork() })
	}
	if tab == scrConfigure && u.pendingCfg != "" {
		p := u.pendingCfg
		u.run(func() { time.Sleep(300 * time.Millisecond); u.loadConfig(p) })
	}
}

// --- BURN --------------------------------------------------------------------

func (u *ui) burnTab() fyne.CanvasObject {
	// BOVENIN: welke release je brandt. HopOS heeft sinds v1.12 ook beta's, en
	// dan is "de laatste release" geen antwoord meer maar een vraag. Dus staat
	// het er: het kanaal als gekleurde pil (fosfor = stable, koper = beta) en
	// het versienummer erachter, dat tegelijk de keuzelijst ís. Boven Type en
	// Device, want die twee kiezen ergens ín een versie — versie + type + board
	// wijzen samen één image aan.
	//
	// De verversknop hoort hier en niet meer in de sectiekop: hij haalt de
	// versielijst op, dus hij staat bij de versielijst.
	u.relChan = newTagPill()
	u.relChan.wrap.Hide() // pas als we weten wélke release we tonen
	u.relSel = widget.NewSelect(nil, u.pickRelease)
	u.relSel.PlaceHolder = "loading the release list…"
	u.relRow = container.NewBorder(nil, nil,
		container.NewHBox(section("Release"), u.relChan.wrap),
		widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), u.fetchReleases),
		u.relSel,
	)

	// ONMISKENBARE knoppen (Derek 06-08: "die keuze moet echt
	// duidelijker!"): de gekozen smaak staat gevuld fosforgroen, de andere
	// panel-grijs — een segmented control, geen radiootje. Media is de derde
	// sinds HopOS v2.2.8: de GUI plus video-codec en optische drives, en net
	// als headless een link-grens, geen instelling.
	u.flavorBtns = map[string]*widget.Button{
		"desktop":  widget.NewButtonWithIcon("Desktop (GUI)", theme.ComputerIcon(), func() { u.setFlavor("desktop") }),
		"headless": widget.NewButtonWithIcon("Headless", theme.VisibilityOffIcon(), func() { u.setFlavor("headless") }),
		"media":    widget.NewButtonWithIcon("Media", theme.MediaVideoIcon(), func() { u.setFlavor("media") }),
	}
	u.flavorVal = "desktop" // de default van elke release
	flavorRow := container.NewGridWithColumns(len(flavorOrder))
	for _, fl := range flavorOrder {
		flavorRow.Add(u.flavorBtns[fl])
	}

	// Twee kolommen board-knoppen — afbeeldingen van de bordjes kunnen hier
	// later in (ButtonWithIcon of een eigen kaartje per bord).
	u.boardBox = container.NewGridWithColumns(2)
	u.setFlavor("desktop")

	localBtn := widget.NewButtonWithIcon("Choose your own…", theme.FolderOpenIcon(), u.pickLocal)
	u.imageLbl = widget.NewLabel("no image selected")
	u.imageLbl.Truncation = fyne.TextTruncateEllipsis
	u.imageTags = container.NewHBox()
	chosenRow := container.NewBorder(nil, nil, widget.NewIcon(theme.FileIcon()),
		container.NewHBox(u.imageTags, localBtn), u.imageLbl)

	cardRow := u.targetRow(nil, nil)

	// Standaard AAN (Derek 06-08): dit is wat een update hoort te doen — nieuwe
	// code, dezelfde node. Alleen voor een kaart of stick; een image-bestand
	// heeft geen config van een node om te bewaren.
	u.keepCfg = widget.NewCheck("Keep this card's config — its name, keys and jobs survive the update", nil)
	u.keepCfg.SetChecked(true)

	// Eén knop: schrijven ís schrijven-en-verifiëren. Een losse "verify"
	// bestond ook en die is weg (Derek 06-08) — hij kon niets zinnigs meer
	// zeggen zodra macOS de verse kaart had gemount en er zijn eigen metadata
	// in had geschreven, en de verificatie die telt zit al in de write.
	writeBtn := widget.NewButtonWithIcon("Write + verify", theme.MediaPlayIcon(), u.writeCard)
	writeBtn.Importance = widget.HighImportance

	// De console is hier het groeiende deel: in BURN wil je zien wat er
	// gebeurt, in CONFIGURE is dat het tekstveld en in FIND de lijst (Derek
	// 06-08). Border met center = console doet precies dat.
	return container.NewBorder(
		container.NewVBox(
			u.relRow,
			section("Type"), flavorRow,
			section("Device"), u.boardBox,
			chosenRow,
			widget.NewSeparator(),
			cardRow,
			u.keepCfg,
			writeBtn,
			u.burnProg.obj,
			section("Console"),
		),
		nil, nil, nil,
		u.logScroll,
	)
}

// targetRow bouwt een doel-kiezer: kaart óf USB-stick (een Pi boot ook van
// USB, dus het heet TARGET en niet "target card" — Derek 06-08). Elke tab die
// er een nodig heeft krijgt zijn eigen widget (Fyne deelt geen objecten tussen
// containers); refreshDevices vult ze allemaal. extra hangt rechts bij (de
// CONFIGURE-tab zet daar zijn image-knop), onChange vuurt bij elke keuze —
// ook bij de automatische keuze als er precies één doel is.
func (u *ui) targetRow(extra fyne.CanvasObject, onChange func(disk.Device)) fyne.CanvasObject {
	sel := widget.NewSelect(nil, nil)
	sel.PlaceHolder = "insert a card or stick — it shows up by itself…"
	if onChange != nil {
		sel.OnChanged = func(desc string) {
			if d, ok := u.devices[desc]; ok {
				onChange(d)
			}
		}
	}
	u.cardSels = append(u.cardSels, sel)
	rescan := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), u.refreshDevices)
	eject := widget.NewButtonWithIcon("Eject", theme.LogoutIcon(), func() {
		u.withDevice(func(d disk.Device) { u.run(func() { u.eject(d) }) })
	})
	right := container.NewHBox(rescan, eject)
	if extra != nil {
		right = container.NewHBox(extra, rescan, eject)
	}
	return container.NewBorder(nil, nil, section("Target"), right, sel)
}

func (u *ui) writeCard() {
	if u.imagePath == "" {
		u.log("Pick an image first.")
		return
	}
	// Windows: een raw write naar een fysieke schijf vraagt beheerdersrechten,
	// en die kan een proces niet zelf worden — het moet opnieuw starten. Dus
	// vragen we het op het moment dat het nodig is, niet als zeurpiet bij het
	// opstarten. Op macOS en Linux is dit een no-op (authopen resp. sudo).
	if priv.NeedsElevation() {
		dialog.ShowConfirm("Administrator rights needed",
			"Windows only lets an administrator write directly to a card.\n\nRestart HopOS Imager as administrator?",
			func(yes bool) {
				if !yes {
					u.log("Not restarted — writing a card needs administrator rights on Windows.")
					return
				}
				if err := priv.Elevate(); err != nil {
					u.fail(err)
					return
				}
				fyne.CurrentApp().Quit()
			}, u.win)
		return
	}
	u.withDevice(func(d disk.Device) {
		dialog.ShowConfirm("Write image",
			fmt.Sprintf("This ERASES ALL DATA on\n%s\n\nand writes\n%s\n\nContinue?", d.Desc, filepath.Base(u.imagePath)),
			func(yes bool) {
				if !yes {
					return
				}
				u.run(func() {
					img, err := os.Open(u.imagePath)
					if err != nil {
						u.fail(err)
						return
					}
					st, _ := img.Stat()
					img.Close()
					if d.Size > 0 && st.Size() > d.Size {
						u.fail(fmt.Errorf("image (%d MB) is larger than the card (%d MB)", st.Size()>>20, d.Size>>20))
						return
					}
					u.log(fmt.Sprintf("Writing %d MB to %s (unmounting first), then reading it back…", st.Size()>>20, d.RawPath))
					start := time.Now()
					// Elke fase begint als "bezig zonder maat"; kan hij tellen,
					// dan zet de eerste telling de gewone balk vanzelf terug.
					// Zo staat de balk niet op 100% te doen alsof het klaar is
					// terwijl je config nog teruggezet wordt (Derek 07-08).
					res, err := opWrite(u.imagePath, d.RawPath, u.keepCfg.Checked,
						func(p string) {
							u.log("· " + p + "…")
							u.burnProg.work()
						}, u.burnProg.begin())
					u.burnProg.measured()
					if err != nil {
						u.fail(err)
						return
					}
					if res.MismatchAt >= 0 {
						u.log(fmt.Sprintf("WRITE FAILED — the card differs from the image at byte %#x. Try again, or another card.", res.MismatchAt))
						return
					}
					u.log(fmt.Sprintf("WRITE OK and verified in %.0fs — %d MB read back byte for byte.",
						time.Since(start).Seconds(), st.Size()>>20))
					switch {
					case res.Kept && res.KeptNode != "":
						u.log("KEPT the config of node " + res.KeptNode + " — same node, new code. Backup: " + res.Backup)
					case res.Kept:
						u.log("KEPT the config that was on the card. Backup: " + res.Backup)
					case u.keepCfg.Checked && res.Backup == "":
						u.log("No config found on the card to keep — it now carries this image's default.")
					}
					if len(res.NewKeys) > 0 {
						// Geen merge, wel eerlijk: dit is wat de nieuwe default
						// zet en jouw config niet heeft.
						u.log("Note: the new default config also sets " + strings.Join(res.NewKeys, ", ") +
							" — your kept config does not. Add it in CONFIGURE if you want it.")
					}
					if res.Warning != "" {
						u.log("WARNING: " + res.Warning)
					}
				})
			}, u.win)
	})
}

// --- CONFIGURE ----------------------------------------------------------------

func (u *ui) configureTab() fyne.CanvasObject {
	// Geen laad-knop: een doel kiezen ís het verzoek om zijn config te zien
	// (Derek 06-08: "zodra je hebt geselecteerd zie je de config, geen button
	// presses nodig"). De image-knop staat ernaast, want een image-bestand
	// bewerken is de uitzondering, niet de regel.
	loadImg := widget.NewButtonWithIcon("Image file…", theme.FileIcon(), func() {
		if u.imagePath != "" {
			u.run(func() { u.loadConfig(u.imagePath) })
			return
		}
		fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err != nil || r == nil {
				return
			}
			p := r.URI().Path()
			r.Close()
			u.run(func() { u.loadConfig(p) })
		}, u.win)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".img"}))
		fd.Show()
	})
	cardRow := u.targetRow(loadImg, u.targetChosen)

	u.cfgLbl = widget.NewLabel("nothing loaded")
	u.cfgLbl.Truncation = fyne.TextTruncateEllipsis
	u.cfgTags = container.NewHBox()

	// Eén veld dat altijd bewerkbaar is én kleur heeft — geen knop die tussen
	// lezen en schrijven wisselt (Derek 07-08: "je wil het altijd editten, er is
	// geen VIEW mode"). Zie editor.go voor hoe dat kan.
	u.cfgEdit = newCfgEditor()
	u.cfgSave = widget.NewButtonWithIcon("Save to card", theme.DocumentSaveIcon(), u.saveConfig)
	u.cfgSave.Importance = widget.HighImportance
	u.cfgSave.Disable()

	// Geen uitlegregel meer onder de kop: wat CONFIGURE is staat nu op de knop
	// op het beginscherm én in de sectiekop (home.go, descConfigure).
	return container.NewBorder(
		container.NewVBox(
			cardRow,
			container.NewBorder(nil, nil, widget.NewIcon(theme.StorageIcon()),
				u.cfgTags, u.cfgLbl),
		),
		container.NewVBox(u.cfgBusy, u.cfgSave),
		nil, nil,
		u.cfgEdit.obj,
	)
}

// targetChosen: een doel kiezen ís het verzoek om zijn config te zien. Alleen
// laden als de CONFIGURE-tab ook open staat — een raw kaart lezen vraagt
// rechten, en een wachtwoordprompt uit de lucht laten vallen terwijl iemand op
// BURN staat is niet vriendelijk. Anders wacht het doel tot je de tab opent.
func (u *ui) targetChosen(d disk.Device) {
	u.pendingCfg = u.cfgPathFor(d)
	if u.onConfigureTab() {
		u.run(func() { u.loadConfig(u.pendingCfg) })
	}
}

// cfgPathFor: ALTIJD het rauwe device. Nooit "kijken of het OS de partitie
// gemount heeft" — dat was een tweede pad met andere faalmodi, en het hele
// config-venster bestaat juist zodat we van mounten onafhankelijk zijn (Derek
// 06-08: "we zouden alles RAW doen... we hebben niet voor niets magie
// packages gemaakt"). Eén weg voor élk board: unmount, authopen, venster.
func (u *ui) cfgPathFor(d disk.Device) string { return d.RawPath }

func (u *ui) loadConfig(target string) {
	u.busy(true)
	defer u.busy(false)
	content, win, err := opCfgRead(target)
	if err != nil {
		u.fail(err)
		return
	}
	out := string(content)
	u.cfgTarget = target
	desktop := isDesktop(content)
	fyne.Do(func() {
		// De pill MOET hier gemaakt worden en niet erbuiten: hij meet tekst op,
		// en de tekstmeter van Fyne mag alleen van de UI-thread komen (anders
		// crasht de shaper — gemeten 07-08).
		tag := pill("headless", colHeadless)
		if desktop {
			tag = pill("desktop", colDesktop)
		}
		u.cfgEdit.SetText(out)
		u.cfgLbl.SetText(target)
		u.cfgTags.Objects = []fyne.CanvasObject{tag}
		u.cfgTags.Refresh()
		u.cfgSave.SetText("Save to " + shortTarget(target))
		u.cfgSave.Enable()
	})
	u.log(fmt.Sprintf("Config loaded from %s — %d bytes in a %d KB window. Edit it and save.",
		shortTarget(target), win.ContentLen, win.Size>>10))
}

func (u *ui) saveConfig() {
	if u.cfgTarget == "" {
		u.log("Load a config first.")
		return
	}
	target, text := u.cfgTarget, u.cfgEdit.Text()
	u.run(func() {
		u.busy(true)
		defer u.busy(false)
		win, err := opCfgWrite(target, []byte(text))
		if err != nil {
			u.fail(err)
			return
		}
		u.log(fmt.Sprintf("Config saved — %d bytes into the %d KB window at %#x.", len(text), win.Size>>10, win.Off))
		u.log("The node reads it on the next boot. Eject the target and put it back.")
		u.loadConfig(target) // terugleren: de tag volgt de nieuwe inhoud
	})
}

func shortTarget(t string) string {
	if isDevice(t) {
		return "card"
	}
	return filepath.Base(t)
}

// --- FIND ----------------------------------------------------------------------

func (u *ui) findTab() fyne.CanvasObject {
	u.scanBtn = widget.NewButtonWithIcon("Scan my network", theme.SearchIcon(), u.scanNetwork)
	u.scanBtn.Importance = widget.HighImportance

	u.nodeList = widget.NewList(
		func() int { return len(u.nodes) },
		func() fyne.CanvasObject {
			name := widget.NewLabel("template")
			name.Truncation = fyne.TextTruncateEllipsis
			flavor, state := newTagPill(), newTagPill()
			// Twee knoppen per node: GUI = het beeld van de node zelf (/kvm
			// op poort 80), Manage = de node beheren op gui.gethop.org.
			// Beide zijn gewone links — deze tool stuurt NOOIT een sleutel
			// mee; die vult de gebruiker daar zelf in (Derek 06-08).
			kvm := widget.NewButtonWithIcon("GUI", theme.ComputerIcon(), nil)
			manage := widget.NewButtonWithIcon("Manage", theme.SettingsIcon(), nil)
			return container.NewBorder(nil, nil, nil,
				container.NewHBox(state.wrap, flavor.wrap, kvm, manage), name)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			row := o.(*fyne.Container)
			name := row.Objects[0].(*widget.Label)
			right := row.Objects[1].(*fyne.Container)
			n := u.nodes[i]
			name.SetText(fmt.Sprintf("%s   %s   ·   %s", n.IP, n.Name, n.Arch))
			// De pills opnieuw zetten: dezelfde objecten, andere inhoud.
			setPillIn(right.Objects[0], n.State, stateColor(n.State))
			fc := colHeadless
			if n.Flavor == "desktop" {
				fc = colDesktop
			}
			setPillIn(right.Objects[1], n.Flavor, fc)

			kvm := right.Objects[2].(*widget.Button)
			kvm.OnTapped = func() { u.open("http://" + n.IP + "/kvm") }
			// Een headless node serveert geen /kvm: dan is de knop er wel,
			// maar dood — verbergen zou lezen als "deze node kan het niet",
			// en dat is een configuratiekeuze, geen eigenschap.
			if n.Flavor == "desktop" {
				kvm.Enable()
			} else {
				kvm.Disable()
			}
			manage := right.Objects[3].(*widget.Button)
			manage.OnTapped = func() {
				q := url.Values{"name": {n.Name}, "ip": {n.IP}}
				u.open("https://gui.gethop.org/?" + q.Encode())
			}
		})
	u.nodeList.OnSelected = func(i widget.ListItemID) {
		if i >= 0 && i < len(u.nodes) {
			u.showConsole(u.nodes[i])
		}
		u.nodeList.UnselectAll()
	}

	// HOE we een node herkennen (een TCP-connect op de console-poort) is techniek
	// en stond hier alleen maar in de weg — Derek 07-08: "dat we zoeken op poort
	// 5555, who cares". Wat er WEL hoort te staan is de belofte bij de knop die
	// naar buiten gaat: deze tool geeft nooit een sleutel mee.
	hint := canvas.NewText("Click a row to read a node's boot log. GUI opens its own screen; Manage hands only its name and address to gui.gethop.org — never a key.", hopMuted)
	hint.TextSize = 11

	return container.NewBorder(
		container.NewVBox(u.scanBtn, u.findProg.obj, hint),
		nil, nil, nil,
		u.nodeList,
	)
}

// setPillIn zet de tekst van een pill die als template in een lijstrij zit.
// De structuur is die van newTagPill: Center(Stack(rect, Center(text))).
func setPillIn(o fyne.CanvasObject, text string, c color.Color) {
	stack, ok := o.(*fyne.Container).Objects[0].(*fyne.Container)
	if !ok || len(stack.Objects) < 2 {
		return
	}
	r, _ := stack.Objects[0].(*canvas.Rectangle)
	t, _ := stack.Objects[1].(*fyne.Container).Objects[0].(*canvas.Text)
	if r == nil || t == nil {
		return
	}
	t.Text, t.Color, r.StrokeColor = text, c, c
	w := fyne.MeasureText(text, t.TextSize, t.TextStyle).Width
	r.SetMinSize(fyne.NewSize(w+20, 21))
	t.Refresh()
	r.Refresh()
}

func stateColor(s string) color.Color {
	switch {
	case strings.HasPrefix(s, "agent up"):
		return hopPhosphor
	case strings.Contains(s, "parked"), strings.Contains(s, "no network"):
		return colWarn
	default:
		return hopMuted
	}
}

func (u *ui) scanNetwork() {
	if u.scanning {
		if u.cancel != nil {
			u.cancel()
		}
		return
	}
	hosts := find.Subnets()
	if len(hosts) == 0 {
		u.log("No local IPv4 network found to scan.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	u.cancel, u.scanning = cancel, true
	fyne.Do(func() {
		u.nodes = nil
		u.nodeList.Refresh()
		u.scanBtn.SetText("Stop scanning")
	})
	u.log(fmt.Sprintf("Scanning %d addresses on tcp/%d…", len(hosts), find.DefaultPort))
	u.run(func() {
		defer cancel()
		st := find.Scan(ctx, hosts, find.DefaultPort,
			func(n find.Node) {
				// Elke vondst meteen in de lijst — een netwerk vol nodes
				// vult zich zo terwijl de scan nog loopt (Derek 06-08).
				fyne.Do(func() {
					u.nodes = append(u.nodes, n)
					sort.Slice(u.nodes, func(i, j int) bool { return ipLess(u.nodes[i].IP, u.nodes[j].IP) })
					u.nodeList.Refresh()
				})
				extra := ""
				if n.InsecND {
					extra = " — API is OPEN (no key set)"
				}
				u.log(fmt.Sprintf("FOUND %s  node %s  %s  %s%s", n.IP, n.Name, n.Arch, n.State, extra))
			},
			u.findProg.begin2())
		u.scanning = false
		fyne.Do(func() { u.scanBtn.SetText("Scan my network") })
		u.log(fmt.Sprintf("Scan done — %d HopOS node(s) found.", st.Found))
	})
}

// showConsole toont de bewaarde console van een node: hetzelfde als
// `nc <node> 5555`, in een venster.
func (u *ui) showConsole(n find.Node) {
	txt := widget.NewMultiLineEntry()
	txt.TextStyle = fyne.TextStyle{Monospace: true}
	txt.SetText(n.Console)
	sc := container.NewScroll(txt)
	sc.SetMinSize(fyne.NewSize(720, 460))
	dialog.ShowCustom(fmt.Sprintf("%s — %s (console tcp/%d)", n.Name, n.IP, find.DefaultPort), "Close", sc, u.win)
}

func ipLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 4 && i < len(pa) && i < len(pb); i++ {
		if len(pa[i]) != len(pb[i]) {
			return len(pa[i]) < len(pb[i])
		}
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

// --- image-bron ----------------------------------------------------------------

func (u *ui) fetchReleases() {
	u.run(func() {
		all, err := release.List(release.Repo)
		if err != nil {
			u.fail(err)
			return
		}
		rels := release.WithImages(all)
		def, ok := release.Pick(rels)
		if !ok {
			u.log("No release with card images found — use Choose your own… instead.")
			return
		}
		tags := make([]string, len(rels))
		for i, r := range rels {
			tags[i] = r.Tag
		}
		u.log(fmt.Sprintf("%d version(s) with card images, newest first.", len(rels)))
		fyne.Do(func() {
			u.releases = rels
			u.relSel.Options = tags
			u.relSel.Refresh()
			// Verversen is geen nieuwe keuze: stond je op een beta, dan blijf je
			// daarop staan zolang die versie bestaat. Alleen wie nog niets koos
			// krijgt de standaard te zien.
			for _, r := range rels {
				if r.Tag == u.relTag {
					u.applyRelease(r)
					return
				}
			}
			u.applyRelease(def)
		})
	})
}

// pickRelease hangt aan de keuzelijst bovenin: een andere versie kiezen.
func (u *ui) pickRelease(tag string) {
	if tag == u.relTag {
		return // onze eigen SetSelected komt hier terug
	}
	for _, r := range u.releases {
		if r.Tag == tag {
			u.applyRelease(r)
			return
		}
	}
}

// applyRelease zet één release in beeld: het kanaal als gekleurde pil bovenin
// met het versienummer ernaast, en de boards van díe release eronder. Draait
// op de UI-thread — de pil meet tekst op, en dat mag alleen daar.
//
// Een board dat al gekozen was blijft gekozen en volgt mee naar de nieuwe
// versie: van versie wisselen is hetzelfde soort keuze als van smaak wisselen
// (zie setFlavor) — versie + type + board sámen wijzen één image aan.
func (u *ui) applyRelease(rel release.Release) {
	same := u.relTag == rel.Tag
	u.relTag = rel.Tag
	u.relChan.set(rel.Channel(), channelColor(rel.Channel()))
	u.relChan.wrap.Show()
	u.relRow.Refresh() // de pil verschijnt (of wordt breder): de regel moet opnieuw geschikt
	u.relSel.SetSelected(rel.Tag)
	u.boards = parseBoards(release.Images(rel.Assets))
	u.rebuildBoards()
	// Dezelfde versie opnieuw (de verversknop) is géén nieuwe keuze: dan niets
	// zeggen en vooral niet opnieuw gaan downloaden — een .img.gz is niet
	// overslaanbaar, dus dat zou minuten kosten voor een image dat er al is.
	if same {
		return
	}
	u.log(fmt.Sprintf("%s %s: %d board(s). Pick the type, then your board — or choose your own image.",
		rel.Channel(), rel.Tag, len(u.boards)))
	if u.selBoard == "" {
		return
	}
	for _, b := range u.boards {
		if b.id != u.selBoard {
			continue
		}
		if a, ok := b.byFlavor[u.flavor()]; ok {
			u.pickAsset(a)
		} else {
			u.selBoard = ""
			u.rebuildBoards()
			u.log("This version has no " + u.flavor() + " image for that board — pick another board.")
		}
		return
	}
	// Het board bestaat niet in deze versie (een nieuw bordje, een oude release).
	u.selBoard = ""
	u.rebuildBoards()
	u.log("This version has no image for that board — pick another board.")
}

func channelColor(ch string) color.Color {
	if ch == release.Beta {
		return colBeta
	}
	return colStable
}

// flavorOrder is de volgorde van de smaakknoppen: gewoon, kaal, en de GUI met
// codec erbij.
var flavorOrder = []string{"desktop", "headless", "media"}

// parseBoards groepeert de release-assets per board — de naamconventie van
// tools/release.sh: hopos-<board>[-headless|-media].img[.gz].
func parseBoards(imgs []release.Asset) []boardRow {
	byID := map[string]*boardRow{}
	var extra []string
	for _, a := range imgs {
		base, flavor := release.SplitFlavor(a.Name)
		id := strings.TrimPrefix(base, "hopos-")
		b := byID[id]
		if b == nil {
			display := boardNames[id]
			if display == "" {
				display = id
			}
			b = &boardRow{id: id, display: display, byFlavor: map[string]release.Asset{}}
			byID[id] = b
			if !slices.Contains(boardOrder, id) {
				extra = append(extra, id)
			}
		}
		b.byFlavor[flavor] = a
	}
	sort.Strings(extra)
	var out []boardRow
	for _, id := range append(append([]string{}, boardOrder...), extra...) {
		if b := byID[id]; b != nil {
			out = append(out, *b)
		}
	}
	return out
}

func (u *ui) flavor() string { return u.flavorVal }

// setFlavor kiest de smaak en kleurt de knoppen: gekozen = gevuld
// (HighImportance → fosforgroen uit het thema), de andere neutraal.
func (u *ui) setFlavor(fl string) {
	u.flavorVal = fl
	for f, btn := range u.flavorBtns {
		btn.Importance = widget.MediumImportance
		if f == fl {
			btn.Importance = widget.HighImportance
		}
		btn.Refresh()
	}
	u.rebuildBoards()
	// Stond er al een board geselecteerd, dan volgt het image de nieuwe smaak
	// vanzelf — Type + Device sámen zijn de keuze.
	if u.selBoard != "" {
		for _, b := range u.boards {
			if b.id != u.selBoard {
				continue
			}
			if a, ok := b.byFlavor[fl]; ok {
				u.pickAsset(a)
			} else {
				u.selBoard = ""
				u.rebuildBoards()
				u.log("This board has no " + fl + " image — pick another board.")
			}
		}
	}
}

// rebuildBoards vult het knoppengrid: twee kolommen boards, het gekozen board
// gevuld groen. Een board zonder de gekozen smaak staat er wél maar
// uitgeschakeld (de LicheeRV en de Mac mini zijn alleen headless, media is er
// alleen waar een codec-blok zit) — verdwijnen zou lezen als "board niet
// ondersteund", en dat is niet waar.
func (u *ui) rebuildBoards() {
	var cells []fyne.CanvasObject
	for _, b := range u.boards {
		if a, ok := b.byFlavor[u.flavor()]; ok {
			asset, id := a, b.id
			btn := widget.NewButton(b.display, nil)
			btn.OnTapped = func() {
				u.selBoard = id
				u.rebuildBoards()
				u.pickAsset(asset)
			}
			if id == u.selBoard {
				btn.Importance = widget.HighImportance
			}
			cells = append(cells, btn)
		} else {
			btn := widget.NewButton(b.display+" — no "+u.flavor()+" image", nil)
			btn.Disable()
			cells = append(cells, btn)
		}
	}
	if len(cells) == 0 {
		cells = append(cells, widget.NewLabelWithStyle("loading release…", fyne.TextAlignCenter, fyne.TextStyle{Italic: true}))
	}
	u.boardBox.Objects = cells
	u.boardBox.Refresh()
}

func (u *ui) pickAsset(a release.Asset) {
	u.run(func() {
		cache, _ := os.UserCacheDir()
		u.log(fmt.Sprintf("Downloading %s (%d MB)…", a.Name, a.Size>>20))
		// Draaien tot de eerste bytes er zijn: verbinden duurt even, en een balk
		// die op 0% staat leest als "er gebeurt niets".
		prog := u.burnProg.begin()
		u.burnProg.work()
		path, err := release.Download(a, filepath.Join(cache, "hop-imager"), prog)
		u.burnProg.measured()
		if err != nil {
			u.fail(err)
			return
		}
		u.setImage(path)
	})
}

func (u *ui) pickLocal() {
	fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil || r == nil {
			return
		}
		path := r.URI().Path()
		r.Close()
		u.run(func() { u.setImage(path) })
	}, u.win)
	fd.SetFilter(storage.NewExtensionFileFilter([]string{".img"}))
	fd.Show()
}

// setImage kiest het image en zet de tags ernaast: de smaak komt uit het
// config-venster zélf (niet uit de naam — werkt dus ook voor lokale of
// hernoemde images), en een image zonder venster krijgt dat als oranje tag.
func (u *ui) setImage(path string) {
	u.imagePath = path
	f, err := os.Open(path)
	if err != nil {
		u.fail(err)
		return
	}
	defer f.Close()

	// Alleen de KEUZE valt hier; de pill zelf wordt op de UI-thread gemaakt,
	// want die meet tekst op en dat mag alleen daar (zie loadConfig).
	label, col := "no config window", colWarn
	if w, err := cfgwin.Find(f); err == nil {
		content, cerr := cfgwin.Content(f, w)
		switch {
		// Media draagt dezelfde config als desktop — het verschil is gelinkte
		// code, niet een regel in het venster. Hier mag de naam dus wél
		// meebeslissen, maar alleen bovenop een config die beeld claimt.
		case cerr == nil && isDesktop(content) && isMediaName(path):
			label, col = "media", colMedia
			u.log(fmt.Sprintf("Image ready: media — the GUI plus video codec and optical drives (config window at %#x).", w.Off))
		case cerr == nil && isDesktop(content):
			label, col = "desktop", colDesktop
			u.log(fmt.Sprintf("Image ready: desktop — boots into the GUI (config window at %#x).", w.Off))
		default:
			label, col = "headless", colHeadless
			u.log(fmt.Sprintf("Image ready: headless — API + network console (config window at %#x).", w.Off))
		}
	} else {
		u.log("Image ready — " + err.Error())
	}
	fyne.Do(func() {
		u.imageLbl.SetText(filepath.Base(path))
		u.imageTags.Objects = []fyne.CanvasObject{pill(label, col)}
		u.imageTags.Refresh()
	})
}

// isMediaName herkent een media-image aan de naam uit tools/release.sh.
func isMediaName(path string) bool {
	_, fl := release.SplitFlavor(filepath.Base(path))
	return fl == "media"
}

// isDesktop kijkt in de config zelf: een desktop-node is een node waar iets het
// glas claimt (FB=1) of de display-job geseed staat — betrouwbaarder dan een
// bestandsnaam.
func isDesktop(cfg []byte) bool {
	return bytes.Contains(cfg, []byte(`"FB":"1"`)) || bytes.Contains(cfg, []byte(`"name":"display"`))
}

// --- devices --------------------------------------------------------------------

// watchDevices houdt de kaartlijst zelf bij: een kaart insteken hoort genoeg
// te zijn (Derek 06-08: "iedereen wil gewoon dat ie zelf gevonden wordt, en
// wat kost het nou"). Eén diskutil-aanroep per twee seconden, en alleen als de
// lijst écht anders is gaan de selecties op het scherm om — anders zou de
// keuze van de gebruiker onder zijn handen wegspringen.
func (u *ui) watchDevices() {
	go func() {
		last := strings.Join(disk.IDs(), "|")
		for range time.Tick(2 * time.Second) {
			now := strings.Join(disk.IDs(), "|")
			if now == last {
				continue
			}
			last = now
			u.refreshDevices()
		}
	}()
}

func (u *ui) refreshDevices() {
	u.run(func() {
		devs, err := disk.List()
		if err != nil {
			u.fail(err)
			return
		}
		names := make([]string, 0, len(devs))
		fyne.Do(func() {
			u.devices = map[string]disk.Device{}
			for _, d := range devs {
				u.devices[d.Desc] = d
				names = append(names, d.Desc)
			}
			for _, s := range u.cardSels {
				sel := s.Selected
				s.Options = names
				switch {
				case slices.Contains(names, sel):
					s.SetSelected(sel) // keuze van de gebruiker blijft staan
				case len(names) == 1:
					s.SetSelected(names[0]) // precies één kaart: die bedoel je
				default:
					s.ClearSelected()
				}
				s.Refresh()
			}
		})
		switch len(devs) {
		case 0:
			u.log("No card or stick found — insert one and it shows up by itself.")
		default:
			for _, d := range devs {
				u.log("Target: " + d.Desc)
			}
		}
	})
}

// withDevice pakt de kaart die in een van de kiezers geselecteerd staat — welke
// tab je ook gebruikt.
func (u *ui) withDevice(fn func(disk.Device)) {
	for _, s := range u.cardSels {
		if d, ok := u.devices[s.Selected]; ok {
			fn(d)
			return
		}
	}
	u.log("Pick a target card first (rescan if the list is empty).")
}

func (u *ui) eject(d disk.Device) {
	if err := disk.Eject(d); err != nil {
		u.fail(err)
		return
	}
	u.log(d.ID + " ejected — safe to remove.")
}

// --- rechten + helpers ----------------------------------------------------------

// open zet een link in de browser. Alleen adres en naam gaan mee — nooit een
// sleutel: die hoort thuis bij de gebruiker, niet in een URL van deze tool.
func (u *ui) open(raw string) {
	parsed, err := url.Parse(raw)
	if err != nil {
		u.fail(err)
		return
	}
	if err := fyne.CurrentApp().OpenURL(parsed); err != nil {
		u.fail(err)
		return
	}
	u.log("Opened " + raw)
}

// run draait werk buiten de UI-thread; alle schermupdates gaan via fyne.Do.
func (u *ui) run(fn func()) { go fn() }

// busy toont de onbepaalde balk op de CONFIGURE-tab: een venster lezen of
// herschrijven heeft geen meetbare stappen (het zoeken over de kaart is één
// beweging), maar wel een begin en een eind — en dat mag je zien.
func (u *ui) busy(on bool) {
	fyne.Do(func() {
		if on {
			u.cfgBusy.Show()
			u.cfgBusy.Start()
			return
		}
		u.cfgBusy.Stop()
		u.cfgBusy.Hide()
	})
}

func (u *ui) log(s string) {
	fyne.Do(func() {
		u.logLines = append(u.logLines, time.Now().Format("15:04:05")+"  "+s)
		text := ""
		for _, l := range u.logLines {
			text += l + "\n"
		}
		u.status.SetText(text)
		u.logScroll.ScrollToBottom()
		u.lastLbl.SetText(s)
	})
}

func (u *ui) fail(err error) {
	u.log("ERROR: " + err.Error())
	fyne.Do(func() { dialog.ShowError(err, u.win) })
}

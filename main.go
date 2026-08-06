// hop-imager — één tool voor de hele levensloop van een HopOS-node:
//
//	BURN       de laatste release naar een kaart schrijven en verifiëren
//	CONFIGURE  de node-config aanpassen — altijd achteraf, op de kaart zelf
//	FIND       draaiende nodes op het netwerk vinden ("waar staat dat ding?")
//
// Die drie tabs zijn Dereks indeling (06-08) en ze staan niet voor niets in
// die volgorde: configureren hoort ná het flashen, niet tijdens. De
// config-bewerking gaat altijd RAW via het #HOPCFG1-venster
// (internal/cfgwin, formaatcontract met hop-os image/hopcfg) — dat werkt op
// élk board, ook als het OS de partitie niet mount (Radxa) of als de config in
// de kernel zit (LicheeRV). Rechten daarvoor komen van een verhoogde worker
// (internal/priv), niet van "start de tool met sudo".
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
//	./hop-imager -scan      (dezelfde zoeker zonder GUI)
package main

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
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

// bunny: Dereks origineel (2026-07-11) — hetzelfde logo dat elke node bij het
// booten toont (metal/cmd/hopos/main.go), dus de tool en het glas matchen.
const bunny = `   (\(\
   ( -.-)     HopOS
   o_(")(")   --------------
              the Go-only OS`

// boardRow is één board uit de release: dezelfde kaart in (maximaal) twee
// smaken. De knoppen tonen boards; de smaak-keuze erboven kiest het asset.
type boardRow struct {
	id       string
	display  string
	byFlavor map[string]release.Asset // "desktop" / "headless"
}

// boardNames maakt van de asset-id's mensennamen; onbekende id's tonen de id
// zelf, zodat een nieuw board in de release nooit onzichtbaar is.
var boardNames = map[string]string{
	"rpi5":        "Raspberry Pi 5",
	"rpi4":        "Raspberry Pi 4",
	"radxa-zero3": "Radxa Zero 3E",
	"licheerv":    "LicheeRV Nano",
	"uefi":        "UEFI · USB stick",
}

var boardOrder = []string{"rpi5", "rpi4", "radxa-zero3", "licheerv", "uefi"}

type ui struct {
	win fyne.Window

	// BURN
	btnDesktop, btnHeadless *widget.Button
	flavorVal               string
	boardBox                *fyne.Container
	boards                  []boardRow
	selBoard                string
	imageLbl                *widget.Label
	imageTags               *fyne.Container
	imagePath               string

	// CONFIGURE
	cfgEntry  *widget.Entry
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

	// gedeeld
	cardSels  []*widget.Select
	devices   map[string]disk.Device
	progress  *widget.ProgressBar
	status    *widget.Label
	logScroll *container.Scroll
	logLines  []string
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
	// Worker-modus (verhoogd, zie internal/priv): geen GUI, doe de bewerking
	// en print het resultaat.
	if slices.Contains(os.Args[1:], "-op") {
		out, err := runOp(os.Args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Print(out)
		return
	}

	// -scan: dezelfde zoeker zonder GUI. Handig in een script ("waar staan
	// mijn nodes?") en het is het meetinstrument voor de FIND-tab zelf.
	if slices.Contains(os.Args[1:], "-scan") {
		hosts := find.Subnets()
		fmt.Fprintf(os.Stderr, "scanning %d addresses on tcp/%d…\n", len(hosts), find.DefaultPort)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		n := 0
		find.Scan(ctx, hosts, find.DefaultPort, func(nd find.Node) {
			n++
			fmt.Printf("%-15s  %-16s  %-8s  %-22s  api-open=%v\n", nd.IP, nd.Name, nd.Arch, nd.State, nd.InsecND)
		}, nil)
		fmt.Fprintf(os.Stderr, "%d node(s) found\n", n)
		return
	}

	a := app.NewWithID("org.gethop.imager")
	a.Settings().SetTheme(hopTheme{})
	u := &ui{win: a.NewWindow("HopOS Imager"), devices: map[string]disk.Device{}}

	u.progress = widget.NewProgressBar()
	u.status = widget.NewLabel("")
	u.status.Wrapping = fyne.TextWrapWord
	u.status.TextStyle = fyne.TextStyle{Monospace: true}
	u.logScroll = container.NewVScroll(u.status)
	u.logScroll.SetMinSize(fyne.NewSize(0, 130))

	refreshBtn := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), u.fetchReleases)
	header := container.NewBorder(nil, nil, nil, container.NewVBox(refreshBtn),
		container.NewVBox(logoArt(), subtitleText()))

	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("Burn", theme.DownloadIcon(), u.burnTab()),
		container.NewTabItemWithIcon("Configure", theme.SettingsIcon(), u.configureTab()),
		container.NewTabItemWithIcon("Find", theme.SearchIcon(), u.findTab()),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	u.win.SetContent(container.NewBorder(
		container.NewVBox(header, widget.NewSeparator()),
		container.NewVBox(u.progress, u.logScroll),
		nil, nil,
		tabs,
	))
	u.win.Resize(fyne.NewSize(820, 780))

	u.log("Welcome. Loading the latest release…")
	u.fetchReleases()
	u.refreshDevices()
	u.win.ShowAndRun()
}

func subtitleText() fyne.CanvasObject {
	sub := canvas.NewText("imager — burn · configure · find", hopMuted)
	sub.TextStyle = fyne.TextStyle{Monospace: true}
	sub.TextSize = 12
	return container.NewCenter(sub)
}

// --- BURN --------------------------------------------------------------------

func (u *ui) burnTab() fyne.CanvasObject {
	// Twee ONMISKENBARE knoppen (Derek 06-08: "die keuze moet echt
	// duidelijker!"): de gekozen smaak staat gevuld fosforgroen, de andere
	// panel-grijs — een segmented control, geen radiootje.
	u.btnDesktop = widget.NewButtonWithIcon("Desktop (GUI)", theme.ComputerIcon(), func() { u.setFlavor("desktop") })
	u.btnHeadless = widget.NewButtonWithIcon("Headless", theme.VisibilityOffIcon(), func() { u.setFlavor("headless") })
	u.flavorVal = "desktop" // de default van elke release
	flavorRow := container.NewGridWithColumns(2, u.btnDesktop, u.btnHeadless)

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

	cardRow := u.cardRow()

	writeBtn := widget.NewButtonWithIcon("Write", theme.MediaPlayIcon(), u.writeCard)
	writeBtn.Importance = widget.HighImportance
	checkBtn := widget.NewButtonWithIcon("Verify", theme.ConfirmIcon(), u.checkCard)

	return container.NewVBox(
		section("Type"), flavorRow,
		section("Device"), u.boardBox,
		chosenRow,
		widget.NewSeparator(),
		cardRow,
		container.NewGridWithColumns(2, writeBtn, checkBtn),
	)
}

// cardRow bouwt een kaart-kiezer. Elke tab die er een nodig heeft krijgt zijn
// eigen widget (Fyne deelt geen objecten tussen containers); refreshDevices
// vult ze allemaal.
func (u *ui) cardRow() fyne.CanvasObject {
	sel := widget.NewSelect(nil, nil)
	sel.PlaceHolder = "insert a card and pick it here…"
	u.cardSels = append(u.cardSels, sel)
	rescan := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), u.refreshDevices)
	eject := widget.NewButtonWithIcon("Eject", theme.LogoutIcon(), func() {
		u.withDevice(func(d disk.Device) { u.run(func() { u.eject(d) }) })
	})
	return container.NewBorder(nil, nil, section("Target card"), container.NewHBox(rescan, eject), sel)
}

func (u *ui) writeCard() {
	if u.imagePath == "" {
		u.log("Pick an image first.")
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
					u.log(fmt.Sprintf("Writing %d MB to %s (unmounting first)…", st.Size()>>20, d.RawPath))
					start := time.Now()
					if _, err := u.privOp([]string{"-op", "write", "-image", u.imagePath, "-dev", d.RawPath}, true); err != nil {
						u.fail(err)
						return
					}
					u.log(fmt.Sprintf("Write done in %.0fs — now hit Verify.", time.Since(start).Seconds()))
				})
			}, u.win)
	})
}

func (u *ui) checkCard() {
	if u.imagePath == "" {
		u.log("Pick an image first.")
		return
	}
	u.withDevice(func(d disk.Device) {
		u.run(func() {
			u.log("Verifying — reading the card back and comparing…")
			out, err := u.privOp([]string{"-op", "check", "-image", u.imagePath, "-dev", d.RawPath}, true)
			if err != nil {
				u.fail(err)
				return
			}
			f := strings.Fields(out)
			switch {
			case len(f) > 1 && f[0] == "MATCH":
				u.log("VERIFY OK — the card matches the image byte for byte (sha256 " + f[1][:12] + "…).")
			case len(f) > 0 && f[0] == "MISMATCH":
				u.log("VERIFY FAILED — the card does NOT match the image. Write it again.")
			default:
				u.log("Verify: unexpected answer " + strings.TrimSpace(out))
			}
		})
	})
}

// --- CONFIGURE ----------------------------------------------------------------

func (u *ui) configureTab() fyne.CanvasObject {
	cardRow := u.cardRow()
	loadCard := widget.NewButtonWithIcon("Load from card", theme.DownloadIcon(), func() {
		u.withDevice(func(d disk.Device) { u.run(func() { u.loadConfig(d.RawPath) }) })
	})
	loadCard.Importance = widget.HighImportance
	loadImg := widget.NewButtonWithIcon("Load from image file…", theme.FileIcon(), func() {
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

	u.cfgLbl = widget.NewLabel("nothing loaded")
	u.cfgLbl.Truncation = fyne.TextTruncateEllipsis
	u.cfgTags = container.NewHBox()
	u.cfgEntry = widget.NewMultiLineEntry()
	u.cfgEntry.TextStyle = fyne.TextStyle{Monospace: true}
	u.cfgEntry.SetPlaceHolder("load a config from a card or an image file")
	u.cfgSave = widget.NewButtonWithIcon("Save to card", theme.DocumentSaveIcon(), u.saveConfig)
	u.cfgSave.Importance = widget.HighImportance
	u.cfgSave.Disable()

	hint := canvas.NewText("configuring is always afterwards: write the card, then edit its config — as often as you like", hopMuted)
	hint.TextSize = 11

	return container.NewBorder(
		container.NewVBox(
			cardRow,
			container.NewGridWithColumns(2, loadCard, loadImg),
			container.NewBorder(nil, nil, widget.NewIcon(theme.StorageIcon()), u.cfgTags, u.cfgLbl),
			hint,
		),
		container.NewVBox(u.cfgSave),
		nil, nil,
		u.cfgEntry,
	)
}

func (u *ui) loadConfig(target string) {
	out, err := u.privOp([]string{"-op", "cfgread", "-dev", target}, false)
	if err != nil {
		u.fail(err)
		return
	}
	u.cfgTarget = target
	tag := pill("headless", colHeadless)
	if isDesktop([]byte(out)) {
		tag = pill("desktop", colDesktop)
	}
	fyne.Do(func() {
		u.cfgEntry.SetText(out)
		u.cfgLbl.SetText(target)
		u.cfgTags.Objects = []fyne.CanvasObject{tag}
		u.cfgTags.Refresh()
		u.cfgSave.SetText("Save to " + shortTarget(target))
		u.cfgSave.Enable()
	})
	u.log(fmt.Sprintf("Config loaded from %s (%d bytes). Edit it and save.", target, len(out)))
}

func (u *ui) saveConfig() {
	if u.cfgTarget == "" {
		u.log("Load a config first.")
		return
	}
	target, text := u.cfgTarget, u.cfgEntry.Text
	u.run(func() {
		tmp, err := os.CreateTemp("", "hopos-*.cfg")
		if err != nil {
			u.fail(err)
			return
		}
		defer os.Remove(tmp.Name())
		if _, err := tmp.WriteString(text); err != nil {
			tmp.Close()
			u.fail(err)
			return
		}
		tmp.Close()
		out, err := u.privOp([]string{"-op", "cfgwrite", "-dev", target, "-cfg", tmp.Name()}, false)
		if err != nil {
			u.fail(err)
			return
		}
		u.log("Config saved — " + strings.TrimSpace(out))
		u.log("The node reads it on the next boot. Eject the card and put it back.")
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
			return container.NewBorder(nil, nil, nil,
				container.NewHBox(state.wrap, flavor.wrap), name)
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
		})
	u.nodeList.OnSelected = func(i widget.ListItemID) {
		if i >= 0 && i < len(u.nodes) {
			u.showConsole(u.nodes[i])
		}
		u.nodeList.UnselectAll()
	}

	hint := canvas.NewText(fmt.Sprintf("every HopOS node serves its console on tcp/%d — that is how we recognise it, and how you read its boot log", find.DefaultPort), hopMuted)
	hint.TextSize = 11

	return container.NewBorder(
		container.NewVBox(section("Nodes on this network"), u.scanBtn, hint),
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
		find.Scan(ctx, hosts, find.DefaultPort,
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
			func(done, total int) {
				fyne.Do(func() { u.progress.SetValue(float64(done) / float64(total)) })
			})
		count := len(u.nodes)
		u.scanning = false
		fyne.Do(func() { u.scanBtn.SetText("Scan my network") })
		u.log(fmt.Sprintf("Scan done — %d HopOS node(s) found.", count))
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
		tag, assets, err := release.Latest(release.Repo)
		if err != nil {
			u.fail(err)
			return
		}
		boards := parseBoards(release.Images(assets))
		fyne.Do(func() {
			u.boards = boards
			u.rebuildBoards()
		})
		u.log(fmt.Sprintf("Release %s: %d board(s). Pick the type, then your board — or choose your own image.", tag, len(boards)))
		if len(boards) == 0 {
			u.log("This release carries no .img assets yet — use Choose your own… instead.")
		}
	})
}

// parseBoards groepeert de release-assets per board — de naamconventie van
// tools/release.sh: hopos-<board>[-headless].img[.gz].
func parseBoards(imgs []release.Asset) []boardRow {
	byID := map[string]*boardRow{}
	var extra []string
	for _, a := range imgs {
		base := strings.TrimSuffix(strings.TrimSuffix(a.Name, ".gz"), ".img")
		flavor := "desktop"
		if strings.HasSuffix(base, "-headless") {
			flavor, base = "headless", strings.TrimSuffix(base, "-headless")
		}
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
	u.btnDesktop.Importance = widget.MediumImportance
	u.btnHeadless.Importance = widget.MediumImportance
	if fl == "desktop" {
		u.btnDesktop.Importance = widget.HighImportance
	} else {
		u.btnHeadless.Importance = widget.HighImportance
	}
	u.btnDesktop.Refresh()
	u.btnHeadless.Refresh()
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
// uitgeschakeld (de LicheeRV is alleen headless) — verdwijnen zou lezen als
// "board niet ondersteund", en dat is niet waar.
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
		path, err := release.Download(a, filepath.Join(cache, "hop-imager"), u.progressCB())
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
	var tags []fyne.CanvasObject
	f, err := os.Open(path)
	if err != nil {
		u.fail(err)
		return
	}
	defer f.Close()
	if w, err := cfgwin.Find(f); err == nil {
		if content, cerr := cfgwin.Content(f, w); cerr == nil && isDesktop(content) {
			tags = append(tags, pill("desktop", colDesktop))
			u.log(fmt.Sprintf("Image ready: desktop — boots into the GUI (config window at %#x).", w.Off))
		} else {
			tags = append(tags, pill("headless", colHeadless))
			u.log(fmt.Sprintf("Image ready: headless — API + network console (config window at %#x).", w.Off))
		}
	} else {
		tags = append(tags, pill("no config window", colWarn))
		u.log("Image ready — " + err.Error())
	}
	fyne.Do(func() {
		u.imageLbl.SetText(filepath.Base(path))
		u.imageTags.Objects = tags
		u.imageTags.Refresh()
	})
}

// isDesktop kijkt in de config zelf: een desktop-node is een node waar iets het
// glas claimt (FB=1) of de display-job geseed staat — betrouwbaarder dan een
// bestandsnaam.
func isDesktop(cfg []byte) bool {
	return bytes.Contains(cfg, []byte(`"FB":"1"`)) || bytes.Contains(cfg, []byte(`"name":"display"`))
}

// --- devices --------------------------------------------------------------------

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
				if slices.Contains(names, sel) {
					s.SetSelected(sel)
				}
				s.Refresh()
			}
		})
		u.log(fmt.Sprintf("Found %d external disk(s).", len(devs)))
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

// privOp voert een bewerking uit die het rauwe device raakt: zelf als we al
// root zijn, anders via een verhoogde worker (systeem-wachtwoordprompt). Met
// withProgress leest de GUI ondertussen het voortgangsbestand van de worker.
func (u *ui) privOp(args []string, withProgress bool) (string, error) {
	if withProgress {
		tmp, err := os.CreateTemp("", "hopimg-*.prog")
		if err != nil {
			return "", err
		}
		pf := tmp.Name()
		tmp.Close()
		defer os.Remove(pf)
		args = append(args, "-progress", pf)
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			t := time.NewTicker(150 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					if d, total := priv.Progress(pf); total > 0 {
						v := float64(d) / float64(total)
						fyne.Do(func() { u.progress.SetValue(v) })
					}
				}
			}
		}()
	}
	if priv.IsRoot() {
		return runOp(args)
	}
	return priv.Escalate(args)
}

// run draait werk buiten de UI-thread; alle schermupdates gaan via fyne.Do.
func (u *ui) run(fn func()) { go fn() }

func (u *ui) progressCB() func(done, total int64) {
	return func(done, total int64) {
		if total > 0 {
			v := float64(done) / float64(total)
			fyne.Do(func() { u.progress.SetValue(v) })
		}
	}
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
	})
}

func (u *ui) fail(err error) {
	u.log("ERROR: " + err.Error())
	fyne.Do(func() { dialog.ShowError(err, u.win) })
}

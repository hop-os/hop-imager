// mkicon tekent het HopOS-icoon: DE BUNNY IN ASCII. Niet een vorm die op de
// bunny lijkt — de tekens zelf, want ASCII is wat HopOS doet: elke node zet deze
// regels op de UART en op het glas, en de imager heeft ze in zijn kop staan.
//
//	go run ./tools/mkicon           → icon.png (512) + icon.iconset/ voor iconutil
//	go run ./tools/mkicon -sheet    → icon-sheet.png: alle maten naast elkaar
//
// Gerenderd met de bitmapfont uit x/image (7×13) en daarna met een HEEL getal
// opgeschaald — nearest neighbour, geen interpolatie. Dat is precies wat je hier
// wil: de pixels blijven vierkant, dus het blijft ASCII in plaats van een vage
// letter.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

var (
	bg       = color.NRGBA{0x0B, 0x0F, 0x14, 0xFF} // --bg van gethop.org
	phosphor = color.NRGBA{0x46, 0xD9, 0x79, 0xFF} // --phosphor
)

// De bunny zoals hij in de node staat (hopos/src/main.rs). Alleen het
// dier: "HopOS / the Rust-only OS" staat er in de app naast en zou een vierkant
// icoon plat drukken.
var bunny = []string{
	`(\(\`,
	`( -.-)`,
	`o_(")(")`,
}

// Op kleine maten alleen de oren (Derek 06-08: "die zijn overduidelijk oren").
// Drie regels van acht tekens in 16 pixels wordt een groene vlek; deze ene regel
// houdt de vorm die je herkent.
var ears = []string{`(\(\`}

// linesFor kiest de tekening bij de maat. De grens ligt waar de hele bunny nog
// op ECHTE pixels past: de bron van 56×39 vult bij schaal 1 al 87% van 64.
func linesFor(size int) []string {
	if size < 64 {
		return ears
	}
	return bunny
}

// art rendert de bunny één-op-één met de bitmapfont: 7 pixels per teken, 13 per
// regel. Dit is de bron waar elke maat uit opgeschaald wordt.
func art(lines []string) *image.NRGBA {
	f := basicfont.Face7x13
	w := 0
	for _, l := range lines {
		if n := len(l) * f.Advance; n > w {
			w = n
		}
	}
	h := len(lines) * f.Height
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	d := &font.Drawer{Dst: img, Src: image.NewUniform(phosphor), Face: f}
	for i, l := range lines {
		d.Dot = fixed.P(0, (i+1)*f.Height-f.Descent)
		d.DrawString(l)
	}
	return img
}

// squircle is de achtergrondvorm: een afgerond vierkant zoals macOS-iconen
// hebben, zodat het icoon niet als losse tekening in de dock hangt.
func squircle(x, y float64) bool {
	const n = 4.0 // hoe hoger, hoe vierkanter
	dx, dy := math.Abs(x-0.5)/0.5, math.Abs(y-0.5)/0.5
	return math.Pow(dx, n)+math.Pow(dy, n) <= 1
}

func render(size int) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, size, size))

	// Achtergrond met zachte rand (4× supersampling op de squircle).
	const ss = 4
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			in := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/ss) / float64(size)
					if squircle(x, y) {
						in++
					}
				}
			}
			if in > 0 {
				out.SetNRGBA(px, py, color.NRGBA{bg.R, bg.G, bg.B, uint8(float64(in) / (ss * ss) * 255)})
			}
		}
	}

	// De ASCII erover, gecentreerd op ~95% van de breedte. Past hij op een HEEL
	// veelvoud, dan schalen we zo — dan blijft elke fontpixel een vierkant
	// blokje. Alleen op de allerkleinste maat kan dat niet en wordt er
	// nearest-neighbour verkleind; liever een beetje ruw dan geklipt.
	a := art(linesFor(size))
	aw, ah := a.Bounds().Dx(), a.Bounds().Dy()
	const fill = 0.95
	dw, dh := aw, ah
	if n := int(math.Floor(float64(size) * fill / float64(aw))); n >= 1 {
		dw, dh = aw*n, ah*n
	} else {
		dw = int(float64(size) * fill)
		dh = ah * dw / aw
	}
	ox, oy := (size-dw)/2, (size-dh)/2
	for py := 0; py < dh; py++ {
		for px := 0; px < dw; px++ {
			c := sample(a, px, py, dw, dh)
			if c.A == 0 {
				continue
			}
			x, y := ox+px, oy+py
			if x < 0 || y < 0 || x >= size || y >= size || out.NRGBAAt(x, y).A == 0 {
				continue
			}
			f := float64(c.A) / 255
			b := out.NRGBAAt(x, y)
			out.SetNRGBA(x, y, color.NRGBA{
				R: mix(b.R, c.R, f), G: mix(b.G, c.G, f), B: mix(b.B, c.B, f), A: b.A,
			})
		}
	}
	return out
}

// sample haalt één doelpixel uit de bron. Groter maken is een blokje kopiëren
// (dus scherp); kleiner maken is het GEMIDDELDE van de bronpixels die eronder
// vallen. Dat laatste is waarom de oren op 16 pixels nog twee oren zijn en geen
// handvol losse stippen: nearest neighbour gooit daar juist de pixels weg die de
// vorm dragen.
func sample(src *image.NRGBA, px, py, dw, dh int) color.NRGBA {
	aw, ah := src.Bounds().Dx(), src.Bounds().Dy()
	x0, x1 := px*aw/dw, (px+1)*aw/dw
	y0, y1 := py*ah/dh, (py+1)*ah/dh
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	var sum, n float64
	for y := y0; y < y1 && y < ah; y++ {
		for x := x0; x < x1 && x < aw; x++ {
			sum += float64(src.NRGBAAt(x, y).A)
			n++
		}
	}
	if n == 0 {
		return color.NRGBA{}
	}
	return color.NRGBA{phosphor.R, phosphor.G, phosphor.B, uint8(sum/n + 0.5)}
}

func mix(a, b uint8, f float64) uint8 {
	return uint8(float64(a)*(1-f) + float64(b)*f + 0.5)
}

// sheet zet het icoon in alle maten naast elkaar, om te keuren hoe het schaalt.
func sheet(path string) error {
	sizes := []int{512, 256, 128, 64, 32, 16}
	const pad = 24
	w, h := pad, 512+2*pad
	for _, s := range sizes {
		w += s + pad
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.NRGBA{0x3A, 0x3F, 0x46, 0xFF}), image.Point{}, draw.Src)
	x := pad
	for _, s := range sizes {
		off := pad + 512 - s // onderkanten uitlijnen
		draw.Draw(canvas, image.Rect(x, off, x+s, off+s), render(s), image.Point{}, draw.Over)
		x += s + pad
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, canvas)
}

func write(path string, size int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, render(size))
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-sheet" {
		if err := sheet("icon-sheet.png"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "icon-sheet.png klaar")
		return
	}
	if err := write("icon.png", 512); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, s := range []struct {
		name string
		size int
	}{
		{"icon_16x16.png", 16}, {"icon_16x16@2x.png", 32},
		{"icon_32x32.png", 32}, {"icon_32x32@2x.png", 64},
		{"icon_128x128.png", 128}, {"icon_128x128@2x.png", 256},
		{"icon_256x256.png", 256}, {"icon_256x256@2x.png", 512},
		{"icon_512x512.png", 512}, {"icon_512x512@2x.png", 1024},
	} {
		if err := write(filepath.Join("icon.iconset", s.name), s.size); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fmt.Fprintln(os.Stderr, "icon.png + icon.iconset/ klaar")
}

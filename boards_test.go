package main

import (
	"slices"
	"testing"

	"github.com/hop-os/hop-imager/internal/release"
)

type wantBoard struct {
	id, display string
	flavors     []string
}

func checkBoards(t *testing.T, names []string, want []wantBoard) []boardRow {
	t.Helper()
	var imgs []release.Asset
	for _, n := range names {
		imgs = append(imgs, release.Asset{Name: n})
	}
	got := parseBoards(imgs)
	if len(got) != len(want) {
		t.Fatalf("got %d boards, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		b := got[i]
		if b.id != w.id || b.display != w.display {
			t.Errorf("board %d = %s (%s), want %s (%s)", i, b.id, b.display, w.id, w.display)
		}
		if len(b.byFlavor) != len(w.flavors) {
			t.Errorf("%s: %d flavours, want %v", b.id, len(b.byFlavor), w.flavors)
		}
		for _, fl := range w.flavors {
			if _, ok := b.byFlavor[fl]; !ok {
				t.Errorf("%s: no %s image", b.id, fl)
			}
		}
	}
	return got
}

// De kaart-images van HopOS v3 (Rust), uit tools/release.sh: twee smaken,
// allebei in de naam, en de Radxa en de Mac mini onder hun v3-id.
func TestParseBoardsV3(t *testing.T) {
	got := checkBoards(t, []string{
		"hopos-altra-headfull.img.gz", "hopos-altra-headless.img.gz",
		"hopos-apple-headless.img.gz",
		"hopos-licheerv-headless.img.gz",
		"hopos-o6n-headfull.img.gz", "hopos-o6n-headless.img.gz",
		"hopos-radxa-headfull.img.gz", "hopos-radxa-headless.img.gz",
		"hopos-rpi4-headfull.img.gz", "hopos-rpi4-headless.img.gz",
		"hopos-rpi5-headfull.img.gz", "hopos-rpi5-headless.img.gz",
	}, []wantBoard{
		{"rpi5", "Raspberry Pi 5", []string{"headfull", "headless"}},
		{"rpi4", "Raspberry Pi 4", []string{"headfull", "headless"}},
		{"radxa", "Radxa Zero 3E", []string{"headfull", "headless"}},
		{"o6n", "Radxa Orion O6N", []string{"headfull", "headless"}},
		{"altra", "Ampere Altra", []string{"headfull", "headless"}},
		{"licheerv", "LicheeRV Nano", []string{"headless"}},
		{"apple", "Mac mini M4", []string{"headless"}},
	})
	if fl := releaseFlavors(got); !slices.Equal(fl, []string{"headfull", "headless"}) {
		t.Errorf("v3 flavours = %v", fl)
	}
}

// De kaart-images van HopOS v2.2.8, de eerste Go-release met drie smaken. De
// Radxa en de Mac mini krijgen de id van v3, zodat een gekozen board meegaat.
func TestParseBoardsV2ThreeFlavors(t *testing.T) {
	got := checkBoards(t, []string{
		"hopos-altra-headless.img.gz", "hopos-altra.img.gz",
		"hopos-licheerv-headless.img.gz", "hopos-m4-headless.img.gz",
		"hopos-o6n-headless.img.gz", "hopos-o6n-media.img.gz", "hopos-o6n.img.gz",
		"hopos-radxa-zero3-headless.img.gz", "hopos-radxa-zero3.img.gz",
		"hopos-rpi4-headless.img.gz", "hopos-rpi4.img.gz",
		"hopos-rpi5-headless.img.gz", "hopos-rpi5.img.gz",
	}, []wantBoard{
		{"rpi5", "Raspberry Pi 5", []string{"desktop", "headless"}},
		{"rpi4", "Raspberry Pi 4", []string{"desktop", "headless"}},
		{"radxa", "Radxa Zero 3E", []string{"desktop", "headless"}},
		{"o6n", "Radxa Orion O6N", []string{"desktop", "headless", "media"}},
		{"altra", "Ampere Altra", []string{"desktop", "headless"}},
		{"licheerv", "LicheeRV Nano", []string{"headless"}},
		{"apple", "Mac mini M4", []string{"headless"}},
	})
	if fl := releaseFlavors(got); !slices.Equal(fl, []string{"desktop", "headless", "media"}) {
		t.Errorf("v2 flavours = %v", fl)
	}
}

// Van versie wisselen houdt de soort kaart: met glas blijft met glas.
func TestEquivalentFlavor(t *testing.T) {
	v3 := []string{"headfull", "headless"}
	v2 := []string{"desktop", "headless", "media"}
	for _, c := range []struct {
		from  string
		avail []string
		want  string
	}{
		{"headfull", v2, "desktop"},
		{"desktop", v3, "headfull"},
		{"media", v3, "headfull"},
		{"headless", v3, "headless"},
		{"headless", v2, "headless"},
		{"", v3, "headless"},
		{"media", v2, "media"},
	} {
		if got := equivalentFlavor(c.from, c.avail); got != c.want {
			t.Errorf("equivalentFlavor(%q, %v) = %q, want %q", c.from, c.avail, got, c.want)
		}
	}
}

func TestIsMediaBoard(t *testing.T) {
	for name, want := range map[string]bool{
		"/cache/hop-imager/hopos-o6n-headfull.img": true,
		"hopos-o6n-headless.img":                   true, // het board; de smaak beslist setImage
		"hopos-rpi4-headfull.img":                  false,
		"my-o6n-card.img":                          false,
	} {
		if got := isMediaBoard(name); got != want {
			t.Errorf("isMediaBoard(%q) = %v, want %v", name, got, want)
		}
	}
}

// De staart van image/cfg/headfull.cfg uit HopOS v3: welcome aan, de
// display-job als commentaar. Die uitgezette regel mag niet tellen.
const headfullTail = `hopos.cluster=hopos
hopos.insecure=1
hopos.console=1
# welcome op poort 80
hopos.init[]={"name":"welcome","driver":"hop","artifacts":[{"url":"https://github.com/hop-os/HopOS/releases/download/apps/welcome-arm64.elf","match":{"node.arch":"arm64"}}],"memory_limit":33554432,"ports":{"http":80}}
#hopos.init[]={"name":"display","driver":"hop","artifacts":[{"url":"https://github.com/hop-os/HopOS/releases/download/apps/display-arm64.elf","match":{"node.arch":"arm64"}}],"memory_limit":33554432,"env":{"GUI":"display"}}
`

func TestConfigGlass(t *testing.T) {
	headless := []byte("hopos.cluster=hopos\nhopos.insecure=1\nhopos.console=1\n")
	withDisplay := []byte(headfullTail + `hopos.init[]={"name":"display","driver":"hop","env":{"GUI":"display"}}` + "\n")
	goDesktop := []byte(`hopos.init[]={"name":"display","env":{"FB":"1"}}` + "\n")
	for _, c := range []struct {
		name           string
		cfg            []byte
		glass, headful bool
	}{
		{"headless.cfg", headless, false, false},
		{"headfull.cfg", []byte(headfullTail), false, true},
		{"headfull.cfg + display", withDisplay, true, true},
		{"v2 desktop", goDesktop, true, true},
	} {
		if got := seedsGlass(c.cfg); got != c.glass {
			t.Errorf("%s: seedsGlass = %v, want %v", c.name, got, c.glass)
		}
		if got := looksHeadfull(c.cfg); got != c.headful {
			t.Errorf("%s: looksHeadfull = %v, want %v", c.name, got, c.headful)
		}
	}
}

// Linkt het image de gui? De naam eerst, dan de default-config.
func TestImageHasGlass(t *testing.T) {
	for _, c := range []struct {
		path string
		def  string
		want bool
	}{
		{"/c/hopos-rpi4-headfull.img", "hopos.cluster=hopos\n", true},
		{"/c/hopos-rpi4-headless.img", headfullTail, false},
		{"/c/hopos-o6n-media.img", "", true},
		{"/c/hopos-rpi4.img", "", true}, // v2 desktop: geen suffix
		{"/c/mine.img", `hopos.init[]={"name":"display","env":{"FB":"1"}}`, true},
		{"/c/mine.img", headfullTail, false}, // display staat uit
	} {
		if got := imageHasGlass(c.path, []byte(c.def)); got != c.want {
			t.Errorf("imageHasGlass(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

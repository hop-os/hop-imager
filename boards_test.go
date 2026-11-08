package main

import (
	"testing"

	"github.com/hop-os/hop-imager/internal/release"
)

// De kaart-images van HopOS v2.2.8, de eerste release met drie smaken.
func TestParseBoardsThreeFlavors(t *testing.T) {
	var imgs []release.Asset
	for _, n := range []string{
		"hopos-altra-headless.img.gz", "hopos-altra.img.gz",
		"hopos-licheerv-headless.img.gz", "hopos-m4-headless.img.gz",
		"hopos-o6n-headless.img.gz", "hopos-o6n-media.img.gz", "hopos-o6n.img.gz",
		"hopos-radxa-zero3-headless.img.gz", "hopos-radxa-zero3.img.gz",
		"hopos-rpi4-headless.img.gz", "hopos-rpi4.img.gz",
		"hopos-rpi5-headless.img.gz", "hopos-rpi5.img.gz",
	} {
		imgs = append(imgs, release.Asset{Name: n})
	}
	want := []struct {
		id, display string
		flavors     []string
	}{
		{"rpi5", "Raspberry Pi 5", []string{"desktop", "headless"}},
		{"rpi4", "Raspberry Pi 4", []string{"desktop", "headless"}},
		{"radxa-zero3", "Radxa Zero 3E", []string{"desktop", "headless"}},
		{"o6n", "Radxa Orion O6N", []string{"desktop", "headless", "media"}},
		{"altra", "Ampere Altra", []string{"desktop", "headless"}},
		{"licheerv", "LicheeRV Nano", []string{"headless"}},
		{"m4", "Mac mini M4", []string{"headless"}},
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
}

func TestIsMediaName(t *testing.T) {
	for name, want := range map[string]bool{
		"/cache/hop-imager/hopos-o6n-media.img": true,
		"hopos-o6n.img":                         false,
		"hopos-o6n-headless.img":                false,
		"my-media-card.img":                     false,
	} {
		if got := isMediaName(name); got != want {
			t.Errorf("isMediaName(%q) = %v, want %v", name, got, want)
		}
	}
}

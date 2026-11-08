package release

import "testing"

func img(name string) Asset { return Asset{Name: name} }

// De keuzelijst mag alleen versies tonen die je ook echt kunt branden, en de
// tool mag NOOIT uit zichzelf een beta laten zien: een voorproefje is een
// keuze. Beide regels zijn hier één functie, dus ze horen ook getest.
func TestPickPrefersNewestStable(t *testing.T) {
	rels := []Release{
		{Tag: "v1.12.0-beta.3", Prerelease: true, Assets: []Asset{img("hopos-rpi5.img.gz")}},
		{Tag: "v1.11.1", Assets: []Asset{img("hopos-rpi5.img.gz")}},
		{Tag: "v1.11.0", Assets: []Asset{img("hopos-rpi5.img.gz")}},
		{Tag: "v1.5.5", Assets: []Asset{img("Source code.zip")}}, // geen image
	}

	list := WithImages(rels)
	if len(list) != 3 {
		t.Fatalf("%d versie(s) met images, wilde 3: %v", len(list), list)
	}

	got, ok := Pick(list)
	if !ok || got.Tag != "v1.11.1" {
		t.Fatalf("Pick gaf %q (ok=%v), wilde de nieuwste stable v1.11.1", got.Tag, ok)
	}
	if got.Channel() != Stable || list[0].Channel() != Beta {
		t.Fatalf("kanalen kloppen niet: %q en %q", got.Channel(), list[0].Channel())
	}
}

// Een repo die nog nooit een stable uitbracht (of een fork die met beta's
// begint) mag geen leeg scherm opleveren: dan is de nieuwste beta het enige
// dat er te branden valt, en dus wat je te zien krijgt.
func TestPickFallsBackToBeta(t *testing.T) {
	rels := []Release{
		{Tag: "v0.2.0-beta.1", Prerelease: true},
		{Tag: "v0.1.0-beta.1", Prerelease: true},
	}
	got, ok := Pick(rels)
	if !ok || got.Tag != "v0.2.0-beta.1" {
		t.Fatalf("Pick gaf %q (ok=%v), wilde de nieuwste beta", got.Tag, ok)
	}
	if _, ok := Pick(nil); ok {
		t.Fatal("Pick op een lege lijst hoort niets te kiezen")
	}
}

// De asset-namen van beide generaties: v3 draagt de smaak altijd in de naam
// (headless/headfull), v2 kende geen suffix voor desktop.
func TestSplitFlavor(t *testing.T) {
	for name, want := range map[string][2]string{
		"hopos-rpi4-headless.img.gz":  {"hopos-rpi4", Headless},
		"hopos-rpi4-headfull.img.gz":  {"hopos-rpi4", Headfull},
		"hopos-o6n-headfull.img.gz":   {"hopos-o6n", Headfull},
		"hopos-apple-headless.img.gz": {"hopos-apple", Headless},
		"hopos-radxa-zero3.img.gz":    {"hopos-radxa-zero3", Desktop},
		"hopos-o6n-media.img.gz":      {"hopos-o6n", Media},
		"hopos-licheerv-headless.img": {"hopos-licheerv", Headless},
	} {
		base, fl := SplitFlavor(name)
		if base != want[0] || fl != want[1] {
			t.Errorf("SplitFlavor(%q) = %q, %q; wil %q, %q", name, base, fl, want[0], want[1])
		}
	}
}

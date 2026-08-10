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

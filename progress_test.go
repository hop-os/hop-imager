package main

import (
	"testing"

	"fyne.io/fyne/v2/test"
)

// De balk moet de WAARHEID vertellen over wat de tool doet. Twee dingen dus:
// een fase zonder stappen laat de onbepaalde balk draaien (anders staat hij op
// 100% terwijl je config nog teruggezet wordt), en zodra er wél iets te tellen
// valt neemt de gewone balk het over zonder dat iemand daarom hoeft te vragen.
func TestProgressWorkThenMeasured(t *testing.T) {
	test.NewApp()

	p := newProg()
	if p.spin.Visible() {
		t.Fatal("de onbepaalde balk hoort in rust verborgen te zijn")
	}

	cb := p.begin()
	p.work()
	if !p.spin.Visible() || p.bar.Visible() {
		t.Fatal("work(): de onbepaalde balk hoort te draaien en de gewone weg te zijn")
	}

	// De eerste telling neemt het over — geen aparte aanroep nodig.
	cb(1, 4)
	if p.spin.Visible() || !p.bar.Visible() {
		t.Fatal("een telling hoort de gewone balk terug te zetten")
	}
	if p.bar.Value != 0.25 {
		t.Fatalf("balk staat op %v, wilde 0.25", p.bar.Value)
	}

	// En een fase die niet kan tellen mag er daarna weer overheen.
	p.work()
	if !p.spin.Visible() {
		t.Fatal("work() na een telling hoort weer te draaien")
	}
	p.measured()
	if p.spin.Visible() || !p.bar.Visible() {
		t.Fatal("measured(): de gewone balk hoort terug te zijn")
	}
}

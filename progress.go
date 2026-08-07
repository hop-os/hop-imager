// progress.go — een voortgangsbalk die van één taak is, die zichzelf opruimt,
// en die ook iets laat zien als er niets te tellen valt.
//
// DRIE FOUTEN DIE HIER OPGELOST WORDEN (Derek, 07-08):
//
//  1. Er was ÉÉN balk, onderaan het venster, gedeeld door alle drie de tabs. Dus
//     een flash op BURN liet CONFIGURE eruitzien alsof de config geschreven was
//     en FIND alsof de scan klaar was. Een balk hoort bij het werk dat je ziet,
//     dus staat er nu één per scherm.
//  2. Een balk die op 100% blijft staan, blijft beweren dat er iets klaar is —
//     ook een kwartier later. Na vijf seconden gaat hij terug naar nul.
//  3. Niet elke fase heeft stappen. Bij het schrijven en verifiëren kun je
//     tellen, maar "je config terugzetten" is één beweging. Die stond dus op
//     100% te wachten, en dat is precies het moment waarop de balk liegt: de
//     tool is nog bezig en de balk zegt klaar. Zo'n fase draait nu de onbepaalde
//     balk (work()), en de eerste telling zet hem vanzelf weer op de gewone.
//
// De generatie-teller is het enige subtiele hier: start je een nieuwe taak
// binnen die vijf seconden, dan mag de reset van de vórige taak de verse
// voortgang niet wissen. Elke taak krijgt daarom een nummer, en een reset doet
// alleen iets als hij nog van de laatste taak is — én als de balk nog echt op
// 100% staat.
package main

import (
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// resetAfter is hoe lang 100% blijft staan. Lang genoeg om te zien dat het
// klaar is, kort genoeg om niet te liegen.
const resetAfter = 5 * time.Second

type prog struct {
	bar  *widget.ProgressBar
	spin *widget.ProgressBarInfinite
	obj  fyne.CanvasObject // de twee op elkaar; er is er altijd één zichtbaar
	gen  atomic.Int64
}

func newProg() *prog {
	p := &prog{bar: widget.NewProgressBar(), spin: widget.NewProgressBarInfinite()}
	p.spin.Stop()
	p.spin.Hide()
	p.obj = container.NewStack(p.bar, p.spin)
	return p
}

// begin start een taak en geeft de voortgangs-callback ervan terug. De balk gaat
// meteen op nul: een nieuwe taak begint niet op de rest van de vorige.
func (p *prog) begin() func(done, total int64) {
	g := p.gen.Add(1)
	p.setValue(g, 0)
	return func(done, total int64) {
		if total <= 0 {
			return
		}
		v := float64(done) / float64(total)
		p.setValue(g, v)
		if done >= total {
			p.resetLater(g)
		}
	}
}

// begin2 is begin() voor tellers die int gebruiken (find.Scan telt adressen).
func (p *prog) begin2() func(done, total int) {
	cb := p.begin()
	return func(done, total int) { cb(int64(done), int64(total)) }
}

// work zet de onbepaalde balk aan: er gebeurt iets, maar niemand weet in
// hoeveel stappen. measured() zet hem weer uit — en de eerste telling doet dat
// ook vanzelf, zodat een fase die wél kan tellen zichzelf overneemt.
func (p *prog) work() {
	fyne.Do(func() {
		p.bar.Hide()
		p.spin.Show()
		p.spin.Start()
	})
}

func (p *prog) measured() { fyne.Do(p.showBar) }

func (p *prog) showBar() {
	if !p.spin.Visible() {
		return
	}
	p.spin.Stop()
	p.spin.Hide()
	p.bar.Show()
}

func (p *prog) setValue(g int64, v float64) {
	if p.gen.Load() != g {
		return // een nieuwere taak is begonnen; deze mag niets meer zeggen
	}
	fyne.Do(func() {
		if p.gen.Load() == g {
			p.showBar()
			p.bar.SetValue(v)
		}
	})
}

func (p *prog) resetLater(g int64) {
	go func() {
		time.Sleep(resetAfter)
		if p.gen.Load() != g {
			return
		}
		fyne.Do(func() {
			// Alleen leegmaken als hij nog vol staat: een taak in twee fasen
			// (schrijven, dan verifiëren) zet hem tussendoor zelf terug, en dan
			// hoort deze reset niets te doen.
			if p.gen.Load() == g && p.bar.Value >= 0.999 {
				p.bar.SetValue(0)
			}
		})
	}()
}

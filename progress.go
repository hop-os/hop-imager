// progress.go — een voortgangsbalk die van één taak is, en die zichzelf opruimt.
//
// TWEE FOUTEN DIE HIER OPGELOST WORDEN (Derek, 07-08):
//
//  1. Er was ÉÉN balk, onderaan het venster, gedeeld door alle drie de tabs. Dus
//     een flash op BURN liet CONFIGURE eruitzien alsof de config geschreven was
//     en FIND alsof de scan klaar was. Een balk hoort bij het werk dat je ziet,
//     dus staat er nu één per scherm.
//  2. Een balk die op 100% blijft staan, blijft beweren dat er iets klaar is —
//     ook een kwartier later. Na vijf seconden gaat hij terug naar nul.
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
	"fyne.io/fyne/v2/widget"
)

// resetAfter is hoe lang 100% blijft staan. Lang genoeg om te zien dat het
// klaar is, kort genoeg om niet te liegen.
const resetAfter = 5 * time.Second

type prog struct {
	bar *widget.ProgressBar
	gen atomic.Int64
}

func newProg() *prog { return &prog{bar: widget.NewProgressBar()} }

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

func (p *prog) setValue(g int64, v float64) {
	if p.gen.Load() != g {
		return // een nieuwere taak is begonnen; deze mag niets meer zeggen
	}
	fyne.Do(func() {
		if p.gen.Load() == g {
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

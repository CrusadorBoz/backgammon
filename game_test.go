package main

import (
	"math/rand"
	"testing"
)

// checker conservation: 15 a side, everywhere, always.
func assertConserved(t *testing.T, p pos, ctx string) {
	t.Helper()
	w, b := 0, 0
	for _, c := range p.pts {
		if c > 0 {
			w += c
		} else if c < 0 {
			b += -c
		}
	}
	w += p.barW + p.offW
	b += p.barB + p.offB
	if w != 15 || b != 15 {
		t.Fatalf("%s: conservation broken white=%d black=%d", ctx, w, b)
	}
}

func TestStartConserved(t *testing.T) {
	assertConserved(t, startPos(), "start")
}

// Bearing off: exact, legal over-shoot, illegal over-shoot, and the Black mirror.
func TestBearOff(t *testing.T) {
	var p pos
	p.pts[5] = 5
	p.pts[4] = 5
	p.pts[0] = 5
	if to, ok := p.destFor(White, 5, 6); !ok || to != OFF {
		t.Fatalf("exact bear-off from 6-point failed: to=%d ok=%v", to, ok)
	}
	var q pos
	q.pts[4] = 15
	if to, ok := q.destFor(White, 4, 6); !ok || to != OFF {
		t.Fatalf("over-shoot bear-off should be legal when nothing higher: to=%d ok=%v", to, ok)
	}
	var r pos
	r.pts[5] = 1
	r.pts[4] = 14
	if _, ok := r.destFor(White, 4, 6); ok {
		t.Fatalf("over-shoot bear-off must be illegal while a higher point is occupied")
	}
	var s pos
	s.pts[18] = -15
	if to, ok := s.destFor(Black, 18, 6); !ok || to != OFF {
		t.Fatalf("black exact bear-off failed: to=%d ok=%v", to, ok)
	}
}

// A checker on the bar must enter before anything else may move.
func TestBarPriority(t *testing.T) {
	p := startPos()
	p.barW = 1
	p.pts[23] = 1 // one White checker left on the board
	// die 2 enters on index 22 (open); the board checker must not be movable
	if ms := p.movesForDie(White, 2); len(ms) != 1 || ms[0].From != BAR {
		t.Fatalf("with a checker on the bar only bar entry is legal, got %v", ms)
	}
}

// Self-play many full games; assert they terminate and stay legal throughout.
func TestSelfPlayTerminates(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for game := 0; game < 200; game++ {
		g := newGameOpts("auto", "hard")
		plies := 0
		for !g.Over && plies < 1000 {
			plies++
			g.roll(rng.Intn(6)+1, rng.Intn(6)+1)
			if !g.rolled { // unplayable roll auto-passed
				continue
			}
			for _, m := range bestPlay(g.pos, g.Turn, g.Dice, g.Level, rng, g.legalNow()) {
				if !g.Move(m.From, m.To) {
					t.Fatalf("game %d: AI produced an illegal move %v->%v", game, m.From, m.To)
				}
				assertConserved(t, g.pos, "midmove")
				if g.Over {
					break
				}
			}
			if g.rolled && !g.Over && len(g.legalNow()) == 0 {
				g.endTurn()
			}
		}
		if !g.Over {
			t.Fatalf("game %d did not finish within 1000 plies (offW=%d offB=%d)", game, g.offW, g.offB)
		}
		assertConserved(t, g.pos, "end")
		if g.off(g.Winner) != 15 {
			t.Fatalf("game %d: winner has only %d borne off", game, g.off(g.Winner))
		}
	}
}

package main

import (
	"math/rand"
	"strings"
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

// In Acey Deucey a player may move a checker already in play instead of being
// forced to enter the opening reserve from the bar first — but a checker knocked
// off by a hit (non-reserve) must be re-entered before any other move.
func TestAceyBarRules(t *testing.T) {
	var p pos
	p.barW = 1
	p.resvW = 1 // opening reserve — entering is optional
	p.pts[23] = 1
	ms := p.movesForDie(White, 2)
	var hasBar, hasBoard bool
	for _, m := range ms {
		if m.From == BAR {
			hasBar = true
		}
		if m.From == 23 {
			hasBoard = true
		}
	}
	if !hasBar || !hasBoard {
		t.Fatalf("opening reserve should allow both entering and a board move, got %v", ms)
	}

	// Same bar checker, but knocked off by a hit (not reserve): must re-enter
	// before the board checker may move.
	p.resvW = 0
	if ms := p.movesForDie(White, 2); len(ms) != 1 || ms[0].From != BAR {
		t.Fatalf("a knocked-off checker must re-enter first, got %v", ms)
	}

	// A hit on a board checker pushes the bar above the reserve, locking it.
	var q pos
	q.barW = 2
	q.resvW = 2 // two in reserve
	q.pts[20] = 1
	q.pts[0] = 1 // a lone White blot in the entry zone
	// Black hits it: White's bar -> 3, reserve still 2, so the bar is locked.
	q.pts[0] = 0
	q.barW = 3
	q.clampResv(White)
	if !q.barLocked(White) {
		t.Fatalf("a knocked-off checker should lock the bar (bar=%d resv=%d)", q.barW, q.resvW)
	}
}

// A knocked-off checker that can't enter (opponent holds every entry point) is a
// shut-out: the turn passes with an explanatory note.
func TestAceyShutOut(t *testing.T) {
	g := newGameOpts("human", "hard", "acey")
	g.pos = pos{} // clear the opening reserve
	g.barW = 1    // one White checker knocked off (no reserve)
	for i := 18; i < 24; i++ {
		g.pts[i] = -2 // Black holds all six of White's entry points
	}
	g.Turn = White
	if !g.barLocked(White) {
		t.Fatal("white should be bar-locked by the knocked-off checker")
	}
	g.roll(3, 5) // neither die can enter against the closed board
	if g.Turn != Black {
		t.Fatalf("a shut-out turn should pass to Black, got turn=%d", g.Turn)
	}
	if !strings.Contains(g.Note, "shut out") {
		t.Fatalf("expected a shut-out note, got %q", g.Note)
	}
}

// Self-play many full games; assert they terminate and stay legal throughout.
func TestSelfPlayTerminates(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for game := 0; game < 200; game++ {
		g := newGameOpts("auto", "hard", "backgammon")
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

// Acey Deucey self-play: all fifteen start on the bar and a 1-2 triggers the
// bonus (play 1-2, choose doubles, roll again). Assert games stay legal,
// conserve checkers, and terminate, and that the bonus state machine runs.
func TestAceyDeuceySelfPlay(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	sawBonus := false
	for game := 0; game < 200; game++ {
		g := newGameOpts("auto", "hard", "acey")
		if g.barW != 15 || g.barB != 15 {
			t.Fatalf("acey start should put all 15 a side on the bar, got W=%d B=%d", g.barW, g.barB)
		}
		plies := 0
		for !g.Over && plies < 4000 {
			plies++
			if g.special == "acey-choose" {
				sawBonus = true
				g.chooseDoubles(bestDouble(g, rng))
				continue
			}
			if !g.rolled {
				g.roll(rng.Intn(6)+1, rng.Intn(6)+1)
			}
			if !g.rolled { // unplayable roll auto-passed (or advanced to a bonus phase)
				continue
			}
			for _, m := range bestPlay(g.pos, g.Turn, g.Dice, g.Level, rng, g.legalNow()) {
				if !g.Move(m.From, m.To) {
					t.Fatalf("acey game %d: illegal AI move %v->%v", game, m.From, m.To)
				}
				assertConserved(t, g.pos, "acey-midmove")
				if g.Over {
					break
				}
			}
			if g.rolled && !g.Over && len(g.legalNow()) == 0 {
				g.advance()
			}
		}
		if !g.Over {
			t.Fatalf("acey game %d did not finish within 4000 plies (offW=%d offB=%d)", game, g.offW, g.offB)
		}
		assertConserved(t, g.pos, "acey-end")
		if g.off(g.Winner) != 15 {
			t.Fatalf("acey game %d: winner has only %d borne off", game, g.off(g.Winner))
		}
	}
	if !sawBonus {
		t.Fatal("expected at least one acey-deucey bonus across 200 games")
	}
}

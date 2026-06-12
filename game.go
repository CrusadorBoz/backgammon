// Backgammon rules engine.
//
// Board model: 24 points held in a single [24]int. A positive count is White's
// checkers on that point, a negative count is Black's. Points are indexed 0..23
// in *absolute* board order; the two players run in opposite directions across
// the same indices:
//
//   White moves from high index toward 0 and bears off past index 0.
//           White's home board is indices 0..5 (the 1..6 points).
//           White enters from the bar onto indices 18..23.
//   Black moves from low index toward 23 and bears off past index 23.
//           Black's home board is indices 18..23 (Black's 1..6 points).
//           Black enters from the bar onto indices 0..5.
//
// This file is pure rules — no HTTP, no randomness. The server (main.go) and the
// computer player (ai.go) build on the exported Game type and the helpers here.

package main

const (
	White = 1
	Black = -1

	BAR = -1 // sentinel "from": a checker sitting on the bar
	OFF = 99 // sentinel "to": a checker borne off the board
)

// pos is a bare position: the 24 points plus each side's bar and bear-off tray.
// It is a value type (the [24]int makes copies cheap and independent), which lets
// the move-search code clone a position with a plain assignment.
type pos struct {
	pts  [24]int
	barW int
	barB int
	offW int
	offB int
}

// move is one checker hop, expressed as endpoints so the front-end never has to
// reason about dice. From == BAR means "enter from the bar"; To == OFF means
// "bear off".
type move struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// startPos returns the standard backgammon opening setup (15 checkers a side).
func startPos() pos {
	var p pos
	// White (positive):
	p.pts[23] = 2 // 24-point
	p.pts[12] = 5 // 13-point
	p.pts[7] = 3  // 8-point
	p.pts[5] = 5  // 6-point
	// Black (negative): mirror image.
	p.pts[0] = -2  // Black's 24-point
	p.pts[11] = -5 // Black's 13-point
	p.pts[16] = -3 // Black's 8-point
	p.pts[18] = -5 // Black's 6-point
	return p
}

func (p *pos) bar(pl int) int {
	if pl == White {
		return p.barW
	}
	return p.barB
}

func (p *pos) addBar(pl, n int) {
	if pl == White {
		p.barW += n
	} else {
		p.barB += n
	}
}

func (p *pos) off(pl int) int {
	if pl == White {
		return p.offW
	}
	return p.offB
}

func (p *pos) addOff(pl int) {
	if pl == White {
		p.offW++
	} else {
		p.offB++
	}
}

// owns reports whether pl has at least one checker on point i.
func (p *pos) owns(pl, i int) bool {
	if pl == White {
		return p.pts[i] > 0
	}
	return p.pts[i] < 0
}

// open reports whether pl may land on point i: empty, own, or a lone enemy blot.
func (p *pos) open(pl, i int) bool {
	if i < 0 || i > 23 {
		return false
	}
	if pl == White {
		return p.pts[i] >= -1
	}
	return p.pts[i] <= 1
}

// entryIndex is the point a bar checker lands on for a given die.
func entryIndex(pl, die int) int {
	if pl == White {
		return 24 - die // die 1->23 (24-point) ... die 6->18 (19-point)
	}
	return die - 1 // die 1->0 ... die 6->5
}

// pipToOff is the distance from point i to that player's bear-off edge.
func pipToOff(pl, i int) int {
	if pl == White {
		return i + 1
	}
	return 24 - i
}

// allHome reports whether every one of pl's checkers is in the home board (and
// none on the bar) — the precondition for bearing off.
func (p *pos) allHome(pl int) bool {
	if p.bar(pl) > 0 {
		return false
	}
	if pl == White {
		for i := 6; i < 24; i++ {
			if p.pts[i] > 0 {
				return false
			}
		}
	} else {
		for i := 0; i < 18; i++ {
			if p.pts[i] < 0 {
				return false
			}
		}
	}
	return true
}

// noHigher reports that pl has no checker on a point *farther* from the edge than
// i (within the home board). It gates over-shoot bear-offs: a die bigger than the
// exact pip may only bear a checker off the highest occupied point.
func (p *pos) noHigher(pl, i int) bool {
	if pl == White {
		for j := i + 1; j <= 5; j++ {
			if p.pts[j] > 0 {
				return false
			}
		}
	} else {
		for j := 18; j < i; j++ {
			if p.pts[j] < 0 {
				return false
			}
		}
	}
	return true
}

// destFor returns the landing point for moving from `from` with one die, and
// whether that single move is legal in this position. It enforces bar-entry
// priority, blocked points, and the bear-off rules. It does not mutate p.
func (p *pos) destFor(pl, from, die int) (int, bool) {
	if from == BAR {
		if p.bar(pl) <= 0 {
			return 0, false
		}
		to := entryIndex(pl, die)
		if p.open(pl, to) {
			return to, true
		}
		return 0, false
	}
	if p.bar(pl) > 0 { // must clear the bar before moving anything else
		return 0, false
	}
	if !p.owns(pl, from) {
		return 0, false
	}
	var dst int
	if pl == White {
		dst = from - die
	} else {
		dst = from + die
	}
	if dst >= 0 && dst <= 23 {
		if p.open(pl, dst) {
			return dst, true
		}
		return 0, false
	}
	// dst ran off the edge -> a bear-off attempt.
	if !p.allHome(pl) {
		return 0, false
	}
	pip := pipToOff(pl, from)
	if die == pip {
		return OFF, true // exact
	}
	if die > pip && p.noHigher(pl, from) {
		return OFF, true // over-shoot from the highest point
	}
	return 0, false
}

// apply performs a validated single move, mutating p, and reports whether it hit
// an enemy blot (sending it to the bar).
func (p *pos) apply(pl, from, die int) (int, bool) {
	to, ok := p.destFor(pl, from, die)
	if !ok {
		return 0, false
	}
	if from == BAR {
		p.addBar(pl, -1)
	} else {
		p.pts[from] -= pl
	}
	if to == OFF {
		p.addOff(pl)
		return to, false
	}
	hit := false
	if pl == White {
		if p.pts[to] == -1 {
			p.pts[to] = 0
			p.barB++
			hit = true
		}
		p.pts[to]++
	} else {
		if p.pts[to] == 1 {
			p.pts[to] = 0
			p.barW++
			hit = true
		}
		p.pts[to]--
	}
	return to, hit
}

// movesForDie lists every legal single move for one die value in this position.
func (p *pos) movesForDie(pl, die int) []move {
	var ms []move
	if p.bar(pl) > 0 {
		if to, ok := p.destFor(pl, BAR, die); ok {
			ms = append(ms, move{BAR, to})
		}
		return ms
	}
	for i := 0; i < 24; i++ {
		if !p.owns(pl, i) {
			continue
		}
		if to, ok := p.destFor(pl, i, die); ok {
			ms = append(ms, move{i, to})
		}
	}
	return ms
}

// maxUsable returns the greatest number of the given dice pl can legally play,
// in any order, from position p. This drives the "you must use as many dice as
// possible" rule. Depth is at most 4 (doubles), so the recursion is cheap.
func maxUsable(p pos, pl int, dice []int) int {
	if len(dice) == 0 {
		return 0
	}
	best := 0
	tried := map[int]bool{}
	for idx, d := range dice {
		if tried[d] { // distinct die values only — avoids duplicate work on doubles
			continue
		}
		tried[d] = true
		for _, m := range p.movesForDie(pl, d) {
			np := p
			np.apply(pl, m.From, d)
			if u := 1 + maxUsable(np, pl, removeAt(dice, idx)); u > best {
				best = u
			}
		}
	}
	return best
}

// ---------------------------------------------------------------------------
// Game: a position plus whose turn it is and the dice state for that turn.
// ---------------------------------------------------------------------------

type Game struct {
	pos
	Turn   int    // White or Black
	Dice   []int  // the full roll for the current turn (2, or 4 on doubles)
	Rem    []int  // dice not yet consumed this turn
	rolled bool   // have the dice been thrown for the current turn?
	maxT   int    // max dice usable this turn, measured from the turn's start
	used   int    // dice consumed so far this turn
	Over   bool   // game finished?
	Winner int    // White / Black once Over
	Mode   string // "computer", "human" (hotseat), or "auto" (watch)
	Level  string // "easy" / "medium" / "hard"
	Note   string // transient status line for the UI
	stack  []undoFrame
}

// undoFrame snapshots enough to roll back a single move within the current turn.
type undoFrame struct {
	before pos
	rem    []int
	used   int
}

func newGameOpts(mode, level string) *Game {
	if mode != "human" && mode != "auto" {
		mode = "computer"
	}
	switch level {
	case "easy", "medium", "hard":
	default:
		level = "medium"
	}
	return &Game{pos: startPos(), Turn: White, Mode: mode, Level: level}
}

// aiControls reports whether the given side is played by the computer.
func (g *Game) aiControls(pl int) bool {
	switch g.Mode {
	case "auto":
		return true
	case "computer":
		return pl == Black
	}
	return false
}

// roll sets the dice for the current turn. If no dice can be played it records a
// note and passes the turn automatically.
func (g *Game) roll(d1, d2 int) {
	if d1 == d2 {
		g.Dice = []int{d1, d1, d1, d1}
	} else {
		g.Dice = []int{d1, d2}
	}
	g.Rem = append([]int(nil), g.Dice...)
	g.used = 0
	g.rolled = true
	g.stack = nil
	g.maxT = maxUsable(g.pos, g.Turn, g.Dice)
	g.Note = ""
	if g.maxT == 0 {
		g.Note = "No legal move with that roll — turn passes."
		g.endTurn()
	}
}

// legalNow lists the moves the side to move may make right now, already filtered
// by the maximal-usage and "must play the higher die" rules.
func (g *Game) legalNow() []move {
	if !g.rolled || g.Over || len(g.Rem) == 0 {
		return nil
	}
	need := g.maxT - g.used // dice we are still obliged to play
	if need <= 0 {
		return nil
	}
	type key struct{ f, t int }
	seen := map[key]bool{}
	var out []move
	tried := map[int]bool{}
	for _, d := range g.Rem {
		if tried[d] {
			continue
		}
		tried[d] = true
		for _, m := range g.movesForDie(g.Turn, d) {
			np := g.pos
			np.apply(g.Turn, m.From, d)
			// Keep only moves that still allow the maximum number of dice.
			if 1+maxUsable(np, g.Turn, removeVal(g.Rem, d)) >= need {
				k := key{m.From, m.To}
				if !seen[k] {
					seen[k] = true
					out = append(out, m)
				}
			}
		}
	}
	// Non-double opening where only one die is playable: it must be the higher
	// one if the higher one can be played at all.
	if g.used == 0 && g.maxT == 1 && len(g.Dice) == 2 && g.Dice[0] != g.Dice[1] {
		hi := g.Dice[0]
		if g.Dice[1] > hi {
			hi = g.Dice[1]
		}
		if len(g.movesForDie(g.Turn, hi)) > 0 {
			var filtered []move
			for _, m := range out {
				if to, ok := g.destFor(g.Turn, m.From, hi); ok && to == m.To {
					filtered = append(filtered, m)
				}
			}
			out = filtered
		}
	}
	return out
}

// Move applies a player's move expressed as endpoints. It validates against
// legalNow, picks a die that realises it while preserving maximal usage, updates
// dice/turn/win state, and returns false if the move was not legal.
func (g *Game) Move(from, to int) bool {
	legal := false
	for _, m := range g.legalNow() {
		if m.From == from && m.To == to {
			legal = true
			break
		}
	}
	if !legal {
		return false
	}
	need := g.maxT - g.used
	bestDie, bestScore := -1, -1
	tried := map[int]bool{}
	for _, d := range g.Rem {
		if tried[d] {
			continue
		}
		tried[d] = true
		if td, ok := g.destFor(g.Turn, from, d); ok && td == to {
			np := g.pos
			np.apply(g.Turn, from, d)
			rest := maxUsable(np, g.Turn, removeVal(g.Rem, d))
			if 1+rest >= need && rest > bestScore {
				bestScore = rest
				bestDie = d
			}
		}
	}
	if bestDie < 0 {
		return false
	}
	g.stack = append(g.stack, undoFrame{
		before: g.pos,
		rem:    append([]int(nil), g.Rem...),
		used:   g.used,
	})
	_, hit := g.pos.apply(g.Turn, from, bestDie)
	g.Rem = removeVal(g.Rem, bestDie)
	g.used++
	if hit {
		g.Note = "Hit! A checker goes to the bar."
	} else {
		g.Note = ""
	}
	if g.off(g.Turn) == 15 {
		g.Over = true
		g.Winner = g.Turn
		g.rolled = false
		return true
	}
	if len(g.Rem) == 0 || len(g.legalNow()) == 0 {
		if len(g.Rem) > 0 && g.Note == "" {
			g.Note = "No further legal move — turn passes."
		}
		g.endTurn()
	}
	return true
}

// undo rolls back the most recent move of the current turn.
func (g *Game) undo() bool {
	if len(g.stack) == 0 {
		return false
	}
	f := g.stack[len(g.stack)-1]
	g.stack = g.stack[:len(g.stack)-1]
	g.pos = f.before
	g.Rem = f.rem
	g.used = f.used
	g.Note = "Move undone."
	return true
}

func (g *Game) canUndo() bool { return len(g.stack) > 0 && !g.Over }

func (g *Game) endTurn() {
	g.Turn = -g.Turn
	g.Dice = nil
	g.Rem = nil
	g.rolled = false
	g.used = 0
	g.maxT = 0
	g.stack = nil
}

// result classifies a finished game for display (single / gammon / backgammon).
func (g *Game) result() string {
	if !g.Over {
		return ""
	}
	loser := -g.Winner
	if g.off(loser) > 0 {
		return "Single game"
	}
	// Loser bore nothing off: gammon, or backgammon if a loser checker is still
	// on the bar or trapped in the winner's home board.
	backgammon := g.bar(loser) > 0
	if !backgammon {
		if g.Winner == White {
			for i := 0; i < 6; i++ {
				if g.pts[i] < 0 {
					backgammon = true
					break
				}
			}
		} else {
			for i := 18; i < 24; i++ {
				if g.pts[i] > 0 {
					backgammon = true
					break
				}
			}
		}
	}
	if backgammon {
		return "Backgammon!"
	}
	return "Gammon!"
}

// ---------------------------------------------------------------------------
// Small slice helpers (dice are tiny, so plain copies are fine).
// ---------------------------------------------------------------------------

// removeAt returns a copy of s without the element at index i.
func removeAt(s []int, i int) []int {
	out := make([]int, 0, len(s)-1)
	out = append(out, s[:i]...)
	out = append(out, s[i+1:]...)
	return out
}

// removeVal returns a copy of s without the first element equal to v.
func removeVal(s []int, v int) []int {
	out := make([]int, 0, len(s))
	dropped := false
	for _, x := range s {
		if !dropped && x == v {
			dropped = true
			continue
		}
		out = append(out, x)
	}
	return out
}

// Computer opponent.
//
// Strategy: enumerate every legal maximal play for the turn (all orderings that
// use the most dice), score each resulting position with a static heuristic, and
// pick the best. "easy" plays randomly, "medium" adds a little noise so it is not
// perfectly predictable, "hard" plays the straight heuristic best.

package main

import "math/rand"

// seq is one complete legal play for a turn: the ordered moves and the position
// they lead to.
type seq struct {
	moves []move
	end   pos
}

// enumerate returns every maximal play (those using maxUsable dice) from p.
// Results are de-duplicated by end position to keep the doubles case bounded.
func enumerate(p pos, pl int, dice []int) []seq {
	mx := maxUsable(p, pl, dice)
	if mx == 0 {
		return nil
	}
	var out []seq
	seen := map[pos]bool{}
	var rec func(cur pos, rem []int, acc []move, depth int)
	rec = func(cur pos, rem []int, acc []move, depth int) {
		if depth == mx {
			if !seen[cur] {
				seen[cur] = true
				cp := append([]move(nil), acc...)
				out = append(out, seq{cp, cur})
			}
			return
		}
		tried := map[int]bool{}
		for idx, d := range rem {
			if tried[d] {
				continue
			}
			tried[d] = true
			for _, m := range cur.movesForDie(pl, d) {
				np := cur
				np.apply(pl, m.From, d)
				if 1+maxUsable(np, pl, removeAt(rem, idx)) >= mx-depth {
					rec(np, removeAt(rem, idx), append(acc, m), depth+1)
				}
			}
		}
	}
	rec(p, dice, nil, 0)
	return out
}

// pipCount is the total distance pl's checkers must still travel (lower is
// better). Bar checkers count a full 25 pips.
func pipCount(p pos, pl int) int {
	tot := p.bar(pl) * 25
	for i := 0; i < 24; i++ {
		if pl == White && p.pts[i] > 0 {
			tot += pipToOff(White, i) * p.pts[i]
		} else if pl == Black && p.pts[i] < 0 {
			tot += pipToOff(Black, i) * (-p.pts[i])
		}
	}
	return tot
}

// blots counts pl's exposed lone checkers (each can be hit).
func blots(p pos, pl int) int {
	n := 0
	for i := 0; i < 24; i++ {
		if (pl == White && p.pts[i] == 1) || (pl == Black && p.pts[i] == -1) {
			n++
		}
	}
	return n
}

// madePoints counts pl's anchored points (2+ checkers); home-board points count
// double because a strong home board traps the opponent on entry.
func madePoints(p pos, pl int) int {
	n := 0
	for i := 0; i < 24; i++ {
		c := p.pts[i]
		if pl == White && c >= 2 {
			n++
			if i < 6 {
				n++
			}
		} else if pl == Black && c <= -2 {
			n++
			if i >= 18 {
				n++
			}
		}
	}
	return n
}

// evalPos scores a position from pl's point of view; higher is better for pl.
func evalPos(p pos, pl int) float64 {
	s := float64(pipCount(p, -pl) - pipCount(p, pl)) // race lead
	s += float64(p.off(pl)-p.off(-pl)) * 12          // borne off
	s -= float64(p.bar(pl)) * 18                     // own checkers stuck on the bar
	s += float64(p.bar(-pl)) * 18                    // opponent stuck on the bar
	s -= float64(blots(p, pl)) * 4                   // our exposure
	s += float64(blots(p, -pl)) * 3                  // their exposure
	s += float64(madePoints(p, pl)) * 2              // our structure
	s -= float64(madePoints(p, -pl)) * 2             // their structure
	return s
}

// bestPlay chooses the moves the computer will make for its turn. allowedFirst
// is the set of moves the rules actually permit as the turn's first move (from
// Game.legalNow) — it carries the "must play the higher die" restriction that
// raw enumeration does not, so we drop any sequence that opens with a move the
// engine would reject.
func bestPlay(p pos, pl int, dice []int, level string, rng *rand.Rand, allowedFirst []move) []move {
	seqs := enumerate(p, pl, dice)
	if len(seqs) == 0 {
		return nil
	}
	if allowedFirst != nil {
		ok := map[move]bool{}
		for _, m := range allowedFirst {
			ok[m] = true
		}
		kept := make([]seq, 0, len(seqs))
		for _, s := range seqs {
			if len(s.moves) > 0 && ok[s.moves[0]] {
				kept = append(kept, s)
			}
		}
		if len(kept) > 0 {
			seqs = kept
		}
	}
	if level == "easy" {
		return seqs[rng.Intn(len(seqs))].moves
	}
	best := seqs[0]
	bestScore := evalPos(seqs[0].end, pl)
	for _, s := range seqs[1:] {
		sc := evalPos(s.end, pl)
		if level == "medium" {
			sc += rng.Float64()*3 - 1.5 // mild noise so play isn't robotic
		}
		if sc > bestScore {
			bestScore = sc
			best = s
		}
	}
	return best.moves
}

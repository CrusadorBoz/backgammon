// Go web server for a game of backgammon.
//
// It serves a small canvas front-end plus a JSON API that drives the rules
// engine in game.go and the computer player in ai.go. Each browser session gets
// its own match, kept in memory and keyed by the bg_id cookie.
//
// Designed to sit behind the bozcode.com nginx reverse proxy at
// /goapp/backgammon/ (public — no sign-in gate). nginx strips that prefix, so
// this server sees plain paths like "/" and "/api/move".

package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	mrand "math/rand"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	defaultAddr = "127.0.0.1:8086"
	cookieName  = "bg_id"
	cookiePath  = "/goapp/backgammon/" // public path the browser sees
)

// listenAddr is the bind address, overridable via BACKGAMMON_ADDR (handy for
// smoke-testing a fresh build on a spare port while the live service runs).
func listenAddr() string {
	if a := os.Getenv("BACKGAMMON_ADDR"); a != "" {
		return a
	}
	return defaultAddr
}

//go:embed assets/index.html assets/styles.css assets/app.js assets/icon.svg
var assets embed.FS

func main() {
	st := newStore()
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/styles.css", serveAsset("assets/styles.css", "text/css; charset=utf-8"))
	mux.HandleFunc("/app.js", serveAsset("assets/app.js", "application/javascript; charset=utf-8"))
	mux.HandleFunc("/icon.svg", serveAsset("assets/icon.svg", "image/svg+xml"))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("/api/state", st.handleState)
	mux.HandleFunc("/api/new", st.handleNew)
	mux.HandleFunc("/api/roll", st.handleRoll)
	mux.HandleFunc("/api/move", st.handleMove)
	mux.HandleFunc("/api/undo", st.handleUndo)
	mux.HandleFunc("/api/acey", st.handleAcey)
	mux.HandleFunc("/api/ai", st.handleAI)

	addr := listenAddr()
	log.Printf("backgammon listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, logRequests(mux)))
}

// ---------------------------------------------------------------------------
// Session store: one match per browser, guarded by a single mutex.
// ---------------------------------------------------------------------------

type store struct {
	mu    sync.Mutex
	games map[string]*Game
	rng   *mrand.Rand
}

func newStore() *store {
	return &store{
		games: map[string]*Game{},
		rng:   mrand.New(mrand.NewSource(seed())),
	}
}

// seed pulls a non-deterministic seed from crypto/rand so games don't all share
// the same dice sequence after a restart.
func seed() int64 {
	n, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return time.Now().UnixNano()
	}
	return n.Int64()
}

// with runs fn against the caller's game under lock, creating a default
// vs-computer match the first time a session is seen.
func (s *store) with(id string, fn func(g *Game)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.games[id]
	if g == nil {
		g = newGameOpts("computer", "easy", "backgammon")
		s.games[id] = g
	}
	fn(g)
}

func (s *store) reset(id string, g *Game) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.games[id] = g
}

// d6 returns a fair die roll.
func (s *store) d6() int { return s.rng.Intn(6) + 1 }

// aiTurn plays one complete computer possession (roll + all moves) if it is the
// computer's move. An Acey Deucey 1-2 bonus — play the 1-2, choose doubles, roll
// again — is resolved within this single call, but the turn is never carried on
// past the hand-over to the other side, so "watch the computer" still alternates
// one side per /api/ai poll. Must be called with the store lock held.
func (s *store) aiTurn(g *Game) {
	if g.Over || !g.aiControls(g.Turn) {
		return
	}
	side := g.Turn
	for !g.Over && g.Turn == side && g.aiControls(side) {
		if g.special == "acey-choose" {
			g.chooseDoubles(bestDouble(g, s.rng))
			continue
		}
		if !g.rolled {
			g.roll(s.d6(), s.d6())
		}
		if g.Over || g.Turn != side { // roll may have auto-passed an unplayable turn
			break
		}
		if !g.rolled { // advanced to a non-rolled bonus phase (e.g. choose doubles)
			if g.special == "acey-choose" {
				continue
			}
			break
		}
		allowed := g.legalNow()
		for _, m := range bestPlay(g.pos, g.Turn, g.Dice, g.Level, s.rng, allowed) {
			if !g.Move(m.From, m.To) || g.Over {
				break
			}
		}
		// Safety net: never leave the computer's turn half-played. If anything
		// remains legal, finish it greedily so the turn always ends.
		for !g.Over {
			legal := g.legalNow()
			if len(legal) == 0 {
				break
			}
			if !g.Move(legal[0].From, legal[0].To) {
				break
			}
		}
		if g.rolled && !g.Over { // dice left unspent (e.g. fully blocked) — advance
			g.advance()
		}
	}
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

func (s *store) handleState(w http.ResponseWriter, r *http.Request) {
	id := sessionID(w, r)
	var out stateDTO
	s.with(id, func(g *Game) { out = dto(g) })
	writeJSON(w, out)
}

func (s *store) handleNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Opponent string `json:"opponent"`
		Level    string `json:"level"`
		Variant  string `json:"variant"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	id := sessionID(w, r)
	s.reset(id, newGameOpts(body.Opponent, body.Level, body.Variant))
	var out stateDTO
	s.with(id, func(g *Game) { out = dto(g) })
	writeJSON(w, out)
}

func (s *store) handleRoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	id := sessionID(w, r)
	var out stateDTO
	s.with(id, func(g *Game) {
		// Humans roll for their own side; the computer rolls via /api/ai. During
		// the Acey Deucey doubles-choice pause there is nothing to roll.
		if !g.Over && !g.rolled && g.special != "acey-choose" && !g.aiControls(g.Turn) {
			g.roll(s.d6(), s.d6())
		}
		out = dto(g)
	})
	writeJSON(w, out)
}

// handleAcey records the player's chosen doubles for an Acey Deucey 1-2 bonus.
func (s *store) handleAcey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Double int `json:"double"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id := sessionID(w, r)
	var out stateDTO
	s.with(id, func(g *Game) {
		if !g.Over && !g.aiControls(g.Turn) {
			g.chooseDoubles(body.Double)
		}
		out = dto(g)
	})
	writeJSON(w, out)
}

func (s *store) handleMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		From int `json:"from"`
		To   int `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id := sessionID(w, r)
	var out stateDTO
	s.with(id, func(g *Game) {
		if !g.Over && !g.aiControls(g.Turn) {
			g.Move(body.From, body.To)
		}
		out = dto(g)
	})
	writeJSON(w, out)
}

func (s *store) handleUndo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	id := sessionID(w, r)
	var out stateDTO
	s.with(id, func(g *Game) {
		if !g.aiControls(g.Turn) {
			g.undo()
		}
		out = dto(g)
	})
	writeJSON(w, out)
}

func (s *store) handleAI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	id := sessionID(w, r)
	var out stateDTO
	s.with(id, func(g *Game) {
		s.aiTurn(g)
		out = dto(g)
	})
	writeJSON(w, out)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	serveAsset("assets/index.html", "text/html; charset=utf-8")(w, r)
}

// ---------------------------------------------------------------------------
// Serialization + helpers
// ---------------------------------------------------------------------------

// stateDTO is the JSON shape the front-end consumes.
type stateDTO struct {
	Points  []int  `json:"points"`
	BarW    int    `json:"barW"`
	BarB    int    `json:"barB"`
	OffW    int    `json:"offW"`
	OffB    int    `json:"offB"`
	Turn    int    `json:"turn"`
	Dice    []int  `json:"dice"`
	Rem     []int  `json:"rem"`
	Rolled  bool   `json:"rolled"`
	Moves   []move `json:"moves"`
	Over    bool   `json:"over"`
	Winner  int    `json:"winner"`
	Result  string `json:"result,omitempty"`
	Mode    string `json:"mode"`
	Level   string `json:"level"`
	Variant string `json:"variant"`
	Phase   string `json:"phase,omitempty"`
	Note    string `json:"note,omitempty"`
	CanUndo bool   `json:"canUndo"`
	AITurn  bool   `json:"aiTurn"`
	PipW    int    `json:"pipW"`
	PipB    int    `json:"pipB"`
}

func dto(g *Game) stateDTO {
	moves := g.legalNow()
	if moves == nil {
		moves = []move{}
	}
	return stateDTO{
		Points:  append([]int(nil), g.pts[:]...),
		BarW:    g.barW,
		BarB:    g.barB,
		OffW:    g.offW,
		OffB:    g.offB,
		Turn:    g.Turn,
		Dice:    append([]int{}, g.Dice...),
		Rem:     append([]int{}, g.Rem...),
		Rolled:  g.rolled,
		Moves:   moves,
		Over:    g.Over,
		Winner:  g.Winner,
		Result:  g.result(),
		Mode:    g.Mode,
		Level:   g.Level,
		Variant: g.Variant,
		Phase:   g.special,
		Note:    g.Note,
		CanUndo: g.canUndo(),
		AITurn:  !g.Over && g.aiControls(g.Turn),
		PipW:    pipCount(g.pos, White),
		PipB:    pipCount(g.pos, Black),
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func serveAsset(key, mime string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := assets.ReadFile(key)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", mime)
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(data)
	}
}

// sessionID reads the session cookie or mints a new one (set on the response).
func sessionID(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		return c.Value
	}
	b := make([]byte, 16)
	rand.Read(b)
	id := hex.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    id,
		Path:     cookiePath,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   7 * 24 * 3600,
	})
	return id
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

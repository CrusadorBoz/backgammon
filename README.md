# Backgammon

A web-based game of backgammon written in Go. A small canvas front-end talks to a
JSON API backed by a complete rules engine — bar entry, hitting, bearing off, the
"use as many dice as you can / play the higher die" rules, and gammon /
backgammon scoring. Play against the computer (easy / medium / hard), against a
friend on the same screen, or sit back and watch the computer play itself.

**▶ Play it live: [bozcode.com/goapp/backgammon](https://bozcode.com/goapp/backgammon/)**

## Layout

| File          | Purpose                                                        |
|---------------|----------------------------------------------------------------|
| `game.go`     | Board model and rules engine (no HTTP, no randomness).         |
| `ai.go`       | Computer opponent: enumerate legal plays, score, pick best.    |
| `main.go`     | HTTP server, in-memory per-session games, JSON API.            |
| `assets/`     | `index.html`, `styles.css`, `app.js`, `icon.svg` (embedded).   |
| `game_test.go`| Conservation, bear-off, bar-priority, and self-play tests.     |

## Run locally

```sh
go build -o backgammon .
BACKGAMMON_ADDR=127.0.0.1:8086 ./backgammon
# open http://127.0.0.1:8086/   (note: the session cookie is Secure, so a
# logged-in game only persists over HTTPS / behind the nginx proxy)
```

## Test

```sh
go test ./...
```

## Deploy (bozcode.com)

It runs behind nginx at `/goapp/backgammon/` (public). To install the systemd
service, add the nginx block, and wire the Apps-menu link in one go:

```sh
sudo bash deploy/deploy-all.sh
```

The board server binds `127.0.0.1:8086`; nginx strips the `/goapp/backgammon/`
prefix before proxying.

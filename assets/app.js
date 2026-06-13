"use strict";

// ---- Canvas geometry (must match the <canvas> width/height attributes) ----
const CW = 874, CH = 556;
const M = 20, PW = 60, BW = 50, OW = 64, PH = 222;
const R = 24, SS = 38;               // checker radius, stack spacing
const barLeft = M + 6 * PW;          // 380
const barRight = barLeft + BW;       // 430
const offLeft = M + 12 * PW + BW;    // 790
const midY = CH / 2;

const WHITE_FILL = "#f1ead4", WHITE_EDGE = "#b3a877";
const BLACK_FILL = "#2a2a33", BLACK_EDGE = "#6c6c82";

const canvas = document.getElementById("board");
const ctx = canvas.getContext("2d");

let state = null;       // latest server state
let selected = null;    // currently selected source: point index, or -1 for bar
let busy = false;       // a request is in flight
let aiTimer = null;     // pending computer-turn timer
let currentVariant = "backgammon"; // "backgammon" or "acey" (Acey Deucey)

// =====================================================================
// Sound effects — synthesized with Web Audio (no audio files to ship).
// =====================================================================
let audioCtx = null;
let soundOn = localStorage.getItem("bg_sound") !== "off";

function ensureAudio() {
  if (!soundOn) return null;
  if (!audioCtx) {
    const AC = window.AudioContext || window.webkitAudioContext;
    if (!AC) return null;
    try { audioCtx = new AC(); } catch (e) { return null; }
  }
  if (audioCtx.state === "suspended") audioCtx.resume();
  return audioCtx;
}

function tone(freq, dur, type, gain, when) {
  const ac = ensureAudio(); if (!ac) return;
  const t0 = ac.currentTime + (when || 0);
  const osc = ac.createOscillator();
  const g = ac.createGain();
  osc.type = type || "triangle";
  osc.frequency.setValueAtTime(freq, t0);
  g.gain.setValueAtTime(0.0001, t0);
  g.gain.exponentialRampToValueAtTime(gain || 0.08, t0 + 0.008);
  g.gain.exponentialRampToValueAtTime(0.0001, t0 + dur);
  osc.connect(g); g.connect(ac.destination);
  osc.start(t0); osc.stop(t0 + dur + 0.02);
}

function noiseBurst(dur, gain, when) {
  const ac = ensureAudio(); if (!ac) return;
  const t0 = ac.currentTime + (when || 0);
  const n = Math.max(1, Math.floor(ac.sampleRate * dur));
  const buf = ac.createBuffer(1, n, ac.sampleRate);
  const d = buf.getChannelData(0);
  for (let i = 0; i < n; i++) d[i] = (Math.random() * 2 - 1) * (1 - i / n);
  const src = ac.createBufferSource(); src.buffer = buf;
  const bp = ac.createBiquadFilter(); bp.type = "bandpass"; bp.frequency.value = 1800; bp.Q.value = 0.7;
  const g = ac.createGain(); g.gain.value = gain || 0.12;
  src.connect(bp); bp.connect(g); g.connect(ac.destination);
  src.start(t0);
}

const sfx = {
  roll() { noiseBurst(0.13, 0.16); noiseBurst(0.12, 0.13, 0.09); noiseBurst(0.10, 0.10, 0.18); },
  move() { tone(300, 0.07, "triangle", 0.10); tone(180, 0.06, "sine", 0.05, 0.005); },
  hit() { tone(150, 0.16, "sawtooth", 0.10); noiseBurst(0.10, 0.10); },
  bearOff() { tone(540, 0.10, "triangle", 0.09); tone(810, 0.14, "triangle", 0.09, 0.08); },
  win() {
    // Rising fanfare (C-E-G-C), then a held major chord with a sparkle on top
    // and a soft shimmer swell — a fuller celebration than a plain arpeggio.
    const run = [523.25, 659.25, 783.99, 1046.5];
    run.forEach((f, i) => tone(f, 0.22, "triangle", 0.11, i * 0.10));
    const top = run.length * 0.10;
    run.forEach((f) => tone(f, 0.95, "triangle", 0.06, top));        // sustained chord
    [1318.5, 1567.98, 2093.0].forEach((f, i) => tone(f, 0.2, "sine", 0.05, top + 0.14 + i * 0.07)); // sparkle
    noiseBurst(0.55, 0.05, top);                                      // shimmer swell
  },
  select() { tone(420, 0.04, "sine", 0.045); },
};

// =====================================================================
// Win celebration — confetti burst over the board (transparent overlay).
// =====================================================================
const confettiCanvas = document.getElementById("confetti");
const confettiCtx = confettiCanvas.getContext("2d");
let confettiParticles = [];
let confettiRAF = null;
const CONFETTI_COLORS = ["#ffd166", "#ef476f", "#06d6a0", "#118ab2", "#f78c6b", "#c77dff", "#ffffff"];

// Fire a burst of paper from the two lower corners, like cannons.
function spawnConfetti(n) {
  for (let i = 0; i < n; i++) {
    const fromLeft = Math.random() < 0.5;
    confettiParticles.push({
      x: fromLeft ? CW * 0.1 : CW * 0.9,
      y: CH * 0.95,
      vx: (fromLeft ? 1 : -1) * (2.5 + Math.random() * 4.5),
      vy: -(8 + Math.random() * 6),
      g: 0.16 + Math.random() * 0.07,
      size: 5 + Math.random() * 6,
      rot: Math.random() * Math.PI,
      vrot: (Math.random() * 2 - 1) * 0.32,
      color: CONFETTI_COLORS[(Math.random() * CONFETTI_COLORS.length) | 0],
      rect: Math.random() < 0.55,
    });
  }
}

function launchConfetti() {
  spawnConfetti(110);
  setTimeout(() => spawnConfetti(80), 260);
  setTimeout(() => spawnConfetti(60), 560);
  if (!confettiRAF) confettiRAF = requestAnimationFrame(stepConfetti);
}

function stepConfetti() {
  confettiCtx.clearRect(0, 0, CW, CH);
  let alive = 0;
  for (const p of confettiParticles) {
    p.vy += p.g;
    p.vx *= 0.992;
    p.x += p.vx;
    p.y += p.vy;
    p.rot += p.vrot;
    if (p.y > CH + 24) continue; // fell off the bottom
    alive++;
    confettiCtx.save();
    confettiCtx.translate(p.x, p.y);
    confettiCtx.rotate(p.rot);
    confettiCtx.fillStyle = p.color;
    if (p.rect) confettiCtx.fillRect(-p.size / 2, -p.size / 2, p.size, p.size * 0.6);
    else { confettiCtx.beginPath(); confettiCtx.arc(0, 0, p.size / 2, 0, Math.PI * 2); confettiCtx.fill(); }
    confettiCtx.restore();
  }
  if (alive > 0) {
    confettiRAF = requestAnimationFrame(stepConfetti);
  } else {
    confettiParticles = [];
    confettiCtx.clearRect(0, 0, CW, CH);
    confettiRAF = null;
  }
}

// =====================================================================
// Match score — points (single 1 / gammon 2 / backgammon 3), persisted.
// =====================================================================
let scoreW = parseInt(localStorage.getItem("bg_scoreW") || "0", 10) || 0;
let scoreB = parseInt(localStorage.getItem("bg_scoreB") || "0", 10) || 0;

function pointsFor(result) {
  if (!result) return 1;
  if (/backgammon/i.test(result)) return 3;
  if (/gammon/i.test(result)) return 2;
  return 1;
}
function renderScore() {
  const w = document.getElementById("scoreW");
  const b = document.getElementById("scoreB");
  if (w) w.textContent = scoreW;
  if (b) b.textContent = scoreB;
}

// React to a state change with sound + scoring. `prev` is null on first load.
function reactToTransition(prev, cur) {
  if (!cur) return;
  if (cur.over && prev && !prev.over) {
    if (cur.winner > 0) scoreW += pointsFor(cur.result);
    else scoreB += pointsFor(cur.result);
    localStorage.setItem("bg_scoreW", String(scoreW));
    localStorage.setItem("bg_scoreB", String(scoreB));
    renderScore();
    sfx.win();
    launchConfetti();
    return;
  }
  if (!prev) { renderScore(); return; } // initial load: paint score, stay silent

  if (cur.rolled && !prev.rolled) sfx.roll();

  const prevOff = (prev.offW || 0) + (prev.offB || 0);
  const curOff = (cur.offW || 0) + (cur.offB || 0);
  const prevBar = (prev.barW || 0) + (prev.barB || 0);
  const curBar = (cur.barW || 0) + (cur.barB || 0);
  if (curOff > prevOff) sfx.bearOff();
  else if (curBar > prevBar) sfx.hit();
  else if (pointsChanged(prev, cur)) sfx.move();
}

function pointsChanged(a, b) {
  if (!a.points || !b.points) return false;
  for (let i = 0; i < 24; i++) if (a.points[i] !== b.points[i]) return true;
  return false;
}

// ---- left x of a board column (0..11), accounting for the central bar ----
function colLeft(col) {
  return col < 6 ? M + col * PW : barRight + (col - 6) * PW;
}

// ---- where point index i lives on screen ----
function slotInfo(i) {
  let top, col;
  if (i < 12) { top = false; col = 11 - i; }
  else { top = true; col = i - 12; }
  return { cx: colLeft(col) + PW / 2, top };
}

// =====================================================================
// API
// =====================================================================
async function api(path, body) {
  const opt = { method: body === undefined ? "GET" : "POST" };
  if (body !== undefined) {
    opt.headers = { "Content-Type": "application/json" };
    opt.body = JSON.stringify(body);
  }
  const res = await fetch(path, opt);
  return res.json();
}

async function refresh(next) {
  if (busy) return;
  busy = true;
  const prev = state;
  try { state = await next; }
  finally { busy = false; }
  selected = null;
  reactToTransition(prev, state);
  render();
  afterRender();
}

// After every state change: drive the computer, update buttons.
function afterRender() {
  if (aiTimer) { clearTimeout(aiTimer); aiTimer = null; }
  const undoBtn = document.getElementById("undo");
  const choosingDoubles = !!(state && state.phase === "acey-choose");
  const canRoll = state && !state.over && !state.rolled && !state.aiTurn && !choosingDoubles;
  undoBtn.disabled = !state || !state.canUndo;

  // Acey Deucey: after a 1-2 the human picks any doubles before play continues.
  document.getElementById("aceyChoose").hidden =
    !(choosingDoubles && !state.aiTurn && !state.over);

  // Big centered blinking Roll button — only on a human's turn (never the computer).
  const rollCenter = document.getElementById("rollCenter");
  if (canRoll) {
    const side = state.turn > 0 ? "White" : "Black";
    rollCenter.textContent = `${side} — Roll`;
    rollCenter.classList.add("show");
  } else {
    rollCenter.classList.remove("show");
  }

  if (state && state.aiTurn && !state.over) {
    if (!state.rolled) {
      // Computer is about to roll: play the sound, then roll and show the dice.
      aiTimer = setTimeout(() => { sfx.roll(); refresh(api("api/ai", {})); }, 650);
    } else {
      // The computer's dice are now on the board — pause long enough to read
      // them (the Roll button stays hidden on the computer's turn) before it
      // plays them.
      aiTimer = setTimeout(() => { refresh(api("api/ai", {})); }, 1300);
    }
  }
}

// =====================================================================
// Rendering
// =====================================================================
function render() {
  ctx.clearRect(0, 0, CW, CH);
  if (!state) return;
  drawWood();
  drawTrays();
  drawPoints();
  drawHighlights();
  drawCheckers();
  drawBar();
  drawOff();
  drawDice();
  drawStatus();
}

// Wooden board: walnut field with faint grain and a framed border.
function drawWood() {
  const g = ctx.createLinearGradient(0, 0, 0, CH);
  g.addColorStop(0, "#f6ecd4");
  g.addColorStop(0.5, "#efe2c4");
  g.addColorStop(1, "#e6d6b2");
  ctx.fillStyle = g;
  ctx.fillRect(0, 0, CW, CH);

  // outer frame
  ctx.strokeStyle = "#2e1d0e";
  ctx.lineWidth = 10;
  ctx.strokeRect(5, 5, CW - 10, CH - 10);
  ctx.strokeStyle = "rgba(150,100,55,0.5)";
  ctx.lineWidth = 2;
  ctx.strokeRect(11, 11, CW - 22, CH - 22);
}

// Vertical wood gradient used for the bar and the bear-off tray.
function woodTray(x, y, w, h) {
  const g = ctx.createLinearGradient(x, y, x, y + h);
  g.addColorStop(0, "#c8975a");
  g.addColorStop(0.5, "#ad7943");
  g.addColorStop(1, "#946035");
  ctx.fillStyle = g;
  ctx.fillRect(x, y, w, h);
  ctx.strokeStyle = "#cf9c64";
  ctx.lineWidth = 2;
  ctx.strokeRect(x, y, w, h);
}

function drawTrays() {
  woodTray(barLeft, M, BW, CH - 2 * M);   // central bar
  woodTray(offLeft, M, OW, CH - 2 * M);   // bear-off tray
}

function drawPoints() {
  for (let i = 0; i < 24; i++) {
    const { cx, top } = slotInfo(i);
    const baseY = top ? M : CH - M;
    const tipY = top ? M + PH : CH - M - PH;
    const light = (i % 2 === 0);
    const g = ctx.createLinearGradient(0, baseY, 0, tipY);
    if (light) { g.addColorStop(0, "#fbecca"); g.addColorStop(1, "#ecc88e"); }
    else { g.addColorStop(0, "#d6924a"); g.addColorStop(1, "#b5703a"); }
    ctx.beginPath();
    ctx.moveTo(cx - PW / 2 + 4, baseY);
    ctx.lineTo(cx + PW / 2 - 4, baseY);
    ctx.lineTo(cx, tipY);
    ctx.closePath();
    ctx.fillStyle = g;
    ctx.globalAlpha = 0.96;
    ctx.fill();
    ctx.globalAlpha = 1;
    ctx.strokeStyle = "rgba(40,22,8,0.35)";
    ctx.lineWidth = 1;
    ctx.stroke();
  }
}

function checkerCenterY(i, k, top) {
  return top ? (M + R + k * SS) : (CH - M - R - k * SS);
}

function drawCheckers() {
  for (let i = 0; i < 24; i++) {
    const c = state.points[i];
    if (c === 0) continue;
    const white = c > 0;
    const n = Math.abs(c);
    const { cx, top } = slotInfo(i);
    const shown = Math.min(n, 5);
    for (let k = 0; k < shown; k++) {
      drawChecker(cx, checkerCenterY(i, k, top), white);
    }
    if (n > 5) {
      labelCount(cx, checkerCenterY(i, 4, top), n, white);
    }
  }
}

function drawChecker(x, y, white) {
  ctx.beginPath();
  ctx.arc(x, y, R, 0, Math.PI * 2);
  ctx.fillStyle = white ? WHITE_FILL : BLACK_FILL;
  ctx.fill();
  ctx.lineWidth = 2;
  ctx.strokeStyle = white ? WHITE_EDGE : BLACK_EDGE;
  ctx.stroke();
  // inner ring for a little depth
  ctx.beginPath();
  ctx.arc(x, y, R - 6, 0, Math.PI * 2);
  ctx.strokeStyle = white ? "rgba(0,0,0,0.10)" : "rgba(255,255,255,0.10)";
  ctx.lineWidth = 1.5;
  ctx.stroke();
}

function labelCount(x, y, n, white) {
  ctx.fillStyle = white ? "#3a3422" : "#e8ecf6";
  ctx.font = "bold 18px sans-serif";
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  ctx.fillText(String(n), x, y);
}

function drawBar() {
  drawStack(barLeft + BW / 2, state.barW, true, midY + 30, +1);
  drawStack(barLeft + BW / 2, state.barB, false, midY - 30, -1);
}

function drawStack(x, n, white, startY, dir) {
  const shown = Math.min(n, 4);
  for (let k = 0; k < shown; k++) {
    drawChecker(x, startY + dir * (R + k * SS), white);
  }
  if (n > 4) labelCount(x, startY + dir * (R + 3 * SS), n, white);
}

function drawOff() {
  const x = offLeft + OW / 2;
  for (let k = 0; k < state.offW; k++) slab(x, CH - M - 12 - k * 16, true);
  for (let k = 0; k < state.offB; k++) slab(x, M + 4 + k * 16, false);
}

function slab(cx, y, white) {
  const w = OW - 18, h = 11;
  ctx.fillStyle = white ? WHITE_FILL : BLACK_FILL;
  ctx.strokeStyle = white ? WHITE_EDGE : BLACK_EDGE;
  ctx.lineWidth = 1.5;
  roundRect(cx - w / 2, y, w, h, 4);
  ctx.fill();
  ctx.stroke();
}

function roundRect(x, y, w, h, r) {
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.arcTo(x + w, y, x + w, y + h, r);
  ctx.arcTo(x + w, y + h, x, y + h, r);
  ctx.arcTo(x, y + h, x, y, r);
  ctx.arcTo(x, y, x + w, y, r);
  ctx.closePath();
}

// Highlight the selected checker and its legal destinations.
function drawHighlights() {
  if (!state.moves) return;
  // legal source markers (subtle) when nothing is selected yet
  if (selected === null && humanToMove()) {
    const froms = new Set(state.moves.map(m => m.from));
    froms.forEach(f => ringSource(f, "rgba(79,140,255,0.55)"));
    return;
  }
  if (selected === null) return;
  ringSource(selected, "#ffd166");
  const dests = state.moves.filter(m => m.from === selected).map(m => m.to);
  dests.forEach(t => {
    if (t === 99) markOff();
    else markDest(t);
  });
}

function ringSource(from, color) {
  let x, y;
  if (from === -1) {
    x = barLeft + BW / 2;
    const white = state.turn > 0;
    y = white ? midY + 30 + R : midY - 30 - R;
  } else {
    const { cx, top } = slotInfo(from);
    const n = Math.abs(state.points[from]);
    const k = Math.min(n, 5) - 1;
    x = cx; y = checkerCenterY(from, Math.max(k, 0), top);
  }
  ctx.beginPath();
  ctx.arc(x, y, R + 4, 0, Math.PI * 2);
  ctx.strokeStyle = color;
  ctx.lineWidth = 3;
  ctx.stroke();
}

function markDest(i) {
  const { cx, top } = slotInfo(i);
  const tipY = top ? M + PH - 26 : CH - M - PH + 26;
  ctx.beginPath();
  ctx.arc(cx, tipY, 11, 0, Math.PI * 2);
  ctx.fillStyle = "rgba(54,192,122,0.85)";
  ctx.fill();
  ctx.strokeStyle = "#ffffff";
  ctx.lineWidth = 2;
  ctx.stroke();
}

function markOff() {
  ctx.save();
  ctx.strokeStyle = "#36c07a";
  ctx.lineWidth = 3;
  ctx.setLineDash([6, 4]);
  ctx.strokeRect(offLeft + 2, M + 2, OW - 4, CH - 2 * M - 4);
  ctx.restore();
}

// ---- dice in the central band ----
function drawDice() {
  if (!state.rolled || !state.dice || state.dice.length === 0) return;
  // figure out which dice are still unused
  const rem = state.rem ? state.rem.slice() : [];
  const dice = state.dice;
  const size = 44, gap = 14;
  const total = dice.length * size + (dice.length - 1) * gap;
  let x = (CW - OW - total) / 2 + (M / 2);
  const y = midY - size / 2;
  for (const d of dice) {
    const idx = rem.indexOf(d);
    const used = idx === -1;
    if (!used) rem.splice(idx, 1);
    drawDie(x, y, size, d, used);
    x += size + gap;
  }
}

function drawDie(x, y, s, val, used) {
  ctx.save();
  ctx.globalAlpha = used ? 0.32 : 1;
  ctx.fillStyle = "#f4f4f8";
  ctx.strokeStyle = "#cfd3e0";
  roundRect(x, y, s, s, 9);
  ctx.fill();
  ctx.lineWidth = 1.5;
  ctx.stroke();
  ctx.fillStyle = "#1a1a22";
  const p = 0.27, m = 0.5, q = 0.73; // pip positions as fractions
  const pts = {
    1: [[m, m]],
    2: [[p, p], [q, q]],
    3: [[p, p], [m, m], [q, q]],
    4: [[p, p], [q, p], [p, q], [q, q]],
    5: [[p, p], [q, p], [m, m], [p, q], [q, q]],
    6: [[p, p], [q, p], [p, m], [q, m], [p, q], [q, q]],
  }[val] || [];
  for (const [fx, fy] of pts) {
    ctx.beginPath();
    ctx.arc(x + fx * s, y + fy * s, s * 0.075, 0, Math.PI * 2);
    ctx.fill();
  }
  ctx.restore();
}

function drawStatus() {
  const turnEl = document.getElementById("turn");
  const noteEl = document.getElementById("note");
  document.getElementById("pipW").textContent = state.pipW;
  document.getElementById("pipB").textContent = state.pipB;

  const winBtn = document.getElementById("winBtn");
  if (state.over) {
    const who = state.winner > 0 ? "White" : "Black";
    const result = state.result ? ` — ${state.result}` : "";
    turnEl.textContent = "";
    noteEl.textContent = "";
    noteEl.className = "note";
    winBtn.textContent = `${who} wins${result} · New game`;
    winBtn.classList.add("show");
    return;
  }
  winBtn.classList.remove("show");
  const side = state.turn > 0 ? "White" : "Black";
  let who = side;
  if (state.aiTurn) who = `${side} (computer)`;
  else if (state.mode === "human") who = `${side} to move`;
  turnEl.textContent = state.rolled ? `${who} — play your dice` : `${who} — roll`;
  noteEl.className = "note";
  if (state.aiTurn) {
    // Keep a shut-out explanation visible even as control passes to the computer.
    noteEl.textContent = /shut out/i.test(state.note || "") ? state.note : "Computer is thinking…";
  } else {
    noteEl.textContent = state.note || "";
  }
}

function humanToMove() {
  return state && state.rolled && !state.over && !state.aiTurn;
}

// =====================================================================
// Input
// =====================================================================
function locate(ev) {
  const rect = canvas.getBoundingClientRect();
  const sx = CW / rect.width, sy = CH / rect.height;
  const x = (ev.clientX - rect.left) * sx;
  const y = (ev.clientY - rect.top) * sy;
  if (x >= offLeft && x <= offLeft + OW) return { off: true };
  if (x >= barLeft && x <= barRight) return { bar: true };
  let col;
  if (x >= M && x < barLeft) col = Math.floor((x - M) / PW);
  else if (x > barRight && x < offLeft) col = 6 + Math.floor((x - barRight) / PW);
  else return null;
  if (col < 0 || col > 11) return null;
  const top = y < midY;
  return { idx: top ? col + 12 : 11 - col };
}

function destsFor(from) {
  return state.moves.filter(m => m.from === from).map(m => m.to);
}

canvas.addEventListener("click", (ev) => {
  if (!humanToMove() || busy) return;
  const hit = locate(ev);
  if (!hit) { selected = null; render(); return; }
  const froms = new Set(state.moves.map(m => m.from));

  if (hit.off) {
    if (selected !== null && destsFor(selected).includes(99)) {
      doMove(selected, 99);
    }
    return;
  }
  if (hit.bar) {
    if (froms.has(-1)) { selected = -1; sfx.select(); render(); }
    else { selected = null; render(); }
    return;
  }
  const i = hit.idx;
  if (selected !== null && destsFor(selected).includes(i)) {
    doMove(selected, i);
    return;
  }
  if (froms.has(i)) { selected = i; sfx.select(); render(); }
  else { selected = null; render(); }
});

function doMove(from, to) {
  refresh(api("api/move", { from, to }));
}

// =====================================================================
// Controls
// =====================================================================
document.getElementById("rollCenter").addEventListener("click", () => {
  if (!busy) refresh(api("api/roll", {}));
});
document.getElementById("undo").addEventListener("click", () => {
  if (!busy) refresh(api("api/undo", {}));
});
function startNewGame() {
  const opponent = document.getElementById("opponent").value;
  const level = document.getElementById("level").value;
  refresh(api("api/new", { opponent, level, variant: currentVariant }));
}
document.getElementById("new").addEventListener("click", startNewGame);
document.getElementById("winBtn").addEventListener("click", startNewGame);

// ---- Hamburger game-picker: Backgammon vs Acey Deucey ----
const menuBtn = document.getElementById("menuBtn");
const menuList = document.getElementById("menuList");
function openMenu(open) {
  menuList.hidden = !open;
  menuBtn.setAttribute("aria-expanded", open ? "true" : "false");
}
menuBtn.addEventListener("click", (e) => { e.stopPropagation(); openMenu(menuList.hidden); });
menuList.addEventListener("click", (e) => e.stopPropagation());
document.addEventListener("click", () => openMenu(false));
document.querySelectorAll(".menu-item").forEach((btn) => {
  btn.addEventListener("click", () => {
    currentVariant = btn.dataset.variant;
    openMenu(false);
    startNewGame();
  });
});

// ---- Acey Deucey doubles chooser ----
document.querySelectorAll("#aceyChoose .acey-btns button").forEach((btn) => {
  btn.addEventListener("click", () => {
    if (!busy) refresh(api("api/acey", { double: parseInt(btn.dataset.d, 10) }));
  });
});

// Reflect server-side variant/mode/level into the UI after each load.
function syncControls() {
  if (!state) return;
  document.getElementById("opponent").value = state.mode;
  document.getElementById("level").value = state.level;
  currentVariant = state.variant || currentVariant;
  const name = currentVariant === "acey" ? "Acey Deucey" : "Backgammon";
  document.getElementById("gameTitle").textContent = name;
  document.getElementById("menuCurrent").textContent = name;
  document.title = `${name} — play online vs the computer | bozcode.com`;
  document.querySelectorAll(".rules-body").forEach((el) => {
    el.hidden = el.dataset.variant !== currentVariant;
  });
  document.querySelectorAll(".menu-item").forEach((el) => {
    el.classList.toggle("active", el.dataset.variant === currentVariant);
  });
}
const _render = render;
render = function () { _render(); syncControls(); };

// sound toggle + score reset
const soundEl = document.getElementById("sound");
soundEl.checked = soundOn;
soundEl.addEventListener("change", () => {
  soundOn = soundEl.checked;
  localStorage.setItem("bg_sound", soundOn ? "on" : "off");
  if (soundOn) { ensureAudio(); sfx.select(); }
});
document.getElementById("resetScore").addEventListener("click", () => {
  scoreW = 0; scoreB = 0;
  localStorage.setItem("bg_scoreW", "0");
  localStorage.setItem("bg_scoreB", "0");
  renderScore();
});
renderScore();

// kick things off
refresh(api("api/state"));

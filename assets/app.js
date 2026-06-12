"use strict";

// ---- Canvas geometry (must match the <canvas> width/height attributes) ----
const CW = 874, CH = 556;
const M = 20, PW = 60, BW = 50, OW = 64, PH = 222;
const R = 24, SS = 38;               // checker radius, stack spacing
const barLeft = M + 6 * PW;          // 380
const barRight = barLeft + BW;       // 430
const offLeft = M + 12 * PW + BW;    // 790
const midY = CH / 2;

const POINT_LIGHT = "#caa46a";
const POINT_DARK = "#8a5a34";
const WHITE_FILL = "#f1ead4", WHITE_EDGE = "#b3a877";
const BLACK_FILL = "#2a2a33", BLACK_EDGE = "#6c6c82";

const canvas = document.getElementById("board");
const ctx = canvas.getContext("2d");

let state = null;       // latest server state
let selected = null;    // currently selected source: point index, or -1 for bar
let busy = false;       // a request is in flight
let aiTimer = null;     // pending computer-turn timer

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
  try { state = await next; }
  finally { busy = false; }
  selected = null;
  render();
  afterRender();
}

// After every state change: drive the computer, update buttons.
function afterRender() {
  if (aiTimer) { clearTimeout(aiTimer); aiTimer = null; }
  const rollBtn = document.getElementById("roll");
  const undoBtn = document.getElementById("undo");
  const canRoll = state && !state.over && !state.rolled && !state.aiTurn;
  rollBtn.disabled = !canRoll;
  rollBtn.classList.toggle("glow", !!canRoll);
  undoBtn.disabled = !state || !state.canUndo;

  if (state && state.aiTurn && !state.over) {
    aiTimer = setTimeout(() => { refresh(api("api/ai", {})); }, 650);
  }
}

// =====================================================================
// Rendering
// =====================================================================
function render() {
  ctx.clearRect(0, 0, CW, CH);
  if (!state) return;
  drawTrays();
  drawPoints();
  drawHighlights();
  drawCheckers();
  drawBar();
  drawOff();
  drawDice();
  drawStatus();
}

function drawTrays() {
  // central bar
  ctx.fillStyle = "#0e1426";
  ctx.fillRect(barLeft, M, BW, CH - 2 * M);
  // bear-off tray
  ctx.fillStyle = "#0e1426";
  ctx.fillRect(offLeft, M, OW, CH - 2 * M);
  ctx.strokeStyle = "#2a3457";
  ctx.lineWidth = 1;
  ctx.strokeRect(offLeft, M, OW, CH - 2 * M);
}

function drawPoints() {
  for (let i = 0; i < 24; i++) {
    const { cx, top } = slotInfo(i);
    const baseY = top ? M : CH - M;
    const tipY = top ? M + PH : CH - M - PH;
    ctx.beginPath();
    ctx.moveTo(cx - PW / 2 + 4, baseY);
    ctx.lineTo(cx + PW / 2 - 4, baseY);
    ctx.lineTo(cx, tipY);
    ctx.closePath();
    ctx.fillStyle = (i % 2 === 0) ? POINT_LIGHT : POINT_DARK;
    ctx.globalAlpha = 0.92;
    ctx.fill();
    ctx.globalAlpha = 1;
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

  if (state.over) {
    const who = state.winner > 0 ? "White" : "Black";
    turnEl.textContent = "Game over";
    noteEl.textContent = `${who} wins — ${state.result || ""}`.trim();
    noteEl.className = "note win";
    return;
  }
  const side = state.turn > 0 ? "White" : "Black";
  let who = side;
  if (state.aiTurn) who = `${side} (computer)`;
  else if (state.mode === "human") who = `${side} to move`;
  turnEl.textContent = state.rolled ? `${who} — play your dice` : `${who} — roll`;
  noteEl.className = "note";
  if (state.aiTurn) noteEl.textContent = "Computer is thinking…";
  else noteEl.textContent = state.note || "";
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
    if (froms.has(-1)) { selected = -1; render(); }
    else { selected = null; render(); }
    return;
  }
  const i = hit.idx;
  if (selected !== null && destsFor(selected).includes(i)) {
    doMove(selected, i);
    return;
  }
  if (froms.has(i)) { selected = i; render(); }
  else { selected = null; render(); }
});

function doMove(from, to) {
  refresh(api("api/move", { from, to }));
}

// =====================================================================
// Controls
// =====================================================================
document.getElementById("roll").addEventListener("click", () => {
  if (!busy) refresh(api("api/roll", {}));
});
document.getElementById("undo").addEventListener("click", () => {
  if (!busy) refresh(api("api/undo", {}));
});
document.getElementById("new").addEventListener("click", () => {
  const opponent = document.getElementById("opponent").value;
  const level = document.getElementById("level").value;
  refresh(api("api/new", { opponent, level }));
});

// Reflect server-side mode/level into the selectors after each load.
function syncControls() {
  if (!state) return;
  document.getElementById("opponent").value = state.mode;
  document.getElementById("level").value = state.level;
}
const _render = render;
render = function () { _render(); syncControls(); };

// kick things off
refresh(api("api/state"));

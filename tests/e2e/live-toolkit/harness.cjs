// tk-harness — the toolkit SSH TUI e2e driver (live 2222 session).
//
// The owner reported "the toolkit is still unusable — the controls don't work
// at all." This harness drives the TUI exactly the way an operator types:
// real ssh -tt sessions, human-paced key sends, raw-byte capture, and
// per-action delta assertions (each control must VISIBLY change the frame).
//
// Conventions (the AG .cjs style): plain JS, harness require, async main,
// process.exit(0) on success.
"use strict";
const { spawn } = require("child_process");

const KEY = "/data_1/test_x/opt/ollitex/ssh/id_ed25519";
const USER = "ollitex";
const HOST = "127.0.0.1";
const PORT = "2222";

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// ---- key encodings (the ssh terminal contract) ------------------------------
const K = {
  enter: "\r",
  esc: "\x1b",
  tab: "\t",
  space: " ",
  backspace: "\x7f",
  ctrlC: "\x03",
  ctrlZ: "\x1a",
  up: "\x1b[A",
  down: "\x1b[B",
  right: "\x1b[C",
  left: "\x1b[D",
  pgup: "\x1b[5~",
  pgdn: "\x1b[6~",
  home: "\x1b[H",
  end: "\x1b[F",
  f1: "\x1b[OP",
  f2: "\x1b[OQ",
  f5: "\x1b[15~",
  f9: "\x1b[20~",
  f10: "\x1b[21~",
  del: "\x1b[3~",
  ins: "\x1b[2~",
};
function keyenc(k) {
  if (typeof k === "string" && Object.prototype.hasOwnProperty.call(K, k)) return K[k];
  if (typeof k === "string" && k.length === 1) return k;
  throw new Error("unknown key: " + k);
}

class SSHSession {
  constructor(command) {
    this.command = command; // string or [] args (bare boot = [])
    this.rawChunks = [];
    this.closed = false;
  }
  spawn() {
    const args = ["-tt", "-i", KEY, "-p", PORT, "-o", "BatchMode=yes",
      "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
      "-o", "NumberOfPasswordPrompts=0", USER + "@" + HOST];
    if (Array.isArray(this.command) && this.command.length) args.push(...this.command);
    const cp = spawn("ssh", args, { stdio: ["pipe", "pipe", "pipe"] });
    this.rawChunks.push(Buffer.from("SSH-START"));
    cp.stdout.on("data", (d) => this.rawChunks.push(d));
    cp.stderr.on("data", (d) => this.rawChunks.push(d));
    cp.on("close", (code) => {
      this.closed = true;
      this.exitCode = code;
      if (this.onClose) this.onClose(code);
    });
    this.cp = cp;
    return this;
  }
  send(text, ms = 300) {
    cp_write(this, text);
    return sleep(ms);
  }
  // SGR mouse coordinate (1-based): btn: 0=left press handled by pair()
  mousePress(col, row) {
    return this.send("\x1b[<" + 0 + ";" + col + ";" + row + "M", 250);
  }
  mouseRelease(col, row) {
    return this.send("\x1b[<" + 0 + ";" + col + ";" + row + "m", 250);
  }
  mouseClick(col, row, settleMs = 500) {
    return Promise.resolve()
      .then(() => this.send("\x1b[<0;" + col + ";" + row + "M", 150))
      .then(() => this.send("\x1b[<0;" + col + ";" + row + "m", settleMs));
  }
  // one key at a time with human pacing
  async type(seq, paceMs = 900) {
    for (const k of seq) {
      const e = Object.prototype.hasOwnProperty.call(K, k) ? keyenc(k) : k;
      await this.send(e, paceMs);
    }
  }
  raw() {
    return Buffer.concat(this.rawChunks).toString("utf8");
  }
  strip() {
    return stripAnsi(this.raw());
  }
  kill() {
    this.cp.kill("SIGKILL");
  }
}

function cp_write(sess, text) {
  if (!sess.cp || sess.closed) return;
  try { sess.cp.stdin.write(text); } catch { /* closed */ }
}

function stripAnsi(s) {
  return s
    .replace(/\x1b\[[0-9;?]*[A-Za-z]/g, "")   // CSI (cursor addressing, modes, mouse)
    .replace(/\x1b\][^\x07\x1b]*(\x07|\x1b\\)?/g, "") // OSC
    .replace(/\x1b[()][0-9A-B]/g, "")            // charset
    .replace(/\x1b[=>/78]/g, "")                 // misc 2-char
    .replace(/[^\x20-\x7e\n\t]/g, "");
}

let PASS = 0;
let FAIL = 0;
function record(name, ok, extra) {
  if (ok) PASS++; else FAIL++;
  console.log((ok ? "PASS" : "FAIL") + "  " + name + (extra ? "  :: " + extra : ""));
}
function report() {
  console.log("");
  console.log("TK-E2E: " + PASS + " PASS / " + FAIL + " FAIL");
}
function failed() {
  return FAIL > 0;
}
// waitUntil: poll the session's stripped stream for a predicate (budget ms)
async function waitUntil(sess, pred, budgetMs = 15000, msg = "condition") {
  const t0 = Date.now();
  for (;;) {
    if (pred(sess.strip())) return true;
    if (Date.now() - t0 > budgetMs) return false;
    if (sess.closed) return pred(sess.strip());
    await sleep(250);
  }
}


// ---- session helpers -----------------------------------------------------------

// pollFor(sess, needle, budgetMs) — keep reading the captured stream until the
// needle appears (or the budget is up). Returns the needle string (truthy) or
// "". This replaces fixed settle sleeps in places where a CONTENDED server
// (previous test's in-flight compose/pull job on the same CPU) can push a
// key-to-paint delta past a single fixed window.
async function pollFor(s, needle, budgetMs = 6000, tickMs = 300) {
  const t0 = Date.now();
  for (;;) {
    if (s.closed) return "";
    if (s.strip().includes(needle)) return needle;
    if (Date.now() - t0 > budgetMs) return "";
    await sleep(tickMs);
  }
}
module.exports = { SSHSession, K, sleep, record, report, failed, waitUntil, keyenc, pollFor };

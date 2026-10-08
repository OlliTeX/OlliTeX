// tk3-stress — INTENSIVE / human-realistic load:
//  H1  long human-paced key sequence (2.0 s per key, the owner's rhythm)
//  H2  fast burst navigation (40 ms pacing)
//  H3  terminal RESIZE: ssh from a 200x50 pty → the TUI MUST re-render wide
//      (proves the client window size drives the layout, not a fixed 80x24)
//  H4  two concurrent TUI sessions, both responsive
//  H5  long idle (60 s) then late keys — input loop still alive
"use strict";
const { SSHSession, K, sleep, record, report, waitUntil, keyenc, failed } = require("./harness.cjs");
const { spawn } = require("child_process");

async function boot(cmd, budget = 20000) {
  const s = new SSHSession(cmd || []).spawn();
  const up = await waitUntil(s, (t) => t.includes("S C R E E N S"), budget, "boot");
  return { s, up };
}

function maxLineLen(text) {
  let m = 0;
  for (const l of text.split("\n")) m = Math.max(m, l.length);
  return m;
}

async function main() {
  // ---- H1: human-paced marathon (2.0 s per key) ------------------------------
  {
    const { s } = await boot([]);
    const seq = ["j", "j", "k", "5", "1", "f10", "esc", "4", "tab", "j", "tab", "2", "p", "1", "q", "n", "j"];
    let alive = true;
    for (const name of seq) {
      await s.send(keyenc(name), 2000);
      if (s.closed) { alive = false; break; }
    }
    const ok = await waitUntil(s, (t) => t.includes("STACK"), 8000);
    record("H1 human-paced marathon 17 keys @2s (session never dies)", alive && ok, "final pane Stack reached, session alive");
    s.kill(); await sleep(300);
  }

  // ---- H2: burst navigation (40 ms between keys) ------------------------------
  {
    const { s } = await boot([]);
    for (const d of ["2", "3", "4", "5", "6", "7", "8", "9", "2", "3"]) {
      await s.send(d, 40);
    }
    const ok = await waitUntil(s, (t) => t.includes("LOGS"), 8000);
    record("H2 burst 10 jumps @40ms lands on Logs", ok && !s.closed, "10 rapid digit jumps ok");
    s.kill(); await sleep(300);
  }

  // ---- H3: RESIZE propagation (200x50 client pty) ------------------------------
  {
    // ssh inside a `script` pty forced to 200x50 → the ssh client negotiates
    // that window → the toolkit TUI must render at width ~200.
    const child = spawn("script", ["-qec",
      "stty rows 50 cols 200 2>/dev/null; exec ssh -tt -i /data_1/test_x/opt/ollitex/ssh/id_ed25519 -p 2222 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o BatchMode=yes ollitex@127.0.0.1",
      "/dev/null"], { stdio: ["pipe", "pipe", "pipe"] });
    let raw = "";
    child.stdout.on("data", (d) => { raw += d.toString(); });
    child.stderr.on("data", (d) => { raw += d.toString(); });
    await sleep(7000);
    const booted = raw.includes("S C R E E N S");
    // the wide render leaves traces: tcell cursor-move sequences with column
    // targets beyond 80, or plain-text runs longer than 80 after strip.
    const wideEscape = /\x1b\[[0-9;]*\d{3,3}\d{2}/.test(raw) || /\x1b\[[1-9]\d{2};/.test(raw);
    const wideText = maxLineLen(raw.replace(/\x1b\[[0-9;?]*[A-Za-z]/g, "").replace(/\x1b\][^\x07\x1b]*/g, "")) > 100;
    record("H3 TUI renders wide for a 200x50 client (resize honored)", booted && (wideEscape || wideText),
      "booted=" + booted + " wideEscape=" + wideEscape + " wideText=" + wideText);
    child.kill("SIGKILL");
    await sleep(300);
  }

  // ---- H4: two concurrent sessions --------------------------------------------
  {
    const a = new SSHSession([]).spawn();
    const b = new SSHSession([]).spawn();
    const aUp = await waitUntil(a, (t) => t.includes("S C R E E N S"), 20000);
    const bUp = await waitUntil(b, (t) => t.includes("S C R E E N S"), 20000);
    const ba = a.strip().length;
    const bb = b.strip().length;
    await a.send("j", 600);
    await b.send("7", 600);
    await a.send("1", 600); // back to dashboard keeps it stable
    const aOk = a.strip().slice(ba).includes("STACK");
    const bOk = b.strip().slice(bb).includes("DOCTOR");
    record("H4 two concurrent TUI sessions both responsive", aUp && bUp && aOk && bOk
      && !a.closed && !b.closed, "A→Stack, B→Doctor, both alive");
    a.kill(); b.kill(); await sleep(400);
  }

  // ---- H5: long idle then late keys ---------------------------------------------
  {
    const { s } = await boot([]);
    await sleep(60000); // 60 s idle — the owner's pause-while-reading rhythm
    const b = s.strip().length;
    await s.send("j", 1500);
    const d = s.strip().slice(b);
    record("H5 60 s idle then 'j' still responds", !s.closed && d.includes("STACK"), "late key handled after idle");
    s.kill(); await sleep(300);
  }

  report();
}

main()
  .then(() => process.exit(failed() ? 1 : 0))
  .catch((e) => {
    console.error("FATAL", e && e.stack ? e.stack : e);
    report();
    process.exit(2);
  });

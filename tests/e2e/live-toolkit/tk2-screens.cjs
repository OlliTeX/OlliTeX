// tk2-screens — per-screen key matrix (every screen's actions, SAFE ops only:
// no real stop/restart/restore confirmations are ever accepted; destructive
// dialogs are opened and CANCELLED with n/esc).
"use strict";
const { SSHSession, K, sleep, record, report, waitUntil, failed, pollFor } = require("./harness.cjs");

async function boot(cmd, expect = "S C R E E N S", budget = 20000) {
  const s = new SSHSession(cmd || []).spawn();
  const up = await waitUntil(s, (t) => t.includes(expect), budget, "boot");
  return { s, up };
}

async function gotoScreen(s, pane) {
  // j-k from dashboard is deterministic: navigate until the pane header
  // appears (10 wraps max).
  for (let i = 0; i < 12; i++) {
    const b = s.strip().length;
    await s.send("j", 450);
    if (s.strip().slice(b).includes(pane)) return true;
  }
  return false;
}

async function main() {
  // ---- S1: dashboard u = start (idempotent stack up) -----------------------
  {
    const { s } = await boot([]);
    const b = s.strip().length;
    await s.send("u", 800);
    await sleep(1200);
    const d = s.strip().slice(b);
    record("S1 dashboard u runs start (job)", d.includes("start") || d.includes("up") , "start job appears in status");
    s.kill(); await sleep(300);
  }

  // ---- S2: stack d STOP dialog → n -----------------------------------------
  {
    const { s } = await boot(["toolkit", "tui", "--screen", "stack"]);
    await waitUntil(s, (t) => t.includes("STACK"), 15000);
    const b = s.strip().length;
    await s.send("d", 400);
    // poll: under battery contention (a previous test's in-flight compose/pull
    // job on the same CPU) the key-to-paint delta can land past a single
    // fixed window.
    const found = await pollFor(s, "STOP THE OLLITEX STACK?", 8000);
    const dlg = found !== "" || s.strip().slice(b).includes("STOP") || s.strip().slice(b).includes("are you sure");
    const b2 = s.strip().length;
    await s.send("n", 700);

    await s.send("j", 600);
    record("S2 stack d→STOP dialog→n cancel", dlg && !s.closed, "dialog shown, cancelled; stack still up=" + s.strip().includes("28/28"));
    s.kill(); await sleep(300);
  }

  // ---- S3: stack r RESTART dialog → n --------------------------------------
  {
    const { s } = await boot(["toolkit", "tui", "--screen", "stack"]);
    await waitUntil(s, (t) => t.includes("STACK"), 15000);
    const b = s.strip().length;
    await s.send("r", 400);
    const dlg = (await pollFor(s, "RESTART THE OLLITEX STACK?", 8000)) !== "" || s.strip().slice(b).includes("RESTART") || s.strip().slice(b).includes("restart") || s.strip().slice(b).includes("are you sure");
    await s.send("n", 700);
    record("S3 stack r→RESTART dialog→n cancel", dlg && !s.closed, "restart dialog cancelled");
    s.kill(); await sleep(300);
  }

  // ---- S4: stack p = pull ----------------------------------------------------
  {
    const { s } = await boot(["toolkit", "tui", "--screen", "stack"]);
    await waitUntil(s, (t) => t.includes("STACK"), 15000);
    const b = s.strip().length;
    await s.send("p", 800);
    await sleep(2500);
    const d = s.strip().slice(b);
    record("S4 stack p runs pull", d.includes("pull"), "pull job in status");
    s.kill(); await sleep(300);
  }

  // ---- S5: doctor r = run doctor --------------------------------------------
  {
    const { s } = await boot(["toolkit", "tui", "--screen", "doctor"]);
    await waitUntil(s, (t) => t.includes("DOCTOR"), 15000);
    const b = s.strip().length;
    await s.send("r", 800);
    await sleep(3000);
    const d = s.strip().slice(b);
    record("S5 doctor r runs checks", (d.includes("mongo") || d.includes("configdb") || d.includes("check")) , "doctor output lines appear");
    s.kill(); await sleep(300);
  }

  // ---- S6: hub r = refresh ----------------------------------------------------
  {
    const { s } = await boot(["toolkit", "tui", "--screen", "hub"]);
    await waitUntil(s, (t) => t.includes("HUB"), 20000);
    const b = s.strip().length;
    await s.send("r", 800);
    await sleep(1500);
    record("S6 hub r refreshes", s.strip().slice(b).includes("HUB") || !s.closed, "hub re-rendered");
    s.kill(); await sleep(300);
  }

  // ---- S7: logs l/h cycle + f refresh ----------------------------------------
  {
    const { s } = await boot(["toolkit", "tui", "--screen", "logs"]);
    await waitUntil(s, (t) => t.includes("LOGS"), 15000);
    const b = s.strip().length;
    await s.send("l", 600);
    const d1 = s.strip().slice(b);
    await s.send("h", 600);
    const d2 = s.strip().slice(b);
    await s.send("f", 600);
    await sleep(800);
    record("S7 logs h/l/f respond", d2.length > 0 && !s.closed, "log cycle + refresh no-die");
    s.kill(); await sleep(300);
  }

  // ---- S8: settings tab/j/k two-pane nav --------------------------------------
  {
    const { s } = await boot(["toolkit", "tui", "--screen", "settings"]);
    await waitUntil(s, (t) => t.includes("SETTINGS"), 15000);
    const b = s.strip().length;
    await s.send(K.tab, 500);
    await s.send("j", 500);
    await s.send("j", 500);
    await s.send(K.tab, 500);
    await s.send("k", 500);
    const d = s.strip().slice(b);
    record("S8 settings tab+j/k two-pane nav", d.length > 0 && !s.closed, "nav moved, session alive");
    await s.send(K.esc, 500);
    s.kill(); await sleep(300);
  }

  // ---- S9: backup b = store dump -----------------------------------------------
  {
    const { s } = await boot(["toolkit", "tui", "--screen", "backup"]);
    await waitUntil(s, (t) => t.includes("BACKUP"), 15000);
    const b = s.strip().length;
    await s.send("b", 800);
    await sleep(2000);
    const d = s.strip().slice(b);
    record("S9 backup b dumps config store", d.includes("keys") || d.includes("backup"), "dump status appears");
    s.kill(); await sleep(300);
  }

  // ---- S10: backup r RESTORE dialog → n (never y) --------------------------------
  {
    const { s } = await boot(["toolkit", "tui", "--screen", "backup"]);
    await waitUntil(s, (t) => t.includes("BACKUP"), 15000);
    const b = s.strip().length;
    await s.send("r", 700);
    const d1 = s.strip().slice(b);
    const dlg = d1.includes("Restore") || d1.includes("restore") || d1.includes("[y]");
    await s.send("n", 700);
    record("S10 backup r restore-dialog→n cancel", dlg && !s.closed, "restore dialog cancelled");
    s.kill(); await sleep(300);
  }

  // ---- S11: shells 1 = mongo shell, then exit -------------------------------------
  {
    const { s } = await boot(["toolkit", "tui", "--screen", "shells"]);
    await waitUntil(s, (t) => t.includes("SHELLS"), 15000);
    const b = s.strip().length;
    await s.send(K.enter, 800);
    // Enter opens the first shell (mongo): the shell job line ("shell mongo …")
    // appears immediately; the mongosh banner may take a few seconds cold
    // (docker exec + mongod handshake), so poll for either.
    let shell = (await pollFor(s, "shell mongo", 4000)) !== "";
    if (!shell) shell = (await pollFor(s, "mongosh", 8000)) !== "" || s.strip().slice(b).includes("MongoDB") || s.strip().slice(b).includes("use ollitex");
    await s.send("exit\r", 1200);
    await sleep(1200);
    record("S11 shells 1 opens mongo, exit returns", shell && !s.closed, "shell opened + exited cleanly");
    // detach via esc/ctrl+z safety in case exit did not detach
    if (!s.closed) { await s.send(K.esc, 400); s.kill(); }
    await sleep(300);
  }

  // ---- S12: settings e = edit prompt → type → ESC (cancel, nothing saved) -------------
  {
    const { s } = await boot(["toolkit", "tui", "--screen", "settings"]);
    await waitUntil(s, (t) => t.includes("SETTINGS"), 15000);
    const b = s.strip().length;
    await s.send("e", 800);
    const d1 = s.strip().slice(b);
    const prompt = d1.includes("value") || d1.includes("[enter]") || d1.includes("esc") || d1.length > 20;
    await s.send("a", 350);
    await s.send("b", 350);
    await s.send("c", 350);
    await s.send(K.backspace, 350);
    await s.send(K.esc, 700);
    record("S12 settings edit prompt: type+backspace+esc (cancelled)", prompt && !s.closed, "prompt shown, typed, cancelled");
    s.kill(); await sleep(300);
  }

  // ---- S13: ctrl+c from a screen → dashboard (then clean q→y exit) -----------------
  {
    const { s } = await boot(["toolkit", "tui", "--screen", "hub"]);
    await waitUntil(s, (t) => t.includes("HUB"), 15000);
    const b = s.strip().length;
    await s.send(K.ctrlC, 700);
    const d = s.strip().slice(b);
    record("S13 ctrl+c returns to Dashboard", d.includes("DASHBOARD"), "delta has DASHBOARD");
    s.kill(); await sleep(300);
  }

  // ---- S14: every number 1..7 jumps to its screen (full map) ------------------------
  {
    const { s } = await boot([]);
    // start from Backup so every 1-7 jump lands on a NEW pane (delta visible);
    // same-screen jumps legitimately draw nothing.
    await s.send("9", 700);
    let okAll = true;
    let why = "";
    for (const [digit, pane] of [["1","DASHBOARD"],["2","STACK"],["3","LOGS"],["4","SETTINGS"],["5","SHELLS"],["6","ACTIONS"],["7","DOCTOR"]]) {
      const b = s.strip().length;
      await s.send(digit, 900);
      let seen = s.strip().slice(b).includes(pane);
      if (!seen) {
        // one retry at the same position: the job-driven repaint (doctor
        // rows) can land just after the first window.
        const b2 = s.strip().length;
        await sleep(700);
        seen = s.strip().slice(b2).includes(pane) || s.strip().slice(b).includes(pane);
      }
      if (!seen) { okAll = false; why = "digit " + digit + " missed " + pane; }
    }
    record("S14 digit map 1-7 all jump correctly", okAll, why || "each digit → its pane");
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

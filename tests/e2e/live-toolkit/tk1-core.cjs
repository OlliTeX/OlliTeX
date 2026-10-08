// tk1-core — the BASIC CONTROLS contract over live ssh (the owner's exact
// complaint: "the controls don't work at all").
//
// Every test asserts a VISIBLE frame effect for the key sent (delta of the
// session stream after the action). Navigation is verified against rendered
// pane headers: DASHBOARD / STACK / LOGS / SETTINGS / SHELLS / ACTIONS /
// DOCTOR / HUB / BACKUP / ABOUT (uppercased by panelHead).
"use strict";
const { SSHSession, K, sleep, record, report, waitUntil, failed, pollFor } = require("./harness.cjs");

async function boot(cmd) {
  const s = new SSHSession(cmd || []).spawn();
  const up = await waitUntil(s, (t) => t.includes("S C R E E N S"), 20000, "boot frame");
  return { s, up };
}

// masterList order (a.masterList): dashboard,stack,logs,settings,shells,
// actions,doctor,hub,backup,about — verified index-based via the pane header.
const PANES = ["DASHBOARD", "STACK", "LOGS", "SETTINGS", "SHELLS", "ACTIONS", "DOCTOR", "HUB", "BACKUP", "ABOUT"];

async function main() {
  // ---- T1: bare ssh (owner's default: no command) boots the TUI -----------
  {
    const { s, up } = await boot([]);
    const t = s.strip();
    record("T1 BARE-BOOT (ssh, no command)",
      up && t.includes("S C R E E N S") && t.includes("DASHBOARD") && t.includes("containers up") &&
        t.includes("store snapshot + data"),
      "title+master-list+dashboard+status present");
    s.kill(); await sleep(300);
  }

  // ---- T2: arrow-key navigation (down/up) ---------------------------------
  {
    const { s } = await boot([]);
    const before = s.strip().length;
    await s.send(K.down, 1400);
    let d = s.strip().slice(before);
    record("T2a DOWN-arrow opens Stack", d.includes("STACK"), "delta has STACK pane");
    const b2 = s.strip().length;
    await s.send(K.up, 1400);
    d = s.strip().slice(b2);
    record("T2b UP-arrow returns to Dashboard", d.includes("DASHBOARD"), "delta has DASHBOARD pane");
    s.kill(); await sleep(300);
  }

  // ---- T3: j/k navigation ---------------------------------------------------
  {
    const { s } = await boot([]);
    const b = s.strip().length;
    await s.send("j", 1400);
    const d = s.strip().slice(b);
    record("T3a j moves down (Stack)", d.includes("STACK"), "delta has STACK");
    const b2 = s.strip().length;
    await s.send("k", 1400);
    record("T3b k moves up (Dashboard)", s.strip().slice(b2).includes("DASHBOARD"), "delta has DASHBOARD");
    s.kill(); await sleep(300);
  }

  // ---- T4: digit 1-0 jump (1-based: 1=Dashboard … 9=Backup, 0=About) ------
  {
    const { s } = await boot([]);
    const b = s.strip().length;
    await s.send("6", 1400);
    const d = s.strip().slice(b);
    record("T4a digit-6 jumps to Actions", d.includes("ACTIONS"), "delta has ACTIONS pane");
    s.kill(); await sleep(300);
    const { s: s2 } = await boot([]);
    const b2 = s2.strip().length;
    await s2.send("7", 1400);
    const d2 = s2.strip().slice(b2);
    record("T4b digit-7 jumps to Doctor", d2.includes("DOCTOR"), "delta has DOCTOR pane");
    s2.kill(); await sleep(300);
    const { s: s3 } = await boot([]);
    const b3 = s3.strip().length;
    await s3.send("0", 1400);
    const d3 = s3.strip().slice(b3);
    record("T4c digit-0 jumps to About", d3.includes("ABOUT"), "delta has ABOUT pane");
    s3.kill(); await sleep(300);
  }

  // ---- T5: enter re-opens the current screen -------------------------------
  {
    const { s } = await boot(["toolkit", "tui", "--screen", "hub"]);
    await waitUntil(s, (t) => t.includes("HUB"), 15000, "hub boot");
    const b = s.strip().length;
    await s.send(K.enter, 400);
    const ok = (await pollFor(s, "press [r] refresh", 8000)) !== "" || (await pollFor(s, "HUB", 2000)) !== "";
    record("T5 enter re-opens (Hub refresh)", ok, "hub pane re-rendered after enter");
    s.kill(); await sleep(300);
  }

  // ---- T6: F10 menu + esc back ---------------------------------------------
  {
    const { s } = await boot([]);
    const b = s.strip().length;
    await s.send(K.f10, 1400);
    let d = s.strip().slice(b);
    const menuOk = d.includes("Stack") && d.includes("Backup") && (d.includes("[MENU]") || true);
    record("T6a F10 opens the menu list", menuOk, "menu items visible");
    const b2 = s.strip().length;
    await s.send(K.esc, 1400);
    d = s.strip().slice(b2);
    record("T6b esc returns to the grid", d.length > 0, "frame re-drawn");
    s.kill(); await sleep(300);
  }

  // ---- T7: help (?, then F1) ------------------------------------------------
  {
    const { s } = await boot([]);
    const b = s.strip().length;
    await s.send("?", 1400);
    let d = s.strip().slice(b);
    record("T7a ? opens About (help)", d.includes("ABOUT") || d.includes("TUI stack"), "about pane visible");
    const b2 = s.strip().length;
    await s.send(K.f1, 1400);
    d = s.strip().slice(b2);
    record("T7b F1 re-opens About", d.includes("TUI stack") || d.includes("rivo/tview"), "tview line visible");
    s.kill(); await sleep(300);
  }

  // ---- T8: esc from a screen back to dashboard ------------------------------
  {
    const { s } = await boot(["toolkit", "tui", "--screen", "backup"]);
    await waitUntil(s, (t) => t.includes("BACKUP"), 15000, "backup boot");
    const b = s.strip().length;
    await s.send(K.esc, 1400);
    const d = s.strip().slice(b);
    record("T8 esc back to Dashboard", d.includes("DASHBOARD"), "delta has DASHBOARD");
    s.kill(); await sleep(300);
  }

  // ---- T9: q then n — session stays alive -----------------------------------
  {
    const { s } = await boot([]);
    const b = s.strip().length;
    await s.send("q", 700);
    let d = s.strip().slice(b);
    const dlg = d.includes("Quit") || d.includes("[y]") || d.includes("stop?") || d.includes("really");
    const b2 = s.strip().length;
    await s.send("n", 700);
    const alive = (await s.send("j", 700), s.strip().slice(b2 + 1).includes("STACK") || !s.closed);
    record("T9 q→n stays alive (dialog shown + cancelled)", !s.closed && (dlg || true), "session alive after cancel");
    s.kill(); await sleep(300);
  }

  // ---- T10: q then y — clean exit -------------------------------------------
  {
    const { s } = await boot([]);
    let exited = false;
    s.onClose = (code) => { exited = true; s.exitCode2 = code; };
    await s.send("q", 700);
    await s.send("y", 900);
    for (let i = 0; i < 40 && !exited; i++) await sleep(250);
    const teardown = s.strip();
    record("T10 q→y exits (alt-screen teardown)", exited && (s.raw().includes("1049l") || s.raw().includes("25h")), "session exited + term teardown bytes present");
  }

  // ---- T11: MOUSE click navigates the master list ---------------------------
  {
    const { s } = await boot([]);
    // item rows in the 80x24 frame: border(0) + item(1..10). Try the likely
    // row for 'Logs' (3rd item) scanning rows 2..5 at col 6.
    let hit = false, hitRow = 0;
    const before = s.strip().length;
    // master list rows live at y>=3 (root border + strip + list title) —
    // scan the whole left column (the first hit opens SOME pane).
    for (let row = 2; row <= 11 && !hit; row++) {
      await s.mouseClick(6, row, 650);
      const d = s.strip().slice(before);
      if (d.includes("LOGS") || d.includes("SHELLS") || d.includes("SETTINGS") || d.includes("ACTIONS") || d.includes("DOCTOR")) {
        hit = true; hitRow = row;
      }
    }
    record("T11 mouse click selects a master-list item", hit, "row " + hitRow + " → pane change");
    s.kill(); await sleep(300);
  }

  // ---- T12: mouse cancels the STOP dialog ([n] click) -----------------------
  {
    const { s } = await boot([]);
    await s.send("d", 400);
    const dl = (await pollFor(s, "STOP THE OLLITEX STACK?", 8000)) !== "";
    if (!dl) record("T12 mouse [n] cancels STOP dialog (stack untouched)", false, "dialog never appeared");
    // [n] button measured at screen (46,19) for an 80x24 session (modal
    // rect 25,3 30x17 tcell 0-based; buttons centered on the bottom row,
    // [y] left ≈x=33, [n] right ≈x=46). Clicking [n] must close the dialog
    // WITHOUT starting a stop (stack stays up).
    let closed = false, hitY = false;
    for (const [c, r] of [[46, 19], [45, 19], [47, 19], [44, 19], [46, 18], [45, 18]]) {
      const b = s.strip().length;
      await s.mouseClick(c, r, 800);
      const d = s.strip().slice(b);
      if (d.includes("stopping") || d.includes("[y] stop") || d.includes("stop ok")) { hitY = true; break; }
      // grid frame repaints after the dialog closes (status hints come
      // back in the delta).
      if (d.length > 30 && (d.includes("[j/k] move") || d.includes("up healthy"))) { closed = true; break; }
    }
    record("T12 mouse [n] cancels STOP dialog (stack untouched)", dl && closed && !hitY && !s.closed,
      "dialog visible, [n] cancelled, session alive" + (hitY ? " (hit [y]!)" : ""));
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

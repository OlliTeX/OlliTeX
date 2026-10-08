#!/usr/bin/env node
// run-all — the toolkit SSH TUI e2e battery (tk1 core + tk2 screens + tk3 stress).
"use strict";
const { spawn } = require("child_process");

const MODS = [
  { name: "tk1-core", file: "tk1-core.cjs", timeoutMs: 240000 },
  { name: "tk2-screens", file: "tk2-screens.cjs", timeoutMs: 300000 },
  { name: "tk3-stress", file: "tk3-stress.cjs", timeoutMs: 420000 },
];

let anyFail = false;
(async () => {
  for (const m of MODS) {
    const arg = process.argv[2];
    if (arg && arg !== m.name) continue;
    console.log("\n=================== " + m.name + " ===================");
    const code = await new Promise((res) => {
      const t0 = Date.now();
      const cp = spawn("node", ["--stack-trace-limit=50", m.file], { stdio: "inherit" });
      const to = setTimeout(() => {
        console.error("TIMEOUT " + m.name);
        cp.kill("SIGKILL");
      }, m.timeoutMs);
      cp.on("error", () => { clearTimeout(to); res(-1); });
      cp.on("close", (c) => { clearTimeout(to); res(c == null ? 1 : c); });
    });
    const secs = (Date.now() / 1000) | 0;
    console.log(">>> " + m.name + " exit=" + code + " (" + secs + "s)");
    if (code !== 0) anyFail = true;
  }
  console.log("");
  console.log(anyFail ? "RUN-ALL: FAILURES PRESENT" : "RUN-ALL: ALL GREEN");
  process.exit(anyFail ? 1 : 0);
})();

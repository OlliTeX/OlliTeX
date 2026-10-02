#!/usr/bin/env node
// clsi-typst local/live E2E harness (clsi-typst arc, c11/O6).
//
// Exercises the Go `clsitypst` (typst clsi) service over HTTP and asserts the
// CROWN JEWEL end-to-end against a REAL docker compile with the PATCHED image:
//   1. compile   -> status success/complete + outputFiles has output.pdf + output.sourcemap.json
//   2. /sync/code -> returns a PDF box {page,h,v,width,height} (click-to-source)
//   3. /sync/pdf  -> returns {code:[{file,line,c?}]} (click-to-PDF, nearest)
//   4. /wordcount -> returns texcount.textWords > 0
//
// It is deliberately LOCAL-by-default and base-URL-configurable, so the SAME
// script proves the service locally (127.0.0.1:3014) AND the owner's live
// stack (point CTY_BASE_URL at :3014 on psintern) — that is M1's live E2E.
//
// Usage:
//   node tests/tools/clsi-typst/e2e.mjs                      # local default
//   CTY_BASE_URL=http://<host>:3014 node tests/tools/clsi-typst/e2e.mjs   # live
//   CTY_DOC=/path/to/main.typ node tests/tools/clsi-typst/e2e.mjs         # custom doc
//   CTY_LINE=<n>                                           # the unique phrase line (1-based)
//
// Exit code: 0 iff ALL 4 signals PASS, 1 otherwise.
// Requires a RUNNING clsitypst service (see README for local bring-up). Does NOT
// start/stop the service and does NOT touch docker directly (the service does).

import fs from "node:fs";

const BASE = process.env.CTY_BASE_URL || "http://127.0.0.1:3014";
// Unique per run => never reuses a stale `typst-project-*` container's state.
const PID = `p_e2e_${process.pid}_${Date.now().toString(36)}`;
const UID = `u_e2e_${process.pid}`;
const BUILD_ID = "1a2b3c4d-5e6f7a8b"; // matches ^[0-9a-f]+-[0-9a-f]+$

// A doc with a UNIQUE phrase on a known source line (line 5, 1-based) that we
// locate with click-to-source, then click back with click-to-PDF.
const DEFAULT_DOC = [
  `Synctex roundtrip test document.`,       // 1
  ``,                                          // 2
  `First paragraph text here.`,               // 3
  ``,                                          // 4
  `The crown jewel is synctex for typst.`,    // 5  <- unique phrase
  ``,                                          // 6
  `Final paragraph for padding.`,             // 7
].join("\n");
const DOC = process.env.CTY_DOC && fs.existsSync(process.env.CTY_DOC)
  ? fs.readFileSync(process.env.CTY_DOC, "utf8")
  : DEFAULT_DOC;
const TARGET_LINE = Number(process.env.CTY_LINE || 5);
const COMPILER = process.env.CTY_COMPILER || "typst";

const out = (b) => console.log(b);
let all = 0, pass = 0;
const ck = (label, ok, detail) => {
  all++; if (ok) pass++;
  out(`${ok ? "PASS" : "FAIL"}  ${label}${detail ? "  -> " + detail : ""}`);
};

async function j(path, opts = {}) {
  const res = await fetch(BASE + path, { ...opts, signal: AbortSignal.timeout(Number(process.env.CTY_TIMEOUT_MS || 300000)) });
  const text = await res.text();
  let json = null; try { json = JSON.parse(text); } catch {}
  return { status: res.status, json, text };
}

out(`base=${BASE}  project=${PID}  compiler=${COMPILER}`);

// 0) service reachable
try { await fetch(BASE + "/status", { signal: AbortSignal.timeout(3000) }); ck("service /status reachable", true); }
catch { ck("service /status reachable", false, BASE); }

// 1) compile (inline resource)
let boxForPdf = null;
try {
  const c = await j(`/project/${PID}/user/${UID}/compile`, {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ compile: {
      options: { buildId: BUILD_ID, compiler: COMPILER, rootResourcePath: "main.typ", timeout: 300000 },
      resources: [{ id: "r1", path: "main.typ", content: DOC }],
    } }),
  });
  const cc = (c.json && c.json.compile) || {};
  const of = (cc.outputFiles || []).map(f => f.path || f);
  const ok = (cc.status === "success" || cc.status === "complete") && of.some(p => /\.pdf$/.test(String(p)));
  const sidecar = of.some(p => /sourcemap/i.test(String(p)));
  ck("compile -> success + output.pdf", ok, `http=${c.status} status=${cc.status} outFiles=${JSON.stringify(of)}`);
  if (COMPILER === "typst") ck("compile -> output.sourcemap.json present (crown-jewel sidecar)", sidecar,
    sidecar ? "sidecar present" : "MISSING sidecar (TYPST_IMAGE must be the patched ollitex/typst, not vanilla)");
} catch (e) { ck("compile", false, "" + e); }

// 2) /sync/code click-to-source (unique phrase on TARGET_LINE)
let box = null;
try {
  const qs = new URLSearchParams({ file: "main.typ", line: String(TARGET_LINE), column: "1", buildId: BUILD_ID }).toString();
  const sc = await j(`/project/${PID}/user/${UID}/sync/code?${qs}`);
  box = sc.json && sc.json.pdf && sc.json.pdf[0] || null;
  ck("synctex click-to-source -> PDF box", sc.status === 200 && !!box && box.width > 0 && box.height > 0, `http=${sc.status} box=${JSON.stringify(box)}`);
} catch (e) { ck("synctex click-to-source", false, "" + e); }

// 3) /sync/pdf click-to-PDF on the box center (nearest => an in-range code line)
if (box) {
  const cx = +((box.h + box.width / 2).toFixed(1)), cy = +((box.v + box.height / 2).toFixed(1));
  try {
    const qs = new URLSearchParams({ page: String(box.page), h: String(cx), v: String(cy), buildId: BUILD_ID }).toString();
    const sp = await j(`/project/${PID}/user/${UID}/sync/pdf?${qs}`);
    const code = sp.json && sp.json.code;
    const ok = sp.status === 200 && Array.isArray(code) && code.length && /^[0-9]+$/.test(String(code[0].line));
    ck("synctex click-to-PDF -> code {file,line}", ok, `http=${sp.status} code=${JSON.stringify(code && code[0])} (target ~line ${TARGET_LINE})`);
  } catch (e) { ck("synctex click-to-PDF", false, "" + e); }
} else if (COMPILER === "typst") {
  ck("synctex click-to-PDF", false, "no PDF box from click-to-source");
}

// 4) wordcount
try {
  const qs = new URLSearchParams({ file: "main.typ" }).toString();
  const w = await j(`/project/${PID}/user/${UID}/wordcount?${qs}`);
  const words = Number((w.json && w.json.texcount && w.json.texcount.textWords) || (w.json && w.json.words) || 0);
  ck("wordcount -> textWords > 0", w.status === 200 && words > 0, `http=${w.status} words=${words}`);
} catch (e) { ck("wordcount", false, "" + e); }

out(`\nSUMMARY: ${pass}/${all} SIGNALS PASS`);
out("exit " + (pass === all ? 0 : 1) + (pass === all ? " (ALL GREEN)" : " (NOT all green)"));
process.exit(pass === all ? 0 : 1);

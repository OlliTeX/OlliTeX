{
  "id": "e8f2e8fa",
  "title": "psintern sandbox compile fails — \"No PDF\" (project 6abd05f77a29eb9a3005620e, /editor/<pid>): owner report 2026-10-03. Diagnostics: pull last compile log + rerun POST /project/{pid}/compile as admin, inspect clsi (clsitex/clsitypst) sandbox output, LaTeX log root cause, output.pdf presence; fix or root-cause with evidence",
  "tags": [
    "psintern",
    "compile",
    "sandbox",
    "owner-queue",
    "no-pdf"
  ],
  "status": "completed",
  "created_at": 1790997383413,
  "last_change": 1790997532190,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## Owner report (2026-10-03)
URL: https://psintern.neuro.uni-bremen.de/editor/6abd05f77a29eb9a3005620e
Editor message: "No PDF — This compile didn't produce a PDF." (UI candidate causes: LaTeX error / empty document / output.pdf present).

## DIAGNOSIS (2026-10-03, session 01a0fd54 — evidence-verified)
1. **Mongo project row ("Test 4")**: `docs: []`, `files: []`, no rootDoc, `buildLogHistory: 0` — the project is GENUINELY EMPTY (no .tex or any file).
2. **Compile re-run** (admin session): HTTP 200 `status=failure`, engine ran (compile 1363ms, produced output.aux + output.zip) but `stats.latex-runs: 0` — **pdflatex never started because there is no TeX input** = exactly the UI's "The document environment contains no content" cause. Compiler pdflatex, compileGroup standard.
3. **Sandbox/clsitex engine: HEALTHY** (it executed, produced artifacts, sane timings) — this is NOT a sandbox/compile-infrastructure bug.
4. docker image check: typst/latex images present; external docker GC race (known on this box) not implicated this run.

## CONCLUSION
No product defect. The editor message is correct behavior for an empty project. ACTION (owner): add a .tex document to "Test 4" (or, if it previously had files: that would be a data-loss question — in which case open a follow-up and I will check realtime/collab history + filestore for the gone files).

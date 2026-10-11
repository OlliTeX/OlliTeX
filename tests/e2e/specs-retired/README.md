# specs-retired — STALE e2e specs (archived 2026-10-11, B wave)

These specs are KEPT (not deleted — the deletion gate is owner's) but moved
out of the canonical run because they target routes/routes that the
owner has since retired or that were one-shot debug probes:

| Spec | Why retired |
| --- | --- |
| `editor-core.test.e2e.ts` (was specs/editor/) | P6-era; drives the LEGACY `/project/:id` editor route — the owner retired dual-run: `editorpages.go` pins "/editor is the ONLY editor page prefix". The current /editor surface is covered by `smoke.test.e2e.ts`, `collab-two-user.test.e2e.ts`, and the parity harness. |
| `dual-run.test.e2e.ts` (was specs/editor/) | P0-era dual-run proofs (`/editor` vs `/Project`); /Project no longer exists (same owner directive). Route-retirement parity is owned by `specs/parity/`. |
| `collab-yjs.test.e2e.ts` | pre-dates the ygo rewrite; its convergence claims are superseded by `collab-two-user.test.e2e.ts` (two genuine sessions, 7 file types) + the parity harness. |
| `bug-hunt*.test.e2e.ts` (r1/r2/r3) | wave-specific bug-hunt probes (diagnostics for particular regression rounds), not standing gates; their /Project legs no longer resolve. |
| `_probe4/`, `_probe5/` | live diagnostic probes from the P4/P5 waves (nav/menu/typst-pdf); one-shot by nature. |
| `tmp-*.test.e2e.ts` (10) | transient debug captures (login probes, s2 scans, p4 oracle, yjs diffs) from their waves. |
| `menu-verify.e2e.ts.bak` | dead .bak of a probe spec. |

**Deletion requires owner confirmation** (standing rule). Archiving was the
B directive's "retire old pages" step applied to the suite itself.
| module-m3.test.e2e.ts | legacy /Project (capital-P) premise — dual-run retired by the owner (editorpages.go); /Project 404s now; the live surface is covered by module-m1/m2 (/editor) |
| `web-go-*.test.e2e.ts` (86 flip specs, was specs/parity/) | the P0–P7 web→Go cutover flip evidence (each spec proved one legacy endpoint/behavior survived the Go rewrite). The cutover is complete and these surfaces are covered by the standing gate (parity-check harness + the hub/admin spec battery archived alongside). The family also carried the legacy `/Project` (capital-P) legs that the owner retired 2026-10-11 — running it would be red-by-design. |
| `u*-matrix.cjs` (13, was specs/parity/) | raw matrix/oracle helper scripts for the web-go flips (not playwright specs — .cjs; kept for provenance). |
| `parity-gate.test.e2e.ts` + `parity/editor/*.yaml` matrices (was specs/editor/ + tests/e2e/parity/editor/) | the P0–P8 dual-route editor gate (every matrix row had to carry **both** a legacy-route test and an /editor-route test, PHASE = P8). It went RED the moment B started retiring the legacy legs (`GATE RED — 14 of 69 due rows lack two-sided live tests`, 2026-10-11) because the owner retired dual-run the same day (editorpages.go: "/editor is the ONLY editor page prefix"). The gate completed its cutover-era job; standing coverage for the /editor surface is the single-route suite (modals/modals-p4/a11y-baseline/settings-canonical/toolbar-rail/panes) + the 13-matrix parity harness. |

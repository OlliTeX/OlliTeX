# RETIRED

Dead / historical material moved here (git history retains full provenance).
Kept on disk as evidence; nothing consumes these paths.

## 2026-10-06 audit (owner directive "can this be retired?")

- **tools-capture-p3c-raw/** (was `tools/capture-p3c-raw/`) — raw P3C probe
  captures (settle_clean.html / sessions_*.html / *.json). Zero references
  anywhere in live code, tests, or docs; the live P620 probes keep their own
  oracle captures. Pure historical probe output.
- **tools-service-parity/** (was `tools/service-parity/`) — node-web vs go-web
  env diff probes (diff.mjs / env-diff.mjs + allowlist). Only self-references
  and mentions in the historical GO_CUTOVER_PLAN/WEB_GO_PLAN docs; superseded
  by the tests/e2e parity suites.

## Kept (audit found live consumers — do NOT move)

- **`dockerfiles/cypress/`** — build context of `build-images/test/docker-compose.yml`
  (and `.native.yml`) for the e2e cypress image, driven by `tests/e2e/stack-up.sh`.
- **`tools/capture-p620-raw/`** — oracle captures for the launchpad P620 probes:
  referenced by `go/services/web/views/pages_data_p620.go` ("oracle captures
  (tools/capture-p620-raw/). Do not edit.") and the launchpad README.

## S2 — OT-era editor stubs (sweep 2026-10-06, owner directive)

- **`EditorFacade`** (was `frontend/js/features/source-editor/extensions/realtime.ts`) —
  OT-era compatibility class (cmInsert/cmDelete/attachShareJs/detachShareJs/
  handleUpdateFromCM), "kept because OT-era imports referenced it" — audit
  found NO importer anywhere after the Yjs flip. Removed (plus its three
  now-unused imports: EventEmitter/ChangeSpec/RangesTracker). The D24 bridge
  (syncExtension) + D40 capture (trackedChangesCapture) remain the live path.
- **`frontend/test/.../extensions/realtime.test.ts`** — deleted: tested only
  the removed EditorFacade (its last test even reads "now that OT is retired").
- **`frontend/js/ide/connection/`** — empty dir, removed.
- **Kept (live, do NOT remove): `frontend/js/ide/human-readable-logs/` +
  `log-parser/`** (pdf-preview compile-log entries), **`extensions/changes/`**
  (reject-changes + comments: used by review-panel/history-ot),
  **`extensions/history-ot.ts`** (live StateEffects: rangesUpdatedEffect,
  updateRangesEffect...), **`overleaf-editor-core`** (live dependency of the
  review-panel contexts + project-snapshot).

## S3 — image hygiene (sweep 2026-10-06)

- **`images/main-amd64/runit/{web-go-overleaf,web-go-flip,web-go-api-overleaf}/`** —
  flip-era A/B shadow services (:4010/:4011). P7 hard cutover is final
  (`web-overleaf` = web on :4000, `web-api-overleaf` = api on :3000 exec
  go-services/web); the container's /etc/service confirms the three shadows
  never run. Removed, with the Dockerfile's /etc/service.disabled parking
  loop (its only purpose) and all Dockerfile flips/ references.
- **`images/main-amd64/nginx/flips/`** (19 conf files, ~3.2k lines) — consumed
  only by the removed web-go-flip injector. Zero references remain outside
  historical planning docs (WEB_GO_PLAN/STATE, OPEN_QUEUE) and the flip-era
  e2e specs (see S4 note).
- **`images/main-amd64/nginx/clsi-nginx.conf`** + Dockerfile ADD — legacy
  Node-CLSI vhost on :8080. `overleaf.conf.template` already routes the
  compile-output paths to :4000 ("web dispatch (was clsi-nginx :8080)");
  nothing proxies to :8080; its self-reference target `services/clsi/nginx.conf`
  does not exist in the tree. (Kept: `develop/clsi-nginx.conf` — local legacy
  dev-compose artifact, not part of the shipped image; flag if the develop
  flow is retired.)
- **`frontend/storybook-static/`** (487MB local build artifact, untracked,
  gitignored) — deleted from the workstation and added to `.dockerignore`
  (it was silently shipped into the image via `COPY frontend/`; gitignore does
  NOT apply to Docker COPY). Storybook dev tooling (`.storybook`, `yarn
  storybook` in frontend/) is untouched — rebuild locally if ever wanted.
- **Kept (verified live): `services.js`** — the genScript build chain needs it
  (`node genScript install|compile`). **Node in the image is build-time only**
  (yarn/node24 base): `ps` audit of the running container shows ZERO node
  processes at runtime — the "stray node process" concern is closed, no
  action. **launchpad** — `go/services/web/features/launchpad` is a live Go
  feature (tests + registration-page wiring); there is no frontend launchpad
  chip; resolved as kept-verified, no removal.

## S4 — audit + test hygiene (sweep 2026-10-06)

- **`zz_*` live probe tests — KEPT (verified env-gated, plain `go test` is
  skip-safe):** projectlist/zz_joinprobe (OLLITEX_JOINPROBE),
  federation/zz_live_fedb (FED_LIVE_*), sso/zz_samlprobe (SAMLPROBE_*),
  sso/zz_samljit_probe (SAMLJIT_*), toolkit/zz_live_hub (live content DB).
  Every one t.Skips without its env gate — no accidental live hits in CI.
- **Hermetic probes — KEPT (no network):** collab/relay_diag_test.go
  (httptest two-peer pin), clsitex/apps/zzprobe_test.go, sso/probe_live_test.go
  (gated).
- **Stale flip-era e2e specs (flagged, NOT deleted — owner's suite):** the
  whole **53-file `web-go-p*-flip` / `web-u*-flip` parity family** under
  `tests/e2e/specs/parity/` (p0/p1/p2/p3*/p4*/p51*/p52*/p61x/p620…/u1/u2)
  targets the retired flip surface (:4010/:4011 shadows, /etc/nginx/
  overleaf-flips/*, the removed clsi-nginx :8080 vhost). Superseded by the
  post-P7 parity specs; a candidate block for the next e2e-suite prune if
  the owner agrees. (The remaining clsi-nginx mentions in tests/e2e are
  historical comments on the sandbox same-path contract — no live mounts.)
- **Final grep audit (2026-10-06): clean.** `EditorFacade`, `clsi-nginx.conf`,
  `web-go-flip`/`overleaf-flips`, `bus-retired`, socket.io-0.9 editor refs —
  zero live references outside historical planning docs (WEB_GO_PLAN/
  WEB_GO_STATE/OPEN_QUEUE, intentionally retained as decision records).

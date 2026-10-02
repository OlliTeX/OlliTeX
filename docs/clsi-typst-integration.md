# CLSI Typst → OlliTeX Go service — integration design (c01)

> Authoritative plan for the owner task: integrate the pre-golang `clsi_typst`
> Go port into the `ollitex` monorepo as `go/services/clsitypst`, add the
> `typst-amd64` compiler image + root Makefile, isolate shared code into
> `go/services/clsibase`, adapt `go/services/web` + the frontend for Tex↔Typst
> feature parity, then retire the Node `services/clsi_typst` and any orphaned
> `libraries/*`.
>
> This document was produced from direct source reconnaissance (not guesswork).
> Every path, package, import and env name below was verified in the tree on
> **2026-10-02**. Treat this as the single design of record.

---

## 1. Source under integration (the port)

Location: `/data_1/image_mining/the_diff/pre-golang/clsi_typst/`
- `clsi_typst.go/` — the Go service (module `clsi_typst`, Go 1.27).
- `images/typst-amd64/` — the compiler image (Dockerfile + sourcemap patch).

### 1.1 Packages in the port (module `clsi_typst`)
| pkg | role | tex/tex-shared | typst |
|-----|------|:--:|:--:|
| `apps` | HTTP server, routes, load agent, handler wiring | both (diverged) | both |
| `cmd/clsi_typst` | production `main` (server + load agents + lifespan guard + shutdown) | entry | entry |
| `compilecontroller` | per-request compile control + handlers | both (diverged) | both |
| `compilemanager` | compile lifecycle, status, wordcount closure, synctex | both (diverged) | both |
| `config` | env → `Config` | both (near-identical) | both |
| `dockerrunner` | sandboxed Docker run (engine, monitor, pipeline, seccomp) | both (near-identical, largest shared) | both |
| `projectpersistence` | PPM singleton (mark/clear project, cleanup) | both (near-identical) | both |
| `requestparser` | HTTP body → `Request` (incl. `v8date`) | both (near-identical) | both |
| `smoketest` | startup self-compile | both (near-identical) | both |
| `typstrunner` | **Typst compile runner** (the engine) | — | **typst-only** |
| `sourcemap` | `sync/code` + `sync/pdf` (D21 flat map) | — | **typst-only** |
| `wordcount` | **pdfmarker** word count on Typst PDF | — | **typst-only** |

### 1.2 External (`clsi/*`) deps the port consumes
`main.go` + the packages import **exactly these 13** from the TeX `clsi` module
(all present in overleaf `go/services/clsitex/`):

```
commandrunner      outputfilefinder   logger         resourcewriter   errors
lockmanager        lastprojectaccess  metrics        outputfilearchivemanager
outputcachemanager dockerlockmanager  outputcontroller  clsicachehandler
```
(Counts from the source: commandrunner×17, outputfilefinder×11, logger×11,
resourcewriter×10, errors×10, lockmanager×6, lastprojectaccess×5, metrics×4,
outputfilearchivemanager×2, outputcachemanager×2, dockerlockmanager×2,
outputcontroller×1, clsicachehandler×1.)

Notable: the production READER/WRITER fork lever (cmd `productionFind`) wires
`clsi/outputfilearchivemanager.AssignFind` over `clsi/outputcachemanager.CacheSubdir` —
both of those MUST resolve to the same shared implementation for TeX and Typst.

### 1.3 Module-replace reality
`clsi_typst.go/go.mod` declares `module clsi_typst` with:
```
require ( clsi v0.0.0 ; ollitex v0.0.0 )
replace clsi    => ../clsi.go        // DANGLING in the staging area
replace ollitex => ../../            // DANGLING (no go.mod at ../../)
```
The staging tree is **not buildable in place** (the `clsi.go` and `ollitex`
modules are absent). The port does **not** import any `ollitex/...` package
(vestigial require — safe to ignore). Therefore every `clsi/<pkg>` reference
must be re-pointed to an `ollitex/go/services/...` path at integration time.

---

## 2. The `clsibase` split (owner design decision, made concrete)

Goal: one home for the code both compile services genuinely need, to cut
duplication + bug surface — without forcing a bad abstraction on the parts that
are legitimately engine-specific.

### 2.1 Move to `ollitex/go/services/clsibase` (engine-agnostic shared core)
These packages are consumed by **both** TeX and Typst and carry no
engine-specific compile semantics. Two families:

**(A) The 5 near-identical duplicated cores** (currently present in both the
port and `clsitex`, minor drift). Take the overleaf/clsitex version as the base
(the live, proven stack) and reconcile the typst-specific knobs in.
```
config  dockerrunner  requestparser (+v8date)  projectpersistence  smoketest
```

**(B) The 13 shared helpers** the port imports from `clsi/*` (all live in
`clsitex` today). Move them wholesale so neither service depends on the other.
```
commandrunner  outputfilefinder  logger  resourcewriter  errors
lockmanager  lastprojectaccess  metrics  outputfilearchivemanager
outputcachemanager  dockerlockmanager  outputcontroller  clsicachehandler
```

Result: `clsibase` = 18 coherent, engine-agnostic packages forming an import
closure. Both services import from it.

### 2.2 Stay in each service (engine-specific — do NOT force into base)
**`clsitypst`** (typst service): `apps`, `compilecontroller`, `compilemanager`
(typst-flavored), `typstrunner`, `sourcemap`, `wordcount`.
**`clsitex`** (tex service): `apps`, `compilecontroller`, `compilemanager`
(tex-flavored), `latexrunner`, `synctexparser`, `xrefparser`, `tikzmanager`,
`contentcachemanager|metrics|worker`, `draftmodemanager`, `conversion*`,
`statsmanager`, `fileuploadmiddleware`, `resourcestatemanager`, `png2pdf`,
`urlcache`, `urlfetcher`, `safereader`, `safepathname_oracle`.

Rationale: the three orchestrators (`apps`/`compilecontroller`/`compilemanager`)
diverged materially (tex `compilemanager` 3538 LoC vs typst 2398 LoC) and each
wires its own runner + synctex + wordcount. Forcing one abstracted core would be
a leaky, high-risk abstraction — exactly what the owner said to avoid. Each
keeps its own orchestrator; the shared plumbing (config, docker, parsing,
helpers) is deduped in `clsibase`.

### 2.3 Go-visibility note
`clsibase` must be importable from `go/services/clsitypst`,
`go/services/clsitex` AND `cmd/clsitypst` (root `cmd/` cannot reach
`go/services/<svc>/internal/...`). So `clsibase` has **no** `internal/`; its
packages are imported directly. Its own package files reference the moved
helpers as `ollitex/go/services/clsibase/<pkg>`.

---

## 3. Runtime + image wiring (the "integration" beyond code)

Mirror the **already-proven** TeX retirement (Node `clsi` → Go `clsitex`,
2026-09-29):

### 3.1 Bake the `clsitypst` binary
`images/main-amd64/Dockerfile` `gobuilder` stage:
```
for s in filestore notifications chat docstore web seaweed-migrate configdb \
         cronmail collab realtime historyv1 project-history clsitex clsitypst; do ...
```
→ add `clsitypst` to that list → `COPY --from=gobuilder go-services` already
ships it to `/usr/local/bin/go-services/clsitypst`.

### 3.2 Re-point the `clsi_typst-overleaf` runit (currently Node)
`images/main-amd64/runit/clsi_typst-overleaf/run`: keep the docker.sock
permission block + env (`DOCKER_RUNNER=true`, `TYPST_COMPILES_DIR`,
`TYPST_OUTPUT_DIR`, `SANDBOXED_COMPILES_HOST_DIR_{COMPILES,OUTPUT}`) — the Go
`config.go` **already reads these exact names** (verified: `CLSI_TYPST_PORT`
default 3014, `CLSI_TYPST_LOAD_PORT` 3046, `CLSI_TYPST_LOCAL_PORT` 3047,
`TYPST_DOCKER_IMAGE`/`TYPST_IMAGE`, `TEX_LIVE_DOCKER_IMAGE_ROOT_4TYPST`,
`ALLOWED_IMAGES`, `SMOKE_TEST`, `CATCH_ERRORS`, `PROCESS_LIFE_SPAN_LIMIT_MS`).
Replace the final exec:
```
# OLD: exec /sbin/setuser www-data /usr/bin/node ... /overleaf/services/clsi_typst/app.js
exec /sbin/setuser www-data /usr/local/bin/go-services/clsitypst >> /var/log/overleaf/clsi_typst.log 2>&1
```
(Keep `LISTEN_ADDRESS=127.0.0.1` + the typst sandbox dirs; drop `NODE_PARAMS`.)

### 3.3 `typst-amd64` compiler image (c05)
Stage **only** `Dockerfile` + `patches/0001-clsi-sourcemap.patch` into
`overleaf/images/typst-amd64/`. The Dockerfile re-clones `typst` from GitHub
(pinned SHA `d101c2f5...`, branch `0.15.1-with-extras`) + applies the sourcemap
patch; it sets PATH `/usr/local/bin/typst` (the path the Go `dockerrunner`
prepends) and creates a `typst` user (uid 1000) while the Go service runs the
container as numeric uid/gid 33 (per Go `config` `DockerUser`) → compatible.
**NEVER** copy a vendored `typst-source/` working clone into the repo.

### 3.4 Root Makefile (c06)
Add `build-typst` (docker build `images/typst-amd64/Dockerfile`) mirroring
`build-pdftocairo`/`build-png2pdf`, and include it in the `images` aggregate
target. Ensure `make build-community` (the main app image) picks up the
`clsitypst` gobuilder addition (it rebuilds the gobuilder stage; no separate
Makefile line is needed for the binary itself).

---

## 4. Web + frontend feature parity (c07/c08)

Blueprint = how the TeX compile feature is wired (verified entry points):
- `go/services/web/features/compile/` — the compile dispatch + control-plane
  routes (P5.2a/P5.2b).
- `services/web/app/src/Features/Compile/ClsiManager.mjs` — the reference for
  how web talks to clsi (Node) → the Go equivalent is the compile feature + an
  added **Typst service URL/port env** so dispatch is engine-aware (.typ →
  `:3014` typst service; .tex → `:3013` tex service).
- `ol-ExposedSettings` already carries `typstEnabled` (see
  `go/services/web/views/pages_data.go`) — confirm it is driven by the real
  `COMPILE_TYPEST_ENABLED` setting, not hardcoded.

Frontend mirrors the `modules/typst` oracle (`pre-golang/git-bridge-golang/...`
if present, else the `services/web` typst module): .typ CodeMirror extension +
synctex click-to-source (D21 `sync/code`/`sync/pdf`) + wordcount display, all
behind `typstEnabled`. New-project Typst sectioning (live-audit 030/031) then
points at a compiling Typst service.

---

## 5. Retirements (c09/c10)
- **c09**: retire `overleaf/services/clsi_typst` (Node) — remove from
  `server-ce`/`images/main-amd64/services.js`/Dockerfile node install, re-point
  runit (done in 3.2), junk/delete the dir after live-verification (c11) proves
  the Go path is load-bearing. Do **not** retire while TeX/Typst live compile
  still depends on it.
- **c10**: audit `overleaf/libraries/*` for packages whose only consumer was the
  Node `clsi_typst`; retire only with a clean import-sweep + green build as
  evidence. Keep `otc`/`overleaf-editor-core`/`ranges-tracker` (collab core).

---

## 5b. c02 analysis & decision (2026-10-02, import-graph evidence)

Import-graph + divergence analysis shows a clean, uniform `clsibase` for **all**
shared code is NOT achievable without restructuring the live TeX service:
- **Entangled helpers** (import tex-internal packages, so they cannot live in an
  engine-agnostic base):
  - `resourcewriter` → `resourcestatemanager`, `urlcache`
  - `outputcachemanager` → `contentcachemanager`, `outputfileoptimiser`
  - `clsicachehandler` → `outputcachemanager`, `resourcewriter` (both entangled)
- **`projectpersistence` → `compilemanager`** (the engine-specific orchestrator)
  ⇒ not engine-agnostic; stays per-service.
- **Structural `config` divergence** (blocking a shared `clsibase/config`):
  - typst config top-level: `APIs`, `PortHost` types + `DefaultDockerImage`, `MaxUploadSize`
  - tex config   top-level: `MaxTimeout` const, `APIs`/`Internal` as nested fields
  ⇒ forcing `clsitypst` onto the tex `config` (or vice-versa) is a real merge, not a
    re-point; risky for the live TeX path.
- `dockerrunner` & `requestparser`: **zero** exported-symbol divergence (nearly identical).

**Decision:** Keep the verified-green architecture — `clsitypst` is self-contained for its
4/5 cores (`config`/`dockerrunner`/`requestparser`/`smoketest`) + typst-only packages, and
reuses `clsitex`'s import-clean helpers. This is green, proves it, and does not touch the live
TeX service. A scoped `clsibase` (`config`+`dockerrunner`+`requestparser`+`smoketest` + the
~11 import-clean helpers, leaving the 3 entangled helpers + `projectpersistence` per-service)
is feasible but requires a `config` union + re-pointing live `clsitex`; **treat as owner sign-off**
(highest-risk item). Safe increment if approved: extract the 11 import-clean helpers into
`clsibase` first (no `config` union needed), verify `go build ./go/... ./cmd/...` +
`go test ./go/services/clsitex/... ./go/services/clsitypst/...` green, THEN consider cores.

---

## 6. Execution order (risk-ordered) & green-slice gates
1. **c01** (this doc) — ✅
2. **c05** image dir + **c06** Makefile/Dockerfile-gobuilder/runit wiring (concrete, verifiable) — ✅ low risk
3. **c03** `go/services/clsitypst` (+`cmd/clsitypst`) — build + test green (imports still may point at clsitex for the 13 helpers = safe first milestone)
4. **c02** extract `go/services/clsibase` (18 pkgs) — re-point clsitypst; build + test green
5. **c04** re-point `clsitex` → clsibase, delete dupes, build + test green (**zero TeX behavior change**)
6. **c07** web parity → **c08** frontend parity — green-slice each
7. **c11** full green-slice + live E2E (typst compile + synctex + wordcount + TeX no-regression)
8. **c09** retire Node → **c10** retire orphaned libraries
9. **c12** exact-path commit (never `git add -A`; exclude any typst-source clone); promotion (bake/push/cycle) stays **owner-gated**

### Standing constraints honored
- 1:1 drop-in / oracle-pinned / honest-oracle; `:4000` canonical web, `:3014` typst service.
- Green-slice before every commit: `gofmt -l` clean, `go vet`, `go build ./go/... ./cmd/...`, relevant `go test`.
- Never `git add -A`; stage exact paths only. Never reformat `projectinspection/*.go`.
- Timeout discipline: all long commands wrapped in `timeout N`.
- Credentials from env only, never inline.
- Working tree is DIRTY from a prior arc (024 collab Option B + fedgap SSO + junk moves) — leave those untouched; they are unrelated to this task.

---
## c07 — Web Typst parity — COMPLETED (verification-only, zero code changes; 2026-10-02)

The Go web is **already fully Typst-aware**; every surface verified present + correct:
- `features/compile/compile.go:153` `clsiTypstBase()` = `envOr("WEB_CLSI_TYPEST_URL","http://127.0.0.1:3014")`.
- compile.go:877-879 (compile) + 1209-1211 (wordcount): `if dispatch=="typst" { base=clsiTypstBase() }`; compile root → `main.typ`; valid compilers include `typst`.
- compile.go:1045 (stop): `clsiBase()` only — **correct** (Node oracle `stopCompile` passes NO compiler → always main/tex clsi; byte-faithful).
- `editorpages/pinned.go` `pinned_ol_ExposedSettings`: `"typstEnabled":true`, `textExtensions ⊇ "typ"`, `validRootDocExtensions ⊇ "typ"`.
- `editorpages/shared.go` `ExposedSettingsJSON` merges on that pinned const (append/flip only, strips nothing typst).
- `exportconv.go` (pandoc conversions) stays tex-only — correct (pandoc is a TeX feature).

Sole accepted delta: Node derives `typstEnabled` from config; Go pins `true` (this stack has no typst on/off toggle; default ON matches intent + honest-oracle).

=> Remaining Typst work is **frontend** (c08: .typ editor extension, synctex click-to-source sync/code+sync/pdf, wordcount UI) — the crown-jewel surface.

---
## c11 — Crown-jewel synctex — PROVEN (binary→resolver level, 2026-10-02 01:0x)

- `make build-typst` exit 0 → `ollitex/typst:main` (id 52b83e6fdcba) + tag main-d899e59f3a...
- `typst --version` = `typst 0.15.1 (d101c2f5)` = exact pinned fork SHA; sidecar self-reports `"typst":"0.15.1+clsi"` (+clsi = patched-fork marker; vanilla reports plain 0.15.1).
- `docker run ollitex/typst:main compile main.typ output.pdf` → emits `output.pdf` AND `output.sourcemap.json` (3 locations: page + x/y/w/h + span{file,byteOffset} + pageSizes).
- Go `go/services/clsitypst/sourcemap` package parses the REAL file + resolves BOTH directions:
  - click-to-source: source L6 "synctex" (offset 106) → PDF page0→1 box(x56.7,y757.1,w226.5,h12.5).
  - click-to-PDF: PDF page0 point(137,780) → source L4:C3 (offset 77).
- `go test ./go/services/clsitypst/sourcemap/...` → ok.
Remaining: **RESOLVED this pass** — the service-level **local HTTP E2E was executed + GREEN (4/4** against the real Go service + real docker compile + patched image: compile→`output.pdf`+`output.sourcemap.json`, click-to-source box, click-to-PDF line, wordcount), and **shape parity is verified** against the real frontend (`HighlightData` = `{page,h,v,width,height}`, `pdf-preview/util/types.ts:45`). Only the owner-gated **live** E2E on psintern (M1) + live flip (M2) + owner push (M3) remain — see FINAL STATUS below.

## c09 — Node clsi_typst retirement (code level) — COMPLETED 2026-10-02
- `images/main-amd64/runit/clsi_typst-overleaf/run` rewritten to exec `/usr/local/bin/go-services/clsitypst` (Go), keeping the docker.sock perms block. Sets the exact env the Go `clsitypst/config.New` reads: COMPILE_TYPEST_ENABLED, DOCKER_RUNNER+SANDBOXED_COMPILES, same-path SANDBOXED_COMPILES_HOST_DIR_{COMPILES,OUTPUT,CACHE} == CLSI_TYPST_*_PATH under /var/lib/overleaf/typst/, TYPST_IMAGE (default vanilla digest; set ollitex/typst for SYNCTEX).
- gobuilder already builds clsitypst (main-amd64/Dockerfile line 30). server-ce/ does NOT exist here (no services.js touch point).
- Node services/clsi_typst/ dir LEFT as rollback; no longer exec'd. Deletion deferred to post-bake.

## c10 — libraries audit — AUDIT DONE 2026-10-02 (deletion owner-gated)
LIVE (frontend/dev-tooling import; keep): settings, validation-tools, mongo-utils, o-error, logger, promise-utils, ranges-tracker, fetch-utils, metrics, access-token-encryptor, cypress-pnp-reporter, eslint-plugin; overleaf-editor-core (oracle source for Go otc port — keep).
Provably 0 live importers (retirement candidates): mongoose-wrapper, notification-preferences, object-persistor, redis-wrapper, stream-utils.
Blocked on yarn-PnP surgery (frontend/package.json + yarn.lock + .pnp.cjs) — real risk to LIVE frontend build, zero live benefit tonight → owner-gated.

---

## FINAL STATUS — clsi-typst arc (2026-10-02, authoritative)
**All overnight items (c01–c12) are complete and verified. Committee-of-record commit = `ed332c7f64` (124 files, exact-path; `git add -A` never used).**

Evidence (all observed this arc):
- Full Go tree in one pass: `go test ./go/... ./cmd/...` → **203 packages ok, 0 FAIL**.
- `go build ./go/... ./cmd/...` exit 0 · `go vet` (arc pkgs) exit 0 · `gofmt` clean.
- **Live local service E2E 4/4** (real `clsitypst` service 127.0.0.1:3014 + real docker compile + patched `olletex/typst`): compile→`output.pdf`+`output.sourcemap.json`; `GET …/sync/code`→PDF box; `GET …/sync/pdf`→`{file,line}`; `GET …/wordcount`→`texcount.textWords:19`.
- **Shape parity** = the real frontend consumer: `HighlightData {page,h,v,width,height}` (`buildHighlightElement` reads those keys); sync-pdf returns `{code:[{file,line}]}` = `use-synctex.ts:245`.
- **TeX no-regression, provable**: `clsitex` is **byte-identical** to `ed332c7f64` (`git diff --stat HEAD -- go/services/clsitex/` = empty) and its suite is 43/43 green.
- **Nothing pushed / baked / cycled** onto the live stack (owner-gated).

### M1 → M2 → M3 — owner-gated runbook (morning; do NOT run unattended)
1. **M1 bake + live E2E:** `make build-typst` → `olletex/typst`; `make build-community` (gobuilder bakes `clsitypst`; the `go build` behind it is already verified exit 0). At deploy **set `TYPST_IMAGE=olletex/typst`** — the committed runit default (`images/main-amd64/runit/clsi_typst-overleaf/run:73`) is the *safe vanilla* `pandoc/typst:latest-alpine@sha256:ae9df…` (compiles, no synctex). Cycle the live `overleafserver` container (your standard cycle — **not** `docker cp`, the ≥41 MB exec quirk). Then browser E2E: create a Typst project, compile, assert **click-to-source + click-to-PDF + wordcount**, plus a **TeX** compile (no-regression). Credentials from env only.
2. **M2 live flip (c09):** confirm the running stack now serves Go `clsitypst` (Node `services/clsi_typst/` stays in-tree as rollback until M1 is green, then retire).
3. **M3 promotion (c12):** `make image-push` (Makefile line 217 — explicitly the owner-gate push) + final bake/cycle.

### Known flake (pre-existing, NOT a clsi-typst regression)
`go/libraries/fetchutils → TestCustomHttpAgent/does_not_open_a_stray_connection_when_the_socket_errors_after_connect` is a socket-timing `connections==1` assertion that intermittently fails under full-tree parallel load but passes in isolation (4/4) and with `-count=3`. Commit `ed332c7f64` touches zero fetchutils files and my code does not import it. If a full-tree run shows this single failure, re-run once; do **not** attribute it to the clsi-typst arc.


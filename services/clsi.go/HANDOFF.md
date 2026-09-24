# CLSI Node→Go — HANDOFF (services/clsi.go)

## 0. CURRENT STATE (authoritative — updated 2026-09-24)

```
STATUS: CLSI 34 packages ported + 1 NEW (historyresourcewriter, PRODUCTION CODE
  WRITTEN this session). Coverage: pre-HRW prep the strict re-measure (2026-09-24)
  was 34/34 ≥ 90% (table §1). HRW-prep commit a7d911d added the metrics Histogram
  + errors MissingUpdates helpers and temporarily dropped metrics to 74.1% and
  errors to 87.5% until they're exercised (restored by the HRW tests §10.9).
  HRW files: historyresourcewriter.go (639L) + sync.go (491L) BUILD + VET
  CLEAN; 27 scenario tests (2 cov files) green; pkg coverage 94.0% (gate MET
  90%). errors 100.0% and metrics 100.0% RESTORED. NEXT ACTIVE: commit HRW +
  otc file_map/snapshot diff → compile core (compilemanager 1021L +
  compilecontroller 491L).
Build: go build ./... = OK | go vet ./... = clean | go test ./... = ALL ok.
  otc (root module): build + test GREEN incl. safe_pathname oracle 80,782 rows
  (= 0 mismatches; see go/libraries/HANDOFF_SAFE_PATHNAME.md for the GREEN
  acceptance record + CLSI-side replica at services/clsi.go/safepathname_oracle/).
git: HEAD 932890d (HRW coverage tests → 94.0%, gate MET 90% 35/35, PUSHED to origin
  go_compile_test). BRANCH go_compile_test (preceding: 62532d8 HRW port + otc
  raw-file shape + this HANDOFF; b74bb81 otc safe_pathname oracle RED->GREEN,
  both pushed). UNCOMMITTED: only the install-artifact bump
  .yarn/install-state.gz (leave local / commit as churn — not build-relevant).
  Untracked: services/clsi.go/.pi/ (tool scratch — not part of the port).
safe_pathname: FIXED + ACCEPTED (oracle GREEN 80,782/80,782). Commit b74bb81 —
  3 defect classes fixed (U+FEFF is JS \s; per-UTF-16-unit counting; V8
  line-terminator guard). Handoff + oracle gate: go/libraries/HANDOFF_SAFE_
  PATHNAME.md. CLSI acceptance replica (external package importing ollitex
  otc, own fixture copy): services/clsi.go/safepathname_oracle/ — upstream
  know what to test against. The old "INTENTIONALLY RED" line below is
  SUPERSEDED.
```

minimatch (go/minimatch/, module ollitex, oracle-accepted):
  - DONE: escape/minimal/braceexpand/classparse/reengine (35,400)/segast/engine/smoke/oracle.
  - ACCEPTANCE RESULT (09-19): 49,828 rows (match_oracle 7,854 | segM 27,360 | full26 3,400 |
    diff26 1,334 | segPortion 9,924 | + testoracle 35,400) — 0 mismatches. Coverage 78.7%
    (oracle is the gate; gate is per-CLSI-package).
  - REMAINING (optional): wire into resourcewriter PRECIOUS_FILE_PATTERN (`{dot:true}`);
    not blocking today (resourcewriter at 93.2% without it).
  - NOT ported (documented divergences, README): fastTest shortcuts, makeRe() (upstream-
    deprecated), Windows paths (CLSI POSIX only), `\p{X}` byte-approx (paths ASCII).
  - LOCKED contract: `New(pattern, *Options) (*Minimatch, error)`, `.Match(f)`, `.MatchList`,
    module `Match(f,pat,*Options)`, `HasMagic`; escape/unescape/expand exported;
    RE2-no-lookahead guards proven equivalent at compile-time (session 7 §5 note).

otc (LIB-15, shared `ollitex` module at repo root `go/libraries/otc/`):
  - clsi/ot (old CLSI-local port of @overleaf/overleaf-editor-core) was DROPPED per user
    directive 2026-09-21 ("reuse go/libraries shared code to reduce maintenance surface").
    Commit 83e7967 deleted clsi/ot and attached the safe_pathname oracle (80,782 rows,
    Node-generated from /tmp/otfuzz) to otc: `go/libraries/otc/safe_pathname.go` +
    `safe_pathname_oracle_test.go`. Upstream go/otc built + green on this machine.
  - otc import path from clsi module: `ollitex/go/libraries/otc` (replaced ../../ → repo
    root module `ollitex`). Consumers: clsi/resourcewriter (and HRW, in write).
  - Phase C history (upstream go modules, via merged main): slices 2–6 + minimatch +
    Phase C1/C2/C3/C4 + Phase B4 all merged (d4f9c24 merge).
  - Phase B (BlobStore) NOW PORTED (was OPEN upstream): `blob_store.go` with
    `BlobStore` interface + `BaseBlobStore{FetchString, PutStringFn, PutObjectFn}`
    (GetBlob default nil; GetString "" for empty hash else FetchString;
    GetObject JSON.parse or {}). HRW extends it with the 4-branch filestore URL
    logic exactly as Node's `class BlobStore extends BlobStoreBase`.
    `NewBlobNotFound(hash)` = blob <hash> not found.
  - safe_pathname oracle: 80,782 rows — GREEN (fixed + accepted 2026-09-24, commit
    b74bb81; 3 defect classes: U+FEFF whitespace, per-UTF-16-unit counting, V8
    line-terminator guard). Spec + acceptance record: go/libraries/HANDOFF_SAFE_
    PATHNAME.md. CLSI-side replica: services/clsi.go/safepathname_oracle/. Everything
    else in otc is green.

CLSI remaining modules (in §1 order):
  - historyresourcewriter (869L) — DONE (639+491 L, build+vet clean, 27 scenario
    tests green @ 94.0%; coverage gate MET per §10.9: clsi-cache populate,
    missing-updates rethrow, draft, tikz, nested dirs, changesFromRaw, fetchString
    404, fullSync, defaultPngConvert bridge all exercised; all 35 pkgs ≥ 90%).
  - compile core: compilemanager (1021L) + compilecontroller (491L) — deps ALL met
    (dockerrunner DONE otc B DONE); compilemanager uses HRW Result.
  - error middleware port (from app.js `err` handler — see §4/12.2)
  - apps/server (route table §3.2) + load agent (TCP 3048 + HTTP 3049)
  - cmd/clsi main + Makefile + live smoke (Docker texlive image PRESENT — pulled)
  - clsi_typst.go (NEW — port services/clsi_typst to clsi_typst.go, feature-equivalent
    to clsi.go, reuses go/libraries/otc (LIB-15) not a 1:1 copy; see next-session notes
    §8)
  - Node parity matrix (acceptance fixtures goldens — deferred to final)

**Takeover procedure:**
1. `cd /tmp/clsi_proj/services/clsi.go && go build $(go list ./... | grep -v '/dockerrunner$')`
   + `go vet` + `go test $(go list ./... | grep -v '/dockerrunner$') -count=1 -cover`.
2. Skim §1 table; top open row = next task. **§4 = locked decisions (no re-derive).**
3. `todo list` (Pi todos) — filter by tags `clsi`/`go-port`.
4. `/tmp/clsi_proj` symlink may not exist in a new container: recreate with
   `ln -sfn /home/davrot/compile/OlliTeX_comp /tmp/clsi_proj`.
5. **Path is CASE-SENSITIVE (`OlliTeX_comp`, TeX!)** — a bit-flip in the path (e.g. `Ollitex_comp`)
   creates a stray shadow dir / ENOENT; this is NOT NFS flakiness. Large writes: stage
   to `/tmp/` then ONE atomic `cp` + `gofmt` + `go build` + `go test` (Write tool
   truncates >~300 lines).

## 1. Task List

| # | Todo id | Task | Status |
|---|---------|------|--------|
| 0 | TODO-9e50c129 | Node baseline (unit + acceptance) as parity reference | RESTORED 2026-09-24 (`.pnp.cjs` repaired + `yarn install` re-run; `yarn vitest run` executes — suite is harness-red, HRW oracle 8/9 green; detail in §9 "Node oracle run" + Blocked note). Practical spec = passing subset + Go port. |
| mm | (oracle) | minimatch port | ACCEPTED (a518e9c pushed; 49,828 oracle rows; 78.7% cover; divergence README'd). |
| 1 | TODO-e5399221 | config (175L) | DONE 93.0% (gate 90% MET — remeasured 2026-09-24). |
| 2 | errors/req/lp/logger | errors, requestparser, lastprojectaccess, logger | DONE: errors 98.4%, requestparser 93.7%, lastprojectaccess 100.0%, logger 100.0. |
| 5 | TODO-d9502cb0 | Output side: clsicache (528L), contentcachemanager (447L), outputcachemanager (688L) | DONE — clsicachehandler 93.9%, contentcachemanager 96.7%, outputcachemanager 92.1%. |
| 6 | TODO-fac1ee46 | Content cache: resourcewriter, historyresourcewriter, urlcache, urlfetcher | DONE 2026-09-24 — urlcache 96.6%, urlfetcher 93.8%, resourcewriter 93.2%, historyresourcewriter 94.0% (coverage gate MET 94.0% ≥ 90%; §10.8/§10.9 closed). |
| 3 | TODO-6a350144 | Compile core: commandrunner (19L), compilemanager (1021L), compilecontroller (491L) | IN PROGRESS — commandrunner 100.0%; compilemanager + compilecontroller remain (otc B dep NOW met after HRW). |
| 7 | TODO-843bd163 | Conversion: tikzmanager (129L), png2pdf (96L), conversionmanager (936L), conversionoutputcleaner (port) | DONE 2026-09-20: tikzmanager 94.9%, png2pdf 93.4%, conversionmanager 91.6%, conversionoutputcleaner 100%, fileuploadmiddleware 93.9%. |
| 4 | TODO-02cae9d7 | DockerRunner (634L) → `dockerrunner` (hand-rolled HTTP-over-unix Engine SPI) + fake for tests | DONE 2026-09-24 (engine.go + unixengine + pipeline + monitor + fingerprint + FakeEngine = 90.9%). |
| 8 | TODO-d7feecff | Server layer: app (route table), load agent (TCP 3048 + HTTP 3049), /status /health_check /smoke_test_force /metrics, error middleware | OPEN (design §4/12 below). |
| 9 | TODO-f464d516 | cmd/clsi main + Makefile + live smoke + Node parity matrix | OPEN (UNBLOCKED: texlive/texlive:latest-full present). |

Coverage log (strict per-package ≥ 90%; 2026-09-24 remeasure — 34 packages).
NOTE: metrics/errors values below are PRE-a7d911d; after a7d911d (HRW-prep)
metrics = 74.1% and errors = 87.5% pending the HRW coverage tests (§10.9).

| package | cover | package | cover |
|---|---|---|---|
| clsicachehandler | 93.9 | outputcontroller | 100.0 |
| commandrunner | 100.0 | outputfilearchivemanager | 91.5 |
| config | **93.0** ✅ | outputfilefinder | 98.2 |
| contentcachemanager | 96.7 | outputfileoptimiser | 97.7 |
| contentcachemetrics | 93.1 | png2pdf | 93.4 |
| contentcacheworker | 100.0 | requestparser | 93.7 |
| conversionmanager | 91.6 | resourcestatemanager | 94.6 |
| conversionoutputcleaner | 100.0 | resourcewriter | 93.2 |
| dockerlockmanager | 93.3 | safereader | 94.2 |
| draftmodemanager | 93.1 | statsmanager | 100.0 |
| errors | **98.4** ✅ | synctexparser | 93.9 |
| fileuploadmiddleware | 93.9 | tikzmanager | 94.9 |
| lastprojectaccess | 100.0 | urlcache | 96.8 |
| latexmetrics | 91.9 | urlfetcher | 96.2 |
| latexrunner | 95.6 | xrefparser | **97.8** ✅ |
| lockmanager | 100.0 | logger | 100.0 |
| metrics | **100.0** ✅ | dockerrunner | 90.9 |

**Coverage gate: 34/34 pkgs ≥ 90%.** The four previously-below packages (errors,
 xrefparser, config, metrics) were lifted by thin tests on 2026-09-24; dockerrunner
 first measured at 90.9%.

Coverage gate cmd: `cd services/clsi.go && go clean -testcache -cache && go test ./... -cover`.

**IMPORTANT**: `go clean -testcache -cache` between coverage generations (stale profiles).

## 2. Environment

- Go **1.27.1** at `/usr/local/go`. Node 24.13.0, Docker Engine 29.5.3 (ApiVersion 1.54,
  MinAPIVersion 1.40 — old-style routes). gcc 15.2.0.
- Docker backbone for CLSI: **hand-rolled HTTP-on-unix client** (zero-dep policy, §4/1).
- Docker socket: `/var/run/docker.sock` (probe:
  `curl -s --unix-socket /var/run/docker.sock http://localhost/containers/json`).
- Docker images available: `texlive/texlive:latest-full` (2.74GB, pulled
  2026-09-24 — live compile smoke UNBLOCKED), `test_unit_clsi-test_unit:latest`
  (1.7GB), `hello-world` (25MB).
- Node 24 / yarn 4: PnP **REPAIRED 2026-09-24** (`.pnp.cjs` restored + `yarn
  install` re-fetched 1.64 GiB into local gitignored `.yarn/cache`) — `yarn
  vitest run` runs (suite harness-red, see §9); Docker run works via dockerode
  at 1.7GB.
- **/tmp/otto/** sandbox (d3.js + fuzzer) used for otc oracle generation + minimatch
  regression.

### 2.1 Parity reference without the suite running
- `services/clsi/test/unit/js/*.test.js` (vitest) — 19 files + fixtures.
- `services/clsi/test/acceptance/js/*.js` (mocha) — 15 files + `fixtures/examples/*`
  (23 LaTeX projects with `output.pdf` + `output.pdfxref` + `options.json`).
- `services/clsi/test/smoke/js/SmokeTests.js`.

### 2.2 CCM oracle fixtures (node v24 probe, MINIMAL.PDF — byte-exact verified)
- fixture: `services/clsi/test/acceptance/fixtures/minimal.pdf` (12313 B)
- minChunk=500 → obj "9 0 " start 1074 end 11235 (10161 B, hash `d7cfc73a...`),
  obj "10 0 " start 11240 end 11784 (544 B, hash `896749b8...`)
- minChunk=1024 → only obj 9 (10165 ≥ 1024; obj 10 size 549 < 1024)

## 3. Architecture Map (Node → Go)

```
Node service
  app.js                → apps/server (express routes, timeouts, error middleware, load agent)
  config/settings.defaults.cjs → config/config.go (DONE)

Controllers (HTTP edge)
  CompileController.js (491L)   → compilecontroller.go
  OutputController.js  (31L)    → outputcontroller.go (DONE 100%)

Compile core
  CompileManager.js (1021L)     → compilemanager/ (deps ALL met: dockerrunner + otc B)
  HistoryResourceWriter.js (869L) → historyresourcewriter/ (DONE 2026-09-24, 94.0%, 27 scenario tests, design §10)
  ResourceWriter.js (398L)      → resourcewriter/ (DONE 93.2%, otc wired)
  DockerRunner.mjs (634L)       → dockerrunner/ (DONE 90.9%, §4/12)
  OutputCacheManager.js         → outputcachemanager/ (DONE 92.1%, §5/7 LOCKED)
  OutputFileFinder/Optimiser/
  ArchiveManager                → outputfilefinder/ (DONE 98.2%)/
                                  outputfileoptimiser/ (DONE 97.7%)/
                                  outputfilearchivemanager/ (DONE 91.5%)
  OTC → go/libraries/otc (shared LIB-15; Phase B DONE; applied via
                          clsi/resourcewriter + historyresourcewriter (otc
                          BlobStore))
  Content / URL cache
  CLSICacheHandler.js (528L)    → clsicachehandler/ (DONE 93.9%)
  ContentCacheManager.js (447L) → contentcachemanager/ (DONE 96.7%)
  UrlCache.js (227L) / UrlFetcher.js (111L) → urlcache/ (DONE 96.8%) /
                                           urlfetcher/ (DONE 96.2%)

Locking / persistence
  LockManager.js                → lockmanager.go (DONE 100%)
  LastProjectAccess.js          → lastprojectaccess.go (DONE 100%)
  ContentCacheMetrics/Worker    → contentcachemetrics.go (DONE 91.5%)/
                                  contentcacheworker.go (DONE 100%)

Conversion path (added 2026-09-20)
  TikzManager.js (129L)         → tikzmanager.go (DONE 94.4%)
  Png2Pdf.js (96L)              → png2pdf.go (DONE 92.9%)
  ConversionManager.js (936L)   → conversionmanager.go (DONE 91.4%)
  ConversionOutputCleaner.js    → conversionoutputcleaner.go (DONE 100%)
  ContentCacheMetrics.js        → contentcachemetrics.go (DONE 91.5%)
  ContentCacheWorker.js         → contentcacheworker.go (DONE 100%)
  LatexRunner.js (404L)         → latexrunner.go (DONE 94.3%)
  CLSICacheHandler.js           → clsicachehandler.go (DONE 93.9%)
  CLSICompileQueue.js           → (in compilecontroller; not yet ported)
  Metrics.js                    → metrics/ (DONE 100.0%) +
                                   clsi_metrics seam (HRW observes via
                                   metrics.ShouldSkipMetrics)
  LatexMetrics.js               → latexmetrics/ (DONE 91.9%)
```

### 3.1 Route table (must match exactly; `app.js` is the spec)

| Method | Path |
|--------|------|
| POST | /project/:project_id/compile |
| POST | /project/:project_id/compile/stop |
| DELETE | /project/:project_id |
| GET | /project/:project_id/sync/code |
| GET | /project/:project_id/sync/pdf |
| GET, POST | /project/:project_id/wordcount |
| GET, POST | /project/:project_id/status |
| (same 8) | under /project/:project_id/user/:user_id/... (user-scoped) |
| GET | /project/:project_id/build/:build_id/output/output.zip |
| GET | /project/:project_id/user/:user_id/build/:build_id/output/output.zip |
| POST | /convert/docx-to-latex (multipart) |
| POST | /convert/document-to-latex (multipart) |
| POST | /project/:project_id/user/:user_id/download/project-to-document |
| POST | /convert/pdf-to-jpeg (multipart) |
| GET | /status → "CLSI is alive\n" |
| GET | /health_check (smoke result / processTooOld / diskCritical) |
| GET | /smoke_test_force |
| POST | /metrics (load agent), TCP load agent `up/down/maint` responses |

Note: `POST /project/:project_id/user/:user_id/download/project-to-document` is a
MUL (multipart) route. All others GET/POST plain.

### 3.2 Server structure (apps/) for `cmd/clsi` (design LOCKED this session)
```
apps/
  main.go          — env + config + logger startup; signal handling; mux
  server.go        — New(...) builds HTTP handlers; injects compilemanager,
                     outputcontroller, clsicachehandler, conversionmanager,
                     loadagent (TCP+HTTP); mounts mux on :3049 (or env HTTP_PORT)
  loadagent.go     — TCP listener on :3048 (default), reads newline-terminated
                     commands `up`/`down`/`maint` → 200 "up"/"down"/"maintenance"
  error.go         — middleware: err → "CLSI server error (internal error: <m>)"
                     500 or "Bad request: (m)" 400 (exact format below)
  health.go        — /status /health_check /smoke_test_force
```

`err` handler (exact format from app.js):
```js
if (err.statusCode && err.statusCode >= 400 && err.statusCode < 500) {
  res.status(err.statusCode).send(`Bad request: (${err.message})`);
} else {
  logger.err(...);
  res.status(500).send(`CLSI server error (internal error: ${err.message})`);
}
```

TCP load agent (from app.js L157-195): 3-line command loop
`command.replace(/[^\w]/g, ' ')` filter on `up|down|maintenance` → respond
`'up'`/`'down'`/`'maintenance'` (each `\n`-terminated, 500 status on unrecognized);
connection-close on error.

## 4. Decisions (LOCKED — no re-derive without evidence)

1. **Docker backbone**: hand-rolled HTTP-on-unix client for the ~10 endpoints used
   (container create/start/kill/wait/logs, volumes list, image inspect). No docker
   SDK dep. **dockerrunner** package (renamed from `dockerclient`). Uses
   dockerode-compatible wire format (probe-verified: POST create uses
   `Content-Type: application/json`, `name` from QUERY STRING, config from JSON
   BODY; `wait` is POST; attach is `POST /containers/{id}/attach?stdout=1&
   stderr=1&stream=1`).
2. **express mux** → `http.ServeMux` (Go 1.22+ patterns `{...}`). Add 404 shim
   (express returns 404 for method-mismatch, Go mux returns 405).
3. **multer** → `http.Request.ParseMultipartForm` + explicit size limits (maxUploadSize
   50MB). For /convert/* routes.
4. **p-limit** → semaphore channel (or sync.Pool for CLSICompileQueue).
5. **workerpool** → single goroutine + job queue (1 worker per Node; V8
   worker_threads don't exist).
6. **archiver** → `archive/zip` + Go `os/exec` for tar.
7. **bunyan** → `log/slog` JSON (LOG_LEVEL env via config.ResolveLevel). Logger
   API: `logger.Debug/Info/Warn/Error/Err(obj map[string]any, msg string)`
   (package-level funcs in levels.go).
8. **Metrics** → internal counters + `/metrics` text matching `Metrics.inc` +
   `Metrics` gauges. NOT prometheus. `metrics.Counter{Name, Value int64, Mu}`,
   `Gauge{Name, Value float64, Mu}`; `PdfCachingStatus *Counter`; `Inc()` per-
   counter.
9. **smoke test**: port behavior only if SMOKE_TEST env on (default off).
10. **Tests**: `testing` + `httptest`; `TestMain` must use `os.Exit(m.Run())`
    (NOT `return m.Run()`); fake Docker Engine for compile-manager tests.
    Golden PDF assertions for acceptance fixtures. Coverage gate: ≥90% per
    CLSI package (except where explicitly documented as oracle-accepted or
    divergence-accepted).
11. **otc (LIB-15)**: use shared otc from `go/libraries/otc` (module `ollitex`).
    clsi/resourcewriter + historyresourcewriter import `ollitex/go/libraries/otc`.
    Do NOT re-derive the clsi/ot port (deleted; commit 83e7967).
12. **dockerrunner** (renamed from `dockerclient` this session; design locked):
    Files: `dockerrunner.go` (struct + Runner API `New(...) *DockerRunner`),
    `engine.go` (Engine SPI: HTTP verbs on unix socket; `Create(id, opts)`,
    `Kill(id)`, `Start(id)`, `Attach(id) io.ReadCloser`, `Destroy(id)`),
    `fingerprint.go` (md5 of opts JSON for content-cache key).
    Runner interface (already defined in `commandrunner/commandrunner.go`):
    `Run(projectID, command, directory, image string, timeout int64, env map[string]
    string, compileGroup string, callback func(error, *RunOutput)) string` +
    `Kill(containerID string, callback func(error))` + `CanRunSyncTeXInOutputDir()
    bool`. `RunOutput{Stdout, Stderr string, ExitCode int, Terminated, Exited,
    TimedOut bool}`.
    Lock: **dockerlockmanager.RunWithLock** (NOT lockmanager — compile-side
    lock uses different manager). See §8 for runOnce gate model.
    Tests: FakeEngine (in-memory, records state) driven; NO real Docker in tests.

## 5. Progress Log

- [x] (session 1) Scaffold: go.mod, HANDOFF, task breakdown. Read all Node source.
- [x] (session 2) 20 leaf packages ported to ≥ 90% coverage each.
- [x] (session 3) V8 `new Date(string)` parser fully ported into requestparser (v8date.go)
  from the full V8 source tree; 241-case differential oracle (node v24, TZ=Europe/Berlin,
  /tmp/v8oracle.tsv) + waves 2–4 all pass.
- [x] (session 4) outputcontroller (31L) at 100%: CreateOutputZip(res http.ResponseWriter,
  outputDir string, req Request) (int, error). 404→NotFoundError shape (no body), success→
  200 + `Content-Disposition: attachment; filename="output.zip"` +
  `X-Content-Type-Options: nosniff` + Content-Length.
- [x] (session 5) contentcachemanager (CCM, 447L) COMPLETE at 96.7%. Design:
  *port updateSameEventLoop as main path* (worker pool is V8-specific).
  `Update(a UpdateArgs) (res *UpdateResult, err error)`,
  UpdateArgs{ContentDir, FilePath, PdfSize int64, PdfCachingMinChunkSize int64, CompileTime float64},
  UpdateResult{ContentRanges, NewContentRanges []ContentRange, ReclaimedSpace int64,
  OverheadDeleteStaleHashes *int64, TimedOutErr error, StartXRefTable *int64}.
  Deadline = min(max(compileTime/4, 1000), pdfCachingMaxProcessingTime) ms.
  `tracker.flush()` runs in finally (both success AND soft-failure after open);
  not called on pre-open errors. `objectIdRaw` split uses 3-byte substring search
  (`bytes.Index(buf, []byte("obj"))`); no-obj branch: rawLen=len(buf)-1, Start=offset+rawLen.
  Soft timeout only inside loop (pre-loop breaches = hard error).
  Error sites: `&clserrors.OError{Message, Info}` for "objectId is too large" and
  "could not read full chunk". `xrefparser.ParseXrefTable` errors wrapped as
  `clserrors.NewNoXrefTableError(parseErr)`. **Exported**: `xrefparser.IsNoXrefError(err)`.
  ContentRange type exists in outputfilefinder AND CCM (CCM keeps its own; no import cycle).
- [x] (session 6) outputcachemanager (OCM, 688L Node source) COMPLETE at 92.1% (2026-09-17).
- [x] (session 7) minimatch reengine.go (724L) DONE (2026-09-18): closed-dialect RE engine
  (forward-set position evaluator, nSeq/nAlt/nChar/nClass/nAnyDot/nAnchorStart/nAnchorEnd/
  nNegLook; CompileRe + Test; idEscapeOK dialect whitelist; closedEval bounded {1,2} +
  unbounded fixed-point closure; pos-based closing-`]` scan for classes; \p{X} via
  unicode tables). Oracle testoracle.tsv 35,400/35,400 rows = V8 ground truth (472 distinct
  (src,flags) pairs x 75 files). reengine_test.go: oracle + hand-verified edge cases +
  dialect-error loud failures. Package coverage 75.3% (reengine error branches +
  unescape 0% at that point — see next).
- [x] (session 7b) escape/unescape MERGE (2026-09-18): old unescape.go DELETED (contained
  dead pass1look/pass1Simple/pass2strip that had drifted from upstream — 3 implementations
  of one function = drift hazard). escape.go now holds BOTH Escape (4 combos: default/
  wpne/mb/wpne+mb; upstream escapes * ? ( ) [ ] \ { } per combo — 7 chars, NO
  over-escape) and Unescape (upstream 3-regex pipeline: pass1 lookaround ((?!\\).|^)\[
  (X)\] with X excluding \ / and (braceVariant) braces, ported as true V8 leftmost-position
  scan — NOT a greedy no-overlap loop; pass2 \\X strip with braceVariant; wpne single-wrap
  mode). CRITICAL BUG FIXED: pass2 originally used `for i:=0; i<len; i++ { i+=2; continue }` =
  double-increment (skipped char after every consumed pair) → `[` boundary corruption
  in inputs like `[[]a[[]`, `a\\\\b`. esc_oracle.tsv 157 rows (single chars 0x00-0x7E +
  43 multi-char patterns incl. `[[...`, `\\\\p{L}`, `{1,2}`, `[a[b]c[d]e`, `[]{}]` etc.) x4
  escape combos + 4 unescape combos (base64 encoding, NOT hex — hex columns collided in
  TSV) ALL GREEN. assertValidPattern/PatternLimit re-added to escape.go after deletion.
  Design (formerly §7) verified against Node 1:1; injectable seams (Now/RandHex/
  UpdateContent/OptimiseFile/ScheduleAfter/MetricsInc/Log) + New() production factory.
  Key behaviors captured: per-dir pump queue (QueueDirOperation generaic, EnqueueDirOperation
  fire-and-forget), ENOENT→cleanupAll, all-expired→cleanupAll, perUser compileDir regexp
  ^[0-9a-f]{24}-[0-9a-f]{24}$ → expire {keep, limit:1}, dark-mode CCM (stats yes, file
  annotation NO), Metrics 'pdf-caching-status' BEFORE error check, archiveLogs fire-and-
  forget (Strace/ArchiveLogs), scheduleBulkCleanup delay=max(CACHE_AGE+oldest-now,0)+60000,
  nodeParseInt16 (0x hex prefix, sign, leading-ws skip), fileHidden ^\.|\/\./, buildId
  hexdate-hexrandom. 60+ tests covering copy/archive/ensureContentDir/cleanupAll error
  seams directly. Test file ≈31KB, staged via /tmp + cp (big-writes-safe).
- [x] (session 8) minimatch PORT COMPLETE (2026-09-19): segast.go (AST parsePortion +
  toRegExpSource: flatten adopt/adoptWithSpace/usurp 10-pass, fillNegs, guards, extglob arms
  incl. `|` alternation + negated `!(...)` end-guard `(?:$|/)` on ROOT filledNegs) and
  engine.go (Minimatch: exported New(f, o *Options) (*Minimatch, error), Match(f string) bool,
  MatchList([]string) []string, HasMagic() bool + module-level Match(f, pattern, o) (bool, error);
  internal make, preprocess levels 0/1/2, tri-value matchGlobstar + body sections, GLOBSTAR
  sentinel, partial/flipNegate/matchBase) + oracle_test.go + mm_smoke_test.go + README.md. Bugs fixed this session (each
  traced to Node source via oracle differs):
    (a) `beHasBracePattern` returned false on nested `{a,{b,c}}` -> now `break`s inner `{`
        and tries next open brace (JS `\{(?:(?!\{).)*\}` semantics).
    (b) matchGlobStarBodySections: `fileParts` view must be TRUNCATED to `remaining` (file
        parts past last GLOBSTAR not touched by engine) + `badDot` check on `.`-leading
        portion restored (JS `#matchGlobStarBodySections`, byte-confirmed 09-19).
    (c) end-guard reads `a.root.filledNegs` (JS `this.#root.#filledNegs`), not `a.filledNegs`.
    (d) closeStr: `+`/`*` chain with bodyDotAllowed closes the group WITHOUT re-emitting the
        type (JS `closeStr` byte-confirmed from compiled source).
    (e) `|` (alternation): acc must RESET between body nodes (JS `#acc` is per-branch — each
        `body.toRegExpSource()` call uses a fresh `#acc`); the single-shared-acc version
        diverged on every `{a|b}{c|d}`-style pattern (segPortion 9,924 rows proved the fix).
    (f) Stale oracles: testdata/ diff26.tsv + full26.tsv were regenerated from a broken
        generator (`{dot:d}` where `d` was the result array -> every row dot:true);
        regenerated with `{dot:dot===1,platform:'posix'}`. Independent fresh recomputation
        (39,947 rows) confirmed 0 ground-truth mismatches before final testdata refresh.
  ACCEPTANCE: 49,828 oracle rows (testoracle 35,400 | segM 27,360 | match_oracle 7,854 |
  segPortion 9,924 | full26 3,400 | diff26 1,334 + escape 157+95 + expand 95 — totals:
  49,828 MATCH-path rows) = 0 mismatches. go build/vet clean; CLSI module builds.
  NOT ported (documented divergences, see go/minimatch/README.md): fastTest shortcuts
  (compile-time only, same dot choice as RE path), makeRe (deprecated, unused by CLSI),
  Windows (POSIX-only; Go rejects Platform!=posix), `\p{X}` byte-approx (ASCII paths),
  nocase byte-fold (CLSI case-sensitive). Coverage 78.7% — oracle-accepted (the HANDOFF
  >=90% gate applies per CLSI package; minimatch is oracle-validated instead).
- [x] (session 9) commandrunner (19L) + resourcewriter (398L) PORT COMPLETE (2026-09-19).
  *commandrunner* at 100.0%: New(dockerRunner, runner, logger) (Runner, error) — the Node
  `process.exit(1)` guard is an error return (caller refuses to start); logs the refusal
  (dockerRunner=false) and selection messages via the logger seam. *resourcewriter* at
  93.2% (gate met): IsExtraneousFile (keep-regex order + precious minimatch {dot:true} +
  forced-extraneous list), CheckPath (POSIX path.Join + basePath+"/" prefix guard — the
  prefix-attack '../foobar/baz' is caught), DeleteFileIfNotDirectory (os.Stat follows
  symlinks; ENOENT=ok, regular file removed; errors propagate), CreateDirectory (non-
  recursive os.Mkdir, EEXIST swallow), writeResourceToDisk (MkdirAll dirname; remote URL ->
  DownloadUrlToFile with conversionSuffix="" + **error SWALLOWED** -> logger.Err +
  metrics.IncDownloadFailed; content -> os.WriteFile, error propagates), RemoveExtraneous
  Files (findOutputFiles -> delete each IsExtraneousFile path), SaveAll/Incremental, and
  SyncResourcesToDisk (full = createProjectDir + saveAll + saveProjectState; incremental =
  checkProjectStateMatches + removeExtraneous + checkResourceFiles + saveIncremental). The
  minimatch port is WIRED IN here: the precious-file glob (PRECIOUS_FILE_PATTERN, {dot:
  true}) is the precious branch of IsExtraneousFile; CLSI default '' is inert. Injectable
  seams: DownloadFile / WriteFile / FindOutputFiles + InitPreciousFileMatcher (the lazy
  ensurePreciousMatcher derives from config only when the package is not initialized).
  18 tests mirror Node ResourceWriter.test.js (the full 15-case isExtraneousFile table +
  the 3 checkPath cases + full/incremental sync + swallow-on-download-error + the prefix-
  attack guard). The default urlcache download closure is exercised via a seeded CLSI
  cache (no HTTP).
- (sessions 7+) per §0 "NOT yet ported" order.
- [x] (session 10) **ot package: safepathname oracle GREEN + extend 2026-09-21.**
  *safepathname.go* (463L) oracle-verified against the Node lib at 80,782 fuzz rows
  (ot/testdata/spfuzz2.json, generated via the real
  `overleaf-editor-core/lib/safe_pathname.js` cleanDebug output). All 10 pipeline
  stages byte-exact, incl. the V8 regex quirks:
    - V8 `$` (non-multiline) = TRUE END ONLY (position == len; NOT before a trailing
      line terminator) — Go regex `^...$` is NOT the same; each stage re-implemented
      as an explicit scan with the stage-specific guard set.
    - **DOT EXCLUSION = {0x0A, 0x0D, 0x2028, 0x2029}** (the 4 line terminators only).
    - **`[^ ]` CAN match line terminators** (negated char classes are not dot-excluded).
    - Effective `\s` for BAD_FILE = {0x20, 0xA0, 0x1680, 0x2000-0x200A, 0x2028, 0x2029,
      0x202F, 0x205F, 0x3000, 0xFEFF}; NOT in \s: 0x180E, 0x180F, 0x200B.
    - BAD_CHAR = `/[/*\\u0000-\u001F\u007F\u0080-\u009F\uD800-\uDFFF]/g`.
    - Stages 4-7 leading/trailing guards (see §5b below for the FINAL models).
  *scanop.go* extended: RetainOp/InsertOp/RemoveOp now carry Equals/CanMergeWith/
  MergeWith/ApplyToLength (merges MUTATE the receiver, mirroring JS `this`); InsertOp
  ApplyToLength uses utf16Len (surrogate pair = 2 units); InsertOp equals via
  equalStringSlices(commentIDs). *blob.go* new: Blob{hash/byteLength/stringLength}
  with NewBlob/BlobFromRaw + 40-hex validator (check-types panics) +
  BlobStore{FetchString seam} mirroring BlobStoreBase (EmptyHash short-circuit, the
  zero-value FetchString = unimplemented-abstract error, GetObject via encoding/json).
  *filemetadata.go* new: DOCUMENT_METADATA_KEYS, IsDocumentMetadata,
  HasDocumentMetadataFlag, WithDocumentMetadataFlag (map[string]any →
  map[string]bool; absent flag left out rather than false). *errors.go* patched:
  ApplyError now carries (msg, operand, resultLength) matching errors.js signatures.

### 5b. ot package — LOCKED DESIGN NOTES (2026-09-21, session 10) [continued below]

Scope: apply-path only. transform/compose/invert/rebase/diff NOT ported. Files live in
`services/clsi.go/ot/` (one Go package `ot`). Cross-package import path: `clsi/ot`.

#### safepathname FINAL stage models (VERIFIED 80,782/80,782 rows)
Work in UTF-16 code units (`utf16.Encode`); the lib operates on `str.length`.
Pipeline (10 stages, each records its label ONLY if the value changed):
```
1. normalize(path) [path-browserify]   -> "normalize": "//" / "\.//" / "a/.//" etc.
2. \\ to /                            -> "workaround for IE"
3. /{1,2}+ to / (run of >=2 slashes)  -> "no multiple slashes"
4. ^(/.*)$ -> "_$1" (prepends _ keep-slash; fires on single "/" = len>=1 AND
   no LT in units[1:])                    -> "no leading /"
5. ^(.+)/$ -> strip trailing / (len>=2, units[len-1]=='/' AND no LT in units[:len-1])
                                                    -> "no trailing /"
6. leading strip: k = count of leading 0x20; match iff k>0 AND no LT in units[k:]
                                                    -> "no leading spaces"
7. trailing strip: j = index past last non-0x20; match iff j>0 AND !LT(units[:j-1])
   <- guard is j-1 NOT j (last non-space char covered by [^ ] which CAN match LT)
                                                    -> "no trailing spaces"
8. if strlen==0 -> "_"                     -> "empty"
9. cleanPart each part (full-string "."/".." skip; trailing "." or"..." gets
   prep ".." -> "/" ; else BAD_CHAR->_, BAD_FILE \s+$->_)  -> "cleanPart"
10. BLOCKED_FILE_RX (^...$ anchored on ENTIRE pathname) -> "@$1"  -> "BLOCKED_FILE_RX"
```
**V8 gotchas (DEFINITIVE, probed):**
- V8 `$` (non-multiline) matches ONLY at true end — NEVER before a trailing line
  terminator (\r\n, \u2028/29). Go `^...$` is NOT the same; each stage is a manual
  scan with a stage-specific guard set.
- DOT EXCLUSION = {0x0A, 0x0D, 0x2028, 0x2029} ONLY. `.` never matches any LT.
- Negated classes (`[^ ]`) CAN match LT (not dot-excluded).
- Effective `\s` for BAD_FILE = {0x20, 0xA0, 0x1680, 0x2000-0x200A, 0x2028, 0x2029,
  0x202F, 0x205F, 0x3000, 0xFEFF}; NOT in \s: 0x180E, 0x180F, 0x200B.
- BAD_CHAR = `[/*\u0000-\u001F\u007F\u0080-\u009F\uD800-\uDFFF]` (each -> "_").
- cleanDebug returns ARRAY [pathname, reason] (result[0]/result[1]), NOT object.
cleanPart all-space part: leading `\s+` eats all, trailing `\s+$` void.
cleanPart dot-parts: only FULL-STRING "." and ".." match, not prefixes/suffixes.

#### scanop.js contract (scanop.go)
- equals: same concrete type; Retain length+tracking; Insert insertion+tracking+
  commentIDs; Remove length.
- canMergeWith: types match; both-nil OR both-mergeable tracking; Insert
  commentIDSSequal/both-nil.
- mergeWith: MUTATES receiver (`this.length += other.length`); panics on
  incompatible merge.
- applyToLength(ctx): ctx {Length, InputCursor, InputLength}; Retain/Remove overrun
  -> ApplyError{msg, operand=op.toJSON(), resultLength}.
- toJSON: {type:'retain'|'insert'|'remove', ...}; fromJSON dispatches isRetain/
  isInsert/isRemove.
- ScanOp BASE (abstract): canMergeWith always false; mergeWith panics. Concrete
  override on each struct.

#### tracking.go (NEEDS EXTEND — current: tracking.go 99L basic only)
Add `TrackingDirective` interface (used by scanop merge + comment_list
applyTrackedChanges): canMergeWith/mergeWith/equals + isClear/isInsert/isDelete
helpers. Source: file_data/tracking_props.js + clear_tracking_props.js.

#### blob.go (DONE 2026-09-21)
- Blob{hash 40-hex, byteLength, stringLength *int}. Panics on bad hash/byteLength
  (check-types -> fmt.Errorf panic).
- BlobFromRaw(hash, byteLength, stringLength *int): nil when hash=="".
- BlobStore{FetchString func(hash) (string, error)}: GetString short-circuits
  EmptyHash to "" (no fetch); nil FetchString = unimplemented-abstract error;
  GetObject via encoding/json (empty string -> json error mirrors JSON.parse('')).

#### filemetadata.go (DONE 2026-09-21)
IsDocumentMetadata / HasDocumentMetadataFlag / WithDocumentMetadataFlag as in
source (cleared flag = LEFT OUT, not false).

#### Remaining files to port (apply-path) — dependency order
comment.go (needs Range methods incl. ExtendBy/MoveBy/InsertAt/Subtract/StartsAfter
/Overlaps), comment_list.go (file_data/comment_list.js), v2_doc_versions.go,
file_data/tracked_change.go, file_data/tracked_change_list.go, filedata.go +
file_data/string_file_data.go (hash_file_data/hollow_* may be skipped — apply-
path only), text_operation.go, edit_operation.go, operation/ (index.js +
add_file/delete_file/move_file/edit_file/add_comment/set_comment_state/
set_file_metadata/delete_comment/edit_no/no_operation — 14 op files), file.js,
file_map.js (plain Go map[string]*File; getPathnames = sorted keys), change.js
(applyTo/applyToChange ONLY — skip transform/rebase/compose/invert), snapshot.go
(default projectVersion "1.0"), + oracle_test.go (35 cases from
/tmp/otto/ot_dataset.json).

#### Key Go conventions
- Range.Pos = *int (nil = JS undefined). toRaw: pos=null when nil.
- InvalidInsertionError message FIXED = "inserted text contains non BMP
  characters" (no interpolation; str carried as field).
- UTF-16 length = utf16Len (already in util.go) for op length math.
- All ot files: same Go package `ot` (no sub-imports).
- Blob EMPTY_HASH and util EmptyHash are THE SAME VALUE (two consts, mirrors
  Blob.EMPTY_HASH and File.emptyFileHash).
- Timestamp model (CHANGE TO SNAPSHOT/COMPILEDCHANGE): store opaque STRING;
  echo verbatim in toRaw; "0" -> "1999-12-31T23:00:00.000Z" (V8 epoch-0 quirk).
- FileMap = map[string]*File + sorted getPathnames (Go maps unordered).
cleanDebug oracle fixture: ot/testdata/spfuzz2.json (80,782 rows).
ot_dataset oracle fixture: /tmp/otto/ot_dataset.json (35 rows) -> to be copied
  into ot/testdata/ when oracle_test is written.


## 5.x Late updates (appended 2026-09-22 → 2026-09-24)

- [x] (2026-09-22) **Conversion path + cache handlers DONE** (all ≥ 90%): tikzmanager 94.4%,
  png2pdf 92.9%, conversionmanager 91.4%, conversionoutputcleaner 100%, contentcachemetrics
  91.5%, contentcacheworker 100%, clsicachehandler 93.7%, latexmetrics 91.1%, latexrunner 94.3%,
  fileuploadmiddleware 93.9%. commandrunner 100%.
- [x] (2026-09-21) **clsi/ot DELETED (commit 83e7967)**: user directive "reuse go/libraries
  shared code" → clsi/resourcewriter + (later) historyresourcewriter import shared
  `ollitex/go/libraries/otc` (LIB-15). safe_pathname oracle (80,782 rows, Node-generated)
  attached to otc at `go/libraries/otc/safe_pathname_oracle_test.go`. The clsi-side §5b
  ot design notes above are SUPERSEDED (they document the abandoned clsi/ot port; otc is
  the source of truth).
- [x] (2026-09-24) Merge upstream main (d4f9c24): otc Phase B4 + Phase C slices 2–6 +
  integrated minimatch port; go/minimatch conflict resolved in favor of upstream.
- [x] (2026-09-24) `config.MaxContainerAge` + env `DOCKERRUNNER_MAX_CONTAINER_AGE` (b609dfb).
- [x] Coverage re-measure (this session): gate MET 34/34 — errors 98.4%,
  xrefparser 97.8%, config 93.0%, metrics 100.0 (thin seam tests added;
  the 4 below-gate numbers above were the pre-merge remeasure).

## 6. Risks / Watch-outs (Go-specific pitfalls hit in past sessions)

- **typed-nil**: `err = func() *TimedOutError { return nil }()` puts a typed-nil in the interface;
  `err != nil` is TRUE. Use `if te := fn(); te != nil { return nil, te }`.
- **gofmt breaks edits**: after `gofmt -w`, alignment changes whitespace; re-read before next edit.
- **`time.Time` has no `<`/`>=`**: use `.After()`/`.Before()`/`!t.After(ref)`.
- **regexp `$` is not line-anchored** without `(?m)` — split lines manually for per-line matching.
- **`os.PathError.Err` is `error`**, not string. **`FindStringSubmatch` takes `string`** not `[]byte`.
- **PATH separator on Linux is `":"`** (not os.PathSeparator). `/usr/bin/sh` must be the absolute
  shebang for fake commands. `touch` may be unavailable in restricted PATH → `: > "$file"`.
- **Node `Path.extname` ≠ `path.Ext`**: hand-rolled `nodeExtname` with preDotState machine.
- **`io.Copy` for stream→file** (not WriteFromReader). **NO `os.CopyFile` in Go** (Open+Create+io.Copy).
- **Node `fs.copyFile` ENOENT = hard error** (compile fails; NOT skip).
- **CCM error sites use `clserrors.OError`** (type fidelity, matching Node's `throw new OError`).
- **Node `deep.equal` ordering**: OutputFileFinder test asserts exact array order; Go must match via
  sorted readdir + dir-after-children walk.
- **Node `url.host.includes(host)`** → `strings.Contains(u.Host, host)`.
- **config.ForTest()** requires `SANDBOXED_COMPILES_HOST_DIR_COMPILES` env when sandboxed compiles
  enabled. **createProjectDir is caller's responsibility**.
- **Coverage profile staleness**: `go clean -testcache -cache` between generations.
- **Named function defaults for testable vars**: `var ScheduleAfter = defaultScheduleAfter`.
- **Go map pointers cannot be indexed**; closures self-referencing need `var f func(...); f = func(){...}`.
- **`Path.extname` on Node** uses preDotState state machine (hand-rolled).
- **`archive_logs`/`strace`**: NOT in settings.defaults.cjs (both undefined → false);
  probe confirms `clsi.optimiseInDocker = true`.
- **Large writes (NOT NFS — mount is fine)**: write large files to `/tmp/` then `cp` +
  `gofmt` + `go build` + `go test` in ONE atomic bash command. **Write tool truncates
  >~300 lines** — use `cat > file <<'EOF'` heredocs, chunked ≤120L with `gofmt -e` after
  each chunk. **`edit` tool on a corrupted file = chaos** — recovery is `cp` from the
  last-known-good and re-apply the single needed fix. (2026-09-24 "NFS flakiness" was
  a case-sensitive path bit-flip — `OlliTeX_comp` vs `Ollitex_comp` — NOT a mount issue.)
- **ALWAYS use `timeout`** on bash commands that touch node/ or v8/ (50k/20k entries).
- **minimatch Go pitfalls (session 7, 2026-09-18)**:
  - **pass2 double-increment** (`for i:=0; i<len; i++ { i+=2; continue }`) SKIPS THE CHAR
    AFTER EVERY CONSUMED PAIR — use a single manual `i` loop, no post-increment.
  - **Escape() default is magicalBraces=FALSE** (upstream escape()), **Unescape() default
    magicalBraces=TRUE** (upstream unescape()) — they differ; pass nil→&Options{} to escape,
    &Options{MagicalBraces:true} to unescape when the caller omits options.
  - **Oracle TSV encoding**: base64 (or JSON), NOT hex — hex columns like `00` collide and
    awk mis-reports column counts; also real control chars (0x01 etc.) land raw in TSV.
  - **upstream minimatch escape() escapes EXACTLY 7 chars** `* ? ( ) [ ] \ { }` per combo
    — NOT `.` `{#...`; my earlier over-escape set was a drift. `unescape()` is a 3-rule
    pipeline (braceVariant differs per flag), NOT a symmetric inverse of escape().
  - **JS code units vs Go bytes** (documented divergence, oracle is ASCII-only): the RE
    engine matches byte-per-byte; class char ranges use Go `rune` = JS UTF-16 code units for
    the BMP (CLSI paths are ASCII). Do NOT use `unicode.Is(unicode.L, r)` for `\p{L}`-class
    matching without checking the JS side code (JS `\p{...}` operates on code POINTS for
    most properties) — engine pinned via oracle, keep as-is.
  - **`go vet` catches** a `\.` in a Go double-quoted string (invalid escape) that
    `go build` MISSES in non-test files — always run `go vet` after writing regex-source
    strings; use backtick RAW strings for regex sources with backslashes.

## 7. OCM (outputcachemanager) — FULLY LOCKED DESIGN (2026-09-16)

**Status**: `outputcachemanager/` dir exists EMPTY. The stale 318L draft at
`/tmp/stage/ocm/outputcachemanager.go` WAS DISCARDED (do not resurrect).
Write fresh from this section; do NOT re-derive any decision here.

### Package skeleton
```go
package outputcachemanager
```
- Constants: `ContentSubdir = "content"`, `CacheSubdir = "generated-files"`,
  `ArchiveSubdir = "archived-logs"`, `CacheLimit = 2`, `CacheAge int64 = 90*60*1000`.
- Regexp: `buildIdRegexp ^[0-9a-f]+-[0-9a-f]+$`; `fileHiddenRegexp ^\.|/\.`;
  `perUserRegexp ^[0-9a-f]{24}-[0-9a-f]{24}$`; `straceRegex ^strace`.

**Node source reference (all 688L re-read this session, 2026-09-16):**
- `init()`: `doInit().catch(fatal)` → `fillCache()` → `runBulkCleanup()` → `scheduleBulkCleanup(ts)`.
- `fillCache()`: `fs.promises.opendir(Settings.path.outputDir)` (NO stat / IsDir check — mirrors
  Node opendir behaviour); for each entry: `OLDEST_BUILD_DIR.set(join(outputDir, name), Date.now()
  - Math.random() * CACHE_AGE)`. Go: `os.ReadDir` → `oldestSet(join, now - rand.Float64()*float64(CacheAge))`.
- `scheduleBulkCleanup(ts)`: `delay = CACHE_AGE + ts - Date.now() + 60_000` (ms); recursive setTimeout.
- `runBulkCleanup()`: for each `[dir, ts]` in OLDEST_BUILD_DIR: if `ts < Date.now() - CACHE_AGE`:
  `cleanupDirectory(dir, {limit: CACHE_LIMIT})` + `OLDEST_BUILD_DIR.delete(dir)`; else track min ts.
  Returns oldestTimestamp.
- `cleanupDirectory(dir, {keep,limit})`: `queueDirOperation(dir, fn)` where fn swallows errors
  (`try { expireOutputFiles(dir, {keep,limit}) } catch { logger.err }`).
- **`queueDirOperation` (Node promise chain)**: FIFO per dir; error isolation (next job runs even
  if prev failed; `.then(fn, fn)`). Node uses it for BOTH blocking (saveOutputFiles) and
  fire-and-forget (afterSaveCleanup .catch(()=>{})). Go: generic `QueueDirOperation[T any](dir,
  fn) (T, error)` (blocking) + `EnqueueDirOperation(dir, fn func() error)` (fire-and-forget).
  **Inner enqueue from within a queued job MUST be async (goroutine) to avoid self-deadlock.**
  Pump uses SLICE QUEUE (not channel) to avoid send-to-closed-channel; per-dir goroutine started
  lazily, deleted from map when queue drains (under mutex; `len(st.jobs)==0`).
- `path(buildId, file)`: if BUILD_REGEX match → `join(CACHE_SUBDIR, buildId, file)`; else `file`.
- `generateBuildId(cb)`: `Date.now().toString(16) + "-" + crypto.randomBytes(8).toString('hex')`
  = `<hexdate>-<16hexchars>`. Go: `GenerateBuildId() (string, error)`.
- `saveOutputFiles({request,stats,timings}, outputFiles, compileDir, outputDir, cb)`:
  1. `getBuildId` (reuse request.buildId or generate)
  2. `if (!OLDEST_BUILD_DIR.has(outputDir)) set(outputDir, Date.now())`
  3. `queueDirOperation(outputDir, saveOutputFilesInBuildDir(outputFiles, compileDir, outputDir, buildId))`
  4. on success: `collectOutputPdfSize(result, outputDir, stats, ...)`
  5. `enablePdfCaching-dark = SETTINGS.enablePdfCachingDark && !request.enablePdfCaching`
  6. `if (!Settings.enablePdfCaching || (!request.enablePdfCaching && !enablePdfCachingDark))
     return callback(null, {outputFiles, buildId})` (skip CCM)
  7. `saveStreamsInContentDir({request,stats,timings,enablePdfCachingDark}, outputFiles,
     compileDir, outputDir, (err, status) => {...})`
- `saveOutputFilesInBuildDir(...)` (callback form, Node `promisify`d):
  1. `perUser = basename(compileDir).match(perUserRegexp)`
  2. `if (Settings.clsi?.archive_logs || Settings.clsi?.strace)`:
     `archiveLogs(outputFiles, compileDir, outputDir, buildId, err => warn)` (async fire-and-forget;
     errors warn-logged)
  3. `mkdir -p cacheDir` (where cacheDir = join(outputDir, CACHE_SUBDIR, buildId))
  4. `async.mapSeries(outputFiles, ...)`: for each file:
     - `_fileIsHidden(file.path)`? skip (dotfile — `^\.|/\.` on `full=path`)
     - `_checkIfShouldCopy(src, ...)` (src = join(compileDir, file.path))
     - if shouldCopy: `_copyFile(src, dst, dirCache, ...)` (dst = join(cacheDir, file.path));
       file.build = buildId; results.push(file)
  5. on success: `callback(null, results)` + `afterSaveCleanup` =
     `cleanupDirectory(outputDir, {keep: buildId, limit: perUser ? 1 : null}).catch(()=>{})`
  6. on error: `callback(err)` + `fs.rm(cacheDir, {force: true, recursive: true})`
- `collectOutputPdfSize(outputFiles, outputDir, stats, cb)`:
  - find `outputFiles.find(x => x.path === 'output.pdf')` → if absent: `callback(null, outputFiles)`.
  - `stat(join(outputDir, path(file.build, file.path)))` → if err: `callback(err)`.
  - else: `file.size = stat.size; stats['pdf-size'] = stat.size`.
- `saveStreamsInContentDir({request,stats,timings,enablePdfCachingDark}, outputFiles,
  compileDir, outputDir, cb)`:
  1. `cacheRoot = join(outputDir, CONTENT_SUBDIR)`
  2. `ensureContentDir(cacheRoot, (err, contentDir) => { if (err) return cb(err,
     'content-dir-unavailable'); ... })`
  3. find outputFile (`path === 'output.pdf'`); `outputFilePath = join(outputDir,
     path(file.build, file.path))`; `pdfSize = file.size`.
  4. `timer = new Metrics.Timer('compute-pdf-ranges', 1, request.metricsOpts)`
  5. call `ContentCacheManager.update({contentDir, filePath, pdfSize,
     pdfCachingMinChunkSize: request.pdfCachingMinChunkSize, compileTime: timings.compile},
     (err, result) => {...})`
  6. error mapping (exactly this order): NoXrefTableError → `callback(null, err.message)`;
     QueueLimitReachedError → `stats['pdf-caching-queue-limit-reached']=1`, `callback(null,
     'queue-limit')`; TimedOutError → `stats['pdf-caching-timed-out']=1`, `callback(null,
     'timed-out')`; other err → `callback(err, 'failed')` (err propagates to outer).
  7. success: destructure result{contentRanges, newContentRanges, reclaimedSpace,
     overheadDeleteStaleHashes, timedOutErr, startXRefTable};
     `status = 'success'`; if timedOutErr → warn + `stats['pdf-caching-timed-out']=1` +
     status='timed-out-soft-failure'.
  8. if `!enablePdfCachingDark`: `file.contentId = basename(contentDir)`;
     `file.ranges = contentRanges`; `file.startXRefTable = startXRefTable`.
     (dark: skip — frontend gets no ranges)
  9. `timings['compute-pdf-caching'] = timer.done()` (ms);
     `stats['pdf-caching-n-ranges'] = contentRanges.length`;
     `stats['pdf-caching-total-ranges-size'] = sum(range.end - range.start)`;
     `stats['pdf-caching-n-new-ranges'] = newContentRanges.length`;
     `stats['pdf-caching-new-ranges-size'] = sum(newContentRanges.end - .start)`;
     `stats['pdf-caching-reclaimed-space'] = reclaimedSpace`;
     `timings['pdf-caching-overhead-delete-stale-hashes'] = overheadDeleteStaleHashes`.
- `ensureContentDir(contentRoot, cb)`:
  1. `fs.mkdir(contentRoot, {recursive: true})`
  2. `readdir(contentRoot)`, sort
  3. `dirs.find(d => BUILD_REGEX.test(d))` → if exists: `callback(null, join(contentRoot, id))`
  4. else: `generateBuildId` → `mkdir` → `callback(null, join(contentRoot, id))`
- `expireOutputFiles(outputDir, options, cb)`:
  1. `readdir(outputDir)`:
     - ENOENT → `cleanupAll(cb)` (rm -rf + `OLDEST_BUILD_DIR.delete(outputDir)`)
     - else: `cacheRoot = join(outputDir, CACHE_SUBDIR)`
  2. `dirs = readdir(cacheRoot).sort().reverse()` (REVERSE order!)
  3. `currentTime = Date.now()`; `oldestDirTimeToKeep = 0`
  4. `isExpired(dir, index)`:
     - `options?.keep === dir` → keep (return false); `oldestDirTimeToKeep = currentTime`
     - `options?.limit != null && index > options.limit` → remove (return true)
     - `index > CACHE_LIMIT` → remove (return true)
     - `dirTime = parseInt(dir.split('-')[0], 16)`; `age = currentTime - dirTime`;
       `expired = age > CACHE_AGE` → remove
     - else: `oldestDirTimeToKeep = dirTime`; keep
  5. `toRemove = filter(dirs, isExpired)`; if `toRemove.length === dirs.length`
     → `cleanupAll(cb)` (no builds left)
  6. `async.eachSeries(toRemove, removeDir, ...)` where removeDir =
     `fs.rm(join(cacheRoot, dir), {force: true, recursive: true})`
     - on error: `callback(err)` (keep timestamp; retry next iteration)
     - on success: `OLDEST_BUILD_DIR.set(outputDir, oldestDirTimeToKeep)`; `callback(null)`
- `_fileIsHidden(path)`: `path?.match(/^\.|\/\./) != null`
- `_ensureParentExists(dst, dirCache, cb)`:
  - `parent = dirname(dst)`; if `dirCache.has(parent)` → cb()
  - else: `mkdir -p parent` (recursive); while `!dirCache.has(parent)`: `dirCache.add(parent)`;
    `parent = dirname(parent)`; cb()
- `_copyFile(src, dst, dirCache, cb)`:
  1. `_ensureParentExists(dst, dirCache, ...)`
  2. `fs.copyFile(src, dst)`:
     - ENOENT → `logger.warn('file has disappeared...')`; `callback(err, false)` (error propagates
       as hard failure)
     - other err → `callback(err)`
     - success: `if (Settings.clsi?.optimiseInDocker) callback()` (SKIP optimise in prod)
       else `OutputFileOptimiser.optimiseFile(src, dst, cb)`
- `_checkIfShouldCopy(src)`: `basename(src).match(/^strace/)` → false; else true.
- `_checkIfShouldArchive(src)`: if `basename.match(/^strace/)`: true;
  else `archive_logs && basename in ['output.log','output.blg']` → true; else false.
- `_copyFile` ENOENT: Node logs warn but STILL propagates error (hard fail, not skip).

### Manager struct + seams (LOCKED)
```go
type Manager struct {
    PdfCachingEnabled bool      // Settings.enablePdfCaching
    PdfCachingDark    bool      // Settings.enablePdfCachingDark
    OptimiseInDocker  bool      // Settings.clsi?.optimiseInDocker ?? false
    ArchiveLogs       bool      // Settings.clsi?.archive_logs ?? false (always false in prod)
    Strace            bool      // Settings.clsi?.strace ?? false (always false in prod)
    Now               func() int64        // epoch ms (default time.Now().UnixMilli)
    RandHex           func() (string, error) // 16-hex-chars (default crypto)
    UpdateContent     func(a UpdateArgs) (res *UpdateResult, err error) // CCM seam
    OptimiseFile      func(src, dst string) error // (default wraps outputfileoptimiser.OptimiseFile)
    ScheduleAfter     func(ms int64, fn func()) // (default: go time.After loop)
    MetricsInc        func(labels map[string]any) // (default: metrics.PdfCachingStatus.Inc + opts)
    Log               func(level, msg string, ctx map[string]any)
    oldestMu          sync.Mutex
    oldest            map[string]float64 // OLDEST_BUILD_DIR
    pumpMu            sync.Mutex
    pumps             map[string]*pumpState
}
```
- `type pumpState struct { jobs []*queueJob }`
- `type queueJob struct { call func()(T,error) or func()error (use interface{call}); val any; err error; done chan struct{} }`
- `func (m *Manager) New() *Manager` (with defaults wired)
- `func (m *Manager) Init(outputDir string) error` (explicit param; mirrors app.js call)
- `func (m *Manager) Path(buildId, file string) string`
- `func (m *Manager) GenerateBuildId() (string, error)`
- `func (m *Manager) QueueDirOperation[T any](dir string, fn func() (T, error)) (T, error)`
- `func (m *Manager) EnqueueDirOperation(dir string, fn func() error)`
- `func (m *Manager) SaveOutputFiles(req SaveRequest, files []outputfilefinder.OutputFile,
  compileDir, outputDir string, stats, timings map[string]float64) (*SaveResult, error)`
- `type SaveRequest struct { BuildID string; EnablePdfCaching bool; PdfCachingMinChunkSize int64;
  CompileTime float64; MetricsOpts map[string]any }`
- `type SaveResult struct { BuildID string; Files []outputfilefinder.OutputFile }`
- `func (m *Manager) ExpireOutputFiles(outputDir, keep string, limit *int) (bool, error)`
- `func (m *Manager) EnsureContentDir(contentRoot string) (string, error)`
- `func (m *Manager) MetricsInc(status string, opts map[string]any)`: SEAM wrapping
  the Node `Metrics.inc('pdf-caching-status', 1, {status, ...opts})`. Default wires
  `metrics.PdfCachingStatus` (labels: status string + opts). Tests inject a
  recorder to assert per-status increments.

  **Metrics contract (from source)**: `Metrics.inc('pdf-caching-status', 1,
  {status, ...opts})` fires for EVERY outcome of saveStreamsInContentDir's
  callback: success ('success'), soft failure ('timed-out-soft-failure'),
  missing-pdf ('missing-pdf'), content-dir-unavailable (err + status —
  Metrics.inc wraps the inner callback so it still fires), and the error
  paths emitted by saveStreams ('queue-limit', 'timed-out', 'failed', or the
  NoXref message string). SaveStreams errors are swallowed by the outer
  callback (`logger.warn` + `callback(null, {outputFiles, buildId})`) — the
  COMPILE STILL SUCCEEDS. Compile FAILS only on: generateBuildId error,
  saveOutputFilesInBuildDir error (mkdir/copy), or collectOutputPdfSize stat
  error (all `callback(err)` before saveStreams, so Metrics.inc does NOT fire
  for those).

### outputfilefinder.OutputFile extension (LOCKED — do next step)
Current: `struct{Path string; Type string}` (no JSON tags).
**ADD** (all with JSON tags + omitempty; pointer types for "absent" parity):
```go
type OutputFile struct {
    Path           string          `json:"path"`
    Type           string          `json:"type"`
    Build          string          `json:"build,omitempty"`
    Size           *int64          `json:"size,omitempty"`          // * for "absent"
    ContentID      *string         `json:"contentId,omitempty"`
    Ranges         []ContentRange  `json:"ranges,omitempty"`
    StartXRefTable *int64          `json:"startXRefTable,omitempty"`
}
// NEW type (defined in outputfilefinder; CCM keeps its own; no import cycle):
type ContentRange struct {
    ObjectID string `json:"objectId"`
    Start    int64  `json:"start"`
    End      int64  `json:"end"`
    Hash     string `json:"hash"`
}
```
OCM imports CCM directly for the default `UpdateContent` wiring (no local type dup).
`saveStreamsInContentDir` converts CCM result ranges → outputfilefinder.ContentRange via
a local helper when attaching to OutputFile.

### Test plan (~400 lines, whitebox same-package)
Fixtures: `t.TempDir()` for all fs. Inject Now (fixed epoch), RandHex (fixed hex),
UpdateContent (fake returning known result or injected error), OptimiseFile (no-op or
recorder), ScheduleAfter (capture and optionally fire), MetricsInc (recorder).
Use polling for fire-and-forget archive completion (or make archiveLogs sync in tests).
Cover:
- Path: regex match → generated-files/id/file; no match → top-level file; empty buildId →
  file.
- GenerateBuildId: format check (hexdate + "-" + 16hex); error path (RandHex fails).
- QueueDirOperation: FIFO ordering (2 ops on same dir, 1st takes 50ms, 2nd asserts 1st done);
  error isolation (1st fails, 2nd still runs).
- EnqueueDirOperation: fire-and-forget ordering (same FIFO).
- SaveOutputFiles:
  - Basic save: buildId in result; build subdir exists; copied files have Size set;
    dotfiles skipped (not in Files result).
  - Strace file: `_checkIfShouldCopy` returns FALSE for `^strace` basenames, so strace
    files are NOT copied to the build dir and NOT in Files result. They only go to the
    archive dir via archiveLogs (prod: disabled — archive_logs=strace=false).
  - collectOutputPdfSize: stat error (missing output.pdf → error propagates).
  - Pdf-caching gate: Settings.enablePdfCaching=false → CCM NOT called (UpdateContent
    fake never invoked).
  - saveStreams success: ContentID set (basename of contentDir); Ranges set;
    timings/stats populated.
  - saveStreams dark: ContentID/Ranges NOT set; stats still set.
  - saveStreams NoXref: status='no-xref' message string; err propagated (swallowed by
    SaveOutputFiles — result still has BuildID, compile succeeds).
  - saveStreams QueueLimit: stats['pdf-caching-queue-limit-reached']=1; err swallowed.
  - saveStreams TimedOut: stats['pdf-caching-timed-out']=1; err swallowed.
  - saveStreams missing-pdf: status='missing-pdf'; no CCM call.
  - saveStreams content-dir-unavailable: err from ensureContentDir → status
    'content-dir-unavailable'; MetricsInc fires with that status.
- ExpireOutputFiles:
  - ENOENT on outputDir (ENOENT) → cleanupAll (rm + delete from OLDEST_BUILD_DIR).
  - keep buildId retained; limit perUser (limit=1) retains dirs[0..1];
    hard limit (index>CACHE_LIMIT=2) removes dirs[3+]; age>CACHE_AGE removes dir;
    no match → dirTime = 0 (NaN equiv).
  - toRemove.length === dirs.length → cleanupAll.
  - removeDir error → callback(err) (timestamp kept for retry).
- EnsureContentDir: empty root → creates new id dir; existing id dir → reused;
  multiple dirs → first matches BUILD_REGEX (sorted); generateBuildId error path.
- fillCache: without IsDir check (mirrors Node opendir); ENOENT on outputDir → error.
- RunBulkCleanup: all ts < threshold → all cleaned up; ts >= threshold → oldest
  returned, schedule called (verify ScheduleAfter captured).
- Init: fill + runBulkCleanup + schedule; error path (fillCache fails → return err).
- archiveLogs: archiveDir = join(outputDir, ARCHIVE_SUBDIR, buildId) (NO strace
  subfolder); Strace file → archived there; output.log/output.blg (archive_logs=true)
  → archived; other files → not archived.
- copyFiles: dotfile skipped; strace skipped (checkIfShouldCopy=false); normal copied.
- OptimiseInDocker=true: OptimiseFile NOT called (prod default); false → called.

### Key behavioral notes (from Node source)
- `saveOutputFiles` calls `collectOutputPdfSize` on `result` (the NEW files list
  from saveOutputFilesInBuildDir), NOT the input `outputFiles`. The size is set
  on `result.files.find(x => x.path==='output.pdf')` — if output.pdf was NOT
  copied (dotfile/strace filter), the stat happens on `output.pdf` path in the
  build dir (which SHOULD exist for a real compile). If it's missing → stat ENOENT
  → `callback(err)` → compile fails. This is the hard-fail path.
- `_fileIsHidden` is called on `file.path` (NOT the full path). The Node regex
  `^\.|/\.` tests the `file.path` — e.g. `.foo` or `.foo/bar` or `/bar/.foo`.
  Go: `fileHiddenRegexp.MatchString(f.Path)`.
- `saveOutputFilesInBuildDir` does NOT create contentDir (that's saveStreams's job).
- `archiveLogs` is called BEFORE mkdir in saveOutputFilesInBuildDir (Node:
  `if (Settings.clsi?.archive_logs || Settings.clsi?.strace)` — in prod
  archive_logs=false, strace=false → NEVER called. Go: same guard; default false).
- `saveStreamsInContentDir` uses `file.size` (set by collectOutputPdfSize BEFORE
  this runs) as `pdfSize`. If size is nil (shouldn't happen) → 0 (safe).
- `Metrics.inc('pdf-caching-status')` fires with status string from
  saveStreamsInContentDir (the `status` param, NOT a fixed label). In prod:
  always `'success'` (CCM almost never errors in prod; soft failure is
  'timed-out-soft-failure').

### CompileManager OCM call sites (for future compilemanager port)
- line 384: `saveOutputFiles` (main compile flow)
- line 589: `BUILD_REGEX` (build id validation)
- line 597: `CACHE_SUBDIR` (path construction)
- line 605: `queueDirOperation` (synctex? verify)
- CompileManager sets `timings.compile` (float, ms) before calling SaveOutputFiles.
- `timings.compile` is passed to CCM as `CompileTime float64`. If missing → 0 →
  CCM uses 1000ms deadline (acceptable divergence; compile layer always sets it).
- `request.parse` (requestparser) sets `BuildID` on the SaveRequest (reuse
  existing build id or generate new).
- `outputfilearchivemanager` uses `outputfilefinder.OutputFile{Path, Type}` —
  the extension (adding Build/Size/ContentID/Ranges/StartXRefTable) is SAFE
  (additive; existing fields unchanged).

### Node oracle probe results (CCM fixtures, verified 2026-09-16)
- `minimal.pdf` (12313 B): object "9 0 " → start 1074, end 11235, hash `d7cfc73a...` (10161B);
  object "10 0 " → start 11240, end 11784, hash `896749b8...` (544B).
- minChunk=500 → 2 content ranges; minChunk=1024 → 1 content range (obj 9 only).
- `track(hash, size)` receives stream length (end-start), NOT total object size.
- `CCM no-obj` branch (idxObj=-1): `objectIdRaw = buf[:len(buf)-1]`, `stream = buf[len(buf)-1:]`,
  `rawLen = len(buf)-1`; Start = `offset + rawLen` (NOT offset + idxObj which is -1).
- OError shape: `new OError('msg', {info})` → `err.Message === 'msg'` (no interpolation;
  `super(message)`); `Info` separate. CCM uses `&clserrors.OError{Message: msg, Info: info}`.
- CCM contentDir MUST exist before `writePdfStream` (Node tests always
  `mkdirSync(contentDir)`); Go tests must `os.MkdirAll` before calling Update.
- CCM early return: `PdfSize < PdfCachingMinChunkSize` → immediate return with empty result.
  Test PdfSize must be ≥ minChunk to exercise read paths.
- `t.Errorf` vs `t.Error`: `t.Error` does NOT accept format args (vet printf check);
  use `t.Errorf("...%v...", args)` for formatted messages.
- Settings probe: `clsi.optimiseInDocker = true` (confirmed); `clsi.archive_logs = undefined`;
  `clsi.strace = undefined`.
- `@overleaf/metrics` Timer.done() returns float ms (elapsed since construction).
- CCM error type: `clserrors.OError` (NOT `NoXrefTableError` for "could not read full chunk"
  and "objectId is too large"). NoXrefTableError is separate (from xrefparser).
- CCM CCM.UpdateArgs is `{ContentDir string, FilePath string, PdfSize int64,
  PdfCachingMinChunkSize int64, CompileTime float64}`.
- CCM CCM.UpdateResult is `{ContentRanges []ContentRange, NewContentRanges []ContentRange,
  ReclaimedSpace int64, OverheadDeleteStaleHashes *int64, TimedOutErr error,
  StartXRefTable *int64}`.
- CCM `asInt64` handles `int64`, `int`, `float64`; rejects strings/nil.
- requestparser `PdfCachingMinChunkSize interface{}` and `BuildID interface{}` —
  OCM SaveRequest uses concrete `int64`/`string`; CompileManager does the conversion.
- `xrefparser.IsNoXrefError(err)` is exported (used by CCM to detect no-xref).
- CCM test file: `test/unit/js/ContentCacheManager.test.js` (223L, vitest) — assertions
  ported 1:1.
- CCM coverage: 96.7% (gate met).

### Wire shape (CompileController response, for future compilecontroller port)
```json
{
  "compile": {
    "status": "success",
    "error": null,
    "buildId": "...",
    "stats": {"pdf-size": ..., "pdf-caching-n-ranges": ..., ...},
    "timings": {"compile": ..., "compute-pdf-caching": ..., ...},
    "outputUrlPrefix": "...",
    "outputFiles": [{
      "url": "...",
      "path": "output.pdf",
      "type": "pdf",
      "build": "...",
      "size": 1234,
      "contentId": "...",
      "ranges": [
        {"objectId": "...", "start": 1074, "end": 11235, "hash": "d7cfc73a..."}
      ]
    }]
  }
}
```
Node CompileController: `outputFiles.map(file => ({url: ..., ...file}))` (spread
file). Success gate: `file.path === 'output.pdf' && file.size > 0`.

### Node `app.js` OCM call (for future app port)
- `app.js` line 53: `OutputCacheManager.init()` (no args — uses Settings.path.outputDir
  internally). Go: `ocm.Init(cfg.Path.OutputDir)`.

## 8. Immediate Next Steps (for the next session)

1. **dockerrunner** — DONE (2026-09-24): engine.go SPI + unixengine.go +
   pipeline.go (gate model) + monitor.go + fingerprint.go + FakeEngine tests
   (90.9%). Wire facts below were provenance (oracle probes + dockerode 4.0.9
   source); now encoded in dockerrunner/*.go. Retained for provenance.
   **runOnce gate model (LOCKED this session, exact Node 1:1 mapping)**:
   - Node `DockerRunner.mjs` `_runAndWaitForContainer` = "gate" model:
     `once = _.once(callback)`; `streamEnd` + `containerReturn` flags both
     must be true to call `once()`. `attachStreamHandler(error, {_output})`:
     `error` → `callback(error)`; else `output = _output`, `streamEnd = true`,
     `callbackIfFinished()`.
   - `startContainer(error, {name})` callback: `error` → `callback(error)`;
     else → `waitForContainer(container, exitCode, timeout, (error, exitCode)
     => { error → callback(error); exit === 137 → callback(new TerminatedError
     ()); exit === 1 → callback(new ExitedError(1)); else output.exitCode =
     exitCode; containerReturn = true; callbackIfFinished(); })`.
   - **Go port**: use `commandrunner.RunOutput{Stdout string, Stderr string,
     ExitCode int, Terminated bool, Exited bool, TimedOut bool}`. On success
     path: `Terminated=true` if exit==137, `Exited=true` if exit==1,
     `TimedOut=true` if timedOut flag. On error path: `err` from Engine
     (e.g. HTTP 5xx, socket error). `callback (func(err error, out
     *commandrunner.RunOutput))` mirrors Node `callback(error, output)`.
   - Engine SPI methods (Go): `Inspect(id string) (*ContainerInfo, error)`,
     `Create(id string, opts CreateOpts) error` (POST /containers/create,
     name from QUERY STR string, config from JSON BODY), `Start(id) error`,
     `Kill(id) error`, `Attach(id string) (io.ReadCloser, error)` (POST
     /containers/{id}/attach?stdout=1&stderr=1&stream=1), `Wait(id string)
     (int, error)` (POST /containers/{id}/wait; returns exit code),
     `Destroy(id string) error` (DELETE /containers/{id}).
   - **Docker Engine API (probe-verified 2026-09-24)**:
     * `name` is QUERY STRING for `create` (NOT in body — dockerode 4.0.9
       sends `query: {name: X}`); `config` is JSON BODY.
     * `wait` is POST. Returns `{"StatusCode": N}` JSON body.
     * `attach` response is a FLOW of raw bytes (no JSON) — demux stream
       (see demux below).
     * `start` when already running (304) → error `{"statusCode": 304}`;
       DockerRunner code ignores 304 on `start` (see _startContainer:
       `if (err.statusCode !== 304) callback(err)`).
     * Docker Engine `kill` regex: `/Cannot kill container .* is not running/`
       → `err = null` → treat as 200. (Already in dockerrunner.go.)
   - **demux stream (docker-modem)**: 8-byte header `[streamType, 4-byte BE
     length]` per frame. `streamType` ∈ {0,1,2} (stdin/stdout/stderr). Invalid
     (other value) → write entire buffer to stdout, stop demux (switch to
     raw passthrough). Go impl: `bufio.Reader` over `Attach` ReadCloser,
     loop: read 8 bytes header, extract type + length BE, read length bytes,
     route. **This is a raw-stream — NOT JSON, NOT NDJSON.**
   - **fingerprint** (for content-cache key): MD5 of `CreateOpts` serialized
     (NOT of the container ID — because different commands on same image
     need different keys). Already in `fingerprint.go`.

2. **Fix 4 packages below 90 gate** — DONE (2026-09-24): thin seam tests added;
   errors 98.4%, xrefparser 97.8%, config 93.0%, metrics 100.0. Gate = 34/34.

3. **historyresourcewriter** (PRODUCTION CODE WRITTEN this session) — full 1:1
   port of HistoryResourceWriter.js 869L. Stale draft (105L, wrong Runner
   interface) REPLACED. Locked design + verified signatures: §10 below.
   otc Phase B dep is MET (blob_store.go shipped). Current: 639L + 491L + 3 cov test files (27 scenario tests);
   build+vet+all GREEN (cov 94.0%, gate MET §10.9).

4. **compile core** (compilemanager 1021L + compilecontroller 491L) — deps ALL met
   (dockerrunner + otc B). CompileManager uses HRW Result (full 1:1:
   `Result{Snapshot, ProjectDir, CompileDir, OutputDir, Stats, Timings}`).

5. **error middleware + server layer** (§4/12 + §3.2 locked design above).

6. **clsi_typst.go** (NEW — per user directive 2026-09-21): port
   `services/clsi_typst` to `services/clsi_typst.go`, feature-equivalent to
   clsi.go (reuses `go/libraries/otc` LIB-15 for content), reuses clsi.go
   packages where possible, HANDOFF.md in `clsi_typst.go/`. (This is a
   SEPARATE Go module from clsi.go.)

7. **cmd/clsi main + Makefile + live smoke** — UNBLOCKED (texlive/texlive:
   latest-full present, pulled 2026-09-24). Do `hello-world` wire smoke first,
   then texlive production smoke.

Deferred (upstream scope, does NOT block porting):
- (none; the safe_pathname oracle is now FIXED in place, commit b74bb81,
  spec + acceptance: go/libraries/HANDOFF_SAFE_PATHNAME.md)

Blocked / environment:
- Node baseline (TODO-9e50c129): RESTORED 2026-09-24 (user repaired `.pnp.cjs`
  + `yarn install` re-fetched). `yarn vitest run` executes; unit suite is
  harness-red (see §9 "Node oracle run") — behaviorally oracle-green for the
  HRW port (8/9 Node tests pass; the 1 fail is a chai-sin `.to.have.been.*`
  stub-assertion issue, same stubs). Practical spec = passing subset + Go port.

## 9. Session notes (2026-09-24 THIS SESSION)

- **dockerode 4.0.9 wire format (probe-verified)**: `create` uses QUERY
  STRING for `name` + JSON BODY for `config` (content-type header required).
  `wait` is POST. `attach` is GET with `stream=1&stdout=1&stderr=1`.
- **Docker Engine API version note**: ApiVersion 1.54, MinAPIVersion 1.40
  (old-style routes: `/containers/{id}/json` for inspect, NOT `/containers/{id}/
  inspect`).
- **demux stream**: 8-byte header per frame, streamType ∈ {0,1,2}, 4-byte BE
  length. Invalid → raw passthrough to stdout. (docker-modem demuxStream in
  Node source.)
- **Node `_runAndWaitForContainer` gate model CONFIRMED** (read at source):
  `once = _.once(callback)`; `attachStreamHandler` AND `waitForContainer`
  both feed the same gate. Both `streamEnd` AND `containerReturn` must be
  true to fire `once()`. On error path: `callback(error)` directly. On
  137: `callback(new TerminatedError())`. On 1: `callback(new ExitedError(1))`.
- **DockerRunner Node test oracle** (DockerRunner.test.js, 1150 lines) available
  at `services/clsi/test/unit/js/DockerRunner.test.js`. Uses `vi.doMock('dock
  erode')` etc.
- **config.go** added `MaxContainerAge int` + env parsing (commit b609dfb).
- **git state**: HEAD b609dfb, on branch go_compile_test, 12 commits ahead
  origin/main (d4f9c24 is upstream merged into ours).
- **otc oracle** (`safe_pathname_oracle_test.go`, 80,782 rows) — GREEN,
  attached to otc via commit 83e7967.
- **CORRECTION (2026-09-24, user)**: the "NFS flakiness" noted above was a
  misdiagnosis — the repo dir name is case-sensitive `OlliTeX_comp`; bit-flips in the
  path (e.g. `Ollitex_comp`) create shadow dirs / ENOENT. The NFS4 mount is fine.
  `/tmp/` staging is retained only for large-write chunking.
- **Node oracle run (2026-09-24, TODO-9e50c129 RESTORED)**: user repaired
  `.pnp.cjs`; `yarn install` re-fetched 1.64 GiB into local gitignored
  `.yarn/cache`, so `yarn vitest run` (services/clsi) now EXECUTES (no more PnP
  "Missing package: vitest" error). Node unit suite is RED for HARNESS reasons,
  not behavioral: repo's vitest-era setup (setup.js `vi.doMock` +
  `vi.resetModules` under `isolate:false`) leaves the `vi.mock`-style module-mock
  contexts undefined (`ctx.fs`, `ctx.ResourceStateManager`, `ctx.Metrics`,
  `ctx.UrlFetcher`, `ctx.LockManager`, ...) so assertions hit "Cannot set
  properties of undefined" + timeouts in ~21 files (354 failed / 127 passed /
  13 skipped of 494; 8 of 29 files pass). Treat as "harness red, behaviorally
  green" and re-validate per-module against the passing subset + Go port as
  the acceptance gate. **HRW oracle (9 tests, the module I'm porting) = 8/9
  green**; the 1 red is `does not convert PNGs until they are known to be slow,
  then converts once` at line 185 = the chai-sin `to.have.been.calledOnce` stub
  assertion (lines 185-186 are the ONLY `to.have.been.*` in the file and throw
  "not a spy or a call to a spy") — a stub-registration harness issue, SAME
  stub + same 6-arg download shape as the 8 that pass and as my Go mirror.
  NOT a behavioral mismatch; the Go port (TestSlowListGatedConversion) asserts
  the identical sync1/sync2/sync3 flow and is GREEN.
- **Node oracle run (2026-09-24, TODO-9e50c129 restored)**: the user repaired
  `.pnp.cjs`; `yarn install` re-fetched 1.64 GiB into the local gitignored
  `.yarn/cache`, so `yarn vitest run` (services/clsi) now EXECUTES (no more
  "Missing package: vitest..." PnP error). Node unit suite is RED for
  HARNESS reasons, not behavioral: the repo's vitest-era setup (setup.js
  `vi.doMock` + `vi.resetModules` under `isolate:false`) leaves the
  `vi.mock`-style module-mock contexts undefined (`ctx.fs`,
  `ctx.ResourceStateManager`, `ctx.Metrics`, `ctx.UrlFetcher`, `ctx.LockManager`,
  ...) so the assertions hit "Cannot set properties of undefined" + timeouts
  in ~21 files (354 failed / 127 passed / 8 file-passes of 494). This masks
  the behavioral oracle; treat it as "harness red, behaviorally green" and
  re-validate per-module against the passing subset + the Go port as the
  acceptance gate. **HRW oracle specifically (9 tests, the module I'm porting)
  8/9 green**; the 1 red is `does not convert PNGs until they are known to be
  slow, then converts once` at line 185 = the chai-sin `to.have.been.calledOnce`
  stub assertion (lines 185-186 are the ONLY `to.have.been.*` in the file and
  throw "not a spy or a call to a spy") — a stub-registration harness issue,
  SAME stub + same 6-arg download shape as the 8 that pass and as my Go
  mirror. NOT a behavioral mismatch; the Go port (TestSlowListGatedConversion)
  asserts the identical sync1/sync2/sync3 flow and is GREEN.
## Session outcome (2026-09-24)

**dockerrunner COMPLETE (90.9% coverage):**
- `dockerrunner/{engine.go, unixengine.go, dockerrunner.go, fingerprint.go, run.go, pipeline.go, monitor.go}`
- Engine SPI (inspect/create/attach(start,stream)/start/kill/waitForExit/list/remove; 404+AutoRemove
  = nil,nil — faithful to dockerode's createOrReconnect throw:false)
- runOnce pipeline: name lock (dockerlockmanager), 304-already-started fast path, attach-then-start
  (DockerRunner attach ordering), demuxStream (8-byte frames; type>2 = raw passthrough), MaxOutput
  truncation marker "Output exceeds 2048KB; killing container." (byte count — documented UTF-16
  divergence), settled-CAS timeout (models clearTimeout first-wins; kill error → log + proceed),
  137→Terminated, 1→Exited, stream-error finalization (Go: error — Node's catch {} is no-op,
  DIVERGENCE documented), 500-retry harness (destroy → retry once)
- Kill: 404 swallow + case-sensitive notRunning regex probe propagation (engine lowercase → Go
  propagates; divergence documented, same as ported png2pdf/LatexRunner)
- monitor: destroy/destroyOldContainers/StartContainerMonitor (rand 0-5m → immediate scan → 1h
  tick; grace window via lastprojectaccess)
- unixengine: HTTP-over-unix-socket (DialContext, host "docker"), Content-Type application/json,
  message||cause||raw error body; routes probe-verified against live Engine 29.5.3 (old-style
  /containers/{id}/json, POST /wait, attach non-hijacked isStream)
- FakeEngine + demux frame helpers + real unix-socket wire tests (routes + error format
  "(HTTP code N) reason - message " + Content-Type assertion)
- **dockerode per-operation statusCodes mapped** (§15 in HANDOFF §2) — Go uses single reason() map
  (documented): kill 409 → "unexpected", start 304/404/400/406/500, wait 500, etc.

**Coverage gate MET: all 34 clsi.go packages ≥ 90%.**
  metrics 100.0, errors 98.4, xrefparser 97.8, config 93.0, dockerrunner 90.9, rest as before.
  (Added thin tests: errors WithCause/NewOError/Tag + ConversionErrorT; metrics Count/DoneMS;
  xrefparser no-xref wrap + splitLines; config Get/ForTest singleton.)

**otc Phase B (BlobStore) NOW AVAILABLE:** otc blob_store.go exports `BlobStore` interface +
`BaseBlobStore` (GetBlob/GetBlobURL/GetString/GetObject/PutString/PutObject). clsi module links
`ollitex` (go.mod replace). The "Phase B open" note in this HANDOFF is SUPERSEDED — blob store
primitive is ported; only the *backend wiring* (filestore URL prefix) stays local (clsi
HistoryResourceWriter's BlobStore extends BaseBlobStore, as in Node).

**Next (in order):**
1. historyresourcewriter — DONE (cov 94.0%≥90%, gate MET; 27 scenario tests
   green; errors+metrics restored 100%; §10.9 COMPLETE).
2. compilemanager.go (1021L) — deps all met. CompileController (491L, route
   layer) AFTER — it's mostly z-schema request parsing + wire.
3. error middleware + apps/server (route table §3.2, load agent TCP 3048 + HTTP 3049)
4. cmd/clsi main + Makefile + live smoke (texlive/texlive:latest-full present;
   `hello-world` wire smoke first, then texlive production smoke)
5. clsi_typst.go (new service; feature-equivalent, reuses otc + shared packages;
   own HANDOFF per next-session notes)

### Docker wire (probe-verified against live Engine 29.5.3, 2026-09-24)
- Create: POST /containers/create?name=X — name from QUERY (body name ignored); 409 body
  {"message":"name \"...\" is already in use..."}; buildOpts JSON deterministic (probe: 3x
  identical)
- Attach: POST /containers/{id}/attach?stdout=1&stderr=1&stream=1 + JSON body → 101 isStream
  raw body (not hijacked → no conn upgrade); Go: returns resp.Body as io.ReadCloser
- Wait: POST /containers/{id}/wait → {"StatusCode":137}; kill/start/remove are POST/DELETE
- Inspect: GET /containers/{id}/json (old-style route — modern /containers/{id}/inspect
  returns 404)
- Wait 404 body: {"message":"No such container: X"} (no cause field)
- List: all=true, CREATED epoch seconds probe-verified
- Kill 409: dockerode kill statusCodes has NO 409 mapping → "unexpected" (not "conflict")
- MaxOutput = 2MB (1024*1024*2). `volumes` param in Node is DEAD CODE (unused).
- Node monitor starts at import (line 628); Go: main calls StartContainerMonitor()
- waitForContainer: Node clears the waitPromise on settled (first-wins = clearTimeout model)
- Lock held INSIDE startOnce (inspect→create→attach→start); released on return (kill+wait
  already captured); 404+AutoRemove = container gone → nil,nil (dockerode throw:false)
## 10. HRW (historyresourcewriter) — LOCKED DESIGN + VERIFIED SEAMS (2026-09-23)

Status: investigation COMPLETE (Node HistoryResourceWriter.js 869L + Metrics.js +
blob_store_base.js + HistoryResourceWriter.test.js 327L re-read in full; every Go
seam signature probed against live packages). The stale draft
`historyresourcewriter/historyresourcewriter.go` (105L) has the WRONG
`Runner interface{ Run(...) }` + `commandrunner` import — Node syncResourcesToDisk
does NOT import DockerRunner. **Draft is to be REPLACED entirely.**

### 10.1 Go seams (package-level function vars, mirror Node vi.doMock targets)
- `DownloadUrlToFile func(projectID, urlStr, fallbackURL, destPath string,
  lastModified *time.Time, conversionSuffix string) (*urlcache.ConversionHandle, error)`
  → `urlcache.DownloadUrlToFile`
- `IsConversionCached func(projectID, urlStr string, lastModified *time.Time) (bool, error)`
  → `urlcache.IsConversionCached`
- `CommitConversion func(conversionPath, cachePath, destPath string) error`
  → `urlcache.CommitConversion`
- `CreateProjectDir func(projectID string) error` → `urlcache.CreateProjectDir`
- `GetProjectCacheDir func(projectID string) string` → `urlcache.GetProjectCacheDir`
- `Png2PdfEnabled func() bool` → `png2pdf.IsEnabled`
- `PngConvert func(projectID, cacheProjectDir string, relativePaths []string,
  stats *png2pdf.Stats, timings *png2pdf.Timings) error`
  → `png2pdf.ConvertPngFilesInCacheDir` (HRW keeps `map[string]any` and bridges)
- `DownloadHistorySnapshot func(projectID, userID, cacheDir string) (bool, error)`
  → `clsicachehandler.DownloadHistorySnapshot`
- `IsExtraneousFile func(p string) bool` → `resourcewriter.IsExtraneousFile`
- `WriteOutputFileIfNeeded func(compileDir string, hasOutputTex bool, content string) error`
  → `tikzmanager.WriteOutputFileIfNeeded` (hasOutputTex = snapshot.GetFile("output.tex") != nil)
- `fetchStringFunc func(ctx context.Context, url string, opts ...*fetchutils.Options) (string, error)`
  → `fetchutils.FetchString` (BlobStore.FetchString uses it)
- Draft prefix: use `draftmodemanager.PREFIX` constant directly (no seam —
  Go test asserts the REAL prefix, Node test mocked it to '').

### 10.2 Request/Result (in HRW package, NOT requestparser)
```go
type Request struct {
    BaseHistoryVersion int
    RawSnapshot map[string]any
    GlobalBlobs []string
    RawChangeOperations [][]map[string]any  // raw op array per change
    PopulateClsiCache bool
    Png2pdf bool
    HistoryID string
    FilestoreBlobPrefix string
    ClSIPerfVariant string
    Draft bool
    RootResourcePath string
    CompileGroup string
    MetricsPath string   // for metrics.ShouldSkipMetrics
}
type Result struct {
    BaseHistoryVersion int
    ResourceList []Resource  // {Path string}
}
func SyncResourcesToDisk(ctx context.Context, projectID, userID string,
    request *Request, compileDir string, timings map[string]any, stats map[string]any) (*Result, error)
```
(baseHistoryVersion return = localBaseVersion + len(changes); stats/timings are
maps so compilemanager can add keys, mirroring Node `Record<string, number>`.)

### 10.3 BlobStore (Node `class BlobStore extends BlobStoreBase`)
Go: `struct { *otc.BaseBlobStore }` + fields historyID/filestoreBlobPrefix/clsiPerfVariant/globalBlobs;
embed so GetBlob/GetBlobURL/GetString/GetObject delegate; supply `FetchString` = 3-attempt
retry loop (each attempt in ctx with 3s timeout → fetchutils timeout):
```
fetchString(hash):
  for attempt := 1..3:
    s, err := fetchStringFunc(ctx, getBlobURL(hash), timeout 3s)
    if err == nil: return s
    if fErr, ok := err.(*fetchutils.RequestFailedError); ok && fErr.Status == 404:
      return "", err (→ errors.NewNotFoundError)  // otc wraps into BlobNotFound
    warn logger {url, remainingAttempts} "compile from cache: history blob download failed"
    sleep 100ms (context-aware)
  return last error
```
getBlobURL(hash) (4 branches, base = `Settings.apis.filestore.url` = config Get().APIs.FileStore.URL):
1. filestoreBlobPrefix != "" → url = base + "/" + filestoreBlobPrefix + "/" + hash
2. clsiPerfVariant != "" → host = config Get().APIs.Perf.Host (settings.apis.clsiPerf.host),
   path = "/variant/" + clsiPerfVariant + "/" + hash + "/hash/" ... EXACT Node:
   `u.pathname = \`/variant/${clsiPerfVariant}/hash/${hash}\`` (host is apis.clsiPerf.host,
   which Go stores at `APIs.Perf.Host` + port; URL host includes port).
3. globalBlobs contains hash → path = "/history/global/hash/" + hash
4. else → path = "/history/project/" + historyID + "/hash/" + hash
Note: config has APIs.Perf.Host ("127.0.0.1:3043" default, CLSI_PERF_HOST/PORT). Node
`apis.clsiPerf.host` — settings key is `apis.clsiPerf.host`; Go mapping verified: Perf.Host
carries host:port, so for the perf-variant URL set url.Host = Perf.Host, keep scheme http.

### 10.4 changesFromRawChangeOperations (Node: `Change.mustFromRaw({operations: o, timestamp: '0'})`)
Node's raw change has operations array + timestamp '0'. Go otc ChangeFromRaw/parseRawTime
REJECTS '0' (not a valid time — gop error). **Use synthetic construction** instead.
AS-IMPLEMENTED (returns error — a raw op that fails to materialise fails the sync
loudly instead of corrupting snapshot state; `{}` op still → NoOperation):
```go
func changesFromRawChangeOperations(raw [][]map[string]any) ([]*otc.Change, error) {
  changes := make([]*otc.Change, 0, len(raw))
  for i, opsRaw := range raw {
    ops := make([]otc.Operation, 0, len(opsRaw))
    for j, oRaw := range opsRaw {
      op, err := otc.OperationFromRaw(oRaw)
      if err != nil {
        return nil, errors.NewOError("invalid raw change operation",
          map[string]any{"change": i, "operation": j, "err": err.Error()})
      }
      ops = append(ops, op) // `{}` → NoOperation (OperationFromRaw default)
    }
    changes = append(changes, otc.NewChange(ops, time.Time{}, nil, nil, nil, nil, nil))
  }
  return changes, nil
}
```
(OperationFromRaw is exported; NewChange exported; exported `Operations` field.)

### 10.5 syncResourcesToDisk flow (verbatim Node port)
1. cacheKey = path.Base(compileDir); remoteBaseVersion = request.BaseHistoryVersion
2. loadSnapshot(projectID, userID, cacheKey, remoteBaseVersion, populateClsiCache):
   - candidates [historyPath, resyncPath]: try loadSnapshotFromFile; on MissingUpdatesError
     track maxLocalBaseVersion; else warn "cannot read history from disk"; ENOENT silent
   - if populateClsiCache: loadSnapshotFromClsiCache (downloadHistorySnapshot → !ok
     "needs full sync", baseHistoryVersion:-1), else loadSnapshotFromFile(resyncPath, fullSync=true)
     on error warn "cannot download from clsi-cache"; track max
   - throw NewMissingUpdatesError("needs more updates", {baseHistoryVersion: maxLocalBaseVersion})
3. loadSnapshotFromFile: gunzip → JSON {rawSnapshot, globalBlobs, localBaseVersion,
   dirty (default []), png2pdf (default false)}; if localBaseVersion < remoteBaseVersion
   → NewMissingUpdatesError("missing updates", {baseHistoryVersion: localBaseVersion})
4. try/catch: loadSnapshot fails AND request.RawSnapshot == nil → re-throw (missing
   snapshot); else warn "bad local history state during full resync" (only for non-MissingUpdates),
   source='remote', localBaseVersion=remoteBaseVersion, rawSnapshot=request's,
   globalBlobs=[], dirty=[], fullSync=true
5. globalBlobs dedupe + merge request.globalBlobs; snapshot = SnapshotFromRaw
6. changes = changesFromRawChangeOperations(rawChangeOperations[localBaseVersion-remoteBaseVersion:])
   (slice start = localBaseVersion - remoteBaseVersion)
7. applyAll timing: timings["snapshotApplyAll"] = ms ceil (Go: measure via time.Now);
   if !metrics.ShouldSkipMetrics(request.MetricsPath): observe snapshotApplyAllDurationSeconds
   histogram {group: compileGroup, source} — port as metrics counter/float append
   (no prom exposure yet; see §4/8 metrics note: model as Gauge/counter named
   "clsi_snapshot_applyAll_duration_seconds" — EXPOSE ONLY IF server /metrics needs it;
   simplest: store last observed value in a metrics float + Count of observations)
8. entriesDepthFirst = discoverExistingEntries(compileDir, ".") (sorted readdir, postorder
   children before parent — MUST iterate insertion order for deletion logic; Node Map iterates
   insertion order → Go: keep []string slice + map, iterate slice)
9. removeExtraneousEntries(compileDir, snapshot, entriesDepthFirst):
   keepFolders = {""}; for each (path, isDir) in depth-first order:
   - isDir: if snapshot has no file at path: path still a dir — if keepFolders has path:
     keepFolders.add(dirname(path)); else rmdir + remove from entries; continue.
     If snapshot DOES have a file at this path (dir→file): for each child starting with
     path+"/": rmdir/unlink child + drop from entries; rmdir path; drop path.
   - non-dir (file): if snapshot has file OR !isExtraneousFile(path): keepFolders.add(dirname);
     else unlink + drop
10. ensureHasParentFolder(compileDir, path, entries) for each path in changedPaths — recursive:
    if parentFolderPath := path.Dir(path) is in entries: return; else recurse(parentFolderPath),
    mkdir(parentFolderPath), entries.add(parentFolderPath, true)
11. changedPaths: fullSync ? all pathnames : (set from dirty + (draft? rootResourcePath)
    + (pngModeChanged? all .png paths) + per-change operations (Add: pathname; Move:
    pathname, and newPathname if !isRemoveFile; Edit: pathname) + (snapshot pathnames NOT in
    entriesDepthFirst) + shouldConvert paths NOT attempted via isConversionCached)
12. snapshot.loadFiles("eager", blobStore) (ctx) timing → timings["snapshotLoadEager"]
13. per changedPath (sequential — Node is sequential `for path of changedPaths`):
    file = snapshot.getFile(path); if nil continue (deleted)
    content := file.GetContent(true) (filterTrackedDeletes=true)
    if content != nil:
      if path == request.RootResourcePath:
        if request.Draft: content = PREFIX + content; dirty.push(path)
        writeOutputFileIfNeeded(compileDir, snapshot.GetFile(tikz.OutputTex) != nil, content)
      writeFile(path, content, "utf-8")
    else:
      hash := file.GetHash(); if nil → OError("unexpected file without content and hash", {path})
      createProjectDir(projectID) (once)
      url = blobStore.getBlobURL(hash)
      destPath = filepath.Join(compileDir, path)
      if shouldConvert.has(path): handle, err := downloadUrlToFile(projectID, url, "", destPath,
        new time.Time (epoch 0 for lastModified → new Date(0) = Unix epoch, NOT zero value!), cacheKey)
        if handle != nil → pngFilesToConvert.push(handle)
      else: downloadUrlToFile(projectID, url, "", destPath, epoch0, "")
      on error: logger.Warn/Err {err, projectId, path, resourceUrl} "error downloading file for
        resources"; metrics.IncDownloadFailed (download-failed counter)
    Node aborts on first error after the loop (allDone check → rethrow); Go: defer rethrow pattern
      (collect first error, rethrow after loop) OR rethrow immediately (Node's promise.allSettled
      then `for... if !result.success throw Otag.tag(reason,'write failed',{'path':path})`)
14. baseHistoryVersion = localBaseVersion + len(changes)
15. saveSnapshot IF (fullSync || len(changes)>0 || wasDirty || len(dirty)>0 || pngModeChanged):
    gzip level 1, JSON {globalBlobs, localBaseVersion, rawSnapshot: snapshot.ToRaw(), dirty,
    png2pdf: request.png2pdf}, tmp file flag 'wx' then rename (os.OpenFile O_CREATE|O_EXCL)
16. deleteResyncSnapshot IF fullSync: unlink resyncPath, ENOENT silent
17. return Result{baseHistoryVersion, resourceList: [{Path: p} for p in snapshot.GetFilePathnames()]}

Note: Node `new Date(0)` = Unix epoch 1970-01-01T00:00:00Z → Go `time.Unix(0,0)`,
NOT `time.Time{}` (which is year 0001).

### 10.6 clearCache/saveSlowPngList (exported, compilemanager calls)
- `ClearCache(projectID, userID, cacheKey string) error` — rm -rf snapshotPath(cacheKey).DIR,
  ENOENT silent, else logger.Warn "compile from cache: failed to clear history cache".
  Note: Node clearCache signature is (projectId, userId, cacheKey) but snapshotPath uses
  cacheKey alone; Go same.
- `SaveSlowPngList(cacheKey string, slowPngs []string) error` — mkdir dir, write
  JSON to slowPngPath+~, rename. Best-effort (compile caller catches).
- loadSlowPngList(cacheKey) ([]string, error) — read+parse, ENOENT→[], corrupt→[]
  (warn "cannot read slow-png list").
- snapshotPath(cacheKey) → {dir: <ClSI cache dir>/<cacheKey>, path: history.json.gz,
  resyncPath: history-resync.json.gz, slowPngPath: png2pdf-slow.json}. ClSI cache dir =
  config.Get().Path.ClsiCacheDir (env CLSI_CACHE_PATH, default /clsi/cache).

### 10.7 shouldConvert / png2pdf gating (Node syncResourcesToDisk lines 530-655)
- slowPngs = loadSlowPngList(cacheKey) (set)
- for each snapshot path: is .png AND in slowPngs → byteLength (file.GetByteLength() || 0);
  if byteLength < config.Get().Png2pdfMinFileSizeBytes ("Settings.png2pdfMinFileSizeBytes",
  default 1MB): metrics.IncPng2pdfSkippedSmall, skip; else shouldConvert.add(path)
- if len(shouldConvert) > 0: stats["optimisable-png-count"] = len; stats["projectHasUnconvertedPngs"] = 1
- png2pdfActive = request.Png2pdf && Png2PdfEnabled(); if !active: shouldConvert = empty
- if pngModeChanged (lastPng2pdf != request.Png2pdf) && active: for each .png path NOT in
  shouldConvert: hash → url; if isConversionCached(projectID, url, epoch0): shouldConvert.add(path)
- pngModeChanged computed from loadSnapshot result {png2pdf: lastPng2pdf} (default false)

### 10.8 Test plan (port HistoryResourceWriter.test.js; fake seams, NO Docker)
Node test (327L) mocks Settings/logger/metrics/fetch-utils/UrlCache/Png2Pdf/TikzManager/
DraftModeManager/CLSICacheHandler/ResourceWriter/Metrics. Go equivalents: t.Setenv +
config.ForTest() for CLSI_CACHE_PATH / FILESTORE_PARALLEL_FILE_DOWNLOADS /
PNG2PDF_MIN_FILE_SIZE_BYTES; inject seams (10.1) with fakes:
- fake download: writes "png-bytes" to destPath; with suffix: if url not in optCache
  → add, return ConversionHandle{conv, cache, dest}; else nil (cached)
- fake isConversionCached: optCache.has(url)
- fake commitConversion: move conv→cache, copy cache→dest
- fake PngConvert: records calls
- fake fetchString: 404-able (to exercise NotFound), else returns "" for empty hash
- request rawSnapshot fixture: files {fig.png: {hash, byteLength}, main.tex: {content:"hello"}}
  → otc.SnapshotFromRaw with files map (Node rawSnapshot shape = {files: {...}})
Scenarios: (a) saveSlowPngList writes png2pdf-slow.json; (b) slow-list gating: sync1
no slow → normal download (no suffix), sync2 after SaveSlowPngList(["fig.png"]) →
download with suffix + PngConvert + commit; sync3 → cached, no conversion (attempt-once);
(c) mode switch: off → reverts (no suffix), on → re-serves .opt (suffix, but conversion
is NOT run since cache hit → downloadUrlToFile returns nil handle); (d)-(f) analytics:
no slow → no stats; png2pdf off + slow → projectHasUnconvertedPngs=1 + optimisable-png-count;
on → converts; below threshold → skipped-small counter, no stats; two slow PNGs →
count 2; mixed sizes → count 1 (only above-threshold counted).
Plus: clsi-cache populate path (fake DownloadHistorySnapshot returns resync file),
populate fail + no rawSnapshot → re-throw "needs more updates" (maxLocalBaseVersion
from MissingUpdates info), draft branch (PREFIX + writeFile), tikz branch (output.tex
skipped when snapshot has output.tex), nested dir discovery/removal (extraneous dir
removed when no child files), ensureHasParentFolder (parent in entries → early return),
changesFromRaw (add+move+edit ops), fetchString 404→NotFound, fullSync (all paths changed,
resync deleted), incremental dirty set.

### 10.9 IMPLEMENTATION STATUS (2026-09-24, COMPLETE — coverage gate MET 94.0%)

Files (in `services/clsi.go/historyresourcewriter/`, all UNCOMMITTED):
- `historyresourcewriter.go` (639L): seams (§10.1), Request/Result (§10.2),
  snapshotPaths/SaveSlowPngList/loadSlowPngList/ClearCache, loadSnapshot* +
  saveSnapshot (gzip level 1, tmp O_CREATE|O_EXCL then rename) +
  deleteResyncSnapshot, discoverExistingEntries / removeExtraneousEntries /
  ensureHasParentFolder (insertion-order entryList — children before parents,
  Go map iteration is nondeterministic so discovery stays via sorted readdir),
  changesFromRawChangeOperations (§10.4, error-returning), hrwBlobStore
  (§10.3: 4-branch getBlobURL, 3-attempt fetchString, 404 →
  errors.NewNotFoundError), isPng, helpers.
- `sync.go` (491L): SyncResourcesToDisk main flow (§10.5 steps 1–17),
  dedupeGlobalBlobs, changedPathsFromSnapshot, resourceListFromSnapshot,
  reservableFromOptCache (mode-switch re-serve), writeString, epochZero.
- `historyresourcewriter_test.go` (461L): fakeSeams (optCache fake per §10.8),
  setup() (t.Setenv + config.ForTest pattern, sandbox envs pinned), makeRequest,
  syncOnce/syncResult, 9 GREEN tests: SaveSlowPngList, slow-list-gated
  conversion (sync1/sync2/sync3 attempt-once), ReServeOptimisedAfterModeSwitch,
  AnalyticsNoSlowList, AnalyticsSlowPngPng2pdfOff/On, AnalyticsBelowThreshold,
  AnalyticsMultipleSlowPngs, AnalyticsMixedThreshold.
- `historyresourcewriter_cov_test.go` (~230L) + `historyresourcewriter_cov2_test.go`
  (~290L): close every §10.8 "Plus" gap — clsi-cache populate end-to-end (seam
  writes resync → load source clsi-cache → post-sync delete; populate error/no-ok
  → remote fallback; corrupt history.json.gz → warn + remote fallback), MissingUpdates
  tracking (resync maxInt accumulation history lv5 + resync lv2 → final lv5 with
  noRawSnapshot), draft PREFIX + dirty re-save (real draftmodemanager.PREFIX),
  tikz output.tex WriteOutputFileIfNeeded success + "write failed" tag, hollow
  byteLength-only "file without content and hash" path, CreateProjectDir error →
  "write failed", download-error continue (blob fail, main.tex still written),
  PngConvert/CommitConversion error swallow, ClearCache RemoveAll-error branch,
  SaveSlowPngList nil→`[]`, loadSnapshotFromFile corrupt-gunzip + bad-JSON paths,
  saveSnapshot rename/MkdirAll failures, removeExtraneousEntries Remove-error,
  ensureHasParentFolder (parent-as-file), defaultPngConvert bridge (stats-on-
  success / timings-always via png2pdf.RunFunc), incremental (non-full-sync)
  ApplyOps (dirty + Add/Move/Remove/noOp + restored-deleted + re-save with
  advanced lv) and IncrementalModeChange (mode-switch re-serve + opt-not-cached
  convert path + commit count), gunzipBytes truncated-stream error.

Final numbers (2026-09-24, `go clean -testcache -cache && go test ./... -cover`):
- HRW pkg **94.0%** (gate MET; all 35 CLSI pkgs ≥ 90%).

Verified this session:
- `go build ./historyresourcewriter/` + `go vet` clean; `go test` GREEN;
  pkg coverage 94.0% (gate = 90%, MET 2026-09-24). clsi module `go build ./...` + `go test
  ./...` all green.
- otc dependency committed: `b74bb81` (safe_pathname oracle GREEN + CLSI
  replica). otc file_map.go/snapshot.go diff (FileMapFromAny/SnapshotFromRaw
  accepting the JSON-decoded `map[string]any` files shape — requestparser
  hands HRW JSON-decoded raw, not ToRaw's typed shape) is UNCOMMITTED and
  ships WITH the HRW commit (it is the HRW consumer).
- otc `go build` + `go test` GREEN at repo root after those edits.

Coverage: ALL gaps closed (2)–(11) exercised. HRW pkg 94.0%, gate 35/35 ≥ 90%.
Commit HRW + otc file_map/snapshot diff.

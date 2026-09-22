# CLSI Node→Go — HANDOFF (services/clsi.go)

## 0. CURRENT STATE (authoritative — updated 2026-09-24)

```
STATUS: CLSI 33 packages ported (29/33 ≥ 90% gate). dockerode/otc duplication RESOLVED:
  clsi/ot DELETED (commit 83e7967) in favor of shared otc (LIB-15, user directive 09-21);
  clsi/resourcewriter imports ollitex otc; safe_pathname oracle (80,782 rows) attached to
  otc (harness go/libraries/otc/safe_pathname_oracle_test.go, commit 83e7967).
Dockerrunner: IN PROGRESS (design locked §4/12; engine SPI written; pipeline+unixengine+
  tests remain — see §1 row 4, §8).
Build: go build = OK (non-dockerrunner) | go vet = clean | go test: 29/33 pkgs green
Cover: 4 packages below 90 gate (to fix): errors 79.7%, xrefparser 86.7%, config 89.3%,
  metrics 69.2% (metrics+errors dropped after upstream merge d4f9c24 — re-test).
minimatch: ACCEPTED + COMMITTED+PUSHED (a518e9c; 49,828 oracle rows, 0 mismatches).
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
    root module `ollitex`). clsi/resourcewriter is the current consumer.
  - Phase C history (upstream go modules, via merged main): slices 2–6 + minimatch +
    Phase C1/C2/C3/C4 + Phase B4 all merged (d4f9c24 merge). Phase B (BlobStore
    backends) history remains OPEN upstream.
  - safe_pathname oracle: 80,782 rows, Node-generated (gen via /tmp/otto/d3.js +
    /tmp/otfuzz fuzzer), attached at commit 83e7967. Divergence class (documented in
    otc/handoff): line-terminator guard `{0x0A,0x0D,0x2028,0x2029}` — Go port rejects
    `{0x00-0x09,0x0B-0x0C,0x0E-0x1F}` (JS `\s` vs Go `\p{L}` differences). NOT a bug,
    NOT a gate failure — otc is in the acceptance-accepted (divergence documented)
    category.

CLSI remaining modules (in §1 order):
  - error middleware port (from app.js `err` handler — see §4/12.2)
  - dockerrunner (design §4/12 + §8)
  - compile core: compilemanager (1021L) + compilecontroller (491L) — DEPENDS ON
    dockerrunner for fake-injection in tests
  - historyresourcewriter (869L) — DEPENDS ON otc (Phase B open)
  - apps/server (route table §3.2) + load agent (TCP 3048 + HTTP 3049)
  - cmd/clsi main + Makefile + live smoke (needs Docker texlive image)
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
5. **NFS flakiness**: write large files to `/tmp/` then `cp` into place (single command);
   heredoc at ≤250L per chunk (longer gets eaten); re-`cp` + `gofmt` + `go build` +
   `go test` in ONE atomic bash call after each large `write`.

## 1. Task List

| # | Todo id | Task | Status |
|---|---------|------|--------|
| 0 | TODO-9e50c129 | Node baseline (unit + acceptance) as parity reference | BLOCKED (PnP `.pnp.cjs` missing in this repo copy — see §2.1). Test EXPECTATIONs in `services/clsi/test/` = practical spec. |
| mm | (oracle) | minimatch port | ACCEPTED (a518e9c pushed; 49,828 oracle rows; 78.7% cover; divergence README'd). |
| 1 | TODO-e5399221 | config (175L) | DONE 89.3% (gate 90% — re-test after upstream merge; may differ) |
| 2 | errors/req/lp/logger | errors, requestparser, lastprojectaccess, logger | DONE: errors 79.7%, requestparser 90.0%, lastprojectaccess 100.0%, logger 100.0 |
| 5 | TODO-d9502cb0 | Output side: clsicache (528L), contentcachemanager (447L), outputcachemanager (688L) | IN PROGRESS — clsicachehandler 93.7%, contentcachemanager 96.7%, outputcachemanager 92.1% |
| 6 | TODO-fac1ee46 | Content cache: resourcewriter, historyresourcewriter, urlcache, urlfetcher | IN PROGRESS — urlcache 96.6%, urlfetcher 93.8%, **resourcewriter 93.2% (otc wired, minimal divergences)**; historyresourcewriter 15 open |
| 3 | TODO-6a350144 | Compile core: commandrunner (19L), compilemanager (1021L), compilecontroller (491L) | IN PROGRESS — commandrunner 100.0%; compilecontroller ported; compilemanager 15 (otc B dep) |
| 7 | TODO-843bd163 | Conversion: tikzmanager (129L), png2pdf (96L), conversionmanager (936L), conversionoutputcleaner (port) | DONE 2026-09-20: tikzmanager 94.4%, png2pdf 92.9%, conversionmanager 91.4%, conversionoutputcleaner 100% |
| 4 | TODO-02cae9d7 | DockerRunner (634L) → `dockerrunner` (hand-rolled HTTP-over-unix Engine SPI) + fake for tests | IN PROGRESS (design §4/12; engine SPI written; pipeline+fake+unixengine+tests remain — see §8) |
| 8 | TODO-d7feecff | Server layer: app (route table), load agent (TCP 3048 + HTTP 3049), /status /health_check /smoke_test_force /metrics, error middleware | OPEN (design §4/12 below) |
| 9 | TODO-f464d516 | cmd/clsi main + Makefile + live smoke + Node parity matrix | OPEN |

Coverage log (strict per-package ≥ 90%; 2026-09-24 remeasure):

| package | cover | package | cover |
|---|---|---|---|
| clsicachehandler | 93.7 | outputcontroller | 100.0 |
| commandrunner | 100.0 | outputfilearchivemanager | 90.2 |
| config | **89.3** ❌ | outputfilefinder | 94.6 |
| contentcachemanager | 96.7 | outputfileoptimiser | 96.7 |
| contentcachemetrics | 91.5 | png2pdf | 92.9 |
| contentcacheworker | 100.0 | requestparser | 90.0 |
| conversionmanager | 91.4 | resourcestatemanager | 93.4 |
| conversionoutputcleaner | 100.0 | resourcewriter | 93.2 |
| dockerlockmanager | 90.2 | safereader | 95.7 |
| draftmodemanager | 90.0 | statsmanager | 100.0 |
| errors | **79.7** ❌ | synctexparser | 93.9 |
| fileuploadmiddleware | 93.9 | tikzmanager | 94.4 |
| lastprojectaccess | 100.0 | urlcache | 96.6 |
| latexmetrics | 91.1 | urlfetcher | 93.8 |
| latexrunner | 94.3 | xrefparser | **86.7** ❌ |
| lockmanager | 100.0 | logger | 100.0 |
| metrics | **69.2** ❌ | | |

**Coverage gate: 29/33 pkgs ≥ 90%.** Below: errors (79.7%), xrefparser (86.7%), config
(89.3%), metrics (69.2%) — re-test after upstream merge (d4f9c24) since upstream may
have touched these files.

Coverage gate cmd: `cd services/clsi.go && go clean -testcache -cache && go test ./... -cover`.

**IMPORTANT**: `go clean -testcache -cache` between coverage generations (stale profiles).

## 2. Environment

- Go **1.27.1** at `/usr/local/go`. Node 24.13.0, Docker Engine 29.5.3 (ApiVersion 1.54,
  MinAPIVersion 1.40 — old-style routes). gcc 15.2.0.
- Docker backbone for CLSI: **hand-rolled HTTP-on-unix client** (zero-dep policy, §4/1).
- Docker socket: `/var/run/docker.sock` (probe:
  `curl -s --unix-socket /var/run/docker.sock http://localhost/containers/json`).
- Docker images available: `test_unit_clsi-test_unit:latest` (1.7GB), `hello-world` (25MB).
- Node 24 / yarn 4: **PnP install broken in THIS copy** (`.pnp.cjs` missing) — tests are
  available AS FILES (test/unit/js, test/acceptance/js), Docker run works via dockerode
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
  CompileManager.js (1021L)     → compilemanager.go (OTC Phase B dep)
  HistoryResourceWriter.js (869L) → historyresourcewriter.go (OPEN — otc dep)
  ResourceWriter.js (398L)      → resourcewriter.go (DONE 93.2%, otc wired)
  DockerRunner.mjs (634L)       → dockerrunner (design §4/12, SPI written)
  OutputCacheManager.js         → outputcachemanager.go (DONE 92.1%, §5/7 LOCKED)
  OutputFileFinder/Optimiser/
  ArchiveManager                → outputfilefinder.go (DONE 94.6%)/
                                  outputfileoptimiser.go (DONE 96.7%)/
                                  outputfilearchivemanager.go (DONE 90.2%)
  OTC → go/libraries/otc (shared LIB-15; Phase B open; applied via clsi/resourcewriter)

Content / URL cache
  CLSICacheHandler.js (528L)    → clsicachehandler.go (DONE 93.7%)
  ContentCacheManager.js (447L) → contentcachemanager.go (DONE 96.7%)
  UrlCache.js (227L) / UrlFetcher.js (111L) → urlcache.go (DONE 96.6%) /
                                           urlfetcher.go (DONE 93.8%)

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
  CLSICacheHandler.js           → clsicachehandler.go (DONE 93.7%)
  CLSICompileQueue.js           → (in compilecontroller; not yet ported)
  Metrics.js / LatexMetrics.js  → metrics.go (DONE 69.2% — RE-TEST after upstream) /
                                   latexmetrics.go (DONE 91.1%)
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
  seams directly. Test file ≈31KB, staged via /tmp + cp (NFS-safe).
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
- [ ] (2026-09-24) Coverage re-measure: **4 pkgs below gate** — errors 79.7%, xrefparser
  86.7%, config 89.3%, metrics 69.2% (upstream merge moved some files; needs re-test).

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
- **NFS instability**: write large files to `/tmp/stage/` then `cp` + `gofmt` + `go build` +
  `go test` in ONE atomic bash command. **Write tool truncates >~300 lines** — use
  `cat > file <<'EOF'` heredocs for large Go files. **Bash heredoc truncates at ~250
  lines** — chunk Go files at ≤120L per heredoc; `gofmt -e` after EACH chunk before
  appending more. **`edit` tool on a corrupted file = chaos** — when a staged file gets
  corrupted from partial edit applications, the ONLY safe recovery is `cp` from the
  last-known-good build (e.g. mmgo) and re-applying the single needed fix, NOT another
  edit chain on the corrupt file.
  `go test` in ONE atomic bash command. **Write tool truncates >~300 lines** — use
  `cat > file <<'EOF'` heredocs for large Go files.
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

1. **dockerrunner** (IN PROGRESS this session). Design locked §4/12. Files:
   `dockerrunner.go` (DockerRunner struct + Runner interface + New),
   `engine.go` (Engine SPI: `Inspect(id)`, `Create(id, CreateOpts)`,
   `Start(id)`, `Kill(id)`, `Wait(id) io.ReadCloser + <int exit>`, `Attach(id)
   io.ReadCloser`, `Destroy(id)`), `fingerprint.go` (md5 of CreateOpts JSON).
   Remaining: `pipeline.go` (runOnce gate model), `unixengine.go` (blocking
   HTTP-over-unix for production), `dockerrunner_test.go` (FakeEngine driven).
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

2. **Fix 4 packages below 90 gate** (errors 79.7%, xrefparser 86.7%, config 89.3%,
   metrics 69.2%) — first check whether upstream merge (d4f9c24) already
   pushed them over 90 (re-test after clean -testcache).

3. **compile core** (compilemanager + compilecontroller) — depends on otc
   (Phase B still OPEN upstream; clsi/ot deleted → import shared otc).

4. **error middleware + server layer** (§4/12 + §3.2 locked design above).

5. **clsi_typst.go** (NEW — per user directive 2026-09-21): port
   `services/clsi_typst` to `services/clsi_typst.go`, feature-equivalent to
   clsi.go (reuses `go/libraries/otc` LIB-15 for content), reuses clsi.go
   packages where possible, HANDOFF.md in `clsi_typst.go/`. (This is a
   SEPARATE Go module from clsi.go.)

6. **cmd/clsi main + Makefile + live smoke** (Docker texlive image needs to be
   built — blocked on `test_unit_clsi-test_unit:latest` availability).

Deferred (otc-dependent, upstream Phase B):
- HistoryResourceWriter (15 open — otc Phase B dep)
- CompileManager (depends on otc Phase B + dockerrunner)

Blocked / environment:
- Docker texlive image for live smoke (`test_unit_clsi-test_unit:latest` at
  1.7GB available; texlive variant NOT yet built).

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
- **NFS flakiness** (2026-09-24): hit twice more this session (HANDOFF.md
  cp + go build). Use `/tmp/` staging + atomic `cp + gofmt + go build + go
  test` in ONE bash command. **Bash heredoc truncates at ~250 lines** — chunk
  Go files ≤120L per chunk.
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
1. historyresourcewriter.go (port from HistoryResourceWriter.js 869L — needs otc: blob_store,
   history, snapshot, file_data; local BlobStore extends ollitex BaseBlobStore like Node
   BlobStoreBase)
2. compilemanager.go (1021L) — deps: resourcewriter (93.2), latexrunner (94.3), dockerrunner,
   lockmanager (100), clsicachehandler (93.7), contentcachemetrics, statsmanager, synctexparser,
   tikzmanager, safeReader, latexmetrics, clsi-metrics, errors, png2pdf, commandrunner, config.
   CompileController (491L, route layer) AFTER — it's mostly z-schema request parsing + wire.
3. error middleware + apps/server (route table §3.2, load agent TCP 3048 + HTTP 3049)
4. cmd/clsi main + Makefile + live smoke (Docker texlive image; only test_unit image present →
   smoke limited to /v1/version + compile-not-allowed + monitor; see §8)
5. clsi_typst.go (new service; feature-equivalent, reuses otc + shared packages; own HANDOFF per
   next-session notes §8)

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

# CLSI Node → Go 1:1 Port — HANDOFF

> **Read this first.** This file is the single source of truth for a session
> taking over the port. Update it at the end of every session.

Repo: `/home/davrot/compile/OlliTeX_comp` (NEVER type this path in bash by hand —
use the stable anchor `/tmp/clsi_proj` → same dir). The directory name is CASE-
SENSITIVE and is `OlliTeX_comp` (TeX, not Tex: the `TeX` in `OlliTeX`). A one-char
bit-flip in the name (e.g. `Ollitex_comp`) creates a stray shadow dir instead of
hitting the mount, so verify with `test -d`; it is NOT an NFS flakiness symptom.
Node source (reference only, do NOT delete): `services/clsi/` (module `@overleaf/clsi`, ESM)
Go target: `services/clsi.go/` (standalone module `clsi`, Go 1.27)

For package-level module state, `services/clsi.go/HANDOFF.md` is co-authoritative
(it covers the same content in more detail). The two files must not diverge:
when updating one, update the other.

## 0. CURRENT STATE (authoritative — updated 2026-09-24)

```
STATUS: 34 CLSI leaf packages ported + dockerrunner DONE (90.9%), ALL 34
        packages pass the strict ≥ 90% coverage gate. NEXT ACTIVE:
        historyresourcewriter PRODUCTION WRITE (in progress — all Node + Go
        API investigation complete, at the write stage; design + seams in
        services/clsi.go/HANDOFF.md §8 step 1).
        Docker daemon ALIVE (Engine 29.5.3). Branch go_compile_test: HEAD
        5f6e609, origin/go_compile_test at b44b4b3 (1 commit UNPUSHED:
        5f6e609 HANDOFF-only). Upstream merges #1-#3 done. clsi/ot DELETED
        (commit 83e7967) — Go CLSI consumers use the shared otc package
        (go/libraries/otc) as single source of truth.

Unstaged (do NOT lose): errors.go + missing-updates helpers (HRW-ready),
        metrics.go + Png2pdfSkippedSmall counter (HRW-ready), both HANDOFFs.
        Untracked: .pi/ (NEVER commit) + services/clsi.go/historyresourcewriter/
        (STALE DRAFT — replace with production package, see below).

Build: go build ./... = OK | go vet ./... = clean | go test ./... -count=1
        = ALL 34 pkgs ok.
Cover: ALL CLSI PACKAGES >= 90% (strict per-package re-measure 2026-09-24;
       table in §1). HRW package NOT yet measured (draft is not the port).
```

**otc (go/libraries/otc) status:** oracle (80,782 rows, Node-generated)
COMMITTED and green EXCEPT safe_pathname, INTENTIONALLY RED until upstream
fixes: 28,634/80,782 mismatches (3 defect classes — U+FEFF whitespace,
per-rune vs per-UTF-16-unit surrogate matching, missing JS '.' line-terminator
guards; spec: go/libraries/HANDOFF_SAFE_PATHNAME.md). Everything else in otc
is green, INCLUDING Phase B `blob_store_base` (2026-09-24) which HRW consumes:
- `otc.Snapshot.LoadFiles(ctx, kind, bs)` — serial, per-file, SWALLOWS errors (known divergence from Node parallel; documented).
- `otc.Snapshot.GetFile`, `GetPathnames` (sorted; Node raw Map order is non-deterministic anyway — see §6), `ToRaw`.
- `otc.NewChange(ops []Operation, ts, authors, origin, v2Authors, projectVersion, v2)` + exported `Operations` field (HRW synthetic-change seam).
- `otc.NewBlob(hash, byteLength, stringLength)` + `otc.NewBlobNotFound(hash)`.
- `otc.BaseBlobStore{FetchString, PutStringFn, PutObjectFn}` — HRW EXTENDS it by supplying `FetchString` (the 4-branch getBlobURL + 3-attempt retry loop; Node's `class BlobStore extends BlobStoreBase` equivalent). `GetBlob` default nil,nil matches Node.
- `ollitex/go/libraries/fetchutils` (`fetchutils.FetchString(ctx, url, *Options)`,
  `RequestFailedError{Status int, Response *http.Response, ...}`):
  404 check is `fErr, ok := err.(*fetchutils.RequestFailedError); ok && fErr.Status == 404`
  (Response may be nil for non-HTTP failures; the Status field is always set).

**DockerRunner (634L `DockerRunner.mjs`) → package `dockerrunner` — COMPLETE
(90.9%). Wire facts below were the provenance (oracle probes against live
engine + dockerode 4.0.9 source; probe program /tmp/clsi_probe) and are
now encoded in dockerrunner/engine.go + unixengine.go + dockerrunner.go:**
- **Route names**: Docker Engine uses OLD routes: `GET /containers/{id}/json`
  (inspect) — NOT `/inspect` (404 "page not found"). Create: `POST
  /containers/create?name=X` with FULL body (name is QUERY-only; engine 307s if
  both). Attach: `POST /containers/{id}/attach?stdout=1&stderr=1&stream=1`
  (POST-only, 200 raw byte stream, NOT hijacked). Wait: `POST
  /containers/{id}/wait` (GET → 404). Vanished container → 404 `{"message":"no such container: <id>"}`.
- **Kill wire**: `POST /containers/{id}/kill` → 409 `{"message":"cannot kill
  container <ID>: container <short> is not running..."}` (lowercase "cannot" —
  Node's `/Cannot kill/` regex does NOT match live engine text → error
  PROPAGATES. Port verbatim, do NOT "normalize").
- **dockerode error format** (modem.js): `"(HTTP code " + code + ") " +
  message + " - " + error + " "` — REPRODUCED in the Go unix engine so
  downstream regexes behave identically.
- **Start 304** = "already running". Lock = inspect→create→attach→start
  (released when start response known; not held during wait). destroyOldContainers
  (name = `project-*` regex + created*1000 + cfg.MaxContainerAge ≤ now → force
  remove; skip if projectId matches regex in name AND lastAccess recent).
  Ulimit cpu soft=timeout/1000+5 hard=timeout/1000+10, Memory=1024^4,
  NetworkDisabled, CapDrop ALL, env merge sorted (documented divergence from
  Node insertion order), $COMPILE_DIR first-occurrence replacement only.
  Gate model: callback fires ONLY when streamEnded AND containerReturned.
- `fired` flag: LatexRunner records processTable[id]=rname only if sync
  callback did NOT fire.

NOT yet ported (in order):
1. **historyresourcewriter** (IN PROGRESS — production write in progress;
   design 100% locked, see services/clsi.go/HANDOFF.md §8 + §10 HRW section).
2. **compilemanager** (1021L) + **compilecontroller** (491L) → uses HRW Result.
3. **apps/server** (route table, load agent TCP 3048 + HTTP 3049,
   /status /health_check /smoke_test_force /metrics, error middleware).
4. **cmd/clsi main + Makefile + live smoke** — unblocked: `texlive/texlive:latest-full`
   IS present (pulled 2026-09-24, 2.74GB). `hello-world` also present for wire
   smoke. test_unit_clsi image present.
5. **clsi_typst.go** (NEW service — sibling of clsi, Typst-only;
   feature-equivalent, reuses clsi.go + otc where appropriate, own
   HANDOFF.md; starts after dockerrunner is green — THAT IS NOW TRUE).
6. When upstream otc safe_pathname fix complete: re-run from repo root
   `go test ./go/libraries/otc` and confirm oracle green
   (committed red until then — 28,634/80,782 mismatches; spec in
   go/libraries/HANDOFF_SAFE_PATHNAME.md). This is UPSTREAM scope and does NOT
   block CLSI porting.

**Takeover procedure (do these FIRST):**
1. `ln -sfn /home/davrot/compile/OlliTeX_comp /tmp/clsi_proj` (symlink may be missing
   in a new container).
2. `cd /tmp/clsi_proj/services/clsi.go && go build ./... && go vet ./... && go test ./... -count=1`
   — ALL GREEN (34 pkgs). The otc safe_pathname oracle (`go test ./go/libraries/otc`
   from repo root) is red BY DESIGN (upstream bug).
3. Skim this §0 + §8 (ordered tasks). Do NOT re-derive the Docker wire facts —
   all proven in §0 and now CODED in dockerrunner/. **§7 = locked OCM design (skip re-research).**
4. `git status` at repo root — untracked should be `.pi/` (never commit) +
   `services/clsi.go/historyresourcewriter/` (stale draft, to be REPLACED by the
   production package — see "HRW draft" note in services/clsi.go/HANDOFF.md §0).
   Push pending: `git push origin go_compile_test` (HEAD is 1 commit ahead:
   5f6e609 handoff-only).
5. Then implement HRW per the locked design (service-level seams: package-level
   function vars, NOT vi.doMock); keep all 34 pkgs ≥ 90%; commit.

## 1. Task List

| # | Todo id | Task | Status |
|---|---------|------|--------|
| 0 | TODO-9e50c129 | Node baseline (unit + acceptance) as parity reference | BLOCKED (PnP `.pnp.cjs` missing in this repo copy — see §2.1). Test EXISTENCE + expectations in `services/clsi/test/` is the practical spec; Docker baseline works in CI. |
| 5 | TODO-d9502cb0 | Output side: outputcachemanager (688L), contentcachemanager (447L), clsicachehandler (528L) | DONE — OCM 92.1%, CCM 96.7%, CLSICacheHandler 93.7%, outputcontroller 100%, outputfile* family. Wordcount-route piece folds into compilecontroller. |
| 6 | TODO-fac1ee46 | Content cache: resourcewriter (398L), historyresourcewriter (869L), urlcache (227L), urlfetcher (111L) | urlcache 96.8%, urlfetcher 96.2%, resourcewriter 93.2% (imports go/minimatch) DONE; historyresourcewriter NEXT (design locked — no longer blocked: otc Phase A+B done). |
| 3 | TODO-6a350144 | Compile core: commandrunner (19L), compilemanager (1021L), compilecontroller (491L) | IN PROGRESS — commandrunner DONE (1:1, 100%); compilemanager + compilecontroller remain (compilemanager now unblocked: dockerrunner + otc ready, after HRW). |
| 7 | TODO-843bd163 | Conversion path: tikzmanager (129L), png2pdf (96L), conversionmanager (936L), latexmetrics (408L) | DONE — tikzmanager 94.9%, png2pdf 93.4%, conversionmanager 91.6%, latexmetrics 91.9%, fileuploadmiddleware 93.9% |
| 4 | TODO-02cae9d7 | DockerRunner (634L) → `dockerrunner` (native Docker Engine API over unix.sock) + fake for tests | DONE — engine.go + pipeline.go + monitor.go + unixengine.go + dockerrunner.go + fingerprint.go + dockerrunner_test.go (FakeEngine + 90.9%) |
| 8 | TODO-d7feecff | Server layer: app (route table from app.js), load agent (TCP 3048 + HTTP 3049), /status /health_check /smoke_test_force /metrics, error middleware | open |
| 9 | TODO-f464d516 | cmd/clsi main + Makefile + live smoke + Node parity matrix | open (UNBLOCKED: texlive/texlive:latest-full present) |
| 10 | (session) | Maintain this HANDOFF.md + todos | ongoing |
| 11 | TODO-90972736 | Port every remaining app module (see top-row of this table each time) | IN PROGRESS |

**Coverage — ALL PASS (strict per-package re-measure 2026-09-24, `go
clean -testcache -cache` + `go test <pkg> -coverprofile -count=1` for each):**

| package | cover | | package | cover |
|---|---|---|---|---|
| commandrunner | 100.0 | | outputfilearchivemanager | 91.5 |
| logger | 100.0 | | draftmodemanager | 93.1 |
| lockmanager | 100.0 | | requestparser | 93.7 |
| statsmanager | 100.0 | | metrics | 100.0 |
| contentcacheworker | 100.0 | | errors | 98.4 |
| outputcontroller | 100.0 | | config | 93.0 |
| lastprojectaccess | 100.0 | | xrefparser | 97.8 |
| latexrunner | 95.6 | | dockerrunner | 90.9 |
| clsicachehandler | 93.9 | | outputfileoptimiser | 97.7 |
| contentcachemanager | 96.7 | | latexmetrics | 91.9 |
| contentcachemetrics | 93.1 | | conversionmanager | 91.6 |
| outputcachemanager | 92.1 | | outputfilefinder | 98.2 |
| dockerlockmanager | 93.3 | | safereader | 94.2 |
| synctexparser | 93.9 | | tikzmanager | 94.9 |
| resourcewriter | 93.2 | | fileuploadmiddleware | 93.9 |
| resourcestatemanager | 94.6 | | png2pdf | 93.4 |
| urlcache | 96.8 | | urlfetcher | 96.2 |

**Gate status: 34/34 packages ≥ 90% — MET** (metrics/errors/config/xrefparser
lifted 2026-09-24; dockerrunner 90.9% first full measure). Method:
`cd services/clsi.go && go clean -testcache -cache && go test clsi/<pkg>
-coverprofile=/tmp/x.prof -count=1 && go tool cover -func /tmp/x.prof | tail -1`.
Re-measure per package before trusting any number.

## 2. Environment (verified)

- Go **1.27.1** at `/usr/local/go`. Node 24.13.0, Docker 29.5.3, gcc 15.2.0.
- Docker images present: `texlive/texlive:latest-full` (2.74GB, pulled
  2026-09-24 — live compile smoke UNBLOCKED), `hello-world`,
  `clsi/test_unit_clsi` (Node baseline harness).
- Docker backbone for Go: hand-rolled HTTP-on-unix client (decision §4/1) — zero-dep policy.
- Node 24 / yarn 4 present, **PnP install broken in this copy** (see §2.1).
- CLSI module: `go 1.27` in go.mod; **NO external Go deps** (pure stdlib).

### 2.1 Parity reference without the suite running
The full Node test corpus is available AS FILES:
- `services/clsi/test/unit/js/*.test.js` (vitest) — 19 files + fixtures.
- `services/clsi/test/acceptance/js/*.js` (mocha) — 15 files + `fixtures/examples/*`
  (23 LaTeX projects with `output.pdf` + `output.pdfxref` + options.json).
- `services/clsi/test/smoke/js/SmokeTests.js`.
Port expectations 1:1 from the test files (they encode the exact contract).

### 2.2 CCM oracle fixtures (node v24 probe, MINIMAL.PDF — verified byte-exact)
- fixture: `services/clsi/test/acceptance/fixtures/minimal.pdf` (12313 B)
- minChunk=500  → obj "9 0 " start 1074 end 11235 (10161 B, hash `d7cfc73a...`),
  obj "10 0 " start 11240 end 11784 (544 B, hash `896749b8...`)
- minChunk=1024 → only obj 9 (10165 ≥ 1024; obj 10 size 549 < 1024)
- snapshot chunks exist at
  `services/clsi/test/unit/js/snapshots/minimalCompile/chunks/{d7cfc73a..., 896749b8...}`
- CCM Go package is COMPLETE at 96.7% and the numbers above are asserted in its tests.

## 3. Architecture Map (Node → Go)

```
Node service
  app.js                → apps/server (NOT yet ported)
  config/settings.defaults.cjs → config/config.go (DONE 93.0%)

Controllers (HTTP edge)
  CompileController.js (491L)   → compilecontroller/ (NOT yet ported)
  OutputController.js (31L)     → outputcontroller/ (DONE 100%)

Compile core
  CompileManager.js (1021L)     → compilemanager/ (NOT yet ported)
  HistoryResourceWriter.js (869L) → historyresourcewriter/ (design locked — NEXT)
  ResourceWriter.js (398L)      → resourcewriter/ (DONE 93.2%)
  DockerRunner.mjs (634L)       → dockerrunner/ (DONE 90.9%)
  OutputCacheManager.js (688L)  → outputcachemanager/ (DONE 92.1%)
  OutputFileFinder.js           → outputfilefinder/ (DONE 98.2%)
  OutputFileOptimiser.js        → outputfileoptimiser/ (DONE 97.7%)
  OutputFileArchiveManager.js   → outputfilearchivemanager/ (DONE 91.5%)

Content / URL cache
  CLSICacheHandler.js (528L)    → clsicachehandler/ (DONE 93.9%)
  ContentCacheManager.js (447L) → contentcachemanager/ (DONE 96.7%)
  UrlCache.js (227L) / UrlFetcher.js (111L) → urlcache/ / urlfetcher/ (DONE)

Locking / persistence
  LockManager.js                → lockmanager/ (DONE 100%)
  LastProjectAccess.js          → lastprojectaccess/ (DONE 100%)
  ContentCacheMetrics/Worker    → contentcachemetrics/ + contentcacheworker/ (DONE)
Route table (must match exactly; `app.js` is the spec):

| Method | Path |
|--------|------|
| POST | /project/:project_id/compile |
| POST | /project/:project_id/compile/stop |
| DELETE | /project/:project_id |
| GET | /project/:project_id/sync/code |
| GET | /project/:project_id/sync/pdf |
| GET | /project/:project_id/wordcount |
| POST | /project/:project_id/wordcount |
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

## 4. Decisions (LOCKED — do not revisit without evidence)

1. **Docker backbone**: hand-rolled HTTP-on-unix client for the ~10 endpoints used
   (container create/start/kill/wait/logs, volumes list, image inspect). No docker SDK dep.
   (Implemented: dockerrunner/engine.go SPI + unixengine.go; FakeEngine for tests.)
2. **express mux** → `http.ServeMux` (Go 1.22+ patterns `{...}`). Add 404 shim for method mismatch
   (express returns 404, Go mux returns 405).
3. **multer** → `http.Request.ParseMultipartForm` + explicit size limits (maxUploadSize 50MB).
4. **p-limit** → semaphore channel.
5. **workerpool** → single goroutine + job queue (1 worker per Node).
6. **archiver** → `archive/zip` + Go tar.
7. **bunyan** → `log/slog` JSON (LOG_LEVEL env honored). Logger: `logger.Debug/Info/Warn/Error/Err(obj map[string]any, msg string)`
   — `init()` calls `Log = NewLogger(resolveLevel(os.Getenv("LOG_LEVEL")))`.
8. **Metrics** → internal counters + `/metrics` text matching `Metrics.inc` + gauges
   (NOT prometheus). `metrics.Counter{Name, Value int64, Mu}`, `Gauge{Name, Value float64, Mu}`,
   `PdfCachingStatus *Counter` exists; `Inc()` per-counter.
9. **smoke test**: port behavior only if SMOKE_TEST env on (default off).
10. **Tests**: `testing` + `httptest`; TestMain must use `os.Exit(m.Run())` (NOT `return m.Run()`);
    fake Docker client for compile-manager tests; golden PDF assertions for acceptance fixtures.
    Coverage gate: every package ≥ 90% — ALL 34 PASS.

## 5. Progress Log

- [x] (session 1) Scaffold: go.mod, HANDOFF, task breakdown. Read all Node source.
- [x] (session 2) 20 leaf packages ported to ≥ 90% coverage each.
- [x] (session 3) V8 `new Date(string)` parser fully ported into requestparser (v8date.go)
  from the full V8 source tree; 241-case differential oracle (node v24, TZ=Europe/Berlin,
  /tmp/v8oracle.tsv) + waves 2–4 all pass. **PENDING FORMAT PASS**: dual-annotate v8date.go
  (C++ verbatim comment + Go line below each line) + citation header (git@github.com:v8/v8.git
  + BSD license). NO V8 citation in README.md under clsi folder.
- [x] (session 4) outputcontroller (31L) at 100%: CreateOutputZip(res http.ResponseWriter,
  outputDir string, req Request) (int, error). 404→NotFoundError shape (no body), success→
  200 + `Content-Disposition: attachment; filename="output.zip"` +
  `X-Content-Type-Options: nosniff` + Content-Length.
- [x] (session 5) contentcachemanager (CCM, 447L) COMPLETE at 96.7%. Design:
  *port updateSameEventLoop as main path* (worker pool is V8-specific).
  Deadline = min(max(compileTime/4, 1000), pdfCachingMaxProcessingTime) ms.
  Soft timeout only inside loop; error-site types + IsNoXrefError exported.
- [x] (session 6) outputcachemanager (OCM, 688L Node source) COMPLETE at 92.1% (2026-09-17).
  Design (formerly §7) verified against Node 1:1; injectable seams + New() production factory.
  60+ tests. (Full OCM locked design retained below in §7.)
- [x] (2026-09-24 sessions) clsi/ot DELETED (shared otc directive); dockerrunner
  COMPLETE (engine.go SPI + pipeline + monitor + unixengine + dockerrunner.go +
  FakeEngine tests = 90.9%); coverage gate lifted 29/33 → 34/34 (metrics 100,
  errors 98.4, config 93.0, xrefparser 97.8 + dockerrunner 90.9); upstream merges
  #1-#3 into go_compile_test (minimatch conflict resolved in favor of upstream's
  integrated port); otc Phase B `blob_store_base` landed (2026-09-24) unblocking HRW.
- (next) historyresourcewriter production write (design 100% locked —
  services/clsi.go/HANDOFF.md §8 + §10 HRW section), then compilemanager →
  compilecontroller → apps/server → cmd/clsi + live smoke (texlive present)
  → clsi_typst.go.
- (this session) HRW write stage: all Node source (HistoryResourceWriter.js 869L,
  Metrics.js, blob_store_base.js) + test (HistoryResourceWriter.test.js 327L, 8
  scenarios incl. saveSlowPngList/slow-list gating/analytics × 6) re-read in full;
  ALL Go seam signatures verified against live packages (otc BlobStore/Snapshot/Change/File,
  urlcache, png2pdf, clsicachehandler, fetchutils, errors, config, logger,
  requestparser, tikzmanager, draftmodemanager, metrics). Locked:
  (a) seams = package-level function vars (NOT DockerRunner);
  (b) stats/timings stay `map[string]any` in HRW;
  (c) Request = flat struct mirroring Node `request` arg (historyId,
  filestoreBlobPrefix, clsiPerfVariant, globalBlobs, rawSnapshot,
  rawChangeOperations, baseHistoryVersion, populateClsiCache, png2pdf, draft,
  rootResourcePath, compileGroup, metricsPath);
  (d) BlobStore embeds *otc.BaseBlobStore + FetchString 3-attempt retry;
  (e) `timestamp:'0'` synthetic changes → build via NewChange, NOT ChangeFromRaw
  (Go parseRawTime rejects '0' — Node tolerates it); use zero time.Time;
  (f) baseHistoryVersion return = localBaseVersion + len(changes);
  (g) Node `fetchutils.RequestFailedError` 404 = `err.Status == 404`.

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
  sorted readdir + dir-after-children walk. Node Map insertion order for
  `getFilePathnames()` is preserved (unsorted) in HRW (Node source order; Go raw `map` is
  non-deterministic — use sorted `snapshot.GetPathnames()` where only membership matters,
  and documented where order does NOT matter).
- **Node `url.host.includes(host)`** → `strings.Contains(u.Host, host)`.
- **config.ForTest()** requires `SANDBOXED_COMPILES_HOST_DIR_COMPILES` env when sandboxed compiles
  enabled. **createProjectDir is caller's responsibility**.
- **Coverage profile staleness**: `go clean -testcache -cache` between generations.
- **Named function defaults for testable vars**: `var ScheduleAfter = defaultScheduleAfter`.
- **Go map pointers cannot be indexed**; closures self-referencing need `var f func(...); f = func(){...}`.
- **`Path.extname` on Node** uses preDotState state machine (hand-rolled).
- **`archive_logs`/`strace`**: NOT in settings.defaults.cjs (both undefined → false);
  probe confirms `clsi.optimiseInDocker = true`.
- **Large writes (NOT NFS — the mount is fine)**: write large files to `/tmp/stage/` then `cp` + `gofmt` + `go build` +
  `go test` in ONE atomic bash command. **Write tool truncates >~300 lines** — use
  `cat > file <<'EOF'` heredocs (chunk ≤120L with `gofmt -e` after each chunk) for
  large Go files. (2026-09-24 "NFS flakiness" was in fact a case-sensitive path
  bit-flip — `OlliTeX_comp` vs `Ollitex_comp` — creating shadow dirs, not a mount issue.)
- **ALWAYS use `timeout`** on bash commands that touch node/ or v8/ (50k/20k entries).
- **Node readdir order is NON-DETERMINISTIC** (tmpfs/ext4 hash order). Go's `os.ReadDir`
  sorts alphabetically. Documented divergence; do NOT "fix" test expectations for this.

## 7. OCM (outputcachemanager) — FULLY LOCKED DESIGN (2026-09-16)

**Status**: package COMPLETE at 92.1% (tests + coverage above). This section is
retained as the reference design (verified 1:1 against Node); see
`services/clsi.go/outputcachemanager/` for the implementation.

### Package skeleton
```go
package outputcachemanager
```
- Constants: `ContentSubdir = "content"`, `CacheSubdir = "generated-files"`,
  `ArchiveSubdir = "archived-logs"`, `CacheLimit = 2`, `CacheAge int64 = 90*60*1000`.
- Regexp: `buildIdRegexp ^[0-9a-f]+-[0-9a-f]+$`; `fileHiddenRegexp ^\.|/\.`;
  `perUserRegexp ^[0-9a-f]{24}-[0-9a-f]{24}$`; `straceRegex ^strace`.
- Manager struct seams: Now/RandHex/UpdateContent/OptimiseFile/ScheduleAfter/
  MetricsInc/Log (see implementation for exact signatures).
- Path semantics, per-dir pump queue (FIFO, error-isolated, goroutine-started
  lazily), archiveLogs fire-and-forget, collectOutputPdfSize ENOENT hard-fail,
  saveStreamsInContentDir error mapping (NoXrefTableError/QueueLimitReachedError/
  TimedOutError/other) — all per implementation (verified against Node).
- Metrics contract: `Metrics.inc('pdf-caching-status', 1, {status, ...opts})` fires
  for EVERY outcome of saveStreamsInContentDir (success, soft failure, missing-pdf,
  content-dir-unavailable, queue-limit, timed-out, failed). SaveStreams errors are
  swallowed by the outer callback (`logger.warn` + `callback(null, {...})`) — the
  COMPILE STILL SUCCEEDS. Compile FAILS only on generateBuildId error,
  saveOutputFilesInBuildDir error (mkdir/copy), or collectOutputPdfSize stat error.

### CompileManager OCM call sites (for future compilemanager port)
- line 384: `saveOutputFiles` (main compile flow)
- line 589: `BUILD_REGEX` (build id validation)
- line 597: `CACHE_SUBDIR` (path construction)
- line 605: `queueDirOperation` (synctex? verify)
- CompileManager sets `timings.compile` (float, ms) before calling SaveOutputFiles.
- `request.parse` (requestparser) sets `BuildID` on the SaveRequest.

### Node `app.js` OCM call (for future app port)
- `app.js` line 53: `OutputCacheManager.init()` (no args — uses Settings.path.outputDir
  internally). Go: `ocm.Init(cfg.Path.OutputDir)`.

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

## 8. Immediate Next Steps (re-ordered 2026-09-24)

1. **historyresourcewriter** — IN PRODUCTION WRITE (design 100% locked in
   services/clsi.go/HANDOFF.md §8 step 1 + §10: entry point, Request/Result
   types, Go seams, blob URL 4-branch logic, fake list for tests). Replace the
   stale `historyresourcewriter.go` draft (105L, wrong `Runner` interface —
   Node's syncResourcesToDisk does NOT use DockerRunner; stale draft to be
   REPLACED). All seam APIs re-verified this session (signatures in
   services/clsi.go/HANDOFF.md §10): DownloadUrlToFile/IsConversionCached/
   CommitConversion/CreateProjectDir, Png2Pdf.IsEnabled + PngConvert bridge,
   DownloadHistorySnapshot, IsExtraneousFile, fetchstring retry (3 attempts,
   3s each), NewChange(ops, zero-time) for the `timestamp:'0'` synthetic
   changes. Target: package ≥ 90%; fake-backed scenarios from
   `HistoryResourceWriter.test.js` (saveSlowPngList, slow-list gating, mode
   switch, analytics × 6) + local-snapshot/clsi-cache load, draft/tikz
   branches, nested/extraneous removal, download-fail, fetchString retry,
   clearCache, 4 URL branches.
2. **commit + push** (`git push origin go_compile_test` — 1 commit pending
   after step 1's commit).
3. **compilemanager** (1021L) → uses HRW Result (full 1:1: `Result{Snapshot,
   ProjectDir, CompileDir, OutputDir, Stats, Timings}`).
4. **apps/server** (route table from app.js, load agent TCP 3048 + HTTP 3049,
   /status /health_check /smoke_test_force /metrics, error middleware).
5. **cmd/clsi main + Makefile + live smoke** — texlive image now present:
   `docker run --rm texlive/texlive:latest-full ...` smoke path via
   dockerrunner over unix socket with `hello-world` first, then texlive for the
   production smoke. Node parity matrix.
6. **clsi_typst.go** (NEW service — sibling of clsi, Typst-only;
   feature-equivalent, reuses clsi.go + otc where appropriate, own HANDOFF.md;
   starts once compilemanager is green).
7. **DEFERRED** (upstream scope, does NOT block porting): when otc safe_pathname
   oracle goes green after upstream fix — re-run `go test ./go/libraries/otc`
   from repo root and confirm.

**Standing / carry-over (do NOT skip silently):**
- Dual-annotate v8date.go (C++ comments + Go lines) — citation header (git@github.com:
   v8/v8.git + BSD licence); NO V8 citation in README.md under clsi folder.
- Update this handoff after every module.

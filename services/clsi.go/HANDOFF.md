# CLSI Node → Go 1:1 Port — HANDOFF

> **Read this first.** This file is the single source of truth for a session
> taking over the port. Update it at the end of every session.

Repo: `/home/davrot/compile/OlliTeX_comp` (NEVER type this path in bash by hand —
use the stable anchor `/tmp/clsi_proj` → same dir; the dir spelling is
`OlliTeX_comp`, confirmed via `od -c`. The "OlliTexas_comp" variant does NOT exist.
NFS mount (`10.10.1.1:/volume1/data_1`) can transiently drop files; the stable
symlink is the workaround.)
Node source (reference only, do NOT delete): `services/clsi/` (module `@overleaf/clsi`, ESM)
Go target: `services/clsi.go/` (standalone module `clsi`, Go 1.27)

## 0. CURRENT STATE (authoritative — updated 2026-09-19)

```
STATUS: CLSI 25 packages COMPLETE + green (all ≥ 90% coverage). minimatch v10.2.6 PORT COMPLETE
        (all oracle files green, 49,828 rows, 0 mismatches; go build/vet clean; CLSI module builds).
Build: go build ./... = OK | go vet: clean | go test: 25/25 ok (clsi module) + minimatch ALL GREEN
Cover: ALL 25 CLSI PACKAGES >= 90% (gate met — see table §1). minimatch pkg 78.7% (gate is
        ≥90% per CLSI package; minimatch is oracle-accepted — oracle is its acceptance gate,
        see ACCEPTANCE RESULT below).

minimatch progress (go/minimatch/, part of root module ollitex):
  DONE + oracle-verified (all green, 2026-09-19):
    - escape.go (escape+unescape merged; esc_oracle.tsv 157 rows x 8 combos ALL GREEN)
    - minimal.go (Options)
    - braceexpand.go (95/95 oracle GREEN; beHasBracePattern nested-brace bug FIXED this session)
    - classparse.go (parseClass port)
    - reengine.go (closed-dialect RE engine; testoracle.tsv 35,400/35,400 GREEN)
    - segast.go (AST parsePortion + toRegExpSource: flatten adopts/usurps, fillNegs, guards,
      extglob arms incl. `|` alternation + negated `!(...)` with end guard `(?:$|/)` on ROOT
      filledNegs; byte-approx `\p{X}` documented divergence)
    - engine.go (Minimatch class: New/parseNegate/make/braceExpand/dedup/slashSplit/preprocess
      level 0/1/2 + Match/MatchList/HasMagic; matchOne + tri-value matchGlobstar +
      matchGlobStarBodySections (file-truncated view + badDot walk check); GLOBSTAR sentinel;
      partial/flipNegate/matchBase)
    - mm_smoke_test.go (hand-verified against node v24 minimatch@10.2.6)
    - oracle_test.go (drives testdata/ TSVs incl. oraclePath() fallback to /tmp/mmprobe/)
  ACCEPTANCE RESULT (09-19): match_oracle.tsv 7,854 | segM.tsv 27,360 | full26.tsv 3,400 |
        diff26.tsv 1,334 | segPortion.tsv 9,924 tested — TOTAL 49,828 rows, 0 mismatches.
        TSVs refreshed in testdata/ (older Sep-18 copies of diff26/full26 were STALE — the
        generator bug `{dot:d}` where d was the result array, so every row was dot:true;
        regenerated via regen26.js with `{dot:dot===1,platform:"posix"}`).
  Remaining (optional / CLSI-side):
    - minimatch README.md written (in go/minimatch/); coverage 78.7% (optional exported-API
      parity test could push to ≥90% but is NOT required — oracle is the acceptance gate).
    - WIRE into resourcewriter (`new Minimatch(pattern, {dot:true})` + PRECIOUS_FILE_PATTERN) —
      part of §1 row 6 (resourcewriter, open). NOT CLSI-side yet.
  NOT ported (documented divergences; see go/minimatch/README.md + §5 session-8 notes):
    - fastTest shortcuts (starRE/starDotExtRE/qmarksRE/*Test) — compile-time only, no oracle,
      same dot choice as RE path (Go builds one source)
    - makeRe() — deprecated upstream, unused by CLSI/.match (no oracle)
    - Windows (isWindows/UNC/drive) — CLSI is POSIX only (Go rejects Platform!=posix)
    - `\p{X}` byte-approximation (CLSI paths ASCII); u-flag / nocase byte-fold (case-sensitive CLSI)

CLSI remaining (minimatch no longer gates):
  0. minimatch (CLSI-only) — PORT COMPLETE (see ACCEPTANCE RESULT above).
     Wire into resourcewriter = content-cache-write item below.
  1.5 **ot package (clsi/ot/)** — IN PROGRESS. The Go port of @overleaf/overleaf-
     editor-core (apply-path only: transform/compose/invert/rebase intentionally NOT
     ported). Files DONE: doc.go, errors.go (full hierarchy incl. NotFoundError/
     TooLongError/PathnameError/NonUniquePathnameError/BadPathnameError/FileNotFound/
     EditMissingFile/NotEditable/SnapshotError), util.go (utf16Len/containsNonBmpChars/
     byteLengthOf/blobHashFromString/maxStringLength/EmptyHash/jsonString), range.go (ALL
     20+ methods incl. InsertAt/SplitAt/Intersect/ToRaw; Pos *int = JS undefined),
     safepathname.go (**COMPLETE — ORACLE 80,782/80,782 rows PASS**, testdata/spfuzz2.json
     7.9MB from real lib), tracking.go, scanop.go (ScanOp base + RetainOp/InsertOp/RemoveOp:
     equals/canMergeWith/mergeWith/applyToLength/toJSON/fromJSON; LengthApplyCtx),
     filemetadata.go, blob.go (Blob + BlobStore{GetString/GetObject/FetchString}).
     REMAINING ~13 files to port (apply-path only): comment.go, comment_list.go,
     v2_doc_versions.go, tracked_change.go (file_data/tracked_change.js),
     tracked_change_list.go, filedata.go (+ string_file_data), text_operation.go,
     edit_operation.go, operation.go (add_file/... index), file.go, file_map.go,
     change.go (apply-path), snapshot.go. Then oracle_test.go (35 cases from
     /tmp/otto/ot_dataset.json). Design notes below §5b.
  1. clsicachehandler (CLSICacheHandler.js 528L) — no Node unit tests;
     port with injectable seams, mirror app.js + CompileManager call sites.
     OCM is DONE — CLSICacheHandler consumes its Promise API (path/generateBuildId/
     saveOutputFiles/expireOutputFiles) + CLSICompileQueue + Metrics.
  2. commandrunner (CommandRunner.js 19L)
  3. compile core: compilemanager (CompileManager.js 1021L) + compilecontroller (CompileController.js 491L)
  4. content cache write: resourcewriter (398L, WIRE minimatch here), historyresourcewriter (869L) (urlcache/urlfetcher done)
  5. conversion: tikzmanager (129L), png2pdf (96L), + any remaining conversion modules
  6. server layer: app (route table from app.js), load agent (TCP 3048 + HTTP 3049),
     /status /health_check /smoke_test_force /metrics, error middleware
  7. DockerRunner.mjs (634L) — native Docker Engine client (hand-rolled HTTP over unix.sock, §4/1)
  8. cmd/clsi main + Makefile + live smoke (Docker texlive)
  9. Node parity matrix (acceptance fixtures = goldens)
```

**Takeover procedure (verify before trusting old notes):**
1. `cd /tmp/clsi_proj/services/clsi.go && go build ./... && go vet ./... && go test ./... -count=1`
2. Skim §1 task table; top open row = your next task. **§7 = locked OCM design (skip re-research).**
3. `todo list` (Pi todos, filter tags `clsi`/`go-port`) — live claim of in-flight task.
4. Do NOT re-derive anything locked in §4/§5/§7.
5. `/tmp/clsi_proj` symlink may not exist in a new container: recreate it:
   `ln -sfn /home/davrot/compile/OlliTeX_comp /tmp/clsi_proj`

## 1. Task List

| # | Todo id | Task | Status |
|---|---------|------|--------|
| 0 | TODO-9e50c129 | Node baseline (unit + acceptance) as parity reference | BLOCKED (PnP `.pnp.cjs` missing in this repo copy — see §2.1). Test EXISTENCE + expectations in `services/clsi/test/` is the practical spec; Docker baseline works in CI. |
| — | (mm) | minimatch v10.2.6 hand-rolled port → `go/minimatch/` (part of module `ollitex`) | **COMPLETE 2026-09-19**: escape/minimal/braceexpand/classparse/reengine (35,400 RE oracle)/segast/engine. Acceptance: **49,828 oracle rows, 0 mismatches** (match_oracle 7,854 + segM 27,360 + full26 3,400 + diff26 1,334 + segPortion 9,924). `go build`/`go vet` clean; CLSI module builds. Coverage 78.7% (oracle-accepted; gate is per-CLSI-package). Remaining wire-in = row 6 (resourcewriter sets `{dot:true}` + PRECIOUS_FILE_PATTERN). |
| 5 | TODO-d9502cb0 | Output side: outputcachemanager (688L), clsicache (528L), contentcachemanager (447L) | IN PROGRESS — DONE: outputfilefinder/optimiser/archivemanager/draftmodemanager + outputcontroller 100% + contentcachemanager 96.7% + **outputcachemanager 92.1%** (2026-09-17). REMAINING: clsicachehandler (528L) |
| 6 | TODO-fac1ee46 | Content cache: resourcewriter (398L), historyresourcewriter (869L), urlcache (227L), urlfetcher (111L) | IN PROGRESS — urlcache 96.6% + urlfetcher 93.8% DONE + **resourcewriter 93.2%** (2026-09-19, minimatch WIRE-IN COMPLETE — precious glob); historyresourcewriter remains |
| 3 | TODO-6a350144 | Compile core: commandrunner (19L), compilemanager (1021L), compilecontroller (491L) | IN PROGRESS — **commandrunner 100.0% DONE** (2026-09-19); compilemanager + compilecontroller remain |
| 7 | TODO-843bd163 | Conversion path: tikzmanager (129L), png2pdf (96L), conversionmanager (936L), conversioncontroller (357L), latexmetrics (408L) | open (commandrunner DONE is its prerequisite) |
| 4 | TODO-02cae9d7 | DockerRunner (634L) → `dockerclient` (native Docker Engine API over unix.sock) + fake for tests | open |
| 8 | TODO-d7feecff | Server layer: app (route table from app.js), load agent (TCP 3048 + HTTP 3049), /status /health_check /smoke_test_force /metrics, error middleware | open |
| 9 | TODO-f464d516 | cmd/clsi main + Makefile + live smoke + Node parity matrix | open |
| 10 | (session) | Maintain this HANDOFF.md + todos | ongoing |
| 11 | TODO-90972736 | Port every remaining app module (see top-row of this table each time) | IN PROGRESS |

Coverage log (strict per-package ≥ 90%):

| package | cover |
|---|---|
| config | 90.1 |
| errors | 98.1 |
| requestparser | 90.0 |
| logger | 100.0 |
| metrics | 100.0 |
| lockmanager | 100.0 |
| dockerlockmanager | 90.2 |
| draftmodemanager | 90.0 |
| statsmanager | 100.0 |
| synctexparser | 93.9 |
| outputfilefinder | 94.6 |
| safereader | 95.7 |
| xrefparser | 90.7 |
| conversionoutputcleaner | 100.0 |
| outputfileoptimiser | 96.7 |
| outputfilearchivemanager | 90.2 |
| outputcontroller | 100.0 |
| lastprojectaccess | 100.0 |
| resourcestatemanager | 93.4 |
| urlfetcher | 93.8 |
| urlcache | 96.6 |
| contentcachemanager | 96.7 |
| outputcachemanager | 92.1 |
| commandrunner | 100.0 |
| resourcewriter | 93.2 |

**Coverage gate met for all 25 packages (≥ 90%).**

Coverage gate cmd: `cd services/clsi.go && go test ./... -coverprofile=/tmp/clsi.cov && go tool cover -func`.
**IMPORTANT**: `go clean -testcache -cache` between profile generations (profiles go stale).

## 2. Environment (verified 2026-09-15)

- Go **1.27.1** at `/usr/local/go`. Node 24.13.0, Docker 29.5.3, gcc 15.2.0.
- Docker backbone for Go: hand-rolled HTTP-on-unix client (decision §4/1) — zero-dep policy.
- Node 24 / yarn 4 present, **PnP install broken in this copy** (see §2.1).

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
  app.js                → clsi.go/app.go  (express routes, timeouts, error middleware, load agent)
  config/settings.defaults.cjs → config/config.go (DONE, 90.1%)

Controllers (HTTP edge)
  CompileController.js (491L)   → compilecontroller.go (compile/stop/sync/wordcount/status routes)
  OutputController.js  (31L)    → outputcontroller.go (DONE 100%)

Compile core
  CompileManager.js (1021L)     → compilemanager.go (core orchestration)
  HistoryResourceWriter.js (869L) → historyresourcewriter.go
  ResourceWriter.js (398L)      → resourcewriter.go
  DockerRunner.mjs (634L)       → dockerclient (hand-rolled, §4/1)
  OutputCacheManager.js (688L)  → outputcachemanager.go (DESIGN LOCKED §7)
  OutputFileFinder.js           → outputfilefinder.go (DONE 94.6%)
  OutputFileOptimiser.js        → outputfileoptimiser.go (DONE 96.7%)
  OutputFileArchiveManager.js   → outputfilearchivemanager.go (DONE 90.2%)

Content / URL cache
  CLSICacheHandler.js (528L)    → clsicachehandler.go (OPEN — next after OCM)
  ContentCacheManager.js (447L) → contentcachemanager.go (DONE 96.7%)
  UrlCache.js (227L) / UrlFetcher.js (111L) → urlcache.go / urlfetcher.go (DONE)

Locking / persistence
  LockManager.js                → lockmanager.go (DONE 100%)
  LastProjectAccess.js          → lastprojectaccess.go (DONE 100%)
  ContentCacheMetrics/Worker    → (in CCM; worker pool is V8 worker_threads — ported as
                                   updateSameEventLoop main path in Go, see CCM §7 notes)
```

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
    Coverage gate: every package ≥ 90%.

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

**glob library verdict (probes done 2026-07-01 — DO NOT RE-RESEARCH)**: `bmatcuk/doublestar/v4` (local clone ~/compile/doublestar, probe sandbox /tmp/mmprobe2, oracle = minimatch-10.2.6 dot:true full26.tsv 3400 rows): **169/3400 = 4.97% mismatch** — no runtime dot flag (dot semantics compile-time hardcoded and wrong vs minimatch: `*` accepts `.b`), `a/**` accepts parent dir `a` (minimatch rejects), `**/x` misses `x`, trailing-slash + char-class rules diverge. go-zglob/gobwas/yargevad similarly lack `{a,b}`/extglob/runtime dot toggles. **=> HAND-ROLLED minimatch port is the only viable path.** Acceptance oracle = probeT2 predicted table (dot:true) + /tmp/mmprobe diff26.tsv differential rows (regenerated for 10.2.6 when node source available).

1. **minimatch engine (CLSI-only)** → `go/minimatch/` — **COMPLETE 2026-09-19** (all oracle
   files green; 49,828 rows, 0 mismatches; go build/vet clean; CLSI module builds — see §5
   session-8 log + §0). Public API: `New(pattern string, o *Options) (*Minimatch, error)`,
   `(*m).Match(f) bool`, `(*m).MatchList([]string) []string`, module `Match(f, pat string, o
   *Options) (bool, error)`, `HasMagic() bool`; `escape/`+`unescape/`+`expand` exported for
   reuse. Wire into resourcewriter (see table §1, content-cache row) with `{dot:true}` + PRECIOUS_FILE_PATTERN.
   **DESIGN PINNED (v10.2.6 dist + probes; full contract below):**
   - `match(f[,partial])` contract: make() = parseNegate → braceExpand (dedup via Map) → slashSplit (`.split('/')`) → preprocess (lvl 0: adjascentGlobstarOptimize; 1: levelOneOptimize; ≥2: firstPhasePreProcess+secondPhasePreProcess) → per-portion parse() → matchOne | matchGlobstar. **`makeRe()` is deprecated + unused by `.match` — NOT ported.**
   - parseNegate: each leading `!` toggles negate; no-match → return `negate || partial`... exact: no-hit → `this.negate` (if partial: `this.pattern.startsWith('/') ? this.negate : this.negate || partial`); hit → `this.flipNegate || !this.negate`.
   - Portion parse(): `'**'`→GLOBSTAR sentinel; `''`→`''`; fastTest shapes (starRE/starDotExtRE/qmarksRE/starDotStarRE/dotStarRE — regexes + pure-logic tests in source; verified ≡ compiled guards, listed in §5 progress).
   - Guards (byte pre-checks per chain portion): noDot = substring[0]≠'.'; noTrav = substring ∉ {'.','..'}; justDots = root portion exactly '.'/'..' (bypass). needNoTrav from escaped-src charAt chain (src[0] ∈ '[.'; `\.` prefix → src[2] ∈ '[.'; `[^` = escaped '^' chain). needNoDot = !dot && src[0] ∈ '[.'.
   - Extglob compile = flatten 10 passes (canAdopt/canAdoptWithSpace/canUsurp; exact adoptionMap/adoptionWithSpaceMap/usurpMap in dist ast.js L58-100) + `#fillNegs` (clone trailing siblings into each `!(...)` arm) + token scan (coalesce `*` → `[^/]*?`, noEmpty = all-stars+no-ext+atRoot → `[^/]+?` else guard, `?` → `[^/]`, char classes, text literal) + guard prefix + `^(?:...)$`.
   - `!(X)`: remainder R must not fully-match ANY arm (arm = its body + `#fillNegs` clones); then tail optional star `[^/]*?` guarded noDot iff `!` node isStart && !dot && !allowDot. Empty `!()` = starNoEmpty.
   - `#matchGlobstar`: bad-dot = portion ∈ {'.','..'} or (dot off && starts '.'); body sections `after = fileLength - remaining non-gs parts`; recursion via `maxGlobstarRecursion` (default 200).
   - FLATTEN maps (from dist): adoptionMap `!→[ @... see dist], ?→[?, @], @→[@], *→[*,+,@,+...], +→[+, @]` EXACT: `!:[' @...'` — transcribe from ast.js (do NOT guess); same for adoptionWithSpaceMap + usurpMap.
   - File-side levelTwoFileOptimize only when optimizationLevel ≥ 2 (default 1 → file slash-split as-is).
   **Files status (09-19):** segast.go (AST parsePortion + toRegExpSource: flatten adoptions/
   usurps, fillNegs, guards, extglob arms incl. `|` alternation + negated `!(...)` end-guard
   `(?:$|/)` on ROOT filledNegs) + engine.go (Minimatch: exported New/Match/MatchList/HasMagic,
   module-level Match; internal make, preprocess 0/1/2, tri-value matchGlobstar, GLOBSTAR
   sentinel) + oracle_test.go (drives testdata/) + mm_smoke_test.go + README.md, ALL DONE + oracle-verified (see §5 session-8 entry + §0).
   **fasttest.go intentionally NOT ported** (compile-time only, no oracle; same dot choice as
   RE path — Go builds one source). **makeRe NOT ported** (deprecated upstream, unused by
   CLSI .match, no oracle).
   ACCEPTANCE GATE = oracle files (testoracle 35,400 | segM 27,360 | match_oracle 7,854 |
   segPortion 9,924 | full26 3,400 | diff26 1,334 | escape 157 | expand 95... total 49,828
   MATCH-path rows, 0 mismatches), NOT a coverage % — the HANDOFF >=90% gate is
   per-CLSI-package. Divergences (README): byte matching not UTF-16 (CLSI paths ASCII);
   windows paths inert (Go rejects Platform!=posix); RE2 can't do lookaheads -> guard byte
   tests (proven equivalent on oracle).
2. Port `CLSICacheHandler.js` (528L) → `clsicachehandler` package. No Node unit tests;
   mirror app.js usage + CompileManager call sites. Uses OCM's Promise API (path,
   generateBuildId, saveOutputFiles, expireOutputFiles) + CLSICompileQueue + Metrics.
3. Port `CompileManager.js` (1021L) — core orchestration (wire OCM + CCM + lockmanager +
   metrics + DockerRunner).
4. Port `CompileController.js` (491L) + `DockerRunner.mjs` (634L).
5. Port resourcewriter (398L) + historyresourcewriter (869L).
6. Port tikzmanager (129L) + png2pdf (96L) + commandrunner (19L).
7. Port server: `app.js` → Go HTTP server + `cmd/clsi/main.go`.
8. Makefile, smoke test, full integration test + Node parity matrix.
9. Update this HANDOFF.md after each module.

### minimatch v10 — PURE-LOGIC TOKEN MODEL (byte-confirmed from compiled regexes, 2026)

**Key findings (probed from Node makeRe().source):**
- Arms are FULLY tokenized (same tokenization as plain strings): `@(a*)` → `(?:a[^/]*?)`; `@(a?c)` → `a[^/]c`; `@([xy])` → `(?!\.)?[xy]` (guard from char `[`)
- Guard (noDot `(?!\.)` / noTrav `(?!(?:^|/)\.\.?(?:$|/))`) attached per ARM chain when arm isStart
  (arms built at parentIndex 0 → effectively ALL arms) and computed from arm's first COMPILED char
  (`[`, or escaped `\.` chains) — for text arms the guard is moot (text can only match itself)
- bodyDotAllowed two-pass: repeated (`+`/`*`) at chain start with dot=false → first rep uses guarded arm,
  subsequent reps `(?:(unguarded-arm))*?` (allowDot=true arm variant)
- `!` extglob: `((?!(?:ARM-src-with-(?:$|/) tail)) + [^/]*?)` — pure-go: assert NO arm fully-matches
  remainder at pos (arms got tail chain copyIn'd from after `!` in parent — i.e. arm = arm-tokens + tail
  appended), then star (lazy any) consumes, chain continues
- empty arms (`|`) = zero-token arm = matches exactly empty
- `@(*|!(x))` style: `!` inside arm = its own arm with tail (whole arm chain appended)
- flatten: adopt (map), adopt-with-space (adds blank arm), usurp (child takes over sole-part parent via
  usurpMap: ?/{*,+:*}, +/{?,*:*}, @/{!..+ all same type}, !/{!:@}); 10 passes
- text chains: NO guards needed (text can't match "."/".." or lead-dot... except justDots `.`/`..` pattern
  matches itself)
- star token: `[^/]*?` any prefix; starNoEmpty = portion is star-only... starNoEmpty only when NOEMPTY
  (isStart&&isEnd&&no-ext-parts) → star that must match nonempty
- qmark = exactly one non-slash char
- class = one byte predicate (JS code-unit vs Go byte: byte-approx, documented divergence)

**minimatch engine v2 (byte-confirmed from Node compiled source, FINAL):**
- ARM tokenization: full glob (escaped `\.` → literal; `\.` chain escapes; `*`; `[...]`; `?`)
- Guard (isStart-only): noDot `(?!\.)`; noTrav `(?!(?:^|/)\.\.?(?:$|/))`; precedence trav>dot
  * text chain: guard moot (can't match dot-leading unless self `.`, `..`)
  * star/q/class leading: guard active; escaped-leading: `\\.` chains → aps check (char at src[0/2/4])
- bodyDotAllowed fold for `+`/`*` at chain start, dot=false: first arm guarded + `(uncrapped-arm)*?`
- `!`-ext: `((?!(?:ARMsrc))` + startNoDot? + star) → pure-logic: reject if ANY arm FULL-matches s[pos:]
  (ARM = arms + tail chain copyIn'd from PARENT chain after `!` node)
- emptyARM = matches empty only
- flatten: 10 passes adopt/adoptWithSpace/usurp over extglob parent types
- toRegExpSource empty-ext: whole-portion `+()`/`*()`/`?()`/`@()` = literal text fallback
- emptyExt `!` = starNoEmpty (matches ≥1 char)
- star: lazy `[^/]*?` = match any length (lazy irrelevant for existence); starNoEmpty = min 1 char
- qmark = exactly 1 non-slash char
- class: posix classes + ranges (JS code-unit ≈ Go byte divergence documented)
- NO-REGEX design: pure-logic backtracking matcher for portions (RE2 can't express lookaheads)
- GLOBSTAR = sentinel for `**` portions; matchOne walks portions×file parts
- matchOne: lockstep `#matchGlobstar` (head/body/tail, body sections, tri-value return)
- optimizationLevel: 0=adjacentGlobstar, 1=levelOneOptimize, ≥2=firstPhase+secondPhase+levelTwoFileOptimize
- dot option: default false (Go zero value), CLSI sets dot:true
- nocase: case-insensitive compare for all tokens (not oracle-tested but supported)
- matchBase, flipNegate, negate (leading `!`), empty pattern, comment pattern
- partial mode, nonull, windows (inert on POSIX)

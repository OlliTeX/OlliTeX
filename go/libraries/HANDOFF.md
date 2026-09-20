# /go/libraries — Node→Go 1:1 library ports (HANDOFF — keep current every package)

**Directive (owner, 2026-09-20):** 1:1 drop-in **GOLANG** conversion of everything under
`libraries/*` into `/go/libraries/*`, stepping up tests: strict test suite per package
with detailed coverage. Keep this doc + the `.pi/todos/` ledger (LIB-01..16,
TODO ID `b5890b9a` = index) updated at every boundary so a fresh session takes over
seamlessly.

**Repo:** `/home/davrot/project-history/OlliTeX-ph`, branch `golang-ph`, Go module
`ollitex` (root `go.mod`, go 1.27). New packages land under `go/libraries/<name>/`.
Build gate per package: `go build ./... && go vet ./... && gofmt -l go/libraries && go test ./libraries/... -cover` (run from
repo root). Commit per package **green** (build+vet+gofmt+tests).

## STATUS (first line updates on any change)

`STATUS: 2026-09-20 — LIB-01 oerror DONE (96.8%); LIB-02 SKIP (Node plumbing); LIB-03 streamutils DONE (97.8%→98.6%); LIB-04 validtools DONE (99.6%→98.9% with ArrayVal/UnionVal composites); LIB-05 settings DONE (98.6%); LIB-06 rangestracker DONE (97.0%, 13 oracle ports + 29 supplement/coverage tests); LIB-08 accesstoken WIP on disk (1 failing golden test — finish before commit); LIB-09 notifprefs on disk, tests green, HANDOFF not yet updated. Next: LIB-07 fetchutils.`


| # | Package | Go pkg | Status | Coverage |
|---|---------|--------|--------|----------|
| 01 | o-error | oerror | ✅ | 96.8% |
| 02 | promise-utils | — | SKIP (Node plumbing — see §Decisions) |
| 03 | stream-utils | streamutils | ✅ | 97.8% |
| 04 | validation-tools | validtools | ✅ | 99.6% |
| 05 | settings | settings | ✅ | 98.6% |
| 06 | ranges-tracker | rangestracker | ✅ | 97.0% |
| 07 | fetch-utils | fetchutils | ○ | — |
| 08 | access-token-encryptor | accesstoken | ○ | — |
| 09 | notification-preferences | notifprefs | ○ | — |
| 10 | redis-wrapper | rediswrapper | ○ | — |
| 11 | mongoose-wrapper + mongo-utils | mongoutils (+wrapper) | ○ | — |
| 12 | object-persistor | persistors | ○ | — |
| 13 | logger | ologger | ○ | — |
| 14 | metrics | ometrics | ○ | — |
| 15 | overleaf-editor-core | otc (multi-phase) | ○ | — |
| 16 | STRICT GATE + final handoff | — | ○ | target ≥85 overall |
| — | eslint-plugin | — | SKIP (ESLint-rules API, no Go analogue; decision documented §Decisions) |
| — | cypress-pnp-reporter | — | SKIP (Mocha reporter shim; same) |

○ open · ◐ in progress · ✅ done

**Order rationale:** dependency-first (o-error is imported by 8 of the 16 Node libs;
validation-tools next since logger/validtools/fetch all build on it + o-error).
Big package otc last; coordinates with ph-go `internal/opmodel` (see LIB-15 note).

## ENVIRONMENT (verified 2026-09-20)

- Go: **go1.27.0 linux/amd66** (repo go.mod `go 1.27`; module `ollitex`).
  `go build ./... && go vet ./... && gofmt -l . && go test ./... -cover` green at repo
  HEAD before start (267 .go files; go/README.md invariants apply: same env vars, same
  wire shape). ph-go sibling (`services/project-history.go`) has its own module
  `project-history` (go.work? NO — two independent modules; cross-imports via relative
  paths are NOT possible; if a lib needs a ph-go internal, re-implement — document).
- Docker (SHSTK workaround mandatory on this kernel):
  `docker run -d --name ph-mongo -p 27017:27017 -e GLIBC_TUNABLES=glibc.cpu.hwcaps=-SHSTK mongo:8.3`
  `docker run -d --name ph-redis -p 6379:6379 redis:7.4.0`
  (for LIB-10/11 live tests; containers may need `docker start` if restarted).
- Node v24.13.0, yarn PnP — the Node test suites (`yarn test` per lib, mocha/vitest)
  are the **oracle**; generate goldens from them before/while porting where cheap.
- Repo go.mod currently only has `go-mail`, `mongo-driver`, `x/crypto`. **Adding Go
  deps requires owner-visible rationale** (document in §Decisions).

## DECISIONS LOG (append-only)

- (LIB-03) **streamutils shape**: Node's bidirectional Transform/PassThrough
  stream ports to `io.Reader` wrappers over the source stream (Go request-body
  pipelines are read-side): `LimitedReader`, `TimeoutReader`, `LoggerReader`,
  `MeteredReader`. `WritableBuffer`/`ReadableString` are direct ports.
  Error surfaces: `*SizeExceededError` (msg pinned byte-for-byte: "exceeded
  stream size limit of %d: %d") + `*AbortError` ("stream timed out").
  `Meter`/`Logger` are NARROW local interfaces — the ometrics (LIB-14) and
  ologger (LIB-13) ports implement/buy them at their boundaries (no
  premature coupling).
  **IncrementalResponse** — Node `AbortController`→Go `context`, Node `res`
  →narrow `ProgressResponse{Write,Finish}`; the `#humanReadableTimeout` pin
  (Node operates in *ms* — its `timeout` is the setTimeout value, NOT a
  pre-converted Duration) is exercised byte-for-byte ("2min3s450ms" style).
  All error-message / log-shape pins match the Node test suite verbatim.
- (LIB-01) **oerror API surface**: Node exports ONLY `OError` (default export). Go exports: `OError{Type,Message,Info,Cause,Tags}`, `New(msg, info, cause...)` / `Of(msg)`, `(.WithInfo/.WithCause/.WithName/.Tag)`, `Tag(err,msg,info) error`, `GetFullInfo(error) map[string]any`, `GetFullStack(error) string`, `MaxTags`, `DroppedTags`, `TaggedError`. `Unwrap()` follows an error-typed cause for stdlib interop.
- (LIB-01) **Pinned divergences (HANDOFF §Divergences)**:
  (a) Node V8 stacks are not representable in Go: `Tag`'s `Error.captureStackTrace` half is dropped; `TaggedError` value (message+info) is the preserved surface and `GetFullStack` renders exactly the Node-pinned line structure (name:message / TaggedError: msg / caused by: chain, 4 spaces per cause depth).
  (b) Node `tag` accepts *any object* (even `{message:'Foo'}`); Go `Tag` on any error wraps in `*OError` (renders `"OError: <Error() text>"`) so repeated tags accumulate (Node singleton behaviour preserved via pointer identity).
  (c) Node monkey-patches plain `Error` instances (`.info=`, `.cause=`); Go equivalent is `New(...).WithInfo/.WithCause`.
  (d) `IsDropped` is structural (message + nil info), not reference `=== DROPPED_TAGS_ERROR` (Go struct comparison with maps is impossible).
  (e) Cap semantics ground-traced with Node itself: `[t1, DROP, t4, t5]` at maxTags=3, `[L0, DROP, L9, L10]` at 11-level recursion — pinned in `TestTagCap*`.
- (LIB-02) **promise-utils SKIP (Node plumbing, no Go drop-in)**: every export is
  JS promise/callback duality machinery (promisify/promisifyAll/promisifyClass,
  callbackify*/expressify/expressifyErrorHandler — Node `node:util` re-exports and
  Express middleware wrappers) plus `pLimit`. Go has no promise objects; its native
  contract IS the already-async function + error surface, and the repo's Go services
  already express each pattern idiomatically at use sites (e.g. `go/pbhttp` owns the
  middleware-shape bits). Porting this would create a dead wrapper layer that the Go
  services would NOT call — worse than a skip. Documented, NOT ported; every consumer
  that needs the *limit-map* idiom uses a Go waitgroup + buffered channel (document at
  consumer, e.g. when CLSI Go port lands).
- (init) Skip eslint-plugin + cypress-pnp-reporter from the Go tree: they implement
  the ESLint/Mocha *tool* APIs (rule definitions, Mocha reporter lifecycle); no runtime
  value in Go, and their "port" would be a dead abstraction. Both documented in HANDOFF
  at gate (LIB-16) as deliberate skips.
- (init) Package naming: Go-idiomatic short names (oerror, promiseutils, …), **not**
  npm names — npm names are the *spec*; Go name is the API. Directory = package path
  `go/libraries/<name>/`.
- (LIB-04) **validtools = hand-rolled zod 4.1.11 1:1 port (NO ZOG)**. Wire parity: Go `Val` interface + `Issue{Message,Code,Path}` mirror zod's `issue.message/issue.code/issue.path` byte-for-byte; `ZodError{Message:"Invalid input"}` → `zve.formatZodErrorLite` → `FriendlyMessage` produce the identical `Validation error: ...` text (see `wire_test.go` vs the 85 goldens). Leaf `refine` messages are oracle-pinned (absent → "received undefined", null → "received null", wrong-type → primitive typing issue). **Multi-file split** per user request: doc/issue/wire/types/datetime/validators/object/validate/errors/handler (+3 test files). NO new Go deps (zod regexes hand-written, datetime `time.Parse` + fallback). Coverage 99.6% (46 uncovered-blocks from the baseline all closed by `coverage_test.go` absent/null/wrong-type matrices + `time.Time`/`Local` arms).
- (LIB-05) **settings = 1:1 port of `merge.js` + `Settings.js`** into `go/libraries/settings`. `merge` is a faithful recursive fold: falsy/absent default slot → fresh `{}` (oracle: 0/""/false/null/absent all yield `{"o":{"a":1}}`); truthy-PRIMITIVE slot → override silently dropped (Node `<prim>[k]=v` no-op, slot survives); arrays/primitives replace wholesale; identity contract = returns the SAME defaults map (mutates in place — Go `reflect.Pointer` pins aliasing). JS `null` override → `ErrNullOverride` + `NullOverrideError{Path}` (Node raw TypeError, no path — path is a Go convenience). `Load` mirrors Settings.js: env mismatch throw, `cwd/{cjs,js}` → entry-dir resolution, NODE_ENV lowercasing, `mergeWith` (R11) dispatch checked on the DEFAULTS module, flying-blind `{}`. Seams: `EnvLookup`, `Filesystem`, `ReadModule`, `*LoadOpts`. Coverage 98.6% (foldLayer `default` arm + jsFalsy int8..uint64 arms + isNonNegativeInt closed by `coverage_test.go`).
- (LIB-06) **rangestracker = 1:1 port of `libraries/ranges-tracker`** (index.cjs 808LoC + schemas.js) into `go/libraries/rangestracker`. Node duck-typed ops → single `Op` struct with presence-pointer fields; applyOp dispatch order I→D→C matches Node (`op.i`/`op.d`/`op.c`). **Live-reference parity**: Node hands out the SAME objects (dirty state, `.changes` elements); Go stores `[]*Change`/`[]*CommentItem` internally and dirty entries are `*RangeRef` live pointers — a captured dirty ref sees later ops (TestDirtyRefsAreLive pins it); value slices are box/copy at the public boundary (New, GetChanges). **sameUser** replicates JS `===` on (absent|null|value): absent≡absent→true, absent-vs-null→false, DeepEqual otherwise. `pickTimestamp` strict `<` (tie→new), ts accepts *time.Time/RFC3339 string, unparseable→new (Node Invalid Date). AddComment: `op.t || newId()` — EMPTY STRING t is falsy → fresh id (pinned). **schemas.js** ports onto validtools as `InsertOpSchema…RangesSchema` + `ParseRanges` (Node safeParse). **validtools gained 2 framework composites** (`compose.go`): `ArrayVal{Item Val}` (z.array) and `UnionVal` (a.or(b) — first clean arm wins; all-fail → single invalid_union issue with per-arm groups, same wire as the DT union). Documented here because L15 otc / docstore schemas will want them too.
- (init) "1:1" granularity rule: exported function/class names map 1:1 (Go export
  convention, e.g. `OError` → `OError`); behaviour (messages, status codes, error
  shapes, edge cases) is byte-for-byte where deterministic. Non-Go-isms (dual stacks,
  `undefined` vs `nil`) get the narrowest faithful equivalent + test pin.

## KNOWN DIVERGENCES / RISKS (fill as they appear)

- (LIB-06) rangestracker: (a) Node `comment.op.c === undefined` throws TypeError on `.length`; Go treats nil `C` as `""` (unreachable for schema-valid data — pinned); (b) `UnionVal` short-circuits at the first clean arm (Node zod evaluates all arms) — failure verdict and clean-arm value are identical, only the failing-issue detail group would differ; (c) `GetChanges`/`New` copy values at the boundary (Node hands out object refs) — live mutation parity is kept INTERNAL (elements + dirty state) so the observable flush-time reads match; (d) `sortChangesStable` uses `sort.SliceStable` (V8 `Array.sort` is stable since ES2019) — equal-key order preserved; (e) seed generation uses math/rand/v2 vs Node Math.random — both non-deterministic, byte-SHAPE 1:1 (18 hex seed, 6-hex increment, zero-padded).

- (LIB-01) oerror: divergences (a)-(e) above — all exercised by tests; no silent behaviour change.
- (LIB-05) settings: (a) `merge`'s V8-key-ordered partial-apply on a `null`-override throw is NOT pinned (Go canonical-numeric-then-lex order is documented, pre-throw partial state is V8-implementation-dependent); (b) `NullOverrideError` carries a `Path` the raw Node TypeError lacks (Go convenience, root-cause text stays verbatim); (c) `Load` seams (`EnvLookup`/`Filesystem`/`ReadModule`) replace Node's PnP `require` + `fs.existsSync` + `process.env` — the injected values MUST be the consuming service's real modules/config files, not re-implementations; (d) `moduleWithMerge` is checked on the DEFAULTS module value (a Go value implementing `MergeWith(any) any`), not a Go-reflect probe like Node's `typeof === 'function'`.
- (init) oerror tests found a real semantic bug: the Node suite checks
  `getFullInfo(error.cause)`, and Go `Tag` tags the *OError in place — the
  wrapper and cause are the same object after `Tag(bar, ...)`. Documented in
  the test.

## NEXT-SESSION PLAYBOOK

1. `TODO` list (tool `todo`): index `b5890b9a`; claim the next ○ LIB task and set
   status `in-progress`.
2. Run the green gate first: `go build ./... && go vet ./... && gofmt -l . && go test ./... -cover` (repo root).
3. Pick the topmost ◐/next ○ from the STATUS table; read the Node source (lib
   `index.js` + `test/` mocha files) before writing Go — the Node test file is the
   acceptance spec.
4. Port → golden-parity tests → coverage floor per todo → update table + commit
   (message: `go/libraries LNN <name>: <what> (<cover>%, <oracle> tests)`).
5. VRAM discipline: one lib per context window; no bulk multi-lib edits; keep this
   doc's STATUS line and table current *before* you stop.
6. **`git add -A` GUARD**: this repo currently has *untracked* in-progress
   work from the separate B3 opmodel track (`services/project-history.go/
   internal/opmodel/*`). NEVER `git add -A` / `git commit -a` — stage only
   `go/libraries/<pkg>` + `go/libraries/HANDOFF.md`. The B3 track commits
   its own files itself. (It was accidentally swept once during L03 and
   reset out; those files remain on disk, untracked.)
   doc's STATUS line and table current *before* you stop.

- (LIB-03) streamutils divergence pin: Node `IncrementalResponse#end` double-call (`res.end()` then `res.destroy()` fallback) collapses to a single `Finish()` here (no Go analogue for the destroy fallback; failing finish on a finished response is observable-equal). `PassThrough` (Node read-side + write-side) ports to the read-side wrapper (documented above).

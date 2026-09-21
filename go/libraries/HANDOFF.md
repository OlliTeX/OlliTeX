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

`STATUS: 2026-09-21 — LIB-01 oerror DONE (96.8%); LIB-02 SKIP (Node plumbing); LIB-03 streamutils DONE (98.6%); LIB-04 validtools DONE (98.9% with ArrayVal/UnionVal composites); LIB-05 settings DONE (98.6%); LIB-06 rangestracker DONE (97.0%); LIB-07 fetchutils DONE (94.9%); LIB-08 accesstoken DONE (95.4% — the failing golden test used a wrong TEST password: Node suite uses 38 '4's, not 46); LIB-09 notifprefs DONE tests-green (69.2%); LIB-10 rediswrapper DONE (95.3% — Driver seam, oracle fully mock-based so NO live redis needed for unit tests); LIB-11 mongoutils+mongowrapper DONE (mongo-utils 88.0% ObjectId/testutils/batchedUpdate + mongoose-wrapper 92.5% façade; batchedUpdate live tests against real mongod via a `Mongo` seam); LIB-12 object-persistor DONE (88.6% — S3Client/GCSStorage seams, fully mock-based, no live cloud; gunzip fix zlib→gzip). Committed: L01,L03,L04,L05,L06,L07,L08,L09,L10,L11,L12 (per-package, explicit staging). Next: LIB-13 ologger → LIB-14 ometrics → LIB-15 otc → LIB-16 strict gate (≥85 overall); notifprefs 69.2% below target must be raised or waived before the gate.`


| # | Package | Go pkg | Status | Coverage |
|---|---------|--------|--------|----------|
| 01 | o-error | oerror | ✅ | 96.8% |
| 02 | promise-utils | — | SKIP (Node plumbing — see §Decisions) |
| 03 | stream-utils | streamutils | ✅ | 98.6% |
| 04 | validation-tools | validtools | ✅ | 98.9% |
| 05 | settings | settings | ✅ | 98.6% |
| 06 | ranges-tracker | rangestracker | ✅ | 97.0% |
| 07 | fetch-utils | fetchutils | ✅ | 94.9% |
| 08 | access-token-encryptor | accesstoken | ✅ | 95.4% |
| 09 | notification-preferences | notifprefs | ✅ | 69.2% |
| 10 | redis-wrapper | rediswrapper | ✅ | 95.3% |
| 11 | mongoose-wrapper + mongo-utils | mongoutils + mongowrapper | ✅ | 88.0% / 92.5% |
| 12 | object-persistor | persistors | ✅ | 88.6% |
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
- (LIB-07) **fetchutils = 1:1 port of `libraries/fetch-utils/index.ts`** (~450 LoC) into `go/libraries/fetchutils` (fetchutils.go core/errors/headers/watchdog/bodyPipe, agent.go Custom{Http,Https}Agent, variants.go fetchJson/fetchStream/fetchNothing/fetchRedirect/fetchString, +4 test files). NO new Go deps (stdlib net/http). Oracle: the 56-case `FetchUtils.test.js` ported (fetchJson/Headers/fetchStream/fetchNothing/RequestFailedError/fetchString/fetchRedirect) + the 10 agent cases (success, non-routable→ConnectTimeoutError→FetchError, retries-after-delay with ≥1500ms pin, no-stray-reconnect, https CA, untrusted→DEPTH_ZERO_SELF_SIGNED_CERT, https non-routable, bad CA PEM, non-positive connectTimeout) + over-timeout warn (120s seam made a var) + edge branches (FetchError surface, non-string header stringify, JSON-body marshal error, expired-cert→ERR_CERT_EXPIRED). Coverage 94.9%.
  **Go-ism seams (narrowest faithful equivalent + test pin):**
  (a) Node `AbortController`/`signal` → Go `context.Context` (cancel == abort); `errors.Is(..., context.Canceled)` NOT used because oerror deliberately does not Unwrap non-OError causes into the stack (pinned in oerror tests) — abort tests assert the transport `context canceled` text instead.
  (b) Node agent (http.Agent with connect-timeout retry) → `*http.Client` + custom `DialContext`/`DialTLSContext` (DialTLSContext is REQUIRED: a DialContext that already did TLS would double-handshake and the server would answer plaintext → "server gave HTTP response to HTTPS client"). Retry loop = up to 3 attempts, per-attempt ctx timeout, retry interval, last-error-wins, timeout→ConnectTimeoutError.
  (c) Node request-body `stream.destroyed` → a `bodyPipe` (io.Pipe + source-Closer tracking): destroy-on-error, destroy-before-transfer (source.Close unblocks the copy and aborts the request via CloseWithError — a clean EOF would only end the body, NOT abort), destroy-if-not-consumed after settle (raw early responder needed: the net/http test server drains the body before flushing early responses, which blocks on unbounded bodies — a Node/Go server-stack diverge, see §Divergences).
  (d) `setLogger({warn})` 120s over-timeout warn → `SetLogger(WarnFunc)` + `RequestWarnTimeout` var (test-overridable) firing with `{url,method,overTimeoutMs,stack}` + the exact Node message "Fetch request did not complete within 120 seconds".
  (e) node-fetch `FetchError` wrapping: connect timeout → FetchError"request to <url> failed, reason: connect timeout"; TLS trust failure → FetchError{Code: DEPTH_ZERO_SELF_SIGNED_CERT} (untrusted/self-signed) / ERR_CERT_EXPIRED. Mapped BEFORE the oerror.Tag wrap (Tag breaks the Unwrap chain for non-OError causes) and inside performRequest so the raw transport error chain is still visible. `errors.As` targets must be VALUE types for x509.CertificateInvalidError / x509.UnknownAuthorityError (they are value-receiver types held as values in the chain — pointer targets do NOT match).
  (f) `parseHeaders` drops null-valued headers (Node stringifies `undefined` → dropped); non-string values stringified (bool/int/float). RequestFailedError = `request failed` message, body-in-info only for 400/409/413/422.
  (g) fetchRedirect returns the RAW Location header (Node parity); the test server issues absolute Locations (Node's server resolves relative→absolute; Go's does not).
  **TEST-ONLY infra pins (not shipped behaviour):** /hang handler blocks on `r.Context().Done()` (not `select{}`) with a 30s self-timeout so Server.Close can't hang; cleanup uses CloseClientConnections + non-blocking Close for live /hang conns (client abandoned mid-request); waitForRequest replaced by a path-polling waitUntilRequest (stale buffered tokens from earlier subtests caused a cancel-before-send race); rawEarlyResponder (raw TCP) mirrors express /json/ignore-request (responds without reading the body).
- (LIB-08) **accesstoken = 1:1 port of `libraries/access-token-encryptor`** (AccessTokenEncryptor.js) into `go/libraries/accesstoken` (accesstoken.go). NO new Go deps (stdlib crypto/{aes,cipher,hkdf,sha512,rand}). Oracle: the Node 27-case suite — constructor validation (all 6 error messages verbatim, including the UTF-16-code-unit "too short" check), encrypt round-trip (label:saltHex16:ctB64:ivHex16 format regex, fresh salt+iv so two encryptions differ), decrypt 2023 golden token → {"hello":"world"}, unknown-2015/2016/2019 labels (v1/v2 schemes not implementable → "unknown access-token-encryptor label <x>"), corrupt-ciphertext/invalid-base64 → "error decrypting token", invalid label/key. Coverage 95.4%.
  **Port internals:** scheme dispatch by first `:`-separated label (Node `split(':',1)`); v3 keyFn = HKDF-SHA512(password_utf8, salt, info="", L=32) — RFC 5869, byte-identical across stacks (Go `hkdf.Key(sha512.New, secret, salt, "", 32)` = Node `crypto.hkdf('sha512', pw, salt, '', 32)`); AES-256-CTR (Node `createCipheriv/Decipheriv`) — CTR has no auth tag so "bad ciphertext" is caught at the JSON.parse step (Node `error decrypting token`), reproduced by the json.Unmarshal branch. Password UTF-16 length via `utf16.Encode` (JS `.length` ≠ byte count). `labelsInOrder` + Config.PasswordOrder restore Node's Object.keys insertion order (Go map iteration is random) so multi-scheme validation/unknown-scheme reports are deterministic and Node-ordered; unlisted config keys are appended sorted.
  **THE 2026-09-21 RED-FIX:** `TestDecryptGolden2023` was FAILING not due to a crypto bug — the Go TEST password was `strings.Repeat("4", 46)` but the Node oracle password is `'4'×38` (counted in `AccessTokenEncryptorTests.js` `this.settings.cipherPasswords`). HKDF is deterministic, so a 46-char password derives a different key and the golden ciphertext decrypts to garbage → `error decrypting token`. Corrected all 6 test passwords to 38. **Lesson: when a 1:1 crypto port's golden test fails, verify the TEST's inputs (salt/iv/password/label) against the Node source byte-for-byte BEFORE suspecting the algorithm — the algorithm was already byte-parity.** (Cross-checked by running the live Node `test:unit` 27/27 passing, and by a direct node one-liner HKDF+CTR decrypt of the exact golden token with the 38-char password.)
- (LIB-10) **rediswrapper = 1:1 port of `libraries/redis-wrapper`** (index.js 239LoC + RedisLocker.js + RedisWebLocker.js + Errors.js) into `go/libraries/rediswrapper` (driver.go / client.go / health.go / locker.go / weblocker.go / errors.go + 6 test files). NO new Go deps (stdlib only + oerror). **Oracle: the Node unit suite is fully driver-mocked (`sandboxed-module` stubs ioredis) — 14/14 passing — so the Go port pins the SAME contract against an injected `Driver` seam; no live redis needed for unit tests** (the Node `test/scripts/standalone.js|cluster.js` are manual live scripts, not suite members). Coverage 95.3% (52 test fns).
- (LIB-11) **mongoutils = 1:1 port of `libraries/mongo-utils`** (ObjectId.js + testutils.js + batchedUpdate.js) into `go/libraries/mongoutils` (objectid.go / testutils.go / options.go / batchedupdate.go + test files). **mongowrapper = façade over `libraries/mongoose-wrapper`** (a 6-line mongoose re-export → stable import point over `go.mongodb.org/mongo-driver`). NO new Go deps (mongo-driver + stdlib already in go.mod). **No seam needed: the functions take `*mongo.Collection` directly (Go pointers, not JS objects), and the suite runs against a real local mongod** (batchedUpdate live walk in both directions, cleanup/drop live, single-flight, options refresh). Deterministic ObjectID edges are pinned with fixed hex timestamps so the walk is reproducible. batchedUpdate pins Node's ascending/descending ObjectID time-window walk, single-flight, options refresh (env `BATCH_*`), and the `ID_EDGE_FUTURE` sentinel. Coverage: mongoutils 88.0%, mongowrapper 92.5%.
- (LIB-12) **persistors = 1:1 port of `libraries/object-persistor`** into `go/libraries/persistors`. Files: `errors.go` (the 7 typed error classes + `asPersistorError` instanceof checks), `helper.go` (PersistorHelper: `wrapError` classification, `Observer` metrics/hash, md5 helpers, `verifyMd5`), `projectkey.go` (ProjectKey format/pad), `ssec.go` (SSE-C key options), `abstract.go` (`Persistor` contract + `BasePersistor` NotImplementedError defaults), `s3_seam.go` / `gcs_seam.go` (narrow SDK seams + `S3Error`/`GCSStatusError` error-shape adapters), `s3persistor.go` / `gcpersistor.go` / `fpersistor.go` (the three backends), `migration.go` (MigrationPersistor primary→fallback + copy-on-miss), `perproject.go` (PerProjectEncryptedS3Persistor + DEK/KEK lifecycle + HKDF), `factory.go` (PersistorFactory + `ObjectPersistor`). **No new Go deps** — `go.mongodb.org`/AWS/GCS SDKs are NOT imported: the Node unit oracle stubs the SDK client (`S3ClientMock.js`, sinon `@google-cloud/storage`), so the Go suite injects FAKE clients implementing the `S3Client`/`GCSStorage` seams (same mock-based approach as LIB-10's `Driver` seam). A real runtime adapter (e.g. the repo's existing S3 gateway `ollitex/go/s3x`) plugs into the seam later without touching the persisted logic. `Logger`/`Metrics` are `Null*`-defaulted NARROW interfaces (`LoggerAPI`/`MetricsAPI`) — they become the real ologger/LIB-13 + ometrics/LIB-14 once those land (no premature coupling). **Go-ism fix pinned:** `autoGunzip` must use `compress/gzip` (Node `zlib.createGunzip`), NOT `compress/zlib` (zlib wrapper format) — an initial wrong choice was caught by the gunzip oracle test. Per-KEK/DEK SSE-C paths, `ifNoneMatch:'*'`→generation-0 / AlreadyWritten mapping, `deletedBucketSuffix`, `unlockBeforeDelete`, `deleteConcurrency`, copy-on-miss and the S3SSEC `no kek matched`/`kek is not 32 bytes long` strings are all oracle-pinned. Coverage: **88.6%** (above the 85% gate).
  - Known divergence: Node `ObjectId` is the 12-byte BSON id from `mongodb`; Go uses `primitive.ObjectID` (same 12 bytes, hex + 32-bit-second timestamp). `ObjectIdFromTimestamp` parity is exact at second resolution (sub-second ms truncate, as in Node's driver).
  **Seam (the Node value IS the driver adapter):** `Driver` interface (SetEx NX/EX, Exists, Eval, Exec→RAW ioredis rows, FlushAll, Host) + `Configure(opts, clusterConfig)` constructor seam (Node `new Redis(opts)` / `new Redis.Cluster(nodes, opts)`); `CreateClient(opts, ctor)` mirrors index.js 1:1 — key_schema stripped, `retry_max_delay ??= 5000`, sentinel → exact `@overleaf/redis-wrapper: redis-sentinel is no longer supported`, cluster dispatch (client.ClusterConfig = nodes, `cluster` deleted from driver opts) vs single-instance.
  **healthCheck** 1:1: token `host=…:pid=…:random=<8hex>:time=<ms>:count=N` (process-unique), key/value `_redis-wrapper:healthCheck{Key,Value}:{token}`, SET EX 60 (non-NX) → multi GET+DEL; error classes `RedisHealthCheckTimedOut('timeout')` (2000ms `HealthCheckTimeout` var seam), `RedisHealthCheckWriteError('write errored'|'write failed')`, `RedisHealthCheckVerifyError('read/delete errored'|'read failed'|'delete failed')` with the Node stage/context fields (writeAck / roundTrippedHealthCheckValue / deleteAck / uniqueToken) — all OError-typed via Errors.js hierarchy (name/message/cause pinned).
  **RedisLocker** 1:1: TTL validation `30 <= ttl < 1000` → exact `redis lock TTL must be at least 30s and below 1000s` (Node "wrong type" case unrepresentable in the int-typed Go API — documented); signed tokens `locked:host…:count…`; SET NX EX; overlong-SET auto-release (`MaxRedisRequestLength` var seam, Node MAX_REDIS_REQUEST_LENGTH=5000ms); poll 50ms→1s backoff cap, `MaxLockWaitTime` 10s → `WrapTimeoutError(Error('Timeout'), id)` (injectable like Node's wrapTimeoutError); CheckLock exists→free?; Extend/Release via the two Lua scripts byte-for-byte (extend arg `lockValue, lockTTLSeconds`; unlock result!==1 → `tried to release timed out lock` / `tried to extend a lock we no longer hold`); metrics `prefix-not-blocking|-blocking|-extend-*` + logger seams (Null* defaults).
  **RedisWebLocker** 1:1: per-key FIFO lock queue = Node `async.queue(concurrency 1)` (LOCK_QUEUES is module-scope → package `lockQueues`; drain removes the entry; `LockQueuesSize` = `_lockQueuesSize`); task continuation = `queue.next()` invoked BEFORE the caller resolves (pin-tested by real store contention: two concurrent RunWithLock on one key serialize, neither interleaves); `_getLockByPolling` FIXED-interval (no backoff, unlike RedisLocker), attempts gauge `lock.<ns>.get.success.tries` / `lock.<ns>.get.failed`; **Node TTL quirk pinned 1:1: `EX REDIS_LOCK_EXPIRY*1000` (EX is seconds → the redis-side TTL is ×1000; the client-side watchdog fires after REDIS_LOCK_EXPIRY seconds: `lock.<ns>.exceeded_lock_timeout` inc + debug log)**; RunWithLock = Node promisified contract: timer `lock.<ns>` → acquire → runner(values, runnerErr) → release (Node `error1 || error2`: runner error wins; release error only surfaces on runner success) → `slow execution during lock` debug over `slow_execution_threshold`; `slowExecutionError` sentinel in the log info.
  **cleanupTestRedis/ensureTestRedis** 1:1: `Refusing to clear Redis instance '<host>' in environment '<env>'` (host must be `redis_test` AND NODE_ENV=test) → FLUSHALL.
  **TEST-ONLY infra:** fakeDriver = scripted responses (repeat-last-element) OR real in-memory store semantics (SET NX exclusivity, unlock/extend CAS over the held value) — this is what makes the serialization/contention pins genuine; echoSetDriver for the healthCheck round-trip; recording metrics/logger doubles.
- (owner 2026-09-21) **Commit bookkeeping**: L01 oerror / L03 streamutils / L04 validtools / L05 settings were DONE+green but LEFT UNCOMMITTED (owner flag: "some go/libraries are not committed yet"), and L09 notifprefs was green but uncommitted. Committed per-package (green) in this session: oerror 96.8%, streamutils 98.6%, validtools 98.9%, settings 98.6%, fetchutils 94.9%, notifprefs 69.2%. rangestracker (L06) was already committed (7bc883cd6e). accesstoken (L08) was RED (1 failing golden test — wrong test password, see LIB-08 entry) and intentionally NOT committed; now green (95.4%) and committed `cf2ecca5d0`. rediswrapper (L10) green (95.3%) and committed (hash in commit log). All commits stage `go/libraries/<pkg>` + HANDOFF.md explicitly (no `git add -A`).

## KNOWN DIVERGENCES / RISKS (fill as they appear)

- (LIB-07) fetchutils: (a) Go net/http **server** drains a request body before flushing an early response (express responds immediately) — so the "destroy request body if not consumed" oracle test uses a raw-TCP early responder, not the stdlib test server; client-side destroy semantics are 1:1 (bodyPipe); (b) abort (Node AbortController) → context cancellation; Go does not Unwrap non-OError causes into the oerror stack (pinned in oerror), so cancel is asserted via the transport error text; (c) x509 codes: Go `UnknownAuthorityError` (untrusted/self-signed) → Node `DEPTH_ZERO_SELF_SIGNED_CERT`; `CertificateInvalidError(Expired)` → `ERR_CERT_EXPIRED` — other reasons render the Go message (no Node code exists in scope); (d) Go 1.27 has no `SelfSigned` InvalidReason — self-signed-untrusted surfaces as `UnknownAuthorityError` (handled as above); (e) `errors.As` needs VALUE-type targets for x509 value-receiver error types (pointer targets miss them).
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

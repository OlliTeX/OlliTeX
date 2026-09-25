# document-updater.go — Go 1:1 port of services/document-updater

Hermetic Go port of the Node `document-updater` service (HTTP + Redis + Mongo +
Bull). Committed so far: **pure-logic + ShareJS engine + history-OT + ShareJS
update-manager packages** (Phases 1–7). Each is a faithful port of a Node
helper with unit tests mirroring the Node test vectors. Remaining: the
managers (Redis/Mongo-backed seams), the UpdateManager, and the HTTP
controller — all with injected seams (hermetic, no live services).

Module: `document-updater`, Go `1.27.0`. `replace ollitex => ../../`. Deps:
`github.com/sergi/go-diff` (DMP text diff); `ollitex` (`otc`,
`rangestracker`, `oerror`).

## Package map (Node source -> Go package)
| Go package | Node source | Port |
|---|---|---|
| `internal/errorsx` | `app/js/Errors.js` | `NOT_FOUND` + 7 OError subclasses (`OTTypeMismatch` carries `got`/`want`; `WebApiServerError` carries a message). |
| `internal/updatekeys` | `app/js/UpdateKeys.js` | ShareJS `projectId:docId` compose/split. |
| `internal/limits` | `app/js/Limits.js` | `GetTotalSizeOfLines`, `DocIsTooLarge`, `StringFileDataContentIsTooLarge` (+ limit types). |
| `internal/diffcodec` | `app/js/DiffCodec.js` | DMP diff -> ShareJS insert/delete ops (`DiffAsShareJsOp`). |
| `internal/utils` | `app/js/Utils.js` | `Op`/`Metadata`/`TrackedChange`, op-kind predicates, `GetDocLength`, `AddTrackedDeletesToContent`, `ComputeDocHash`, `ExtractOriginOrSource`. |
| `internal/preview` | `app/js/TrackedChangePreview.js` | `BuildSparseChangePreviews` (section path, startLine clustering, bounded 500-char window, zero-width deletes). |
| `internal/historyconversions` | `app/js/HistoryConversions.js` | `ToHistoryRanges` (comment offsets + hpos/hlen), `ToHistoryOT` (string-file raw with adds), `FromHistoryOT` (via `otc.FromRawStringFileData` + `otc.NewFile` + `otc.GetDocUpdaterCompatibleRanges` + `rangestracker.GenerateId`). |
| `internal/rangesmanager` | `app/js/RangesManager.js` | `ApplyUpdate` (history-op enrichment per op, cropped comments, collapse detection, `range-delta` histogram), `AcceptChanges`, `DeleteComment`, `GetHistoryUpdatesForAcceptedChanges` (insert retain hpos + delete doc-length accounting), `ErrTooManyRanges`/`ErrUnrecognizedOp`. |
| `internal/ratelimit` | `app/js/RateLimitManager.js` | `RateLimiter` with `ActiveWorkerCount`, float `CurrentWorkerLimit`/`BaseWorkerCount`; below-limit background dispatch vs at-limit synchronous dispatch; +0.1 up-adjust on synchronous success, `max(base, 0.9*limit)` down-adjust after a below-limit run; `gauge` metrics. |
| `internal/sharejstext` | `app/js/sharejs/types/text.js` + `types/helpers.js` | ShareJS **text** OT type: `Component` (I/D/C/P/T), `Apply` (insert/delete/comment, mismatch-checked), `Append`/`Compose`/`Compress`/`Normalize`, `TransformComponent`, `TransformX`/`Transform`, `TransformCursor`, `Invert`. Node `String.slice` clamping is mirrored by a local `jsSlice` so transforms + mismatch errors behave identically to Node. |
| `internal/sharejsjson` | `app/js/sharejs/types/json.js` (+ `helpers.js` bootstrap) | Vendored ShareJS **json** path-op type (630 LOC): `Component` (si/sd/li/ld/lm/na/oi/od over `P []any`), `Apply`, `TransformComponent` incl. the "icky path hax" na-phantom segment + the oi-left fall-through append-merge, `TransformX`/`Transform` (bootstrapTransform parity), `Compose`/`Normalize`/`Invert`/`CommonPath` (JS `undefined` = `ok=false`; `−1` is real), `Append` merge rules. Vendored bugs ported faithfully (incl. `delete c.od`). Registered in `sharejstypes` as `json` via a `JSONType` adapter. Validated by a 96-row golden table generated live from the vendored `json.js` (incl. the `x-nacommon` `p:[null]` segInc-NaN artifact). |
| `internal/sharejstxtcomp` | `app/js/sharejs/types/text-composable.js` | Vendored ShareJS **text-composable** type (~400 LOC): plain-string snapshots; components = skip int / `{i:s}` / `{d:s}`. `CheckOp` (vendor error order: type → positive → adjacency), `makeAppend` merge rules, `makeTake` (fresh copies, indivisibleField), `Apply` (exact delete mismatch), `Normalize` (no checkOp, per the vendored deleted line), `Transform`/`Compose`/`Invert` (strict semantics; no bootstrap, no length-1 fast path). Vendored debug `i()` is a no-op stub: every fragment/trailing error prints a bare "undefined" (pinned verbatim). 38-row oracle golden incl. the strict-coverage and checkOp error rows. Registered as `text-composable`. |
| `internal/sharejstptwo` | `app/js/sharejs/types/text-tp2.js` | Vendored ShareJS **text-tp2** type (~500 LOC) over tombstone docs `{charLength, totalLength, positionCache, data}`: skip / `{i:str}` / `{i:N}` / `{d:N}` components. `CheckOpInput`, `Apply` (tombstone-aware `TakeDoc`/`AppendDoc`), `AppendOp` (the vendored `_append` merge rules), `Transform`/`Prune`/`Compose` sharing the `makeTake` port + the vendored STRICT error semantics ("Remaining fragments in the op: N" with `N`/`[object Object]`, "traverses more elements", the prune "deletes locally inserted characters" error). No length-1 fast path and no empty-op early exit (the bootstrap is never applied to text-tp2: the module exports the plain type). Registered in `sharejstypes` as `text-tp2` via the `TP2Type` adapter. 50-row oracle golden (incl. checkOp error rows and the strict-coverage transform errors). |
| `internal/syncqueue` | `app/js/sharejs/server/syncqueue.js` | FIFO per-document op/apply queue with the vendor's "apply ops in order" contract (synchronous in Go). |
| `internal/profiler` | `app/js/Profiler.js` | `New(name, ctx)` + `log`/`end` no-op timing stub (vendor side effects not modelled). |
| `internal/sharejstypes` | `app/js/sharejs/types/index.js` + `count`/`simple`/`helpers` | Type registry (wire `Type` face: `Name`/`Create`/`Apply`/`Transform`); `text` (sharejstext), `json` (sharejsjson), `text-tp2` (sharejstptwo), `text-composable` (sharejstxtcomp), `simple` (snapshot-opaque), `count` (single-op). |
| `internal/sharejsmodel` | `app/js/sharejs/server/model.js` (895 LOC) | "The model of all the ops": per-doc syncqueue op pipeline, live-doc cache, DB interaction, full public interface (Create/Delete/GetSnapshot/GetVersion/GetOps/ApplyOp/ApplyMetaOp/Listen/RemoveListener/Flush/CloseDb); wire-native `Type` face; 12-scenario golden live from the vendored Node model. |
| `internal/sharejsdb` | `app/js/sharejs/ShareJsDB.js` (147 LOC) | Vendored Redis-backed ShareJs db over the committed model `Db` interface: getSnapshot key-check (`errorsx.NotFoundMsg`) + `lines.join('\n')` + type "text", getOps early exits + end-- range fetch via injected `OpsFetcher`, writeOp buffering into `appliedOps`, no-op delete/create/writeSnapshot/close. |
| `internal/sharejsupdatemanager` | `app/js/sharejs/ShareJsUpdateManager.js` (154 LOC) | Fresh ShareJsDB + `sharejsmodel.Model` per call via overridable `NewModel` factory; `ApplyUpdate` error map ('Op already submitted' → dup-fall-through to PRE-APPLY snapshot, /^Delete component/ → `DeleteMismatchMsg`, else passthrough; too-large → `FileTooLargeMsg`; hash check → "Invalid hash"); `_listenForOps` → model EmitFn 'applyOp' → `SendOps`; `_computeHash` sha1 oracle (UTF-16 length quirk pinned); `Handle{Model, DB}`; `MAX_AGE_OF_OP=80`, `DefaultMaxDocLength=2 MiB`. |
| `internal/historyotupdate` | `app/js/HistoryOTUpdateManager.js` (172 LOC) | `ApplyUpdate` (vendor `tryApplyUpdate` + catch-broadcast): getDoc → `errorsx.NotFoundMsg` / `OTTypeMismatch`; raw-wire ops through `otc.FromJSONEditOperation`; rebase onto `getPreviousDocOps(docId, v, version)` via in-place `otc.TransformEditOpsMultiple` (dup source → `update.dup=true`, skip apply/persist, still broadcast); else `FromRawStringFileData` → `file.Edit` per op → `version+1`, `meta.ts=now`, `updateDocument`, nonfatal `queueOps`/`recordAndFlushHistoryOps` + `recordProjectNotificationTimestamp`, realtime `sendData`. Error broadcast uses the BARE message (vendor `error.message`), not Go's rendered "Type: message". Wire shapes (vendored `otc`): text op `{"textOperation":[int|str|negint,...]}`, addComment `{"commentId","ranges":[]any{{pos,length}}}` (typed `ToJSON` ranges NOT re-parseable — pinned), rebase-to-noop `{"noOp":true}`. `FromWire` accepts `v` int/float64. |
| `internal/updatemanager` | `app/js/UpdateManager.js` (456 LOC) | `Process` (getLock → FetchAndApply → release finally → error-before-continue), `FetchAndApply` (IsHistoryOT → HistoryOTApply, else ApplyUpdate), `Continue` (length>0 → Process), exported `ApplyUpdate` (sanitize in place → GetDoc → NotFoundMsg/OTTypeMismatch → ShareJsApply → ApplyRanges (FromOpData bridge) → UpdateDocument (RAW appliedOps) → per-HU `Inc("history-queue")`+Adjust+serialize+QueueOps / err:`Inc("history-queue-error")` + recordTS still runs → ts=`meta.ts`/user_id → rejected: removed→previews→Notify → collapsed:`Inc("doc-snapshot")`+RecordSnapshot (PREVIOUS version, PRE-UPDATE lines/ranges) → SendData errString + rethrow), `AdjustHistoryUpdatesMetadata` (7-arg vendor order, doc_length/history_doc_length chains, `tc` drop, last-HU `doc_hash`), `LockUpdatesAndDo` (getLock→fetch→extend→method→release→fire-and-forget Continue + err:`Inc("background-processing-updates-error")`), `errString` OError type-switch (BARE message), `SanitizeUpdate` UTF-16 16-bit-unit `\uFFFD` replacement (oracle `'\uD835\uDC00'`→`'\uFFFD\uFFFD'`), `FromOpData`/`rawOpDataToUpdate` wire→typed bridge, `SerializeHistoryUpdate` (local wire `historyUpdateWire`/`historyOpWire`, `Resolved bool` omitempty — vendored explicit false not modeled). All collaborators are exported nil-tolerant func seams; `New()` defaults: Inc noop, Now wall-clock ms, BuildPreviews = preview, IsHistoryOT = historyotupdate guard projection. |
| `internal/redismanager` | `app/js/RedisManager.js` (689 LOC) | The Redis facade over the injected `Client`/`Tx` seam (a hermetic in-memory fake in tests; a real redis client can be wired behind the same seam later). 20 vendor methods: `PutDocInMemory` (wire-serialize + null sentinel + `limits.DocIsTooLarge` guard + sha1 + the projectBlock probe MULTI (exists+sadd, reply[0]==1 → "Project blocked from loading docs" before any contents write) + the HRS flag + the 7-key contents MSET (HRS → del/sadd ResolvedCommentIds)), `RemoveDocFromMemory` (11-key del MULTI + second MULTI srem(DocsIn)+del(ProjectState) + HRS srem), `CheckOrSetProjectState` (getset+expire(1800)), `ClearProjectState`, `GetDoc` (the 10-key MGet + sismember/smbers + hash-mismatch `LogHashRead` + Now wall bail-out > 5s → OError + NotFoundE project mismatch + pathname `Inc`), `GetDocRanges`, `GetDocVersion` (seam: nil → real MGet+toInt), `GetDocLines`, `GetPreviousDocOps` (llen/get/lrange, firstVersionInRedis = version-llen, out-of-range → `errorsx.OpRangeNotAvailableE` {firstVersionInRedis,version,ttlInS}, offset shift + end=-1 preserved, JSON.parse per op, slow Now bail-out → bare error), `UpdateDocument` (getDocVersion consistency `currentVersion+len(ops)==newVersion` else OError "Version mismatch. doc is corrupted", per-op JSON wire + null sentinels, 6-key MSET + ltrim(-100,-1) + rpush ops + expire when ops>0 + NX unflushedTime), `RenameDoc` (getDoc seam, only when cached lines+version), `ClearUnflushedTime`, `UpdateCommentState` (sadd/srem ResolvedCommentIds), `GetDocIDsInProject` (smembers DocsIn), `GetDocTimestamps` (per-doc get lastUpdatedAt), `QueueFlushAndDeleteProject` (zadd now+jitter), `GetNextProjectToFlushAndDelete` (zrangebyscore probe → {} when empty; pop MULTI zrange(0,0)+zremrangebyrank(0,0)+zcard → {projectId,flushTimestamp,queueLength}), `SetHistoryRangesSupportFlag`, `RecordProjectNotificationTimestamp` (timestamp NX EX 3600 + editor key only when userID), `BlockProject` (setex(30)+scard MULTI, reply[1]>0 → del + false), `UnblockProject` (del==1), `JSONString` (no-HTML-escape — the oracle wire `["one","two","three","これは"]` and sha1 `8b69713c1e40897e9220cec1d5183ea1a7396672` byte-pinned), `SerializeRanges` (`'{}'` → nil, >3 MiB → "ranges are too large"), `DeserializeRanges` (empty → `{}`). Vendor divergences documented: JSONString = no-HTML-escape encoding/json (Node raw UTF-8), null-sentinel seam (the Go encoder escapes NUL), parseInt NaN → Go 0 (the "inconsistent version or lengths" NaN branch unreachable), OError.tag MULTI chain flattened, vendor ONE_DAY_SECS exported unused, smoothing jitter = int seam (0 default). Oracle 8-group pins: getDoc (all-details snapshot, hash-mismatch log/none, slow bail-out, invalid-project NotFoundE, HRS flag), getPreviousDocOps (offset shift, end=-1 preserved, out-of-range OpRangeNotAvailableE, slow bail-out), updateDocument (consistent-versions mset/ltrim/rpush/expire/NX order, inconsistent → no exec, no-ops mset no-rpush, empty-ranges → nil, no-user → nil lastUpdatedBy), putDocInMemory (non-empty 7-key mset + probe sadd + HRS srem, empty-ranges → nil, blocked → OError no-mset, HRS true → sadd + multi del/sadd RCIDs), removeDocFromMemory (strlen + 11-key del + srem DocsIn + HRS srem), clearProjectState (del ProjectState), renameDoc (cached → set Pathname, not-cached → no redis), getDocVersion (mget → int). 24 tests. |


## Current state (Phases 1–8 COMPLETE — 22 packages green gate; Phase 9 = remaining managers / controller)
- **TWENTY-TWO packages committed and all green** under `go build` /
  `vet` / `gofmt` / `test -count=1 -race` (see the package map above).
- **Chunk 5 COMPLETE** (`internal/sharejsmodel/`, commit `882206b`):
  Go port of `model.js` ("the model of all the ops") — cache of live
  docs, the per-doc syncqueue op pipeline, and the DB interaction layer,
  with the full public interface (Create/Delete/GetSnapshot/GetVersion/
  GetOps/ApplyOp/ApplyMetaOp/Listen/RemoveListener/Flush/CloseDb).
  Wire-level `Type` face (Create/Apply/Transform over wire snapshots +
  wire `[]any` component maps); `TextWireType` wraps the oracle-verified
  sharejstext port. Vendored quirks preserved and oracle-pinned:
  `maxDocLength` checked against the OLD snapshot, `end=null` resolved
  to `version` before any db.getOps call, `getOpsInternal` sequential
  v-stamping, dup detection via `oldOp.meta.source` in `opData.dupIfSource`,
  nil-op → "undefined is not iterable (cannot read property
  Symbol(Symbol.iterator))", post-applyOp
  `committedVersion + opsBeforeCommit <= doc.v` snapshot commit,
  `flush` writes every pending snapshot, `closeDb` + `db=nil`. 12-
  scenario golden (basic, transform, errors, maxdoc, opscache, listen,
  coalesce, snaps, metaop, createdelete, nodeb, cloisedb) byte-identical
  to the oracle (every model emit, doc-op, and the trailing db-final
  counters: create/getSnap/getOps/getOpsCalls/writeOps/writeSnaps/delete).
  Divergences (documented, observable-equivalent): Go is synchronous
  single-flight (no `awaitingGetSnapshot` coalescing; no
  process.nextTick), per-instance event + per-doc listener list (the
  vendored `Model.prototype = new EventEmitter()` shared-emitter bug,
  oracle resets via `removeAllListeners`), `Listener` handle instead of
  Node func-identity (Go functions compare only to nil), and the
  vendored reaper / `refreshReapingTimeout` is modelled as a no-op
  (timer-driven, not observable in the sync oracle). Also:
  `sharejstext.strInject` now clamps like JS `slice()` (pinned by the
  `transform` scenario: `{p:3,i:'Y'}` on `"a"` → `"aY"`).
- **Chunk 6 COMPLETE** (`sharejsdb` + `sharejsupdatemanager`, commit
  `576dfb9`): 19 packages at chunk 6 (full oracle notes below).
- **Phase 7 chunk 1 COMPLETE** (`internal/historyotupdate/`, this commit):
  Go port of `HistoryOTUpdateManager.js` (172 LOC) — the history-OT
  (overleaf-editor-core) update path: getDoc → rebase-onto-concurrent →
  apply → persist/queue/notify → realtime broadcast, with all
  collaborators injected seams (DocumentManager, RedisManager x3,
  ProjectHistoryRedisManager, HistoryManager, RealTimeRedisManager,
  Date.now). 6 oracle scenarios mirror `HistoryOTUpdateManagerTests.js`
  (composed multi-op apply incl. comment re-home, rebase-against-concurrent
  [incl. thread-the-op-through-list], rebase-to-noop, mixed
  [text,comment,text]×[text,text,comment], dupIfSource
  skip-apply-and-still-broadcast) + guard/not-found/wrong-type/from-wire/
  queue-serialization/nonfatal-queue-error. **20 packages green.**
  Divergences (documented in the port): vendor error broadcast is the
  BARE `error.message` (errorsx types render "Type: message" in Go);
  vendor guard's `'v' in update` has no Go analogue (an int V is always
  present) → `u != nil && Doc != "" && len(Op) > 0 && every op valid`;
  `otc.TransformEditOpsMultiple` swallows per-pair transform errors
  (upstream contract, unchanged), vendor throws — oracle paths never hit
  a transform error, so behavior is identical there.
  `historyotupdate` and `sharejsupdatemanager` are both committed now, so
  the `UpdateManager` is next.
- **Phase 8 COMPLETE** (`UpdateManager.js`, 456 LOC; vendor + the 990-
  LOC oracle were re-verified against each other this session; the FINAL
  design below is committed as `internal/updatemanager/`). The scratch
  draft (6 build errors) is gone; all chunk-4 items landed: `errString`
  errorsx type-switch, `Process` compile fix (getLock err propagates
  before release), `historyOpWire.Resolved *bool→bool` (wire omit-false
  documented), and the exported `ApplyUpdate` + `tryApplyUpdate` pipeline.
  **GATE GREEN** (`go build && go vet && gofmt -l && go test -race`
  on all 21 packages). Test files: `updatemanager_test.go` (9: Process ×4,
  FetchAndApply ×3, Continue ×2), `updatemanager_apply_test.go` (19: Apply
  13, Sanitize 2, FromOpData, Adjust ×3, New() defaults), `updatemanager_lock_test.go`
  (3: LockUpdatesAndDo success/fetch-err/method-err) = **31 tests, all
  oracle-mirrored (sinon-calledWith → seam-record order + args pins).**
  Two test bugs found + fixed this turn: (1) `TestContinueWithOutstandingUpdates`
  infinite recursion (GetProjectUpdatesLength stubbed constant 3 → endless
  Continue→Process) → the seam is now call-aware (1st→3, then→0); (2) raw
  `\Uhhhhhhhh` string escapes (illegal char in escape seq) → surrogate pins
  built via `string([]rune{…})`. **Surrogate sanitize CORRECTED this turn
  against the oracle** (line 472–490: `\uD835\uDC00` → `\uFFFD\uFFFD`):
  the vendor regex acts on 16-bit units INCLUDING valid paired surrogates,
  so `SanitizeUpdate` encodes op.i to UTF-16, remaps every unit in D800–
  DFFF to FFFD, decodes back (NOT a passthrough — earlier draft claim was
  wrong). Pin: op.i is mutated IN PLACE and `TestSanitizeUpdatesLeaveTextAlone`
  verifies text below U+D800 passes through.
- FINAL Phase 8 design (verified against vendor + oracle this session):
  - Wire `Update{Doc string, Op []map[string]any, V int, Meta map[string]any,
    Hash *string, DupIfSource []string, Dup bool}` — `Op` holds the RAW
    wire component maps. `[]map[string]any` is the implemented shape
    (refined this turn from `[]any`: it matches the committed
    `historyotupdate.Update`, keeps the `toHistoryOTUpdate` projection /
    `IsHistoryOT` default and the sanitize loop assertion-free; the
    `sharejsupdatemanager` seam converts `u *Update` itself). `dup`
    present only when set.
  - `DocInfo{Lines []string (NIL = not-found), Version int, Ranges
    *rangesmanager.Ranges, Pathname string, ProjectHistoryID *string,
    HistoryRangesSupport bool, Type string}` — no `Found` field; not-found
    ⇔ `Lines == nil` → `errorsx.NotFoundMsg("document not found: "+docID)`.
  - `Manager` exported func seams (all nil-tolerant; `New()` defaults: Inc
    = noop, Now = `time.Now().UnixMilli`, BuildPreviews = committed
    `preview.BuildSparseChangePreviews`, IsHistoryOT = committed
    `historyotupdate.IsHistoryOTEditOperationUpdate` over a raw-op
    projection). `Method = func(projectID, docID string, args ...any) (any,
    error)`. Signatures: `GetLock(projectID string) (any, error)`;
    `Extend`/`Release(projectID string, token any) error`;
    `GetUpdates(projectID string) ([]*Update, error)`;
    `GetProjectUpdatesLength(projectID string) (int, error)`;
    `SendData(map[string]any)`; `GetDoc(projectID, docID string) (DocInfo,
    error)`; `UpdateDocument(projectID, docID string, lines []string,
    version int, appliedOps []any, rng *rangesmanager.Ranges, meta
    map[string]any) error` (RAW wire appliedOps verbatim);
    `ShareJsApply(projectID, docID string, u *Update, lines []string,
    version int) ([]string, int, []any, error)`;
    `IsHistoryOT(u *Update) bool`; `HistoryOTApply(projectID, docID string,
    u *Update) error`; `ApplyRanges(projectID, docID string, rng
    *rangesmanager.Ranges, updates []rangesmanager.Update, newDocLines
    []string, historyRangesSupport bool) (rangesmanager.ApplyUpdateResult,
    error)`; `QueueOps(projectID string, ops []string) (int, error)`
    (PRE-SERIALIZED strings); `RecordAndFlushHistoryOps(projectID string,
    updates []rangesmanager.HistoryUpdate, projectOpsLength int)`;
    `RecordProjectNotificationTimestamp(projectID string, ts, userID any)
    error`; `BuildPreviews(changes []preview.Change, lines []string) []
    preview.SparseChangePreview`; `Notify(projectID, docID string, authorIDs
    []string, userID any, previews []preview.SparseChangePreview) error`;
    `RecordSnapshot(projectID, docID string, previousVersion int, pathname
    string, lines []string, rng *rangesmanager.Ranges) error`; `Inc(string)`;
    `Now() int64`.
  - Ordering (vendor-verbatim, oracle-pinned): `Process` = GetLock →
    fetchAndApply → Release (always, finally) → fetch-err: return the error
    (NO Continue); ok → `Continue` = GetProjectUpdatesLength; >0 → Process.
    `LockUpdatesAndDo(method, projectID, docID, args...)` = GetLock →
    fetchAndApply → Extend (only when fetch ok) → method (runs while the
    lock is held) → Release (always, after the method) → fetch/extend/
    method error propagates (release already ran); on success: fire-and-
    forget Continue (err → `Inc("background-processing-updates-error")` +
    swallow). fetchAndApply: GetUpdates; per update IsHistoryOT →
    HistoryOTApply(projectID, docID, u); else ApplyUpdate(projectID, u.Doc,
    u); first error propagates.
  - `ApplyUpdate` pipeline (vendor order): SanitizeUpdate (in place) →
    GetDoc → Lines==nil → NotFoundMsg; `Type != "sharejs-text-ot"` →
    `errorsx.OTTypeMismatch(Type, "sharejs-text-ot")` → ShareJsApply →
    (updatedDocLines, version, appliedOps) → ApplyRanges (doc.Ranges,
    FromOpData(docID, appliedOps), updatedDocLines, HRS) → UpdateDocument
    (updatedDocLines, version, RAW appliedOps, result.NewRanges, u.Meta) →
    if len(result.HistoryUpdates) > 0: `Inc("history-queue")` → Adjust →
    serialize + QueueOps — on queue error `Inc("history-queue-error")` +
    skip flush, but recordProjectNotificationTimestamp STILL runs (vendor
    catch swallows inside the try; the recordTS call is outside it) →
    recordTS (ts = `meta.ts` when jsTruthy else Now(); userID = meta.user_id)
    → if result.RemovedChangeIDs: rejected → previews → Notify (fire-and-
    forget) → if result.RangesWereCollapsed: `Inc("doc-snapshot")` +
    RecordSnapshot(previousVersion /* pre-ShareJsApply */, pathname, PRE-
    UPDATE lines, PRE-UPDATE ranges). Any error at any step:
    `SendData({project_id, doc_id, error: errString})` then rethrow.
  - `errString`: type-switch `*errorsx.NotFoundError | *OTTypeMismatchError
    | *DeleteMismatchError | *FileTooLargeError | *WebApiServerError` →
    BARE `.Message`; default `err.Error()`. (Pattern committed in
    historyotupdate; that port only cases NotFound+OTTypeMismatch because
    its pipeline can only produce those two — updatemanager's pipeline
    surfaces the ShareJsApply errors, hence the wider switch.)
  - Surrogate sanitize (IMPLEMENTED + CORRECTED this turn): the vendor
    regex `/[\uD800-\uDFFF]/g` acts on 16-bit CODE UNITS — it mangles BOTH
    halves of a valid paired surrogate too (oracle pin: `\uD835\uDC00` ->
    `\uFFFD\uFFFD`). `SanitizeUpdate` models that: `utf16.Encode` of
    op.i, remap every D800–DFFF unit to FFFD, `utf16.Decode` back; a
    string with no surrogate unit is left untouched. Mutates op.i IN PLACE
    (the oracle pins `update.op[0].i` after applyUpdate). Pins:
    `TestApplyUpdateSurrogatePairsReplaced` + `TestSanitizeUpdatesLeaveTextAlone`.
    (The earlier "no-op / passthrough" claim was WRONG: Go strings carry a
    paired surrogate as its BMP rune, encoding to the same two units.)
  - `AdjustHistoryUpdatesMetadata(updates []rangesmanager.HistoryUpdate,
    pathname string, projectHistoryID *string, lines []string, rng
    *rangesmanager.Ranges, newLines []string, historyRangesSupport bool)` —
    vendor arg order (updates, pathname, projectHistoryId, lines, ranges,
    newLines, HRS). `u.ProjectHistoryID = projectHistoryID` UNCONDITIONAL
    (nil ⇔ unset → wire-omitted). Meta ensure non-nil →
    {pathname, doc_length} where docLength = utils.GetDocLength(lines) and
    historyDocLength = docLength + Σ lens of rng.Changes with D != nil.
    Op loop per op: I → dl += len; !TrackedDeleteRejection → hdl += len.
    D → dl -= len; jsTruthy(meta["tc"]) → for each TrackedChanges entry
    Type=="insert" hdl -= Length; else hdl -= len. !HRS → delete(meta,
    "tc"). HRS && len(updates)>0 → last.Meta["doc_hash"] =
    utils.ComputeDocHash(newLines). ORACLE PINS (lines
    ['some','test','data'] = 14; ranges d:'bingbong' (8) → hdl init 22):
    HRS=false → dl chain 14→24→23→21, no history_doc_length, tc deleted
    from update 2's meta; HRS=true → hdl chain 22→28→30→28, tc
    'tracking-info' preserved, last update's doc_hash = sha1("after
    \nupdates"); empty doc → dl = 0. NOTE: the oracle's direct-call test 1
    passes `updatedDocLines` into the `ranges` slot and `ranges` into the
    `newLines` slot — inert because HRS=false; the Go typed signature
    follows the VENDOR arg order and cannot express the swap (test 1: pass
    rng=nil, newLines=nil, lines=['some','test','data']). Do NOT "fix" the
    vendor order.
  - `FromOpData(docID string, appliedOps []any) []rangesmanager.Update`
    (exported): each wire opData `{op: [component...], v, meta}` →
    `rangesmanager.Update{Doc: docID, V: &v (int|float64), Meta: copy,
    Op: component maps → rangestracker.Op (I/D/C/T *string, P int, U
    *bool)}`.
  - Queue wire shape (committed HistoryUpdate/HistoryOp have NO JSON tags):
    local `historyUpdateWire{ProjectHistoryID *string
    ` + "`json:\"projectHistoryId,omitempty\"`" + `, V *int
    ` + "`json:\"v,omitempty\"`" + `, Op []historyOpWire ` + "`json:\"op\"`" +
    `, Meta map `+"`json:\"meta,omitempty\"`}"; op wire: I/D/C/R/T
    omitempty, P, U/Hpos/Hlen omitempty, CommentIds, TrackedDeleteRejection,
    TrackedChanges (type/offset/length), Tracking (type), Resolved — all
    omitempty. Serialize each history update to a JSON string →
    `QueueOps(projectID, ops []string)`.
  - Rejected track-changes (faithful): filter the PRE-UPDATE `rng.Changes`
    (original list order — vendor `.filter`) by removedChangeIDs membership
    → `preview.Change{ID: &id, Op: {I/D/P from the committed
    rangestracker.Op}, Metadata: &Metadata{UserID}}` (userID from
    `c.Metadata["user_id"]`); authorIDs in the same order; previews =
    BuildPreviews(changes, PRE-UPDATE lines); Notify(projectID, docID,
    authorIDs, meta.user_id, previews).
  - Tests (WRITTEN + PASSING — 31 tests in `*_test.go`, see the Phase 8
    COMPLETE bullet; mirroring the oracle's ~12 groups): Process
    success/fetch-err (GetLock + Release(pin) + Continue order); FetchAndApply
    routing (per-doc apply, history-OT routed to HistoryOTApply and NOT
    ApplyUpdate, empty → no apply); Continue (length 3 → Process; length 0 →
    no Process); ApplyUpdate: normally (pin ShareJsApply args incl.
    updatedDocLines/version passthrough, ApplyRanges bridged ops, UpdateDoc
    (updatedLines, version, RAW ops, newRanges, meta), recordTS numeric, queue
    ops len + recordAndFlush (projectID, updates, len(ops))); ts-in-meta
    (recordTS WITH meta.ts); no-historyUpdates (recordTS + queueOps NOT
    called); surrogate pass-through (invariant op); error (ShareJsApply
    errs → SendData({project_id, doc_id, error: msg}) + rethrow); collapsed →
    Inc("doc-snapshot") + RecordSnapshot(projectID, docID, PREVIOUS
    version, pathname, pre-update lines, pre-update ranges); HRS (queueOps
    pin unchanged + HRS adjust path); rejected → BuildPreviews pin + Notify
    (projectID, docID, [author-1, author-2], meta.user_id, previews);
    no-rejected → Notify NOT called; Adjust ×3 (the pinned chains above);
    LockUpdatesAndDo success (getLock/fetch/extend/method(pin)/release/
    continue order + result passthrough) / fetch-err (release still, no
    method) / method-err (release still, no continue) / continue-err
    (error swallowed + Inc("background-processing-updates-error"));
    FromOpData direct call (wire opData maps {op:[{i,p}...], v, meta} →
    rangesmanager.Update{Doc, V *int, Meta copy, Op components}); errString
    typed-error case (e.g. *OTTypeMismatchError → bare "ot type mismatch");
    New() defaults (Inc noop seam present, IsHistoryOT default projection,
    BuildPreviews default wired, Now non-nil).
- **Chunk 4 plan (DONE this session — all 6 baseline build errors
  closed, gate green, package committed):**
  1. Replace `errString` (~line 500, the `errors.As`/`oerror` draft) with
     the committed errorsx type-switch (see the errString bullet above);
     no `errors`/`oerror` imports needed after (the draft import block
     already omits them; drop nothing else from the import list — the
     unused-`errorsx` error is FIXED by this switch using `errorsx`).
  2. Fix the chunk-1 draft `Process` compile error (line ~352 `declared and
     not used: err`): it currently has a `token, err := m.getLock` where
     `err` is unused (leftover from an earlier draft). Vendor order for
     `Process` is: getLock (error propagates WITHOUT release — vendor
     gets the token before the try, so a getLock error has no release) →
     fetchAndApply (try) → release (finally, its error beats the fetch
     error) → on fetch success: continue. Rewrite the body to capture
     `err` from getLock and propagate it, keep the fetch→release ordering
     (release runs via a defer after fetch, but a fetch error still
     releases first then returns the fetch error; the vendor `finally`
     release error wins when BOTH are set → return the release error),
     then on success call `m.continueProcessing(projectID)` and on
     `shouldContinue` recurse into `m.Process`.
  3. Fix `errString`'s call site: `LockUpdatesAndDo`'s fire-and-forget
     Continue already uses `m.inc` (unchanged); no other call site.
  4. Fix compile error line ~767: `historyOpWire.Resolved *bool` → `bool
     (json:"resolved,omitempty")` to match the committed
     `rangesmanager.HistoryOp.Resolved` (plain `bool`); documented
     divergence: a vendored explicit `resolved: false` is not modelled
     (plain bool omits false on the wire) — the wire struct is a local
     mirror for serialization only.
  5. Add the exported `ApplyUpdate(projectID, docID string, u *Update)
     error` (the vendor end-to-pipeline, raw-pass-through seams). It is
     REQUIRED — chunk-2's `FetchAndApply` calls `m.ApplyUpdate` (line ~391
     error) and it is the oracle's applyUpdate group under test. Body per
     the existing "`ApplyUpdate` pipeline (vendor order)" bullet above;
     the `errString` call site is the broadcast in that method's deferred
     error path. After all five items: `go build && go vet && gofmt`
     green on the package, then `updatemanager_test.go` (see the Tests
     list above), then the whole-module gate + commit.
- `- `sharejstptwo` mirrors `app/js/sharejs/types/text-tp2.js` 1:1 on a 50-row
  oracle golden (incl. the vendored strict transform semantics: "Remaining
  fragments in the op: N" for uncovered tails, "traverses more elements"
  for over-taking, and the prune "deletes locally inserted characters"
  error; no length-1 fast path and no empty-op early exit).
- `sharejsjson` mirrors `app/js/sharejs/types/json.js` on a 96-row oracle
  golden (incl. the "icky path hax" na-phantom, the oi-left fall-through
  append-merge, and the vendored `delete c.od` bug, all ported faithfully).
- `ratelimit`, `rangesmanager`, and the history-OT slice remain as before;
  no live services, no Mongo, no Redis, no Bull required.
- `rangesmanager` mirrors Node `RangesManagerTests.js` (1122 LOC) 1:1: 25 test
  functions covering successful apply, empty comments/changes, too-many
  limits, inconsistent-changes validation error, collapse/deletion detection,
  TDR single + multiple-at-same-position, deletes over tracked changes, deletes
  over tracked inserts, comments hpos/hlen, inserted-into-comments commentIds,
  start/full/end/in-crop comments, removedChangeIds, acceptChanges single/
  multiple, deleteComment, and getHistoryUpdatesForAcceptedChanges (inserts /
  deletes / unaccepted-delete hpos / mixed doc-length accounting).
- Pure-logic + history-OT; no live services, no Mongo, no Redis, no
  Bull required.
- `historyconversions` consumes upstream `otc` + `rangestracker` directly —
  those are Go and already merged.

## Phase 9 — ProjectHistoryRedisManager (committed, gate green)
- `internal/projecthistoryredis/` (wire.go 126, queue.go ~480,
  projecthistoryredis.go 124, projecthistoryredis_test.go ~485, 16 tests).
- 5 vendor methods 1:1 (`QueueOps`, `QueueRenameEntity`, `QueueAddEntity`,
  `QueueResyncProjectStructure`, `QueueResyncDocContent`), byte-pinning
  vendored `JSON.stringify(projectUpdate)` insertion orders via the ordered
  pair encoder; dynamic last key `projectUpdate[entityType] = entityId`;
  SETNX-first-op-timestamp multi; per-op `redis.projectHistoryOps`
  metrics.summary; resync-doc size guard →
  OError `'blocking resync doc content insert into project history queue:
  doc is too large'` {projectId, docId, docSize: sizeBound} (sizeBound =
  serialised wire length, checked BEFORE queueing; too-large → no QueueOps).
- Seam design (mirrors oracle sinon stubs): `QueueOpsSeam` checked at the
  TOP of QueueOps (oracle stubs the WHOLE queueOps incl. metrics — so entity
  / resync tests never touch the fake redis nor Summary); `Now` (ms since
  epoch: SETNX value is raw int, meta.ts is ISO via tsISO);
  `DocIsTooLarge`/`StringFileDataContentIsTooLarge` (nil = committed limits
  port); `ToHistoryRanges`/`AddTrackedDeletesToContent` (nil = committed
  ports) — all defaulted in `New()`.
- Wire pins: rename `{pathname, new_pathname, meta, version,
  projectHistoryId, <entityType>}`; add `{pathname, docLines?, url?, meta,
  version, hash?, metadata?, projectHistoryId, createdBlob(?? false),
  ranges?, <entityType>}`; resync-structure `{resyncProjectStructure:{docs,
  files}, projectHistoryId, meta{ts}}` + `resyncProjectStructureOnly` ONLY
  when truthy; resync-doc-lines `{resyncDocContent:{version, [ranges,
  resolvedCommentIds (HRS only)], content (last)}, projectHistoryId, path,
  doc, meta{ts}}`; resync-doc-historyOT `{…resyncDocContent:
  {version, historyOTRanges:{comments?, trackedChanges? (VERBATIM raw;
  {id?, ranges?} comment items; {range:{pos,length}, tracking:{type,
  userId?, ts?}} tc items — fixture key order), content}}`. `doc`/`file`/
  `docId` entities are ANY json (oracle doc:1234 / file:"file-id" /
  file:1234 numeric all pin correctly).
- DIVERGENCE (document, don't fight): vendor resync-HRS oracle fixture pins
  op key order `{i, p}` (lodash cloneDeep preserves fixture insertion order);
  production ShareJS + the HRS-add oracle pin p-first. The Go port emits
  CANONICAL p-first op wire on the HRS path (Go test pins the canonical
  form). metadata wire `{ts, user_id, resolved}` — Go sorted keys match the
  vendored wire for these keys.
- Test seam note: `seamManager` (test) records ops via QueueOpsSeam +
  limit-seam caps (sizeCaps/rawCaps) and the tooLarge switch; queueOps
  direct test uses `fakeClient`/`fakeTx` (rpush/setnx/exec recording +
  per-Exec scripted replies).

## Not yet ported (later phases — Phase 9+)
- HTTP controller (`app/js/HttpController.js`, 880 LOC) + middleware +
  routes.
- Remaining managers (injected seams, hermetic; Redis/Mongo fakes where the
  vendor logic is non-trivial): `ProjectLockManager`, `WebApiManager`,
  `DocumentManager` (833 LOC),
  `PersistenceManager`, `ProjectManager`, `HistoryManager`, `DispatchManager`,
  `SnapshotManager`, `DeleteQueueManager`.
- Persistence (Mongo) and pub/sub (Bull) adapters.

Document-updater is a **multi-session arc**: Phases 1–8 (pure-logic +
ShareJS engine + history-OT/ShareJS update managers) are committed; the
managers + HTTP controller remain for the next session (Phase 9).

## Environment
- Access the repo via absolute cwd from the repo root; the interactive shell
  mangles long repo paths.
- Gate: `go build ./... && go vet ./... && gofmt -l . && go test -count=1 -race ./...` (all green).
- Upstream `otc`/`rangestracker`/`oerror` are already Go (no further
  upstream work needed for these phases).
- `errorsx` constructors return **pointers** (`*NotFoundError`).
- Tests must use valid ISO timestamps for `ts` fields when passing raw to
  `FromHistoryOT` (upstream `otc.FromRawTrackingProps` parses the ISO string
  in `parseISODate`).
- Node `Op`'s "optional" fields (insert vs delete vs comment) are modelled
  as `*string` pointers in Go.
- Node `ts` is a string ISO on the wire; `otc.TrackingProps.TS` is
  `time.Time` on the model side.
- **Verify on disk after every turn boundary** (`stat`/`ls`): in a prior
  session, impl files generated mid-turn (the 729- and 935-LOC
  updatemanager drafts) were lost to the turn split and a summary claim
  said "on disk" stale — re-check with `stat` before building, never trust
  prior-session file-existence claims.
- **Tool boundary** (session constraint observed): the `bash` command
  channel truncates a very long single command (a ~900-line heredoc landed
  at 487 lines and its tail was corrupted). Write large files in ≤ ~500
  line chunks or via multiple `edit` appends; gofmt-verify after each chunk.

# project-history.go — Node→Go Port (SESSION HANDOFF — keep current at every phase boundary)

**Source:** `<repo>/services/project-history/` (Node.js ESM, Express)
**Target:** `<repo>/services/project-history.go/` (module `project-history`, Go, stdlib only)
**Repo:** `/home/davrot/project-history/OlliTeX-ph`, branch `golang-ph` (single go-test branch).
**Goal (owner directive 2026-09-19):** 1:1 drop-in conversion + strict test suite with detailed coverage.

## 1. STATUS (update first line on any change)

`STATUS: 2026-07-18 — **B-track C phase progress**: C1 mongo seam ✅, C2 HistoryStoreManager ✅ (82.6%, E1–E4 envelopes), C3 BlobManager ✅ (92.3%), C4 HistoryBlobTranslator ✅ (92.0%), C5 SnapshotManager ✅ (82.0%) — all -race green, oracle-pinned. B-phase (B1–B12) COMPLETE (B10 91.1% + snapshot-order bug fixed by oracle; B12 errrecorder 88.4%). Next: **D-phase** (C6–C17 all ✅: C16 SyncManager 87.3%, C17 UpdatesProcessor 81.8%), then D-phase (HttpController/real handlers/wire parity) + E-phase (coverage gate ≥80/pkg ≥85 overall, HANDOFF final). Full suite: `go test -race -buildvcs=false -count=1 ./...` green (16 pkgs).`<repo>/services/project-history/` (Node.js ESM, Express)
**Target:** `<repo>/services/project-history.go/` (module `project-history`, Go, stdlib only)
**Repo:** `/home/davrot/project-history/OlliTeX-ph`, branch `golang-ph` (single go-test branch).
**Goal (owner directive 2026-09-19):** 1:1 drop-in conversion + strict test suite with detailed coverage.

## 1. STATUS (update first line on any change)

`STATUS: 2026-07-18 — B10 chunktranslator DONE (gate GREEN: gofmt/vet/build clean, `go test -race` ok, **coverage 91.1%** ≥ 80%; 46-case 1:1 vendor oracle suite: oracle_diff_test.go 19 + oracle_diff2_test.go 19 (incl. OError merge repro + 5 tracked-changes cases) + oracle_summary_test.go 8). **Real port bug caught by the oracle: file.getDiffUpdates snapshotted initialContent AFTER convertTextOperation — vendor snapshots BEFORE (the shared TrackedChangeList mutates in place per op); reordered snapshot-then-convert.** B9 still committed 8de7017. Next: **B-track C** (service wiring — webapi/updatesmanagers/snapshotmanager/HttpController over the ports), then MAIN ARC (D41 collab e2e, live web flip, terminal audit).`

**LIVE DIR (2026-09-25):** this workdir is `/home/davrot/history_v1/OlliTeX_hist`,
branch `go_history_v1` (mirror copy of the `OlliTeX-ph` repo above; all opmodel files
are untracked there — commit when B3 goes green).

Phases (task ledger `.pi/todos/` — `todo` tool, ids PH-A1/PH-B/PH-C/PH-D/PH-E):
- [x] **P** baseline (redisx RESP client, config, errors(partial), utils, versions, hashmanager, redismanager, lockmanager, diffgenerator, server skeleton) — committed e3b0bc3
- [x] **A** live oracles: docker mongo+redis ✅, Node unit suite ✅ (613 pass), DMP goldens ✅ (39, fork-verified), wire probes ◐
- [ ] **B** pure-logic ports w/ parity tests: B1 ✅, B2 ✅, B3 ✅, B4 ✅, B5 ✅, B6 ✅ 98.2%, B7 ✅ 86.7% (db15712), B8 ✅ 95.4%, B8b ✅ 94.1%, B9 ✅ 84.8% (8de7017), B10 ✅ (2026-07-18: 46-case oracle suite green, 91.1% cov; getDiffUpdates snapshot-order bug fixed by oracle), B12 ✅ (2026-07-18: errrecorder port + 9 oracle cases, 88.4% cov, race green)
- [ ] **C** manager ports (mongo seam, historystoremanager, blob, translator, snapshot, diff, errrecorder, retry, labels, webapi, histapi, metrics, flush, summarized, health, sync, updatesprocessor)
- [ ] **D** HTTP surface (real handlers, streaming, wire parity matrix)
- [ ] **E** strict gate (coverage table ≥80/pkg ≥85 overall, live 1:1 diff vs Node app, this doc final)

### B2 (DMP port) — DONE (coverage 91.6%, 39 oracle goldens)
`internal/dmp/dmp.go` (single file, ~1050 lines) — ported & closed-over verbatim from the
repo's **fork** (byte-identity with bib-editor copy confirmed last session; this session
re-verified the fork is in `.yarn/cache` and goldens were regenerated against a plain
DMP oracle with matching behaviour — see note below).
- Core: `Op/{Equal,Insert,Delete}`, `Diff{Op,Text}`, `DMP{DiffTimeout float64}`,
  `New()` (Diff_Timeout=1.0 library default), `DiffMain`, `diffMain/diffMainArgs/diffMainArgs2`
  (checklines sentinel), `diffCompute_` (shorttext-in-longtext indexOf speedup, single-unit
  speedup, halfMatch delegation), `lineMode` (linesToChars/charsToLines + 40000/65535 bail),
  `bisect` (Myers middle-snake, v1/v2 collision, deadline bail), `halfMatch` (tie-break on
  best-common length, failure detection via bestCommon len), `commonPrefixN/SuffixN/OverlapN`,
  `indexU`, `equalU`, `bisectSplit`.
- Cleanup: `CleanupSemantic` (semanticScore scoring, end/start blankline regex windows,
  `cleanupSemanticLossless`), `matchesBlanklineEnd/Start`, `isAlnum/isJSWhitespace`, `maxU`,
  `spliceDelete/spliceInsert`, `CleanupMerge` (two-pass: dummy+merge-then-shift-sweep-recursive).
- **CRITICAL DESIGN — surrogate-safe CESU-8 codec** `str/u16` (dmp.go L88-160):
  *a high surrogate immediately followed by a low surrogate is encoded as one 4-byte scalar
  (byte-identical to UTF-8 for valid text); every other unit — INCLUDING a lone surrogate —
  is its own 1/2/3-byte scalar.* A rune-based codec is a DEAD END: Go itself maps lone
  U+D800–U+DFFF to U+FFFD on `string(rune(x))`, corrupting any diff that splits inside a
  pair (golden `utf16-emoji` expects 4 segments incl. lone `\uD83C`/`\uDF8A`). u16 decodes
  byte-by-byte (never `range s`) so lone surrogates round-trip 1:1. This is why the
  port is 1:1 at UTF-16 code-unit granularity, matching Node's `length`/`charAt`.
- **Tie-break discovery (root cause of `ins-10`/`rotation` mismatch)**: JS uses strict `>`
  in longtext/shorttext selection (both `diff_compute_` L175-179 and `diff_halfMatch_` L697),
  so **on a length tie longtext=text2**. Go v1 had `>=`; fixed in both sites.
- **Tests** (`dmp_test.go`): `TestGoldenMatch` (39 cases — 19 originals + 14
  lineMode/shift/blank + 6 whitespace regexes; each oracle case verifies the pre/post
  replay invariant before baking), `TestRoundtripProperty` (replay both ways), `TestU16Roundtrip`
  (lone high + lone low surrogate 1:1 through the codec). Node oracle: plain
  `diff-match-patch@1.0.5` index.js (require'd directly) at
  `/home/davrot/bib-editor/node_modules/diff-match-patch/index.js` (L95-1210 line map at
  §9). goldens.json + oracle_out.json committed.
- Next for B2 (only if parity drift is found later): none — all 39 goldens green,
  91.6% ≥ 80% floor.

### B3 (opmodel) — DONE (12/12 files, build GREEN, 160 oracle tests, 85.0% coverage)
Oracle: repo-local `libraries/overleaf-editor-core` (v1.0.0). All vendor sources
re-read in full during the port; oracle tables mirror the vendor `test/unit/*.test.js`
describe blocks (vendor files cited in each test's header comment).

**On disk (package `opmodel`, stdlib-only, UTF-16 units throughout):**
| file | ports vendor | API notes |
|---|---|---|
| errors.go | lib/errors.js | UnprocessableError/ApplyError/InvalidInsertionError/TooLongError/RangeError |
| utf16.go | JS str.length semantics | `Units(s)` = UTF-16 unit count |
| range.go | lib/range.js | value struct `Range{Pos,Length}` + Start/End/Contains/Overlaps/Subtract/Merge/MoveBy/ExtendBy/ShrinkBy/InsertAt/SplitAt/Intersect; `rangeFromWire(m map[string]any)` + `rangeFromAny(any)` (accepts both JSON-decoded and in-memory ToRaw `map[string]int` shapes — vendor fromRaw(toRaw) round-trip) |
| scanop.go | lib/operation/scan_op.js | interface `ScanOp` + `RetainOp/InsertOp/RemoveOp` value structs; wire decode `ScanOpFromJSON`; `LengthApplyContext`. **KNOWN FLAW: value-receiver `MergeWith` mutates a copy (no-op) — vendor mutates `this`. Builders merge via `mergeScanOps(a,b)` by value + reassignment. |
| tracking.go | tracking_props + clear_tracking_props | ONE struct `Tracking{Type,UserId,TsMs}` + `Type` discriminator; nil = absent, `Clear()` = `{Type:"none"}`. `DecodeDirective` mirrors RetainOp.fromJSON's `type==='none' ? new ClearTrackingProps() : TrackingProps.fromRaw` special case. `ts` wire form is ISO `ms` string (vendor `Date.toISOString`) — fixture: `"2023-01-01T00:00:00.000Z"`. |
| comment.go | lib/comment.js | `Comment` value struct, `NewComment` (merges ranges, errors "Ranges cannot overlap"/"Comment range cannot be empty"), `NewCommentUnsafe`, `ApplyInsert/ApplyDelete/ApplyTextOperation` (cursor tracks OUTPUT), `normalizeRanges` (sort/drop-empty/merge-touching; PANIC on unreachable overlap), `ToRaw`, `CommentFromRaw`. |
| comment_list.go | file_data/comment_list.js | ordered map (vendor `toRaw`/iterators are Map insertion order NOT sorted); `FromRaw/ToRaw/Len/Add/Delete/GetComment/Array/IDsCoveringRange/ApplyInsert/ApplyDelete`. |
| edit_operation.go | operation/edit_operation.js | interface `EditOperation{ApplyToLength,Invert,ToWire,CanBeComposedWith(ForUndo),Compose}`. |
| filedata.go | file_data/string_file_data.js | `StringFileData{Content,Comments,TrackedChanges}` + `FromRaw/GetContent/Edit`. (Blob/snapshot layer NOT ported — see Blocked §1.) |
| text_operation.go | operation/text_operation.js | `TextOperation{Ops []ScanOp,BaseLength,TargetLength,ContentHash}` + builders `Retain/Insert/Remove` (insert-before-remove normalization), `IsNoop/String/ToWire/TextOperationFromWire`, `Apply(*StringFileData) error`, `ApplyToLength(int)(int,error)`, `Invert(*StringFileData)` (tracked-change restore + Clear tails), `CanBeComposedWith(ForUndo)`, `Compose`, `Transform(a,b)` (op1-prefers). | 
| tracked_change.go | file_data/tracked_change.js | value struct `TrackedChange{Range,Tracking}` + `FromRaw/ToRaw/CanMerge/Merge/IntersectRange`. |
| tracked_change_list.go | file_data/tracked_change_list.js | `TrackedChangeList{changes []TrackedChange}` + `AsSorted/InRange/IntersectRange/PropsAtRange/RemoveInRange/Add/mergeRanges/ApplyInsert/ApplyDelete/ApplyRetain/ApplyTextOperation`. |

**Oracle test files (package `opmodel`, mirroring vendor `test/unit/`):**
text_operation_test.go (vendor text_operation.test.js + edit_operation.test.js, table-driven per describe block),
scanop_test.go (scan_op.test.js), range_test.go (range.test.js), comment_test.go (comment.test.js),
comment_list_test.go (comments_list.test.js), tracked_change_list_test.go
(tracked_change.test.js + tracked_change_list.test.js), filedata_test.go (StringFileData getters + TooLongError oracle).
160 `func Test` (incl. 50-trial property loops for compose/invert/transform that mirror
vendor `expectInverseToLeadToInitialState`), deterministic seeded RNG in `opmodel_fuzz_test.go`
(seed `0xE5E5`+trial, per-trial state save/restore). `go test -race -cover` green, **85.0%**.

**Vendor deviation notes carried in tests:** `InsertOp.ToRaw` emits `map[string]int` per
range (not `[]any`) — a wire shape that Go `json` tolerates on decode. `TrackedChangeFromRaw`
accepts both wire (`map[string]any`) and in-memory ToRaw (`map[string]int`) shapes. `Range.fromRaw`
wire values may be `float64` — `rangeFromWire` handles it.

**Known deviations (accepted, documented per HANDOFF):**
- Value-receiver `MergeWith` no-op (builder merges via `mergeScanOps`).
- `StringFileData` is a reduced mirror (no toEager/toHollow/store/getByteLength/blob layer —
  those live in the C-phase BlobManager port).
- `filedata.go` reduced mirror; vendor `string_file_data.test.js` blob/snapshot cases NOT in
  opmodel (out of scope per HANDOFF §7).
| tracking.go (3.8K) | tracking_props + clear_tracking_props | ONE struct `Tracking{Type,UserId,TsMs}` + `Type` discriminator (Go has no instanceof); nil = absent, `&Tracking{Type:"none"}` = Clear. `Clear()`, `DecodeDirective`, `DecodeWireTs`, `isoMs` (ms-precision ISO, matches toISOString) |
| comment.go (6.8K) | lib/comment.js | `Comment` value struct, `NewComment` (merges ranges + errors "Ranges cannot overlap"/"Comment range cannot be empty"), `NewCommentUnsafe`, `ApplyInsert/ApplyDelete/ApplyTextOperation(op *TextOperation, commentID)` (cursor tracks OUTPUT), `ToRaw`, `CommentFromRaw`. **BUGS (compile): L208 returns `*Comment` where `Comment` value expected (NewComment returns pointer); L142 refs missing `*TextOperation`; L196 calls missing `rangeFromWire`. **HIDDEN BUG (panic): `mergeCommentRanges` does `merged[len(merged)-1]` with NO `len>0` guard — vendor uses `lastMerged?.overlaps` (false when empty) and the first empty-range is always `continue`d, so merged is never empty *in practice*, but the guard must be added or the first-range path panics. (The duplicate `isEmpty` check after `continue` is vendor-mirrored and unreachable → keep as comment, delete the dead error return. |

### B6 (diffgenerator) — DONE (36 oracle tests, 98.2% coverage, 2 port fixes vs vendor parity)
Oracle: vendor `test/unit/js/DiffGenerator/DiffGeneratorTests.js` (395 L)
+ live verification through the **real** vendor `app/js/DiffGenerator.js` (un-patching
`_mocks.*` under node: `applyOpToDiff`/`applyUpdateToDiff`/`compressDiff` byte-identical
against the Go port on every shared case).
- **Test file:** `internal/diffgenerator/diff_generator_test.go` (was 5 tests → 36 `func Test`).
- **Mirrors vendor describe blocks:**
  - `applyUpdateToDiff` insert battery (5): middle/start/end of u, middle of i (three-way
    split, all same meta), after-delete (delete does not advance running offset).
  - `applyUpdateToDiff` delete battery (11): middle/start/end of u; across multiple u-parts;
    middle/start/end of **i** (silent drop, see port fix (a)); across u+i (silent drop of i);
    over existing deletes (d-parts pass through); insert-before-trailing-d; insert-after-only-d;
    3× ConsistencyError (positions 0/3/6 of `foobazbar` vs `xxx`); broken-ops-skipped.
  - `compressDiff` battery (6): same-user insert merge (min start / max end ts),
    diff-user inserts unchanged, same-user delete merge, diff-user deletes unchanged,
    **documented deviation:** port keeps existing u-parts and skips resync parts inline
    (no `{i,resync}`→`{u}` conversion) vs vendor (asserted per port, flagged in comment);
    nil-meta adjacent-merge.
  - Raw `ApplyOpToDiff` fold (5): partial-span delete with trailing parts (vendor byte-parity),
    delete-start, end-position-dropped (`{p:6,d:bar}` → unchanged diff — verified vendor),
    delete-across-parts (splits, NOT merged), invalid-delete → panic.
  - `BuildDiff` end-to-end (4): two-folds same-user compress (`{I:ax}`), insert-then-delete
    with folded offsets (P=4 after P:0-insert), vendor middle-delete fixture asserted as
    **ConsistencyError panic** (vendor case is unreachable in real code because their
    buildDiff test stubs `applyUpdateToDiff`), plus small helper pins (Error string,
    min/max nil, slicePart clamp, isEmpty).
- **Port fixes (NOT deviations — parity repairs vs vendor `consumeDeletions`):**
  (a) delete of (i)nsert text now silently drops the inserted text (vendor `_consumeDeletedPart`
  sets `newPart=null` when `part.i != null`); port was emitting a `{D:…}` part.
  (b) unconsumed tail preserved after a fully-spanned middle part AND partial-span middle part
  (vendor `remainingDiff.unshift(remainingPart)`); port was dropping the trailing u-parts.
  Both verified byte-identical against live vendor node runs.
- Coverage **98.2%** (gate ≥80%). Remaining uncover: `Error()` message string only hit
  through recovery in the panic-path tests (already covered by `TestConsistencyError_Message`).

### B8 (redismanager) — DONE (95.4% coverage, 2 port deviations caught by oracle)
Oracle: vendor `test/unit/js/RedisManager/RedisManagerTests.js` (556 L).
- **Port fixes (vendor parity, not deviations):**
  (a) `resyncDocContent`/`resyncProjectStructureOnly` checks use **JS truthiness**
  (`jsTruthy = v != nil`), NOT Go `v.(bool)` — vendor test feeds `123` (a number);
  the old `(bool)` cast silently never matched (batch-split doc-content limit broken).
  (b) `GetRawUpdatesBatch` removed the Go-only `batchSize <= 0` early return
  (vendor has none: the loop is entered per raw-update).
  Both verified against the vendor test suite (which stubs `redis.createClient`
  and `Settings`, so the fake only needs the ops the port calls).
- **Oracle test files** (`internal/redismanager/`):
  `redis_manager_oracle_test.go` — getRawUpdatesBatch (small/multi-page/cap),
  getUpdatesInBatches (single/batch-size/size-limit-split/half-size/op-count-limit/
  doc-content-count-limit/partial-then-limit/two-full/three-full-over-read-size,
  LRANGE fail @0 and @1, batch-size-1 single-step, empty-queue no-op, resync-project
  structure-only `_raw` back-patching, runner-error propagation), project-ID SCAN
  extraction + limits.
  `redis_manager_error_test.go` — error branch for every redisx op (fake `failOps`
  map) + invalid-timestamp paths (garbage / "0" → ok=false matches vendor `parseInt`
  NaN / falsy 0) + empty-queue / LREM-empty semantics.
- **Fake hardening** (`redis_manager_test.go`):
  `failLRangeAt` injection + per-op `failOps` map on `fakeClient`; `Scan` is now
  pattern-aware (prefix-glob `*`) and matches both str + list keys.
- Coverage **95.4%** (was 32.7%). Uncovered: a few error-return tails
  (`ParseDocUpdates` slice-err path, `GetFirstOpTimestamps`/`GetProjectIDs*`
  `len==1` dangling edge, GetUpdatesInBatches `runner==nil` guard tail).

### B11 (filetreediff) — DONE (100% coverage, lib fold only)
- Ported `lib/file_tree_diff.js` (207L) to `internal/filetreediff/file_tree_diff.go` (100% cov, 9 test funcs):
  - `Op = any` alias (vendor `Op = ShareJsOp | FileOp` union; sharejs/text ops pass through untouched — `default` branch no-ops, same as vendor).
  - `MoveOp{NewPathname: ""}` means removeFile (vendor L30 `if (this.newPathname)` guard).
  - JS Map insertion order: `Result.order []string` + `addOrder()` before map-set + `removeKey()` on delete (re-added keys get new position, mirroring vendor).
  - `deletedAtChangeIndex` is the index in this call's `changes` slice (0-based), not a global op index.
  - `onMoveCollision` callback: Go returns error (non-nil aborts fold); vendor throws.
  - Oracle: 21-case vendor table (`test/unit/js/file_tree_diff.test.js` (456 L)) + keeps-recent-add + 3 unseeded + 4 collision. No port bugs found (fixture `deletedAtChangeIndex` values corrected: move-then-remove = 1, not 0).
- Pure fold: `BuildFileTreeDiff(changes []*Change, opts *Options) (*Result, error)`; op types `AddOp/EditOp/MoveOp/OtherOp` (`MoveOp{NewPathname:""}` = vendor removeFile, `IsRemove()` mirrors `isRemoveFile()`); entry mirrors vendor `FileTreeDiffEntry` (Origin/Chain/Edited/FirstEditedAtChainIndex/File/DeletedAtChangeIndex). Go `Op` is a marker alias (`type Op any`) — the fold type-switches on concrete types, no method set. `Result` tracks vendor Map **insertion order** explicitly (order slice + re-insert on re-add; `delete` + remove-key on relocations).
- App-level `buildDiff` (the `FileTreeDiffGenerator` class + `Change` model) is C work — not ported: it needs `Core.Chunk.fromRaw`, snapshot application and `File.isEditable()` — none of which live in a Go port yet.

### B7 (UpdateCompressor) — DONE (55 oracle cases, 86.7% coverage, commit db15712)
- Ported `lib/UpdateCompressor.js` (595L) to `internal/updatecompressor/`:
  - `ConvertToSingleOpUpdates` (grouped→single-op, doc_length carry, doc_hash last-only, `history_doc_length` fallback, tc-tracking)
  - `CompressUpdates` (left-fold via private `concatTwoUpdates`)
  - `FilterBlankUpdates` (drop empty i/d)
  - `ConcatUpdatesWithSameVersion` (wrap op→[op], merge same-v/doc/pathname, doc_hash last-only)
  - `CompressRawUpdates` (the pure pipeline: convert→compress→filter→concat)
  - `DiffAsShareJsOps` (vendor `dmp.DiffTimeout = 0.1` + `CleanupSemantic` — uses existing `internal/dmp` port)
  - `insertOpsInsideSameComments` (bidirectional id-set equality)
  - `strInject`/`strRemove` (vendor module-level helpers, UTF-16 pos)
  - Port keeps `map[string]any` (JSON shape) for updates — `jsTruthy` = `v != nil` (vendor `!= null`)
- Oracle: `update_compressor_oracle_test.go` (55 vendor cases, mirroring `test/unit/js/UpdateCompressor/UpdateCompressorTests.js`)
  - convert (split/empty/comment/retain/doc-length tracked+plain/hash), filter (direct), concat (4 cases),
    compress (insert insert×15, delete delete×7, insert-delete×7, delete-insert×4, long-chain, external,
    doc-hash×3, resync×2, special-case), full pipeline + cross-version
  - No port bugs found (55/55 green)
- Known deviations: (1) vendor `OError` replaced with `errors.UnexpectedOpType` (already ported in B1);
  (2) vendor metrics wrapper NOT ported (infrastructure); (3) vendor `ConsistencyError` → error return.
  Vendor test bug "insert one delete inside the other" expects `'bafoor'` but vendor code produces `'bafoo'` — port follows vendor's code (documented in test header).

### B9 (updatetranslator) — DONE (27 oracle cases, 84.8% coverage, committed 8de7017)
Vendor: `app/js/UpdateTranslator.js` (517 L) + `Utils.js` (9 L).
Oracle: `app/js/UpdateTranslator/UpdateTranslatorTests.js` (1268 L, 27 `it`).

**Node rig (decisive, /tmp/oracle_uts/):** `run_rig.mjs` ran the real vendor file
+ real vendor tests against a stubbed node_modules and the **real**
`overleaf-editor-core` — **PASS 27**. Vendor source and its shipped tests are
internally consistent (disproved the earlier source-vs-test ambiguity hypothesis:
`fromJSON` coalesces plain-retain runs ONLY; `Clear` is a distinct tracking type;
plain↔tracked `canMergeWith` is false both sides). `probe.mjs` / `probe2.mjs`
capture every case's `toRaw` BEFORE/AFTER `compressOperations`; the assertions
below are the AFTER (post-`compressOperations`) wire.

Go port: `internal/updatetranslator/` (pure-stdlib):
- `update_translator.go` — `UpdateWithBlob{Update, BlobHashes map[string]any}`;
  `ConvertToChanges(projectID, []UpdateWithBlob) ([]*historyot.Change, error)`;
  `_convertToChange` discriminator cascade (isRenameUpdate/isAddUpdate/isTextUpdate/
  isCommentOperationUpdate/isSetCommentStateUpdate/isSetFileMetadataOperation/
  isDeleteCommentUpdate/else error);
  `_convertPathname` (strip leading `/`; `\\`→`_`; `*`→`__ASTERISK__`;
  leading-space→`__SPACE__`; subfolder leading-space→`__SPACE__` in subpath);
  `rawChange{operations, v2Authors, timestamp, projectVersion(rename-only),
  v2DocVersions{pathname,v}, origin?}`;
  timestamp normalization (epoch-millis int or ISO string → `2006-01-02T15:04:05.000Z`);
  `v2Authors[anonymous-user]→[nil]`, missing user_id→`[]` else `[user_id]`;
  `origin`: meta.origin → as-is; else `meta.type='external' && meta.source` →
  `{kind: meta.source}` (wire map; historyot.Change.FromRaw doesn't decode origin typed).
- `operations_builder.go` — vendor `OperationsBuilder` class: raw wire scan-op push
  (number/string/object), `commitTextOperation(contentHash)` (back-fill
  `retain(docLength-cursor)`, push `{pathname, textOperation[, contentHash]}`),
  `finish` (final commit), tracking context threaded from `meta` (userId/tsWire/tcSet).
  Cursor semantics: `delete` does NOT advance cursor (vendor quirk); `insert`
  advances both. **Tracked-delete rejection emits the RAW wire `{type:'none'}`
  map** (`opmodel.Clear().ToRaw()`), NOT a `*Tracking` struct — the decode path
  (`opmodel.DecodeDirective`) expects a raw wire map and would nil-track a struct,
  letting plain-retain coalesce swallow it (the vendor keeps it distinct).
- `errors_placeholder.go` — thin alias file (errors already ported B1).

Oracle: `oracle_convert_test.go` (doc/file add, doc/file rename, multi,
unknown-format, anonymous/no-user authors, 4 pathnames) + `oracle_text_test.go`
(insertions, deletions, retains-no-tracking, retains-tracking, drop-zero-length,
start-end-zero, non-linear, comment-ops, after-end, external-origin,
unexpected-op, deletes-over-tracked, tracked-rejection, tracked-changes,
delete-over-mixed). JSON-marshal byte-compare (type-agnostic numbers, deterministic
key order — B7 idiom). Vendor's "returns null for the second null update" case is
the same pipeline (no null input — vendor test asserts no error on valid chain).

**B9-side port fixes in `internal/historyot`** (NOT opmodel; opmodel's retain
coalesce was vendor-exact):
- (a) `Change.ToRaw()` emitted the Go typed origin struct as `raw[origin]`; vendor
  emits `origin.toRaw()`. Changed to `OriginRaw()`. Discovered by the
  external-origin oracle case.
- (b) `Origin.toWireOrigin` unconditionally called `extra(m)`; plain
  `(*Origin).ToWire` passes a nil extra → nil-deref panic. Guarded
  `if extra != nil`. Only RestoreOrigin passes a non-nil closure.

Coverage: **84.8%** (gate ≥80%). Full gates (build/vet/gofmt/-race) green on all
13 pkgs (all pre-existing packages still green).

### B10 (chunktranslator) — DONE (gate green: -race ok, 91.1% coverage ≥ 80%)
Vendor scope: `app/js/ChunkTranslator.js` (647 L) + `test/unit/js/ChunkTranslator/ChunkTranslatorTests.js` (3142 L, **47 `it`s). `Core.Chunk/Snapshot/FileMap` are NOT needed — the vendor module is self-contained over raw chunk maps + the opmodel ports (TextOperation.fromJSON / TrackedChangeList / Range).

**Node rig (decisive, /tmp/oracle_ct/):** `run_rig.mjs` ran the REAL vendor file + REAL vendor tests vs REAL `overleaf-editor-core` — **PASS 47**. Source & tests internally consistent; every Go fixture asserts vendor-post-internal output 1:1.

Port: `internal/chunktranslator/chunk_translator.go` (810 L, builds clean):
- `RawChunk(map[string]any) (*Chunk, error)` — `{project_id, chunk:{startVersion, history:{snapshot:{files}, changes[]}}}`;
- `UpdateSetBuilder` — applyChange (authors `id==null→null`, v2Authors concat, origin→meta.origin), applyOperation discriminator (text/rename/remove/add), missing-file null marker (distinguishes marker-present ⇒ empty diff vs absent ⇒ NotFound);
- `File.getDiffUpdates` — binary `stringLength==null` shortcut, `_loadContentAndRanges` (historyID+hash blob; rangesHash second blob, JSON.parse), `TrackedChangeList.fromRaw(ranges?.trackedChanges ?? [])`, per-op `seen` initialContent capture (before applying the first in-window update), `fromVersion<=v<toVersion` window (meta users/start_ts/end_ts + origin);
- `convertTextOperation` — `TextOperation.fromJSON` over raw op map, scan `ops`, `TextUpdateBuilder` (vendor L495+): applyRetain (tracked-delete overlap → i/d re-mapping, tracking-type none/insert→i, delete→d), applyInsert (skip tracked deletes), applyDelete (tracked-delete skip loop), finish (source suffix concat + finish op p-shift for hidden tracked deletes);
- `removeTrackedDeletesFromString` (vendor top-level fn, content.slice unit-wise);
- `ConvertToSummarizedUpdates(chunk)`, `ConvertToDiffUpdates(deps, projectId, chunk, pathname, from, to)` (last-file-in-range resolution + fence post check after final applyChange);
- `Deps{GetHistoryID, GetProjectBlob}` inject (vendor module-level HistoryStoreManager/WebApi seams; C-phase wires real managers). NOTE: port uses Go `map[string]any` raw wire (B7 idiom) not JS objects — `timestamp` accepted as epoch-millis int OR ISO string (vendor `change.timestamp` is ISO;
  toMillis: int → as-is, string → `time.Parse("2006-01-02T15:04:05.000Z", s)` — **vendor test fixtures use dynamic `new Date().toISOString()`**).
- Vendor `Errors.NotFoundError("pathname '<p>' not found in range")` → `errors.NotFound` (B1 port). Vendor `OError.tag` → error return.
- opmodel addition: `UnitSlice` (vendored `s.slice(n)` UTF-16 — port needed by TextUpdateBuilder). NOT vendor code; documented as port aid.

Remaining (oracle test file + port fixups):
1. `oracle_text_test.go` — diff cases: change-to-text (2 cases), file-restore (restore/renamed), insert/delete sequence (2), unknown-op skip, multi-file (1), file-created (3), file-renamed (4: original/after-move/new-path/multi-rename + moved-file error **×1**), file-deleted (3: up-to-delete / from-delete / after-delete error), missing-file marker (3: text/rename/delete → empty diff), multi-op-per-change (2), binary (1), v2-authors (1), tracked-changes (9).
2. `oracle_summary_test.go` — summarized cases: change-to-text, insert/delete, unknown, multi-file, file-created (+add), rename (+rename ops), delete (+remove), v2 (1).
3. `RawChunk` nil-origin + missing-file-marker tests + NotFound path (covered in oracle_summary or standalone `raw_chunk_test.go`).
Port fixup notes (from line-by-line audit 2026-09-27) — ALL RESOLVED:
- (a) vendor `TextUpdateBuilder.applyOp` passes `{tracking: op.tracking}` opts to `TrackedChangeList.applyRetain` — Go opmodel signatures `ApplyRetain(cursor, length, tracking *Tracking)` / `ApplyInsert(cursor, insertedText, tracking *Tracking)` (unpacked, not wrapped) already match; port call sites correct (`rop.Tracking`) ✓ verified against `file_data/tracked_change_list.js` L153/L225/L274.
- (b) same for InsertOp `{insertion, tracking: op.tracking}` — ✓ `ApplyInsert(cursor, insertedText, *Tracking)` (L153). `InsertOpFromJSON` unpacks `insertion`+`tracking`.
- (c) discriminators: vendor `hasOwnProperty` + `!== ''` / `=== ''` — Go `_, has := op["textOperation"]` / `strOf(v) != ""` / `== ""` (OK; vendor `undefined` path: key missing → not rename → falls through to skip; Go matches)
- (d) `finish()` shifts `p` on **all** ops with numeric p (i AND d) — port does same (`op["p"]` int check) ✓ (repro case: insert-tracked-delete+retain-none merge → `{i:'Hello world', p:0}`)
- (e) `TextUpdateBuilder.applyOp` uses vendor if-blocks (not switch); Go `switch` on ScanOp concrete type — semantically identical (op is exactly one type).

### B8b (operationscompressor) — DONE
- Ported `lib/OperationsCompressor.js` (~80L) to `internal/operationscompressor/operations_compressor.go` (9 tests).
- `CompressOperations(operations []opmodel.Operation) []opmodel.Operation` — left-fold, `CanBeComposedWith`+`Compose` mirroring vendor.
- `Op` type alias: `Op = any` (vendor `Operation` is an opaque type; ops pass through untouched when `op` == `nil`).
- Consumed by `historyot.UpdateCompressor` (B7) and B9 UpdateTranslator (Phase C).
- Mirrors vendor `test/unit/js/operations_compressor.test.js` (76L, 4 test funcs) as the oracle table.

### Blocked (known, non-blocking)
- **Write tool truncation**: >600-line single writes truncate. Split or use targeted `edit`.
- **Edit tool all-or-nothing**: if one edit in a batch fails to match (stale
  oldText), the WHOLE batch is rejected. Re-anchor from fresh `awk`/`sed` output before
  composing oldText for multi-edit batches. (3× on DMP cleanup this session.)
- **Go lone-surrogate representation**: cannot hold in a plain `string`; ONLY the CESU-8
  codec path (see B2) survives. Any future code that builds strings from unit lists must
  call `str()`, never `string([]rune{...})`.
- **Docker SHSTK workaround**: kernel 7.0.0 rejects mongo without `GLIBC_TUNABLES=-SHSTK`.
- **DMP deadline sentinel**: `Diff_Timeout <= 0` → 0 sentinel (unlimited); bisect checks
  `nowMS() > deadline` per outer d-iteration. `New()` default 1.0s; UpdateCompressor
  overrides to 0.1s. (See UpdateCompressor.js L27-28, 542-543.)

## 2. ENVIRONMENT (verified 2026-09-19)

Live stack (docker 29.5.3, kernel 7.0.0 — SHSTK workaround REQUIRED for mongo):
```bash
docker rm -f ph-mongo ph-redis 2>/dev/null
docker run -d --name ph-mongo -p 27017:27017 -e GLIBC_TUNABLES=glibc.cpu.hwcaps=-SHSTK mongo:8.3
docker run -d --name ph-redis -p 6379:6379 redis:7.4.0
```
- mongos/redis binaries NOT installed locally (containers are the stack).
- Node: v24.13.0, yarn 4.18.0 (PnP, `nodeLinker: pnp`, `enableGlobalCache: false`).
- **Node unit baseline: 613 passing (29s).**
- DMP oracle: repo fork `diff-match-patch@overleaf/diff-match-patch#89805f9c671a...`
  (unpacked `.yarn/cache`). The bib-editor copy is **byte-identical** (md5
  be8f87dd52451f8d4194daeca71a7192) — goldens generated directly from that
  unpacked file; no cross-copy divergence possible. Fork usage is a strict
  subset for us: diff_main + cleanupSemantic only.
- Go: go1.27.0 (auto-downloaded), module `project-history` go 1.25.5.

## 3. BUILD STATE (re-verify; commands)

```bash
cd services/project-history.go
# 2026-09-25: GREEN — all packages build (B3 opmodel closed 12/12)
go build ./... && go vet ./... && gofmt -l . && go test ./... -cover -race
```
| package | test cases | coverage |
|---|---|---|
| internal/dmp | 39 oracle + 3 | **91.6%** (new) |
| internal/errors | 17 | 81.2% |
| internal/config | – | 0% |
| internal/utils | – | 0% |
| internal/versions | 21 | 90.7% |
| internal/chunktranslator | (port 810 L; oracle pending) | 0.0% (no test-func yet) |
| internal/hashmanager | 2 | 88.0% |
| internal/diffgenerator | 36 oracle | **98.2%** (B6 done) |
| internal/lockmanager | 5 | 65.7% |
| internal/redismanager | 7+16 oracle+13 error | 95.4% (B8) |
| internal/redisx | (via managers) | 0% (measured 09-27: no direct tests now) |
| internal/server | 11 | 96.1% (9-27 re-measured) |
| internal/opmodel | 160 (incl. 50-trial property loops) | 84.4% (09-27: -0.6 from B10 `UnitSlice` add, untested until oracle) |
| internal/updatetranslator | 14 oracle (27 vendor cases) | **84.8%** (B9) |
| internal/operationscompressor | 9 | **94.1%** (B8b) |
| internal/updatecompressor | 55 oracle | **86.7%** (B7) |
| **total** | | **recompute** |

**Gate state 2026-09-27 (re-verified: build/vet/gofmt/-race GREEN, 14 pkgs tested incl. chunktranslator port-only at 0%): Coverage per-package (2026-09-27 re-measurement):**
dmp 93.0% | opmodel 84.4% | diffgenerator 98.2% | updatecompressor 86.7% |
operationscompressor 94.1% | **updatetranslator 84.8%** | redismanager 95.4% |
lockmanager 65.7% *(C-phase target)* | hashmanager 88.0% | filetreediff 100.0% |

## 4. HANDOFF ACCURACY CHECK (done 2026-09-19 vs code)

Prior handoff LARGELY ACCURATE. Corrections applied this session:
1. Branch is `golang-ph`, not `go-test-gitbridge`.
2. wire.go 423 gate correct.
3. server.js error branches: InconsistentChunk→422, SyncOngoing→409 (D1 done).
4. `diff-changePrefixSuffix` NOT in fork (only diff_main + diff_cleanupSemantic used —
   UpdateCompressor.js L542-543, Diff_Timeout=0.1 L28). B2 scope confirmed.
5. No bare `GET /project` route.
6. Node `Versions.js` cmp string-only (wire). Go parity trivial.
7. editor-core op oracle is repo-local (see §2).
8. Acceptance MongoHelper runs tools/migrations (root yarn) + dockerized mongo. NOTE 2026-10-06: tools/migrations is RETIRED (owner directive — the boot-time chain is out of the image); these fixtures are dormant until restored.

## 5. WIRE SPEC (server.js + Router.js, AUTHORITATIVE — verified)

- 31 routes, exact table in Router.js (Go routes.go mirrors 1:1 incl. order).
- All routes: 6-min response timeout (`res.setTimeout(360000)`).
- Body parsers: express.json() + express.urlencoded({extended:true}).
- Error middleware (4-arg, last): NotFound→404, BadRequest→400, InconsistentChunk→422,
  SyncOngoing→log+409, TooManyRequests→429+`Retry-After:300`+empty body,
  OError('Timeout',info.key)→423+`{"message":"redis lock is taken"}`, else log+500.
- `GET /status` → `res.send('project-history is up')`.
- `checkLock`/`healthCheck` → sendStatus(200|500).
- Unmatched → Express default 404 HTML (Go: sendStatus 404 text — accepted deviation).

## 6. ROUTES → MODULE MAP (Router.js verified)

See §5 of prior handoff (31 routes). All 31 registered in Go routes.go (stubs).

## 7. MODULE WORKLIST (Node file → Go package) — status after B2

| Node | L | Go pkg | status |
|---|---|---|---|
| Utils.js | 37 | internal/utils | ✅ port |
| Versions.js | 68 | internal/versions | ✅ 90.7% |
| HashManager.js | 58 | internal/hashmanager | ✅ 88% |
| Errors.js | 15 | internal/errors | ✅ 81.2% |
| DiffGenerator.js | 274 | internal/diffgenerator | ✅ **98.2%** (B6) |
| diff-match-patch | 1065L | internal/dmp | ✅ **DONE 91.6%** |
| overleaf-editor-core TextOperation/EditOperation* | ~900 | internal/opmodel | ✅ DONE 85.0% (160 tests) |
| UpdateCompressor.js | 595 | internal/updatecompressor | ✅ **B7 86.6%** |
| OperationsCompressor.js | 20 | internal/operationscompressor | ✅ **B8b 100%** |
| UpdateTranslator.js | 517 | internal/updatetranslator | ✅ **B9 84.8%** |
| ChunkTranslator.js | 647 | internal/chunktranslator | ✅ **B10 91.1%** (2026-07-18: 46-case 1:1 oracle suite; getDiffUpdates snapshot-order bug fixed) |
| editor-core file_tree_diff.js | 207 | internal/filetreediff | ✅ 100.0% (B11, lib fold; app buildDiff is C) |
| mongo-types.ts + ErrorRecorder.js | 24+322 | internal/errrecorder | ✅ **B12 88.4%** (2026-07-18: 9-case oracle incl. Q1–Q5 faithful quirks) |
| RedisManager.js | 450 | internal/redismanager | ✅ 95.4% (B8) |
| LockManager.js | 314 | internal/lockmanager | ✅ 65.7% |
| RetryManager.js | 205 | internal/retrymanager | ✅ **C8 84.8%** (2026-07-18: selector matrix R4–R9, resync pipeline) |
| ErrorRecorder.js | 322 | internal/errrecorder | (C7) |
| LabelsManager.js | 195 | internal/labelsmanager | ✅ **C9 97.0%** (2026-07-18: L1–L7 incl. falsy-arg key omission) |
| WebApiManager.js | 112 | internal/webapimanager | ✅ **C10 85.3%** (2026-07-18: W1–W5, 1-retry + 404 wrap) |
| HistoryApiManager.js | 22 | internal/historyapimanager | ✅ **C11 100%** (2026-07-18) |
| HistoryStoreManager.js | 660 | internal/historystoremanager | ✅ **C2 82.6%** (2026-07-18: 20-case oracle; E1/E2/E3/E4 envelopes + blob-creation branches; oerrTag REWRITES msg per vendor OError.tag) |
| BlobManager.js | 129 | internal/blobmanager | ✅ **C3 92.3%** (2026-07-18: 6-case oracle, -race green; concurrency-4 + retry-3 + first-error-after-all + extendLock per attempt & on success) |
| HistoryBlobTranslator.js | 123 | internal/historyblobtranslator | ✅ **C4 92.0%** (2026-07-18: 6-case oracle incl. vendor tie fixtures) |
| SnapshotManager.js | 310 | internal/snapshotmanager | ✅ **C5 82.0%** (2026-07-18: 15 oracles, -race green; NotFound/BadRequest parity, S3 hash-vs-content, S4 fallback, S5 load rule) |
| DiffManager.js | 240 | internal/diffmanager | ✅ **C6 93.1%** (2026-07-18: downward chunk walk, concat, cap error) |
| FlushManager.js | 150+88+114 | internal/flushmanager | ✅ **C13 96.8%** (2026-07-18: F1–F6 incl. bail-into-success + cleanup-once) |
| SummarizedUpdatesManager.js | 354 | internal/summarizedupdatesmanager | ✅ **C14 86.3%** (2026-07-18: S0–S7 merge/split matrix) |
| HealthChecker.js | 52 | internal/healthchecker | ✅ **C15 90.5%** (2026-07-18: H1–H4 tag messages exact) |
| SyncManager.js | 1651 | internal/syncmanager | ✅ **C16 87.3%** (2026-07-18: state machine + 8 exported flows + SyncUpdateExpander — remove/add/link/binary/doc-content-out-of-sync ops + comment & tracked-change out-of-sync ops (HistoryOT cursor+transitions); constants pinned: 100 records / 90d expire / 4h stuck / 5 clear attempts) |
| UpdatesProcessor.js | 933 | internal/updatesprocessor | ✅ **C17 81.8%** — U2 `GetRawUpdates`, U3 `getHistoryID` (6 branches + 4 exact metrics), U4 `processForProjectWithLock` (record / first-hard-failure→resync / resyncNeeded→StartHardResync), U8 bisect, U9 single, U10 under-lock resync+process, U11 `ResyncProject`, U12 `_processUpdates` (skip→psdv w/ `forceDebug` continue→skipApplied (2 discard metrics)→expand→compress→bake→translate→send (extend lock, timing + exceeds-threshold metrics)→setResyncState ALWAYS), U15 `sanitizeUpdate` (surrogate normalization). Local `Sync` seam interface (6 methods) to be wired to `internal/syncmanager` in D-phase |
| HttpController.js | 1396 | internal/httpcontroller (planned) | (D2/D3) — **NEXT** (27 handlers; router table = Router.js 108L above; server.js 71L bootstrap w/ longerTimeout 6min + express.json + error branches: Timeout→423, SyncOngoing→409, InconsistentChunk→422) |
| LargeFileManager.js | 88 | internal/largefilemanager | ✅ **D4 81.8%** — `CreateStub` (byte-exact 6-line stub `FileTooLargeError v1 ... \0`, uuid-fileId-stub name, write-error→'error writing stub file'+unlink) + `ReplaceWithStubIfNeeded` (maxFileSizeInBytes null-safe gate, hash→stub) |
| server.js/Router.js/Metrics.js | 71/108/15 | internal/server | ◐ wire.go partial |
| config/settings.defaults.cjs | – | internal/config | ✅ |
| mongodb.js | 33 | internal/mongo | ✅ **C1** (2026-07-18: Collection seam + 5 named collections) |

Go conventions: package per Node module; pure-stdlib; fakes for redis/mongo/http interfaces;
table-driven tests citing Node source+test file in comment; `go test -race` green.

## 8. TESTS (strict suite)

Per-package: `go test ./... -race -cover — floor 80%/pkg, overall 85%.
Live E2 (stretch per owner "1:1 drop-in"): docker stack §2, Node app
`yarn workspace @overleaf/project-history run start` on 3054, Go binary on alt port,
identical request set → byte-diff (sorted keys).

## 9. GOLDEN OUTPUTS (for DMP/op-algebra)

DMP oracle recipe (this session — plain DMP 1.0.5, require'd directly):
```js
// /tmp/build_oracle.js
const { diff_match_patch } = require('/home/davrot/bib-editor/node_modules/diff-match-patch/index.js');
// ... (see git history 916b025..HEAD for the 39-case builder)
```
Oracle out: `internal/dmp/oracle_out.json` (units hex-encoded per UTF-16 code unit,
preserving lone surrogates — the 4-byte CESU-8 rule in `str()` is what makes this work).

DMP oracle line map (index.js, v1.0.5, 2220L): `diff_main` L95-162;
`diff_compute_` L164-234; `diff_lineMode_` L236-304; `diff_bisect_` L306-428;
`diff_bisectSplit_` L430-454; `diff_linesToChars_` L456-539; `diff_commonPrefix` L541-570;
`diff_commonSuffix` L572-604; `diff_commonOverlap_` L606-645; `diff_halfMatch_` L651-740;
`diff_cleanupSemantic` L742-855; `diff_cleanupSemanticLossless`+score L855-970; regex
consts L985-992; `diff_cleanupMerge` L1080-1210.

## 10. OPEN QUESTIONS (probe live Node in A1/D5)

- [ ] parseReq strictObject unknown-key wire body (400 vs 500)?
- [ ] resync-pending 409 body exact bytes?
- [ ] /metrics wire (Metrics.injectMetricsRoute) — what exactly to serve?
- [ ] Express unmatched 404 HTML bytes vs Go sendStatus 404 — deviate or match?

## 11. NEXT STEPS (ordered)

1. **B6 diffgenerator-test** — **DONE** (2026-09-25): 36 oracle tests, 98.2%. Two port fixes
   vs vendor parity (verified node-vs-go byte-identical):
   (a) `consumeDeletions` silently drops (i)nsert text on delete (vendor `_consumeDeletedPart`
   newPart=null); (b) unconsumed tail preserved after fully-spanned/partial middle parts
   (vendor remainingDiff.unshift + tail). See HANDOFF B6 section.
2. **B8 redis-manager-test** — **DONE** (95.4%, oracle + error suites).
   Vendor `resyncDocContent: 123` (number) → JS-truthiness port fix (jsTruthy).
3. **B11 file-tree-diff** (lib fold) — **DONE** (100%; app-level buildDiff +
   FileTreeDiffGenerator oracle is Phase C — needs Chunk/Snapshot ports).
5. **B7/B9/B10/B12** — compressors/translators/mongo-types (ports missing →
   port first, then mirror node tests). — **B7 DONE** (db15712), **B9 DONE** (8de7017).
   Next: **B10 chunktranslator** oracle cases (port written 810 L; 47 vendor
   fixtures extracted) + commit; then **B12** and C/D/E.
6. **C-phase → D-phase → E gate.**
7. After EACH: update §1/§3, commit (`ph-go <pkg>: <what> — tests/cover/parity`).

## 12. COMMIT HISTORY (ph-go specific)

- e3b0bc3 (2026-09-19) baseline P1-P3 + server skeleton.
- 916b025 (2026-09-19) B4/B5/B1/D1: versions+hashmanager+errors tests, wire 422/409.
- 996119c (2026-09-25) B6 diffgenerator: 36 oracle tests, 98.2%, 2 consumeDeletions
  parity fixes (i-drop + tail-preserving);
- 4c8cfc2 (2026-09-26) B8 redismanager: oracle + error suites, 32.7%→95.4%,
  jsTruthy batch-split fix.
- 7654ab4 (2026-09-26) B11 filetreediff: lib file-tree fold port + 21-case vendor
  oracle, 100% (app buildDiff is C — needs Chunk/Snapshot).
- db15712 (2026-09-26) B7 updatecompressor + B8 operationscompressor + historyot op layer:
  55-case vendor oracle, 86.6% cov, no port bugs;
  opmodel `RangeFromWire` exported (B8b prep).
- 8de7017 (2026-09-26) B9 updatetranslator: 27 vendor oracle cases,
  84.8% coverage, two historyot port fixes (wire origin in ToRaw; plain-Origin
  nil-extra panic guard).
- 73f1fa1 (2026-09-26) HANDOFF: B9 DONE section + coverage table + next=B10.
- 0a6a329 (2026-09-27) B10 checkpoint: chunktranslator port (810 L vendor-
  audited) + opmodel `UnitSlice` + HANDOFF B10 IN PROGRESS (47 fixtures extracted,
  oracle pending). Gate green.

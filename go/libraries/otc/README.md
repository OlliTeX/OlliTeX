# `go/libraries/otc` — Overleaf operational-transformation engine

Go 1:1 port of **`libraries/overleaf-editor-core`** (npm
`@overleaf/editor-core`): the **OT engine** the editor and the document store
build on — text/text-range transformations (retain/insert/remove), tracked
changes and comments, the file tree (files, blob store, file data), and the
file-tree **operations + concurrent-edit transform + rebase** (the change /
snapshot / origin model).

This is a **multi-phase** package. Phases A + B (slices 1–4) + C1 are landed
and green; the client / history / schemas surface (Phase C remainder) is in
progress. See `HANDOFF.md` (LIB-15 entry) for the phase map and the exact
per-slice decisions.

## The phase map (Node → Go)
| Phase | Node source → Go | What it gives you |
| --- | --- | --- |
| **A — OT core** | `operation/text_operation.js`, `scan_op.js`, `range.js`, `comment*.js`, `tracked_change*.js`, `file_data/string_file_data.js`, `tracking*.js`, `errors.js`, `util.js` | the text-operation algebra: `TextOperation` (retain/insert/remove, apply/invert/compose/transform), `Range`, comments + comment-list, tracked changes + list, `StringFileData`, the OT error family |
| **B1 — value model** | `author.js`, `author_list.js`, `label.js`, `file_metadata.js`, `safe_pathname.js`, `file_type_detector.js`, `text_file_defaults.js` | `Author`/`AuthorList` (assertV1/V2), `Label`, doc-metadata checks, `SafePathname` (clean/cleanDebug), `FileTypeDetector` |
| **B2 — Blobs** | `blob.js`, `blob_utils.js` | the `Blob` model (git blob hash), `BlobHashFromString/Buffer/Stream/File` (sha1 `blob N\0…` = `git hash-object`), string-length detection |
| **B3 — Edit ops** | `operation/edit_operation.js` + builder/impls/transformer, `blob_store_base.js` | the `EditOperation` interface (text-edit + comment ops), `EditOperationBuilder.fromJSON`, `EditOperationTransformer` (text+comment matrix), the `BlobStore` seam |
| **B4 — File tree** | `file.js`, `file_data/*`, `file_map.js`, `errors.js` | the `FileData` interface + variants (Hash/Binary/Hollow/String/**Lazy**), `File`, `FileMap` (add/move/remove/conflicts), nullable accessors |
| **C1 — concurrent edit** | `operation/index.js` (+ no/add/move/edit/set), `change.js`, `snapshot.js`, `rebase.js`, `origin/*`, `v2_doc_versions.js` | the `Operation` family (add/move/edit/set/meta + no-op), the **4×4 transform matrix**, `Change`, `Snapshot`, `rebaseChanges`, `Origin` (+ restore variants), `V2DocVersions` |

## The core API (high-level)
- **Text ops:** `NewTextOperation()` → `.Retain/.Insert/.Remove`;
  `.Apply`, `.Invert`, `.Compose`, `.Transform`.
- **File tree:** `FileFromHash(/String)`, `FileData` variants, `FileMap`
  (`.AddFile/.MoveFile/.GetFile/.WouldConflict`), `BlobStore` seam.
- **Edit ops:** `EditOperation` (`.ToJSON/.Apply/.ApplyToLength/.Invert/
  .Compose/.CanBeComposedWith*`), `EditOperationBuilder`,
  `EditOperationTransformer`.
- **Concurrent edit:** `Operation` family (`.ToRaw/.ApplyTo/.Transform`),
  `Change` (`.FromRaw/.ApplyTo/.TransformAfter`), `Snapshot` (`.AddFile/
  .EditFile/.ApplyAll`), `Origin*` (`.FromRaw/.ToRaw`), `V2DocVersions`,
  `RebaseChanges`.

Node-platform specifics (Date ↔ timestamp, `Buffer.byteLength`, Set/Map
ordering, `OError` info) are expressed with Go idiom; the **wire shape
(`toRaw`/`fromRaw`) and the oracle-pinned error messages are preserved
exactly**. The Node `OError.tag` in-place semantics are reproduced via the
OT error family implementing `oerror.InfoProvider` (see the C1/B4
`tagErr` decision in `HANDOFF.md`).

## Conventions / gotchas
- **Nullability**: Node `null`/`undefined` returns are **Go pointers**
  (`*string`/`*int64`/`*bool`; nil = absent). Node `throw` methods are Go
  `error` returns.
- **String length is UTF-16 code units** (Node `content.length`); **byte
  length is UTF-8** (`Buffer.byteLength`) — `utf16Units` / `len` respectively.
- **Blob hash is the git blob hash** — `sha1("blob <n>\0<content>")`,
  matching `git hash-object` and the GitHub-reported SHAs (pinned).
- **The transform matrix** is the concurrent-edit core; the reverse-direction
  pairs are computed as `f(b,a).reverse()` — Node's `transpose(F)` rule
  (pinned by the 57-test operation oracle).

## Testing & coverage
`go test ./go/libraries/otc/ -count=1 -cover` — oracle-pinned to the Node
`overleaf-editor-core` unit suite across all landed phases (text-op fuzz is
**seed-pinned**, 500 trials each, mirroring the Node randomised oracles; the
concurrent-edit surface mirrors the 42-test origin/snapshot/change/rebase +
57-test operation/transform suites). **Coverage: 88.6%** (above the 85% gate).

**safe_pathname oracle (GREEN 2026-09-24)**: `TestSafePathnameOracle`
(80,782 Node-generated rows, fixture `testdata/spfuzz2.json`) passes — the port
now operates per UTF-16 code unit (defect: per-rune sup matching + missing V8
`.` line-terminator guards + `unicode.IsSpace` missing U+FEFF; all fixed, see
`go/libraries/HANDOFF_SAFE_PATHNAME.md`). The CLSI-side acceptance replica
(`services/clsi.go/safepathname_oracle`) runs the same fixture in the clsi module.

## Dependencies
Standard library (`crypto/sha1`, `unicode/utf16`, `os`, `path`, `time`,
`regexp`, `encoding/json`) + `ollitex/go/libraries/oerror`.

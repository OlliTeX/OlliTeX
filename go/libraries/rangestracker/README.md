# `go/libraries/rangestracker` — comment / tracked-change range tracker

Go 1:1 port of **`libraries/ranges-tracker`** (`index.cjs` ~808 LoC +
`schemas.js`): the engine that tracks the *ranges* a user has selected — for
**comments** and **tracked changes** — across the document, and produces the
per-user "dirty" (out-of-date) state. Used by the editor to know which comments
and tracked changes need re-rebasement.

## The API
| Symbol | Purpose |
| --- | --- |
| `func New(...) *RangesTracker` / `type RangesTracker` | one tracker per (file, user); holds the comment + tracked-change range sets and their dirty state |
| `type Op` | a range op — Node's duck-typed `op.i`/`op.d`/`op.c` → a single Go struct with presence-pointer fields; apply dispatch order is **I→D→C** (matching Node) |
| `type InsertOp` / `DeleteOp` / `CommentOp` | the three op kinds |
| `type Change` | one applied change (op + ts + user) kept in the tracker's `.changes` |
| `type CommentItem` / `type CommentMetadataSchema`, etc. | the comment / tracked-change item + the zod-compatible schemas (ported onto `validtools`) |
| `type RangeRef` / `type DirtyState` / `func Dirty(...)` | a *live* reference to a tracked range; the dirty (stale) set per user |
| `func ParseRanges(...)` | parse the raw ranges input (Node `schemas.js` `safeParse`) |
| `func GenerateId` / `func GenerateIdSeed` | the stable id generator (18-hex seed + 6-hex increment, zero-padded — Node `Math.random` parity is non-deterministic on both sides but byte-shaped 1:1) |
| `ErrUnknownOp`, `ErrDeleteMismatch`, `ErrDeletedCommentMismatch` | the error variants |
| `type Metadata` | the per-range metadata |

## How it maps to Node
- **Duck-typed ops → one `Op` struct.** Node reads `op.i` / `op.d` / `op.c`
  (insert/delete/comment); Go models those as pointer fields on a single struct
  (present = pointer non-nil). `applyOp` dispatches **I, then D, then C** exactly
  as the Node code does.
- **Live reference parity.** Node hands out the *same* objects (dirty set,
  `.changes` elements), so a captured reference sees later mutations. Go keeps
  `[]*Change` / `[]*CommentItem` internally and dirty entries are `*RangeRef`
  **live pointers** — a captured dirty ref sees later ops (pinned by
  `TestDirtyRefsAreLive`); value slices are box/copy-at-the-boundary (`New`,
  `GetChanges`).
- **`sameUser`** replicates JS `===` on (absent | null | value): absent≡absent
  → true, absent-vs-null → false, otherwise `DeepEqual`.
- **`pickTimestamp`** uses strict `<` (tie → the new ts); the ts input accepts
  `*time.Time` or an RFC3339 string; an unparseable ts → fresh (Node
  `Invalid Date`).
- **`AddComment`** uses `op.t || newId()`: an **empty-string** `t` is falsy → a
  fresh id is generated (pinned).

## Schemas
`schemas.js` ports onto `validtools` as the `CommentSchema` / `CommentOpSchema` /
`RangesSchema` / `InsertOpSchema` / `DeleteOpSchema` / metadata schemas +
`ParseRanges` (Node `safeParse`). `compose.go`'s `ArrayVal`/`UnionVal`
composites (also documented in the validtools README) are the framework pieces
these rely on.

## Testing & coverage
`go test ./go/libraries/rangestracker/ -count=1 -cover` — oracle-pinned to the Node
`ranges-tracker` suite. **Coverage: 97.0%** (above the 85% gate).

## Dependencies
Standard library + `ollitex/go/libraries/validtools` (schemas + `ParseRanges`) +
`ollitex/go/libraries/oerror`.

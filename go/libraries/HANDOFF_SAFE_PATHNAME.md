# otc safe_pathname — oracle divergence handoff (FIXED — oracle GREEN)

**Status (2026-09-24):** `TestSafePathnameOracle` is **GREEN** (80,782/80,782).
The three defects below were fixed in place in `go/libraries/otc/safe_pathname.go`
by rewriting the port to operate on UTF-16 code units, mirroring the Node
reference and the known-good reference at commit `a8a67f3`
(`services/clsi.go/ot/safepathname.go`, since deleted from history).

Verify:

```bash
go test ollitex/go/libraries/otc -run TestSafePathnameOracle -count=1
# ok: PASS safe_pathname oracle: 80782 rows
# (CLSI-side acceptance replica: services/clsi.go/safepathname_oracle —
#  same 80,782-row fixture, external package importing ollitex/go/libraries/otc)
```

## Upstream handoff (what to merge into origin/main)

The full fix + test + fixture + this doc arrived on branch `go_compile_test`
(the CLSI port branch) after merging origin/main forward; upstream takes it by
merging, cherry-picking, or `git diff` of these paths:

| Path | Role |
|---|---|
| `go/libraries/otc/safe_pathname.go` | **The fix** — full UTF-16-unit port of `safe_pathname.js` (see "The fix" below) |
| `go/libraries/otc/safe_pathname_test.go` | `[]uint16`-level `cleanPartUnits` pins (BAD_CHAR/BAD_FILE branches incl. FEFF + surrogate units) + the Node `expectClean` reason-string suite |
| `go/libraries/otc/safe_pathname_oracle_test.go` | **Unchanged** acceptance oracle (never edited; the RED came from the implementation) |
| `go/libraries/otc/testdata/spfuzz2.json` | 80,782-row fixture, Node-generated; ground truth (re-verified live Node v24.13.0 vs fixture: 0 diffs) |
| `go/libraries/HANDOFF_SAFE_PATHNAME.md` | This doc (was RED, now GREEN) |
| `go/libraries/HANDOFF.md` | Top banner updated RED → GREEN |
| `services/clsi.go/safepathname_oracle/` | CLSI-side acceptance replica (test-only package + `doc.go` + copied fixture); runs in the `clsi` module via `replace ollitex => ../../` |

Exported API is UNCHANGED (contract for all consumers, incl. otc `file_map.go`):

```go
Clean(pathname string) string
CleanDebug(pathname string) (cleaned string, reason string)
IsClean(pathname string) bool
IsCleanDebug(pathname string) (clean bool, reason string)
```

No fixture or oracle-test changes were needed — the 80,782/80,782 acceptance
gate is the same one that was RED.

## The three defects that were fixed (verified differential, exact counts)

| Class | Defect (old `safe_pathname.go`) | Rows |
|---|---|---|
| A | U+FEFF handled as non-whitespace (JS `\s` includes FEFF; Go `unicode.IsSpace` does not) | 3,361 |
| B | Supplementary chars counted per-rune, not per UTF-16 surrogate unit (U+15238 → one `_`, should be `__`) | 23,133 |
| C | Four regex stages missing the JS `.` line-terminator guard (V8 `.` excludes U+000A/U+000D/U+2028/U+2029; old port did unconditional string ops and recorded reasons it should not) | 2,140 |
| | **Total** | **28,634** (all 28,634 RED mismatches) |

Classification evidence (repro rows, hex UTF-16 units; LSEP=2028, CR=000D,
NBSP=00A0):

- **A** — fixture idx 2506: input `\uFEFF` → want `"_"`/reason `"cleanPart"`/not clean; old otc left it unchanged and `IsClean` true.
- **B** — idx 6371: input `4E00 15238 0009 0040 1680 002E 74 65 78 000D 2028 4E00`; want `4E00 5F 5F 5F 40 1680 2E 74 65 78 5F 2028 4E04` (U+15238 → `5F 5F`); old otc emitted a single `5F`. Also `IsCleanDebug` counted runes for the 1024 limit where Node counts UTF-16 units.
- **C** — idx 6378 (LSEP + trailing space): want `"no trailing spaces"` NOT recorded; old otc trimmed and recorded it. idx 6641 (LSEP after leading `/`): stage must be skipped.

`IsCleanDebug` length check is now UTF-16-code-units (JS `str.length`); the
reason-string semantics are unchanged (stage labels recorded only when the
stage actually changed the string, including the guard skip behavior).

## The fix (how `safe_pathname.go` now works)

- **UTF-16 units throughout.** Manual `utf16EncodeStr`/`utf16DecodeString`
  (Go's `unicode/utf16` maps lone surrogates to U+FFFD and must not be used);
  every stage operates on `[]uint16`. `utf16Len` mirrors JS `str.length`.
- **`isBadChar(u uint16)`**: `u=='/' || u=='*' || u<=0x1F || u==0x7F ||
  u in 0x80–0x9F || u in 0xD800–0xDFFF` — per unit, so a supplementary char
  (a surrogate pair) becomes `__` and a lone surrogate becomes `_`.
- **`isBadFileSpace(u uint16)`**: the JS `\s` set
  `{0009–000D, 0020, 00A0, 1680, 2000–200A, 2028, 2029, 202F, 205F, 3000, FEFF}`
  — the reachable-difference vs `unicode.IsSpace` is FEFF, now included.
- **Guard stages** ("no leading /", "no trailing /", "no leading spaces",
  "no trailing spaces") each check `containsLineTerinator` over the range the
  V8 regex's `.+`/`.*` must cover, and skip silently (no change, no reason)
  on mismatch — V8 faithful. "No trailing spaces" additionally requires a
  non-0x20 unit before the trailing run (the `[^ ]` in `^(.*[^ ]) *$`); the
  last non-space unit itself may be a line terminator (negated classes are not
  dot-excluded). All-space inputs are handled by `isAllSpaces`.
- **`normalizePosix`/`normalizeStringPosix`**: unit-based port of
  path-browserify's `posix.normalize` (the `var code` out-of-scope quirk
  preserved from Node).
- **`blockedFiles`**: anchored whole-pathname table (13 names) replacing the
  old regex.

Unit pins in `safe_pathname_test.go`: per-UTF-16-unit `cleanPartUnits` cases
(slash, star, 0x01/0x7F/0x80/0x9F, the U+1F600 pair → `__`, lone high/low
surrogates, FEFF edges, 0x2000/0x202F/0x3000/0xA0, the full `.`/`..`/`....`
cases) so the oracle is not the only protection.

## Acceptance

```bash
# ollitex module (the fix location)
go test ollitex/go/libraries/otc -run TestSafePathnameOracle -count=1
# clsi module (acceptance replica)
( cd services/clsi.go && go test ./safepathname_oracle -count=1 )
# expect: PASS safe_pathname oracle: 80782 rows (both)
```

The oracle test is committed and stays committed; it is the acceptance gate.
No changes to the fixture or the test itself are needed (or wanted) — the
fixture came from the Node reference.

## Scope note

- Stages unchanged by the fix: `workaround for IE`, `no multiple slashes`,
  `empty`, `cleanPart` structure, `BLOCKED_FILE_RX`. The rework covered the
  three defect classes above plus the `utf16Len`/`normalizePosix` internals.
- Consumers: otc `file_map.go` (`IsCleanDebug`) and the two oracle tests.
  The exported signatures are unchanged, so nothing else needed to move.
- CLSI (upstream consumer) now sees the contract in BOTH modules; keep the
  two copies of the fixture byte-identical (they are copies, and the CLSI
  one is what upstream merges).

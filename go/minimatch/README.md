# go/minimatch — 1:1 port of minimatch@10.2.6 (CLSI)

Package `minimatch`, part of module `ollitex`. CLSI (the Go rewrite of the
Overleaf CLSI Node service) uses this for its `dot:true` file-keep patterns
(`keepdir/**`, `.other/keepdir/**`, PRECIOUS_FILE_PATTERN etc.). POSIX only.

## API (what CLSI uses)

```go
m, _ := minimatch.New(pattern, &minimatch.Options{Dot: true})
m.Match("keepdir/a/b.txt")        // bool (line 1409 .match semantics)

// one-shot module-level convenience:
ok, _ := minimatch.Match(file, pattern, &minimatch.Options{Dot: true})

// building blocks used by the oracles:
minimatch.Escape(s, o)           // escape (7 magic chars per combo)
minimatch.Unescape(s, o)         // unescape
minimatch.Expand(pattern, o)     // brace-expand -> []string
minimatch.CompileRe(src)         // closed-dialect RE engine (*reProg)
```

`*Minimatch` fields used by the oracle driver (not CLSI): `set [][][]mCell`
(`mCell{globstar, kind, s, re}` — string / compiled-RE / GLOBSTAR cells).

### Ported (fidelity)
- `Minimatch` class: ctor, `parseNegate`, `make` (comment/empty/globset/
  set), `slashSplit`, `preprocess` (optimization levels 0/1/2 — `..` squeeze,
  adjacent-`**` collapse, first/second phase), `braceExpand`.
- `AST.parsePortion` + full `toRegExpSource`: segments, extglob
  (with `|` alternation, nested, negated `!` fill), char classes incl.
  POSIX `[:class:]`, escape `\`, `[`/`]`, dotguard, `!`-negate filledNegs,
  `end`(isEnd) guard.
- `matchOne` / globstar / `matchGlobStarBodySections` — tri-valued (walk/hit/
  fail) port of the JS `#matchOne` + `#matchGlobStarBodySections`.
- escape/unescape (7-char combo), minimal brace-expand, classparse.
- `reengine`: closed-dialect JS regex (no `lookbehind`, no `(?<name>`, no
  unicode-`\p{...}` property escapes; byte-mode approximation) + `test`.

### Deliberately NOT ported (documented divergences)
- **Fast tests** (`starRE`, `starDotExtRE`, `qmarksRE`, `qmarksNoExtRE` +
  `*Test*` closures): upstream compile-time shortcuts that pick the *same*
  dot choice as the regex path. Go builds one source; oracle-verified
  equivalent. **`makeRe` dropped** (no oracle, not used by CLSI).
- **Windows** (`isWindows`, UNC/drive-letter, CRLF): CLSI is POSIX. Port
  returns error for `Platform:"win32"/"linux"`... (only `""`/`"posix"` accepted).
- **`\p{...}` / `u` flag**: Go `re` engine runs in byte mode; upstream `\p{X}`
  matches by code point while the Go engine matches by byte. For CLSI
  patterns (ASCII paths) the two are identical; document divergence for any
  multi-byte `[\p{...}]` use.
- **`nocase`**: port applies byte-level case folding (documented; CLSI uses
  case-sensitive). `nocaseMagicOnly` handled.
- **Level 0 unreachable** (`adjacentGlobstarOptimize`): upstream `optimizationLevel ?? 1`
  yields 0 only when a caller explicitly passes `0`; the Go `Options.OptimizationLevel`
  int can't distinguish "unset" (zero) from "explicit 0", so `optimizationLevel()` maps
  0→1. Consequence: level 0 (adjacent-**globstar** merge only) is unreachable via the
  public API in this port — `adjacentGlobstarOptimize` is dead code here (not covered;
  not a bug).
- **Level 2 is NOT level‑1‑identical for `.`/`..`/empty paths (faithful, not a bug):**
  upstream `levelTwoFileOptimize` (file side) and `firstPhasePreProcess` (pattern side)
  run only at level≥2 and strip/normalise `.`/`..`/empty segments, so e.g. Go
  `Match("a/./b", "a/b", level2)=true` while `level1=false`. This matches upstream
  `src/index.ts` (verified by reading the source); the oracle TSVs pin the default
  level 1 only. Tests therefore assert level-2 == level-1/oracle only for CLEAN
  inputs (no `.`, `..`, or empty segments).
- **Level-2 `**/../` non-termination — FIXED:** `pre/**/../p1/p2/rest` (level 2)
  previously did not terminate (a full `go test` run burned the 10m default
  timeout on it). Root cause: in `firstPhasePreProcess`, the second `**/../`
  branch was built as `make([]string, len(parts)+1)` and copied `parts[gs:]`,
  which KEPT the adjacent `..` and re-pushed a pattern identical to the input, so
  `didSomething` stayed `true` forever. Fix (mirrors upstream `src/index.ts`
  `other = parts.slice(0); other[gs] = '**'`): build `other` at the SAME length as
  the `**`-removed `parts` and set `other[gs] = "**"`, which OVERWRITES the `..`
  (shifted onto index `gs` after the splice) — so the branch is
  `pre/**/p1/p2/rest`, and the whole expansion terminates. Regression-pinned by
  `coverage_test.go::TestLevel2DotNormalizationSmoke` (the `pre/**/../p1/p2/rest`
  row now completes in microseconds). Upstream's own `**/..` *performance*
  caveat (walking) does not apply to `match()`, which now terminates.

### Oracle coverage (acceptance gate; see HANDOFF.md)
- `testoracle.tsv` 35,400 rows (RE-engine) GREEN
- `escape.tsv`/`expand.tsv`/`esc_oracle.tsv` GREEN
- `match_oracle.tsv` 7,854 rows (Minimatch.class `dot:true`) GREEN
- `segM.tsv` 27,360 + `segPortion.tsv` 9,924 (AST per-portion) GREEN
- `full26.tsv` 3,400 + `diff26.tsv` 1,334 (differential, dot 0+1) GREEN
- 30,000+ hand-written smoke/edge cases GREEN
- `coverage_test.go` — public API surface (module `Match`, `MatchList`, `HasMagic`,
  `MMBraceExpand`) + level-2 over the 7,854-row oracle (CLEAN-file assertions) +
  level-2 `.`/`..` normalisation smoke (incl. the fixed `**/../` regression): GREEN.
  **Total statement coverage: 86.2%** (meets the ≥ 85% gate). The remaining un-covered
  statements are the documented level-0 branch (unreachable here) and the
  level-1≠level-2 `.`/`..`/`""` normalisation differences (faithful to upstream; not
  oracle-pinned in the level-1 TSVs — not force-covered). The former `**/../`
  non-termination is FIXED and regression-pinned (see above).

Run: `go test -cover ./go/minimatch/...`

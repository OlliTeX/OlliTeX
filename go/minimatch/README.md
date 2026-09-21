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

### Oracle coverage (acceptance gate; see HANDOFF.md)
- `testoracle.tsv` 35,400 rows (RE-engine) GREEN
- `escape.tsv`/`expand.tsv`/`esc_oracle.tsv` GREEN
- `match_oracle.tsv` 7,854 rows (Minimatch.class `dot:true`) GREEN
- `segM.tsv` 27,360 + `segPortion.tsv` 9,924 (AST per-portion) GREEN
- `full26.tsv` 3,400 + `diff26.tsv` 1,334 (differential, dot 0+1) GREEN
- 30,000+ hand-written smoke/edge cases GREEN

Run: `go test -cover ./go/minimatch/...`

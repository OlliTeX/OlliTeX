# `go/libraries/settings` — settings loader + deep merge

Go 1:1 port of **`libraries/settings`** (npm `@overleaf/settings` v3.0.0):
`merge.js` (deep merge) + `Settings.js` (per-process config/defaults
discovery). Every CE service boots through this to resolve its settings
object from `settings.defaults.cjs` + an environment-specific override.

## The API
| Symbol | Purpose |
| --- | --- |
| `func Merge(overrides, defaults map[string]any) (map[string]any, error)` | deep-merge `overrides` over `defaults` (maps recurse; everything else replaces) — Node `merge(settings, defaults)` |
| `func Load(opts *LoadOpts) (*Result, error)` | mirrors Node `Settings.js` — the env/config discovery walk (see below) returning the resolved object + which paths were used |
| `type Result struct{ Value map[string]any; DefaultsPath, OverridesPath string; FlyingBlind bool }` | `FlyingBlind` = neither defaults nor overrides found → `{}` (Node "I'm flying blind") |
| `type LoadOpts struct{ Env EnvLookup; FS Filesystem; ReadModule func(string)(any,error); CWD, EntryPointDir string }` | the seams + process context (`EnvLookup` = `process.env`, `Filesystem` = `fs.existsSync`, `ReadModule` = PnP `require` of the config module) |
| `ErrNilDefaults`, `ErrNilOverrides`, `ErrNullOverride` | the merge/load error variants |
| `type NullOverrideError` / `func PathOf(err error) string` | a null-override failure that records the offending settings **path**; `PathOf` extracts it |

## The `Load` discovery walk (Node order)
1. `OVERLEAF_CONFIG = env OVERLEAF_CONFIG || env SHARELATEX_CONFIG`; both set
   + unequal → `found mismatching SHARELATEX_CONFIG, rename to OVERLEAF_CONFIG`.
2. `NODE_ENV = env NODE_ENV || "development"` (lowercased) — only used to build
   the `settings.<NODE_ENV>.{cjs,js}` fallback path.
3. `defaultsPath` = first existing of `<CWD>/config/settings.defaults.{cjs,js}`,
   `<EntryDir>/config/settings.defaults.{cjs,js}`.
4. `overridesPath` = first existing of `OVERLEAF_CONFIG` (only when the env is
   non-empty), `<CWD>/config/settings.<NODE_ENV>.{cjs,js}`.
5. neither → `No settings or defaults found. I'm flying blind.` → `{}`.
6. dispatch: `settings.mergeWith` is a function → `mergeWith(overrides)`, else
   `merge(overrides, settings)`.

## Conventions / gotchas
- **V8 key order is reproduced.** Node iterates `Object.entries` in
  index-then-string order, so `Merge`'s partial-apply order is pinned: keys are
  normalized to *canonical-numeric-then-lex* before merging. A Go-map's random
  iteration order is never exposed.
- **`Merge` mutates/returns `defaults`** building the merged object; overrides
  win at every level (a truthy-`0`/`false`/`""` override replaces the default —
  the `jsFalsy` arms are covered for every int/width the runtime can hold).
- **`NullOverrideError` carries a `Path`** the plain Node TypeError lacks
  (Go convenience for diagnostics); the root-cause text stays verbatim.
- **`moduleWithMerge`** is detected on the *defaults module value* (a Go value
  implementing `MergeWith(any) any`), not a reflect probe like Node's
  `typeof === 'function'`.

## Testing & coverage
`go test ./go/libraries/settings/ -count=1 -cover` — oracle-pinned to the Node
`settings` suite (the merge deep-recurse, V8-order partial-apply-on-throw, every
discovery branch, flying-blind, both-mismatch, `mergeWith` dispatch).
**Coverage: 98.6%** (above the 85% gate).

## Dependencies
Standard library only.

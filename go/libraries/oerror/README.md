# `go/libraries/oerror` — portable Overleaf error type

Go 1:1 port of **`libraries/o-error`** (npm `@overleaf/o-error`, `index.cjs`).
A tiny, dependency-free package that gives the whole Overleaf Go tree a single,
inspectable error type (`*OError`) with structured **info**, a **cause** chain,
and **tagged** annotations that survive wrapping.

## What it's for
Node's `OError` is the canonical error shape across the services: a `message`,
an optional `info` object, an optional `cause`, plus `OError.tag` (attach
structured metadata, capped at `OError.maxTags`) and `OError.getFullInfo` /
`getFullStack` (read it all back out of a wrapped chain). Go has no built-in
equivalent, so the ported libraries and services use this package to:

1. throw a typed error with info + cause — `New("msg", map[string]any{...}, cause)`;
2. attach structured tags onto an error (in place) — `(*OError).Tag(msg, info)`;
3. read the merged info / the rendered stack out of a wrapped chain —
   `GetFullInfo`, `GetFullStack`; and
4. expose a foreign error's info to `GetFullInfo` by implementing `InfoProvider`.

## The API
| Symbol | Purpose |
| --- | --- |
| `type OError struct{ Type, Message string; Info map[string]any; Cause any; Tags []TaggedError }` | the error; `Error()` renders `"<Type or OError>: <message>"`; `Unwrap()` supports `errors.Is/As` |
| `func New(message string, info map[string]any, cause ...any) *OError` | build one (nil `info`/`cause` are no-ops, like Node's falsy check) |
| `func Of(message string) *OError` | the no-info/no-cause form |
| `(*OError).WithName(name)` / `.WithInfo(m)` / `.WithCause(c)` | chainable setters (Go stand-ins for a Node subclass / `withInfo` / `withCause`) |
| `(*OError).Tag(message, info)` / `func Tag(err error, message string, info map[string]any) error` | attach a tag **in place** for `*OError` (Node singleton semantics); the static `Tag` wraps any other error once, then tags (a wrapped plain error renders `OError: <its text>`) |
| `func GetFullInfo(err error) map[string]any` | merged info across the whole cause chain + each level's `Info`/`Tags` — **outermost level wins**; reads `InfoProvider` on foreign causes |
| `func GetFullStack(err error) string` | `"<error>"` + one line per tag + `"caused by:"` chain (4-space indent per depth) — the exact line structure the Node suite pins |
| `type TaggedError struct{ Message string; Info map[string]any }` | one tag; `IsDropped()` = the sentinel |
| `type InfoProvider interface{ OErrorInfo() map[string]any }` | opt-in: a non-`OError` error exposes its info map to `GetFullInfo` (Node reads `.info` off *any* object in the chain) |
| `var MaxTags int = 100`, `var DroppedTags TaggedError` | the per-error tag cap + the `"... dropped tags"` sentinel occupying slot 1 (Node `OError.maxTags` / `DROPPED_TAGS_ERROR`) |

## Conventions / gotchas
- **Tagging is in-place for `*OError`.** `Tag(obar, "k", info)` returns the *same*
  `*OError` (mutated) — so `errors.As`/`GetFullInfo` both see the tag, and repeated
  tags accumulate on one object (Node's "singleton error" behaviour).
- **`GetFullInfo` walks the chain**, merging cause-first then each level's info+tags,
  outermost winning on key collision. This is what the `otc` lazy-file error oracles
  rely on (the OT error family implements `InfoProvider`).
- **No V8 stack capture.** Node's `tag` records a stack trace; Go drops that and keeps
  the tag's `message` + `info` (documented divergence). `TaggedError.IsDropped()` is
  structural (message match), not Node's reference `===`.
- Errors are *returned* (Go idiom); the package also works when you `panic` one to
  mirror a Node `throw`.

## Testing & coverage
`go test ./go/libraries/oerror/ -count=1 -cover` — oracle-pinned to the Node
`libraries/o-error` suite (New/Of/Tag/MaxTags/cap-drop/GetFullInfo chain + InfoProvider/
GetFullStack line layout). **Coverage: 96.8%** (above the 85% gate).

## Dependencies
None (standard library only).

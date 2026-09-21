# `go/libraries/validtools` — schema validation (zod-compatible)

Go 1:1 port of **`libraries/validation-tools`** (npm `@overleaf/validation-tools`,
a wrapper over **zod**): a small schema/validation library that produces the
exact same **issue lists** and **user-facing error messages** as the Node
services, plus the `handleValidationError` HTTP helper. It is the shared
validation layer for the ported services (route/params schemas, `params`
404-class failures, `zz.uploadedFile`, etc.).

## The core model
| Symbol | Purpose |
| --- | --- |
| `type Val interface{ Validate(present bool, v any) (any, []Issue) }` | the primitive schema contract — `present=false` = key absent, `v==nil` = present-null (Node's `optional().transform(v => v ?? undefined)` folds both into one "absent") |
| `type Field struct{ Name string; Optional, Nullish bool; Schema Val }` | one declared object field (`z…().optional()` / `zz.nullabsorption`) |
| `func NewObject(fields …Field) *Object` | zod **non-strict** `z.object` — declared fields validated in order, unrecognized keys **silently dropped** (no issue); the common route/outer-envelope form |
| `func NewStrictObject(fields …Field) *StrictObject` | `z.strictObject` — unrecognized keys emit an `unrecognized_keys` issue (the `params`/route-segment + `zz.uploadedFile` 404-class shapes) |
| `type Issue` | one validation problem (path + code + message) — the zod issue shape |
| `type ZodError struct{ Issues []Issue }` | a failed parse (the issue list) |
| `func NewValidationError(ze *ZodError, data any) *ValidationError` | build the **friendly** thrown error: one entry per top-level issue, joined with `"; "`, no title-case prefix (Node `validateSchema` exact text); `.Error()` = the message, `.ZodError()` = the issue list for the wire path |
| `InvalidRequestError` / `InvalidParamsError` + `New…` | typed errors so `HandleValidationError` can `errors.As` them and route the status (params → 404-class; request → 400) |
| `func HandleValidationError(func(err, res, next))` / `func CreateHandleValidationError(statusCode int)…` | the exported HTTP error handler (`createHandleValidationError(statusCode)`); default 400 |
| `func Wire(...)` | the B2 wire encoding for `{"error": …}` responses |

## The validator vocabulary
Scalars/composites: `StringVal`, `NumberVal`, `BoolVal`, `StringArrayVal`,
`ArrayVal` (any element schema), `UnionVal` (`a.or(b)`), `EnumVal`.
Domain validators: `Datetime` (`DTSchema`/`DTAdapterVal`), `Filepath`,
`RouteSegment`, `SafePath`, `ProjectHistoryID`, `ChunkID`, `EventName`, plus the
service-specific ID validators (`BuildID`, `SubmissionID`, `UploadedFile`,
`IndexSeg`). `compose.go` adds `ArrayVal` + `UnionVal` as reusable framework
composites (used by rangestracker / otc schemas).

## Conventions / gotchas
- **Issue order is declaration order**; `unrecognized_keys` comes **after** the
  per-field issues (pinned by the Node oracle).
- **Key order is sorted alphabetically** for determinism — Node preserves
  JSON.parse insertion order, Go maps are unordered (documented deviation; the
  sorted wire order is what the Node suite sees).
- **`Object` vs `StrictObject`** matters: unknown keys are dropped (no issue)
  under `Object` but a 404-class `unrecognized_keys` issue under `StrictObject`.
  Pick the one matching the Node `z.object` / `z.strictObject` in the route.
- **Validation is a port, not a reimplementation** — goldens were produced
  against **live Node 22 + zod 4.1.11 + zod-validation-error 4.0.1**, and the
  audit-fix regressions in `audit_fixes_test.go` were each verified against that
  oracle before being applied.

## Testing & coverage
`go test ./go/libraries/validtools/ -count=1 -cover` — oracle-pinned to the Node
`validation-tools` golden suite (friendly-message text, strict vs non-strict,
optionals/nullish, unions, arrays, the domain validators, handler status
routing). **Coverage: 98.9%** (above the 85% gate).

## Dependencies
Standard library + `ollitex/go/libraries/oerror` (so `HandleValidationError` can
`errors.As` the typed errors to route status).

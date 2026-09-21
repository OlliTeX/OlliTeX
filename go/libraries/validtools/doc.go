// Package validtools is the 1:1 drop-in Go port of `libraries/validation-tools`
// (npm `@overleaf/validation-tools` over zod 4.1.11 + zod-validation-error 4.0.1).
//
// The Node package wraps request validation in one error model: a failed Zod
// parse is rendered to a wire string (`fromError(err).toString()`), classified
// into `InvalidRequestError` / `InvalidParamsError`, and routed by
// `handleValidationError` (400 vs 404 vs pass-through). This package ports
//
//   - the `zz.*` leaf validators (objectId … uploadedFile, datetime variants)
//     as Go predicates returning the *exact* issues the Node schema emits,
//     including chained refinements (Node evaluates ALL failing checks, e.g.
//     filepath "/../../etc" -> ["Path is absolute", "Path traversal detected"]);
//   - the strict-object machinery (declared-key order, one trailing
//     unrecognized_keys issue);
//   - the wire renderer (1:1 with zod-validation-error v4 fromError());
//   - the error types and the HTTP handler classification;
//   - validateSchema's friendly assembly ("\"f\" is required" / "\"f\" - msg").
//
// Go conventions (documented deltas):
//
//   - JS `undefined` (absent) is the Go zero/absent convention; JS `null` is a
//     nil map value. Callers pass (present bool, v any) per field.
//   - Go has no Date-vs-string union: time.Time inputs satisfy the `z.date()`
//     arm; string inputs satisfy the `z.iso.datetime()` arm.
//   - Leaf validators return (value, issues); the strict-object framework
//     composes them. Services then raise NewInvalidRequest/ParamsError and
//     route via HandleValidationError (see handler.go) or render friendly
//     messages via ValidateSchema (see validate.go).
//
// Wire strings are golden-pinned against Node (probe tests).
package validtools

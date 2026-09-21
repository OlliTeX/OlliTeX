package validtools

import (
	"encoding/json"
	"errors"
	"net/http"
)

// handler.go — handleValidationError / createHandleValidationError
// (validation-tools/index.js + Errors.js).
//
// Go surface mirrors the Express one: (err, res http.ResponseWriter, next).
// The B2 service (docstore) owns its middleware chain; this is the leaf.

// CreateHandleValidationError mirrors createHandleValidationError(statusCode).
//
//	Class dispatch (Node instanceof, Go errors.As):
//	  InvalidParamsError    → HTTP 404
//	  InvalidRequestError   → HTTP statusCode (default 400)
//	  anything else         → next() unchanged
//
// Body for both validation errors:
//
//	{"error": <Wire(zodError)>, "statusCode": <code>}
//
// as Node's `res.status(x).json({ error: fromError(zodError).toString(),
// statusCode })`.
func CreateHandleValidationError(statusCode int) func(err error, res http.ResponseWriter, next func(error)) {
	return func(err error, res http.ResponseWriter, next func(error)) {
		var req *InvalidRequestError
		var params *InvalidParamsError
		if errors.As(err, &req) {
			writeWire(res, req.ZodError, statusCode)
			return
		}
		if errors.As(err, &params) {
			writeWire(res, params.ZodError, 404)
			return
		}
		next(err) // Node: next(err)
	}
}

// HandleValidationError is the exported default (statusCode 400), matching
// `export default createHandleValidationError()`.
var HandleValidationError = CreateHandleValidationError(400)

func writeWire(res http.ResponseWriter, z *ZodError, code int) {
	body, _ := json.Marshal(map[string]any{
		"error":      Wire(z.Issues),
		"statusCode": code,
	})
	res.Header().Set("Content-Type", "application/json; charset=utf-8")
	res.WriteHeader(code)
	_, _ = res.Write(body)
}

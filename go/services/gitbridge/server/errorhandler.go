package server

import (
	"fmt"
	"net/http"
)

// productionErrorBody ports ProductionErrorHandler: a single JSON line, no
// Content-Type, no Pragma. Java writes `{"message":"HTTP error <status>"}`
// and nothing else (Jetty auto-adds Content-Length).
func productionErrorBody(code int) []byte {
	return []byte(fmt.Sprintf(`{"message":"HTTP error %d"}`, code))
}

// grid writes a ProductionErrorHandler cell with NO Content-Type header —
// live parity with Jetty (Java writes the body without setting a CT, and
// Jetty does not auto-add one; Go's ResponseWriter would default to
// "text/plain; charset=utf-8" unless the header KEY is present, so it is
// marked with a nil slice: present map key, nothing written to the wire).
func grid(w http.ResponseWriter, code int) {
	w.Header()["Content-Type"] = nil
	w.WriteHeader(code)
	_, _ = w.Write(productionErrorBody(code))
}

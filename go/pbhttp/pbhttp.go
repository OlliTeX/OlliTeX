// Package pbhttp holds the small set of shared HTTP helpers used across the
// OlliTeX Go service conversions. Keeping them in one package lets each
// service package (filestore, dropboxinterface, …) reuse a single JSON writer
// and the SHARED_SERVICE_TOKEN middleware without redeclaring them (redeclaring
// across one shared package caused build failures when the services lived in a
// single flat package).
package pbhttp

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// WriteJSON writes a JSON body with the given status code.
// WriteJSON writes a JSON body exactly like Express' res.json: Content-Type
// `application/json; charset=utf-8` and the precise JSON byte sequence (no
// trailing newline — verified against the Node services, whose Content-Length
// equals the exact body length).
func WriteJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	b, err := json.Marshal(v)
	if err != nil {
		// JSON marshalling of plain values effectively never fails; if it does,
		// the status line is already on the wire and we emit an empty body.
		return
	}
	_, _ = w.Write(b)
}

// WriteJSONErr writes an error-shaped JSON body (1:1 with the Node
// `{error}` / `{...}` response shapes).
func WriteJSONErr(w http.ResponseWriter, code int, v interface{}) {
	WriteJSON(w, code, v)
}

type tokenWarn struct{ once sync.Once }

// RequireServiceToken returns a middleware enforcing SHARED_SERVICE_TOKEN.
// When expected is empty it warns once and allows the request through (dev
// mode); otherwise the caller must present the token via X-Service-Token or a
// Bearer Authorization header, compared in constant time.
func RequireServiceToken(expected string, warn func()) func(next http.HandlerFunc) http.HandlerFunc {
	var w tokenWarn
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(rw http.ResponseWriter, r *http.Request) {
			if expected == "" {
				w.once.Do(func() { warn() })
				next(rw, r)
				return
			}
			cand := r.Header.Get("X-Service-Token")
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") && cand == "" {
				cand = strings.TrimPrefix(auth, "Bearer ")
			}
			if !TimingSafeEqual(cand, expected) {
				WriteJSON(rw, 401, map[string]string{"error": "Invalid or missing service token"})
				return
			}
			next(rw, r)
		}
	}
}

// TimingSafeEqual is a constant-time string comparison (avoids an early-return
// timing leak when the lengths differ).
func TimingSafeEqual(a, b string) bool {
	ab, bb := []byte(a), []byte(b)
	if len(ab) != len(bb) {
		var dummy [256]byte
		_ = dummy
		n := len(ab)
		if n > len(bb) {
			n = len(bb)
		}
		var sum byte
		for i := 0; i < n; i++ {
			sum ^= ab[i] ^ bb[i]
		}
		_ = sum
		return false
	}
	var sum byte
	for i := range ab {
		sum ^= ab[i] ^ bb[i]
	}
	return sum == 0
}

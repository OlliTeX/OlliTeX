// Package server: HTTP server layer (ports GitBridgeServer + all handlers).
//
// Java Jetty Handler.Sequence is mirrored here as a Go middleware chain wrapped
// around a base mux. Order: CORS (outermost, per Jetty) → route dispatch.
package server

import (
	"net/http"
)

// allowedOrigins is the set of Origin values from config.allowedCorsOrigins
// (comma-separated string in the config file).
type originSet map[string]struct{}

func newOriginSet(csv string) originSet {
	s := originSet{}
	for _, o := range splitCSV(csv) {
		if o != "" {
			s[o] = struct{}{}
		}
	}
	return s
}

func splitCSV(s string) []string {
	var out []string
	start := 0
	for i, c := range s {
		if c == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// CORSHandler is the Java CORSHandler (outermost in the Jetty chain). With a
// missing Origin header it passes through untouched (Java: origin == null
// → return false). With an allowed Origin it injects the ACAO headers on
// every response and short-circuits OPTIONS with 200; with a disallowed
// Origin, OPTIONS → 403, other methods pass through without headers.
func CORSHandler(allowed originSet, next http.Handler) http.Handler {
	has := func(origin string) bool {
		_, ok := allowed[origin]
		return ok
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Not a CORS request (Java: return false).
			next.ServeHTTP(w, r)
			return
		}
		ok := has(origin)
		if ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, POST, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "86400") // cache for 24h
		}
		if r.Method == http.MethodOptions {
			if ok {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusForbidden)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

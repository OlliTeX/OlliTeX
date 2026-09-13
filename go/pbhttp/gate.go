package pbhttp

import (
	"net/http"
	"strings"
	"sync"
)

// ExpressNotFound emits the exact default 404 page Express (finalhandler)
// serves for unknown routes/methods: text/html; charset=utf-8 with the
// byte layout verified against the Node services. Shared by every Go service
// that must 404 like its Node counterpart (moved out of linked-url-proxy so
// the contract lives in one place).
func ExpressNotFound(w http.ResponseWriter, r *http.Request) {
	msg := strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;",
	).Replace("Cannot " + r.Method + " " + r.URL.RequestURI())
	body := "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">" +
		"\n<title>Error</title>\n</head>\n<body>\n<pre>" + msg + "</pre>\n</body>\n</html>\n"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(body))
}

// AuthGate mirrors the Node services' `app.use(requireServiceToken)`: the
// token check runs for EVERY request BEFORE routing (openPaths pass through
// unchecked), and any failure is answered with the exact Node 401 JSON
// `{"error":"Invalid or missing service token"}` — including requests for
// unknown paths, which Node's app-level middleware short-circuits. A valid
// (or absent-requirement) token falls through to the wrapped handler.
//
// The Node token is read from X-Service-Token or `Authorization: Bearer ...`
// and compared in constant time; openPaths is the Node per-service exemption
// list (dropboxinterface/datamanipulator exempt /health, webdavinterface
// exempts nothing).
func AuthGate(next http.Handler, expected string, warn func(), openPaths ...string) http.Handler {
	open := make(map[string]bool, len(openPaths))
	for _, p := range openPaths {
		open[p] = true
	}
	var warned sync.Once
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if expected == "" {
			warned.Do(func() { warn() })
			next.ServeHTTP(w, r)
			return
		}
		if open[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		cand := r.Header.Get("X-Service-Token")
		if cand == "" {
			if a := r.Header.Get("Authorization"); len(a) > 7 && strings.ToLower(a[:7]) == "bearer " {
				cand = strings.TrimSpace(a[7:])
			}
		}
		if !TimingSafeEqual(cand, expected) {
			WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid or missing service token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

package pbhttp

import (
	"bytes"
	"io"
	"net/http"
)

// LimitBodyWith bounds incoming request bodies to limit bytes, mirroring the
// express.json({ limit }) middleware the Node services use. express parses the
// JSON body eagerly and, when the body exceeds the limit, surfaces
// body-parser's entity.too.large error. What the client sees depends on the
// service's own error handling — some services let it fall through to Express'
// default (413), others catch everything and force 500. LimitBodyWith captures
// that: the caller chooses the overflow status + body text so each Go service
// matches its Node original exactly.
//
//	code  — HTTP status emitted on overflow (Node 413 or Node 500, per service)
//	text  — response body on overflow (Node "request entity too large" /
//	"Internal Server Error", per service)
//
// Only methods that carry a body (POST/PUT/PATCH/DELETE) are bounded, matching
// express.json. The (bounded) body is then replayed to the handler via a fresh
// reader so the route decodes it exactly once.
func LimitBodyWith(h http.Handler, limit int64, code int, text string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !methodHasBody(r.Method) || r.Body == nil {
			h.ServeHTTP(w, r)
			return
		}
		overflow := func() {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(code)
			_, _ = w.Write([]byte(text))
		}
		if r.ContentLength > limit {
			overflow()
			return
		}
		buf, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
		if err != nil {
			overflow()
			return
		}
		if int64(len(buf)) > limit {
			overflow()
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(buf))
		h.ServeHTTP(w, r)
	})
}

// LimitBody bounds bodies to limit bytes and responds 413 "payload too large"
// on overflow — the 1:1 behaviour of the dropbox/github/webdav/datamanipulator
// Node services (no global error handler, so Express' default 413 applies).
func LimitBody(h http.Handler, limit int64) http.Handler {
	return LimitBodyWith(h, limit, http.StatusRequestEntityTooLarge, "payload too large")
}

func methodHasBody(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

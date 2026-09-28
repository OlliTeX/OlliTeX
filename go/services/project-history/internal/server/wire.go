// Package server hosts the project-history HTTP surface (server.js wire spec).
package server

import (
	"encoding/json"
	"errors"
	"net/http"

	pherr "ollitex/go/services/project-history/internal/errors"
)

var (
	// NotFound → Express res.sendStatus(404) "404 Not Found".
	ErrNotFound = errors.New("not found")
)

func statusText(code int) string {
	switch code {
	case 200:
		return "OK"
	case 204:
		return "No Content"
	case 400:
		return "Bad Request"
	case 404:
		return "Not Found"
	case 409:
		return "Conflict"
	case 422:
		return "Not Accepted"
	case 423:
		return "Locked"
	case 429:
		return "Too Many Requests"
	default:
		return "Internal Server Error"
	}
}

// sendStatus mirrors Express res.sendStatus(code): STATUS_CODES body, text/html.
func sendStatus(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(statusText(code)))
}

func jsonBody(w http.ResponseWriter, code int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func textPlain(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write([]byte(body))
}

// Dispatch is the server.js error middleware (registered last).
func Dispatch(w http.ResponseWriter, r *http.Request, err error, fallback func() http.HandlerFunc) {
	_ = r
	var phErr *pherr.Error
	if errors.As(err, &phErr) {
		switch phErr.Kind {
		case pherr.KindNotFound:
			sendStatus(w, 404)
			return
		case pherr.KindBadRequest:
			sendStatus(w, 400)
			return
		case pherr.KindInconsistentChunk: // 422 (server.js L53-54)
			sendStatus(w, 422)
			return
		case pherr.KindSyncOngoing: // 409 (server.js L55-57)
			sendStatus(w, 409)
			return
		case pherr.KindTooManyRequests: // 429 + Retry-After (server.js L58)
			w.Header().Set("Retry-After", "300")
			w.WriteHeader(429)
			return
		}
		// OError message "Timeout" + info.key → 423 (server.js L60-66).
		if phErr.Timeout && phErr.Key != "" {
			jsonBody(w, 423, map[string]any{"message": "redis lock is taken"})
			return
		}
	}
	jsonBody(w, 500, map[string]any{"message": "an internal error occurred"})
}

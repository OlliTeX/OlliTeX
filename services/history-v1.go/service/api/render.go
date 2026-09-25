// render.go — Go port of services/history-v1/api/controllers/render.js +
// the app.js fallback 404 and terminal error handler.
//
// Node render.*: makeErrorRenderer(status)(res[, message]) sends
// { message: message || HTTPStatus[status] }. The http-status names:
//
//	400 Bad Request            404 Not Found
//	409 Conflict               413 Request Entity Too Large
//	422 Unprocessable Entity
//
// The terminal error handler (app.js) renders { message: err.message,
// error: {} } with the err.statusCode/err.status when 400<=s<600, else 500.
package api

import (
	"encoding/json"
	"log"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // Node res.json: no HTML escaping
	_ = enc.Encode(v)
}

// renderBadRequest — Node render.badRequest (400 {message: "Bad Request"}).
func renderBadRequest(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"message": "Bad Request"})
}

// renderNotFound — Node render.notFound (404 {message: "Not Found"}).
func renderNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
}

// renderUnprocessableEntity — Node render.unprocessableEntity
// (422 {message: "Unprocessable Entity"}).
func renderUnprocessableEntity(w http.ResponseWriter) {
	writeJSON(w, 422, map[string]string{"message": "Unprocessable Entity"})
}

// renderConflict — Node render.conflict (409 {message: "Conflict"}).
func renderConflict(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]string{"message": "Conflict"})
}

// renderRequestEntityTooLarge — Node render.requestEntityTooLarge
// (413 {message: "Request Entity Too Large"}).
func renderRequestEntityTooLarge(w http.ResponseWriter) {
	writeJSON(w, http.StatusRequestEntityTooLarge,
		map[string]string{"message": "Request Entity Too Large"})
}

// renderConflictMsg — Node render.conflict(res, message): a conflict 409 with
// a caller-supplied message. (e.g. createProjectBlob: "File hash mismatch").
func renderConflictMsg(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusConflict, map[string]string{"message": msg})
}

// renderTooLargeSize — Node copyProjectBlob's 413: { size: N } (a DIFFERENT
// body shape from the message-based render.requestEntityTooLarge).
func renderTooLargeSize(w http.ResponseWriter, size int) {
	writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"size": size})
}

// errorStatus — the status code the app.js terminal handler picks:
// err statusCode/status when 400<=s<600, else 500.
func errorStatus(err error) int {
	if s, ok := err.(*statusError); ok && s.code >= 400 && s.code < 600 {
		return s.code
	}
	if s := httpStatusCodeOf(err); s >= 400 && s < 600 {
		return s
	}
	return http.StatusInternalServerError
}

// statusError — HTTP status carried on a controller error (Node OError
// subclasses that set statusCode: Chunk NotFoundError -> 404, etc.).
type statusError struct {
	msg  string
	code int
}

func (e *statusError) Error() string { return e.msg }

// httpStatusCodeOf — best-effort statusCode lookup mirroring the Node
func httpStatusCodeOf(err error) int {
	switch e := err.(type) {
	case *statusError:
		return e.code
	case *authStatusError:
		return e.code
	case *NotPorted:
		return http.StatusNotImplemented
	default:
		return 0
	}
}

// authStatusError — the 401/403 shapes carry no body in the app.js handler
// beyond {message, error:{}}; we render them exactly like any other error
// (Node: the error handler renders err.message for auth failures too, since
// the auth middleware next()'d a plain Error with a statusCode + headers).
type authStatusError struct {
	code int
	msg  string
}

func (e *authStatusError) Error() string { return e.msg }

// handleAPIError — app.js terminal error handler (Go: no headersSent
// tracking needed; handlers fail before Writing anything once an error is
// returned).
func handleAPIError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	log.Printf("api error: %v", err)
	status := http.StatusInternalServerError
	msg := err.Error()
	if ae, ok := err.(*AuthError); ok {
		for k, v := range ae.Headers {
			w.Header().Set(k, v)
		}
		status = ae.Status
		msg = ae.Msg
	}
	code := httpStatusCodeOf(err)
	if s, ok := err.(*statusError); ok {
		code = s.code
		msg = s.msg
	} else if a, ok := err.(*authStatusError); ok {
		code = a.code
		msg = a.msg
	}
	if code >= 400 && code < 600 {
		status = code
	}
	writeJSON(w, status, map[string]any{"message": msg, "error": map[string]any{}})
}

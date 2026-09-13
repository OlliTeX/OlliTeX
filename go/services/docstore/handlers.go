package docstore

// handlers.go — routing, Express fallback responses, body limiting/parsing
// (express.json equivalent) and shared response helpers.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

const oopsMessage = "Oops, something went wrong"

// objectInput is a decoded request object (body or query) with its key
// appearance order — the order drives the strictObject "Unrecognized key(s)"
// issue rendering.
type objectInput struct {
	keys   []string
	fields map[string]any
}

func (o *objectInput) has(key string) bool {
	_, ok := o.fields[key]
	return ok
}

func (o *objectInput) str(key string) (string, bool) {
	s, ok := o.fields[key].(string)
	return s, ok
}

// routeSpec is one Express route (method + pattern) with its Go handler.
type routeSpec struct {
	method  string
	seg     []string
	handler func(ctx context.Context, w http.ResponseWriter, params routeParams, r *http.Request)
}

// Server holds the dependencies of the route handlers.
type Server struct {
	cfg   Config
	store Store
	arch  Archiver
	logf  func(format string, args ...any)
}

func NewServer(cfg Config, store Store, archiver Archiver) *Server {
	logf := cfg.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Server{cfg: cfg, store: store, arch: archiver, logf: logf}
}

// Router wires every route (HEAD mirrors GET, Express-style) and the Express
// fallback page for unknown paths and methods (verified live: 404 HTML
// "Cannot <METHOD> <path>" with the two security headers).
func (s *Server) Router() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.Method
		if method == "HEAD" {
			method = "GET"
			w = &headResponseWriter{ResponseWriter: w}
		}
		path := r.URL.EscapedPath()
		if len(path) > 1 {
			path = strings.TrimSuffix(path, "/") // Express non-strict slash match
		}
		seg := splitPath(path)

		matched := false
		for _, spec := range s.specs() {
			if len(spec.seg) != len(seg) || !matchSpec(spec.seg, seg) {
				continue
			}
			if spec.method != method {
				matched = true
				continue
			}
			if !matched {
				// first match in route order — Express dispatches the first
				// registered matching layer
				for _, sp := range s.specs() {
					if sp.method == method && len(sp.seg) == len(seg) && matchSpec(sp.seg, seg) {
						sp.handler(r.Context(), w, extractParams(sp.seg, seg), r)
						return
					}
				}
			}
			spec.handler(r.Context(), w, extractParams(spec.seg, seg), r)
			return
		}
		if matched {
			s.logf("docstore: no method for %s %s", r.Method, path)
		} else {
			s.logf("docstore: 404: %s %s", r.Method, path)
		}
		expressFallback(w, r.Method, path)
	})
}

// headResponseWriter drops response bodies (HEAD semantics) while keeping the
// status/headers the GET path would have written.
type headResponseWriter struct {
	http.ResponseWriter
	wrote bool
}

func (h *headResponseWriter) WriteHeader(code int) {
	h.wrote = true
	h.ResponseWriter.WriteHeader(code)
}

func (h *headResponseWriter) Write(b []byte) (int, error) {
	if !h.wrote {
		h.ResponseWriter.WriteHeader(http.StatusOK)
		h.wrote = true
	}
	return len(b), nil
}

func splitPath(p string) []string {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return []string{}
	}
	return strings.Split(p, "/")
}

func matchSpec(segs, seg []string) bool {
	for i, s1 := range segs {
		if strings.HasPrefix(s1, ":") {
			continue
		}
		if s1 != seg[i] {
			return false
		}
	}
	return true
}

func extractParams(segs, seg []string) routeParams {
	p := routeParams{}
	for i, s1 := range segs {
		if !strings.HasPrefix(s1, ":") {
			continue
		}
		v, err := url.PathUnescape(seg[i])
		if err != nil {
			v = seg[i]
		}
		switch s1 {
		case ":project_id":
			p.projectID = v
		case ":doc_id":
			p.docID = v
		}
	}
	return p
}

// ---- body reading (express.json equivalent) ----------------------------------

type bodyState int

const (
	bodyUndefined bodyState = iota // non-json Content-Type → fields undefined
	bodyObject                     // decoded object (possibly empty)
	bodyArray                      // top-level JSON array (body-parser allows)
)

// body is the parsed request body for the body-carrying routes.
type body struct {
	state bodyState
	obj   *objectInput
}

func emptyBody() body {
	return body{state: bodyObject, obj: &objectInput{fields: map[string]any{}}}
}

// readJSONBody reproduces express.json for the docstore service:
//
//   - body over the route limit → generic handler: 500 "Oops, something went
//     wrong" (text/html; verified text — chat's JSON error body does NOT
//     apply to this service).
//   - non-JSON Content-Type → all fields undefined (validation 400).
//   - empty body → {} (observed: "received undefined" field errors).
//   - invalid JSON / top-level scalar → 500 "Oops...".
//
// ok=false means the 500 response was already written.
func (s *Server) readJSONBody(w http.ResponseWriter, r *http.Request, limit int64) (b body, ok bool) {
	if !jsonContentType(r.Header.Get("Content-Type")) {
		return body{state: bodyUndefined}, true
	}
	if r.Body == nil {
		return emptyBody(), true
	}
	buf, _ := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if int64(len(buf)) > limit {
		s.logf("docstore: body over limit of %d bytes", limit)
		expressSendText(w, http.StatusInternalServerError, oopsMessage)
		return body{}, false
	}
	if len(strings.TrimSpace(string(buf))) == 0 {
		return emptyBody(), true
	}
	var probe any
	if err := json.Unmarshal(buf, &probe); err != nil {
		s.logf("docstore: invalid JSON body")
		expressSendText(w, http.StatusInternalServerError, oopsMessage)
		return body{}, false
	}
	switch v := probe.(type) {
	case map[string]any:
		if v == nil {
			return emptyBody(), true
		}
		return body{state: bodyObject, obj: &objectInput{
			keys:   sortedKeys(v),
			fields: v,
		}}, true
	case []any:
		return body{state: bodyArray}, true
	default:
		s.logf("docstore: body top-level %T rejected", probe)
		expressSendText(w, http.StatusInternalServerError, oopsMessage)
		return body{}, false
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// jsonContentType mirrors express body-parser's typeis 'json' match.
func jsonContentType(ct string) bool {
	mt := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	if mt == "" {
		return false
	}
	if i := strings.LastIndex(mt, "/"); i >= 0 {
		sub := mt[i+1:]
		return sub == "json" || strings.HasSuffix(sub, "+json")
	}
	return false
}

// ---- shared response helpers -------------------------------------------------

// writeJSON: res.json (application/json; charset=utf-8, no trailing newline).
// Encoded with the Node-parity writer (no HTML escaping; jobj keeps explicit
// key order).
func writeJSON(w http.ResponseWriter, code int, node any) {
	var sb strings.Builder
	writeJSONNode(&sb, node)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, sb.String())
}

// writeValidationError: handleValidationError 1:1 — 404 when any issue is
// rooted at params, else 400; all issues in schema order joined by "; ".
func writeValidationError(w http.ResponseWriter, issues []issue) {
	code := http.StatusBadRequest
	if hasParamsIssue(issues) {
		code = http.StatusNotFound
	}
	text := "Validation error"
	if len(issues) > 0 {
		rendered := make([]string, len(issues))
		for i, e := range issues {
			rendered[i] = e.text
		}
		text += ": " + strings.Join(rendered, "; ")
	}
	writeJSON(w, code, jobj{{"error", text}, {"statusCode", code}})
}

// mapError: app.js generic error handler — NotFoundError → 404 status text,
// DocModifiedError/DocVersionDecrementedError → 409 status text, anything
// else → 500 "Oops, something went wrong" (text/html).
func (s *Server) mapError(w http.ResponseWriter, err error) {
	switch err {
	case ErrNotFound:
		s.logf("docstore: not found")
		expressSendStatus(w, http.StatusNotFound)
	case ErrDocModified, ErrVersionDown:
		s.logf("docstore: conflict: %v", err)
		expressSendStatus(w, http.StatusConflict)
	default:
		s.logf("docstore: request errored: %v", err)
		expressSendText(w, http.StatusInternalServerError, oopsMessage)
	}
}

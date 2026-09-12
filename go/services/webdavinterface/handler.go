package webdavinterface

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	pbhttp "ollitex/go/pbhttp"
)

// --- HTTP server (1:1 with server.mjs routes) --------------------------------

// WebDAVHandlers bundles the 7 routes.
type WebDAVHandlers struct {
	Cfg WebDAVConfig
}

// Mux builds the http mux with all 1:1 routes.
func (h *WebDAVHandlers) Mux() http.Handler {
	cfg := h.Cfg
	cfg.withDefaults()

	warnFn := func() {
		fmt.Println("warn: SHARED_SERVICE_TOKEN is unset; accepting unauthenticated requests (should be restricted to in-container callers)")
	}
	auth := pbhttp.RequireServiceToken(cfg.ServiceToken, warnFn)

	mux := http.NewServeMux()
	mux.HandleFunc("/check", auth(h.check))
	mux.HandleFunc("/list", auth(h.list))
	mux.HandleFunc("/mkdir", auth(h.mkdir))
	mux.HandleFunc("/file", auth(h.file))
	mux.HandleFunc("/move", auth(h.move))
	// 1:1 with Node express.json({ limit: '50mb' }).
	return pbhttp.LimitBody(mux, 50<<20)
}

func bodyJSON(r *http.Request, v interface{}) {
	_ = json.NewDecoder(r.Body).Decode(v)
}

func (h *WebDAVHandlers) client(serverURL, username, password string) (*WebDAVClient, error) {
	return NewWebDAVClient(h.Cfg, serverURL, username, password)
}

func (h *WebDAVHandlers) check(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ServerURL string `json:"server_url"`
		Username  string `json:"username"`
		Password  string `json:"password"`
	}
	bodyJSON(r, &b)
	if b.ServerURL == "" || b.Username == "" || b.Password == "" {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": "Missing required fields: server_url, username, password"})
		return
	}
	c, err := h.client(b.ServerURL, b.Username, b.Password)
	if err != nil {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := c.Check(r.Context()); err != nil {
		st, msg := providerStatusError(webdavStatusOf(err), stringErr(err))
		pbhttp.WriteJSONErr(w, st, map[string]string{"error": msg})
		return
	}
	pbhttp.WriteJSON(w, 200, map[string]interface{}{"status": "ok", "message": "Connection successful"})
}

func (h *WebDAVHandlers) list(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ServerURL string `json:"server_url"`
		Username  string `json:"username"`
		Password  string `json:"password"`
		Path      string `json:"path"`
	}
	bodyJSON(r, &b)
	if b.ServerURL == "" || b.Username == "" || b.Password == "" || b.Path == "" {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": "Missing required fields"})
		return
	}
	c, err := h.client(b.ServerURL, b.Username, b.Password)
	if err != nil {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": err.Error()})
		return
	}
	entries, lerr := c.List(r.Context(), b.Path)
	if lerr != nil {
		st, msg := providerStatusError(webdavStatusOf(lerr), stringErr(lerr))
		pbhttp.WriteJSONErr(w, st, map[string]string{"error": msg})
		return
	}
	pbhttp.WriteJSON(w, 200, map[string]interface{}{"entries": entries})
}

func (h *WebDAVHandlers) mkdir(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ServerURL string `json:"server_url"`
		Username  string `json:"username"`
		Password  string `json:"password"`
		Path      string `json:"path"`
	}
	bodyJSON(r, &b)
	if b.ServerURL == "" || b.Username == "" || b.Password == "" || b.Path == "" {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": "Missing required fields"})
		return
	}
	c, err := h.client(b.ServerURL, b.Username, b.Password)
	if err != nil {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": err.Error()})
		return
	}
	created, cErr := c.CreateDirectory(r.Context(), b.Path)
	if cErr != nil {
		st, msg := providerStatusError(webdavStatusOf(cErr), stringErr(cErr))
		pbhttp.WriteJSONErr(w, st, map[string]string{"error": msg})
		return
	}
	if created {
		pbhttp.WriteJSON(w, 200, map[string]interface{}{"status": "ok", "created": true})
	} else {
		pbhttp.WriteJSON(w, 200, map[string]interface{}{"status": "ok", "created": false, "message": "Directory already exists"})
	}
}

func (h *WebDAVHandlers) move(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ServerURL string `json:"server_url"`
		Username  string `json:"username"`
		Password  string `json:"password"`
		Src       string `json:"src"`
		Dst       string `json:"dst"`
	}
	bodyJSON(r, &b)
	if b.ServerURL == "" || b.Username == "" || b.Password == "" || b.Src == "" || b.Dst == "" {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": "Missing required fields"})
		return
	}
	c, err := h.client(b.ServerURL, b.Username, b.Password)
	if err != nil {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if mErr := c.Move(r.Context(), b.Src, b.Dst); mErr != nil {
		st, msg := providerStatusError(webdavStatusOf(mErr), stringErr(mErr))
		pbhttp.WriteJSONErr(w, st, map[string]string{"error": msg})
		return
	}
	pbhttp.WriteJSON(w, 200, map[string]interface{}{"status": "ok", "moved": true})
}

// file handles GET/POST/DELETE /file (1:1).
func (h *WebDAVHandlers) file(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.fileGet(w, r)
	case http.MethodPost:
		h.filePost(w, r)
	case http.MethodDelete:
		h.fileDelete(w, r)
	default:
		pbhttp.WriteJSONErr(w, 405, map[string]string{"error": "method not allowed"})
	}
}

func basicPassword(authHeader string) string {
	if !strings.HasPrefix(authHeader, "Basic ") {
		return ""
	}
	token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Basic "))
	if token == "" {
		return ""
	}
	decoded, derr := base64.StdEncoding.DecodeString(token)
	if derr != nil {
		return ""
	}
	s := string(decoded)
	sep := strings.Index(s, ":")
	if sep == -1 {
		return ""
	}
	return s[sep+1:]
}

func (h *WebDAVHandlers) fileGet(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	serverURL := r.Header.Get("X-Server-Url")
	username := r.Header.Get("X-Username")
	if p == "" || serverURL == "" {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": "Missing required parameters"})
		return
	}
	password := basicPassword(r.Header.Get("Authorization"))
	if password == "" {
		pbhttp.WriteJSONErr(w, 401, map[string]string{"error": "Authentication required"})
		return
	}
	c, err := h.client(serverURL, username, password)
	if err != nil {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": err.Error()})
		return
	}
	b64, dErr := c.Download(r.Context(), p)
	if dErr != nil {
		if webdavStatusOf(dErr) == 404 {
			pbhttp.WriteJSONErr(w, 404, map[string]string{"error": "not found"})
			return
		}
		st, msg := providerStatusError(webdavStatusOf(dErr), stringErr(dErr))
		pbhttp.WriteJSONErr(w, st, map[string]string{"error": msg})
		return
	}
	pbhttp.WriteJSON(w, 200, map[string]interface{}{"path": p, "content_base64": b64})
}

func (h *WebDAVHandlers) filePost(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ServerURL     string  `json:"server_url"`
		Username      string  `json:"username"`
		Password      string  `json:"password"`
		Path          string  `json:"path"`
		ContentBase64 *string `json:"content_base64"`
		Etag          string  `json:"etag"`
	}
	bodyJSON(r, &b)
	if b.ServerURL == "" || b.Username == "" || b.Password == "" || b.Path == "" || b.ContentBase64 == nil {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": "Missing required fields"})
		return
	}
	c, err := h.client(b.ServerURL, b.Username, b.Password)
	if err != nil {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": err.Error()})
		return
	}
	etag := b.Etag
	if uErr := c.Upload(r.Context(), b.Path, *b.ContentBase64, etag); uErr != nil {
		st := webdavStatusOf(uErr)
		if st == 412 || reConflict.MatchString(stringErr(uErr)) {
			pbhttp.WriteJSONErr(w, 412, map[string]interface{}{"error": "ETag mismatch - file modified", "status": 412})
			return
		}
		if st == 404 {
			pbhttp.WriteJSONErr(w, 404, map[string]string{"error": "parent path not found"})
			return
		}
		mapped, msg := providerStatusError(st, stringErr(uErr))
		pbhttp.WriteJSONErr(w, mapped, map[string]string{"error": msg})
		return
	}
	pbhttp.WriteJSON(w, 200, map[string]interface{}{"status": "ok", "uploaded": true})
}

func (h *WebDAVHandlers) fileDelete(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	serverURL := r.Header.Get("X-Server-Url")
	username := r.Header.Get("X-Username")
	if p == "" || serverURL == "" {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": "Missing required parameters"})
		return
	}
	password := basicPassword(r.Header.Get("Authorization"))
	if password == "" {
		pbhttp.WriteJSONErr(w, 401, map[string]string{"error": "Authentication required"})
		return
	}
	c, err := h.client(serverURL, username, password)
	if err != nil {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": err.Error()})
		return
	}
	notFound, dErr := c.Delete(r.Context(), p)
	if dErr != nil {
		st := webdavStatusOf(dErr)
		if st == 404 || reNotFound.MatchString(stringErr(dErr)) {
			pbhttp.WriteJSON(w, 200, map[string]interface{}{"status": "ok", "notFound": true, "message": "File not found"})
			return
		}
		mapped, msg := providerStatusError(st, stringErr(dErr))
		pbhttp.WriteJSONErr(w, mapped, map[string]string{"error": msg})
		return
	}
	if notFound {
		pbhttp.WriteJSON(w, 200, map[string]interface{}{"status": "ok", "notFound": true, "message": "File not found"})
		return
	}
	pbhttp.WriteJSON(w, 200, map[string]interface{}{"status": "ok", "deleted": true})
}

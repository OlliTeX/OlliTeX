package dropboxinterface

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	pbhttp "ollitex/go/pbhttp"
)

// --- HTTP server (1:1 with dropbox server.mjs) -------------------------------

// DropboxHandlers bundles the 7 routes + /health.
type DropboxHandlers struct {
	Cfg DropboxConfig
}

// bodyJSON decodes a JSON request body (best-effort, ignoring errors).
func bodyJSON(r *http.Request, v interface{}) {
	_ = json.NewDecoder(r.Body).Decode(v)
}

// Mux builds the http mux (1:1 routes).
func (h *DropboxHandlers) Mux() http.Handler {
	cfg := h.Cfg
	cfg.withDefaults()
	warnFn := func() {
		fmt.Println("warn: SHARED_SERVICE_TOKEN is unset; accepting unauthenticated requests (should be restricted to in-container callers)")
	}
	auth := pbhttp.RequireServiceToken(cfg.ServiceToken, warnFn)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		pbhttp.WriteJSON(w, 200, map[string]interface{}{"status": "ok", "service": "dropboxinterface"})
	})
	mux.HandleFunc("/check", auth(h.check))
	mux.HandleFunc("/list", auth(h.list))
	mux.HandleFunc("/mkdir", auth(h.mkdir))
	mux.HandleFunc("/file", auth(h.file))
	mux.HandleFunc("/move", auth(h.move))
	// 1:1 with Node express.json({ limit: '50mb' }).
	return pbhttp.LimitBody(mux, 50<<20)
}

func (h *DropboxHandlers) client(accessToken string) (*DropboxClient, error) {
	return NewDropboxClient(h.Cfg, accessToken)
}

func dbxTokenFrom(r *http.Request, bodyToken string) string {
	if r.Header.Get("X-Access-Token") != "" {
		return r.Header.Get("X-Access-Token")
	}
	if bodyToken != "" {
		return bodyToken
	}
	if q := r.URL.Query().Get("access_token"); q != "" {
		return q
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func (h *DropboxHandlers) check(w http.ResponseWriter, r *http.Request) {
	var b struct {
		AccessToken string `json:"access_token"`
	}
	bodyJSON(r, &b)
	tok := dbxTokenFrom(r, b.AccessToken)
	if tok == "" {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": "Missing access token. Use body parameter or X-Access-Token header."})
		return
	}
	if verr := dbxValidateToken(tok); verr != nil {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": verr.Error()})
		return
	}
	c, _ := h.client(tok)
	if _, cerr := c.Check(r.Context()); cerr != nil {
		st := dropboxStatusOf(cerr)
		if st == 401 {
			pbhttp.WriteJSONErr(w, 401, map[string]interface{}{"error": "Invalid or expired access token", "statusCode": 401})
			return
		}
		pbhttp.WriteJSONErr(w, st, map[string]interface{}{"error": dbxOrDefault(safeProviderError(cerr.Error(), tok), "Connection failed"), "statusCode": st})
		return
	}
	pbhttp.WriteJSON(w, 200, map[string]string{"status": "ok", "message": "Connection successful"})
}

func (h *DropboxHandlers) list(w http.ResponseWriter, r *http.Request) {
	var b struct {
		AccessToken string `json:"access_token"`
		Path        string `json:"path"`
		Recursive   bool   `json:"recursive"`
	}
	bodyJSON(r, &b)
	tok := dbxTokenFrom(r, b.AccessToken)
	if tok == "" {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": "Missing access token"})
		return
	}
	c, _ := h.client(tok)
	result, lerr := c.List(r.Context(), b.Path, b.Recursive)
	if lerr != nil {
		st := dropboxStatusOf(lerr)
		switch st {
		case 401:
			pbhttp.WriteJSONErr(w, 401, map[string]string{"error": "Invalid token"})
		case 403:
			pbhttp.WriteJSONErr(w, 403, map[string]string{"error": "Permission denied"})
		case 404:
			pbhttp.WriteJSONErr(w, 404, map[string]string{"error": "Path not found"})
		default:
			pbhttp.WriteJSONErr(w, st, map[string]string{"error": dbxOrDefault(safeProviderError(lerr.Error(), tok), "List failed")})
		}
		return
	}
	pbhttp.WriteJSON(w, 200, result)
}

func (h *DropboxHandlers) mkdir(w http.ResponseWriter, r *http.Request) {
	var b struct {
		AccessToken string `json:"access_token"`
		Path        string `json:"path"`
	}
	bodyJSON(r, &b)
	tok := dbxTokenFrom(r, b.AccessToken)
	if tok == "" || b.Path == "" {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": "Missing required fields"})
		return
	}
	c, _ := h.client(tok)
	created, cerr := c.CreateDirectory(r.Context(), b.Path)
	if cerr != nil {
		st := dropboxStatusOf(cerr)
		if st == 409 {
			pbhttp.WriteJSON(w, 200, map[string]interface{}{"status": "ok", "created": false, "message": "Directory already exists"})
			return
		}
		if st == 401 {
			pbhttp.WriteJSONErr(w, 401, map[string]string{"error": "Invalid token"})
			return
		}
		pbhttp.WriteJSONErr(w, st, map[string]string{"error": dbxOrDefault(safeProviderError(cerr.Error(), tok), "Create directory failed")})
		return
	}
	if created {
		pbhttp.WriteJSON(w, 200, map[string]string{"status": "ok", "created": "true"})
	}
}

func (h *DropboxHandlers) move(w http.ResponseWriter, r *http.Request) {
	var b struct {
		AccessToken string `json:"access_token"`
		Src         string `json:"src"`
		Dst         string `json:"dst"`
	}
	bodyJSON(r, &b)
	tok := dbxTokenFrom(r, b.AccessToken)
	if tok == "" || b.Src == "" || b.Dst == "" {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": "Missing required fields"})
		return
	}
	c, _ := h.client(tok)
	result, merr := c.Move(r.Context(), b.Src, b.Dst)
	if merr != nil {
		st := dropboxStatusOf(merr)
		if st == 409 {
			pbhttp.WriteJSONErr(w, 409, map[string]interface{}{"error": "Conflict detected", "message": safeProviderError(merr.Error(), tok)})
			return
		}
		if st == 401 {
			pbhttp.WriteJSONErr(w, 401, map[string]string{"error": "Invalid token"})
			return
		}
		pbhttp.WriteJSONErr(w, st, map[string]string{"error": dbxOrDefault(safeProviderError(merr.Error(), tok), "Move failed")})
		return
	}
	pbhttp.WriteJSON(w, 200, result)
}

// file handles GET/POST/DELETE /file (1:1).
func (h *DropboxHandlers) file(w http.ResponseWriter, r *http.Request) {
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

func (h *DropboxHandlers) fileGet(w http.ResponseWriter, r *http.Request) {
	p := r.Header.Get("X-Path")
	if p == "" {
		p = r.URL.Query().Get("path")
	}
	b := struct {
		AccessToken string `json:"access_token"`
	}{}
	bodyJSON(r, &b)
	tok := dbxTokenFrom(r, b.AccessToken)
	if tok == "" || p == "" {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": "Missing required parameters"})
		return
	}
	c, _ := h.client(tok)
	b64, notFound, derr := c.Download(r.Context(), p)
	if derr != nil {
		st := dropboxStatusOf(derr)
		if st == 401 {
			pbhttp.WriteJSONErr(w, 401, map[string]string{"error": "Invalid token"})
			return
		}
		if st == 404 {
			pbhttp.WriteJSONErr(w, 404, map[string]string{"error": "File not found"})
			return
		}
		pbhttp.WriteJSONErr(w, st, map[string]string{"error": dbxOrDefault(safeProviderError(derr.Error(), tok), "Download failed")})
		return
	}
	if notFound {
		pbhttp.WriteJSONErr(w, 404, map[string]string{"error": "File not found"})
		return
	}
	rel := strings.TrimPrefix(p, "/")
	pbhttp.WriteJSON(w, 200, map[string]interface{}{"relative_path": rel, "content_base64": b64})
}

func (h *DropboxHandlers) filePost(w http.ResponseWriter, r *http.Request) {
	var b struct {
		AccessToken   string  `json:"access_token"`
		Path          string  `json:"path"`
		ContentBase64 *string `json:"content_base64"`
		Mode          string  `json:"mode"`
		Rev           string  `json:"rev"`
	}
	bodyJSON(r, &b)
	tok := dbxTokenFrom(r, b.AccessToken)
	if tok == "" || b.Path == "" || b.ContentBase64 == nil {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": "Missing required fields"})
		return
	}
	mode := "overwrite"
	if b.Mode != "" {
		mode = b.Mode
	}
	c, _ := h.client(tok)
	effMode := mode
	if b.Rev != "" {
		effMode = "update"
	}
	result, uerr := c.Upload(r.Context(), b.Path, *b.ContentBase64, effMode, b.Rev)
	if uerr != nil {
		st := dropboxStatusOf(uerr)
		if strings.Contains(uerr.Error(), "conflict") || st == 412 {
			pbhttp.WriteJSONErr(w, 412, map[string]interface{}{"error": "Upload conflict: File modified on server", "status": 412})
			return
		}
		if st == 401 {
			pbhttp.WriteJSONErr(w, 401, map[string]string{"error": "Invalid token"})
			return
		}
		pbhttp.WriteJSONErr(w, st, map[string]string{"error": dbxOrDefault(safeProviderError(uerr.Error(), tok), "Upload failed")})
		return
	}
	pbhttp.WriteJSON(w, 200, map[string]interface{}{"status": "ok", "uploaded": true, "revision": result["revision"], "dropbox_id": result["dropbox_id"]})
}

func (h *DropboxHandlers) fileDelete(w http.ResponseWriter, r *http.Request) {
	p := r.Header.Get("X-Path")
	if p == "" {
		p = r.URL.Query().Get("path")
	}
	b := struct {
		AccessToken string `json:"access_token"`
	}{}
	bodyJSON(r, &b)
	tok := dbxTokenFrom(r, b.AccessToken)
	if tok == "" || p == "" {
		pbhttp.WriteJSONErr(w, 400, map[string]string{"error": "Missing required parameters"})
		return
	}
	c, _ := h.client(tok)
	notFound, derr := c.Delete(r.Context(), p)
	if derr != nil {
		st := dropboxStatusOf(derr)
		if st == 401 {
			pbhttp.WriteJSONErr(w, 401, map[string]string{"error": "Invalid token"})
			return
		}
		if st == 404 || notFound {
			pbhttp.WriteJSON(w, 200, map[string]interface{}{"status": "ok", "notFound": true, "message": "File already removed or never existed"})
			return
		}
		pbhttp.WriteJSONErr(w, st, map[string]string{"error": dbxOrDefault(safeProviderError(derr.Error(), tok), "Delete failed")})
		return
	}
	pbhttp.WriteJSON(w, 200, map[string]interface{}{"status": "ok", "deleted": true})
}

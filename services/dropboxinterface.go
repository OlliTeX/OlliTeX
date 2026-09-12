package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// DropboxError carries a mapped HTTP status + message (1:1 with the Node
// customError { statusCode, message, dropboxErrorCode }).
type DropboxError struct {
	StatusCode       int
	Message          string
	DropboxErrorCode string
	Err              error
}

func (e *DropboxError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "dropbox error"
}
func (e *DropboxError) Unwrap() error { return e.Err }

func dropboxStatus(code int, msg, dbxEcode string) *DropboxError {
	return &DropboxError{StatusCode: code, Message: msg, DropboxErrorCode: dbxEcode}
}

func dropboxStatusOf(err error) int {
	var de *DropboxError
	if errors.As(err, &de) {
		return de.StatusCode
	}
	return 0
}

// DropboxEntry is a normalized list entry (1:1 with Node list()).
type DropboxEntry struct {
	RelativePath string  `json:"relative_path"`
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Size         int64   `json:"size"`
	Binary       bool    `json:"binary"`
	Checksum     *string `json:"checksum"`
	Hash         *string `json:"hash"`
	ContentHash  *string `json:"content_hash"`
	Mtime        string  `json:"mtime"`
	DropboxID    string  `json:"dropbox_id"`
	Rev          *string `json:"rev"`
}

// DropboxConfig holds the client runtime settings.
type DropboxConfig struct {
	ServiceToken string       // SHARED_SERVICE_TOKEN ("" = legacy permissive)
	APIBase      string       // default https://api.dropbox.com/2
	ContentBase  string       // default https://content.dropboxapi.com/2
	HTTPClient   *http.Client // upstream transport (tests inject a fake)
}

func (c *DropboxConfig) withDefaults() {
	if c.APIBase == "" {
		c.APIBase = "https://api.dropbox.com/2"
	}
	if c.ContentBase == "" {
		c.ContentBase = "https://content.dropboxapi.com/2"
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{}
	}
}

// --- token helpers (1:1 with auth.mjs) ---------------------------------------

// dbxValidateToken mirrors Node validateToken (non-empty string; warns if not
// an sl. token but still accepts).
func dbxValidateToken(token string) error {
	if token == "" {
		return errors.New("Missing or invalid access token")
	}
	return nil
}

// dbxIsTokenFormat is 1:1 with DropboxClient.isValidToken (sl. or dp. prefix).
func dbxIsTokenFormat(token string) bool {
	return strings.HasPrefix(token, "sl.") || strings.HasPrefix(token, "dp.")
}

// dbxSanitizeTokenForLogging shows first 10 + last 4 chars (1:1).
func dbxSanitizeTokenForLogging(token string) string {
	if token == "" {
		return "[none]"
	}
	if len(token) <= 14 {
		return "[hidden]"
	}
	return token[:10] + "..." + token[len(token)-4:]
}

// safeProviderError redacts a token value and access_token= patterns for
// logs/responses (1:1 with server.mjs).
func safeProviderError(msg, token string) string {
	if token != "" {
		msg = strings.ReplaceAll(msg, token, "<redacted-token>")
	}
	msg = dbxTokenLE.ReplaceAllString(msg, "access_token=<redacted>")
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return msg
}

var dbxTokenLE = regexp.MustCompile(`(?i)access[_-]?token[=:'" ]+[A-Za-z0-9._~+-]+`)

// --- Dropbox API v2 client (1:1 with DropboxClient.mjs) ---------------------

// DropboxClient performs Dropbox API v2 operations.
type DropboxClient struct {
	Cfg         DropboxConfig
	AccessToken string
}

func NewDropboxClient(cfg DropboxConfig, accessToken string) (*DropboxClient, error) {
	if accessToken == "" {
		return nil, errors.New("Missing or invalid access token")
	}
	cfg.withDefaults()
	return &DropboxClient{Cfg: cfg, AccessToken: accessToken}, nil
}

// dbxCall performs a JSON API call and returns the body + status.
func (c *DropboxClient) dbxCall(ctx context.Context, url string, header http.Header, body []byte) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	if header != nil {
		for k, vs := range header {
			for _, v := range vs {
				req.Header.Set(k, v)
			}
		}
	}
	if body == nil && rd == nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Cfg.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return resp.StatusCode, b, rerr
	}
	return resp.StatusCode, b, nil
}

// apiError parses a Dropbox error body ({"error":{...}}) and returns a
// classified *DropboxError via mapDropboxError.
func (c *DropboxClient) apiError(status int, body []byte) *DropboxError {
	var env struct {
		Error *dropboxErrorDetails `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Error != nil {
		code, msg, ecode := mapDropboxError(status, env.Error)
		return dropboxStatus(code, msg, ecode)
	}
	code, msg, ecode := mapDropboxError(status, nil)
	return dropboxStatus(code, msg, ecode)
}

// Check verifies auth (1:1).
func (c *DropboxClient) Check(ctx context.Context) (string, error) {
	status, body, err := c.dbxCall(ctx, c.Cfg.APIBase+"/users/get_current_account", nil, nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", c.apiError(status, body)
	}
	var r struct {
		AccountID string `json:"account_id"`
	}
	if jerr := json.Unmarshal(body, &r); jerr != nil {
		return "", jerr
	}
	return r.AccountID, nil
}

// dbxListEntry is one Dropbox file entry from list_folder.
type dbxListEntry struct {
	Name           string  `json:"name"`
	PathDisplay    string  `json:"path_display"`
	Tag            string  `json:".tag"`
	Size           int64   `json:"size"`
	Hash           *string `json:"hash"`
	ContentHash    *string `json:"content_hash"`
	ServerModified string  `json:"server_modified"`
	ID             string  `json:"id"`
	Rev            *string `json:"rev"`
}

// List returns the entries for a path with cursor pagination (1:1).
func (c *DropboxClient) List(ctx context.Context, p string, recursive bool) (map[string]interface{}, error) {
	cursor := ""
	all := []DropboxEntry{}
	for {
		reqBody := map[string]interface{}{
			"path":               p,
			"recursive":          recursive,
			"include_media_info": false,
			"include_deleted":    false,
			"limit":              1000,
		}
		if cursor != "" {
			reqBody["cursor"] = cursor
		} else {
			reqBody["cursor"] = nil
		}
		b, jerr := json.Marshal(reqBody)
		if jerr != nil {
			return nil, jerr
		}
		status, body, err := c.dbxCall(ctx, c.Cfg.APIBase+"/files/list_folder", nil, b)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, c.apiError(status, body)
		}
		var r struct {
			Entries []dbxListEntry `json:"entries"`
			Cursor  string         `json:"cursor"`
			HasMore bool           `json:"has_more"`
		}
		if jerr := json.Unmarshal(body, &r); jerr != nil {
			return nil, jerr
		}
		for _, e := range r.Entries {
			rel := strings.TrimPrefix(e.PathDisplay, "/")
			if rel == "" {
				rel = e.Name
			}
			typ := "file"
			if e.Tag == "folder" {
				typ = "folder"
			}
			var mtime string
			if e.ServerModified != "" {
				if t, perr := parseISO8601(e.ServerModified); perr == nil {
					mtime = t.UTC().Format("2006-01-02T15:04:05.000Z")
				}
			}
			all = append(all, DropboxEntry{
				RelativePath: rel,
				Name:         e.Name,
				Type:         typ,
				Size:         e.Size,
				Binary:       true,
				Checksum:     nil,
				Hash:         e.Hash,
				ContentHash:  e.ContentHash,
				Mtime:        mtime,
				DropboxID:    e.ID,
				Rev:          e.Rev,
			})
		}
		if !r.HasMore {
			break
		}
		cursor = r.Cursor
	}
	return map[string]interface{}{"entries": all, "has_more": false}, nil
}

// Download returns base64 content; 404 -> notFound (1:1).
func (c *DropboxClient) Download(ctx context.Context, p string) (string, bool, error) {
	arg, _ := json.Marshal(map[string]string{"path": p})
	h := http.Header{}
	h.Set("Dropbox-API-Arg", string(arg))
	h.Set("Content-Type", "application/octet-stream")
	status, body, err := c.dbxCall(ctx, c.Cfg.ContentBase+"/files/download", h, nil)
	if err != nil {
		return "", false, err
	}
	if status < 200 || status >= 300 {
		if st := dropboxStatusOf(c.apiError(status, body)); st == 404 {
			return "", true, nil
		}
		return "", false, c.apiError(status, body)
	}
	return base64.StdEncoding.EncodeToString(body), false, nil
}

// Upload creates/updates a file (1:1, incl. mode/rev + mute; conflict -> 412).
func (c *DropboxClient) Upload(ctx context.Context, p, contentBase64 string, mode string, rev string) (map[string]interface{}, error) {
	content, err := base64.StdEncoding.DecodeString(contentBase64)
	if err != nil {
		return nil, err
	}
	arg := map[string]interface{}{
		"path": p,
		"mode": mode,
		"mute": true,
	}
	if mode == "update" && rev != "" {
		arg["mode"] = map[string]interface{}{"update": rev}
	}
	argJSON, _ := json.Marshal(arg)
	h := http.Header{}
	h.Set("Dropbox-API-Arg", string(argJSON))
	h.Set("Content-Type", "application/octet-stream")
	status, body, err := c.dbxCall(ctx, c.Cfg.ContentBase+"/files/upload", h, content)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, c.apiError(status, body)
	}
	var r struct {
		Rev string `json:"rev"`
		ID  string `json:"id"`
	}
	if jerr := json.Unmarshal(body, &r); jerr != nil {
		return nil, jerr
	}
	return map[string]interface{}{"success": true, "revision": r.Rev, "dropbox_id": r.ID}, nil
}

// Delete removes a path; 404 -> notFound (1:1).
func (c *DropboxClient) Delete(ctx context.Context, p string) (bool, error) {
	b, _ := json.Marshal(map[string]string{"path": p})
	status, body, err := c.dbxCall(ctx, c.Cfg.APIBase+"/files/delete_v2", nil, b)
	if err != nil {
		return false, err
	}
	if status < 200 || status >= 300 {
		if st := dropboxStatusOf(c.apiError(status, body)); st == 404 {
			return true, nil
		}
		return false, c.apiError(status, body)
	}
	return false, nil
}

// CreateDirectory creates a folder; 409 -> created=false (1:1).
func (c *DropboxClient) CreateDirectory(ctx context.Context, p string) (bool, error) {
	b, _ := json.Marshal(map[string]string{"path": p})
	status, body, err := c.dbxCall(ctx, c.Cfg.APIBase+"/files/create_folder_v2", nil, b)
	if err != nil {
		return false, err
	}
	if status < 200 || status >= 300 {
		if st := dropboxStatusOf(c.apiError(status, body)); st == 409 {
			return false, nil
		}
		return false, c.apiError(status, body)
	}
	return true, nil
}

// Move renames/moves a path (1:1).
func (c *DropboxClient) Move(ctx context.Context, src, dst string) (map[string]interface{}, error) {
	b, _ := json.Marshal(map[string]string{"from_path": src, "to_path": dst})
	status, body, err := c.dbxCall(ctx, c.Cfg.APIBase+"/files/move_v2", nil, b)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, c.apiError(status, body)
	}
	var r struct {
		Metadata struct {
			PathDisplay string `json:"path_display"`
		} `json:"metadata"`
	}
	if jerr := json.Unmarshal(body, &r); jerr != nil {
		return nil, jerr
	}
	return map[string]interface{}{"success": true, "old_path": src, "new_path": r.Metadata.PathDisplay}, nil
}

// GetMetadata returns file metadata (1:1).
func (c *DropboxClient) GetMetadata(ctx context.Context, p string) (map[string]interface{}, error) {
	b, _ := json.Marshal(map[string]interface{}{"path": p, "include_deleted": false})
	status, body, err := c.dbxCall(ctx, c.Cfg.APIBase+"/files/get_metadata", nil, b)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, c.apiError(status, body)
	}
	var r struct {
		PathDisplay    string  `json:"path_display"`
		Name           string  `json:"name"`
		Tag            string  `json:".tag"`
		Size           int64   `json:"size"`
		Rev            *string `json:"rev"`
		ID             string  `json:"id"`
		ServerModified string  `json:"server_modified"`
	}
	if jerr := json.Unmarshal(body, &r); jerr != nil {
		return nil, jerr
	}
	typ := "file"
	if r.Tag == "folder" {
		typ = "folder"
	}
	var mtime string
	if r.ServerModified != "" {
		if t, perr := parseISO8601(r.ServerModified); perr == nil {
			mtime = t.UTC().Format("2006-01-02T15:04:05.000Z")
		}
	}
	return map[string]interface{}{
		"path_display": r.PathDisplay,
		"name":         r.Name,
		"type":         typ,
		"size":         r.Size,
		"rev":          r.Rev,
		"dropbox_id":   r.ID,
		"mtime":        mtime,
	}, nil
}

// --- Dropbox error mapping (1:1 with _mapDropboxError) ----------------------

type dropboxErrorDetails struct {
	AccessToken *string                   `json:"access_token"`
	Path        *dropboxPathError         `json:"path"`
	RateLimit   *struct{ Summary string } `json:"rate_limit"`
	Conflict    *struct{ Summary string } `json:"conflict"`
	Folder      *struct{ Summary string } `json:"folder"`
	Summary     *string                   `json:"summary"`
}

type dropboxPathError struct {
	Tag     string  `json:".tag"`
	Summary *string `json:"summary"`
}

func mapDropboxError(status int, d *dropboxErrorDetails) (int, string, string) {
	code, msg := 500, "Unknown Dropbox error"
	var summary string
	if d != nil && d.Summary != nil {
		summary = *d.Summary
	}
	pathNotFound := d != nil && d.Path != nil && d.Path.Tag == "not_found"

	invalidToken := d != nil && d.AccessToken != nil && strings.Contains(*d.AccessToken, "invalid_access_token")
	if status == 401 || invalidToken {
		code, msg = 401, "Invalid or expired access token"
	} else if status == 429 || strings.Contains(summary, "rate_limit_exceeded") {
		code, msg = 429, "Rate limit exceeded. Please wait and try again."
	} else if status == 403 {
		code, msg = 403, "Permission denied"
	} else if status == 404 || pathNotFound {
		code, msg = 404, "File or folder not found"
	} else if status == 409 {
		code = 409
		conflict := ""
		if d != nil && d.Conflict != nil {
			conflict = d.Conflict.Summary
		}
		if strings.Contains(conflict, "different_file") || strings.Contains(conflict, "same_file") {
			msg = "File conflict detected"
		} else {
			msg = "Conflict: " + (summaryIf(summary, "Unknown Dropbox error"))
		}
	} else if status >= 500 {
		code, msg = 503, "Dropbox service temporarily unavailable"
	}
	return code, msg, summary
}

func summaryIf(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// parseISO8601 parses an RFC3339/ISO-8601 timestamp (Dropbox server_modified).
func parseISO8601(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

// dbxOrDefault returns s if non-empty, else def (Node's `x || 'default'`).
func dbxOrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// --- HTTP server (1:1 with dropbox server.mjs) -------------------------------

// DropboxHandlers bundles the 7 routes + /health.
type DropboxHandlers struct {
	Cfg DropboxConfig
}

// Mux builds the http mux (1:1 routes).
func (h *DropboxHandlers) Mux() http.Handler {
	cfg := h.Cfg
	cfg.withDefaults()
	warnFn := func() {
		fmt.Println("warn: SHARED_SERVICE_TOKEN is unset; accepting unauthenticated requests (should be restricted to in-container callers)")
	}
	auth := requireServiceToken(cfg.ServiceToken, warnFn)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{"status": "ok", "service": "dropboxinterface"})
	})
	mux.HandleFunc("/check", auth(h.check))
	mux.HandleFunc("/list", auth(h.list))
	mux.HandleFunc("/mkdir", auth(h.mkdir))
	mux.HandleFunc("/file", auth(h.file))
	mux.HandleFunc("/move", auth(h.move))
	return mux
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
		writeJSONErr(w, 400, map[string]string{"error": "Missing access token. Use body parameter or X-Access-Token header."})
		return
	}
	if verr := dbxValidateToken(tok); verr != nil {
		writeJSONErr(w, 400, map[string]string{"error": verr.Error()})
		return
	}
	c, _ := h.client(tok)
	if _, cerr := c.Check(r.Context()); cerr != nil {
		st := dropboxStatusOf(cerr)
		if st == 401 {
			writeJSONErr(w, 401, map[string]interface{}{"error": "Invalid or expired access token", "statusCode": 401})
			return
		}
		writeJSONErr(w, st, map[string]interface{}{"error": dbxOrDefault(safeProviderError(cerr.Error(), tok), "Connection failed"), "statusCode": st})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok", "message": "Connection successful"})
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
		writeJSONErr(w, 400, map[string]string{"error": "Missing access token"})
		return
	}
	c, _ := h.client(tok)
	result, lerr := c.List(r.Context(), b.Path, b.Recursive)
	if lerr != nil {
		st := dropboxStatusOf(lerr)
		switch st {
		case 401:
			writeJSONErr(w, 401, map[string]string{"error": "Invalid token"})
		case 403:
			writeJSONErr(w, 403, map[string]string{"error": "Permission denied"})
		case 404:
			writeJSONErr(w, 404, map[string]string{"error": "Path not found"})
		default:
			writeJSONErr(w, st, map[string]string{"error": dbxOrDefault(safeProviderError(lerr.Error(), tok), "List failed")})
		}
		return
	}
	writeJSON(w, 200, result)
}

func (h *DropboxHandlers) mkdir(w http.ResponseWriter, r *http.Request) {
	var b struct {
		AccessToken string `json:"access_token"`
		Path        string `json:"path"`
	}
	bodyJSON(r, &b)
	tok := dbxTokenFrom(r, b.AccessToken)
	if tok == "" || b.Path == "" {
		writeJSONErr(w, 400, map[string]string{"error": "Missing required fields"})
		return
	}
	c, _ := h.client(tok)
	created, cerr := c.CreateDirectory(r.Context(), b.Path)
	if cerr != nil {
		st := dropboxStatusOf(cerr)
		if st == 409 {
			writeJSON(w, 200, map[string]interface{}{"status": "ok", "created": false, "message": "Directory already exists"})
			return
		}
		if st == 401 {
			writeJSONErr(w, 401, map[string]string{"error": "Invalid token"})
			return
		}
		writeJSONErr(w, st, map[string]string{"error": dbxOrDefault(safeProviderError(cerr.Error(), tok), "Create directory failed")})
		return
	}
	if created {
		writeJSON(w, 200, map[string]string{"status": "ok", "created": "true"})
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
		writeJSONErr(w, 400, map[string]string{"error": "Missing required fields"})
		return
	}
	c, _ := h.client(tok)
	result, merr := c.Move(r.Context(), b.Src, b.Dst)
	if merr != nil {
		st := dropboxStatusOf(merr)
		if st == 409 {
			writeJSONErr(w, 409, map[string]interface{}{"error": "Conflict detected", "message": safeProviderError(merr.Error(), tok)})
			return
		}
		if st == 401 {
			writeJSONErr(w, 401, map[string]string{"error": "Invalid token"})
			return
		}
		writeJSONErr(w, st, map[string]string{"error": dbxOrDefault(safeProviderError(merr.Error(), tok), "Move failed")})
		return
	}
	writeJSON(w, 200, result)
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
		writeJSONErr(w, 405, map[string]string{"error": "method not allowed"})
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
		writeJSONErr(w, 400, map[string]string{"error": "Missing required parameters"})
		return
	}
	c, _ := h.client(tok)
	b64, notFound, derr := c.Download(r.Context(), p)
	if derr != nil {
		st := dropboxStatusOf(derr)
		if st == 401 {
			writeJSONErr(w, 401, map[string]string{"error": "Invalid token"})
			return
		}
		if st == 404 {
			writeJSONErr(w, 404, map[string]string{"error": "File not found"})
			return
		}
		writeJSONErr(w, st, map[string]string{"error": dbxOrDefault(safeProviderError(derr.Error(), tok), "Download failed")})
		return
	}
	if notFound {
		writeJSONErr(w, 404, map[string]string{"error": "File not found"})
		return
	}
	rel := strings.TrimPrefix(p, "/")
	writeJSON(w, 200, map[string]interface{}{"relative_path": rel, "content_base64": b64})
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
		writeJSONErr(w, 400, map[string]string{"error": "Missing required fields"})
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
			writeJSONErr(w, 412, map[string]interface{}{"error": "Upload conflict: File modified on server", "status": 412})
			return
		}
		if st == 401 {
			writeJSONErr(w, 401, map[string]string{"error": "Invalid token"})
			return
		}
		writeJSONErr(w, st, map[string]string{"error": dbxOrDefault(safeProviderError(uerr.Error(), tok), "Upload failed")})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"status": "ok", "uploaded": true, "revision": result["revision"], "dropbox_id": result["dropbox_id"]})
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
		writeJSONErr(w, 400, map[string]string{"error": "Missing required parameters"})
		return
	}
	c, _ := h.client(tok)
	notFound, derr := c.Delete(r.Context(), p)
	if derr != nil {
		st := dropboxStatusOf(derr)
		if st == 401 {
			writeJSONErr(w, 401, map[string]string{"error": "Invalid token"})
			return
		}
		if st == 404 || notFound {
			writeJSON(w, 200, map[string]interface{}{"status": "ok", "notFound": true, "message": "File already removed or never existed"})
			return
		}
		writeJSONErr(w, st, map[string]string{"error": dbxOrDefault(safeProviderError(derr.Error(), tok), "Delete failed")})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"status": "ok", "deleted": true})
}

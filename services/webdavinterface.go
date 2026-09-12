package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WebDAV error carrying a provider status code (1:1 with the Node client
// errors tagged with `error.status`).
type WebDAVError struct {
	Status int
	Msg    string
	Err    error
}

func (e *WebDAVError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "webdav error"
}

func (e *WebDAVError) Unwrap() error { return e.Err }

func webdavStatus(status int, msg string) *WebDAVError { return &WebDAVError{Status: status, Msg: msg} }

// WebDAVEntry is the normalized directory entry returned by List/Check.
// Field-for-field with the Node WebDAVClient.list() mapping.
type WebDAVEntry struct {
	Href        string `json:"href"`
	Path        string `json:"path"`
	IsDirectory bool   `json:"isDirectory"`
	Etag        string `json:"etag"`
	ModifiedAt  string `json:"modifiedAt"` // ISO-8601 UTC or ""
	Size        int64  `json:"size"`
}

// WebDAVConfig holds the service/client runtime settings.
type WebDAVConfig struct {
	ServiceToken string       // SHARED_SERVICE_TOKEN ("" = legacy permissive, warn once)
	MaxRetries   int          // default 2
	RetryDelayMs int          // default 100
	HTTPClient   *http.Client // upstream transport (tests inject a fake)

	Sleep func(time.Duration) // backoff sleeper (tests make it instant); nil sleeps real
}

func (c *WebDAVConfig) withDefaults() {
	if c.MaxRetries == 0 {
		c.MaxRetries = 2
	}
	if c.RetryDelayMs == 0 {
		c.RetryDelayMs = 100
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if c.Sleep == nil {
		c.Sleep = time.Sleep
	}
}

// --- error sanitization helpers (1:1 with server.mjs H8/M9) ------------------

var credentialURLRe = regexp.MustCompile(`(?i)(https?|webdav)://[^@\s]+@`)

// safeError redacts credential-bearing URLs for LOGS (1:1 with Node safeError).
func safeError(err error) string {
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}
	if len(msg) > 1000 {
		msg = msg[:1000]
	}
	return credentialURLRe.ReplaceAllString(msg, "$1://<redacted>@")
}

var (
	reAuth     = regexp.MustCompile(`(?i)unauthorized|authentication|invalid (token|credential)`)
	reNotFound = regexp.MustCompile(`(?i)not found|no such`)
	reConflict = regexp.MustCompile(`(?i)conflict|precondition`)
)

// providerStatusError maps a provider error to a generic {status,error} for
// RESPONSES (1:1 with Node providerStatusError).
func providerStatusError(status int, msg string) (int, string) {
	if status == 401 || reAuth.MatchString(msg) {
		return 401, "authentication failed"
	}
	if status == 404 || reNotFound.MatchString(msg) {
		return 404, "not found"
	}
	if status == 409 || status == 412 || reConflict.MatchString(msg) {
		return 409, "modified since last sync"
	}
	return 502, "provider request failed"
}

// sanitizeUrlForLogging strips embedded credentials from a URL for logs (1:1).
func sanitizeUrlForLogging(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Scheme + "://" + u.Host + u.Path
}

// webdavStatusOf extracts a provider status from *WebDAVError (else 0).
func webdavStatusOf(err error) int {
	var we *WebDAVError
	if errors.As(err, &we) {
		return we.Status
	}
	return 0
}

// isRetryable reports whether a WebDAV status should be retried (1:1: 423 /
// 502 / 503 / 504).
func isRetryable(status int) bool {
	switch status {
	case 423, 502, 503, 504:
		return true
	}
	return false
}

// --- WebDAV client ------------------------------------------------------------

// WebDAVClient performs the WebDAV protocol operations. It is the 1:1 Go port
// of services/webdavinterface/app/src/WebDAVClient.mjs.
type WebDAVClient struct {
	Cfg      WebDAVConfig
	BaseURL  string
	Username string
	Password string
}

// NewWebDAVClient validates auth + URL (1:1 with the node constructor).
func NewWebDAVClient(cfg WebDAVConfig, baseURL, username, password string) (*WebDAVClient, error) {
	if username == "" || password == "" {
		return nil, errors.New("Missing authentication credentials")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("WebDAV URL must use HTTP or HTTPS")
	}
	cfg.withDefaults()
	base := strings.TrimSuffix(u.Scheme+"://"+u.Host+u.Path, "/")
	return &WebDAVClient{Cfg: cfg, BaseURL: base, Username: username, Password: password}, nil
}

// do performs one upstream request with Basic auth.
func (c *WebDAVClient) do(ctx context.Context, method, u string, header http.Header, body []byte) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	if header != nil {
		for k, vs := range header {
			for _, v := range vs {
				req.Header.Set(k, v)
			}
		}
	}
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	return c.Cfg.HTTPClient.Do(req)
}

// executeWithRetry runs op with exponential backoff on 423/502/503/504 (1:1).
func (c *WebDAVClient) executeWithRetry(ctx context.Context, op func() error) error {
	var last error
	for attempt := 0; attempt <= c.Cfg.MaxRetries; attempt++ {
		err := op()
		if err == nil {
			return nil
		}
		last = err
		st := webdavStatusOf(err)
		if !isRetryable(st) {
			return err
		}
		delay := time.Duration(c.Cfg.RetryDelayMs*int(1<<uint(attempt))) * time.Millisecond
		if c.Cfg.Sleep != nil {
			c.Cfg.Sleep(delay)
		} else {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return last
}

// closeErr closes a response body and classifies a non-2xx status as a
// *WebDAVError carrying the provider status.
func collectStatus(resp *http.Response, allow ...int) (body []byte, err error) {
	ok := false
	for _, a := range allow {
		if resp.StatusCode == a {
			ok = true
			break
		}
	}
	defer resp.Body.Close()
	b, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return nil, rerr
	}
	if !ok {
		return b, webdavStatus(resp.StatusCode, "WebDAV request failed: "+resp.Status)
	}
	return b, nil
}

// Check lists the root (1:1 with node check()).
func (c *WebDAVClient) Check(ctx context.Context) error {
	return c.executeWithRetry(ctx, func() error {
		_, _, err := c.propfind(ctx, "/")
		return err
	})
}

// List returns the normalized directory entries for path (1:1).
func (c *WebDAVClient) List(ctx context.Context, resourcePath string) ([]WebDAVEntry, error) {
	var entries []WebDAVEntry
	if err := c.executeWithRetry(ctx, func() error {
		raw, _, perr := c.propfind(ctx, resourcePath)
		if perr != nil {
			return perr
		}
		entries = buildEntries(resourcePath, parseMultistatus(raw))
		return nil
	}); err != nil {
		return nil, err
	}
	return entries, nil
}

// buildEntries maps parsed multistatus entries to normalized paths (1:1 with
// node list()).
func buildEntries(resourcePath string, entries []WebDAVEntry) []WebDAVEntry {
	parent := strings.TrimSuffix(resourcePath, "/")
	if parent == "" {
		parent = "/"
	}
	for i := range entries {
		base := entries[i].Href
		if base == "" {
			base = path.Base(uPath(resourcePath))
		}
		// Node: path = filename || `${parent}/${basename}` collapsed.
		full := parent + "/" + base
		if !strings.HasPrefix(full, "/") {
			full = "/" + full
		}
		entries[i].Path = collapseSlashes(full)
	}
	return entries
}

// Download returns the base64 file content (1:1).
func (c *WebDAVClient) Download(ctx context.Context, resourcePath string) (string, error) {
	var out string
	if err := c.executeWithRetry(ctx, func() error {
		b, derr := c.downloadRaw(ctx, resourcePath)
		if derr != nil {
			return derr
		}
		out = base64.StdEncoding.EncodeToString(b)
		return nil
	}); err != nil {
		return "", err
	}
	return out, nil
}

func (c *WebDAVClient) downloadRaw(ctx context.Context, resourcePath string) ([]byte, error) {
	u := c.url(resourcePath)
	resp, err := c.do(ctx, http.MethodGet, u, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return nil, rerr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return b, webdavStatus(resp.StatusCode, "WebDAV download failed")
	}
	return b, nil
}

// Upload creates the parent dir (recursive) then PUTs content (1:1, incl.
// If-Match etag, 404-retry mkcol+put, retriable retries).
func (c *WebDAVClient) Upload(ctx context.Context, resourcePath, contentBase64 string, etag string) error {
	content, err := base64.StdEncoding.DecodeString(contentBase64)
	if err != nil {
		return err
	}
	parent := "/"
	if idx := strings.LastIndex(resourcePath, "/"); idx > 0 {
		parent = resourcePath[:idx]
	}
	return c.executeWithRetry(ctx, func() error {
		_ = c.mkcCol(ctx, parent)
		perr := c.put(ctx, resourcePath, content, etag)
		if st := webdavStatusOf(perr); st == 404 {
			_ = c.mkcCol(ctx, parent)
			perr = c.put(ctx, resourcePath, content, etag)
		}
		if st := webdavStatusOf(perr); st == 412 || reConflict.MatchString(stringErr(perr)) {
			return webdavStatus(412, "Upload conflict: ETag mismatch")
		}
		return perr
	})
}

// Delete removes a file; 404 -> success with notFound (1:1).
func (c *WebDAVClient) Delete(ctx context.Context, resourcePath string) (notFound bool, err error) {
	var nf bool
	if e := c.executeWithRetry(ctx, func() error {
		resp, rerr := c.do(ctx, http.MethodDelete, c.url(resourcePath), nil, nil)
		if rerr != nil {
			return rerr
		}
		b, cErr := collectStatus(resp, 200, 204)
		_ = b
		if cErr != nil {
			if webdavStatusOf(cErr) == 404 {
				nf = true
				return nil
			}
			return cErr
		}
		return nil
	}); e != nil {
		return false, e
	}
	return nf, nil
}

// Move moves src -> dst with overwrite (1:1).
func (c *WebDAVClient) Move(ctx context.Context, source, destination string) error {
	return c.executeWithRetry(ctx, func() error {
		h := http.Header{}
		h.Set("Destination", c.url(destination))
		resp, err := c.do(ctx, "MOVE", c.url(source), h, nil)
		if err != nil {
			return err
		}
		_, cErr := collectStatus(resp, 200, 201, 204)
		return cErr
	})
}

// CreateDirectory MKCOLs a path; 405/ALREADY_EXISTS -> created=false (1:1).
func (c *WebDAVClient) CreateDirectory(ctx context.Context, resourcePath string) (created bool, err error) {
	var createdFlag bool
	if e := c.executeWithRetry(ctx, func() error {
		resp, rerr := c.do(ctx, "MKCOL", c.url(resourcePath), nil, nil)
		if rerr != nil {
			return rerr
		}
		resp.Body.Close()
		if resp.StatusCode == 405 {
			createdFlag = false
			return nil
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return webdavStatus(resp.StatusCode, "WebDAV MKCOL failed")
		}
		createdFlag = true
		return nil
	}); e != nil {
		return false, e
	}
	return createdFlag, nil
}

// --- primitive WebDAV verbs ---------------------------------------------------

func (c *WebDAVClient) url(resourcePath string) string {
	if resourcePath == "" {
		resourcePath = "/"
	}
	if !strings.HasPrefix(resourcePath, "/") {
		resourcePath = "/" + resourcePath
	}
	return c.BaseURL + resourcePath
}

func (c *WebDAVClient) propfind(ctx context.Context, resourcePath string) (body []byte, resp *http.Response, err error) {
	u := c.url(resourcePath)
	h := http.Header{}
	h.Set("Depth", "1")
	h.Set("Content-Type", "application/xml; charset=utf-8")
	xmlBody := []byte(`<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:">
  <D:allprop/>
</D:propfind>`)
	resp, err = c.do(ctx, "PROPFIND", u, h, xmlBody)
	if err != nil {
		return nil, nil, err
	}
	b, cErr := collectStatus(resp, 207)
	if cErr != nil {
		return b, resp, cErr
	}
	return b, resp, nil
}

func (c *WebDAVClient) mkcCol(ctx context.Context, dirPath string) error {
	// Recursive MKCOL of each ancestor (1:1 with createDirectory recursive:true).
	parts := strings.Split(strings.Trim(dirPath, "/"), "/")
	abs := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		abs += "/" + p
		created, cErr := c.CreateDirectory(ctx, abs)
		if cErr != nil && webdavStatusOf(cErr) != 405 {
			return cErr
		}
		if created {
			_ = created
		}
	}
	return nil
}

func (c *WebDAVClient) put(ctx context.Context, resourcePath string, content []byte, etag string) error {
	h := http.Header{}
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Overwrite", "T")
	if etag != "" {
		h.Set("If-Match", etag)
	}
	resp, err := c.do(ctx, http.MethodPut, c.url(resourcePath), h, content)
	if err != nil {
		return err
	}
	_, cErr := collectStatus(resp, 200, 201, 204)
	return cErr
}

func stringErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func collapseSlashes(s string) string {
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	return s
}

func uPath(p string) string {
	if idx := strings.LastIndex(p, "?"); idx >= 0 {
		p = p[:idx]
	}
	return p
}

// --- minimal DAV multistatus parse ------------------------------------------

type davMultistatus struct {
	XMLName   xml.Name      `xml:"multistatus"`
	Responses []davResponse `xml:"response"`
}

type davResponse struct {
	Href      string        `xml:"href"`
	PropStats []davPropstat `xml:"propstat"`
}

type davPropstat struct {
	XMLName xml.Name `xml:"propstat"`
	Prop    davProp  `xml:"prop"`
}

type davProp struct {
	Displayname      string      `xml:"displayname"`
	Getetag          string      `xml:"getetag"`
	Getlastmodified  string      `xml:"getlastmodified"`
	Getcontentlength string      `xml:"getcontentlength"`
	Resourcetype     *davResType `xml:"resourcetype"`
}

type davResType struct {
	Collection *struct{} `xml:"collection"`
}

func parseMultistatus(raw []byte) []WebDAVEntry {
	var ms davMultistatus
	if err := xmlUnmarshalFlexible(raw, &ms); err != nil {
		return nil
	}
	out := make([]WebDAVEntry, 0, len(ms.Responses))
	for _, r := range ms.Responses {
		e := WebDAVEntry{Href: baseName(r.Href)}
		for _, ps := range r.PropStats {
			p := ps.Prop
			if p.Displayname != "" {
				e.Href = p.Displayname
			}
			if p.Getetag != "" {
				e.Etag = p.Getetag
			}
			if p.Getlastmodified != "" {
				if t, terr := parseHTTPDate(p.Getlastmodified); terr == nil {
					e.ModifiedAt = t.UTC().Format(time.RFC3339)
				}
			}
			if p.Getcontentlength != "" {
				if n, serr := strconv.ParseInt(strings.TrimSpace(p.Getcontentlength), 10, 64); serr == nil {
					e.Size = n
				}
			}
			if p.Resourcetype != nil && p.Resourcetype.Collection != nil {
				e.IsDirectory = true
			}
		}
		out = append(out, e)
	}
	return out
}

func parseHTTPDate(s string) (time.Time, error) {
	if t, err := http.ParseTime(s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

func baseName(href string) string {
	p := uPath(href)
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return ""
	}
	return path.Base(p)
}

func xmlUnmarshalFlexible(b []byte, v interface{}) error {
	return xmlUnmarshal(b, v)
}

func xmlUnmarshal(b []byte, v interface{}) error {
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = false
	return dec.Decode(v)
}

// writeJSON writes a JSON body with the code (helper for the server handlers).
func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONErr writes an error object (1:1 with the node `{error}` / `{...}` shapes).
func writeJSONErr(w http.ResponseWriter, code int, v interface{}) {
	writeJSON(w, code, v)
}

// --- service-token auth (1:1 with requireServiceToken) ----------------------

type tokenWarn struct {
	once sync.Once
}

// requireServiceToken returns a middleware enforcing SHARED_SERVICE_TOKEN.
func requireServiceToken(expected string, warn func()) func(next http.HandlerFunc) http.HandlerFunc {
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
			if !timingSafeEqual(cand, expected) {
				writeJSON(rw, 401, map[string]string{"error": "Invalid or missing service token"})
				return
			}
			next(rw, r)
		}
	}
}

// timingSafeEqual is a constant-time string compare (1:1 with timingSafeEqual).
func timingSafeEqual(a, b string) bool {
	ab, bb := []byte(a), []byte(b)
	if len(ab) != len(bb) {
		// still compare a fixed length to avoid an early-return timing leak
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
	auth := requireServiceToken(cfg.ServiceToken, warnFn)

	mux := http.NewServeMux()
	mux.HandleFunc("/check", auth(h.check))
	mux.HandleFunc("/list", auth(h.list))
	mux.HandleFunc("/mkdir", auth(h.mkdir))
	mux.HandleFunc("/file", auth(h.file))
	mux.HandleFunc("/move", auth(h.move))
	return mux
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
		writeJSONErr(w, 400, map[string]string{"error": "Missing required fields: server_url, username, password"})
		return
	}
	c, err := h.client(b.ServerURL, b.Username, b.Password)
	if err != nil {
		writeJSONErr(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := c.Check(r.Context()); err != nil {
		st, msg := providerStatusError(webdavStatusOf(err), stringErr(err))
		writeJSONErr(w, st, map[string]string{"error": msg})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"status": "ok", "message": "Connection successful"})
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
		writeJSONErr(w, 400, map[string]string{"error": "Missing required fields"})
		return
	}
	c, err := h.client(b.ServerURL, b.Username, b.Password)
	if err != nil {
		writeJSONErr(w, 400, map[string]string{"error": err.Error()})
		return
	}
	entries, lerr := c.List(r.Context(), b.Path)
	if lerr != nil {
		st, msg := providerStatusError(webdavStatusOf(lerr), stringErr(lerr))
		writeJSONErr(w, st, map[string]string{"error": msg})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"entries": entries})
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
		writeJSONErr(w, 400, map[string]string{"error": "Missing required fields"})
		return
	}
	c, err := h.client(b.ServerURL, b.Username, b.Password)
	if err != nil {
		writeJSONErr(w, 400, map[string]string{"error": err.Error()})
		return
	}
	created, cErr := c.CreateDirectory(r.Context(), b.Path)
	if cErr != nil {
		st, msg := providerStatusError(webdavStatusOf(cErr), stringErr(cErr))
		writeJSONErr(w, st, map[string]string{"error": msg})
		return
	}
	if created {
		writeJSON(w, 200, map[string]interface{}{"status": "ok", "created": true})
	} else {
		writeJSON(w, 200, map[string]interface{}{"status": "ok", "created": false, "message": "Directory already exists"})
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
		writeJSONErr(w, 400, map[string]string{"error": "Missing required fields"})
		return
	}
	c, err := h.client(b.ServerURL, b.Username, b.Password)
	if err != nil {
		writeJSONErr(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if mErr := c.Move(r.Context(), b.Src, b.Dst); mErr != nil {
		st, msg := providerStatusError(webdavStatusOf(mErr), stringErr(mErr))
		writeJSONErr(w, st, map[string]string{"error": msg})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"status": "ok", "moved": true})
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
		writeJSONErr(w, 405, map[string]string{"error": "method not allowed"})
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
		writeJSONErr(w, 400, map[string]string{"error": "Missing required parameters"})
		return
	}
	password := basicPassword(r.Header.Get("Authorization"))
	if password == "" {
		writeJSONErr(w, 401, map[string]string{"error": "Authentication required"})
		return
	}
	c, err := h.client(serverURL, username, password)
	if err != nil {
		writeJSONErr(w, 400, map[string]string{"error": err.Error()})
		return
	}
	b64, dErr := c.Download(r.Context(), p)
	if dErr != nil {
		if webdavStatusOf(dErr) == 404 {
			writeJSONErr(w, 404, map[string]string{"error": "not found"})
			return
		}
		st, msg := providerStatusError(webdavStatusOf(dErr), stringErr(dErr))
		writeJSONErr(w, st, map[string]string{"error": msg})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"path": p, "content_base64": b64})
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
		writeJSONErr(w, 400, map[string]string{"error": "Missing required fields"})
		return
	}
	c, err := h.client(b.ServerURL, b.Username, b.Password)
	if err != nil {
		writeJSONErr(w, 400, map[string]string{"error": err.Error()})
		return
	}
	etag := b.Etag
	if uErr := c.Upload(r.Context(), b.Path, *b.ContentBase64, etag); uErr != nil {
		st := webdavStatusOf(uErr)
		if st == 412 || reConflict.MatchString(stringErr(uErr)) {
			writeJSONErr(w, 412, map[string]interface{}{"error": "ETag mismatch - file modified", "status": 412})
			return
		}
		if st == 404 {
			writeJSONErr(w, 404, map[string]string{"error": "parent path not found"})
			return
		}
		mapped, msg := providerStatusError(st, stringErr(uErr))
		writeJSONErr(w, mapped, map[string]string{"error": msg})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"status": "ok", "uploaded": true})
}

func (h *WebDAVHandlers) fileDelete(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	serverURL := r.Header.Get("X-Server-Url")
	username := r.Header.Get("X-Username")
	if p == "" || serverURL == "" {
		writeJSONErr(w, 400, map[string]string{"error": "Missing required parameters"})
		return
	}
	password := basicPassword(r.Header.Get("Authorization"))
	if password == "" {
		writeJSONErr(w, 401, map[string]string{"error": "Authentication required"})
		return
	}
	c, err := h.client(serverURL, username, password)
	if err != nil {
		writeJSONErr(w, 400, map[string]string{"error": err.Error()})
		return
	}
	notFound, dErr := c.Delete(r.Context(), p)
	if dErr != nil {
		st := webdavStatusOf(dErr)
		if st == 404 || reNotFound.MatchString(stringErr(dErr)) {
			writeJSON(w, 200, map[string]interface{}{"status": "ok", "notFound": true, "message": "File not found"})
			return
		}
		mapped, msg := providerStatusError(st, stringErr(dErr))
		writeJSONErr(w, mapped, map[string]string{"error": msg})
		return
	}
	if notFound {
		writeJSON(w, 200, map[string]interface{}{"status": "ok", "notFound": true, "message": "File not found"})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"status": "ok", "deleted": true})
}

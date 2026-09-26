package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// WebAPI — the Go web private join endpoint (go/services/web/features/
// projectlist/joinview.go): POST /project/:pid/join with basic auth.
// Pinned contract:
//
//	400 → validation  403 text "Forbidden" (no access)  404 text "Not Found" (ghost)
//	200 → JSON {project, privilegeLevel, isRestrictedUser, isTokenMember, isInvitedMember}
type WebAPI struct {
	BaseURL string // e.g. http://127.0.0.1:4000 (no trailing slash)
	User    string // WEB_API_USER, default "overleaf"
	Pass    string // WEB_API_PASSWORD, default "password"
	HTTP    *http.Client
}

// FlushURL — the document-updater flush endpoint the Node real-time called on
// last-leaver (DocumentUpdaterManager.flushProjectToMongoAndDelete):
// DELETE {docUpdater}/project/{pid}?background=true.
type FlushAPI struct {
	BaseURL string // e.g. http://127.0.0.1:3003
	HTTP    *http.Client
}

func (w *WebAPI) client() *http.Client {
	if w.HTTP != nil {
		return w.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (f *FlushAPI) client() *http.Client {
	if f.HTTP != nil {
		return f.HTTP
	}
	return &http.Client{Timeout: 5 * time.Second}
}

// JoinErrorKind — the Node error taxonomy for join failures (real-time
// Errors.js). Connection-rejected messages the client sees:
//
//	403 → "not authorized"      (NotAuthorizedError)
//	404 → "project not found"   (CodedError ProjectNotFound)
//	429 → "rate-limit hit when joining project" (CodedError TooManyRequests)
//	else → "web api request failed <status>" (WebApiRequestFailedError)
type joinStatus struct {
	project        map[string]any
	privilegeLevel string
	isRestricted   bool
	isTokenMember  bool
	isInvited      bool
	rejectMessage  string // non-empty → connectionRejected with this message
}

// Join calls the web private join API.
func (w *WebAPI) Join(ctx context.Context, pid, userID, anonToken string) (joinStatus, error) {
	uid := userID
	if uid == "" {
		uid = "anonymous-user"
	}
	body := map[string]any{"userId": uid}
	if anonToken != "" {
		body["anonymousAccessToken"] = anonToken
	}
	b, _ := json.Marshal(body)
	u := strings2(w.BaseURL) + "/project/" + url.PathEscape(pid) + "/join"
	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(b))
	if err != nil {
		return joinStatus{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.User != "" {
		req.SetBasicAuth(w.User, w.Pass)
	}
	res, err := w.client().Do(req)
	if err != nil {
		return joinStatus{}, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))

	switch {
	case res.StatusCode == 403:
		return joinStatus{rejectMessage: "not authorized"}, nil
	case res.StatusCode == 404:
		return joinStatus{rejectMessage: "project not found"}, nil
	case res.StatusCode == 429:
		return joinStatus{rejectMessage: "rate-limit hit when joining project"}, nil
	case res.StatusCode >= 400:
		return joinStatus{rejectMessage: fmt.Sprintf("web api request failed %d", res.StatusCode)}, nil
	}
	var out struct {
		Project          map[string]any `json:"project"`
		PrivilegeLevel   string         `json:"privilegeLevel"`
		IsRestrictedUser bool           `json:"isRestrictedUser"`
		IsTokenMember    bool           `json:"isTokenMember"`
		IsInvitedMember  bool           `json:"isInvitedMember"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return joinStatus{}, fmt.Errorf("corrupted join response: %w", err)
	}
	if out.Project == nil || out.PrivilegeLevel == "" {
		// Node: !privilegeLevel → NotAuthorizedError.
		return joinStatus{rejectMessage: "not authorized"}, nil
	}
	return joinStatus{
		project:        out.Project,
		privilegeLevel: out.PrivilegeLevel,
		isRestricted:   out.IsRestrictedUser,
		isTokenMember:  out.IsTokenMember,
		isInvited:      out.IsInvitedMember,
	}, nil
}

// Flush — best-effort last-leaver flush (parity call; a no-op in practice
// since the OT text-sync buffer is empty in the Yjs world).
func (f *FlushAPI) Flush(ctx context.Context, pid string) {
	if f == nil || f.BaseURL == "" {
		return
	}
	u := strings2(f.BaseURL) + "/project/" + url.PathEscape(pid) + "?background=true"
	req, err := http.NewRequestWithContext(ctx, "DELETE", u, nil)
	if err != nil {
		return
	}
	res, err := f.client().Do(req)
	if err != nil {
		return
	}
	io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	res.Body.Close()
}

func strings2(s string) string {
	if len(s) > 0 && s[len(s)-1] == '/' {
		return s[:len(s)-1]
	}
	return s
}

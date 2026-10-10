package wakatime

// WakaTime/Wakapi HTTP client (reference WakaTimeApiClient.mjs parity).

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultAPIURL — reference constant (wakatime.com v1 surface).
const DefaultAPIURL = "https://wakatime.com/api/v1"

// wakatimeUserAgent — WakaTime/Wakapi parse this as
// `wakatime/version (os) editor/editorVersion editor-wakatime/pluginVersion`;
// must start with "wakatime/" and carry a parenthesised OS token or the
// account UI reports the editor as "unknown" (reference fix note).
const wakatimeUserAgent = "wakatime/1.0.0 (linux) ollitex/1.0 ollitex-wakatime/1.0"

const wakaTimeout = 15 * time.Second

type wakaCreds struct {
	APIURL string
	APIKey string
}

type wakaClient struct {
	http *http.Client
}

func newWakaClient() *wakaClient {
	return &wakaClient{http: &http.Client{Timeout: wakaTimeout}}
}

func normalizeAPIURL(apiURL string) string {
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}
	return strings.TrimRight(apiURL, "/")
}

// do — audited N1: the destination URL passes the instance policy (shape
// + host lockdown + private-IP rejection when configured) IMMEDIATELY
// before the dial, so even a credential stored before a policy tightening
// cannot reach a forbidden host. Policy rejects are surfaced to the
// caller as errHostBlocked/errInvalidURL (400 shape).
func (c *wakaClient) do(ctx context.Context, cr wakaCreds, method, u string, body []byte) (int, []byte, error) {
	if perr := checkCredsPolicy(ctx, wakaCreds{APIURL: u}); perr != nil {
		return 0, nil, perr
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return 0, nil, errBadAPI
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(cr.APIKey)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", wakatimeUserAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded || strings.Contains(err.Error(), "timeout") {
			return 0, nil, errTimeout
		}
		return 0, nil, errBadAPI
	}
	defer resp.Body.Close()
	b, rerr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if rerr != nil {
		return resp.StatusCode, nil, errUpstream
	}
	return resp.StatusCode, b, nil
}

func okStatus(code int) bool { return code >= 200 && code < 300 }

// verifyCredentials — GET {base}/users/current (reference parity).
func (c *wakaClient) verifyCredentials(ctx context.Context, cr wakaCreds) error {
	status, _, err := c.do(ctx, cr, http.MethodGet, normalizeAPIURL(cr.APIURL)+"/users/current", nil)
	if err != nil {
		return err
	}
	if okStatus(status) {
		return nil
	}
	return mapStatus(status)
}

// sendHeartbeat — POST {base}/users/current/heartbeats (reference parity).
func (c *wakaClient) sendHeartbeat(ctx context.Context, cr wakaCreds, hb map[string]any) error {
	body, err := json.Marshal(hb)
	if err != nil {
		return errBadAPI
	}
	status, _, err := c.do(ctx, cr, http.MethodPost,
		normalizeAPIURL(cr.APIURL)+"/users/current/heartbeats", body)
	if err != nil {
		return err
	}
	if okStatus(status) {
		return nil
	}
	return mapStatus(status)
}

// sendHeartbeatsBulk — POST {base}/users/current/heartbeats.bulk.
func (c *wakaClient) sendHeartbeatsBulk(ctx context.Context, cr wakaCreds, hbs []map[string]any) error {
	body, err := json.Marshal(hbs)
	if err != nil {
		return errBadAPI
	}
	status, _, err := c.do(ctx, cr, http.MethodPost,
		normalizeAPIURL(cr.APIURL)+"/users/current/heartbeats.bulk", body)
	if err != nil {
		return err
	}
	if okStatus(status) {
		return nil
	}
	return mapStatus(status)
}

// projectSummary — GET {base}/users/current/summaries?start=&end=&project=
// (reference: sum data[].projects[name==project].total_seconds).
func (c *wakaClient) projectSummary(ctx context.Context, cr wakaCreds, project string, rangeDays int) (int64, error) {
	q := url.Values{}
	end := time.Now()
	start := end.AddDate(0, 0, -rangeDays)
	q.Set("start", start.UTC().Format("2006-01-02"))
	q.Set("end", end.UTC().Format("2006-01-02"))
	q.Set("project", project)
	status, raw, err := c.do(ctx, cr, http.MethodGet,
		normalizeAPIURL(cr.APIURL)+"/users/current/summaries?"+q.Encode(), nil)
	if err != nil {
		return 0, err
	}
	if status >= 400 {
		return 0, mapStatus(status)
	}
	var m struct {
		Data []struct {
			Projects []struct {
				Name      string `json:"name"`
				TotalSecs int64  `json:"total_seconds"`
			} `json:"projects"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return 0, errUpstream
	}
	var total int64
	for _, day := range m.Data {
		for _, p := range day.Projects {
			if p.Name == project {
				total += p.TotalSecs
			}
		}
	}
	return total, nil
}

// userSummary — GET {base}/users/current/summaries?start=&end= (NO project
// filter): totals ALL of the user's projects. owner 2026-10-10 (Q2):
// the /user-settings WakaTime tab shows the user's own cross-project time
// there (their dashboard remains the full view).
func (c *wakaClient) userSummary(ctx context.Context, cr wakaCreds, rangeDays int) (int64, error) {
	q := url.Values{}
	end := time.Now()
	start := end.AddDate(0, 0, -rangeDays)
	q.Set("start", start.UTC().Format("2006-01-02"))
	q.Set("end", end.UTC().Format("2006-01-02"))
	status, raw, err := c.do(ctx, cr, http.MethodGet,
		normalizeAPIURL(cr.APIURL)+"/users/current/summaries?"+q.Encode(), nil)
	if err != nil {
		return 0, err
	}
	if status >= 400 {
		return 0, mapStatus(status)
	}
	var m struct {
		Data []struct {
			Projects []struct {
				TotalSecs int64 `json:"total_seconds"`
			} `json:"projects"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return 0, errUpstream
	}
	var total int64
	for _, day := range m.Data {
		for _, p := range day.Projects {
			total += p.TotalSecs
		}
	}
	return total, nil
}

func mapStatus(code int) *ErrWaka {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return errForbidden
	case http.StatusNotFound:
		return errNotFound
	case http.StatusTooManyRequests:
		return errTooMany
	default:
		return &ErrWaka{Status: http.StatusInternalServerError, Message: "WakaTime request error"}
	}
}

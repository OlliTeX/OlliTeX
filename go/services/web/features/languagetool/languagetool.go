// Package languagetool — P6.15: the LanguageTool proxy surface
// (services/web/modules/languagetool — LanguageToolRouter.mjs /
// LanguageToolController.mjs / adminConfig.mjs).
//
// Node oracle (2026-09-18, live-pinned on the Node leg; see WEB_GO_PLAN.md):
//
// URLs resolve PER REQUEST (admin changes apply at runtime):
//   admin JSON `languageToolUrl` (the llm admin settings file — the LLM
//   module owns it; empty string = skip)
//   > env LANGUAGE_TOOL_URL (value as-is)
//   > env LANGUAGETOOL_URL (trailing slashes stripped)
//   > env LANGUAGE_TOOL_HOST[:PORT|8010] (http://)
//   > undefined → UNAVAILABLE
// `languageToolDisabledByAdmin` (same admin JSON) → UNAVAILABLE.
// Available URLs additionally get trailing slashes stripped.
//
// Routes (global chain: anon GET+json → 401 "Unauthorized"; anon GET bare →
// 302 /login; anon non-GET → 403 "Forbidden" — Route.NoLogin=false):
//
// GET /languagetool/languages (requireLogin)
//   unavailable → 503 {"error":"LanguageTool is not available. Ask your administrator."}
//   LT GET /v2/languages (Accept json, 10s):
//     !ok     → 502 {"error":"LanguageTool request failed"}
//     timeout → 504 {"error":"LanguageTool timed out"}
//     other   → next(err) → the rendered 500 view
//     ok      → 200 LT JSON verbatim (Node res.json(parsed) — raw stream is
//               byte-identical for the LT v2 array payloads)
//
// POST /languagetool/check (requireLogin)
//   body {language='auto', text?, data?, picky?}
//   unavailable → 503 (same body)
//   !text && !data → 400 {"error":"text or data is required"}
//   level: body HAS 'picky' → true→picky / false→default; absent →
//          env LANGUAGE_TOOL_LEVEL (lowercased; 'picky'|'default') else 'picky'
//   LT POST /v2/check (x-www-form-urlencoded, 60s) — params in Node
//   URLSearchParams insertion order:
//     language, data|stringified-data (or the text), level,
//     disabledRules=WHITESPACE_RULE,COMMA_PARENTHESIS_WHITESPACE,
//                    CONSECUTIVE_SPACES,DASH_RULE,UPPERCASE_SENTENCE_START
//     (data/text sliced to 100_000 chars)
//     !ok/timeout/network → 200 {"matches":[]} (the linter stays silent)
//     other               → next(err) → 500
//     ok                  → 200 LT JSON verbatim
//
// POST /admin/languagetool/check (site admin — requireLogin +
// ensureUserIsSiteAdmin; non-member → 302 /restricted?from=… — the
// core.RequireSiteAdmin restrictedBounce, pinned P6.5/P6.10 family)
//   url = body.url (truthy) || resolved ; no url →
//   400 {"success":false,"error":"No LanguageTool URL configured"}
//   LT GET /v2/languages (15s):
//     !ok  → 200 {"success":false,"error":"LanguageTool server responded with status N"}
//     ok   → 200 {"success":true,"message":"LanguageTool reachable","languageCount":N}
//            (N = array length, 0 when the body is not an array)
//     catch→ 500 {"success":false,"error":"Connection attempt failed"}

package languagetool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/llmsettings"
)

const (
	languagesTimeout = 10 * time.Second
	checkTimeout     = 60 * time.Second
	connCheckTimeout = 15 * time.Second

	maxTextSize = 100_000

	latexDisabledRules = "WHITESPACE_RULE,COMMA_PARENTHESIS_WHITESPACE,CONSECUTIVE_SPACES,DASH_RULE,UPPERCASE_SENTENCE_START"

	unavailableMsg = `{"error":"LanguageTool is not available. Ask your administrator."}`
)

// ---------- URL resolution ----------------------------------------------------

type ltResolved struct {
	available bool
	url       string
}

// ltResolve — Node effectiveUrl() (per request):
//
//	admin.languageToolUrl > LANGUAGE_TOOL_URL > LANGUAGETOOL_URL (trim /) >
//	LANGUAGE_TOOL_HOST[:PORT] ; then trim trailing slashes; unavailable when
//	empty or languageToolDisabledByAdmin.
func ltResolve() ltResolved {
	var raw string
	adminURL, disabled := adminSettingsLanguagetool()
	switch {
	case adminURL != "":
		raw = adminURL
	case os.Getenv("LANGUAGE_TOOL_URL") != "":
		raw = os.Getenv("LANGUAGE_TOOL_URL")
	case os.Getenv("LANGUAGETOOL_URL") != "":
		raw = strings.TrimRight(os.Getenv("LANGUAGETOOL_URL"), "/")
	case os.Getenv("LANGUAGE_TOOL_HOST") != "" || os.Getenv("LANGUAGE_TOOL_PORT") != "":
		host := os.Getenv("LANGUAGE_TOOL_HOST")
		port := os.Getenv("LANGUAGE_TOOL_PORT")
		if host == "" {
			host = "languagetool"
		}
		if port == "" {
			port = "8010"
		}
		raw = "http://" + host + ":" + port
	}
	u := strings.TrimRight(raw, "/")
	return ltResolved{available: u != "" && !disabled, url: u}
}

// adminSettingsLanguagetool — Node readAdminSettings() + the two
// LanguageTool-specific fields. Node's reader: fs.readFile of
// LLM_ADMIN_SETTINGS_PATH (default /var/lib/overleaf/data/llm-admin-settings.json),
// empty file / missing / bad JSON → {} (warn-logged).
func adminSettingsLanguagetool() (string, bool) {
	path := os.Getenv("LLM_ADMIN_SETTINGS_PATH")
	if path == "" {
		path = "/var/lib/overleaf/data/llm-admin-settings.json"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var obj map[string]any
	if json.Unmarshal(bytes.TrimSpace(b), &obj) != nil {
		return "", false
	}
	u, _ := obj["languageToolUrl"].(string)
	d, _ := obj["languageToolDisabledByAdmin"].(bool)
	return u, d
}

// ---------- Feature ----------------------------------------------------------

func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "languagetool",
		Routes: []core.Route{
			{Method: "GET", Path: "/languagetool/languages", Handler: hLanguages(a)},
			{Method: "POST", Path: "/languagetool/check", Handler: hCheck(a)},
			{Method: "POST", Path: "/admin/languagetool/check", Handler: hAdminCheck(a)},
		},
	}
}

// ---------- plumbing ----------------------------------------------------------

// is streamed verbatim; Node res.json(parsed) is byte-identical for the LT
// v2 payloads). Returns (status, body, timeout, err).
func ltFetch(ctx context.Context, method, u string, headers map[string]string, body []byte, timeout time.Duration) (int, []byte, bool, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequest(method, u, bytes.NewReader(body))
	if err != nil {
		return 0, nil, false, err
	}
	req = req.WithContext(cctx)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Method = method
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return 0, nil, true, err
		}
		return 0, nil, false, err
	}
	defer resp.Body.Close()
	b, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return resp.StatusCode, nil, false, rerr
	}
	return resp.StatusCode, b, false, nil
}

func ltLevel(body map[string]any) string {
	if has, ok := body["picky"]; ok {
		if has == true {
			return "picky"
		}
		return "default"
	}
	envLevel := strings.ToLower(os.Getenv("LANGUAGE_TOOL_LEVEL"))
	if envLevel == "picky" || envLevel == "default" {
		return envLevel
	}
	return "picky"
}

// ---------- GET /languagetool/languages ---------------------------------------

func hLanguages(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		r := ltResolve()
		if !r.available {
			res.JSON(503, []byte(unavailableMsg))
			return
		}
		status, b, timeout, err := ltFetch(cxt.Req.Context(), "GET", r.url+"/v2/languages",
			map[string]string{"Accept": "application/json"}, nil, languagesTimeout)
		if timeout {
			res.JSON(504, []byte(`{"error":"LanguageTool timed out"}`))
			return
		}
		if err != nil {
			// Node: non-timeout fetch error → next(err) → the rendered 500.
			res.SendStatus(500)
			return
		}
		if status < 200 || status >= 300 {
			res.JSON(502, []byte(`{"error":"LanguageTool request failed"}`))
			return
		}
		res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
		res.W.WriteHeader(200)
		_, _ = res.W.Write(llmsettings.NodeJSONRoundTrip(b))
	}
}

// ---------- POST /languagetool/check ------------------------------------------

func hCheck(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		r := ltResolve()
		if !r.available {
			res.JSON(503, []byte(unavailableMsg))
			return
		}
		body := map[string]any{}
		if cxt.Req != nil && cxt.Req.Body != nil {
			raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 4<<20))
			if len(bytes.TrimSpace(raw)) > 0 {
				var m map[string]any
				if json.Unmarshal(raw, &m) == nil {
					body = m
				}
			}
		}
		language, _ := body["language"].(string)
		if language == "" {
			language = "auto"
		}
		text, hasText := body["text"]
		data, hasData := body["data"]
		if (!hasText || text == nil) && (!hasData || data == nil) {
			res.JSON(400, []byte(`{"error":"text or data is required"}`))
			return
		}
		level := ltLevel(body)
		// Node URLSearchParams insertion order: language, data|text, level,
		// disabledRules. Form encoding = application/x-www-form-urlencoded
		// (spaces → '+', the same percent set as Go's QueryEscape).
		var buf strings.Builder
		w := func(k, v string) {
			buf.WriteString(url.QueryEscape(k) + "=" + url.QueryEscape(v) + "&")
		}
		w("language", language)
		if hasData && data != nil {
			ds := ""
			if s, ok := data.(string); ok {
				ds = s
			} else if ser, err := json.Marshal(data); err == nil {
				ds = string(ser)
			}
			if len(ds) > maxTextSize {
				ds = ds[:maxTextSize]
			}
			w("data", ds)
		} else {
			ts := ""
			if s, ok := text.(string); ok {
				ts = s
			}
			if len(ts) > maxTextSize {
				ts = ts[:maxTextSize]
			}
			w("text", ts)
		}
		w("level", level)
		w("disabledRules", latexDisabledRules)
		form := strings.TrimSuffix(buf.String(), "&")

		status, b, timeout, err := ltFetch(cxt.Req.Context(), "POST", r.url+"/v2/check",
			map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"},
			[]byte(form), checkTimeout)
		if timeout || err != nil {
			// Node: timeout OR TypeError (network) → the linter stays silent.
			res.JSON(200, []byte(`{"matches":[]}`))
			return
		}
		if status < 200 || status >= 300 {
			res.JSON(200, []byte(`{"matches":[]}`))
			return
		}
		res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
		res.W.WriteHeader(200)
		_, _ = res.W.Write(llmsettings.NodeJSONRoundTrip(b))
	}
}

// ---------- POST /admin/languagetool/check -------------------------------------

func hAdminCheck(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		provided := ""
		if cxt.Req != nil && cxt.Req.Body != nil {
			raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 4<<20))
			var m map[string]any
			if json.Unmarshal(raw, &m) == nil {
				if u, ok := m["url"]; ok {
					provided, _ = u.(string)
				}
			}
		}
		r := ltResolve()
		resolved := provided
		if resolved == "" {
			resolved = r.url
		}
		resolved = strings.TrimRight(resolved, "/")
		if resolved == "" {
			res.JSON(400, []byte(`{"success":false,"error":"No LanguageTool URL configured"}`))
			return
		}
		status, b, _, err := ltFetch(cxt.Req.Context(), "GET", resolved+"/v2/languages",
			map[string]string{"Accept": "application/json"}, nil, connCheckTimeout)
		if err != nil {
			res.JSON(500, []byte(`{"success":false,"error":"Connection attempt failed"}`))
			return
		}
		if status < 200 || status >= 300 {
			res.JSON(200, []byte(`{"success":false,"error":"LanguageTool server responded with status `+fmt.Sprint(status)+`"`))
			return
		}
		count := 0
		var arr []any
		if json.Unmarshal(b, &arr) == nil {
			count = len(arr)
		}
		res.JSON(200, []byte(`{"success":true,"message":"LanguageTool reachable","languageCount":`+fmt.Sprint(count)+`}`))
	}
}

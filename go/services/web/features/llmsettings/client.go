package llmsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------- SSRF guard (Node assertPublicLlmBaseUrl, exact details) ----------

// blockedURL — Node fail(): `Blocked LLM base URL (<detail>). Point the
// provider at a reachable public or LAN LLM endpoint.` — exact.
func blockedURL(detail string) error {
	return fmt.Errorf("Blocked LLM base URL (%s). Point the provider at a reachable public or LAN LLM endpoint.", detail)
}

// assertPublicLlmBaseUrl — Node parity (LLMClient.mjs). The URL is parsed
// with NEW URL semantics: a bare host (no scheme) is INVALID.
func assertPublicLlmBaseUrl(rawURL string) error {
	u, perr := url.Parse(strings.TrimSpace(rawURL))
	if perr != nil || u.Scheme == "" {
		return blockedURL("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		// Node checks AFTER a successful new URL(): 'ftp://x' parses fine,
		// then fails the protocol test. Go's url.Parse is more lenient
		// (accepts 'not a url' as a path) — scheme check closes the gap.
		return blockedURL("only http(s) URLs are allowed")
	}
	host := u.Hostname()
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	lh := strings.ToLower(host)
	if lh == "" {
		return blockedURL("missing host")
	}
	if lh == "localhost" || strings.HasSuffix(lh, ".localhost") ||
		strings.HasSuffix(lh, ".local") || strings.HasSuffix(lh, ".internal") ||
		strings.HasSuffix(lh, ".lan") || strings.HasSuffix(lh, ".home.arpa") ||
		lh == "0.0.0.0" || lh == "::" || lh == "::1" {
		return blockedURL("loopback/local name")
	}
	if jsRegexIPv6UL.MatchString(lh) || jsRegexIPv6LL.MatchString(lh) {
		return blockedURL("blocked IPv6 range")
	}
	if jm := jsRegexIPv4.FindStringSubmatch(lh); jm != nil {
		a, _ := strconv.Atoi(jm[1])
		b, _ := strconv.Atoi(jm[2])
		switch {
		case a == 0:
			return blockedURL("unspecified range")
		case a == 127:
			return blockedURL("loopback range")
		case a == 169 && b == 254:
			return blockedURL("cloud metadata / link-local range")
		}
	}
	return nil
}

var (
	jsRegexIPv6UL = regexp.MustCompile(`^f[cd][0-9a-f]{2}:`)
	jsRegexIPv6LL = regexp.MustCompile(`^fe[89ab][0-9a-f]:`)
	jsRegexIPv4   = regexp.MustCompile(`^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$`)
)

// ---------- provider normalization (Node LLMClient.mjs) ----------

// llmError — APICallError-style {code, message, status?}.
type llmError struct {
	code    string
	message string
	status  int
}

func (e *llmError) Error() string { return e.code + ": " + e.message }

// errLLMBadConfig — normalizeProviderSpec throw parity.
func errBadConfig(msg string) *llmError { return &llmError{code: "llm-bad-config", message: msg} }

// detectProviderType — Node: substring test on the raw string.
func detectProviderType(baseURL string) string {
	s := strings.TrimSpace(baseURL)
	if strings.Contains(s, "anthropic.com") {
		return "anthropic"
	}
	if strings.Contains(s, "openai.com") {
		return "openai"
	}
	return "openaiCompatible"
}

var stripV = regexp.MustCompile(`/v\d+$`)
var stripSlash = regexp.MustCompile(`/+$`)

// normalizeSpec — Node normalizeProviderSpec (throws llm-bad-config on
// unknown type or missing model).
func normalizeSpec(providerType, base, apiKey, model string) (map[string]string, error) {
	pt := providerType
	ok := false
	for _, t := range providerTypes {
		if pt == t {
			ok = true
		}
	}
	if !ok {
		return nil, errBadConfig("Unknown provider type")
	}
	if model == "" {
		return nil, errBadConfig("No model name provided")
	}
	base = strings.TrimSpace(base)
	base = stripSlash.ReplaceAllString(base, "")
	key := strings.TrimSpace(apiKey)
	if pt == "anthropic" {
		base = stripV.ReplaceAllString(base, "")
	}
	return map[string]string{"providerType": pt, "baseUrl": base, "apiKey": key, "model": model}, nil
}

// envModelList — Node envModelList().
func envModelList() []string {
	raw := lEnv("LLM_AVAILABLE_MODELS")
	if raw == "" {
		raw = lEnv("LLM_MODEL_NAME")
	}
	out := make([]string, 0)
	for _, m := range strings.Split(raw, ",") {
		m = strings.TrimSpace(m)
		if m != "" {
			out = append(out, m)
		}
	}
	return out
}

// defaultReviewMaxTokens — Node DEFAULT_REVIEW_MAX_TOKENS.
func defaultReviewMaxTokens() int {
	if v := lEnv("LLM_REVIEW_MAX_TOKENS"); v != "" {
		if n, err := parseIntJS(v); err == nil && n > 0 {
			return n
		}
	}
	return 12000
}

// parseIntJS — Node parseInt(x, 10): leading-numeric parse, ”/'abc' -> NaN.
func parseIntJS(s string) (int, error) {
	t := strings.TrimLeft(s, " \t\n\r")
	if t == "" {
		return 0, errors.New("nan")
	}
	i := 0
	neg := false
	if t[0] == '-' {
		neg = true
		i = 1
	} else if t[0] == '+' {
		i = 1
	}
	n := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		n = n*10 + int(t[i]-'0')
		i++
	}
	if i <= 0 || (i == 1 && (t[0] == '-' || t[0] == '+')) {
		return 0, errors.New("nan")
	}
	if neg {
		n = -n
	}
	return n, nil
}

func clampJS(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// ---------- listModels (Node LLMClient.listModels) ----------

var llmHTTP = &http.Client{Timeout: 61 * time.Second}

func isTimeoutErr(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	if strings.Contains(strings.ToLower(err.Error()), "timeout") || strings.Contains(err.Error(), "context deadline exceeded") {
		return true
	}
	return false
}

// listModels — Node parity: base defaults, /v1 for anthropic, headers,
// error codes (llm-timeout / auth / llm-bad-model / llm-error) + messages.
func listModels(base, apiType, apiKey string) ([]string, error) {
	defaultBase := "https://api.openai.com/v1"
	if apiType == "anthropic" {
		defaultBase = "https://api.anthropic.com"
	}
	b := base
	if b == "" {
		b = defaultBase
	}
	b = stripSlash.ReplaceAllString(b, "")
	isAnt := apiType == "anthropic"
	uStr := b + "/models"
	if isAnt {
		uStr = b + "/v1" + "/models"
	}
	req, err := http.NewRequest("GET", uStr, nil)
	if err != nil {
		return nil, &llmError{code: "llm-error", message: "Could not reach " + uStr + " (" + err.Error() + ")"}
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("user-agent", "overleaf-llm-module")
	if isAnt {
		req.Header.Set("x-api-key", apiKey)
		if req.Header.Get("x-api-key") == "" {
			req.Header.Set("x-api-key", "overleaf-local")
		}
		req.Header.Set("anthropic-version", "2023-06-01")
	} else if apiKey != "" {
		req.Header.Set("authorization", "Bearer "+apiKey)
	}
	resp, doErr := llmHTTP.Do(req)
	if doErr != nil {
		if isTimeoutErr(doErr) {
			return nil, &llmError{code: "llm-timeout", message: "Timed out listing models at " + uStr}
		}
		inner := "network error"
		if msg, ok := innerErrMsg(doErr); ok {
			inner = msg
		}
		return nil, &llmError{code: "llm-error", message: "Could not reach " + uStr + " (" + inner + ")"}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		sb := string(body)
		if len(sb) > 120 {
			sb = jsSlice(sb, 120)
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			msg := fmt.Sprintf("The provider rejected the API key (HTTP %d)", resp.StatusCode)
			if sb != "" {
				msg += " — " + sb
			}
			return nil, &llmError{code: "auth", message: msg, status: resp.StatusCode}
		}
		code := "llm-error"
		if resp.StatusCode == 404 {
			code = "llm-bad-model"
		}
		msg := fmt.Sprintf("Backend returned %d", resp.StatusCode)
		if sb != "" {
			msg += ": " + sb
		}
		return nil, &llmError{code: code, message: msg, status: resp.StatusCode}
	}
	var dec any
	if jerr := json.Unmarshal(bodyRead(resp), &dec); jerr != nil {
		return nil, &llmError{code: "llm-error", message: jerr.Error()}
	}
	m, _ := dec.(map[string]any)
	list := m["data"]
	if list == nil {
		list = m["models"]
	}
	out := make([]string, 0)
	if arr, ok := list.([]any); ok {
		for _, mm := range arr {
			var s string
			switch t := mm.(type) {
			case string:
				s = t
			case map[string]any:
				if id, ok := t["id"].(string); ok && id != "" {
					s = id
				} else if nm, ok := t["name"].(string); ok {
					s = nm
				}
			}
			if s != "" {
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func bodyRead(resp *http.Response) []byte {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return b
}

// innerErrMsg — Node `err?.message || 'network error'` for a Go *url.Error.
// Node: 'fetch failed'. Go: `Get "https://x/y": <cause>` — the gate
// normalizes everything after "( ", so emit the Go cause verbatim.
func innerErrMsg(err error) (string, bool) {
	msg := err.Error()
	if i := strings.LastIndex(msg, `": `); i != -1 && (strings.HasPrefix(msg, `Get "`) || strings.HasPrefix(msg, `Post "`)) {
		return msg[i+4:], true
	}
	return msg, true
}

// ---------- chatText (Node LLMClient.chatText + wrapError) ----------

// chatProbe — the lightweight chat probe used by the BYO /check success
// path. Error mapping mirrors wrapError exactly (battery: never reached —
// listModels failures short-circuit first — but kept faithful).
func chatProbe(base, apiType, apiKey, model string) (string, error) {
	b := strings.TrimSpace(base)
	b = stripSlash.ReplaceAllString(b, "")
	isAnt := apiType == "anthropic"
	var uStr string
	var payload map[string]any
	if isAnt {
		if b == "" {
			b = "https://api.anthropic.com"
		}
		uStr = b + "/v1/messages"
		payload = map[string]any{
			"model":      model,
			"max_tokens": 16,
			"messages":   []any{map[string]any{"role": "user", "content": "Reply with the single word OK."}},
		}
	} else {
		if b == "" {
			b = "https://api.openai.com/v1"
		}
		uStr = b + "/chat/completions"
		payload = map[string]any{
			"model":       model,
			"messages":    []any{map[string]any{"role": "user", "content": "Reply with the single word OK."}},
			"max_tokens":  16,
			"temperature": 0,
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", &llmError{code: "llm-error", message: "LLM request failed: " + err.Error()}
	}
	client := &http.Client{Timeout: 61 * time.Second}
	req, err := http.NewRequest("POST", uStr, strings.NewReader(string(raw)))
	if err != nil {
		return "", &llmError{code: "llm-error", message: fmt.Sprintf("LLM request failed: %s (model: %s)", err.Error(), model)}
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("user-agent", "overleaf-llm-module")
	if isAnt {
		req.Header.Set("anthropic-version", "2023-06-01")
		key := apiKey
		if key == "" {
			key = "overleaf-local"
		}
		req.Header.Set("x-api-key", key)
	} else if apiKey != "" {
		req.Header.Set("authorization", "Bearer "+apiKey)
	}
	resp, doErr := client.Do(req)
	if doErr != nil {
		return "", &llmError{code: "llm-error", message: fmt.Sprintf("LLM request failed: %s (model: %s)", doErr.Error(), model)}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		switch resp.StatusCode {
		case 401, 403:
			return "", &llmError{code: "auth", message: "The provider rejected the API key (HTTP 401/403). Check the key and that it has not expired."}
		case 404:
			return "", &llmError{code: "llm-bad-model", message: fmt.Sprintf("Model not found on the backend (404) (model: %s). Re-scan the provider model list (Account → LLM settings → Scan) and select an available model.", model)}
		case 429:
			return "", &llmError{code: "llm-rate-limited", message: "Rate limited by LLM backend (429)"}
		default:
			return "", &llmError{code: "llm-error", message: fmt.Sprintf("LLM backend error (HTTP %d) for model: %s. This is usually transient provider overload — retry, or select a different model.", resp.StatusCode, model)}
		}
	}
	var dec any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&dec); err != nil {
		return "", nil
	}
	m, _ := dec.(map[string]any)
	if choices, ok := m["choices"].([]any); ok && len(choices) > 0 {
		if c0, ok := choices[0].(map[string]any); ok {
			if msg, ok := c0["message"].(map[string]any); ok {
				if s, ok := msg["content"].(string); ok {
					// Node stripThinkTags + assertNonEmpty (empty -> 'empty-response')
					s = regexp.MustCompile(`(?i)</?think[^>]*>`).ReplaceAllString(s, "")
					if strings.TrimSpace(s) == "" {
						return "", &llmError{code: "empty-response", message: "The model returned no visible text. Reasoning models may spend the whole output budget on thinking - raise the output budget or disable reasoning for this task."}
					}
					return s, nil
				}
			}
		}
	}
	if content, ok := m["content"].([]any); ok && len(content) > 0 {
		if c0, ok := content[0].(map[string]any); ok {
			if s, ok := c0["text"].(string); ok {
				if strings.TrimSpace(s) == "" {
					return "", &llmError{code: "empty-response", message: "The model returned no visible text. Reasoning models may spend the whole output budget on thinking - raise the output budget or disable reasoning for this task."}
				}
				return s, nil
			}
		}
	}
	return "", nil
}

// ---------- BYO check / scan loops (Node LLMSettingsController) ----------

// candidateTypes — [requested, ...others]
func candidateTypes(requested string) []string {
	out := []string{requested}
	for _, t := range providerTypes {
		if t != requested {
			out = append(out, t)
		}
	}
	return out
}

// checkLoopOutcome — the /check failure shape.
type byoFail struct {
	status  int
	code    string
	message string
	details bool // true -> body uses `details` (+models); false -> `message`
	models  []string
}

// byoCheck — Node checkProviderConnection core loop + failure mapping.
// (success handled by caller for the 200 body).
func byoCheck(baseURL, apiKey, requestedType, model string) (models []string, detType string, success bool, fail *byoFail) {
	// Node: each type -> normalize (throws -> lastError continue) ->
	// listModels (throws -> lastError continue) -> model-missing ->
	// DEFINITIVE 404 (returned, no fallthrough) -> chatText (throws ->
	// lastError continue) -> success.
	var lastErr *llmError
	for _, typ := range candidateTypes(requestedType) {
		spec, nerr := normalizeSpec(typ, baseURL, apiKey, func() string {
			if model != "" {
				return model
			}
			return "qwen"
		}())
		if nerr != nil {
			le, _ := nerr.(*llmError)
			if le != nil {
				lastErr = le
			}
			_ = spec
			continue
		}
		ids, lerr := listModels(spec["baseUrl"], spec["providerType"], spec["apiKey"])
		if lerr != nil {
			le, _ := lerr.(*llmError)
			if le != nil {
				lastErr = le
			}
			continue
		}
		models = ids[:min(len(ids), 500)]
		if model != "" && !containsString(models, model) {
			return nil, "", false, &byoFail{status: 404, code: "model-not-found", details: true, models: models, message: fmt.Sprintf(`Model "%s" not available`, model)}
		}
		cm := model
		if cm == "" && len(models) > 0 {
			cm = models[0]
		}
		if cm == "" {
			cm = "qwen"
		}
		spec2, _ := normalizeSpec(typ, baseURL, apiKey, cm)
		if _, cerr := chatProbe(spec2["baseUrl"], spec2["providerType"], spec2["apiKey"], cm); cerr != nil {
			le, _ := cerr.(*llmError)
			if le != nil {
				lastErr = le
			}
			continue
		}
		detType = typ
		return models, detType, true, nil
	}
	// failure mapping: auth -> 401; else err.status or 500.
	status := 500
	if lastErr != nil {
		if lastErr.code == "auth" {
			status = 401
		} else if lastErr.status != 0 {
			status = lastErr.status
		}
		return nil, "", false, &byoFail{status: status, code: lastErr.code, message: lastErr.message}
	}
	return nil, "", false, &byoFail{status: 500, code: "llm-error", message: "LLM request failed"}
}

// byoScan — Node scanProviderModels core loop (no model check, no chat).
func byoScan(baseURL, apiKey, requestedType string) (models []string, detType string, success bool, fail *byoFail) {
	var lastErr *llmError
	for _, typ := range candidateTypes(requestedType) {
		spec, nerr := normalizeSpec(typ, baseURL, apiKey, "scan")
		if nerr != nil {
			le, _ := nerr.(*llmError)
			if le != nil {
				lastErr = le
			}
			_ = spec
			continue
		}
		ids, lerr := listModels(spec["baseUrl"], spec["providerType"], spec["apiKey"])
		if lerr != nil {
			le, _ := lerr.(*llmError)
			if le != nil {
				lastErr = le
			}
			continue
		}
		m := ids
		if len(m) > 500 {
			m = m[:500]
		}
		return m, typ, true, nil
	}
	status := 500
	if lastErr != nil {
		if lastErr.code == "auth" {
			status = 401
		} else if lastErr.status != 0 {
			status = lastErr.status
		}
		return nil, "", false, &byoFail{status: status, code: lastErr.code, message: lastErr.message}
	}
	return nil, "", false, &byoFail{status: 500, code: "llm-error", message: "LLM request failed"}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

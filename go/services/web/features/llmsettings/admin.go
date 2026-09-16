package llmsettings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ollitex/go/services/web/core"
)

// ---------- default prompts (LLMPrompts.mjs, byte-exact) ----------

const defAskAiSystemPrompt = `You are a LaTeX writing assistant embedded in an editor. Preserve existing LaTeX commands, math, and citation keys exactly. Reply in the same language as the input text, unless the request explicitly names a target language (e.g. a translation) — in that case use the target language. When asked to rewrite or transform text, return only the resulting text, with no preamble and no Markdown code fences.`

const defErrorPrompt = "**Please help me:**\n1. In one or two sentences, what this error actually means\n2. The exact line(s) with the problem and why\n3. The minimal corrected code (only the changed lines, in a ```latex block)\n4. One short tip to avoid it in the future\nKeep the answer compact — a precise two-line fix beats a long explanation."

const defReviewSystemPrompt = `You are a meticulous reviewer that checks whether a LaTeX document complies with writing guidelines for academic theses and internship reports.

You will receive:
1. DOCUMENT: the full LaTeX source of the project, split into files, each introduced by a line "% ===== FILE: <path> =====".
2. Possibly SCAN HINTS: mechanical pattern-scan results computed in code from the source, exhaustive for their listed patterns. TRUST their counts and their "none found" statements (they beat your own reading for those patterns). The listed candidates over-capture on purpose: judge each one in context before counting it as a violation.
3. GUIDELINES: the requirement(s) to check in THIS pass. Judge ONLY these requirements; every other aspect of the document is out of scope here.

Be strict and skeptical. "ok" means you actually verified the requirement, not that you found related-looking text. Use the "analysis" field as your worksheet, BEFORE judging: when a requirement covers every figure, table or citation, walk through them there one by one (a compact enumeration in "analysis" is encouraged: writing it out is how you verify). When nothing is wrong a count suffices; enumerate when you are checking item by item. For a requirement asserting an ABSENCE (nothing of some kind exists), state what you scanned and how completely. If you could not verify exhaustively, say so and use "partial" instead of "ok". Keep "evidence" compact regardless: it is the part the user reads.

Evidence rules:
- The evidence must actually support the verdict: quote text that CONTAINS the thing you are judging, with the file path from the nearest "FILE:" header. Never quote unrelated text just to fill the field.
- For a requirement that is not satisfied, quote the offending text; if it occurs in several places, list up to five, separated by " | ".
- A quote cannot prove an absence: for absence requirements the evidence must describe the scan (for example "scanned all 31 entries in references.bib, none points to Wikipedia").
- Report counts plus at most five short examples, never a full enumeration. Keep each item's evidence under about 500 characters: it is the part the user reads, and pages of pasted source make the report unreadable.
- NEVER mention line numbers or equation numbers: the source you receive has neither, so any you produce would be invented. Locate only by file path and verbatim quote.
- For "na", state briefly why it cannot be verified from the source.

Reply in the same language as the GUIDELINES (for example, in Italian if the guidelines are in Italian). This includes the "suggestion" field.

Length discipline: keep each "analysis" under about 2000 characters — it is a worksheet, not a transcript; if the enumeration is long, summarize it ("checked items 1-31: 4 problems, listed below").

Return ONLY a JSON object, with no preamble, no explanation, and no code fences, in exactly this shape:
{
  "items": [
    { "analysis": "what you scanned and what you found, written before judging", "requirement": "the guideline requirement, restated concisely", "status": "ok", "evidence": "file path and verbatim quote(s), or the description of the scan", "suggestion": "a concrete suggestion to satisfy it (empty string when status is ok)" }
  ]
}
Use "ok" when clearly satisfied, "partial" when partially satisfied or only partially verified, "missing" when not satisfied, "na" when not applicable or impossible to verify from the source.`

var defActionPrompts = []struct{ k, v string }{
	{"paraphrase", `Paraphrase the following LaTeX text. Keep every LaTeX command, math, and citation key intact. Output only the paraphrased text, with no preamble, no explanation, and no code fences. Do not call, name, or simulate any tool, function, or API — answer directly with the text.

{{selection}}`},
	{"academic", `Rewrite the following LaTeX text in fluent, formal academic English. Preserve every LaTeX command, math, and citation key. Output only the rewritten text, with no preamble and no code fences. Do not call, name, or simulate any tool, function, or API — answer directly with the text.

{{selection}}`},
	{"concise", `Rewrite the following LaTeX text more concisely, preserving its meaning and every LaTeX command, math, and citation. Output only the rewritten text, nothing else. Do not call, name, or simulate any tool, function, or API — answer directly with the text.

{{selection}}`},
	{"translate", `Translate the following LaTeX text into {{language}}. Keep every LaTeX command, math, and citation key exactly as-is (translate only the natural-language text). Output only the translated text, with no preamble and no code fences. Do not call, name, or simulate any tool, function, or API — answer directly with the translated text.

{{selection}}`},
	{"synonyms", `Suggest two or three well-chosen academic synonyms or phrasings for each key content word in the following LaTeX text, one per line, in the format "word — alternative(s)". Keep every LaTeX command intact and do not rewrite the whole text. Output only the list, with no preamble and no code fences, and do not call or simulate any tool or function.

{{selection}}`},
}

// actionPromptsObj — DEFAULT_ASK_AI_ACTION_PROMPTS (Node key order).
func actionPromptsObj() obj {
	out := obj{}
	for _, p := range defActionPrompts {
		out = out.set(p.k, p.v)
	}
	return out
}

// mergeActionPrompts — Node parity.
func mergeActionPrompts(stored any) obj {
	out := actionPromptsObj()
	if o, ok := stored.(obj); ok {
		for _, p := range defActionPrompts {
			if v, ok := o.get(p.k); ok {
				if s, isStr := v.(string); isStr {
					out = out.set(p.k, s)
				}
			}
		}
	}
	return out
}

// ---------- admin settings file (Node LLMAdminController file IO) ----------

// adminFile — parsed admin settings (ordered; missing file = empty obj).
type adminFile struct {
	raw obj
}

func (a adminFile) get(key string) (any, bool) { return a.raw.get(key) }

func (a adminFile) has(key string) bool { return a.raw.has(key) }

func (a adminFile) str(key string) string {
	v, ok := a.raw.get(key)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func (a adminFile) getBool(key string) bool {
	v, ok := a.raw.get(key)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func (a adminFile) arr(key string) []any {
	v, ok := a.raw.get(key)
	if !ok {
		return nil
	}
	a2, _ := v.([]any)
	return a2
}

func (a adminFile) num(key string) float64 {
	v, ok := a.raw.get(key)
	if !ok {
		return 0
	}
	f, _ := v.(float64)
	return f
}

// readAdminFile — Node readAdminSettings: ENOENT -> {}; parse error -> {}
// (warn-and-empty).
func readAdminFile(path string) adminFile {
	raw, err := os.ReadFile(path)
	if err != nil {
		return adminFile{}
	}
	v, perr := ojsonParse(string(raw))
	if perr != nil {
		return adminFile{}
	}
	o, ok := v.(obj)
	if !ok {
		return adminFile{}
	}
	return adminFile{raw: o}
}

// writeAdminFile — Node writeAdminSettings: mkdir -p; JSON.stringify(data,
// null, 2) (NO trailing newline); chmod 600; atomic rename.
func writeAdminFile(path string, data obj) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp-%d-%d", path, os.Getpid(), time.Now().UnixMilli())
	body := jsonIndent(data)
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

var _ = strings.ToLower

// ---------- feature flags + compliance (shared admin-file readers) ----------

// featureFlags — Node getLLMFeatureFlags.
func featureFlags() map[string]bool {
	s := readAdminFile(envOrDefault("LLM_ADMIN_SETTINGS_PATH", "/var/lib/overleaf/data/llm-admin-settings.json"))
	return map[string]bool{
		"chatEnabled":       s.getBool("chatEnabled") != false,
		"completionEnabled": s.getBool("completionEnabled") != false,
		"reviewEnabled":     s.getBool("reviewEnabled") != false,
	}
}

// getAdminLLMSettings — Node (env fallback + decrypt).
func getAdminLLMSettings() map[string]any {
	s := readAdminFile(envOrDefault("LLM_ADMIN_SETTINGS_PATH", "/var/lib/overleaf/data/llm-admin-settings.json"))
	envModels := envModelList()
	jsonHasModels := len(s.arr("allowedModels")) > 0
	allowed := s.arr("allowedModels")
	if !jsonHasModels {
		allowed = anySlice(envModels)
	}
	apiUrl := s.str("llmApiUrl")
	if apiUrl == "" {
		apiUrl = lEnv("LLM_API_URL")
	}
	apiType := s.str("llmApiType")
	if apiType == "" {
		apiType = lEnv("LLM_API_TYPE")
	}
	if apiType == "" {
		u := s.str("llmApiUrl")
		if u == "" {
			u = lEnv("LLM_API_URL")
		}
		apiType = detectProviderType(u)
	}
	apiKey := ""
	if v := s.str("llmApiKey"); v != "" {
		apiKey = cryptoNewForFile().storedToPlaintext(v)
	}
	if apiKey == "" {
		apiKey = lEnv("LLM_API_KEY")
	}
	cm := s.str("completionModel")
	rm := s.str("reviewModel")
	mct := s.num("maxContextTokens")
	if mct == 0 {
		mct = 32000
	}
	rmt := s.num("reviewMaxTokens")
	if rmt == 0 {
		rmt = float64(defaultReviewMaxTokens())
	}
	return map[string]any{
		"llmApiUrl":         strOrEnv(apiUrl),
		"llmApiType":        apiType,
		"llmApiKey":         strOrEnv(apiKey),
		"allowedModels":     allowed,
		"completionModel":   cm,
		"reviewModel":       rm,
		"maxContextTokens":  int64(mct),
		"reviewMaxTokens":   int64(rmt),
		"chatEnabled":       s.getBool("chatEnabled") != false,
		"completionEnabled": s.getBool("completionEnabled") != false,
		"reviewEnabled":     s.getBool("reviewEnabled") != false,
	}
}

func cryptoNewForFile() *lCrypto { return newLCrypto() }

func strOrEnv(s string) any {
	if s == "" {
		return nil // Node: ... || null
	}
	return s
}

func anySlice(a []string) []any {
	out := make([]any, 0, len(a))
	for _, s := range a {
		out = append(out, s)
	}
	return out
}

func envOrDefault(over, dflt string) string {
	if v := lEnv(over); v != "" {
		return v
	}
	return dflt
}

// ---------- admin handlers ----------

func (f *fs) adminGuard(cxt *core.Cxt, res *core.Res) bool {
	return f.app.RequireSiteAdmin(cxt, res)
}

// GET /admin/llm/settings/json — Node buildDisplaySettings (exact order).
func (f *fs) adminGet(cxt *core.Cxt, res *core.Res) {
	if !f.adminGuard(cxt, res) {
		return
	}
	s := readAdminFile(f.adminPath())
	envModels := envModelList()
	jsonHasModels := len(s.arr("allowedModels")) > 0
	allowedModels := s.arr("allowedModels")
	if !jsonHasModels {
		allowedModels = anySlice(envModels)
	}
	kmDefault := anySlice(envModels)
	if jsonHasModels {
		kmDefault = s.arr("allowedModels")
	}
	knownModels := s.arr("knownModels")
	if len(knownModels) == 0 {
		knownModels = kmDefault
	}
	apiUrl := s.str("llmApiUrl")
	if apiUrl == "" {
		apiUrl = lEnv("LLM_API_URL")
	}
	apiType := s.str("llmApiType")
	if apiType == "" {
		apiType = lEnv("LLM_API_TYPE")
	}
	if apiType == "" {
		u := s.str("llmApiUrl")
		if u == "" {
			u = lEnv("LLM_API_URL")
		}
		apiType = detectProviderType(u)
	}
	hasLlmApiKey := s.str("llmApiKey") != "" || lEnv("LLM_API_KEY") != ""
	rubrics := s.arr("complianceRubrics")
	if rubrics == nil {
		rubrics = []any{}
	}
	mct := s.num("maxContextTokens")
	if mct == 0 {
		mct = 32000
	}
	rmt := s.num("reviewMaxTokens")
	if rmt == 0 {
		rmt = float64(defaultReviewMaxTokens())
	}
	o := jobj(
		"systemPrompt", s.str("systemPrompt"),
		"llmApiUrl", apiUrl,
		"llmApiType", apiType,
		"hasLlmApiKey", hasLlmApiKey,
		"allowedModels", allowedModels,
		"knownModels", knownModels,
		"completionModel", s.str("completionModel"),
		"llmApiUrlFromEnv", s.str("llmApiUrl") == "" && lEnv("LLM_API_URL") != "",
		"llmApiTypeFromEnv", s.str("llmApiType") == "" && lEnv("LLM_API_TYPE") != "",
		"hasApiKeyFromEnv", s.str("llmApiKey") == "" && lEnv("LLM_API_KEY") != "",
		"allowedModelsFromEnv", !jsonHasModels && len(envModels) > 0,
		"complianceRubrics", rubrics,
		"reviewModel", s.str("reviewModel"),
		"maxContextTokens", int64(mct),
		"reviewMaxTokens", int64(rmt),
		"chatEnabled", s.getBool("chatEnabled") != false,
		"completionEnabled", s.getBool("completionEnabled") != false,
		"reviewEnabled", s.getBool("reviewEnabled") != false,
		"llmDisabledByAdmin", s.getBool("llmDisabledByAdmin") == true,
		"languageToolUrl", s.str("languageToolUrl"),
		"languageToolDisabledByAdmin", s.getBool("languageToolDisabledByAdmin") == true,
		"askAiSystemPrompt", s.str("askAiSystemPrompt"),
		"errorPrompt", s.str("errorPrompt"),
		"reviewSystemPrompt", s.str("reviewSystemPrompt"),
		"askAiActionPrompts", mergeActionPrompts(mustGet(s.raw, "askAiActionPrompts")),
	)
	promptDefaults := jobj(
		"askAiSystemPrompt", "",
		"errorPrompt", "",
		"reviewSystemPrompt", "",
		"askAiActionPrompts", actionPromptsObj(),
	)
	// effective prompts (Node: settings.X || DEFAULT)
	eAskAi := s.str("askAiSystemPrompt")
	if eAskAi == "" {
		eAskAi = defAskAiSystemPrompt
	}
	o = o.set("askAiSystemPrompt", eAskAi)
	eErr := s.str("errorPrompt")
	if eErr == "" {
		eErr = defErrorPrompt
	}
	o = o.set("errorPrompt", eErr)
	eRev := s.str("reviewSystemPrompt")
	if eRev == "" {
		eRev = defReviewSystemPrompt
	}
	o = o.set("reviewSystemPrompt", eRev)
	// promptDefaults with the real defaults (positions after, Node order)
	promptDefaults = jobj(
		"askAiSystemPrompt", defAskAiSystemPrompt,
		"errorPrompt", defErrorPrompt,
		"reviewSystemPrompt", defReviewSystemPrompt,
		"askAiActionPrompts", actionPromptsObj(),
	)
	_ = promptDefaults
	o = o.set("promptDefaults", jobj(
		"askAiSystemPrompt", defAskAiSystemPrompt,
		"errorPrompt", defErrorPrompt,
		"reviewSystemPrompt", defReviewSystemPrompt,
		"askAiActionPrompts", actionPromptsObj(),
	))
	jres(200, res, o)
}

// mustGet — value or nil.
func mustGet(o obj, key string) any {
	v, _ := o.get(key)
	return v
}

// orInt — parseInt-like: stored token -> int (NaN -> dflt).
func orInt(s string, dflt int) int {
	if s == "" {
		return dflt
	}
	n, err := parseIntJS(s)
	if err != nil {
		return dflt
	}
	return n
}

// POST /admin/llm/settings — Node saveAdminSettings (exact merge + write).
//
// Node: `updatedSettings = { ...existing, <explicit keys in code order> }`
// — existing key ORDER is preserved; the explicit keys keep their existing
// position when present in the file and are appended (in code order) when
// new; llmApiKey is set LAST (appended last when new). jsonIndent mirrors
// JSON.stringify(data, null, 2).
func (f *fs) adminSave(cxt *core.Cxt, res *core.Res) {
	if !f.adminGuard(cxt, res) {
		return
	}
	b := f.readBody(cxt)
	body := b.objV
	parsed, issues := validateAdminSettings(body, b.isArr)
	if len(issues) > 0 {
		errList := make([]any, 0, len(issues))
		for _, is := range issues {
			errList = append(errList, jobj("field", is.path, "message", is.message))
		}
		jres(400, res, jobj("ok", false, "error", issues[0].message, "errors", errList))
		return
	}
	s := readAdminFile(f.adminPath())
	ex := s.raw
	exStr := func(k, dflt string) string {
		if v := s.str(k); v != "" {
			return v
		}
		return dflt
	}
	// rubrics
	if rubricsVal, haveRubrics := body.get("complianceRubrics"); haveRubrics {
		if a, ok := rubricsVal.([]any); ok {
			if msg := validateComplianceRubrics(a); msg != "" {
				jres(400, res, jobj("error", msg))
				return
			}
		}
	}
	var sanitizedRubrics any
	san, okSan := sanitizeComplianceRubrics(mustGet(body, "complianceRubrics"))
	if okSan {
		arr := make([]any, 0, len(san))
		for _, r := range san {
			arr = append(arr, jobj("id", r["id"], "name", r["name"], "guidelines", r["guidelines"], "scanPatterns", r["scanPatterns"]))
		}
		sanitizedRubrics = arr
	} else {
		if a := s.arr("complianceRubrics"); a != nil {
			sanitizedRubrics = a
		} else {
			sanitizedRubrics = []any{}
		}
	}
	// maxContextTokens (parseInt + clamp): present number -> clamp; present non-number ->
	// schema already rejected it; absent -> existing||32000.
	mct := float64(orInt(exStr("maxContextTokens", ""), 32000))
	if v, ok := body.get("maxContextTokens"); ok {
		if f64, isNum := v.(float64); isNum {
			mct = float64(clampJS(int(f64), 2000, 1000000))
		}
		_ = parsed
	}
	// reviewMaxTokens
	defaultRMT := float64(defaultReviewMaxTokens())
	rmt := float64(orInt(exStr("reviewMaxTokens", ""), int(defaultRMT)))
	if v, ok := body.get("reviewMaxTokens"); ok {
		if f64, isNum := v.(float64); isNum {
			rmt = float64(clampJS(int(f64), 500, 128000))
		}
	}
	// action prompts
	var sanitizedActionPrompts obj
	if v, ok := body.get("askAiActionPrompts"); ok {
		sanitizedActionPrompts = obj{}
		if o, isObj := v.(obj); isObj {
			for _, p := range defActionPrompts {
				if val, ok2 := o.get(p.k); ok2 {
					if str, isStr := val.(string); isStr {
						sanitizedActionPrompts = sanitizedActionPrompts.set(p.k, jsSlice(str, 4000))
					}
				}
			}
		}
	} else {
		if eo, isObj := ex.get("askAiActionPrompts"); isObj {
			if _, isArr := eo.([]any); !isArr {
				if o2, isObj2 := eo.(obj); isObj2 {
					sanitizedActionPrompts = o2
				}
			}
		}
		if sanitizedActionPrompts == nil {
			sanitizedActionPrompts = obj{}
		}
	}
	// string fields (typeof string ? v : existing||'')
	strField := func(key string, dflt string) string {
		if v, ok := body.get(key); ok {
			if str, isStr := v.(string); isStr {
				return str
			}
		}
		return exStr(key, dflt)
	}
	boolField := func(key string, dflt bool) bool {
		if v, ok := body.get(key); ok {
			if b2, isB := v.(bool); isB {
				return b2
			}
		}
		return dflt
	}
	// assemble: existing order preserved, explicit keys in Node code order
	updated := ex
	updated = updated.set("systemPrompt", strField("systemPrompt", ""))
	updated = updated.set("llmApiUrl", strField("llmApiUrl", ""))
	updated = updated.set("llmApiType", strField("llmApiType", ""))
	// allowedModels / knownModels
	allowedModels := []any{}
	if v, ok := body.get("allowedModels"); ok {
		if a, isA := v.([]any); isA {
			allowedModels = a
		}
	}
	if _, ok := body.get("allowedModels"); !ok {
		if a := s.arr("allowedModels"); a != nil {
			allowedModels = a
		}
	}
	updated = updated.set("allowedModels", allowedModels)
	knownModels := []any{}
	if v, ok := body.get("knownModels"); ok {
		if a, isA := v.([]any); isA {
			knownModels = a
		}
	}
	if _, ok := body.get("knownModels"); !ok {
		if a := s.arr("knownModels"); a != nil {
			knownModels = a
		} else if a := s.arr("allowedModels"); a != nil {
			knownModels = a
		}
	}
	updated = updated.set("knownModels", knownModels)
	updated = updated.set("completionModel", strField("completionModel", ""))
	updated = updated.set("complianceRubrics", sanitizedRubrics)
	updated = updated.set("reviewModel", strField("reviewModel", ""))
	updated = updated.set("maxContextTokens", mct)
	updated = updated.set("reviewMaxTokens", rmt)
	updated = updated.set("chatEnabled", boolField("chatEnabled", s.getBool("chatEnabled") != false))
	updated = updated.set("completionEnabled", boolField("completionEnabled", s.getBool("completionEnabled") != false))
	updated = updated.set("reviewEnabled", boolField("reviewEnabled", s.getBool("reviewEnabled") != false))
	updated = updated.set("llmDisabledByAdmin", boolField("llmDisabledByAdmin", s.getBool("llmDisabledByAdmin") == true))
	updated = updated.set("languageToolUrl", strField("languageToolUrl", ""))
	updated = updated.set("languageToolDisabledByAdmin", boolField("languageToolDisabledByAdmin", s.getBool("languageToolDisabledByAdmin") == true))
	updated = updated.set("askAiSystemPrompt", strField("askAiSystemPrompt", ""))
	updated = updated.set("errorPrompt", strField("errorPrompt", ""))
	updated = updated.set("reviewSystemPrompt", strField("reviewSystemPrompt", ""))
	updated = updated.set("askAiActionPrompts", sanitizedActionPrompts)
	// llmApiKey last
	if truthy(mustGet(body, "clearLlmApiKey")) {
		updated = updated.set("llmApiKey", "")
	} else if v, ok := body.get("llmApiKey"); ok {
		if str, isStr := v.(string); isStr && strings.TrimSpace(str) != "" {
			updated = updated.set("llmApiKey", f.crypto.encrypt(strings.TrimSpace(str)))
		}
	}
	if err := writeAdminFile(f.adminPath(), updated); err != nil {
		jres(500, res, jobj("ok", false, "error", "write-failed", "message", err.Error()))
		return
	}
	jres(200, res, jobj("success", true))
}

// POST /admin/llm/settings/check — Node checkAdminLLMConnection.
func (f *fs) adminCheck(cxt *core.Cxt, res *core.Res) {
	if !f.adminGuard(cxt, res) {
		return
	}
	b := f.readBody(cxt)
	body := b.objV
	adminSettings := getAdminLLMSettings()
	llmApiUrl := body.str("apiUrl")
	if llmApiUrl == "" {
		if s, ok := adminSettings["llmApiUrl"].(string); ok {
			llmApiUrl = s
		}
	}
	llmApiKey := body.str("apiKey")
	if llmApiKey == "" {
		if s, ok := adminSettings["llmApiKey"].(string); ok {
			llmApiKey = s
		}
	}
	llmApiType := body.str("apiType")
	if llmApiType == "" {
		if s, ok := adminSettings["llmApiType"].(string); ok {
			llmApiType = s
		}
	}
	if llmApiType == "" {
		u := llmApiUrl
		if u == "" {
			u = readAdminFile(f.adminPath()).str("llmApiUrl")
		}
		llmApiType = detectProviderType(u)
	}
	testModel := ""
	if a, ok := adminSettings["allowedModels"].([]any); ok && len(a) > 0 {
		if s, ok := a[0].(string); ok {
			testModel = s
		}
	}
	if testModel == "" {
		raw := lEnv("LLM_MODEL_NAME")
		if raw != "" {
			testModel = strings.TrimSpace(strings.Split(raw, ",")[0])
		}
	}
	if llmApiUrl == "" {
		jres(400, res, jobj("success", false, "error", "LLM API URL is required"))
		return
	}
	// normalizeSpecFor(url, key, type, model||'probe')
	typeName := llmApiType
	if typeName == "" {
		typeName = detectProviderType(llmApiUrl)
	}
	base := strings.TrimRight(strings.TrimSpace(llmApiUrl), "/")
	if typeName == "anthropic" {
		base = stripV.ReplaceAllString(base, "")
	}
	ids, lerr := listModels(base, typeName, llmApiKey)
	if lerr != nil {
		le, _ := lerr.(*llmError)
		status := 500
		if le != nil {
			if le.code == "auth" {
				status = 401
			} else if le.status != 0 {
				status = le.status
			}
		}
		jres(status, res, jobj(
			"success", false,
			"error", "LLM connection failed",
			"status", status,
			"details", leMsg(le, lerr),
			"models", []any{},
		))
		return
	}
	if testModel != "" && !containsString(ids, testModel) {
		jres(404, res, jobj(
			"success", false,
			"error", fmt.Sprintf("Model %q is not available on this backend", testModel),
			"status", 404,
			"models", strAny(ids),
		))
		return
	}
	jres(200, res, jobj("success", true, "message", "Connection successful", "models", strAny(ids)))
}

func leMsg(le *llmError, lerr error) string {
	if le != nil {
		return le.message
	}
	if lerr != nil {
		return lerr.Error()
	}
	return "LLM connection failed"
}

// POST /admin/llm/models — Node scanAdminModels.
func (f *fs) adminScan(cxt *core.Cxt, res *core.Res) {
	if !f.adminGuard(cxt, res) {
		return
	}
	b := f.readBody(cxt)
	body := b.objV
	adminSettings := getAdminLLMSettings()
	llmApiUrl := body.str("apiUrl")
	if llmApiUrl == "" {
		if s, ok := adminSettings["llmApiUrl"].(string); ok {
			llmApiUrl = s
		}
	}
	llmApiKey := body.str("apiKey")
	if llmApiKey == "" {
		if s, ok := adminSettings["llmApiKey"].(string); ok {
			llmApiKey = s
		}
	}
	llmApiType := body.str("apiType")
	if llmApiType == "" {
		if s, ok := adminSettings["llmApiType"].(string); ok {
			llmApiType = s
		}
	}
	if llmApiType == "" {
		u := llmApiUrl
		if u == "" {
			u = readAdminFile(f.adminPath()).str("llmApiUrl")
		}
		llmApiType = detectProviderType(u)
	}
	if llmApiUrl == "" {
		jres(400, res, jobj("success", false, "error", "Admin LLM API URL must be configured first"))
		return
	}
	base := strings.TrimRight(strings.TrimSpace(llmApiUrl), "/")
	if llmApiType == "anthropic" {
		base = stripV.ReplaceAllString(base, "")
	}
	ids, lerr := listModels(base, llmApiType, llmApiKey)
	if lerr != nil {
		le, _ := lerr.(*llmError)
		status := 500
		if le != nil {
			if le.code == "auth" {
				status = 401
			} else if le.status != 0 {
				status = le.status
			}
		}
		jres(status, res, jobj(
			"success", false,
			"error", "Model scan failed",
			"status", status,
			"details", leMsg(le, lerr),
		))
		return
	}
	jres(200, res, jobj("success", true, "models", strAny(ids)))
}

// GET /admin/llm/usage — Node usageSummary (site scope).
func (f *fs) adminUsage(cxt *core.Cxt, res *core.Res) {
	if !f.adminGuard(cxt, res) {
		return
	}
	daysQ := cxt.Req.URL.Query().Get("days")
	days, nanErr := parseIntJS(daysQ)
	daysArg := 30
	if nanErr == nil {
		daysArg = days
	}
	summary, err := getUsageSummary(cxt.Req.Context(), f.app, "", daysArg)
	if err != nil || summary == nil {
		jres(200, res, jobj("ok", false, "error", "unavailable"))
		return
	}
	jres(200, res, jobj(
		"ok", true,
		"days", summary.days,
		"calls", summary.calls,
		"inputTokens", summary.inputTokens,
		"outputTokens", summary.outputTokens,
		"totalTokens", summary.totalTokens,
		"byDay", summary.byDay,
		"byAction", summary.byAction,
		"byModel", summary.byModel,
	))
}

// GET /admin/llm/settings — Node 301 to the hub section.
func (f *fs) adminSettingsRedirect(cxt *core.Cxt, res *core.Res) {
	res.Redirect(cxt.Req, 301, "/hub#/site.llm.instance")
}

package llmsettings

import (
	"fmt"
	"regexp"
	"strings"
)

// ---------- zod v4 message parity (Node zod ^4 in this tree) ----------

// recName — "received X" part from a JS-ish value.
func recName(v any, present bool) string {
	if !present {
		return "undefined"
	}
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64, int, int64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case obj, map[string]any:
		return "object"
	}
	return "undefined"
}

func expString(v any, present bool) string {
	return "Invalid input: expected string, received " + recName(v, present)
}
func expNumber(v any, present bool) string {
	return "Invalid input: expected number, received " + recName(v, present)
}
func expBoolean(v any, present bool) string {
	return "Invalid input: expected boolean, received " + recName(v, present)
}
func expArray(v any, present bool) string {
	return "Invalid input: expected array, received " + recName(v, present)
}

const enumProviderMsg = `Invalid option: expected one of "openai"|"anthropic"|"openaiCompatible"`

// zissue — one zod issue (path joined with '.' per Node pin format).
type zissue struct {
	path    string
	message string
}

func issuesDetail(issues []zissue) string {
	parts := make([]string, 0, len(issues))
	for _, i := range issues {
		parts = append(parts, i.path+": "+i.message)
	}
	return strings.Join(parts, "; ")
}

// jsSlice — JS String.prototype.slice(0, n) (ASCII parity with the pins).
func jsSlice(s string, n int) string {
	if utf16Len(s) <= n {
		return s
	}
	i := 0
	for i < len(s) {
		i++
		if utf16Len(s[:i]) >= n {
			break
		}
	}
	return s[:i]
}

// utf16Len — JS .length (UTF-16 code units); runes as the generalization.
func utf16Len(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// ---------- rowSchema (LLMSettingsController.mjs:55-97) ----------

type rowField struct {
	name            string
	providerType    string
	baseUrl         string
	apiKey          string
	models          []string
	completionModel string
	enabled         bool
}

const (
	msgModelList = "completionModel must be one of the listed models"
	msgBaseReq   = "A base URL is required for OpenAI-compatible providers"
)

// validateRow — rowSchema.safeParse parity. Issue order = field order + the
// superRefine customs at the end:
//
//	name, providerType, baseUrl, apiKey, models, completionModel, enabled,
//	[baseUrl custom], [completionModel custom].
func validateRow(body obj, isArr bool) (*rowField, []zissue) {
	if isArr {
		return nil, []zissue{{"", "Invalid input: expected object, received array"}}
	}
	var issues []zissue
	add := func(path, msg string) { issues = append(issues, zissue{path, msg}) }

	// name: required string (trim, min 1, max 80)
	var name string
	nameOK := true
	if v, ok := body.get("name"); ok {
		if s, isStr := v.(string); isStr {
			name = strings.TrimSpace(s)
			if name == "" || utf16Len(name) > 80 {
				nameOK = false
			}
		} else {
			add("name", expString(v, true))
			nameOK = false
		}
	} else {
		add("name", expString(nil, false))
		nameOK = false
	}

	// providerType: z.enum
	providerType := ""
	ptOK := false
	if v, ok := body.get("providerType"); ok {
		if s, isStr := v.(string); isStr {
			for _, t := range providerTypes {
				if s == t {
					providerType = t
					ptOK = true
					break
				}
			}
		}
	}
	if !ptOK {
		add("providerType", enumProviderMsg)
	}

	// baseUrl: optional string (trim, max 500), default ''
	baseUrl := ""
	if v, ok := body.get("baseUrl"); ok {
		if s, isStr := v.(string); isStr {
			baseUrl = strings.TrimSpace(s)
			if utf16Len(baseUrl) > 500 {
				add("baseUrl", "Invalid input: expected string, received string")
			}
		} else if v != nil {
			add("baseUrl", expString(v, true))
		} else {
			add("baseUrl", expString(nil, true))
		}
	}

	// apiKey: optional string (trim, max 2000), default ''
	apiKeyIn := ""
	if v, ok := body.get("apiKey"); ok {
		if s, isStr := v.(string); isStr {
			apiKeyIn = strings.TrimSpace(s)
			if utf16Len(apiKeyIn) > 2000 {
				add("apiKey", "Invalid input: expected string, received string")
			}
		} else if v != nil {
			add("apiKey", expString(v, true))
		} else {
			add("apiKey", expString(nil, true))
		}
	}

	// models: required array(string trim min1 max200) min(1) max(100)
	models := make([]string, 0)
	if v, ok := body.get("models"); ok {
		if a, isArr := v.([]any); isArr {
			for i, e := range a {
				if s, isStr := e.(string); isStr {
					models = append(models, strings.TrimSpace(s))
				} else {
					add(fmt.Sprintf("models.%d", i), expString(e, e == nil && true))
				}
			}
			if len(a) == 0 {
				add("models", "Invalid input: expected array, received array")
			} else if len(a) > 100 {
				add("models", "Invalid input: expected array, received array")
			}
		} else {
			add("models", expArray(v, v != nil))
		}
	} else {
		add("models", "Invalid input: expected array, received undefined")
	}

	// completionModel: optional string (trim, max 200), default ''
	completionModel := ""
	if v, ok := body.get("completionModel"); ok {
		if s, isStr := v.(string); isStr {
			completionModel = strings.TrimSpace(s)
			if utf16Len(completionModel) > 200 {
				add("completionModel", "Invalid input: expected string, received string")
			}
		} else if v != nil {
			add("completionModel", expString(v, true))
		} else {
			add("completionModel", expString(nil, true))
		}
	}

	// enabled: optional boolean, default true
	enabled := true
	if v, ok := body.get("enabled"); ok {
		if b, isB := v.(bool); isB {
			enabled = b
		} else {
			add("enabled", expBoolean(v, v != nil))
		}
	}

	// superRefine customs (Node order: baseUrl first, then completionModel)
	if providerType == "openaiCompatible" && baseUrl == "" {
		add("baseUrl", msgBaseReq)
	}
	if completionModel != "" && !containsString(models, completionModel) {
		add("completionModel", msgModelList)
	}

	if len(issues) > 0 || !nameOK || !ptOK {
		return nil, issues
	}
	return &rowField{
		name:            name,
		providerType:    providerType,
		baseUrl:         baseUrl,
		apiKey:          apiKeyIn,
		models:          models,
		completionModel: completionModel,
		enabled:         enabled,
	}, nil
}

func containsString(a []string, s string) bool {
	for _, e := range a {
		if e == s {
			return true
		}
	}
	return false
}

// jsStrings — Node array-filter+map for string[] fields.
func jsStrings(v any) []string {
	out := make([]string, 0)
	if a, ok := v.([]any); ok {
		for _, e := range a {
			if s, isStr := e.(string); isStr && s != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

// ---------- llmSettingsSchema (LLMAdminController.mjs) ----------

// validateAdminSettings — all fields optional; wrong TYPE -> issue each
// (schema order). Returns the typed values + issues.
func validateAdminSettings(body obj, isArr bool) (map[string]any, []zissue) {
	if isArr {
		return nil, []zissue{{"", "Invalid input: expected object, received array"}}
	}
	var issues []zissue
	add := func(path, msg string) { issues = append(issues, zissue{path, msg}) }
	out := map[string]any{}

	type fieldSpec struct {
		key    string
		expect string // string | number | boolean | array | record
		maxLen int
	}
	specs := []fieldSpec{
		{"systemPrompt", "string", 4000},
		{"llmApiUrl", "string", 0},
		{"llmApiType", "string", 0},
		{"llmApiKey", "string", 0},
		{"clearLlmApiKey", "boolean", 0},
		{"allowedModels", "array", 0},
		{"knownModels", "array", 0},
		{"completionModel", "string", 0},
		{"complianceRubrics", "array", 0},
		{"reviewModel", "string", 0},
		{"maxContextTokens", "number", 0},
		{"reviewMaxTokens", "number", 0},
		{"chatEnabled", "boolean", 0},
		{"completionEnabled", "boolean", 0},
		{"reviewEnabled", "boolean", 0},
		{"llmDisabledByAdmin", "boolean", 0},
		{"languageToolUrl", "string", 2048},
		{"languageToolDisabledByAdmin", "boolean", 0},
		{"askAiSystemPrompt", "string", 8000},
		{"errorPrompt", "string", 8000},
		{"reviewSystemPrompt", "string", 8000},
		{"askAiActionPrompts", "record", 0},
	}
	for _, spec := range specs {
		v, ok := body.get(spec.key)
		if !ok {
			continue
		}
		switch spec.expect {
		case "string":
			s, isStr := v.(string)
			if !isStr {
				add(spec.key, expString(v, v != nil))
				continue
			}
			if spec.maxLen > 0 && utf16Len(s) > spec.maxLen {
				add(spec.key, "Invalid input: expected string, received string")
				continue
			}
			out[spec.key] = s
		case "number":
			if _, isNum := v.(float64); !isNum {
				add(spec.key, expNumber(v, v != nil))
				continue
			}
			out[spec.key] = v
		case "boolean":
			if _, isB := v.(bool); !isB {
				add(spec.key, expBoolean(v, v != nil))
				continue
			}
			out[spec.key] = v
		case "array":
			if _, isA := v.([]any); !isA {
				add(spec.key, expArray(v, v != nil))
				continue
			}
			out[spec.key] = v
		case "record":
			if _, isO := v.(obj); !isO {
				add(spec.key, "Invalid input: expected object, received "+recName(v, v != nil))
				continue
			}
			out[spec.key] = v
		}
	}
	return out, issues
}

// ---------- compliance rubrics (shared, LLMAdminController.mjs) ----------

// sanitizeComplianceRubrics — Node parity: cap fields, drop id-less /
// nameless, cap list at 50. Returns ok=false when `list` is not an array.
func sanitizeComplianceRubrics(list any) ([]map[string]string, bool) {
	a, ok := list.([]any)
	if !ok {
		return nil, false
	}
	out := make([]map[string]string, 0, len(a))
	for _, r := range a {
		m, _ := asMap(r)
		id := jsStrCoerce(mVal(m, "id"))
		name := jsSlice(jsStrCoerce(mVal(m, "name")), 200)
		guidelines := jsSlice(jsStrCoerce(mVal(m, "guidelines")), 20000)
		scanPatterns := jsSlice(jsStrCoerce(mVal(m, "scanPatterns")), 4000)
		if id == "" || name == "" {
			continue
		}
		out = append(out, map[string]string{
			"id":           id,
			"name":         name,
			"guidelines":   guidelines,
			"scanPatterns": scanPatterns,
		})
	}
	if len(out) > 50 {
		out = out[:50]
	}
	return out, true
}

func mVal(m map[string]any, key string) any {
	if m == nil {
		return nil
	}
	return m[key]
}

// validateComplianceRubrics — Node parity; returns the first error message
// ("" = ok). Regex compile via RE2: the pinned patterns ('([abc' invalid,
// 'wikipedia\.org' valid) compile identically under V8 and RE2 (documented
// divergence: JS-only syntax like backreferences REJECTS under RE2 — an
// extra-strict edge the pins do not exercise).
var jsRegexRe = regexp.MustCompile(".") // touch import

func validateComplianceRubrics(list any) string {
	a, ok := list.([]any)
	if !ok {
		return ""
	}
	for _, r := range a {
		m, _ := asMap(r)
		nameVal := mVal(m, "name")
		name := "?"
		if s, ok := nameVal.(string); ok && s != "" {
			name = s
		}
		patternsText := ""
		if s, ok := mVal(m, "scanPatterns").(string); ok {
			patternsText = s
		}
		if utf16Len(patternsText) > 4000 {
			return fmt.Sprintf(`Scan patterns of rubric "%s" must be 4000 characters or fewer`, name)
		}
		for _, rawLine := range strings.Split(patternsText, "\n") {
			line := strings.TrimSpace(rawLine)
			if line == "" {
				continue
			}
			sep := strings.Index(line, "::")
			body := line
			if sep != -1 {
				body = line[sep+2:]
			}
			body = strings.TrimSpace(body)
			if body == "" {
				continue
			}
			if utf16Len(body) > 200 {
				return fmt.Sprintf(`Scan pattern in rubric "%s" is too long (max 200 characters)`, name)
			}
			if _, err := regexp.Compile("(?i)" + body); err != nil {
				return fmt.Sprintf(`Invalid scan pattern regex in rubric "%s": %s`, name, jsSlice(body, 80))
			}
		}
	}
	return ""
}

var _ = jsRegexRe

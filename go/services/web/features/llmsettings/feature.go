// Package llmsettings — P6.4a: the OlliTeX "llm" module settings surface
// (services/web/modules/llm). Go drop-in for the Node handlers:
//
//	BYO provider rows        /user/llm-providers*        (LLMSettingsController.mjs)
//	selected model           /user/llm/selected-model
//	compliance rubrics       /user/llm/compliance
//	usage (user)             /user/llm-usage
//	grammar prefs            /user/llm-settings/grammar
//	legacy page 301          /user/llm-settings
//	admin settings file CRUD /admin/llm/settings*        (LLMAdminController.mjs)
//	admin models/check       /admin/llm/models, /admin/llm/settings/check
//	admin usage              /admin/llm/usage
//	legacy page 301          /admin/llm/settings
//
// Node authority files (byte pins: /tmp/p64a_node.json):
//
//	LLMSettingsController.mjs, LLMAdminController.mjs, LLMCrypto.mjs,
//	LLMClient.mjs, LLMUsage.mjs, LLMGrammar.mjs, LLMPrompts.mjs.
//
// Conventions (same as P6.3b):
//   - responses are hand-built JSON in the Node exact key order;
//   - the admin settings file is read/written with Node key order +
//     JSON.stringify(data, null, 2) semantics (ojson.go);
//   - anonymous / member-denied responses come from the core global chain;
//     admin handlers only see site-admin requests (core.RequireSiteAdmin).
package llmsettings

import (
	"context"
	"crypto/rand"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"ollitex/go/services/web/core"
)

var (
	reProviderPat = regexp.MustCompile(`^/user/llm-providers/([^/]+)$`)
	reProviderDel = regexp.MustCompile(`^/user/llm-providers/([^/]+)/delete$`)

	// P6.4b — project-scoped llm routes (LLMRouter.mjs, ensureUserCanReadProject
	// chain). First capture = Project_id; second (status/cancel) = jobId.
	reLLMProj = regexp.MustCompile(`^/project/([0-9a-fA-F]{24})/llm/%s$`)
)

// llmProjPath — compile the shared shape per segment (Node's exact paths).
var (
	reLLMModels     = regexp.MustCompile(`^/project/([0-9a-fA-F]{24})/llm/models$`)
	reLLMFeatures   = regexp.MustCompile(`^/project/([0-9a-fA-F]{24})/llm/features$`)
	reLLMSrcCtx     = regexp.MustCompile(`^/project/([0-9a-fA-F]{24})/llm/source-context$`)
	reLLMPrompts    = regexp.MustCompile(`^/project/([0-9a-fA-F]{24})/llm/prompts$`)
	reLLMChat       = regexp.MustCompile(`^/project/([0-9a-fA-F]{24})/llm/chat$`)
	reLLMCompletion = regexp.MustCompile(`^/project/([0-9a-fA-F]{24})/llm/completion$`)
	reLLMCompileFix = regexp.MustCompile(`^/project/([0-9a-fA-F]{24})/llm/compile-fix$`)
	reLLMGrammar    = regexp.MustCompile(`^/project/([0-9a-fA-F]{24})/llm/grammar$`)
	reLLMGenerate   = regexp.MustCompile(`^/project/([0-9a-fA-F]{24})/llm/generate$`)
	reLLMRubrics    = regexp.MustCompile(`^/project/([0-9a-fA-F]{24})/llm/compliance/rubrics$`)
	reLLMStart      = regexp.MustCompile(`^/project/([0-9a-fA-F]{24})/llm/compliance/start$`)
	reLLMStatus     = regexp.MustCompile(`^/project/([0-9a-fA-F]{24})/llm/compliance/status/([^/]+)$`)
	reLLMCancel     = regexp.MustCompile(`^/project/([0-9a-fA-F]{24})/llm/compliance/cancel/([^/]+)$`)
)

var _ = reLLMProj

// ---------- constants (Node sources) ----------

var providerTypes = []string{"openai", "anthropic", "openaiCompatible"}

var grammarModes = []string{"default", "lt", "llm", "lt+llm"}

const epoch0ISO = "1970-01-01T00:00:00.000Z"
const maxProvidersPerUser = 10

// ---------- Feature / routes (Node LLMRouter.mjs order) ----------

func Feature(a *core.App) core.Feature {
	fs := NewFS(a)
	return core.Feature{
		Name: "llmsettings",
		Routes: []core.Route{
			{Method: "GET", Path: "/user/llm/selected-model", Handler: fs.getSelectedModel},
			{Method: "POST", Path: "/user/llm/selected-model", Handler: fs.saveSelectedModel},
			{Method: "GET", Path: "/user/llm/compliance", Handler: fs.getUserCompliance},
			{Method: "POST", Path: "/user/llm/compliance", Handler: fs.saveUserCompliance},
			{Method: "GET", Path: "/user/llm-providers", Handler: fs.getProviders},
			{Method: "POST", Path: "/user/llm-providers", Handler: fs.addProvider},
			{Method: "POST", Path: "/user/llm-providers/check", Handler: fs.checkProviderConnection},
			{Method: "POST", Path: "/user/llm-providers/scan", Handler: fs.scanProviderModels},
			{Method: "POST", Pattern: reProviderPat, Handler: fs.updateProvider},
			{Method: "POST", Pattern: reProviderDel, Handler: fs.deleteProvider},
			{Method: "GET", Path: "/user/llm-usage", Handler: fs.userUsageSummary},
			{Method: "GET", Path: "/user/llm-settings/grammar", Handler: fs.getGrammarSettings},
			{Method: "POST", Path: "/user/llm-settings/grammar", Handler: fs.saveGrammarSettings},
			{Method: "GET", Path: "/user/llm-settings", Handler: fs.userSettingsRedirect},
			// admin block (LLMAdminController) — admin authorization is the
			// core chain (RequireSiteAdmin), mirrors AuthorizationMiddleware.
			{Method: "GET", Path: "/admin/llm/settings/json", Handler: fs.adminGet},
			{Method: "POST", Path: "/admin/llm/settings", Handler: fs.adminSave},
			{Method: "POST", Path: "/admin/llm/settings/check", Handler: fs.adminCheck},
			{Method: "POST", Path: "/admin/llm/models", Handler: fs.adminScan},
			{Method: "GET", Path: "/admin/llm/usage", Handler: fs.adminUsage},
			{Method: "GET", Path: "/admin/llm/settings", Handler: fs.adminSettingsRedirect},
			// P6.4b — project-scoped live-LLM surface (LLMChatController +
			// LLMComplianceController; login chain = core global gate).
			{Method: "GET", Pattern: reLLMModels, Handler: fs.llmGetModels},
			{Method: "GET", Pattern: reLLMFeatures, Handler: fs.llmGetFeatures},
			{Method: "GET", Pattern: reLLMSrcCtx, Handler: fs.llmSourceContext},
			{Method: "GET", Pattern: reLLMPrompts, Handler: fs.llmGetPrompts},
			{Method: "POST", Pattern: reLLMChat, Handler: fs.llmChat},
			{Method: "POST", Pattern: reLLMCompletion, Handler: fs.llmCompletion},
			{Method: "POST", Pattern: reLLMCompileFix, Handler: fs.llmCompileFix},
			{Method: "POST", Pattern: reLLMGrammar, Handler: fs.llmGrammar},
			{Method: "POST", Pattern: reLLMGenerate, Handler: fs.llmGenerate},
			{Method: "GET", Pattern: reLLMRubrics, Handler: fs.llmComplianceRubrics},
			{Method: "POST", Pattern: reLLMStart, Handler: fs.llmComplianceStart},
			{Method: "GET", Pattern: reLLMStatus, Handler: fs.llmComplianceStatus},
			{Method: "POST", Pattern: reLLMCancel, Handler: fs.llmComplianceCancel},
		},
	}
}

// fs — feature state (Node module-level closures).
type fs struct {
	app    *core.App
	crypto *lCrypto
}

// NewFS — construct (env comes from the process; tests may SetEnv first).
func NewFS(a *core.App) *fs {
	return &fs{app: a, crypto: newLCrypto()}
}

// adminPath — Node LLM_ADMIN_SETTINGS_PATH || default.
func (f *fs) adminPath() string {
	if p := lEnv("LLM_ADMIN_SETTINGS_PATH"); p != "" {
		return p
	}
	return "/var/lib/overleaf/data/llm-admin-settings.json"
}

// ---------- body reading (handler side, P6.4a) ----------

// bodyIn — Node request-body semantics for these handlers.
//
// core pre-pass: JSON-CT POST/PUT/PATCH/DELETE bodies reach here ONLY for
// `{...}`/`[...]` roots (scalar / unparseable already answered 400).
// Non-JSON CT -> Node req.body === {} (express.json skipped). The oracle
// battery always sends application/json + a JSON payload, '{}' included.
type bodyIn struct {
	isArr bool
	objV  obj
}

func (f *fs) readBody(cxt *core.Cxt) bodyIn {
	out := bodyIn{}
	ct := cxt.Req.Header.Get("Content-Type")
	if !strings.Contains(strings.ToLower(ct), "json") || cxt.Req.Body == nil {
		return out
	}
	raw, err := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
	if err != nil || len(raw) == 0 {
		return out
	}
	v, perr := ojsonParse(string(raw))
	if perr != nil {
		return out
	}
	switch t := v.(type) {
	case []any:
		out.isArr = true
		_ = t
	case obj:
		out.objV = t
	}
	return out
}

// ---------- user document helpers ----------

// userDoc — the projections the llm controller reads.
type userDoc struct {
	found               bool
	llmProviders        []any // stored rows (filter r.id && Array.isArray(r.models))
	llmApiUrl           any
	llmApiType          any
	llmApiKey           any
	llmModelName        any
	llmModels           any
	llmModelNames       any
	llmCompletionModel  any
	llmCompletionModels any
	useOwnLLMSettings   any
	selectedModel       any
	compRubrics         any
	grammar             any
}

func (f *fs) loadUser(ctx context.Context, uidHex string) (*userDoc, error) {
	db, err := f.app.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	oid, err := primitive.ObjectIDFromHex(uidHex)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	err = db.Collection("users").FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return &userDoc{}, nil
		}
		return nil, err
	}
	u := &userDoc{
		found:               true,
		llmApiUrl:           doc["llmApiUrl"],
		llmApiType:          doc["llmApiType"],
		llmApiKey:           doc["llmApiKey"],
		llmModelName:        doc["llmModelName"],
		llmModels:           doc["llmModels"],
		llmModelNames:       doc["llmModelNames"],
		llmCompletionModel:  doc["llmCompletionModel"],
		llmCompletionModels: doc["llmCompletionModels"],
		useOwnLLMSettings:   doc["useOwnLLMSettings"],
		selectedModel:       doc["llmSelectedModel"],
		compRubrics:         doc["llmComplianceRubrics"],
		grammar:             doc["grammar"],
	}
	if arr := asAnySlice(doc["llmProviders"]); arr != nil {
		u.llmProviders = arr
	}
	return u, nil
}

func (f *fs) usersColl(ctx context.Context) (*mongo.Collection, error) {
	db, err := f.app.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	return db.Collection("users"), nil
}

// rID — row id string (or "").
func rID(m map[string]any) string {
	if m == nil {
		return ""
	}
	s, _ := m["id"].(string)
	return s
}

// asMap — tolerant map extraction (bson.M / primitive.M both decode to
// map[string]any in this driver's use here).
func asMap(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case primitive.M:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = x
		}
		return out, true
	case obj:
		out := make(map[string]any, len(t))
		for _, e := range t {
			out[e.k] = e.v
		}
		return out, true
	}
	return nil, false
}

// asAnySlice — tolerant BSON array extraction: the driver decodes arrays
// into interface{} targets as the NAMED type primitive.A, so a plain
// v.([]any) assertion does not match.
func asAnySlice(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case primitive.A:
		out := make([]any, len(t))
		copy(out, t)
		return out
	}
	return nil
}

func jsStrCoerce(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return jsNum(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	}
	return ""
}

func nowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// makeRowId — Node: Math.random().toString(16).slice(2,10).padEnd(8,'0');
// gate-normalized (any 8-hex string is accepted).
func makeRowId() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000"
	}
	const hexd = "0123456789abcdef"
	out := make([]byte, 0, 8)
	for _, x := range b {
		out = append(out, hexd[x&0xf], hexd[x>>4])
	}
	return string(out)
}

// durationMS — `${Date.now()-started}ms`.
func durationMS(start time.Time) string {
	ms := int(time.Since(start) / time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	return strconv.Itoa(ms) + "ms"
}

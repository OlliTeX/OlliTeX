package chat

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"ollitex/go/mongoh"
	"ollitex/go/pbhttp"
)

// Config bundles the knobs read from the environment, mirroring the Node
// service (services/chat/config/settings.defaults.cjs):
//
//	host  = LISTEN_ADDRESS || '127.0.0.1'
//	port  = 3010 (fixed — no env override)
//	mongo = MONGO_CONNECTION_STRING || mongodb://MONGO_HOST||127.0.0.1/sharelatex
type Config struct {
	Host     string
	Port     int
	MongoURI string
	DB       string
}

// WithDefaults applies the Node 1:1 env defaults.
func (c *Config) WithDefaults() {
	if c.Host == "" {
		c.Host = envOr("LISTEN_ADDRESS", "127.0.0.1")
	}
	if c.Port == 0 {
		c.Port = 3010
	}
	if c.MongoURI == "" {
		c.MongoURI = envOr("MONGO_CONNECTION_STRING", "")
		if c.MongoURI == "" {
			c.MongoURI = "mongodb://" + envOr("MONGO_HOST", "127.0.0.1") + "/sharelatex"
		}
	}
	if c.DB == "" {
		c.DB = mongoh.DBFromURI(c.MongoURI, "sharelatex")
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Service constants (Node: MessageHttpSchemas.js).
const (
	maxMessageLength    = 10 * 1024  // z.string().max(10240)
	defaultMessageLimit = 50         // DEFAULT_MESSAGE_LIMIT
	bodyLimit           = 100 * 1024 // express.json() default '100kb'
)

// utf16Len is the JS string.length equivalent (UTF-16 code units), used for
// the 10240 "bytes" content cap as Node measures it.
func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// objectIDHex is Zod zz.objectId's acceptance: 24 hex chars (case-insensitive).
var objectIDHex = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// Server is the chat HTTP service over a Store.
type Server struct {
	store Store
	// nowMs is the Date.now() equivalent; a seam so tests control timestamps.
	nowMs func() int64
	// logf receives best-effort logs (Node logger calls are fire-and-forget).
	logf func(format string, args ...any)
}

// NewServer builds a chat Server over the given Store.
func NewServer(store Store, cfg Config) *Server {
	cfg.WithDefaults() // applied for consistency; callers also pass the same values
	return &Server{
		store: store,
		nowMs: func() int64 { return time.Now().UnixMilli() },
		logf:  func(format string, args ...any) { log.Printf(format, args...) },
	}
}

// Router returns the service http.Handler with the Node 1:1 route table and
// error semantics:
//
//   - express.json() default 100kb body limit; overflow is caught by chat's
//     500 JSON error handler → 500 {"message":"Internal error: request entity
//     too large"} (verified against the Node service)
//   - validation failures → 404 {error,statusCode:404} when any path param is
//     invalid, else 400 {error,statusCode:400} (handleValidationError), with
//     the zod-validation-error text (issues joined by "; ", schema order)
//   - missing message/thread (route level) → res.sendStatus(404) → 404
//     "Not Found" (text/plain)
//   - any other unmatched route → the app.use catch-all → 404
//     {"message":"Not found"} for ALL methods
//   - handler/internal failures → 500 {"message":"Internal error: ..."}
func (s *Server) Router() http.Handler {
	recovering := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				reason := "internal error"
				switch v := rec.(type) {
				case error:
					reason = v.Error()
				case string:
					reason = v
				}
				s.logf("chat: unhandled error: %v", rec)
				pbhttp.WriteJSON(w, http.StatusInternalServerError, struct {
					Message string `json:"message"`
				}{"Internal error: " + reason})
			}
		}()
		s.dispatch(w, r)
	})
	return pbhttp.LimitBodyJSON(recovering, bodyLimit, http.StatusInternalServerError, "Internal error: request entity too large")
}

// ---- routing ----------------------------------------------------------------

type route struct {
	method  string
	segs    []string // leading ':' = path param
	handler func(w http.ResponseWriter, r *http.Request, params map[string]string)
}

// routes mirrors the registration order in services/chat/app.js. (All routes
// are unambiguous by segment count + static words, so first-match order cannot
// change behaviour — mirrored anyway.)
func (s *Server) routes() []route {
	return []route{
		{"GET", []string{"project", ":projectId", "messages"}, s.getGlobalMessages},
		{"POST", []string{"project", ":projectId", "messages"}, s.sendGlobalMessage},
		{"GET", []string{"project", ":projectId", "messages", ":messageId"}, s.getGlobalMessage},
		{"DELETE", []string{"project", ":projectId", "messages", ":messageId"}, s.deleteGlobalMessage},
		{"POST", []string{"project", ":projectId", "messages", ":messageId", "edit"}, s.editGlobalMessage},
		{"POST", []string{"project", ":projectId", "thread", ":threadId", "messages"}, s.sendMessage},
		{"GET", []string{"project", ":projectId", "thread", ":threadId", "messages", ":messageId"}, s.getThreadMessage},
		{"DELETE", []string{"project", ":projectId", "thread", ":threadId", "messages", ":messageId"}, s.deleteMessage},
		{"POST", []string{"project", ":projectId", "thread", ":threadId", "messages", ":messageId", "edit"}, s.editMessage},
		{"DELETE", []string{"project", ":projectId", "thread", ":threadId", "user", ":userId", "messages", ":messageId"}, s.deleteUserMessage},
		{"POST", []string{"project", ":projectId", "thread", ":threadId", "resolve"}, s.resolveThread},
		{"POST", []string{"project", ":projectId", "thread", ":threadId", "reopen"}, s.reopenThread},
		{"GET", []string{"project", ":projectId", "thread", ":threadId"}, s.getThread},
		{"DELETE", []string{"project", ":projectId", "thread", ":threadId"}, s.deleteThread},
		{"GET", []string{"project", ":projectId", "threads"}, s.getThreads},
		{"GET", []string{"project", ":projectId", "resolved-thread-ids"}, s.getResolvedThreadIds},
		{"DELETE", []string{"project", ":projectId"}, s.destroyProject},
		{"POST", []string{"project", ":projectId", "duplicate-comment-threads"}, s.duplicateCommentThreads},
		{"POST", []string{"project", ":projectId", "generate-thread-data"}, s.generateThreadData},
		{"POST", []string{"project", ":projectId", "clone-comment-threads"}, s.cloneCommentThreads},
		{"GET", []string{"status"}, s.status},
	}
}

// dispatch mirrors Express matching: first route whose method AND path match
// wins (HEAD is served by the GET routes, like Express); otherwise the
// app.use catch-all → 404 {"message":"Not found"} (all methods).
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	segs := pathSegments(r.URL.Path)
	methods := []string{r.Method}
	if r.Method == http.MethodHead {
		methods = append(methods, http.MethodGet)
	}
	for _, m := range methods {
		for _, rt := range s.routes() {
			if rt.method != m {
				continue
			}
			if params, ok := matchPath(rt.segs, segs); ok {
				rt.handler(w, r, params)
				return
			}
		}
	}
	pbhttp.WriteJSON(w, http.StatusNotFound, struct {
		Message string `json:"message"`
	}{"Not found"})
}

func pathSegments(p string) []string {
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1] // Express non-strict: trailing slash optional
	}
	if p == "" || p == "/" {
		return []string{""}
	}
	return strings.Split(strings.TrimPrefix(p, "/"), "/")
}

func matchPath(route, req []string) (map[string]string, bool) {
	if len(route) != len(req) {
		return nil, false
	}
	params := map[string]string{}
	for i, rs := range route {
		if strings.HasPrefix(rs, ":") {
			params[rs[1:]] = req[i]
		} else if rs != req[i] {
			return nil, false
		}
	}
	return params, true
}

// ---- response helpers (Express-equivalent) ----------------------------------

// sendStatus mirrors res.sendStatus(code): express sets text/plain + the
// StatusText body (verified against the Node service: "Not Found",
// Content-Type: text/plain; charset=utf-8).
func sendStatus(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, http.StatusText(code))
}

type validationErr struct {
	Error      string `json:"error"`
	StatusCode int    `json:"statusCode"`
}

// writeValidationError mirrors handleValidationError 1:1:
// res.status(code).json({ error: fromError(zodError).toString(), statusCode })
// with code 404 when any issue is rooted at params, else 400. All issues of
// the request (params + query + body) appear, in schema order, joined by
// "; " (verified against the Node service).
func writeValidationError(w http.ResponseWriter, code int, issues []string) {
	text := "Validation error"
	if len(issues) > 0 {
		text += ": " + strings.Join(issues, "; ")
	}
	pbhttp.WriteJSON(w, code, validationErr{text, code})
}

// objectIDIssue / typeIssue build the zod-validation-error fragments observed
// from the Node service.
func objectIDIssue(path string) string { return `invalid Mongo ObjectId at "` + path + `"` }
func typeIssue(expect, got, path string) string {
	return `Invalid input: expected ` + expect + `, received ` + got + ` at "` + path + `"`
}

// ---- body reading (express.json equivalent) ----------------------------------

type bodyKind int

const (
	bodyUndefined bodyKind = iota // non-json Content-Type → req.body undefined
	bodyObject                    // valid JSON object (possibly empty)
	bodyArray                     // valid JSON array
	bodyFatal                     // top-level scalar / invalid JSON → Node 500 path
)

// jsonTypeName mirrors the values zod reports as "received".
func jsonTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// bodyParse is the result of classifying a request body the way express.json
// + the schema see it. `order` keeps the object keys in JSON appearance order
// (Go maps lose order; the raw-order list drives the strictObject
// "Unrecognized key" issue order).
type bodyParse struct {
	kind   bodyKind
	fields map[string]json.RawMessage
	order  []string
}

// readBodyClassified reproduces what express.json + the schema see:
//
//   - Content-Type must be a json type (express typeis 'json'), else the body
//     stays undefined (observed: text/plain + JSON bytes → "undefined" 400 set)
//   - empty body → {} (observed: field-missing "received undefined" errors,
//     not a syntax error)
//   - top-level scalar or invalid JSON → body-parser strict error → the
//     service 500 handler (observed: 500 {"message":"Internal error:
//     Unexpected token '\"', \"\"hej\"\" is not valid JSON"} for '"hej"')
func (s *Server) readBodyClassified(w http.ResponseWriter, r *http.Request) (bp bodyParse, done bool) {
	if !jsonContentType(r.Header.Get("Content-Type")) {
		return bodyParse{kind: bodyUndefined}, true
	}
	var buf []byte
	if r.Body != nil {
		buf, _ = io.ReadAll(r.Body)
	}
	if len(strings.TrimSpace(string(buf))) == 0 {
		return bodyParse{kind: bodyObject, fields: map[string]json.RawMessage{}}, true
	}
	trimmed := strings.TrimSpace(string(buf))
	var probe json.RawMessage
	if err := json.Unmarshal(buf, &probe); err != nil {
		s.bodyParseError(w, trimmed)
		return bodyParse{kind: bodyFatal}, true
	}
	switch probe[0] {
	case '{':
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(probe, &fields); err != nil {
			s.bodyParseError(w, trimmed)
			return bodyParse{kind: bodyFatal}, true
		}
		if fields == nil {
			fields = map[string]json.RawMessage{}
		}
		return bodyParse{kind: bodyObject, fields: fields, order: rawObjectKeys(probe)}, true
	case '[':
		return bodyParse{kind: bodyArray}, true
	default:
		s.bodyParseError(w, trimmed)
		return bodyParse{kind: bodyFatal}, true
	}
}

// rawObjectKeys lists the keys of a top-level JSON object in document order
// (drives the strictObject "Unrecognized key" issue order). The body already
// parsed successfully, so this scanner only needs to be robust, not strict.
func rawObjectKeys(obj []byte) []string {
	var order []string
	n := len(obj)
	if n == 0 || obj[0] != '{' {
		return order
	}
	i := 1
	for i < n {
		for i < n && isJSONWhitespaceByte(obj[i]) || obj[i] == ',' {
			i++
		}
		if i >= n || obj[i] == '}' || obj[i] != '"' {
			break
		}
		i++ // opening quote
		start := i
		for i < n && obj[i] != '"' {
			if obj[i] == '\\' {
				i++
			}
			i++
		}
		key := unescapeJSONStringKey(string(obj[start:i]))
		if i < n {
			i++ // closing quote
		}
		for i < n && (obj[i] == ' ' || obj[i] == '\t') {
			i++
		}
		if i >= n || obj[i] != ':' {
			break
		}
		i++ // colon
		i = skipJSONValue(obj, i)
		order = append(order, key)
	}
	return order
}

func isJSONWhitespaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// skipJSONValue advances i past the JSON value starting at i (strings, nested
// objects/arrays, literals), respecting escaped quotes.
func skipJSONValue(b []byte, i int) int {
	n := len(b)
	for i < n && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	if i >= n {
		return i
	}
	switch b[i] {
	case '"':
		i++
		for i < n && b[i] != '"' {
			if b[i] == '\\' {
				i++
			}
			i++
		}
		if i < n {
			i++
		}
	case '{':
		depth := 0
		for i < n {
			c := b[i]
			if c == '"' {
				i++
				for i < n && b[i] != '"' {
					if b[i] == '\\' {
						i++
					}
					i++
				}
				if i < n {
					i++
				}
				continue
			}
			if c == '{' {
				depth++
			} else if c == '}' {
				depth--
			}
			i++
			if c == '}' && depth == 0 {
				break
			}
		}
	case '[':
		depth := 0
		for i < n {
			c := b[i]
			if c == '"' {
				i++
				for i < n && b[i] != '"' {
					if b[i] == '\\' {
						i++
					}
					i++
				}
				if i < n {
					i++
				}
				continue
			}
			if c == '[' {
				depth++
			} else if c == ']' {
				depth--
			}
			i++
			if c == ']' && depth == 0 {
				break
			}
		}
	default: // number / true / false / null
		for i < n && b[i] != ',' && b[i] != '}' && b[i] != ']' && !isJSONWhitespaceByte(b[i]) {
			i++
		}
	}
	return i
}

// unescapeJSONStringKey handles the realistic escapes in object keys.
func unescapeJSONStringKey(s string) string {
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	return s
}

// bodyParseError writes the Node-observed 500 for rejected bodies:
// 500 {"message":"Internal error: Unexpected token '<first>', "<body>" is not
// valid JSON"} (exact text verified for '"hej"' and '123').
func (s *Server) bodyParseError(w http.ResponseWriter, raw string) {
	msg := "Unexpected token '" + raw[:1] + "', \"" + raw + "\" is not valid JSON"
	s.logf("chat: body parse error: %s", msg)
	pbhttp.WriteJSON(w, http.StatusInternalServerError, struct {
		Message string `json:"message"`
	}{"Internal error: " + msg})
}

// jsonContentType mirrors express' typeis 'json' matching: application/json
// and any */*+json media type (case-insensitive, params ignored).
func jsonContentType(ct string) bool {
	mt := strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	if mt == "" {
		return false
	}
	if i := strings.LastIndex(mt, "/"); i >= 0 {
		sub := mt[i+1:]
		return sub == "json" || strings.HasSuffix(sub, "+json")
	}
	return false
}

// ---- field validation helpers -------------------------------------------------

// jsonValue decodes a raw field into its Go JSON value.
func jsonValue(raw json.RawMessage) any {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

// requiredObjectIDField validates a required zz.objectId() field (user_id,
// targetProjectId, ...): absent → "received undefined"; non-string →
// "received <type>"; bad hex → "Invalid Mongo ObjectId".
func requiredObjectIDField(fields map[string]json.RawMessage, key, path string, issues *[]string) (string, bool) {
	raw, ok := fields[key]
	if !ok {
		*issues = append(*issues, typeIssue("string", "undefined", path))
		return "", false
	}
	return objectIDString(raw, path, issues)
}

// optionalObjectIDField validates an optional zz.objectId() field (edit body
// userId): absent → OK; present must be a 24-hex string.
func optionalObjectIDField(fields map[string]json.RawMessage, key, path string, issues *[]string) (string, bool) {
	raw, ok := fields[key]
	if !ok {
		return "", false
	}
	return objectIDString(raw, path, issues)
}

func objectIDString(raw json.RawMessage, path string, issues *[]string) (string, bool) {
	v := jsonValue(raw)
	if str, ok := v.(string); ok {
		if objectIDHex.MatchString(str) {
			return str, true
		}
		*issues = append(*issues, objectIDIssue(path))
		return "", false
	}
	*issues = append(*issues, typeIssue("string", jsonTypeName(v), path))
	return "", false
}

// contentField validates the messageContent schema (z.string with the custom
// error 'No content provided' + max 10240). Non-string or empty values both
// report the custom error (observed: content:123 → "No content provided").
func contentField(fields map[string]json.RawMessage, issues *[]string) (string, bool) {
	raw, ok := fields["content"]
	if !ok {
		*issues = append(*issues, `No content provided at "body.content"`)
		return "", false
	}
	v := jsonValue(raw)
	str, isStr := v.(string)
	if !isStr || str == "" {
		*issues = append(*issues, `No content provided at "body.content"`)
		return "", false
	}
	if utf16Len(str) > maxMessageLength {
		*issues = append(*issues, `Content too long (> 10240 bytes) at "body.content"`)
		return "", false
	}
	return str, true
}

// threadsField validates z.strictObject({ threads: z.array(zz.objectId()) }),
// appending one issue per bad element in index order (zod validates each).
func threadsField(fields map[string]json.RawMessage, issues *[]string) ([]string, bool) {
	raw, ok := fields["threads"]
	if !ok {
		*issues = append(*issues, `Invalid input: expected array, received undefined at "body.threads"`)
		return nil, false
	}
	v := jsonValue(raw)
	arr, isArr := v.([]any)
	if !isArr {
		*issues = append(*issues, typeIssue("array", jsonTypeName(v), "body.threads"))
		return nil, false
	}
	out := make([]string, 0, len(arr))
	allOk := true
	for i, elem := range arr {
		path := "body.threads[" + strconv.Itoa(i) + "]"
		if str, isStr := elem.(string); isStr {
			if objectIDHex.MatchString(str) {
				out = append(out, str)
				continue
			}
			*issues = append(*issues, objectIDIssue(path))
			allOk = false
			continue
		}
		*issues = append(*issues, typeIssue("string", jsonTypeName(elem), path))
		allOk = false
	}
	if !allOk {
		return nil, false
	}
	return out, true
}

// unknownBodyKeys appends the strictObject "Unrecognized key" issues in JSON
// appearance order — after the shape field issues, as observed.
func unknownBodyKeys(bp bodyParse, known map[string]bool, issues *[]string) {
	seen := map[string]bool{}
	for _, key := range bp.order {
		if seen[key] {
			continue
		}
		seen[key] = true
		if !known[key] {
			*issues = append(*issues, fmt.Sprintf("Unrecognized key: %q at \"body\"", key))
		}
	}
}

// ---- query validation (getGlobalMessages) ------------------------------------

type queryMessages struct {
	before int64
	limit  int
}

// validateQuery validates
//
//	z.strictObject({ before: z.coerce.number().int().optional(),
//	                limit:  z.coerce.number().int().default(50) })
//
// appending issues in schema order (before, limit, then unknown keys).
func validateQuery(r *http.Request, issues *[]string) queryMessages {
	q := r.URL.Query()
	before := int64(0)
	limit := defaultMessageLimit

	if vals := q["before"]; len(vals) > 0 {
		if n, ok := coerceIntQuery(vals); ok {
			before = n
		} else {
			*issues = append(*issues, `Invalid input: expected number, received NaN at "query.before"`)
		}
	}
	if vals := q["limit"]; len(vals) > 0 {
		if n, ok := coerceIntQuery(vals); ok {
			limit = int(n)
		} else {
			*issues = append(*issues, `Invalid input: expected number, received NaN at "query.limit"`)
		}
	}
	for _, key := range queryKeysInOrder(r) {
		switch key {
		case "before", "limit":
			continue
		}
		*issues = append(*issues, fmt.Sprintf("Unrecognized key: %q at \"query\"", key))
	}
	return queryMessages{before: before, limit: limit}
}

// coerceIntQuery mirrors z.coerce.number().int(): Number(s) must be finite and
// integral. Repeated keys (Node: qs array → NaN) are invalid.
func coerceIntQuery(vals []string) (int64, bool) {
	if len(vals) != 1 {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(vals[0]), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	if f != math.Trunc(f) || f < math.MinInt64 || f > math.MaxInt64 {
		return 0, false
	}
	return int64(f), true
}

func queryKeysInOrder(r *http.Request) []string {
	var out []string
	seen := map[string]bool{}
	for _, pair := range strings.Split(r.URL.RawQuery, "&") {
		if pair == "" {
			continue
		}
		key := pair
		if i := strings.IndexByte(pair, '='); i >= 0 {
			key = pair[:i]
		}
		if u, err := url.QueryUnescape(key); err == nil {
			key = u
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

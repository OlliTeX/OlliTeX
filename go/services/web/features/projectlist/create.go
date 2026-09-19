package projectlist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
)

// P4.7 — POST /project/new  (project creation, BASIC template — the primary
// "New Project" flow). Node sources (oracle, pinned live 2026-09-14):
//
//	router.mjs  webRouter.post('/project/new', requireLogin, rateLimit, newProject)
//	ProjectController.newProject
//	  -> projectName = body.projectName != null ? trim : undefined
//	  -> template === 'example' ? createExampleProject : createBasicProject
//	  -> res.json({ project_id, owner_ref, owner:{first_name,last_name,email,_id} })
//	ProjectCreationHandler.createBasicProject = _createBlankProject + _createRootDoc
//	  -> validateProjectName (blank/len/`/`)  -> InvalidNameError -> 400 text/plain
//	  -> Project (Mongoose) saved with the default field set (see crInsertProject)
//	  -> HistoryManager.initializeProject  (POST project_history /project {historyId})
//	  -> _buildTemplate('mainbasic.tex')   -> docstore revision 0 (POST project/:pid/doc/:doc)
//
// Contract (live oracle):
//
//   - 200 application/json {project_id, owner_ref, owner:{first_name,last_name,email,_id}}
//     (new project doc + rootFolder/main.tex + docstore revision 0 + history entry)
//   - blank name (absent or whitespace)   -> 400 text/plain "Project name cannot be blank"
//   - name with `/`                       -> 400 text/plain "Project name cannot contain / characters"
//   - name > 150 UTF-16 units             -> 400 text/plain "Project name is too long"
//   - body.projectName non-string         -> 400 application/json zod (received <T>)
//   - body.template non-string            -> 400 application/json zod (received <T>)
//   - unrecognized body key(s)            -> 400 application/json zod "Unrecognized key(s)"
//   - JSON root not object: number/str/null -> 400 application/json `{}` (express strict)
//   - JSON root array                    -> 400 application/json zod "expected object, received array"
//   - invalid JSON                        -> 400 application/json `{}` (express strict)
//   - anonymous                           -> 403 text/plain "Forbidden" (CSRF before requireLogin)
//   - template === 'example'              -> example project (P4.7b: files + clsi; follow-up)
//
// zod accumulates ALL errors joined by "; " in order: value-errors
// (projectName, then template) then the unrecognized-key error.
var createNewPat = regexp.MustCompile(`^/project/new$`)

func crEnvOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func crDocstoreBase() string { return crEnvOr("WEB_DOCSTORE_URL", "http://127.0.0.1:3016") }
func crHistoryBase() string  { return crEnvOr("WEB_PROJECT_HISTORY_URL", "http://127.0.0.1:3054") }
func crBasicTemplate() string {
	return crEnvOr("WEB_BASIC_PROJECT_TEMPLATE",
		"/overleaf/services/web/app/templates/project_files/mainbasic.tex")
}

// P4.7b — 'example' variant config. The template dir holds main.tex,
// sample.bib and frog.jpg (free build uses the '-sp' suffix variant). The
// history-v1 blob store is the same backend project-history(3054) initialises;
// auth mirrors Node's HISTORY_V1_BASIC_AUTH (staging / V1_HISTORY_PASSWORD).
func crV1HistoryBase() string { return crEnvOr("WEB_V1_HISTORY_URL", "http://127.0.0.1:3100/api") }
func crV1HistoryUser() string { return crEnvOr("V1_HISTORY_USER", "staging") }
func crV1HistoryPass() string { return crEnvOr("V1_HISTORY_PASSWORD", "") }
func crExampleProjectDir() string {
	return crEnvOr("WEB_EXAMPLE_PROJECT_DIR",
		"/overleaf/services/web/app/templates/project_files/example-project-sp")
}

var crHTTP = &http.Client{Timeout: 12 * time.Second}

var createMonthNames = []string{
	"January", "February", "March", "April", "May", "June",
	"July", "August", "September", "October", "November", "December",
}

// crControlRe mirrors Node Sanitize.mjs CONTROL_CHARS_RE (Go RE2: use \x{...}).
var crControlRe = regexp.MustCompile(`[\x00-\x1f\x7f-\x9f\x{200b}\x{200c}\x{200d}\x{2060}\x{feff}]`)

func crSanitizeControl(s string) string {
	return crControlRe.ReplaceAllStringFunc(s, func(m string) string {
		r := []rune(m)
		if len(r) == 0 {
			return m
		}
		return fmt.Sprintf(`\u%04x`, int(r[0]))
	})
}

func crUTF16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// crNameError mirrors Node validateProjectName (returns the 400 message, true).
func crNameError(name string) (string, bool) {
	if name != "" {
		name = crSanitizeControl(name)
	}
	if crUTF16Len(name) == 0 {
		return `Project name cannot be blank`, true
	}
	if crUTF16Len(name) > 150 {
		return `Project name is too long`, true
	}
	if strings.Contains(name, `/`) {
		return `Project name cannot contain / characters`, true
	}
	return ``, false
}

func crZodType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case int, int64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case nil:
		return "null"
	default:
		return "object"
	}
}

// crOrderedKeys returns the top-level JSON object keys in document order.
func crOrderedKeys(raw []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		return nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			break
		}
		keys = append(keys, fmt.Sprint(t))
		var v json.RawMessage
		_ = dec.Decode(&v) // consume the value
	}
	return keys
}

type crBody struct {
	projectName    string
	projectPresent bool
	template       string
	tmplPresent    bool
}

// crParseResult: ok==true -> use body; bare -> reply `400 {}`;
// zod (zodMsg != "") -> reply `400 {"error":"Validation error: "+zodMsg,"statusCode":400}`.
type crParseResult struct {
	ok     bool
	bare   bool
	zodMsg string
	body   *crBody
}

// crParseCreateBody implements Node's z.strictObject { projectName?, template? }.
func crParseCreateBody(raw []byte) crParseResult {
	if len(bytes.TrimSpace(raw)) == 0 {
		return crParseResult{ok: true, body: &crBody{}} // empty body == {} (projectName undefined)
	}
	if !json.Valid(raw) {
		return crParseResult{bare: true} // express.json strict: bare 400 {}
	}
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	switch trimmed[0] {
	case '[':
		return crParseResult{zodMsg: `Invalid input: expected object, received array at "body"`}
	case '{':
		// object — continue
	default:
		return crParseResult{bare: true} // number / string / null / boolean -> express strict 400 {}
	}

	var bm map[string]any
	_ = json.Unmarshal(raw, &bm)
	keys := crOrderedKeys(raw)
	present := func(k string) bool { _, ok := bm[k]; return ok }

	var errs []string
	if present("projectName") {
		if _, ok := bm["projectName"].(string); !ok {
			errs = append(errs, "Invalid input: expected string, received "+crZodType(bm["projectName"])+` at "body.projectName"`)
		}
	}
	if present("template") {
		if _, ok := bm["template"].(string); !ok {
			errs = append(errs, "Invalid input: expected string, received "+crZodType(bm["template"])+` at "body.template"`)
		}
	}
	var unrecognized []string
	for _, k := range keys {
		if k != "projectName" && k != "template" {
			unrecognized = append(unrecognized, k)
		}
	}
	if len(unrecognized) == 1 {
		errs = append(errs, `Unrecognized key: "`+unrecognized[0]+`" at "body"`)
	} else if len(unrecognized) > 1 {
		quoted := make([]string, len(unrecognized))
		for i, k := range unrecognized {
			quoted[i] = `"` + k + `"`
		}
		errs = append(errs, "Unrecognized keys: "+strings.Join(quoted, ", ")+` at "body"`)
	}
	if len(errs) > 0 {
		return crParseResult{zodMsg: strings.Join(errs, "; ")}
	}

	b := &crBody{}
	if present("projectName") {
		b.projectName = bm["projectName"].(string)
		b.projectPresent = true
	}
	if present("template") {
		b.template = bm["template"].(string)
		b.tmplPresent = true
	}
	return crParseResult{ok: true, body: b}
}

// crZodReply marshals the 400 json {error,statusCode} with proper escaping.
// crZodReply — Node 400 {"error":"Validation error: <issues>","statusCode":400}
// with RAW '<'/'>' bytes (zod's Too big/Too small messages carry <=/>= —
// Go's default json.Marshal HTML-escapes them to \\u003c/\\u003e; pinned
// 2026-09-18 via the typst 'Too big ... <=100' A/B).
func crZodReply(message string) []byte {
	type env struct {
		Error      string `json:"error"`
		StatusCode int    `json:"statusCode"`
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(env{"Validation error: " + message, 400}); err != nil {
		return []byte(`{}`)
	}
	// json.Encoder appends a newline Node's res.json never has.
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

// crBasicDocLines mirrors Node _buildTemplate('mainbasic.tex').
func crBasicDocLines(name, first, last string) []string {
	b, err := os.ReadFile(crBasicTemplate())
	if err != nil {
		b = []byte(`\documentclass{article}
\usepackage{graphicx} % Required for inserting images

\title{<%= project_name %>}
\author{<%= user.first_name %> <%= user.last_name %>}
\date{<%= month %> <%= year %>}

\begin{document}

\maketitle

\section{Introduction}

\end{document}
`)
	}
	s := string(b)
	now := time.Now().UTC()
	s = strings.ReplaceAll(s, `<%= project_name %>`, name)
	s = strings.ReplaceAll(s, `<%= user.first_name %>`, first)
	s = strings.ReplaceAll(s, `<%= user.last_name %>`, last)
	s = strings.ReplaceAll(s, `<%= month %>`, createMonthNames[int(now.Month())-1])
	s = strings.ReplaceAll(s, `<%= year %>`, fmt.Sprintf(`%d`, now.Year()))
	return strings.Split(s, "\n")
}

type crOwner struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	ID        string `json:"_id"`
}
type crOut struct {
	ProjectID string  `json:"project_id"`
	OwnerRef  string  `json:"owner_ref"`
	Owner     crOwner `json:"owner"`
}

func newProjectHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if cxt.Sess == nil || !cxt.Sess.IsLoggedIn() {
			// POST -> the core's csrf check bounces anonymous to 403 first;
			// the 401 (requireLogin) branch is unreachable for anon here.
			res.SendStatus(401)
			return
		}
		uid := cxt.Sess.UserIDHex()

		raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		pr := crParseCreateBody(raw)
		switch {
		case pr.bare:
			res.JSON(400, []byte(`{}`))
			return
		case !pr.ok && pr.zodMsg != "":
			res.JSON(400, crZodReply(pr.zodMsg))
			return
		case !pr.ok:
			res.JSON(400, []byte(`{}`))
			return
		}
		body := pr.body

		name := ""
		if body.projectPresent {
			name = strings.TrimSpace(body.projectName)
		}
		if msg, bad := crNameError(name); bad {
			res.PlainText(400, msg)
			return
		}

		u, ok2 := loadOwnerUser(a, cxt, uid)
		if !ok2 {
			res.JSON(500, []byte("internal error"))
			return
		}

		// Branch: 'example' (main.tex + sample.bib + frog.jpg blob) vs the
		// default BASIC (mainbasic.tex). Both return the new project id.
		var pid primitive.ObjectID
		if body.tmplPresent && body.template == "example" {
			pid = crCreateExampleProject(a, cxt, name, uid, u)
		} else {
			pid = crCreateBasicProject(a, cxt, name, uid, u)
		}

		b := core.JSON(crOut{
			ProjectID: pid.Hex(),
			OwnerRef:  uid,
			Owner: crOwner{
				FirstName: u.first,
				LastName:  u.last,
				Email:     u.email,
				ID:        uid,
			},
		})
		res.JSON(200, b)
	}
}

// crCreateBasicProject: basic 'New Project' (mainbasic.tex -> main.tex).
func crCreateBasicProject(a *core.App, cxt *core.Cxt, name, uid string, u crOwnerUser) primitive.ObjectID {
	pid := primitive.NewObjectID()
	docID := primitive.NewObjectID()
	rootID := primitive.NewObjectID()
	docs := bson.A{bson.D{
		{Key: "name", Value: "main.tex"},
		{Key: "_id", Value: docID},
	}}
	// version 1: blank project (version 0) + one addDoc (main.tex) $inc.
	crInsertProject(a, cxt, pid, rootID, &docID, name, uid, u.spellCheckLanguage, "pdflatex", docs, bson.A{}, 1)
	crCreateDocRevision(cxt, pid, docID, crBasicDocLines(name, u.first, u.last))
	crInitHistory(cxt, pid.Hex())
	return pid
}

type crOwnerUser struct {
	first, last, email, spellCheckLanguage string
}

func loadOwnerUser(a *core.App, cxt *core.Cxt, uid string) (crOwnerUser, bool) {
	if a.Mongo == nil || uid == "" {
		return crOwnerUser{}, false
	}
	oid, err := primitive.ObjectIDFromHex(uid)
	if err != nil {
		return crOwnerUser{}, false
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return crOwnerUser{}, false
	}
	var d primitive.D
	opts := options.FindOne().SetProjection(bson.D{
		{Key: "first_name", Value: 1},
		{Key: "last_name", Value: 1},
		{Key: "email", Value: 1},
		{Key: "ace.spellCheckLanguage", Value: 1},
	})
	if db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}, opts).Decode(&d) != nil {
		return crOwnerUser{}, false
	}
	spell := ""
	if ace, ok := dget(d, "ace").(primitive.D); ok {
		spell = asStr(dget(ace, "spellCheckLanguage"))
	}
	// Node's User model default for ace.spellCheckLanguage is "en"; a project
	// inherits the (default-applied) value, so empty/absent => "en".
	if spell == "" {
		spell = "en"
	}
	return crOwnerUser{
		first:              asStr(dget(d, "first_name")),
		last:               asStr(dget(d, "last_name")),
		email:              asStr(dget(d, "email")),
		spellCheckLanguage: spell,
	}, true
}

// crInsertProject writes the project document, mirroring Node's Mongoose
// default field set + the basic project's rootFolder/main.tex.
// crInsertProject writes the project document, mirroring Node's Mongoose
// default field set. The rootFolder contents (docs / fileRefs / rootDoc_id)
// are parameterised so the BASIC ('main.tex') and EXAMPLE (main.tex, sample.bib,
// frog.jpg) variants share one document shape.
func crInsertProject(a *core.App, cxt *core.Cxt, pid, rootID primitive.ObjectID, rootDocID *primitive.ObjectID, name, ownerRef, spellLang, compiler string, docs bson.A, fileRefs bson.A, version int) {
	if a.Mongo == nil {
		return
	}
	uid, _ := primitive.ObjectIDFromHex(ownerRef)
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return
	}
	e := []primitive.ObjectID{}
	doc := bson.D{
		{Key: "_id", Value: pid},
		{Key: "name", Value: name},
		{Key: "lastUpdatedBy", Value: uid},
		{Key: "active", Value: true},
		{Key: "readOnly", Value: false},
		{Key: "owner_ref", Value: uid},
		{Key: "collaberator_refs", Value: e},
		{Key: "reviewer_refs", Value: e},
		{Key: "readOnly_refs", Value: e},
		{Key: "pendingEditor_refs", Value: e},
		{Key: "pendingReviewer_refs", Value: e},
		{Key: "publicAccesLevel", Value: "private"},
		{Key: "compiler", Value: compiler},
		{Key: "spellCheckLanguage", Value: spellLang},
		{Key: "deletedByExternalDataSource", Value: false},
		{Key: "description", Value: ""},
		{Key: "trashed", Value: e},
		{Key: "grammarPicky", Value: true},
		{Key: "tokens", Value: bson.D{}},
		{Key: "tokenAccessReadOnly_refs", Value: e},
		{Key: "tokenAccessReadAndWrite_refs", Value: e},
		{Key: "overleaf", Value: bson.D{{Key: "history", Value: bson.D{
			{Key: "id", Value: pid.Hex()},
			{Key: "display", Value: true},
		}}}},
		{Key: "lastUpdated", Value: time.Now().UTC()},
		{Key: "editAccessRequests", Value: []bson.D{}},
		{Key: "rootFolder", Value: bson.A{bson.D{
			{Key: "name", Value: "rootFolder"},
			{Key: "_id", Value: rootID},
			{Key: "docs", Value: docs},
			{Key: "fileRefs", Value: fileRefs},
			{Key: "folders", Value: []bson.D{}},
		}}},
		{Key: "deletedDocs", Value: []bson.D{}},
		{Key: "collabratecUsers", Value: []primitive.M{}},
		{Key: "__v", Value: 0},
		{Key: "version", Value: version},
		{Key: "rootDoc_id", Value: func() any {
			if rootDocID == nil {
				return nil
			}
			return *rootDocID
		}()},
	}
	_, _ = db.Collection("projects").InsertOne(ctx, doc)
}

// crCreateDocRevision asks the docstore (Go service) for revision 0 of the doc.
func crCreateDocRevision(cxt *core.Cxt, pid, docID primitive.ObjectID, lines []string) {
	payload, _ := json.Marshal(map[string]any{
		"lines":   lines,
		"version": 0,
		"ranges":  map[string]any{},
	})
	req, err := http.NewRequestWithContext(cxt.Req.Context(), "POST",
		strings.TrimSuffix(crDocstoreBase(), "/")+"/project/"+pid.Hex()+"/doc/"+docID.Hex(),
		bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := crHTTP.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
}

// crInitHistory asks project-history (history-v1, in-container) to initialize
// the project history: POST /project {historyId}.
func crInitHistory(cxt *core.Cxt, historyID string) {
	payload, _ := json.Marshal(map[string]any{"historyId": historyID})
	req, err := http.NewRequestWithContext(cxt.Req.Context(), "POST",
		strings.TrimSuffix(crHistoryBase(), "/")+"/project", bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := crHTTP.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
}

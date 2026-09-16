// Package compile ports the editor COMPILE CONTROL PLANE (P5.2a):
//
//	POST /project/:Project_id/compile        (CompileController.compile)
//	POST /project/:Project_id/compile/stop   (CompileController.stopCompile)
//
// (Express routing is case-insensitive — both /Project/ and /project/ match.)
//
// The compile ENGINE stays the Node `clsi` service. This package is the web
// control plane only: authz + recently-compiled guard + root-doc resolution
// + limits + docstore/filestore resource assembly + the clsi HTTP round
// trip + the exact response reshaping the React client expects.
//
// Contracts pinned from the LIVE production source + oracle (2026-09-15):
//
//	Router (services/web/app/src/router.mjs):
//	  POST /project/:Project_id/compile      — RateLimiter `compile-project-
//	  http` (200 pts/10 min, NOT ported — the P4 parity decision: gate never
//	  hits the ceiling), ensureUserCanReadProject.
//	  POST /project/:Project_id/compile/stop — ensureUserCanReadProject only.
//
//	Authz (P4.2, byte-pinned): invalid ObjectId → 404 JSON (NOT accept-dep):
//	  {"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}
//	  absent project → 404 HTML general/404 (NOT accept-dep)
//	  non-member (not public) → 403: accept json {"message":"restricted"} /
//	  otherwise HTML user/restricted.  canRead = owner_ref || collaberator_
//	  refs || reviewer_refs || readOnly_refs || (tokenBased token refs) ||
//	  publicAccesLevel in {readOnly, readAndWrite} || site admin.
//	  Anonymous POST bounces at the session+CSRF gate (403 "Forbidden",
//	  pinned P4.5 — core middleware, not this handler).
//
//	CompileManager.compile (services/web/app/src/Features/Compile/
//	CompileManager.mjs) — ORDER MATTERS:
//	  1. recently-compiled: redis SET compile:<pid>:<uid> true EX 1 NX;
//	     reply ≠ 'OK' ⇒ return {status:'too-recently-compiled', outputFiles:[]}
//	  2. autocompile limits — `if (!isAutoCompile) return true` short-circuit
//	     (the gate never sends auto_compile; both checks no-op here)
//	  3. if !options.rootResourcePath: ensureRootDocumentIsValid; failure ⇒
//	     {status:'validation-problems', validationProblems:{mainFile:'no main
//	     file specified'}, outputFiles:[]}   (NO limits keys yet)
//	  4. buildId = Date.now().toString(16) + '-' + random 8 hex
//	  5. getProject (compiler/imageName/overleaf.history.id/rootDoc_id/
//	     rootFolder) → limits = _getProjectCompileLimits(project):
//	       timeout   = owner.features.compileTimeout || 180
//	       compileGroup = owner.features.compileGroup || 'standard'
//	                      (alphaProgram ⇒ 'alpha')
//	       compileBackendClass = standard→'free' else 'premium'
//	       compiler  = project.compiler   (RAW; response echoes this value,
//	                      omitted when the project has none)
//	  6. clsi: ClsiManager._buildRequest → standard "old way" path:
//	       _getContentFromMongo → getAllDocs/getAllFiles (project tree);
//	       _finaliseRequest (resources + rootResourcePath resolution):
//	         - resources: docs {path: doc.lines.join('\n')} (stubs skipped);
//	          files {path, url: filestore/history/project/<hid>/hash/<hash>,
//	          modified: file.created?.getTime()}
//	         - rootResourcePath: options.rootResourcePath, else the doc whose
//	          _id == project.rootDoc_id, else options.rootDoc_id override
//	          (WINS), else typst→'main.typ' / hasMainFile→'main.tex' /
//	          single doc / throw.
//	       options → clsi compileOptionsSchema (strictObject!):
//	         {buildId, editorId?, compiler: normalized project.compiler,
//	          timeout, imageName?, draft, png2pdf:false,
//	          enableCheckpoint:false, stopOnFirstError, check?,
//	          compileGroup, compileFromClsiCache:false,
//	          enablePdfCaching:false, flags?, metricsMethod: compileGroup}
//	       URL: <clsi|clsi_typst>/project/<pid>/user/<uid>/compile
//	            ?compileBackendClass=free&compileGroup=standard
//
//	Response (CompileController res.json — undefined OMITTED, explicit
//	values KEPT, key order preserved):
//	  {status, outputFiles, outputFilesArchive, compileGroup, compiler,
//	   clsiServerId?, clsiCacheShard?, validationProblems?, stats?,
//	   timings?, outputUrlPrefix?, pdfDownloadDomain?,
//	   pdfCachingMinChunkSize?}
//	  outputFilesArchive = buildId ? {path:'output.zip',
//	    url:'/project/<pid>/user/<uid>/build/<buildId>/output/output.zip',
//	    type:'zip'} : EXPLICIT null.
//	  outputFiles (ClsiManager._parseOutputFiles): every file {path,
//	    url: pathname-only, type, build}; output.pdf ALSO {contentId?,
//	    ranges: file.ranges || [], size, startXRefTable?, createdAt: new
//	    Date()}.
//
//	CompileController.stopCompile: ClsiManager.stopCompile (options
//	UNDEFINED — URL query serialises literal "undefined"; clsi ignores it)
//	→ res.sendStatus(200)  ⇒ 200 text/plain, body "OK", Vary: Accept.
//
//	clsi HTTP error mapping (ClsiManager._postToClsi): 413→project-too-large
//	/ 409→conflict / 423→compile-in-progress / 502|503→unavailable
//	(web 200 + the mapped status + outputFiles[] + limits).
//
// Deliberately out of scope (P5.2b + edge modes): GET
// /project/:id/output/cached/output.overleaf.json, /download/project/:id/
// build/... output routes, compileFromHistory, incrementalCompilesEnabled,
// png2pdf premium, auto-compile rate limiter.
package compile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Express `webRouter.post('/project/:Project_id/...')` — one non-empty path
// segment for the id (Node matches ANY non-empty token here; the zod
// objectId validation then 404s the invalid ones, pinned P4.5), then the
// literal suffix. /[Pp]roject/ covers both spellings (express is
// case-insensitive).
var compilePat = regexp.MustCompile(`^/[Pp]roject/([^/]+)/compile$`)
var stopPat = regexp.MustCompile(`^/[Pp]roject/([^/]+)/compile/stop$`)

var validOID = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// malformed404 — Node's 404 JSON when the id is not a valid ObjectId (not
// accept-dependent; pinned P4.5 live).
const malformed404 = `{"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}`

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func clsiBase() string {
	return strings.TrimSuffix(envOr("WEB_CLSI_URL", "http://127.0.0.1:3013"), "/")
}

func clsiTypstBase() string {
	return strings.TrimSuffix(envOr("WEB_CLSI_TYPEST_URL", "http://127.0.0.1:3014"), "/")
}

func docstoreBase() string {
	host := os.Getenv("DOCSTORE_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	return strings.TrimSuffix(envOr("WEB_DOCSTORE_URL", "http://"+host+":3016"), "/")
}

func filestoreBase() string {
	host := os.Getenv("FILESTORE_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	return strings.TrimSuffix(envOr("WEB_FILESTORE_URL", "http://"+host+":3009"), "/")
}

// Feature registers the compile control-plane routes (session+CSRF ON —
// Node mounts both under the session router; the anonymous POST bounces at
// the core gate exactly like Node's CSRF 403, pinned P4.5).
func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "compile",
		Routes: []core.Route{
			{Method: "POST", Pattern: compilePat, Handler: compileHandler(a)},
			{Method: "POST", Pattern: stopPat, Handler: stopHandler(a)},
			// P5.2b — output read (download-PDF button + clsi-cache shapes)
			{Method: "GET", Pattern: pdfDownloadPattern, Handler: pdfDownloadHandler(a)},
			{Method: "GET", Pattern: cachedJSONPattern, Handler: cachedBuildJSONHandler(a)},
			{Method: "GET", Pattern: cachedFilePattern, Handler: cachedFileHandler(a)},
		},
	}
}

// ---------------------------------------------------------------------------
// small helpers (mirrors projectlist's P4-pinned shapes)
// ---------------------------------------------------------------------------

func dget(d primitive.D, key string) any {
	for _, e := range d {
		if e.Key == key {
			return e.Value
		}
	}
	return nil
}

func oidHex(v any) string {
	switch t := v.(type) {
	case primitive.ObjectID:
		return t.Hex()
	case string:
		return strings.ToLower(t)
	}
	return ""
}

func asOIDList(v any) []string {
	var out []string
	a, ok := v.(primitive.A)
	if !ok {
		return out
	}
	for _, e := range a {
		if s := oidHex(e); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func inOIDList(v any, uid string) bool {
	if uid == "" {
		return false
	}
	for _, s := range asOIDList(v) {
		if s == uid {
			return true
		}
	}
	return false
}

// canRead mirrors canUserReadProject (P4.2 contract, byte-pinned).
func canRead(p primitive.D, uid string, isAdmin bool) bool {
	if uid == "" {
		return false
	}
	if oidHex(dget(p, "owner_ref")) == uid {
		return true
	}
	if inOIDList(dget(p, "collaberator_refs"), uid) ||
		inOIDList(dget(p, "reviewer_refs"), uid) ||
		inOIDList(dget(p, "readOnly_refs"), uid) {
		return true
	}
	if pal, _ := dget(p, "publicAccesLevel").(string); pal == "tokenBased" {
		if inOIDList(dget(p, "tokenAccessReadAndWrite_refs"), uid) ||
			inOIDList(dget(p, "tokenAccessReadOnly_refs"), uid) {
			return true
		}
	}
	if pal, _ := dget(p, "publicAccesLevel").(string); pal == "readOnly" || pal == "readAndWrite" {
		return true
	}
	return isAdmin
}

func loadUserAdmin(a *core.App, cxt *core.Cxt, uid string) bool {
	if a.Mongo == nil || uid == "" || !core.AdminPrivilegeAvailable() {
		return false
	}
	oid, err := primitive.ObjectIDFromHex(uid)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	var d primitive.D
	opts := options.FindOne().SetProjection(bson.D{{Key: "isAdmin", Value: 1}})
	err = db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}, opts).Decode(&d)
	if err != nil {
		return false
	}
	b, _ := dget(d, "isAdmin").(bool)
	return b
}

func pageBase(cxt *core.Cxt) views.PageData {
	d := views.PageData{Nonce: views.NewNonce(), Origin: cxt.SiteURL}
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
		if uid := cxt.Sess.UserIDHex(); uid != "" {
			// Node's 404/403 views render the signed-in session user
			// (ol-usersEmail + ol-user_id metas) — pinned in this gate.
			d.UserID = uid
			d.UserEmail = sessEmail(cxt.Sess)
		}
	}
	// 403/404 views' <link rel=alternate> = origin + '/' + request path
	// without leading slash (res.locals.currentUrl, P4-pinned).
	d.Path = strings.TrimPrefix(cxt.Req.URL.Path, "/")
	return d
}

func sessEmail(s *core.Session) string {
	if raw, ok := s.GetRaw("passport"); ok {
		var p struct {
			User json.RawMessage `json:"user"`
		}
		if json.Unmarshal(raw, &p) == nil && string(p.User) != "null" && string(p.User) != "" {
			if e := userField(p.User, "email"); e != "" {
				return e
			}
		}
	}
	if raw, ok := s.GetRaw("user"); ok && string(raw) != "null" {
		if e := userField(raw, "email"); e != "" {
			return e
		}
	}
	return ""
}

func userField(raw json.RawMessage, field string) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	s, _ := m[field].(string)
	return s
}

// denyRead mirrors HttpErrorHandler.forbidden (accept-dependent, P4-pinned).
func denyRead(cxt *core.Cxt, res *core.Res) {
	if core.AcceptsJSON(cxt.Req) {
		res.JSON(403, []byte(`{"message":"restricted"}`))
		return
	}
	views.Restricted403(res.W, pageBase(cxt))
}

func notFound(cxt *core.Cxt, res *core.Res) {
	views.NotFoundPage(res.W, pageBase(cxt))
}

const internal500 = `{"error":{"type":"InternalServerError","message":"Internal Server Error"}}`

// loadProject returns the project doc, or (nil, nil) when absent (Node:
// NotFoundError → 404 HTML general/404, NOT accept-dependent).
func loadProject(ctx context.Context, a *core.App, idHex string) (*primitive.D, error) {
	oid, err := primitive.ObjectIDFromHex(strings.ToLower(idHex))
	if err != nil {
		return nil, nil
	}
	if a.Mongo == nil {
		return nil, nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	var d primitive.D
	e := db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d)
	if e == mongo.ErrNoDocuments {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	return &d, nil
}

// ---------------------------------------------------------------------------
// project tree walk + docstore content
// ---------------------------------------------------------------------------

type treeDoc struct {
	Path string
	ID   string
}

type treeFile struct {
	Path    string
	Hash    string
	Created int64 // epoch ms; 0 when the field is absent
}

// walk mirrors Node's _getAllFoldersFromProject for the standard path:
// rootFolder is an ARRAY; Node seeds the walk at rootFolder with basePath
// '/' and each doc at the root level is a BARE name (e.g. 'main.tex');
// subfolder docs accumulate 'sub/…' segments; leading '/' is stripped by
// path semantics. Depth-first, docs then fileRefs per folder (the map order
// is Node's tree order; clsi keys resources by path so order is
// semantically neutral).
func walk(root primitive.A) (docs []treeDoc, files []treeFile) {
	var walkFolder func(f any, base string)
	walkFolder = func(f any, base string) {
		fd, ok := f.(primitive.D)
		if !ok {
			return
		}
		p := func(name string) string {
			if base == "" {
				return name
			}
			return base + "/" + name
		}
		for _, dv := range asD(dget(fd, "docs")) {
			d, dok := dv.(primitive.D)
			if !dok {
				continue
			}
			dn, _ := dget(d, "name").(string)
			if dn == "" {
				continue
			}
			docs = append(docs, treeDoc{Path: p(dn), ID: oidHex(dget(d, "_id"))})
		}
		for _, fv := range asD(dget(fd, "fileRefs")) {
			fr, fok := fv.(primitive.D)
			if !fok {
				continue
			}
			fn, _ := dget(fr, "name").(string)
			if fn == "" {
				continue
			}
			hash, _ := dget(fr, "hash").(string)
			var created int64
			switch t := dget(fr, "created").(type) {
			case primitive.DateTime:
				created = int64(t)
			case int64:
				created = t
			}
			files = append(files, treeFile{Path: p(fn), Hash: hash, Created: created})
		}
		for _, sub := range asD(dget(fd, "folders")) {
			if sub == nil {
				continue
			}
			sd, ok := sub.(primitive.D)
			if !ok {
				continue
			}
			sname, _ := dget(sd, "name").(string)
			if sname == "" {
				continue
			}
			walkFolder(sd, p(sname))
		}
	}
	for _, rf := range root {
		walkFolder(rf, "")
	}
	return docs, files
}

// asD coerces a (possibly nil) primitive.A or []any to a slice of any.
func asD(v any) []any {
	switch t := v.(type) {
	case primitive.A:
		return t
	case []any:
		return t
	case nil:
		return nil
	}
	return nil
}

// getDocLines — Go docstore GET /project/:pid/doc → [{_id, lines, rev}]
// (content = lines.join('\n'), the exact Node _buildRequest shape).
func getDocLines(ctx context.Context, pidHex string) (map[string][]string, error) {
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(docstoreBase() + "/project/" + pidHex + "/doc")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, io.ErrUnexpectedEOF
	}
	var arr []struct {
		ID    string   `json:"_id"`
		Lines []string `json:"lines"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, d := range arr {
		if d.ID != "" && d.Lines != nil {
			out[d.ID] = d.Lines
		}
	}
	return out, nil
}

func genBuildId() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return strconv.FormatInt(time.Now().UnixMilli(), 16) + "-" + hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// limits (CompileManager._getProjectCompileLimits, live-pinned)
// ---------------------------------------------------------------------------

type limits struct {
	Timeout      int
	CompileGroup string
	BackendClass string // free | premium
	CompilerRaw  string // project.compiler verbatim ("" ⇒ response omits)
}

const (
	defCompileGroup   = "standard" // Settings.defaultFeatures.compileGroup
	defCompileTimeout = 180        // Settings.defaultFeatures.compileTimeout
)

func computeLimits(owner *primitive.D, projectCompiler string) limits {
	lim := limits{
		Timeout:      defCompileTimeout,
		CompileGroup: defCompileGroup,
		BackendClass: "free", // standardCompileBackendClass
		CompilerRaw:  projectCompiler,
	}
	if owner == nil {
		return lim
	}
	if alpha, _ := dget(*owner, "alphaProgram").(bool); alpha {
		lim.CompileGroup = "alpha"
	}
	features, _ := dget(*owner, "features").(primitive.D)
	if features == nil {
		features = primitive.D{}
	}
	fget := func(k string) any {
		return dget(features, k)
	}
	if g, ok := fget("compileGroup").(string); ok && g != "" {
		lim.CompileGroup = g
	}
	if t, ok := fget("compileTimeout").(int32); ok && t > 0 {
		lim.Timeout = int(t)
	} else if t, ok := fget("compileTimeout").(int64); ok && t > 0 {
		lim.Timeout = int(t)
	} else if t, ok := fget("compileTimeout").(float64); ok && int(t) > 0 {
		lim.Timeout = int(t)
	}
	if lim.CompileGroup != "standard" {
		lim.BackendClass = "premium" // priorityCompileBackendClass
	}
	return lim
}

// ---------------------------------------------------------------------------
// response shapes (CompileController res.json contract — key order +
// undefined-omission are part of the parity surface)
// ---------------------------------------------------------------------------

type ofArchive struct {
	Path string `json:"path"`
	URL  string `json:"url"`
	Type string `json:"type"`
}

type ofOut struct {
	Path           string          `json:"path"`
	URL            string          `json:"url"`
	Type           string          `json:"type"`
	Build          string          `json:"build"`
	ContentID      *string         `json:"contentId,omitempty"`
	Ranges         json.RawMessage `json:"ranges,omitempty"` // explicit [] for output.pdf; absent otherwise (RawMessage is []byte: nil ⇒ omitted, "[]" kept)
	Size           *float64        `json:"size,omitempty"`
	StartXRefTable *float64        `json:"startXRefTable,omitempty"`
	CreatedAt      *string         `json:"createdAt,omitempty"`
}

type resp struct {
	Status             string          `json:"status"`
	OutputFiles        *[]ofOut        `json:"outputFiles,omitempty"`
	Archive            *ofArchive      `json:"outputFilesArchive"`
	CompileGroup       string          `json:"compileGroup,omitempty"`
	Compiler           string          `json:"compiler,omitempty"`
	ClsiServerID       string          `json:"clsiServerId,omitempty"`
	ClsiCacheShard     string          `json:"clsiCacheShard,omitempty"`
	ValidationProblems json.RawMessage `json:"validationProblems,omitempty"`
	Stats              json.RawMessage `json:"stats,omitempty"`
	Timings            json.RawMessage `json:"timings,omitempty"`
	OutputURLPrefix    *string         `json:"outputUrlPrefix,omitempty"`
	PDFDownloadDomain  string          `json:"pdfDownloadDomain,omitempty"`
	PDFCachingMinCk    json.RawMessage `json:"pdfCachingMinChunkSize,omitempty"`
}

func writeJSON(res *core.Res, status int, body *resp) {
	b, _ := json.Marshal(body)
	res.JSON(status, b)
}

// ---------------------------------------------------------------------------
// shared preflight: id validity → project → authz
// ---------------------------------------------------------------------------

// preflight runs the Node middleware order: zod objectId (404 JSON),
// project load (404 HTML), ensureUserCanReadProject (403). Returns the
// project doc on success.
func preflight(cxt *core.Cxt, res *core.Res, a *core.App, idParam string) (*primitive.D, bool) {
	if !validOID.MatchString(idParam) {
		res.JSON(404, []byte(malformed404))
		return nil, false
	}
	ctx := cxt.Req.Context()
	p, lerr := loadProject(ctx, a, idParam)
	if lerr != nil {
		res.JSON(500, []byte(internal500))
		return nil, false
	}
	if p == nil {
		notFound(cxt, res)
		return nil, false
	}
	if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
		denyRead(cxt, res)
		return nil, false
	}
	if !canRead(*p, cxt.Sess.UserIDHex(), loadUserAdmin(a, cxt, cxt.Sess.UserIDHex())) {
		denyRead(cxt, res)
		return nil, false
	}
	return p, true
}

// ---------------------------------------------------------------------------
// POST /project/:id/compile
// ---------------------------------------------------------------------------

type reqBody struct {
	StopOnFirstError      *bool  `json:"stopOnFirstError"`
	EditorID              string `json:"editorId"`
	RootResourcePath      string `json:"rootResourcePath"`
	RootDocID             string `json:"rootDoc_id"`
	Compiler              string `json:"compiler"`
	Draft                 bool   `json:"draft"`
	Check                 string `json:"check"`
	IncrementalCompilesOn bool   `json:"incrementalCompilesEnabled"`
}

func compileOptionsOf(lim limits, dispatch string) map[string]any {
	// ClsiManager._finaliseRequest option set (standard path; the
	// clsi strictObject schema accepts exactly these names).
	return map[string]any{
		"buildId":              "", // filled by caller
		"compiler":             dispatch,
		"draft":                false,
		"png2pdf":              false,
		"enableCheckpoint":     false,
		"stopOnFirstError":     false,
		"compileGroup":         lim.CompileGroup,
		"compileFromClsiCache": false,
		"enablePdfCaching":     false,
		"metricsMethod":        lim.CompileGroup,
		"timeout":              lim.Timeout,
	}
}

func compileHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		param := cxt.Params["1"]
		if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
			denyRead(cxt, res) // defensive; the core gate bounces first (P4.5-pinned)
			return
		}
		uid := cxt.Sess.UserIDHex()

		p, ok := preflight(cxt, res, a, param)
		if !ok {
			return
		}
		pid := strings.ToLower(param)
		ctx := cxt.Req.Context()

		q := cxt.Req.URL.Query()
		_, isAuto := q["auto_compile"]
		fileLineErrors := q.Get("file_line_errors") != ""
		body := reqBody{}
		if cxt.Req.Body != nil {
			if raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20)); len(raw) > 0 {
				_ = json.Unmarshal(raw, &body)
			}
		}
		if !(body.Check == "validate" || body.Check == "error" || body.Check == "silent") {
			body.Check = ""
		}
		stopOnFirst := body.StopOnFirstError != nil && *body.StopOnFirstError
		_ = isAuto                     // non-auto short-circuit (Node: if (!isAutoCompile) return true)
		_ = body.IncrementalCompilesOn // edge mode — P5.2 follow-up (docupdater path)

		// ---- 1) recently-compiled guard (SET compile:<pid>:<uid> true EX 1 NX)
		if a.Redis != nil {
			set, rerr := a.Redis.SETNXEXReply("compile:"+pid+":"+uid, "true", time.Second)
			if rerr == nil && !set {
				empty := []ofOut{}
				writeJSON(res, 200, &resp{Status: "too-recently-compiled", OutputFiles: &empty})
				return
			}
		}

		// ---- owner limits (Node fetches owner after the validation branch only
		//      for the SUCCESS/limits path; the validation-problems branch has
		//      no limits keys — computed here but used only after root resolve)
		ownerHex := oidHex(dget(*p, "owner_ref"))
		var ownerDoc *primitive.D
		if ownerHex != "" && a.Mongo != nil {
			if ooid, oerr := primitive.ObjectIDFromHex(ownerHex); oerr == nil {
				opts := options.FindOne().SetProjection(bson.D{
					{Key: "alphaProgram", Value: 1},
					{Key: "features", Value: 1},
				})
				if db, derr := a.Mongo.DB(ctx); derr == nil {
					var od primitive.D
					if db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: ooid}}, opts).Decode(&od) == nil {
						ownerDoc = &od
					}
				}
			}
		}
		projectCompiler, _ := dget(*p, "compiler").(string)
		lim := computeLimits(ownerDoc, projectCompiler)

		// ---- 2) tree + docstore content
		rootFolder, _ := dget(*p, "rootFolder").(primitive.A)
		docs, files := walk(rootFolder)
		lines, derr := getDocLines(ctx, pid)
		if derr != nil {
			res.JSON(500, []byte(internal500))
			return
		}

		// ---- 3) rootResourcePath resolution (Node _finaliseRequest order)
		rootDocProject := oidHex(dget(*p, "rootDoc_id"))
		rootDocOverride := strings.ToLower(body.RootDocID)
		if body.RootResourcePath == "" && rootDocOverride == "" {
			// Node: CompileManager.ensureRootDocumentIsValid gate. The
			// fixture-standard projects have a valid rootDoc_id; when NOTHING
			// resolves below we emit the Node validation-problems shape.
		}
		root := body.RootResourcePath
		override := ""
		hasMain := false
		singlePath := ""
		n := 0
		for _, d := range docs {
			n++
			singlePath = d.Path
			if d.ID != "" && d.ID == rootDocProject {
				root = d.Path
			}
			if d.ID != "" && d.ID == rootDocOverride {
				override = d.Path
			}
			if d.Path == "main.tex" {
				hasMain = true
			}
		}
		if override != "" {
			root = override // Node: rootResourcePathOverride wins
		}
		if root == "" {
			dispatch := lim.CompilerRaw
			if dispatch == "typst" {
				root = "main.typ"
			} else if hasMain {
				root = "main.tex"
			} else if n == 1 {
				root = singlePath
			} else if body.RootResourcePath == "" {
				// Node CompileManager validation-problems branch (200 JSON;
				// NO limits keys — limits not yet part of the object).
				vp := json.RawMessage(`{"mainFile":"no main file specified"}`)
				empty := []ofOut{}
				writeJSON(res, 200, &resp{Status: "validation-problems", OutputFiles: &empty, ValidationProblems: vp})
				return
			}
		}
		if root == "" {
			res.JSON(500, []byte(internal500)) // Node: OError throw → 500
			return
		}

		// ---- 4) buildId (Node: generated right after the validation branch)
		bid := genBuildId()

		// ---- 5) dispatch compiler (Node _buildRequest normalisation). Node's
		// production flow applies limits.compiler (project.compiler) AFTER
		// options.compiler, so the clsi request always carries the PROJECT
		// compiler (the body override is a no-op there — live-pinned).
		dispatch := lim.CompilerRaw
		if dispatch != "pdflatex" && dispatch != "latex" &&
			dispatch != "xelatex" && dispatch != "lualatex" &&
			dispatch != "typst" {
			dispatch = "pdflatex" // Settings.defaultLatexCompiler
		}
		_ = body.Compiler

		// ---- 6) resources (docs first, then files — Node order)
		type resEntry struct {
			Path     string `json:"path"`
			Content  string `json:"content,omitempty"`
			URL      string `json:"url,omitempty"`
			Modified *int64 `json:"modified,omitempty"`
		}
		resources := []resEntry{}
		historyID := ""
		if ov, ok := dget(*p, "overleaf").(primitive.D); ok {
			if h, ok := dget(ov, "history").(primitive.D); ok {
				historyID, _ = dget(h, "id").(string)
			}
		}
		for _, d := range docs {
			if l, ok := lines[d.ID]; ok {
				resources = append(resources, resEntry{Path: d.Path, Content: strings.Join(l, "\n")})
			}
		}
		for _, f := range files {
			e := resEntry{Path: f.Path}
			if f.Hash != "" && historyID != "" {
				e.URL = filestoreBase() + "/history/project/" + historyID + "/hash/" + f.Hash
				if f.Created > 0 {
					ms := f.Created
					e.Modified = &ms
				}
			}
			resources = append(resources, e)
		}

		// ---- 7) clsi request (standard path)
		options := compileOptionsOf(lim, dispatch)
		options["buildId"] = bid
		options["draft"] = body.Draft
		options["stopOnFirstError"] = stopOnFirst
		options["check"] = body.Check
		if body.EditorID != "" {
			options["editorId"] = body.EditorID
		} else {
			delete(options, "editorId")
		}
		if body.Check == "" {
			delete(options, "check")
		}
		if img, ok := dget(*p, "imageName").(string); ok && img != "" && dispatch != "typst" {
			options["imageName"] = img
		}
		if fileLineErrors {
			options["flags"] = []string{"-file-line-error"}
		}

		base := clsiBase()
		if dispatch == "typst" {
			base = clsiTypstBase()
		}
		u := base + "/project/" + pid + "/user/" + uid + "/compile" +
			"?compileBackendClass=" + url.QueryEscape(lim.BackendClass) +
			"&compileGroup=" + url.QueryEscape(lim.CompileGroup)
		reqBody2, _ := json.Marshal(map[string]any{
			"compile": map[string]any{
				"options":          options,
				"resources":        resources,
				"rootResourcePath": root,
			},
		})
		creq, _ := http.NewRequest("POST", u, strings.NewReader(string(reqBody2)))
		creq.Header.Set("Accept", "application/json")
		creq.Header.Set("Content-Type", "application/json")
		cctx, cancel := context.WithTimeout(ctx, 240*time.Second)
		defer cancel()
		cresp, cerr := http.DefaultClient.Do(creq.WithContext(cctx))
		if cerr != nil {
			res.JSON(500, []byte(internal500))
			return
		}
		cbody, _ := io.ReadAll(io.LimitReader(cresp.Body, 32<<20))
		_ = cresp.Body.Close()

		// ---- 8) clsi HTTP error mapping (Node _postToClsi)
		if cresp.StatusCode == 413 {
			empty := []ofOut{}
			writeJSON(res, 200, &resp{Status: "project-too-large", OutputFiles: &empty,
				CompileGroup: lim.CompileGroup, Compiler: lim.CompilerRaw})
			return
		}
		if cresp.StatusCode == 409 {
			empty := []ofOut{}
			writeJSON(res, 200, &resp{Status: "conflict", OutputFiles: &empty,
				CompileGroup: lim.CompileGroup, Compiler: lim.CompilerRaw})
			return
		}
		if cresp.StatusCode == 423 {
			empty := []ofOut{}
			writeJSON(res, 200, &resp{Status: "compile-in-progress", OutputFiles: &empty,
				CompileGroup: lim.CompileGroup, Compiler: lim.CompilerRaw})
			return
		}
		if cresp.StatusCode == 502 || cresp.StatusCode == 503 {
			empty := []ofOut{}
			writeJSON(res, 200, &resp{Status: "unavailable", OutputFiles: &empty,
				CompileGroup: lim.CompileGroup, Compiler: lim.CompilerRaw})
			return
		}
		if cresp.StatusCode >= 400 {
			res.JSON(cresp.StatusCode, cbody)
			return
		}

		// ---- 9) reshape (Node _parseOutputFiles + controller res.json)
		var clsiDoc struct {
			Compile *struct {
				Status          string            `json:"status"`
				OutputFiles     []json.RawMessage `json:"outputFiles"`
				BuildID         *string           `json:"buildId"`
				Stats           json.RawMessage   `json:"stats"`
				Timings         json.RawMessage   `json:"timings"`
				OutputURLPrefix *string           `json:"outputUrlPrefix"`
				ClsiCacheShard  *string           `json:"clsiCacheShard"`
			} `json:"compile"`
		}
		if jerr := json.Unmarshal(cbody, &clsiDoc); jerr != nil || clsiDoc.Compile == nil {
			res.JSON(502, cbody)
			return
		}
		cl := clsiDoc.Compile

		outFiles := []ofOut{}
		for _, rawOf := range cl.OutputFiles {
			var f struct {
				Path      string          `json:"path"`
				URL       string          `json:"url"`
				Type      string          `json:"type"`
				Build     string          `json:"build"`
				ContentID *string         `json:"contentId"`
				Ranges    json.RawMessage `json:"ranges"`
				Size      *float64        `json:"size"`
				StartXRef *float64        `json:"startXRefTable"`
			}
			if uerr := json.Unmarshal(rawOf, &f); uerr != nil {
				continue
			}
			o := ofOut{Path: f.Path, URL: outputPathname(f.URL), Type: f.Type, Build: f.Build}
			if f.Path == "output.pdf" {
				o.ContentID = f.ContentID // omitted when clsi has none
				if f.Ranges != nil && len(f.Ranges) > 0 {
					o.Ranges = f.Ranges
				} else {
					o.Ranges = json.RawMessage("[]") // Node: file.ranges || []
				}
				o.Size = f.Size
				o.StartXRefTable = f.StartXRef
				now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
				o.CreatedAt = &now
			}
			outFiles = append(outFiles, o)
		}

		r := &resp{
			Status:       cl.Status,
			OutputFiles:  &outFiles,
			CompileGroup: lim.CompileGroup,
			Compiler:     lim.CompilerRaw,
		}
		if cl.BuildID != nil && *cl.BuildID != "" {
			r.Archive = &ofArchive{
				Path: "output.zip",
				URL:  "/project/" + pid + "/user/" + uid + "/build/" + *cl.BuildID + "/output/output.zip",
				Type: "zip",
			}
		}
		if cl.Stats != nil {
			r.Stats = cl.Stats
		}
		if cl.Timings != nil {
			r.Timings = cl.Timings
		}
		if cl.OutputURLPrefix != nil {
			// Node passes compile.outputUrlPrefix THROUGH — a present EMPTY
			// string is a defined value and is serialized (observed live:
			// "outputUrlPrefix":""), so no != "" filter.
			v := *cl.OutputURLPrefix
			r.OutputURLPrefix = &v
		}
		if cl.ClsiCacheShard != nil && *cl.ClsiCacheShard != "" {
			r.ClsiCacheShard = *cl.ClsiCacheShard
		}
		writeJSON(res, 200, r)
	}
}

// outputPathname = Node `new URL(file.url).pathname`.
func outputPathname(u string) string {
	if p, err := url.Parse(u); err == nil && p.Path != "" {
		return p.Path
	}
	return u
}

// ---------------------------------------------------------------------------
// POST /project/:id/compile/stop
// ---------------------------------------------------------------------------

func stopHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		param := cxt.Params["1"]
		if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
			denyRead(cxt, res)
			return
		}
		uid := cxt.Sess.UserIDHex()
		if _, ok := preflight(cxt, res, a, param); !ok {
			return
		}
		pid := strings.ToLower(param)

		// Node ClsiManager.stopCompile (options UNDEFINED → the query
		// serialises literal "undefined"; clsi ignores the params):
		//   <clsi>/project/<pid>/user/<uid>/compile/stop?
		//     compileBackendClass=undefined&compileGroup=undefined
		u := clsiBase() + "/project/" + pid + "/user/" + uid + "/compile/stop" +
			"?compileBackendClass=undefined&compileGroup=undefined"
		creq, _ := http.NewRequest("POST", u, nil)
		creq.Header.Set("Accept", "application/json")
		cctx, cancel := context.WithTimeout(cxt.Req.Context(), 30*time.Second)
		defer cancel()
		cresp, cerr := http.DefaultClient.Do(creq.WithContext(cctx))
		if cerr != nil {
			res.JSON(500, []byte(internal500))
			return
		}
		_, _ = io.Copy(io.Discard, cresp.Body)
		_ = cresp.Body.Close()
		if cresp.StatusCode >= 400 {
			res.JSON(500, []byte(internal500))
			return
		}
		res.SendStatus(200)
	}
}

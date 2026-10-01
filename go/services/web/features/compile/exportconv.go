// exportconv.go — audit-013: project export to Word / Markdown / HTML via
// pandoc (Node 1:1 port):
//
//	GET /project/:pid/download/conversion/{type}
//	    → ProjectDownloadsController.exportProjectConversion
//	      (DocumentConversionManager.convertProjectToDocument → CLSI
//	        POST /project/:pid/user/:uid/download/project-to-document)
//	    → {downloadUrl} (responseFormat=json) or direct attachment stream
//
//	GET /project/:pid/download/conversion/{conversionId}/{type}/build/{buildId}/output/{file}
//	    → ProjectDownloadsController.downloadPreparedProjectExport
//	      (_streamConvertedDocumentToResponse: res.attachment(
//	        `${safeProjectName}.${ext}`) + nosniff + no buffering)
//
// The CLSI Go side already implements the conversion itself
// (conversioncontroller.ConvertProjectToDocument +
// conversionmanager.ConvertLaTeXToDocumentInDirWithLock, pandoc in the
// PANDOC_IMAGE container); this file is the web plane the editor's
// "Export as …" menu items call (frontend export-project-with-conversion-
// button, gated by ol-ExposedSettings.enablePandocConversions + the
// export-* split-test flags).
package compile

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"ollitex/go/services/web/core"
)

var (
	// Node exportProjectConversionSchema: type = docx | markdown | html.
	convCreatePat = regexp.MustCompile(
		`^/project/([0-9a-f]{24})/download/conversion/(docx|markdown|html)$`)
	// Node downloadPreparedProjectExportSchema: conversionId = uuid,
	// buildId = buildId (uuid in the json branch), type enum, file = filepath.
	convDownloadPat = regexp.MustCompile(
		`^/project/([0-9a-f]{24})/download/conversion/([0-9a-fA-F-]{8,40})/(docx|markdown|html)/build/([0-9a-fA-F-]{4,40})/output/(.+)$`)
)

// convExt — Node SUPPORTED_CONVERSION_TYPES (docx→docx, markdown→zip,
// html→zip; the markdown/html artifacts are zipped by the CLSI).
func convExt(t string) string {
	switch t {
	case "docx":
		return "docx"
	case "markdown", "html":
		return "zip"
	default:
		return ""
	}
}

// convLimits — computeLimits inputs exactly as compileHandler (owner doc
// projection + project compiler), reused for the conversion body options.
func convLimits(ctx context.Context, a *core.App, p *bson.D) limits {
	ownerHex := oidHex(dget(*p, "owner_ref"))
	var ownerDoc *bson.D
	if ownerHex != "" && a.Mongo != nil {
		if ooid, oerr := bson.ObjectIDFromHex(ownerHex); oerr == nil {
			opts := options.FindOne().SetProjection(bson.D{
				{Key: "alphaProgram", Value: 1},
				{Key: "features", Value: 1},
			})
			if db, derr := a.Mongo.DB(ctx); derr == nil {
				var od bson.D
				if db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: ooid}}, opts).Decode(&od) == nil {
					ownerDoc = &od
				}
			}
		}
	}
	projectCompiler, _ := dget(*p, "compiler").(string)
	return computeLimits(ownerDoc, projectCompiler)
}

// convRoot — Node ClsiManager._buildRequest root resolution: explicit
// override wins; else the doc matching project.rootDoc_id; else main.tex
// when present; else the single doc; else error (Node OError('no main file
// specified')).
func convRoot(p *bson.D, rqp string, docs []treeDoc) (string, bool) {
	rootDocHex := ""
	if v, ok := dget(*p, "rootDoc_id").(bson.ObjectID); ok {
		rootDocHex = v.Hex()
	} else if v, ok := dget(*p, "rootDoc_id").(string); ok {
		rootDocHex = strings.ToLower(v)
	}
	if rqp != "" {
		return rqp, true
	}
	single := ""
	hasMain := false
	for _, d := range docs {
		single = d.Path
		if d.Path == "main.tex" {
			hasMain = true
		}
	}
	for _, d := range docs {
		if rootDocHex != "" && strings.EqualFold(d.ID, rootDocHex) {
			return d.Path, true
		}
	}
	if hasMain {
		return "main.tex", true
	}
	if len(docs) == 1 {
		return single, true
	}
	return "", false
}

// convBody — the CLSI conversion request (Node
// buildDocumentConversionRequest → _buildRequest(null, projectId, userId,
// {..., populateClsiCache: true, incrementalCompilesEnabled: false})).
// The strictObject schema requires a buildId matching /^[0-9a-f]+-[0-9a-f]+$/
// — a generated UUIDv4 (Node: the web generates this buildId for conversion
// requests; OutputCacheManager.generateBuildId is a uuidv4).
// convBuildId — Node OutputCacheManager.generateBuildId 1:1:
//
//	crypto.randomBytes(8).toHex() + "-" + Date.now().hex
//
// → `{dateHex}-{16hex}` — exactly one dash (the CLSI buildId regex
// /^[0-9a-f]+-[0-9a-f]+$/ forbids a UUIDv4's three internal dashes).
func convBuildID() string {
	rnd := make([]byte, 8)
	_, _ = rand.Read(rnd)
	var sb strings.Builder
	for _, c := range rnd {
		sb.WriteString(fmt.Sprintf("%x", c))
	}
	return fmt.Sprintf("%x", time.Now().UnixNano()/int64(time.Millisecond)) + "-" + sb.String()
}

func convBody(lim limits, dispatch string, resources []any, root, uid string) []byte {
	options := compileOptionsOf(lim, dispatch)
	options["buildId"] = convBuildID()  // Node: OutputCacheManager.generateBuildId (web side)
	options["populateClsiCache"] = true // Node buildDocumentConversionRequest
	compileBody := map[string]any{
		"options":          options,
		"resources":        resources,
		"rootResourcePath": root,
	}
	b, _ := json.Marshal(map[string]any{
		"compile": compileBody,
		"userId":  uid,
	})
	return b
}

// convResources — docs (inline content) then files (filestore blob urls),
// the same rules compile.go pins (zero-line stub docs and hashless fileRefs
// skipped — CLSI rejects path-only entries).
func convResources(p *bson.D, docs []treeDoc, files []treeFile, lines map[string][]string) []any {
	type resEntry struct {
		Path     string `json:"path"`
		Content  string `json:"content,omitempty"`
		URL      string `json:"url,omitempty"`
		Modified *int64 `json:"modified,omitempty"`
	}
	out := []any{}
	historyID := ""
	if ov, ok := dget(*p, "overleaf").(bson.D); ok {
		if h, ok := dget(ov, "history").(bson.D); ok {
			historyID, _ = dget(h, "id").(string)
		}
	}
	for _, d := range docs {
		l, ok := lines[d.ID]
		if !ok || len(l) == 0 {
			continue
		}
		out = append(out, resEntry{Path: d.Path, Content: strings.Join(l, "\n")})
	}
	for _, f := range files {
		if f.Hash == "" || historyID == "" {
			continue
		}
		e := resEntry{Path: f.Path}
		e.URL = filestoreBase() + "/history/project/" + historyID + "/hash/" + f.Hash
		if f.Created > 0 {
			ms := f.Created
			e.Modified = &ms
		}
		out = append(out, e)
	}
	return out
}

// convName — the project name (Node ProjectGetter {name: true}).
func convName(p *bson.D) string {
	n, _ := dget(*p, "name").(string)
	return n
}

// ---------------------------------------------------------------------------
// GET /project/:pid/download/conversion/{type}
// ---------------------------------------------------------------------------

func exportConvCreateHandler(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
			denyRead(cxt, res)
			return
		}
		uid := cxt.Sess.UserIDHex()
		p, ok := preflight(cxt, res, a, cxt.Params["1"])
		if !ok {
			return
		}
		path := cxt.Req.URL.Path
		m := convCreatePat.FindStringSubmatch(path)
		typ := m[2]
		pid := strings.ToLower(cxt.Params["1"])
		ctx := cxt.Req.Context()
		q := cxt.Req.URL.Query()
		rf := q.Get("responseFormat")
		if rf == "" {
			rf = "stream"
		}
		if rf != "json" && rf != "stream" {
			// Node: zod parseReq failure → 400 (expressify).
			res.SendStatus(400)
			return
		}

		// ---- 1) tree + docstore content (compile.go pins the oracle)
		rootFolder, _ := dget(*p, "rootFolder").(bson.A)
		docs, files := walk(rootFolder)
		lines, derr := getDocLines(ctx, pid)
		if derr != nil {
			res.JSON(500, []byte(internal500))
			return
		}
		root, rOk := convRoot(p, q.Get("rootResourcePath"), docs)
		if !rOk {
			res.JSON(500, []byte(internal500)) // Node 'no main file specified'
			return
		}

		// ---- 2) CLSI conversion request (synchronous; the conversion
		//        finishes before the response — the frontend shows a
		//        preparing toast meanwhile).
		lim := convLimits(ctx, a, p)
		dispatch := lim.CompilerRaw
		if dispatch != "pdflatex" && dispatch != "latex" &&
			dispatch != "xelatex" && dispatch != "lualatex" && dispatch != "typst" {
			dispatch = "pdflatex"
		}
		body := convBody(lim, dispatch,
			convResources(p, docs, files, lines), root, uid)

		u := clsiBase() + "/project/" + pid + "/user/" + uid +
			"/download/project-to-document?type=" + url.QueryEscape(typ) +
			"&responseFormat=json"
		creq, eerr := http.NewRequest("POST", u, strings.NewReader(string(body)))
		if eerr != nil {
			res.JSON(500, []byte(internal500))
			return
		}
		creq.Header.Set("Accept", "application/json")
		creq.Header.Set("Content-Type", "application/json")
		cctx, cancel := context.WithTimeout(ctx, 300*time.Second)
		defer cancel()
		cresp, cerr := http.DefaultClient.Do(creq.WithContext(cctx))
		if cerr != nil {
			res.JSON(500, []byte(internal500))
			return
		}
		cbody, _ := io.ReadAll(io.LimitReader(cresp.Body, 32<<20))
		_ = cresp.Body.Close()

		// CLSI 422 = user-facing conversion error (Node
		// DocumentConversionError → res.status(422).json({error})).
		if cresp.StatusCode == 422 {
			msg := "pandoc conversion failed"
			var parsed struct {
				Error string `json:"error"`
			}
			if jerr := json.Unmarshal(cbody, &parsed); jerr == nil && parsed.Error != "" {
				msg = parsed.Error
			}
			b, _ := json.Marshal(map[string]string{"error": msg})
			res.JSON(422, b)
			return
		}
		if cresp.StatusCode != 200 {
			res.JSON(cresp.StatusCode, cbody)
			return
		}
		var conv struct {
			ConversionID string `json:"conversionId"`
			BuildID      string `json:"buildId"`
			File         string `json:"file"`
		}
		if jerr := json.Unmarshal(cbody, &conv); jerr != nil || conv.ConversionID == "" ||
			conv.BuildID == "" || conv.File == "" {
			res.JSON(502, cbody)
			return
		}

		downloadURL := "/project/" + pid + "/download/conversion/" + conv.ConversionID +
			"/" + typ + "/build/" + conv.BuildID + "/output/" + conv.File

		if rf == "json" {
			b, _ := json.Marshal(map[string]string{"downloadUrl": downloadURL})
			res.JSON(200, b)
			return
		}
		streamConversionFile(ctx, res, p, uid, conv.ConversionID, conv.BuildID,
			conv.File, typ)
	}
}

// ---------------------------------------------------------------------------
// GET /project/:pid/download/conversion/{cid}/{type}/build/{bid}/output/{file}
// ---------------------------------------------------------------------------

func exportConvDownloadHandler(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
			denyRead(cxt, res)
			return
		}
		p, ok := preflight(cxt, res, a, cxt.Params["1"])
		if !ok {
			return
		}
		uid := cxt.Sess.UserIDHex()
		m := convDownloadPat.FindStringSubmatch(cxt.Req.URL.Path)
		if m == nil {
			res.SendStatus(404)
			return
		}
		cid, typ, bid, file := m[2], m[3], m[4], m[5]
		if strings.Contains(file, "..") || strings.HasPrefix(file, "/") {
			res.SendStatus(404)
			return
		}
		streamConversionFile(cxt.Req.Context(), res, p, uid, cid, bid, file, typ)
	}
}

// streamConversionFile — Node DocumentConversionManager.
// streamConvertedProjectDocument (CLSI getOutputFileURL(conversionId,
// buildId, file)) + _streamConvertedDocumentToResponse: fetch, set
// Content-Length + attachment(`${safeName}.${ext}`) + nosniff, pipeline.
func streamConversionFile(ctx context.Context, res *core.Res, p *bson.D,
	uid, convID, buildID, file, typ string) {
	u := clsiBase() + "/project/" + convID + "/user/" + uid +
		"/download/build/" + buildID + "/output/" + url.PathEscape(file)
	req, rerr := http.NewRequest("GET", u, nil)
	if rerr != nil {
		res.JSON(500, []byte(internal500))
		return
	}
	req.Header.Set("Accept", "application/octet-stream")
	cctx, cancel := context.WithTimeout(ctx, 600*time.Second)
	defer cancel()
	cresp, cerr := http.DefaultClient.Do(req.WithContext(cctx))
	if cerr != nil {
		res.JSON(500, []byte(internal500))
		return
	}
	defer cresp.Body.Close()
	if cresp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(cresp.Body, 1<<20))
		res.JSON(cresp.StatusCode, b)
		return
	}
	ext := convExt(typ)
	name := safeProjectName(convName(p)) + "." + ext
	ct := "application/octet-stream"
	switch ext {
	case "docx":
		ct = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case "zip":
		ct = "application/zip"
	}
	h := res.W.Header()
	h.Set("Content-Type", ct)
	h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Accel-Buffering", "no")
	if cl := cresp.Header.Get("Content-Length"); cl != "" {
		h.Set("Content-Length", cl)
	}
	res.W.WriteHeader(200)
	_, _ = io.Copy(res.W, cresp.Body)
}

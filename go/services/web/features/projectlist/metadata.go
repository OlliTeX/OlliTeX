package projectlist

import (
	"context"
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// /project/:project_id/metadata — Node router.mjs (MetaController.getMetadata):
//
//	GET /project/:project_id/metadata
//	  requireLogin -> ensureUserCanReadProject -> MetaHandler.getAllMetaForProject
//	  -> res.json({ projectId, projectMeta })
//
// projectMeta: map docID -> { labels, packages, packageNames, documentClass }
// extracted from the doc lines (MetaHandler.mjs 1:1; the Node service also
// flushes to mongo first — the docstore latest revision is the same source of
// truth on the Go line, no flush needed: nothing pending outside the collab
// rooms, which persist synchronously).
//
// `packages` = the command-palette suggestion map (packageMapping.mjs, 1.26
// MB, embedded 1:1 so the editor palette hints are unchanged).

// metaPat — `webRouter.get('/project/:project_id/metadata')`.
var metaPat = regexp.MustCompile(`^/project/([^/]+)/metadata$`)

//go:embed packagemapping.json
var packageMappingFS embed.FS

// packageMapping — package name -> palette suggestion array (Node
// packageMapping.mjs default export). Lazily decoded once.
var (
	pmOnce sync.Once
	pmJSON []any
	pmMap  map[string]any
)

func packageMapping() map[string]any {
	pmOnce.Do(func() {
		b, err := packageMappingFS.ReadFile("packagemapping.json")
		if err != nil {
			return
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(b, &m) == nil {
			pmMap = make(map[string]any, len(m))
			for k, v := range m {
				var dv any
				if json.Unmarshal(v, &dv) == nil {
					pmMap[k] = dv
				}
			}
		}
	})
	return pmMap
}

// docMetaOut — Node DocMeta shape + key order (labels, packages,
// packageNames, documentClass). documentClass nil -> null.
type docMetaOut struct {
	Labels        []string       `json:"labels"`
	Packages      map[string]any `json:"packages"`
	PackageNames  []string       `json:"packageNames"`
	DocumentClass *string        `json:"documentClass"`
}

// metaRe — MetaHandler.mjs (Node /g semantics via FindAllStringSubmatch).
var (
	metaCommentRe  = regexp.MustCompile(`(^|[^\\])%.*`)
	metaLabelRe    = regexp.MustCompile(`\\label{(.{0,80}?)}`)
	metaLabelOptRe = regexp.MustCompile(`\blabel={?(.{0,80}?)[\s},\]]`)
	metaPkgRe      = regexp.MustCompile(`^\\usepackage(?:\[.{0,80}?\])?{(.{0,80}?)}`)
	metaReqPkgRe   = regexp.MustCompile(`^\\RequirePackage(?:\[.{0,80}?\])?{(.{0,80}?)}`)
	metaClassRe    = regexp.MustCompile(`^\\documentclass(?:\[.{0,80}?\])?{(.{0,80}?)}`)
)

// metaFromLines — extractMetaFromDoc 1:1 (comment-strip, then label /
// option-label / usepackage / RequirePackage / documentclass).
func metaFromLines(lines []string) docMetaOut {
	out := docMetaOut{Labels: []string{}, PackageNames: []string{}, Packages: map[string]any{}}
	mapping := packageMapping()
	for _, raw := range lines {
		line := metaCommentRe.ReplaceAllString(raw, "$1")
		for _, m := range metaLabelRe.FindAllStringSubmatch(line, -1) {
			if s := strings.TrimSpace(m[1]); s != "" {
				out.Labels = append(out.Labels, s)
			}
		}
		for _, m := range metaLabelOptRe.FindAllStringSubmatch(line, -1) {
			if s := strings.TrimSpace(m[1]); s != "" {
				out.Labels = append(out.Labels, s)
			}
		}
		addPkgs := func(re *regexp.Regexp) {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				for _, item := range strings.Split(m[1], ",") {
					if s := strings.TrimSpace(item); s != "" {
						out.PackageNames = append(out.PackageNames, s)
					}
				}
			}
		}
		addPkgs(metaPkgRe)
		addPkgs(metaReqPkgRe)
		if out.DocumentClass == nil {
			if m := metaClassRe.FindStringSubmatch(line); m != nil {
				dc := m[1]
				out.DocumentClass = &dc
			}
		}
	}
	for _, p := range out.PackageNames {
		if v, ok := mapping[p]; ok {
			out.Packages[p] = v
		}
	}
	return out
}

// metaResp — Node { projectId, projectMeta } (key order pinned).
type metaResp struct {
	ProjectID   string         `json:"projectId"`
	ProjectMeta map[string]any `json:"projectMeta"`
}

// metaDocView — the docstore /project/{pid}/doc list view (docView: _id then
// lines/rev/version/...).
type metaDocView struct {
	ID    string   `json:"_id"`
	Lines []string `json:"lines"`
}

var metaHTTP = &http.Client{Timeout: 20 * time.Second}

// metadataHandler — MetaController.getMetadata (above).
func metadataHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
			if core.AcceptsJSON(cxt.Req) {
				res.SendStatus(401)
			} else {
				res.Redirect(cxt.Req, 302, "/login")
			}
			return
		}
		uid := cxt.Sess.UserIDHex()
		param := cxt.Params["1"]
		relPath := strings.TrimPrefix(cxt.Req.URL.Path, "/")
		if !validOID.MatchString(param) {
			res.JSON(404, []byte(malformed404))
			return
		}
		oid, err := primitive.ObjectIDFromHex(strings.ToLower(param))
		if err != nil {
			res.JSON(404, []byte(malformed404))
			return
		}
		doc, lerr := loadProject(a, cxt, oid)
		if lerr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		if doc == nil {
			views.NotFoundPage(res.W, pageBase(cxt, relPath))
			return
		}
		if !canRead(uid, loadUserAdmin(a, cxt, uid), *doc) {
			if core.AcceptsJSON(cxt.Req) {
				res.JSON(403, []byte(`{"message":"restricted"}`))
			} else {
				views.Restricted403(res.W, pageBase(cxt, relPath))
			}
			return
		}

		projectMeta := map[string]any{}
		// DocstoreManager.getAllDocs (Node) = GET {docstore}/project/{pid}/doc.
		base := strings.TrimSuffix(entDocstoreURL(), "/")
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 15*time.Second)
		defer cancel()
		req, rerr := http.NewRequestWithContext(ctx, "GET", base+"/project/"+oid.Hex()+"/doc", nil)
		if rerr == nil {
			req.Header.Set("Accept", "application/json")
			if rresp, gerr := metaHTTP.Do(req); gerr == nil {
				if rresp.StatusCode == 200 {
					body, berr := io.ReadAll(rresp.Body)
					rresp.Body.Close()
					if berr == nil {
						var docs []metaDocView
						if json.Unmarshal(body, &docs) == nil {
							for _, d := range docs {
								projectMeta[d.ID] = metaFromLines(d.Lines)
							}
						}
					}
				} else {
					rresp.Body.Close()
				}
			}
		}

		b, _ := json.Marshal(metaResp{ProjectID: oid.Hex(), ProjectMeta: projectMeta})
		res.JSON(200, b)
	}
}

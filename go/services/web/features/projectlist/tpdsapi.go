//go:build !nocgo

package projectlist

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"unicode/utf16"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
)

// TPDS / Dropbox-GitHub-sync private-API write endpoints (router.mjs,
// privateApiRouter + requirePrivateApiAuth). The target deployment USES
// Dropbox/GitHub sync, so these are part of the 1:1 drop-in (Node :3000).
//
// Node sources (oracle):
// TpdsController.createProject (POST /user/:user_id/project/new):
//
//	parseReq(createProjectSchema={params.user_id:zz.objectId, body.projectName?:string},
//	  logOnly) -> generateUniqueName(user_id, projectName) -> createBlankProject(
//	  user_id, name, {}, {skipCreatingInTPDS}) -> res.json({projectId:_id}).
//
// Pinned Node :3000 wire (2026-09-23):
//
//	unauth / wrong-cred                -> 401 text/plain "Unauthorized" (12B) + WA + XPB
//	user_id !24hex (notanoid, all-digit) -> 404 JSON `params.user_id` (91B) + XPB
//	valid uid + valid name             -> 200 JSON {"projectId":"<24hex>"} (40B) + XPB  (CREATES a BLANK project)
//	valid uid + invalid name           -> 500 text/plain "Internal Server Error" (21B) + XPB
//	  (invalid = absent/empty/too-long/slash/backslash/leading-or-trailing-whitespace;
//	   the TPDS path does NOT map name-validation errors to 4xx — Node falls through to
//	   the Express 500; no project is created)
//
// The 200 path reuses the P4.7 blank-creation primitives (crInsertProject blank
// shape — rootFolder with empty docs/fileRefs, no main.tex/docstore — + crInitHistory).
var tpdsProjectNewPat = regexp.MustCompile(`^/user/([^/]+)/project/new$`)

// tpdsNameOK — mirrors Node validateProjectName for the TPDS path: any failure
// (absent / blank / >150 UTF-16 / contains '/' / contains '\' / leading-or-
// trailing whitespace) rejects the create (Node surfaces it as the Express 500).
func tpdsNameOK(present bool, name string) bool {
	if !present {
		return false
	}
	if name == "" {
		return false
	}
	if len(utf16.Encode([]rune(name))) > 150 {
		return false
	}
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	if name != strings.TrimSpace(name) {
		return false
	}
	return true
}

// tpdsPlain500 — Node's Express 500 for an unmapped error (name validation in the
// TPDS path): text/plain "Internal Server Error" (21B) + XPB + weak ETag (==
// "15-…" since 21 == 0x15).
func tpdsPlain500(r *core.Res) {
	r.W.Header().Set("X-Powered-By", "Express")
	r.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
	r.W.Header().Set("Content-Length", "21")
	r.W.Header().Set("ETag", core.EtagWeakBody("Internal Server Error"))
	r.W.WriteHeader(500)
	_, _ = r.W.Write([]byte("Internal Server Error"))
}

func tpdsCreateProjectHandler(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		req := c.Req
		if c.A.Cfg.Profile != "api" {
			// Defensive: core.App skips APIOnly routes on the web profile, so this
			// is unreachable on :4000; if it ever is, 404 (Node web :4000 404s it).
			r.JSON(404, delParamVA("user_id"))
			return
		}
		mm := tpdsProjectNewPat.FindStringSubmatch(req.URL.Path)
		if mm == nil {
			return // unreachable (dispatch pattern-gates)
		}
		uidHex := mm[1]
		if !a.APIBasicGate401(c, r, req) {
			return // unauth / wrong basic → 401 challenge wire
		}
		if !delHex24(uidHex) {
			// Node expressify 404-VA (res.json path → X-Powered-By).
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(404, delParamVA("user_id"))
			return
		}
		uid := strings.ToLower(uidHex)

		// Read the body and detect `projectName` presence (Node schema is
		// non-strict; only projectName is used).
		raw, _ := io.ReadAll(io.LimitReader(req.Body, 1<<20))
		var bodyObj map[string]json.RawMessage
		present := false
		var name string
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, &bodyObj); err == nil {
				if v, ok := bodyObj["projectName"]; ok {
					present = true
					_ = json.Unmarshal(v, &name)
				}
			}
		}
		if !tpdsNameOK(present, name) {
			tpdsPlain500(r) // Node: invalid name → Express 500 (no project created)
			return
		}

		// generateUniqueName (ensure-unique against the owner's project names) +
		// createBlankProject (BLANK: rootFolder w/ empty docs/fileRefs, no main.tex).
		unique := nzipEnsureUnique(nzipUserNames(a, c, uid), name)
		sp := "en"
		if u, okU := loadOwnerUser(a, c, uid); okU && u.spellCheckLanguage != "" {
			sp = u.spellCheckLanguage
		}
		pj := primitive.NewObjectID()
		rootID := primitive.NewObjectID()
		crInsertProject(a, c, pj, rootID, nil, unique, uid, sp, "pdflatex", bson.A{}, bson.A{}, 0)
		crInitHistory(c, pj.Hex())

		r.W.Header().Set("X-Powered-By", "Express")
		r.JSON(200, []byte(`{"projectId":"`+pj.Hex()+`"}`))
	}
}

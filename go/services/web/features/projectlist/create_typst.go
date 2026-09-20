// create_typst.go — P6.16: POST /project/new/typst (services/web/modules/typst
// — TypstRouter.mjs newTypstProject).
//
// Node oracle (2026-09-18, live-pinned; WEB_GO_PLAN.md P6.16):
//
// Schema (parseReq, REQ_VALIDATION_MODE=enforce-log → failures 400; the
// schema is z.object — NON-strict, unrecognized keys are STRIPPED, unlike
// the TeX /project/new strictObject battery):
//	body.projectName  string .trim() .max(100) .optional() .transform(empty→undefined)
//	body.template     string .max(50) .optional()   (null → 400 received null)
//  400 json = {"error":"Validation error: <issue> [; <issue>]","statusCode":400}
//   - type:  Invalid input: expected string, received <type> at "body.<field>"
//            (both fields' issues are collected, schema order)
//   - max:   Too big: expected string to have <=100 characters at "body.projectName"
//            Too big: expected string to have <=50 characters at "body.template"
//   - array body → Invalid input: expected object, received array at "body"
//   - invalid JSON / top-level number|string|null|boolean → 400 {}
//   (express.json strict — same family as the TeX route)
//
// Then Node's project-name check (400 text/plain, before creation):
//	"Project name cannot be blank"            (absent / whitespace / "")
//	"Project name is too long"                (>150 utf-16 — unreachable: zod 100 first)
//	"Project name cannot contain / characters"
//
// Creation (createBlankProject userId, name, {compiler:'typst'}) + template
// seed (buildTemplateFiles — lodash _.template with project_name:
// projectName||'My project'); lodash does NOT HTML-escape interpolations
// (verified against lodash 4.18.1 + the live rendered docs): plain
// substitution of `<%= project_name %>`. Only the basic and article
// templates carry the tag; example does not. First entry = root doc.
//
//	 template  docs                              version   files
//	 basic     main.typ                          1         -
//	 article   main.typ, references.bib          2         -
//	 example   main.typ, sample.bib              3         frog.jpg (same byte
//	            blob as the TeX example — one hash)
//	 anything else / absent → basic
//
// 200 = {project_id, owner_ref, owner:{first_name,last_name,email,_id}}
// (the TeX /project/new shape — same res.json).
//
// Global chain: anon POST → 403 "Forbidden" (pinned); csrf missing → 403.
//
// The typst template files read from disk (the CE image ships them under
// /overleaf/services/web/modules/typst/app/templates/project_files), same
// convention as crExampleProjectDir.

package projectlist

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
)

func crTypstTemplateDir() string {
	return crEnvOr("WEB_TYPST_PROJECTS_DIR",
		"/overleaf/services/web/modules/typst/app/templates/project_files")
}

// crTypstDocLines reads a template, applies lodash's plain `<%= project_name %>`
// substitution (raw — lodash 4.18.1 does not escape interpolation values) and
// splits exactly like Node's `.split('\n')`.
func crTypstDocLines(dir, rel, name string) []string {
	b, err := os.ReadFile(dir + "/" + rel)
	if err != nil {
		return []string{}
	}
	s := strings.ReplaceAll(string(b), "<%= project_name %>", name)
	return strings.Split(s, "\n")
}

// crCreateTypstProject — Node createBlankProject(compiler 'typst') +
// buildTemplateFiles seed. version = 0 (blank) + #addDoc + #addFile.
func crCreateTypstProject(a *core.App, cxt *core.Cxt, name, uid string, u crOwnerUser, tmpl string) primitive.ObjectID {
	dir := crTypstTemplateDir()

	switch {
	case tmpl == "article":
		pid := primitive.NewObjectID()
		mainID := primitive.NewObjectID()
		bibID := primitive.NewObjectID()
		rootID := primitive.NewObjectID()
		docs := bson.A{
			bson.D{{Key: "name", Value: "main.typ"}, {Key: "_id", Value: mainID}},
			bson.D{{Key: "name", Value: "references.bib"}, {Key: "_id", Value: bibID}},
		}
		crInsertProject(a, cxt, pid, rootID, &mainID, name, uid, u.spellCheckLanguage, "typst", docs, bson.A{}, 2)
		crInitHistory(cxt, pid.Hex())
		crCreateDocRevision(cxt, pid, mainID, crTypstDocLines(dir, "article/main.typ", name))
		crCreateDocRevision(cxt, pid, bibID, crTypstDocLines(dir, "article/references.bib", name))
		return pid

	case tmpl == "example":
		pid := primitive.NewObjectID()
		mainID := primitive.NewObjectID()
		bibID := primitive.NewObjectID()
		rootID := primitive.NewObjectID()

		frog, frogErr := os.ReadFile(dir + "/example/frog.jpg")
		var fileRefs bson.A
		hash := ""
		if frogErr == nil {
			hash = crGitBlobHash(frog)
			fileRefs = bson.A{bson.D{
				{Key: "name", Value: "frog.jpg"},
				{Key: "created", Value: time.Now().UTC()},
				{Key: "rev", Value: 0},
				{Key: "linkedFileData", Value: nil},
				{Key: "hash", Value: hash},
				{Key: "_id", Value: primitive.NewObjectID()},
			}}
		}
		docs := bson.A{
			bson.D{{Key: "name", Value: "main.typ"}, {Key: "_id", Value: mainID}},
			bson.D{{Key: "name", Value: "sample.bib"}, {Key: "_id", Value: bibID}},
		}
		crInsertProject(a, cxt, pid, rootID, &mainID, name, uid, u.spellCheckLanguage, "typst", docs, fileRefs, 3)
		crInitHistory(cxt, pid.Hex())
		crCreateDocRevision(cxt, pid, mainID, crTypstDocLines(dir, "example/main.typ", name))
		crCreateDocRevision(cxt, pid, bibID, crTypstDocLines(dir, "example/sample.bib", name))
		if frogErr == nil && hash != "" {
			crUploadBlob(cxt, pid.Hex(), hash, frog)
		}
		return pid

	default: // basic (incl. unknown/absent template — Node fallback)
		pid := primitive.NewObjectID()
		mainID := primitive.NewObjectID()
		rootID := primitive.NewObjectID()
		docs := bson.A{bson.D{
			{Key: "name", Value: "main.typ"},
			{Key: "_id", Value: mainID},
		}}
		crInsertProject(a, cxt, pid, rootID, &mainID, name, uid, u.spellCheckLanguage, "typst", docs, bson.A{}, 1)
		crInitHistory(cxt, pid.Hex())
		crCreateDocRevision(cxt, pid, mainID, crTypstDocLines(dir, "mainbasic.typ", name))
		return pid
	}
}

// crParseTypstBody implements the NON-strict typst schema (unlike the TeX
// strict battery): type + max checks on both fields, schema order, no
// Unrecognized-key errors.
func crParseTypstBody(raw []byte) crParseResult {
	if len(bytes.TrimSpace(raw)) == 0 {
		return crParseResult{ok: true, body: &crBody{}}
	}
	if !json.Valid(raw) {
		return crParseResult{bare: true}
	}
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	switch trimmed[0] {
	case '[':
		return crParseResult{zodMsg: `Invalid input: expected object, received array at "body"`}
	case '{':
		// object — continue
	default:
		return crParseResult{bare: true}
	}

	var bm map[string]any
	_ = json.Unmarshal(raw, &bm)
	present := func(k string) bool { _, ok := bm[k]; return ok }

	var errs []string
	if present("projectName") {
		s, ok := bm["projectName"].(string)
		if !ok {
			errs = append(errs, "Invalid input: expected string, received "+crZodType(bm["projectName"])+` at "body.projectName"`)
		} else if crUTF16Len(s) > 100 {
			errs = append(errs, `Too big: expected string to have <=100 characters at "body.projectName"`)
		}
	}
	if present("template") {
		s, ok := bm["template"].(string)
		if !ok {
			errs = append(errs, "Invalid input: expected string, received "+crZodType(bm["template"])+` at "body.template"`)
		} else if crUTF16Len(s) > 50 {
			errs = append(errs, `Too big: expected string to have <=50 characters at "body.template"`)
		}
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

// newTypstProjectHandler — POST /project/new/typst.
func newTypstProjectHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if cxt.Sess == nil || !cxt.Sess.IsLoggedIn() {
			res.SendStatus(401)
			return
		}
		uid := cxt.Sess.UserIDHex()

		raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		pr := crParseTypstBody(raw)
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

		name := ""
		if pr.body.projectPresent {
			name = strings.TrimSpace(pr.body.projectName) // Node .trim()
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

		tmpl := ""
		if pr.body.tmplPresent {
			tmpl = pr.body.template
		}
		pid := crCreateTypstProject(a, cxt, name, uid, u, tmpl)

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

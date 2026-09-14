package serveradmin

import (
	"ollitex/go/services/web/core"
)

// ---------- handlers ----------

func editorState(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		// `!== false` semantics for both fields (pinned:
		// {"editorIsOpen":true,"siteIsOpen":true}).
		body := `{"editorIsOpen":` + boolJSON(core.EditorOpen()) +
			`,"siteIsOpen":` + boolJSON(core.SiteOpen()) + `}`
		res.JSON(200, []byte(body))
	}
}

func openEditor(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		// Node: no body validation (openEditor(req,res) ignores body).
		drainBody(cxt.Req)
		core.SetEditorOpen(true, true)
		res.Redirect(cxt.Req, 302, "/admin#open-close-editor")
	}
}

func closeEditor(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		body, keyOrder, kind, ok := readBody(cxt.Req)
		if !ok {
			if kind == "scalar" {
				res.BareWrite(400, []byte("{}"))
				return
			}
			res.JSON(400, validationError(`Invalid input: expected object, received `+kind+` at \"body\"`))
			return
		}
		var issues []string
		isOpen, present := body["isOpen"]
		if present && !isBool(isOpen) {
			t := "undefined"
			if isOpen != nil {
				t = zodType(isOpen)
			}
			issues = append(issues, `Invalid input: expected boolean, received `+t+` at \"body.isOpen\"`)
		}
		if u, bad := unrecognized(body, keyOrder, "isOpen"); bad {
			issues = append(issues, u)
		}
		if len(issues) > 0 {
			res.JSON(400, validationError(joinIssues(issues)))
			return
		}
		if present && isOpen != nil {
			core.SetEditorOpen(isOpen.(bool), true)
		} else {
			// Node: `Settings.editorIsOpen = body.isOpen` — missing (or
			// null) → the /status reader sees CLOSED while editor-state
			// reports OPEN (pinned live: the tri-state divergence).
			core.SetEditorOpen(false, false)
		}
		res.Redirect(cxt.Req, 302, "/admin#open-close-editor")
	}
}

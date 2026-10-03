package serveradmin

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"

	"ollitex/go/services/web/core"
)

// ---- disconnectAllUsers (Node AdminController) ----------------------------------

var (
	saEventCounter uint64
	saEventID = func() string {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		return fmt.Sprintf("%x", b)
	}()
)

// saEmitRoom — EditorRealTimeController.emitToRoom: channel `editor-events`
// with Node's blob {room_id,message,payload,_id}. (Same wire contract as
// trackchanges' tcEmitRoom; duplicated here to avoid a cross-feature import.)
func saEmitRoom(a *core.App, roomID, message string, payload []interface{}) {
	if a == nil || a.Redis == nil {
		return
	}
	n := atomic.AddUint64(&saEventCounter, 1)
	host, _ := os.Hostname()
	if host == "" {
		host = "web"
	}
	pl, _ := json.Marshal(payload)
	blob, _ := json.Marshal(struct {
		RoomID  string          `json:"room_id"`
		Message string          `json:"message"`
		Payload json.RawMessage `json:"payload"`
		ID      string          `json:"_id"`
	}{roomID, message, pl, "web:" + host + ":" + saEventID + "-" + fmt.Sprint(n)})
	_ = a.Redis.Publish("editor-events", string(blob))
}

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

func disconnectAllUsers(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		// Node: delay = query.delay > 0 ? query.delay : 10;
		//       emitToAll('forceDisconnect', MSG, delay); redirect('/admin#...').
		delay := "10"
		if dq := cxt.Req.URL.Query().Get("delay"); dq != "" {
			if n, err := strconv.Atoi(dq); err == nil && n > 0 {
				delay = strconv.Itoa(n)
			}
		}
		msg := "Sorry, we are performing a quick update to the editor and need to close it down. Please refresh the page to continue."
		drainBody(cxt.Req)
		saEmitRoom(a, "all", "forceDisconnect", []interface{}{msg, delay})
		res.Redirect(cxt.Req, 302, "/admin#open-close-editor")
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

// P6.12 — the 11 track-changes route handlers.
//
// Node flows (TrackChangesController + ChatApiHandler/DocumentUpdaterHandler,
// oracle-verified 2026-09-18; chat = the shared Go chat service on :3010,
// document-updater (Node) on :3003):
//   track_changes : validate → project.updateOne → emit → 204
//   accept        : DU accept (first) → emit → 204                 [write chain]
//   ranges        : DU ranges.docs                                  [read chain]
//   changes/users : docstore ids → formatPersonalInfo each          [read chain]
//   threads       : chat threads → injectUserInfoIntoThreads        [read chain]
//   send          : chat send → user → emit → 204                   [read chain]
//   edit          : GET message → author-or-403 → chat edit → emit → 204
//   del-message   : GET message → author-or-403 → chat user-scoped delete → emit → 204
//   resolve       : user → chat resolve → emit → DU resolve  → 204
//   reopen        : chat reopen → emit → DU reopen   → 204
//   del-thread    : chat delete → emit → DU delete   → 204         [write chain]
//
// Every downstream failure (transport or non-2xx) → next(err) → rendered
// 500 page (Node: general/500 view). ForbiddenError → 403 restricted VIEW
// (ErrorController.forbidden — always HTML, even with accept-json).
// Limiters (shared redis keys): reads = ranges/users/threads (60/min);
// writes = everything else (20/min) — per the Node router wiring.

package trackchanges

import (
	"encoding/json"
	"io"
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// tcBodyOf — the request body as a JSON object ({} when absent). The core
// pre-handler rejects scalar roots before any handler runs.
func tcBodyOf(cxt *core.Cxt) map[string]interface{} {
	out := map[string]interface{}{}
	if cxt == nil || cxt.Req == nil || cxt.Req.Body == nil {
		return out
	}
	b, err := io.ReadAll(io.LimitReader(cxt.Req.Body, 4<<20))
	if err != nil || len(b) == 0 {
		return out
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]interface{}{}
	}
	return out
}

func tc204(res *core.Res) { res.W.WriteHeader(204) }

func tcJSONStr(s string) string {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // Node's JSON.stringify: " stays \", not \u0022
	if err := enc.Encode(s); err != nil {
		return `""`
	}
	return strings.TrimRight(buf.String(), "\n")
}

// tcForbidden — ErrorController.forbidden (always the restricted VIEW):
// res.status(403); res.render('user/restricted').
func tcForbidden(cxt *core.Cxt, res *core.Res) {
	views.Restricted403(res.W, tcPageData(cxt, tcRelPath(cxt)))
}

// tcPreflightMessage — Node edit/delete pre-check:
// getThreadMessage(...) — any failure (404/5xx) surfaces as the 500 page;
// on success the message's user_id drives the author check.
func tcPreflightMessage(cxt *core.Cxt, a *core.App, pid, tid, mid string) (string, bool) {
	b, ok := tcCall(cxt.Req.Context(), "GET", tcChatBase()+"/project/"+pid+"/thread/"+tid+"/messages/"+mid, "")
	if !ok {
		return "", false
	}
	var m struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return "", false
	}
	return m.UserID, true
}

// ---- track_changes -----------------------------------------------------------

func hTrackChanges(a *core.App, lim *tcLimitRunner) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !lim.writes(cxt, res) {
			return
		}
		if _, ok := tcAuthzProject(a, cxt, res, false); !ok {
			return
		}
		body := tcBodyOf(cxt)
		state, msg := tcTCState(body)
		if msg != nil {
			res.JSON(400, []byte(`{"message":`+tcJSONStr(*msg)+`}`))
			return
		}
		if a.Mongo == nil {
			tcErr500(cxt, res)
			return
		}
		oid, _ := primitive.ObjectIDFromHex(strings.ToLower(cxt.Params["1"]))
		db, err := a.Mongo.DB(cxt.Req.Context())
		if err != nil {
			tcErr500(cxt, res)
			return
		}
		if _, uerr := db.Collection("projects").UpdateOne(cxt.Req.Context(),
			bson.D{{Key: "_id", Value: oid}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "track_changes", Value: state}}}}); uerr != nil {
			tcErr500(cxt, res)
			return
		}
		var sv interface{}
		ser, _ := json.Marshal(state)
		_ = json.Unmarshal(ser, &sv)
		tcEmitRoom(a, cxt.Params["1"], "toggle-track-changes", []interface{}{sv})
		tc204(res)
	}
}

// ---- accept changes (document-updater first, then announce) -------------------

func hAcceptChanges(a *core.App, lim *tcLimitRunner) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !lim.writes(cxt, res) {
			return
		}
		if _, ok := tcAuthzProject(a, cxt, res, true); !ok {
			return
		}
		pid, docID := cxt.Params["1"], cxt.Params["2"]
		body := tcBodyOf(cxt)
		duBody := `{}`
		cids, hasCids := body["change_ids"]
		if hasCids {
			if ser, err := json.Marshal(cids); err == nil {
				duBody = `{"change_ids":` + string(ser) + `}`
			}
		}
		b, ok := tcCall(cxt.Req.Context(), "POST", tcDuBase()+"/project/"+pid+"/doc/"+docID+"/change/accept", duBody)
		if !ok || b == nil {
			tcErr500(cxt, res)
			return
		}
		// Node: emitToRoom(project_id, 'accept-changes', doc_id, change_ids);
		// an undefined change_ids is dropped by JSON.stringify.
		payload := []interface{}{docID}
		if hasCids {
			payload = append(payload, cids)
		}
		tcEmitRoom(a, pid, "accept-changes", payload)
		tc204(res)
	}
}

// ---- ranges / changes-users -----------------------------------------------------

func hGetAllRanges(a *core.App, lim *tcLimitRunner) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !lim.reads(cxt, res) {
			return
		}
		pid, ok := tcAuthzProject(a, cxt, res, false)
		if !ok {
			return
		}
		b, ok := tcCall(cxt.Req.Context(), "GET", tcDuBase()+"/project/"+pid+"/ranges", "")
		if !ok {
			tcErr500(cxt, res)
			return
		}
		var wrap struct {
			Docs json.RawMessage `json:"docs"`
		}
		if err := json.Unmarshal(b, &wrap); err != nil || wrap.Docs == nil {
			tcErr500(cxt, res)
			return
		}
		res.JSON(200, []byte(wrap.Docs))
	}
}

func hGetChangesUsers(a *core.App, lim *tcLimitRunner) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !lim.reads(cxt, res) {
			return
		}
		pid, ok := tcAuthzProject(a, cxt, res, false)
		if !ok {
			return
		}
		b, ok := tcCall(cxt.Req.Context(), "GET", tcDocstoreBase()+"/project/"+pid+"/tracked-changes-user-ids", "")
		if !ok {
			tcErr500(cxt, res)
			return
		}
		var ids []string
		if err := json.Unmarshal(b, &ids); err != nil {
			tcErr500(cxt, res)
			return
		}
		parts := make([]string, 0, len(ids))
		ctx := cxt.Req.Context()
		for _, uid := range ids {
			p, okP := tcPersonalJSON(ctx, a, uid)
			if !okP {
				tcErr500(cxt, res)
				return
			}
			parts = append(parts, p) // Node: missing user → {} (formatPersonalInfo(null))
		}
		res.JSON(200, []byte("["+strings.Join(parts, ",")+"]"))
	}
}

// ---- threads (chat + user injection) -------------------------------------------

type tcMsg struct {
	ID        string          `json:"id"`
	Content   json.RawMessage `json:"content"`
	Timestamp int64           `json:"timestamp"`
	UserID    string          `json:"user_id"`
	EditedAt  *int64          `json:"edited_at,omitempty"`
	RoomID    string          `json:"room_id,omitempty"`
}

type tcThread struct {
	Messages      []tcMsg            `json:"messages"`
	Resolved      *bool              `json:"resolved,omitempty"`
	ResolvedAt    *string            `json:"resolved_at,omitempty"`
	ResolvedByHex *string            `json:"resolved_by_user_id,omitempty"`
	ResolvedByRaw string             `json:"-"` // injected (tail key)
	UserFrags     map[string]*string `json:"-"` // user_id → injected user JSON
}

func tcThreadTail(t *tcThread) string {
	var sb strings.Builder
	if t.Resolved != nil {
		if *t.Resolved {
			sb.WriteString(`,"resolved":true`)
		} else {
			sb.WriteString(`,"resolved":false`)
		}
	}
	if t.ResolvedAt != nil {
		s, _ := json.Marshal(t.ResolvedAt)
		sb.WriteString(`,"resolved_at":`)
		sb.Write(s)
	}
	if t.ResolvedByHex != nil {
		s, _ := json.Marshal(t.ResolvedByHex)
		sb.WriteString(`,"resolved_by_user_id":`)
		sb.Write(s)
	}
	return sb.String()
}

// tcSerializeThread — Node final key order (observed live):
//
//	thread : messages, resolved?, resolved_at?, resolved_by_user_id?,
//	         resolved_by_user? (only when resolved truthy)
//	message: id, content, timestamp, user_id, edited_at?, room_id?, user?
//	         (user ABSENT when the user is missing — never null)
func tcSerializeThread(t *tcThread) (string, error) {
	msgs := make([]string, 0, len(t.Messages))
	for i := range t.Messages {
		m := t.Messages[i]
		var user *json.RawMessage
		if t.UserFrags != nil {
			if raw, has := t.UserFrags[m.UserID]; has && raw != nil {
				rj := json.RawMessage(*raw)
				user = &rj
			}
		}
		mj, err := json.Marshal(struct {
			ID        string           `json:"id"`
			Content   json.RawMessage  `json:"content"`
			Timestamp int64            `json:"timestamp"`
			UserID    string           `json:"user_id"`
			EditedAt  *int64           `json:"edited_at,omitempty"`
			RoomID    string           `json:"room_id,omitempty"`
			User      *json.RawMessage `json:"user,omitempty"`
		}{m.ID, m.Content, m.Timestamp, m.UserID, m.EditedAt, m.RoomID, user})
		if err != nil {
			return "", err
		}
		msgs = append(msgs, string(mj))
	}
	tail := ""
	if t.Resolved != nil && *t.Resolved && t.ResolvedByRaw != "" {
		tail = `,"resolved_by_user":` + t.ResolvedByRaw
	}
	return `{"messages":[` + strings.Join(msgs, ",") + `]` + tcThreadTail(t) + tail + "}", nil
}

func tcGetThreads(cxt *core.Cxt, a *core.App, pid string) ([]byte, bool) {
	ctx := cxt.Req.Context()
	b, ok := tcCall(ctx, "GET", tcChatBase()+"/project/"+pid+"/threads", "")
	if !ok {
		return nil, false
	}
	var outer map[string]tcThread
	if err := json.Unmarshal(b, &outer); err != nil {
		return nil, false
	}
	ids := map[string]bool{}
	for _, t := range outer {
		if t.Resolved != nil && *t.Resolved && t.ResolvedByHex != nil {
			ids[*t.ResolvedByHex] = true
		}
		for _, m := range t.Messages {
			ids[m.UserID] = true
		}
	}
	personal := map[string]string{}
	for uid := range ids {
		p, okP := tcPersonalJSON(ctx, a, uid)
		if !okP {
			return nil, false
		}
		personal[uid] = p
	}
	keys := make([]string, 0, len(outer))
	for k := range outer {
		keys = append(keys, k)
	}
	sort.Strings(keys) // go-chat marshals the outer map with sorted keys
	var parts []string
	for _, k := range keys {
		t := outer[k]
		t.UserFrags = map[string]*string{}
		for _, m := range t.Messages {
			if p, has := personal[m.UserID]; has {
				v := p
				t.UserFrags[m.UserID] = &v
			}
		}
		if t.Resolved != nil && *t.Resolved && t.ResolvedByHex != nil {
			if p, has := personal[*t.ResolvedByHex]; has {
				t.ResolvedByRaw = p
			}
		}
		s, err := tcSerializeThread(&t)
		if err != nil {
			return nil, false
		}
		ks, _ := json.Marshal(k)
		parts = append(parts, string(ks)+":"+s)
	}
	return []byte("{" + strings.Join(parts, ",") + "}"), true
}

func hGetThreads(a *core.App, lim *tcLimitRunner) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !lim.reads(cxt, res) {
			return
		}
		pid, ok := tcAuthzProject(a, cxt, res, false)
		if !ok {
			return
		}
		b, okT := tcGetThreads(cxt, a, pid)
		if !okT {
			tcErr500(cxt, res)
			return
		}
		res.JSON(200, b)
	}
}

// ---- send comment ---------------------------------------------------------------

func hSendComment(a *core.App, lim *tcLimitRunner) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !lim.writes(cxt, res) {
			return
		}
		pid, ok := tcAuthzProject(a, cxt, res, false)
		if !ok {
			return
		}
		tid := cxt.Params["2"]
		body := tcBodyOf(cxt)
		content, _ := body["content"].(string)
		uid := tcUID(cxt)
		reqBody, _ := json.Marshal(struct {
			UserID  string `json:"user_id"`
			Content string `json:"content"`
		}{uid, content})
		b, okS := tcCall(cxt.Req.Context(), "POST", tcChatBase()+"/project/"+pid+"/thread/"+tid+"/messages", string(reqBody))
		if !okS || b == nil {
			tcErr500(cxt, res)
			return
		}
		userRaw, okU := tcPersonalJSON(cxt.Req.Context(), a, uid)
		if !okU {
			tcErr500(cxt, res)
			return
		}
		// Node: message = chat comment object + message.user appended;
		// emitToRoom(project_id, 'new-comment', thread_id, message) — payload
		// [thread_id, message] — then 204.
		var cm tcMsg
		_ = json.Unmarshal(b, &cm)
		var user *json.RawMessage
		rj := json.RawMessage(userRaw)
		user = &rj
		mj, err := json.Marshal(struct {
			ID        string           `json:"id"`
			Content   json.RawMessage  `json:"content"`
			Timestamp int64            `json:"timestamp"`
			UserID    string           `json:"user_id"`
			EditedAt  *int64           `json:"edited_at,omitempty"`
			RoomID    string           `json:"room_id,omitempty"`
			User      *json.RawMessage `json:"user,omitempty"`
		}{cm.ID, cm.Content, cm.Timestamp, cm.UserID, cm.EditedAt, cm.RoomID, user})
		if err != nil {
			tcErr500(cxt, res)
			return
		}
		tcEmitRoom(a, pid, "new-comment", []interface{}{tid, mj})
		tc204(res)
	}
}

// ---- edit / delete message (author-gated) ---------------------------------------

func hEditMessage(a *core.App, lim *tcLimitRunner) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !lim.writes(cxt, res) {
			return
		}
		if _, ok := tcAuthzProject(a, cxt, res, false); !ok {
			return
		}
		pid, tid, mid := cxt.Params["1"], cxt.Params["2"], cxt.Params["3"]
		uid := tcUID(cxt)
		msgUID, okM := tcPreflightMessage(cxt, a, pid, tid, mid)
		if !okM {
			tcErr500(cxt, res)
			return
		}
		if msgUID != uid { // Node: String(message.user_id) !== String(user_id) → ForbiddenError → 403 view
			tcForbidden(cxt, res)
			return
		}
		body := tcBodyOf(cxt)
		content, _ := body["content"].(string)
		reqBody, _ := json.Marshal(struct {
			Content string `json:"content"`
			UserID  string `json:"userId"`
		}{content, uid})
		b, okC := tcCall(cxt.Req.Context(), "POST", tcChatBase()+"/project/"+pid+"/thread/"+tid+"/messages/"+mid+"/edit", string(reqBody))
		if !okC || b == nil {
			tcErr500(cxt, res)
			return
		}
		tcEmitRoom(a, pid, "edit-message", []interface{}{tid, mid, content})
		tc204(res)
	}
}

func hDeleteMessage(a *core.App, lim *tcLimitRunner) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !lim.writes(cxt, res) {
			return
		}
		if _, ok := tcAuthzProject(a, cxt, res, false); !ok {
			return
		}
		pid, tid, mid := cxt.Params["1"], cxt.Params["2"], cxt.Params["3"]
		uid := tcUID(cxt)
		msgUID, okM := tcPreflightMessage(cxt, a, pid, tid, mid)
		if !okM {
			tcErr500(cxt, res)
			return
		}
		if msgUID != uid {
			tcForbidden(cxt, res)
			return
		}
		// Node: deleteUserMessage → DELETE .../user/:userId/messages/:messageId
		b, okC := tcCall(cxt.Req.Context(), "DELETE", tcChatBase()+"/project/"+pid+"/thread/"+tid+"/user/"+uid+"/messages/"+mid, "")
		if !okC || b == nil {
			tcErr500(cxt, res)
			return
		}
		tcEmitRoom(a, pid, "delete-message", []interface{}{tid, mid})
		tc204(res)
	}
}

// ---- resolve / reopen / delete thread (chat then DU) ----------------------------

func hResolveThread(a *core.App, lim *tcLimitRunner) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !lim.writes(cxt, res) {
			return
		}
		if _, ok := tcAuthzProject(a, cxt, res, false); !ok {
			return
		}
		pid := cxt.Params["1"]
		tid := cxt.Params["3"]
		uid := tcUID(cxt)
		userRaw, okU := tcPersonalJSON(cxt.Req.Context(), a, uid)
		if !okU {
			tcErr500(cxt, res)
			return
		}
		reqBody, _ := json.Marshal(struct {
			UserID string `json:"user_id"`
		}{uid})
		if b, okC := tcCall(cxt.Req.Context(), "POST", tcChatBase()+"/project/"+pid+"/thread/"+tid+"/resolve", string(reqBody)); !okC || b == nil {
			tcErr500(cxt, res)
			return
		}
		tcEmitRoom(a, pid, "resolve-thread", []interface{}{tid, userRaw})
		duBody, _ := json.Marshal(struct {
			UserID string `json:"user_id"`
		}{uid})
		if b, okD := tcCall(cxt.Req.Context(), "POST", tcDuBase()+"/project/"+pid+"/doc/"+cxt.Params["2"]+"/comment/"+tid+"/resolve", string(duBody)); !okD || b == nil {
			tcErr500(cxt, res)
			return
		}
		tc204(res)
	}
}

func hReopenThread(a *core.App, lim *tcLimitRunner) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !lim.writes(cxt, res) {
			return
		}
		if _, ok := tcAuthzProject(a, cxt, res, false); !ok {
			return
		}
		pid := cxt.Params["1"]
		tid := cxt.Params["3"]
		uid := tcUID(cxt)
		b, ok := tcCall(cxt.Req.Context(), "POST", tcChatBase()+"/project/"+pid+"/thread/"+tid+"/reopen", "")
		if !ok || b == nil {
			tcErr500(cxt, res)
			return
		}
		tcEmitRoom(a, pid, "reopen-thread", []interface{}{tid})
		duBody, _ := json.Marshal(struct {
			UserID string `json:"user_id"`
		}{uid})
		if b, okD := tcCall(cxt.Req.Context(), "POST", tcDuBase()+"/project/"+pid+"/doc/"+cxt.Params["2"]+"/comment/"+tid+"/reopen", string(duBody)); !okD || b == nil {
			tcErr500(cxt, res)
			return
		}
		tc204(res)
	}
}

func hDeleteThread(a *core.App, lim *tcLimitRunner) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !lim.writes(cxt, res) {
			return
		}
		if _, ok := tcAuthzProject(a, cxt, res, true); !ok {
			return
		}
		pid := cxt.Params["1"]
		tid := cxt.Params["3"]
		uid := tcUID(cxt)
		b, ok := tcCall(cxt.Req.Context(), "DELETE", tcChatBase()+"/project/"+pid+"/thread/"+tid, "")
		if !ok || b == nil {
			tcErr500(cxt, res)
			return
		}
		tcEmitRoom(a, pid, "delete-thread", []interface{}{tid})
		duBody, _ := json.Marshal(struct {
			UserID string `json:"user_id"`
		}{uid})
		if b, okD := tcCall(cxt.Req.Context(), "DELETE", tcDuBase()+"/project/"+pid+"/doc/"+cxt.Params["2"]+"/comment/"+tid, string(duBody)); !okD || b == nil {
			tcErr500(cxt, res)
			return
		}
		tc204(res)
	}
}

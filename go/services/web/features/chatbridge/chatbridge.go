// Package chatbridge ports the Node chat router (ChatController routes)
// onto the Go web, proxying to the Go chat service (127.0.0.1:3010):
//
//	GET    /project/:Project_id/messages             (ensureUserCanReadProject)
//	POST   /project/:Project_id/messages             (ensureUserCanWriteProject)
//	DELETE /project/:Project_id/messages/:messageId  (author or owner)
//	POST   /project/:Project_id/messages/:messageId/edit (author or owner)
//
// Contracts (Node services/web chat router + chat service, 2026-09-30 audit):
//   - GET: query {limit, before} passthrough; the chat service JSON message
//     array is returned verbatim (empty array when none).
//   - POST: the modern frontend sends {content, client_id}; the Go chat
//     service body contract is {content, userId?} — client_id is a
//     client-side dedup key and is NOT part of the server contract, so the
//     web strips it and injects userId from the session (Node's
//     ChatController did the same with req.user.id).
//   - DELETE/EDIT: author of the message or the project owner
//     (Node ensureUserCanDeleteOrEditMessage); otherwise 403 restricted.
//   - The authz chain mirrors the compile feature's pinned shapes: invalid
//     ObjectId -> 404 JSON, absent project -> 404 HTML, non-member -> 403
//     (JSON restricted on accept-json / HTML user/restricted otherwise —
//     the core views helpers keep it simple: JSON here, as the modern
//     frontend is the consumer).
package chatbridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

var (
	msgListPat = regexp.MustCompile(`^/[Pp]roject/([^/]+)/messages$`)
	msgDelPat  = regexp.MustCompile(`^/[Pp]roject/([^/]+)/messages/([^/]+)$`)
	msgEditPat = regexp.MustCompile(`^/[Pp]roject/([^/]+)/messages/([^/]+)/edit$`)
	validOID   = regexp.MustCompile(`^[0-9a-f]{24}$`)
)

const (
	chatBase    = "http://127.0.0.1:3010"
	notFound404 = `{"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}`
	internal500 = `{"error":{"type":"InternalServerError","message":"Internal Server Error"}}`
	restricted  = `{"message":"restricted"}`
)

func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "chatbridge",
		Routes: []core.Route{
			{Method: "GET", Pattern: msgListPat, Handler: messagesHandler(a, false)},
			{Method: "POST", Pattern: msgListPat, Handler: messagesHandler(a, true)},
			{Method: "DELETE", Pattern: msgDelPat, Handler: itemHandler(a, false)},
			{Method: "POST", Pattern: msgEditPat, Handler: itemHandler(a, true)},
		},
	}
}

func oidHex(v any) string {
	if o, ok := v.(bson.ObjectID); ok {
		return o.Hex()
	}
	if s, ok := v.(string); ok {
		return strings.ToLower(s)
	}
	return ""
}

func inList(v any, uid string) bool {
	for _, x := range asArr(v) {
		if oidHex(x) == strings.ToLower(uid) {
			return true
		}
	}
	return false
}

func asArr(v any) []any {
	a, _ := v.([]any)
	return a
}

func dget(d bson.D, key string) any {
	for i := range d {
		if d[i].Key == key {
			return d[i].Value
		}
	}
	return nil
}

// loadProject fetches the project doc (or nil when absent).
func loadProject(ctx context.Context, a *core.App, idHex string) (*bson.D, error) {
	oid, err := bson.ObjectIDFromHex(strings.ToLower(idHex))
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
	var d bson.D
	if err := db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// chatUserWire is the shape the editor chat pane reads off each message:
// message.user.{id, first_name, last_name?, email}. The Go chat service
// returns only user_id (string); the Node web ChatController enriched the
// sender details, so we do the same here (R: own message rendered as
// "Deleted user" because message.user was undefined).
func chatUserWire(ctx context.Context, a *core.App, idHex string) map[string]any {
	uid, err := bson.ObjectIDFromHex(strings.ToLower(strings.TrimSpace(idHex)))
	if err != nil {
		return nil
	}
	if a.Mongo == nil {
		return nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil
	}
	var d bson.D
	if err := db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: uid}}).
		Decode(&d); err != nil {
		return nil
	}
	if v, ok := dget(d, "deleted").(bool); ok && v {
		return nil
	}
	out := map[string]any{"id": uid.Hex()}
	if s, ok := dget(d, "first_name").(string); ok {
		out["first_name"] = s
	}
	if s, ok := dget(d, "last_name").(string); ok {
		out["last_name"] = s
	}
	if s, ok := dget(d, "email").(string); ok {
		out["email"] = s
	}
	return out
}

// enrichChatMessages adds a resolved `user` object to each message that
// carries a user_id (keeping user_id for parity). Unknown/deleted senders
// stay without a user object (the pane shows the deleted-user affordance).
func enrichChatMessages(ctx context.Context, a *core.App, body []byte) []byte {
	var msgs []map[string]any
	if err := json.Unmarshal(body, &msgs); err != nil {
		return body
	}
	seen := map[string]map[string]any{}
	for _, m := range msgs {
		s, _ := m["user_id"].(string)
		if s == "" {
			continue
		}
		k := strings.ToLower(s)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = chatUserWire(ctx, a, s)
	}
	changed := false
	for _, m := range msgs {
		s, _ := m["user_id"].(string)
		if s == "" {
			continue
		}
		if u, ok := seen[strings.ToLower(s)]; ok && u != nil {
			m["user"] = u
			changed = true
		}
	}
	if !changed {
		return body
	}
	out, err := json.Marshal(msgs)
	if err != nil {
		return body
	}
	return out
}

// gate applies the read/write project membership check (owner,
// collabs, reviewers, read-only refs for reads; owner/collabs for writes).
func gate(a *core.App, cxt *core.Cxt, res *core.Res, write bool) (uid string, p *bson.D, ok bool) {
	if cxt.Sess == nil || cxt.Sess.UserIDHex() == "" {
		// anonymous: Node's session gate bounces before the authz (the JSON
		// chat API sees 403 restricted).
		coreJSON(cxt, res, 403, []byte(restricted))
		return
	}
	uid = cxt.Sess.UserIDHex()
	seg := cxt.Params["1"]
	if !validOID.MatchString(seg) {
		coreJSON(cxt, res, 404, []byte(notFound404))
		return
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
	defer cancel()
	pd, lerr := loadProject(ctx, a, seg)
	if lerr != nil {
		coreJSON(cxt, res, 500, []byte(internal500))
		return
	}
	if pd == nil {
		coreJSON(cxt, res, 404, nil)
		return
	}
	owner := oidHex(dget(*pd, "owner_ref"))
	if owner == "" {
		owner = oidHex(dget(*pd, "owner"))
	}
	collab := dget(*pd, "collab_refs")
	if collab == nil {
		collab = dget(*pd, "collaberator_refs")
	}
	rw := strings.EqualFold(owner, strings.ToLower(uid)) || inList(collab, uid)
	if write {
		if !rw {
			coreJSON(cxt, res, 403, []byte(restricted))
			return
		}
	} else {
		review := dget(*pd, "reviewer_refs")
		read := dget(*pd, "readOnly_refs")
		tokRW := dget(*pd, "tokenAccessReadAndWrite_refs")
		tokRO := dget(*pd, "tokenAccessReadOnly_refs")
		pl, _ := dget(*pd, "publicAccesLevel").(string)
		can := rw || inList(review, uid) || inList(read, uid) ||
			inList(tokRW, uid) || inList(tokRO, uid) ||
			pl == "readOnly" || pl == "readAndWrite"
		if !can {
			coreJSON(cxt, res, 403, []byte(restricted))
			return
		}
	}
	p = pd
	ok = true
	return
}

// coreJSON writes the chat API JSON response (the modern frontend is JSON;
// the accept-HTML branches live in the other features' views helpers).
func coreJSON(cxt *core.Cxt, res *core.Res, code int, body []byte) {
	if body == nil {
		res.W.WriteHeader(code)
		return
	}
	res.JSON(code, body)
}

func chatRoundTrip(cxt *core.Cxt, method, target string, body []byte) (int, []byte, error) {
	var rdr *strings.Reader
	if body != nil {
		rdr = strings.NewReader(string(body))
	} else {
		rdr = strings.NewReader("")
	}
	req, err := http.NewRequest(method, target, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 60*time.Second)
	defer cancel()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, b, nil
}

func messagesHandler(a *core.App, send bool) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid, _, ok := gate(a, cxt, res, send)
		if !ok {
			return
		}
		pid := cxt.Params["1"]
		if !send {
			u := chatBase + "/project/" + pid + "/messages?" + cxt.Req.URL.Query().Encode()
			code, body, err := chatRoundTrip(cxt, "GET", u, nil)
			if err != nil {
				coreJSON(cxt, res, 500, []byte(internal500))
				return
			}
			if code == 200 {
				body = enrichChatMessages(cxt.Req.Context(), a, body)
			}
			coreJSON(cxt, res, code, body)
			return
		}
		raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		var bp map[string]any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &bp)
		}
		if bp == nil {
			bp = map[string]any{}
		}
		content, _ := bp["content"].(string)
		// chat service send contract: {user_id (required OID), content} — strict.
		out := map[string]any{"user_id": uid, "content": content}
		body, merr := json.Marshal(out)
		if merr != nil {
			coreJSON(cxt, res, 500, []byte(internal500))
			return
		}
		u := chatBase + "/project/" + pid + "/messages"
		code, cbody, err := chatRoundTrip(cxt, "POST", u, body)
		if err != nil {
			coreJSON(cxt, res, 500, []byte(internal500))
			return
		}
		coreJSON(cxt, res, code, cbody)
	}
}

func itemHandler(a *core.App, edit bool) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid, pd, ok := gate(a, cxt, res, true)
		if !ok {
			return
		}
		pid := cxt.Params["1"]
		mid := cxt.Params["2"]
		if !validOID.MatchString(mid) {
			coreJSON(cxt, res, 404, []byte(`{"error":"Validation error: Invalid Mongo ObjectId at \"params.messageId\"","statusCode":404}`))
			return
		}
		gu := chatBase + "/project/" + pid + "/messages/" + mid
		code, mbody, err := chatRoundTrip(cxt, "GET", gu, nil)
		if err != nil {
			coreJSON(cxt, res, 500, []byte(internal500))
			return
		}
		if code != 200 {
			coreJSON(cxt, res, code, mbody)
			return
		}
		var ms map[string]any
		_ = json.Unmarshal(mbody, &ms)
		author, _ := ms["user_id"].(string)
		owner := oidHex(dget(*pd, "owner_ref"))
		if strings.ToLower(author) != strings.ToLower(uid) && owner != strings.ToLower(uid) {
			coreJSON(cxt, res, 403, []byte(restricted))
			return
		}
		if !edit {
			dcode, dbody, derr := chatRoundTrip(cxt, "DELETE", gu, nil)
			if derr != nil {
				coreJSON(cxt, res, 500, []byte(internal500))
				return
			}
			coreJSON(cxt, res, dcode, dbody)
			return
		}
		raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		var bp map[string]any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &bp)
		}
		if bp == nil {
			bp = map[string]any{}
		}
		content, _ := bp["content"].(string)
		out := map[string]any{"content": content, "userId": uid}
		eb, merr := json.Marshal(out)
		if merr != nil {
			coreJSON(cxt, res, 500, []byte(internal500))
			return
		}
		ecode, ebody, eerr := chatRoundTrip(cxt, "POST", gu+"/edit", eb)
		if eerr != nil {
			coreJSON(cxt, res, 500, []byte(internal500))
			return
		}
		coreJSON(cxt, res, ecode, ebody)
	}
}

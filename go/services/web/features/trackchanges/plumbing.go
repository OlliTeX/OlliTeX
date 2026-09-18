package trackchanges

import (
	"context"
	"errors"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// ---- shared plumbing --------------------------------------------------------

func tcUID(cxt *core.Cxt) string {
	if cxt.Sess == nil {
		return ""
	}
	return cxt.Sess.UserIDHex()
}

func tcPageData(cxt *core.Cxt, reqPath string) views.PageData {
	d := views.PageData{Nonce: views.NewNonce(), Path: reqPath}
	origin := cxt.SiteURL
	if origin == "" {
		origin = "http://" + cxt.Req.Host
	}
	d.Origin = origin
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
		d.UserEmail, d.UserID = core.PageUserSlots(cxt.Sess)
	}
	return d
}

func tcRelPath(cxt *core.Cxt) string {
	return strings.TrimPrefix(cxt.Req.URL.Path, "/")
}

// tcErr500 — Node: next(err) with an error carrying no status → the
// rendered general/500 view (the 500 HTML page). All downstream failures
// (chat/DU/docstore 4xx-5xx, unexpected errors) take this path.
func tcErr500(cxt *core.Cxt, res *core.Res) {
	views.Error500Page(res.W, tcPageData(cxt, tcRelPath(cxt)))
}

// tcAuthzProject — the /project/:project_id gate (shared P4 chain), with the
// read/write membership split:
//
//	read  = owner ∪ collab refs ∪ readonly ∪ reviewer ∪ tokenAccess refs
//	write = owner ∪ editor refs ∪ tokenAccessReadAndWrite refs
//
// pinned: bad oid → 404 JSON; ghost → 404 OlliTeX view; other → 403
// (accept-json → {"message":"restricted"}; else Restricted403 view).
func tcAuthzProject(a *core.App, cxt *core.Cxt, res *core.Res, write bool) (string, bool) {
	seg := cxt.Params["1"]
	if !tcHex24.MatchString(seg) {
		res.JSON(404, []byte(tcBadOID404))
		return "", false
	}
	oid, oerr := primitive.ObjectIDFromHex(strings.ToLower(seg))
	if oerr != nil {
		res.JSON(404, []byte(tcBadOID404))
		return "", false
	}
	uid := tcUID(cxt)
	doc, lerr := tcLoadProject(a, cxt.Req.Context(), oid)
	if lerr != nil {
		res.JSON(500, []byte(`{"message":"internal error"}`))
		return "", false
	}
	if doc == nil {
		views.NotFoundPage(res.W, tcPageData(cxt, tcRelPath(cxt)))
		return "", false
	}
	if uid == "" || !tcCanAccess(uid, *doc, write) {
		if core.AcceptsJSON(cxt.Req) {
			res.JSON(403, []byte(`{"message":"restricted"}`))
		} else {
			views.Restricted403(res.W, tcPageData(cxt, tcRelPath(cxt)))
		}
		return "", false
	}
	return seg, true
}

func tcLoadProject(a *core.App, ctx context.Context, oid primitive.ObjectID) (*bson.D, error) {
	if a.Mongo == nil {
		return nil, nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	var d bson.D
	if err := db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func tcDocVal(doc bson.D, key string) (interface{}, bool) {
	for _, e := range doc {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

func tcDocStr(doc bson.D, key string) (string, bool) {
	v, ok := tcDocVal(doc, key)
	if !ok {
		return "", false
	}
	if s, ok := v.(string); ok {
		return s, true
	}
	if o, ok := v.(primitive.ObjectID); ok {
		return o.Hex(), true
	}
	return "", false
}

func tcOidHex(v interface{}) (string, bool) {
	switch t := v.(type) {
	case string:
		return strings.ToLower(t), true
	case primitive.ObjectID:
		return t.Hex(), true
	}
	return "", false
}

func tcInList(doc bson.D, key, uidHex string) bool {
	arr, ok := tcDocVal(doc, key)
	if !ok {
		return false
	}
	items, ok := arr.([]interface{})
	if !ok {
		return false
	}
	for _, it := range items {
		if s, ok := tcOidHex(it); ok && strings.EqualFold(s, uidHex) {
			return true
		}
	}
	return false
}

// tcCanAccess mirrors Node's ensureUserCan{Read,Write}ProjectContent
// membership test (owner_ref/owner + the access-ref arrays).
func tcCanAccess(uidHex string, doc bson.D, write bool) bool {
	own, _ := tcDocStr(doc, "owner_ref")
	if own == "" {
		if o, ok := tcDocVal(doc, "owner"); ok {
			if dm, ok := o.(bson.D); ok {
				for _, k := range []string{"userId", "user_id", "_id"} {
					for _, e := range dm {
						if e.Key == k {
							if s, ok := tcOidHex(e.Value); ok && s != "" {
								own = s
							}
						}
					}
				}
			}
		}
	}
	if own != "" && strings.EqualFold(own, uidHex) {
		return true
	}
	if write {
		for _, key := range []string{"collab_refs", "collaborator_refs", "tokenAccessReadAndWrite_refs"} {
			if tcInList(doc, key, uidHex) {
				return true
			}
		}
		return false
	}
	for _, key := range []string{
		"collab_refs", "collaborator_refs", "readonly_refs",
		"reviewer_refs", "tokenAccessReadOnly_refs", "tokenAccessReadAndWrite_refs",
	} {
		if tcInList(doc, key, uidHex) {
			return true
		}
	}
	return false
}

// tcTCState — validateTrackChangesState (Node oracle): the exact 400
// messages + the stored shape (true | false | {…}).
func tcTCState(body map[string]interface{}) (interface{}, *string) {
	on, hasOn := body["on"]
	onFor, hasOnFor := body["on_for"]
	onGuests, hasOnGuests := body["on_for_guests"]

	fail := func(m string) (interface{}, *string) {
		msg := m
		return nil, &msg
	}
	if hasOn && on != true && on != false {
		return fail(`"on" must be a boolean`)
	}
	if hasOnFor {
		m, ok := onFor.(map[string]interface{})
		if !ok {
			return fail(`"on_for" must be an object`)
		}
		for k, v := range m {
			if k != "__guests__" && !tcHex24.MatchString(k) {
				return fail(`"on_for" keys must be user ids`)
			}
			if v != true && v != false {
				return fail(`"on_for" values must be booleans`)
			}
		}
	}
	if hasOnGuests && onGuests != true && onGuests != false {
		return fail(`"on_for_guests" must be a boolean`)
	}
	if !hasOn && !hasOnFor && !hasOnGuests {
		return fail("No track-changes fields provided")
	}

	state := map[string]interface{}{}
	if hasOnFor {
		if m, ok := onFor.(map[string]interface{}); ok {
			for k, v := range m {
				state[k] = v
			}
		}
	}
	if hasOnGuests {
		state["__guests__"] = onGuests
	}
	if on == true {
		return true, nil
	}
	if on == false && len(state) == 0 {
		return false, nil
	}
	return state, nil
}

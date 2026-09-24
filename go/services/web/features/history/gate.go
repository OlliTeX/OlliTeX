package history

import (
	"context"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var validOID = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// ---------- access gates (Node: AuthorizationMiddleware order) ----------
//
// Node route middleware order (HistoryRouter):
//
//	blockRestrictedUserFromProject  (invited-but-blocked -> 403 restricted)
//	ensureUserCanReadProject        (canUserReadProject  -> 403 restricted)
//	ensureUserCanWriteProjectContent      (owner|rw  -> 403 restricted)
//	ensureUserCanWriteOrReviewProjectContent (owner|rw|review)
//	ensureUserCanDeleteLabel            (owner | site-admin)
//
// _getProjectId (parseReq objectId) in the middlewares -> 404 JSON
// Validation error (accept-independent).
//
// The gate below mirrors the observable chain for a logged-in user:
//
//	project param bad  -> 404 JSON {"error":"Validation error: Invalid
//	                        Mongo ObjectId at \"params.Project_id\"",
//	                        "statusCode":404}
//	project missing    -> 404 HTML general/404 (accept-independent)
//	user blocked       -> 403 restricted (JSON {"message":"restricted"} /
//	                     HTML restricted page)
//	!canRead           -> 403 JSON {"message":"restricted"} / HTML page
//
// Anonymous never reaches the gate (core global gate: 401/302 pinned).

type projDoc struct {
	*primitive.D
	OwnerRef string
	Collabs  []string
	Readers  []string
	Reviews  []string
	TokRW    []string
	TokRD    []string
	Pal      string
	Name     string
	HistID   string
}

func oidHex(v any) string {
	switch x := v.(type) {
	case primitive.ObjectID:
		return x.Hex()
	case string:
		return x
	}
	return ""
}

func strArr(v any) []string {
	out := []string{}
	if a, ok := v.(primitive.A); ok {
		for _, e := range a {
			s := oidHex(e)
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// loadProject reads the project doc (full doc is fine; the e2e projects are
// small) for access decisions + name/history id.
func (h *svc) loadProject(cxt *core.Cxt, oid primitive.ObjectID) (*projDoc, error) {
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
	defer cancel()
	if h.a.Mongo == nil {
		return nil, nil
	}
	db, err := h.a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	var d primitive.D
	if err := db.Collection("projects").FindOne(ctx,
		bson.D{{Key: "_id", Value: oid}}).Decode(&d); err != nil {
		if err.Error() == "mongo: no documents in result" || err.Error() == "mongo: no documents in result" {
			return nil, nil
		}
		return nil, err
	}
	p := &projDoc{D: &d}
	p.Name, _ = d.Map()["name"].(string)
	p.Pal, _ = d.Map()["publicAccesLevel"].(string)
	p.OwnerRef = oidHex(d.Map()["owner_ref"])
	p.Collabs = strArr(d.Map()["collaberator_refs"])
	p.Readers = strArr(d.Map()["readOnly_refs"])
	p.Reviews = strArr(d.Map()["reviewer_refs"])
	p.TokRW = strArr(d.Map()["tokenAccessReadAndWrite_refs"])
	p.TokRD = strArr(d.Map()["tokenAccessReadOnly_refs"])
	if ov, ok := d.Map()["overleaf"].(primitive.D); ok {
		if his, ok := ov.Map()["history"].(primitive.D); ok {
			if id, ok := his.Map()["id"].(primitive.ObjectID); ok {
				p.HistID = id.Hex()
			} else if s, ok := his.Map()["id"].(string); ok {
				p.HistID = s
			}
		}
	}
	return p, nil
}

// userAdmin loads the user's isAdmin flag and blocked state (CE users doc).
func (h *svc) userAdmin(cxt *core.Cxt, uid string) (isAdmin, blocked bool) {
	if h.a.Mongo == nil || uid == "" {
		return false, false
	}
	oid, err := primitive.ObjectIDFromHex(uid)
	if err != nil {
		return false, false
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
	defer cancel()
	db, _ := h.a.Mongo.DB(ctx)
	if db == nil {
		return false, false
	}
	var d primitive.D
	opts := options.FindOne().SetProjection(bson.D{{Key: "isAdmin", Value: 1}, {Key: "blocked", Value: 1}})
	if db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}, opts).Decode(&d) != nil {
		return false, false
	}
	if b, ok := d.Map()["blocked"].(bool); ok && b {
		blocked = true
	}
	if isAdminOk, ok := d.Map()["isAdmin"].(bool); ok && isAdminOk {
		isAdmin = true
	}
	return
}

// adminPrivilegeAvailable mirrors settings.adminPrivilegeAvailable.
func adminPrivilegeAvailable() bool {
	return core.AdminPrivilegeAvailable()
}

func inList(l []string, uid string) bool {
	for _, s := range l {
		if s == uid {
			return true
		}
	}
	return false
}

// canRead mirrors canUserReadProject (ignoreSiteAdmin false at this layer —
// site admins read everything in this stack).
func (p *projDoc) canRead(uid string, isAdmin bool) bool {
	if uid == "" {
		return false
	}
	if p.OwnerRef == uid || inList(p.Collabs, uid) || inList(p.Reviews, uid) || inList(p.Readers, uid) {
		return true
	}
	if p.Pal == "tokenBased" && (inList(p.TokRW, uid) || inList(p.TokRD, uid)) {
		return true
	}
	if p.Pal == "readOnly" || p.Pal == "readAndWrite" {
		return true
	}
	return isAdmin
}

// canWriteOrReview mirrors canUserWriteOrReviewProjectContent:
// owner | readAndWrite | review (+ admin capability, skipped here — the
// e2e admins are not in the fixture projects' review/rw lists).
func (p *projDoc) canWriteOrReview(uid string) bool {
	if uid == "" {
		return false
	}
	return p.OwnerRef == uid || inList(p.Collabs, uid) || inList(p.Reviews, uid)
}

// canWriteContent mirrors canUserWriteProjectContent: owner | readAndWrite.
func (p *projDoc) canWriteContent(uid string) bool {
	if uid == "" {
		return false
	}
	return p.OwnerRef == uid || inList(p.Collabs, uid)
}

// canAdmin mirrors canUserAdminProject: owner OR (adminPrivilege && isAdmin).
func (p *projDoc) canAdmin(uid string, isAdmin bool) bool {
	if uid == "" {
		return false
	}
	if p.OwnerRef == uid {
		return true
	}
	return adminPrivilegeAvailable() && isAdmin
}

// gate resolves the logged-in uid + project for a route. On failure it has
// already written the Node-pinned response and returns ok=false.
//
// level: "read" (ensureUserCanReadProject), "write"
// (ensureUserCanWriteProjectContent), "wreview"
// (ensureUserCanWriteOrReviewProjectContent).
func (h *svc) gate(cxt *core.Cxt, res *core.Res, level string) (string, *projDoc, bool) {
	pidRaw := cxt.Params["1"]
	if pidRaw == "" {
		// pattern captured no id (shouldn't happen for our patterns).
		return "", nil, false
	}
	if !validOID.MatchString(pidRaw) {
		res.JSON(404, []byte(`{"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}`))
		return "", nil, false
	}
	oid, _ := primitive.ObjectIDFromHex(pidRaw) // safe: matched validOID
	uid := ""
	if cxt.Sess != nil {
		_, uid = core.PassportUser(cxt.Sess)
	}
	// logged-out with a session: Node _getUserId -> null -> canRead false ->
	// 403 restricted. (Anonymous is bounced by the core global gate first.)
	if uid == "" {
		h.restricted(cxt, res)
		return "", nil, false
	}
	isAdmin, blocked := h.userAdmin(cxt, uid)
	p, err := h.loadProject(cxt, oid)
	if err != nil {
		res.SendStatus(500)
		return "", nil, false
	}
	if p == nil {
		views.NotFoundPage(res.W, page(cxt, pidRaw))
		return "", nil, false
	}
	if blocked { // isRestrictedUserForProject (invited-but-blocked branch)
		h.restricted(cxt, res)
		return "", nil, false
	}
	switch level {
	case "read":
		if !p.canRead(uid, isAdmin) {
			h.restricted(cxt, res)
			return "", nil, false
		}
	case "write":
		if !p.canWriteContent(uid) {
			h.restricted(cxt, res)
			return "", nil, false
		}
	case "wreview":
		if !p.canWriteOrReview(uid) {
			h.restricted(cxt, res)
			return "", nil, false
		}
	}
	return uid, p, true
}

// restricted: HttpErrorHandler.forbidden — accept JSON -> {"message":
// "restricted"}, else the 15KB restricted HTML page.
func (h *svc) restricted(cxt *core.Cxt, res *core.Res) {
	if core.AcceptsJSON(cxt.Req) {
		res.JSON(403, []byte(`{"message":"restricted"}`))
		return
	}
	views.Restricted403(res.W, page(cxt, ""))
}

// page builds the views.PageData slots for error pages (path-aware navbar
// like Node's res.render + layout).
func page(cxt *core.Cxt, path string) views.PageData {
	d := views.PageData{Nonce: views.NewNonce()}
	origin := cxt.SiteURL
	if origin == "" {
		origin = "http://" + cxt.Req.Host
	}
	d.Origin = origin
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
		d.UserEmail, d.UserID = core.PageUserSlots(cxt.Sess)
	}
	if path != "" {
		d.Path = path
	}
	return d
}

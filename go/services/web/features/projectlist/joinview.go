// joinview.go — U-API, POST /project/:Project_id/join (Node EditorRouter,
// privateApiRouter only → APIOnly; Node EditorHttpController.joinProject,
// "called by the real-time API to load up the current project state").
//
// Wire pinned Node api :3000 (2026-09-24), full-header captured:
//
//	unauth / wrong basic            → 401 "Unauthorized" (APIBasicGate401 wire)
//	params.Project_id not hex24     → 404 JSON VA "…params.Project_id"
//	body invalid                    → 400 JSON VA (userId union / unknown key)
//	params+body both bad            → 404 (param precedence), "; "-joined (param first)
//	ghost project                   → 404 text/plain "Not Found"
//	access NONE (non-member, anon)  → 403 text/plain "Forbidden"
//	access granted                  → 200 JSON {project, privilegeLevel,
//	                                             isRestrictedUser, isTokenMember,
//	                                             isInvitedMember}
//
// The 200 `project` model = Node ProjectEditorHandler.buildProjectModelView —
// fixed key insertion order (undefined values dropped by res.json):
//	_id, name, [rootDoc_id], [mainBibliographyDoc_id], rootFolder:[tree],
//	publicAccesLevel, dropboxEnabled, [compiler], [description],
//	[spellCheckLanguage], [referenceFormat], grammarPicky, [png2pdf],
//	deletedByExternalDataSource, [imageName],
//	owner|{_id}, members, invites, [editAccessRequests | myAccessRequest],
//	features, [trackChangesState]
//
//  features = _.defaults(ownerMember.user.features, DEFAULTS) (owner's keys in
//  stored order + missing DEFAULTS keys appended in DEFAULTS order), then
//  referencesSearch ||= references and mendeley ||= references (in place).
//  trackChangesState = project.track_changes||false, only if features.trackChanges.
//  folder = {_id,name?,folders,fileRefs,docs} (undefined dropped; fileRefs
//  filtered to non-null); file = {_id,name,linkedFileData?,created?,hash?};
//  doc = {_id,name?}; user = {_id,first_name?,last_name?,email?,privileges,
//  signUpDate?,pendingEditor?,pendingReviewer?}.
//
// Privilege (non-site-admin — the only path active in this CE stack; no site
// admin, fixture owner is a plain user): owner_ref→owner,
// collablator_refs→readAndWrite, reviewer_refs→review, readOnly_refs→readOnly,
// (tokenBased) token refs, then legacy public readOnly/readAndWrite; anon →
// public/token (private + no token → NONE).
// isTokenMember = member from a TOKEN source (false for a real ref/owner);
// isInvitedMember = member from a non-TOKEN source (true for owner/refs);
// isRestrictedUser = (NONE) || (readOnly && (token||!user) && !invited).
//
// accessRequestData: OWNER caller → editAccessRequests (loadAccessRequestsView,
// [] when no requests — the P4.13 gate state); non-owner w/ a user →
// myAccessRequest (the caller's own request, null when none); anon → none.

package projectlist

import (
	"context"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var joinPat = regexp.MustCompile(`^/project/([^/]+)/join$`)

func joinHandler(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		req := c.Req
		mm := joinPat.FindStringSubmatch(req.URL.Path)
		if mm == nil {
			views.NotFoundPage(r.W, pageBase(c, strings.TrimPrefix(req.URL.Path, "/")))
			return
		}
		pidHex := mm[1]
		if c.A.Cfg.Profile != "api" {
			// Defensive: APIOnly → the web profile skips the route; Node does
			// not wire this path on webRouter either.
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(404, delParamVA("Project_id"))
			return
		}
		if !a.APIBasicGate401(c, r, req) {
			return // unauth / wrong basic → 401 challenge wire
		}

		// --- validation (params.Project_id : body), param-first precedence ---
		body, tag, isObj := apReadBody(req)
		var segs []string
		paramBad := false
		if !delHex24(pidHex) {
			paramBad = true
			segs = append(segs, `Invalid Mongo ObjectId at \"params.Project_id\"`)
		}
		uidHex, anon := "", false
		if isObj {
			if unk, ok := apUnknown(body, "body", "userId", "anonymousAccessToken"); !ok {
				segs = append(segs, unk)
			}
			v, has := body.M["userId"]
			if !has {
				segs = append(segs, `Invalid input: expected string, received undefined at \"body.userId\" or Invalid input: expected \"anonymous-user\" at \"body.userId\"`)
			} else if s, ok := v.(string); ok {
				switch {
				case s == "anonymous-user":
					anon = true
				case delHex24(s):
					uidHex = strings.ToLower(s)
				default:
					segs = append(segs, `Invalid Mongo ObjectId at \"body.userId\"`)
				}
			} else {
				segs = append(segs, `Invalid input: expected string, received `+apType(v)+` at \"body.userId\" or Invalid input: expected \"anonymous-user\" at \"body.userId\"`)
			}
			if t, has := body.M["anonymousAccessToken"]; has {
				if _, ok := t.(string); !ok {
					segs = append(segs, `Invalid input: expected string, received `+apType(t)+` at \"body.anonymousAccessToken\"`)
				}
			}
		} else {
			segs = append(segs, `Invalid input: expected string, received `+tag+` at \"body\" or Invalid input: expected object at \"body\"`)
		}
		if len(segs) > 0 {
			status := 400
			if paramBad {
				status = 404
			}
			r.W.Header().Set("X-Powered-By", "Express")
			r.JSON(status, joinVA(status, segs...))
			return
		}

		// --- load project (ghost → 404 plain "Not Found") ---
		oid, _ := primitive.ObjectIDFromHex(pidHex)
		ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			details404Plain(r)
			return
		}
		var pd primitive.D
		if err := db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&pd); err != nil {
			details404Plain(r)
			return
		}

		// --- privilege + member flags ---
		pal := asStr(dget(pd, "publicAccesLevel"))
		var level string
		var invited, tokenMemb bool
		if anon {
			level = joinPrivilegeAnon(pal)
		} else if uidHex != "" {
			level = joinPrivilegeForUser(uidHex, pd, pal)
			invited = joinInvitedMember(uidHex, pd, pal)
			tokenMemb = joinTokenMember(uidHex, pd, pal)
		}
		if level == "" { // Node: privilegeLevel null/NONE → project=null → 403
			r.W.Header().Set("X-Powered-By", "Express")
			apiText(r, 403, "Forbidden")
			return
		}
		isRestricted := joinRestricted(uidHex != "", level, tokenMemb, invited)
		isOwner := !anon && ownerRefHex(pd) == uidHex

		// --- owner / members / invites ---
		var ownerMember *primitive.D
		var members, invites primitive.A
		if !isRestricted {
			ownerMember = joinLoadUser(a, ctx, db, dget(pd, "owner_ref"))
			members = joinInvitedMembers(a, ctx, db, pd, ownerRefHex(pd))
			invites = primitive.A{} // P4.13: no invites
		}

		projectView := joinProjectModelView(pd, ownerMember, members, invites, isRestricted, isAnon(isOwner, anon), uidHex, isOwner, a, ctx, db)

		var outer primitive.D
		outer = append(outer,
			primitive.E{Key: "project", Value: projectView},
			primitive.E{Key: "privilegeLevel", Value: level},
			primitive.E{Key: "isRestrictedUser", Value: isRestricted},
			primitive.E{Key: "isTokenMember", Value: tokenMemb},
			primitive.E{Key: "isInvitedMember", Value: !anon && invited},
		)
		r.W.Header().Set("X-Powered-By", "Express") // res.json sets X-Powered-By
		r.JSON(200, core.OrderedD(outer))
	}
}

// isAnon — whether accessRequestData should be omitted (anonymous caller).
func isAnon(isOwner, anonReq bool) bool {
	return anonReq // for an owner caller this is always false
}

// joinVA — {"error":"Validation error: <seg; seg>","statusCode":N}.
func joinVA(status int, segs ...string) []byte {
	return []byte(`{"error":"Validation error: ` + strings.Join(segs, "; ") + `","statusCode":` + itoa(int64(status)) + `}`)
}

// --- privilege / member flags (non-site-admin path) ---

func joinPrivilegeForUser(uidHex string, d primitive.D, pal string) string {
	if ownerRefHex(d) == uidHex {
		return "owner"
	}
	if inOIDList(dget(d, "collablator_refs"), uidHex) {
		return "readAndWrite"
	}
	if inOIDList(dget(d, "reviewer_refs"), uidHex) {
		return "review"
	}
	if inOIDList(dget(d, "readOnly_refs"), uidHex) {
		return "readOnly"
	}
	if pal == "tokenBased" {
		if inOIDList(dget(d, "tokenAccessReadAndWrite_refs"), uidHex) {
			return "readAndWrite"
		}
		if inOIDList(dget(d, "tokenAccessReadOnly_refs"), uidHex) {
			return "readOnly"
		}
	}
	if pal == "readOnly" {
		return "readOnly"
	}
	if pal == "readAndWrite" {
		return "readAndWrite"
	}
	return ""
}

func joinPrivilegeAnon(pal string) string {
	if pal == "readOnly" {
		return "readOnly"
	}
	if pal == "readAndWrite" {
		return "readAndWrite"
	}
	return ""
}

func joinInvitedMember(uidHex string, d primitive.D, pal string) bool {
	if uidHex == "" {
		return false
	}
	if ownerRefHex(d) == uidHex {
		return true
	}
	return inOIDList(dget(d, "collablator_refs"), uidHex) ||
		inOIDList(dget(d, "reviewer_refs"), uidHex) ||
		inOIDList(dget(d, "readOnly_refs"), uidHex)
}

func joinTokenMember(uidHex string, d primitive.D, pal string) bool {
	if uidHex == "" || pal != "tokenBased" {
		return false
	}
	return inOIDList(dget(d, "tokenAccessReadAndWrite_refs"), uidHex) ||
		inOIDList(dget(d, "tokenAccessReadOnly_refs"), uidHex)
}

func joinRestricted(hasUser bool, level string, tokenMemb, invited bool) bool {
	if level == "" {
		return true
	}
	return level == "readOnly" && (tokenMemb || !hasUser) && !invited
}

func ownerRefHex(d primitive.D) string { return strings.ToLower(oidHex(dget(d, "owner_ref"))) }

// --- user / members loading ---

func joinLoadUser(a *core.App, ctx context.Context, db *mongo.Database, o any) *primitive.D {
	if a == nil || db == nil {
		return nil
	}
	hex := oidHex(o)
	if hex == "" {
		return nil
	}
	oid, err := primitive.ObjectIDFromHex(hex)
	if err != nil {
		return nil
	}
	var d primitive.D
	if err := db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d); err != nil {
		return nil
	}
	return &d
}

func joinInvitedMembers(a *core.App, ctx context.Context, db *mongo.Database, pd primitive.D, owner string) primitive.A {
	out := primitive.A{}
	seen := map[string]bool{}
	privFor := map[string]string{
		"collablator_refs": "readAndWrite",
		"reviewer_refs":    "review",
		"readOnly_refs":    "readOnly",
	}
	for _, key := range []string{"collablator_refs", "reviewer_refs", "readOnly_refs"} {
		arr, ok := dget(pd, key).(primitive.A)
		if !ok {
			continue
		}
		for _, e := range arr {
			he := oidHex(e)
			if he == "" || he == owner || seen[he] {
				continue
			}
			seen[he] = true
			u := joinLoadUser(a, ctx, db, e)
			if u == nil {
				continue
			}
			out = append(out, joinUserModel(u, privFor[key]))
		}
	}
	return out
}

// --- access-request views ---

// joinAccessRequestsView — Node loadAccessRequestsView: for each stored
// request, the flattened user detail + privilegeLevel + currentPrivilegeLevel.
// P4.13 has none → [].
func joinAccessRequestsView(ar primitive.A, pd primitive.D, a *core.App, ctx context.Context, db *mongo.Database) primitive.A {
	out := primitive.A{}
	for _, r := range ar {
		rr, ok := r.(primitive.D)
		if !ok {
			continue
		}
		uid := dget(rr, "userId")
		u := joinLoadUser(a, ctx, db, uid)
		if u == nil {
			continue
		}
		cur := ""
		if inOIDList(dget(pd, "collablator_refs"), oidHex(uid)) {
			cur = "readAndWrite"
		} else if inOIDList(dget(pd, "readOnly_refs"), oidHex(uid)) {
			cur = "readOnly"
		} else if inOIDList(dget(pd, "reviewer_refs"), oidHex(uid)) {
			cur = "review"
		}
		e := primitive.D{
			{Key: "_id", Value: dget(*u, "_id")},
			{Key: "email", Value: dget(*u, "email")},
			{Key: "first_name", Value: dget(*u, "first_name")},
			{Key: "last_name", Value: dget(*u, "last_name")},
			{Key: "privilegeLevel", Value: dget(rr, "privilegeLevel")},
			{Key: "currentPrivilegeLevel", Value: cur},
		}
		if ra := dget(rr, "requestedAt"); ra != nil {
			if iso, ok := signUpDateISO(ra); ok {
				e = append(e, primitive.E{Key: "requestedAt", Value: iso})
			}
		}
		out = append(out, e)
	}
	return out
}

// joinMyAccessRequest — Node getAccessRequestForUser: the caller's own request
// ({privilegeLevel, requestedAt}) or null.
func joinMyAccessRequest(ar primitive.A, uidHex string) any {
	for _, r := range ar {
		rr, ok := r.(primitive.D)
		if !ok {
			continue
		}
		if strings.ToLower(oidHex(dget(rr, "userId"))) != uidHex {
			continue
		}
		e := primitive.D{{Key: "privilegeLevel", Value: dget(rr, "privilegeLevel")}}
		if ra := dget(rr, "requestedAt"); ra != nil {
			if iso, ok := signUpDateISO(ra); ok {
				e = append(e, primitive.E{Key: "requestedAt", Value: iso})
			}
		}
		return e
	}
	return nil // null
}

// --- view builders (order-preserving, undefined dropped) ---

func joinUserModel(u *primitive.D, priv string) primitive.D {
	var m primitive.D
	m = append(m, primitive.E{Key: "_id", Value: dget(*u, "_id")})
	if s := dget(*u, "first_name"); s != nil {
		m = append(m, primitive.E{Key: "first_name", Value: s})
	}
	if s := dget(*u, "last_name"); s != nil {
		m = append(m, primitive.E{Key: "last_name", Value: s})
	}
	if s := dget(*u, "email"); s != nil {
		m = append(m, primitive.E{Key: "email", Value: s})
	}
	m = append(m, primitive.E{Key: "privileges", Value: priv})
	if su := dget(*u, "signUpDate"); su != nil {
		if iso, ok := signUpDateISO(su); ok {
			m = append(m, primitive.E{Key: "signUpDate", Value: iso})
		}
	}
	return m
}

func joinFeatures(ownerMember *primitive.D) primitive.D {
	var feats primitive.D
	if ownerMember != nil {
		if f := dget(*ownerMember, "features"); f != nil {
			if fd, ok := f.(primitive.D); ok {
				feats = fd
			}
		}
	}
	// _.defaults(object, source): object's keys (stored order) first, then
	// source keys appended (in source order) for the missing ones.
	defs := []primitive.E{
		{Key: "collaborators", Value: int32(-1)},
		{Key: "versioning", Value: false},
		{Key: "dropbox", Value: false},
		{Key: "compileTimeout", Value: int64(60)},
		{Key: "compileGroup", Value: "standard"},
		{Key: "templates", Value: false},
		{Key: "references", Value: false},
		{Key: "referencesSearch", Value: false},
		{Key: "mendeley", Value: false},
		{Key: "trackChanges", Value: false},
		{Key: "trackChangesVisible", Value: true}, // ProjectEditorHandler.trackChangesAvailable
		{Key: "symbolPalette", Value: false},
	}
	present := map[string]bool{}
	for _, e := range feats {
		present[e.Key] = true
	}
	for _, d := range defs {
		if !present[d.Key] {
			feats = append(feats, d)
		}
	}
	if b := featBool(feats, "references"); b {
		featSet(feats, "referencesSearch", true)
		featSet(feats, "mendeley", true)
	}
	return feats
}

func featBool(d primitive.D, key string) bool {
	for _, e := range d {
		if e.Key == key {
			if b, ok := e.Value.(bool); ok {
				return b
			}
			return false
		}
	}
	return false
}

func featSet(d primitive.D, key string, val any) {
	for i := range d {
		if d[i].Key == key {
			d[i].Value = val
			return
		}
	}
}

func joinProjectModelView(pd primitive.D, ownerMember *primitive.D, members, invites primitive.A, isRestricted, anonReq bool, uidHex string, isOwner bool, a *core.App, ctx context.Context, db *mongo.Database) primitive.D {
	var v primitive.D
	v = append(v, primitive.E{Key: "_id", Value: dget(pd, "_id")})
	if nm := dget(pd, "name"); nm != nil {
		v = append(v, primitive.E{Key: "name", Value: nm})
	}
	if x := dget(pd, "rootDoc_id"); x != nil {
		v = append(v, primitive.E{Key: "rootDoc_id", Value: x})
	}
	if x := dget(pd, "mainBibliographyDoc_id"); x != nil {
		v = append(v, primitive.E{Key: "mainBibliographyDoc_id", Value: x})
	}
	v = append(v, primitive.E{Key: "rootFolder", Value: joinRootFolder(pd)})
	if x := dget(pd, "publicAccesLevel"); x != nil {
		v = append(v, primitive.E{Key: "publicAccesLevel", Value: x})
	}
	// Node (ProjectEditorHandler.mjs): `dropboxEnabled: !!project.existsInDropbox`
	// — strict truthiness. NOT existence: `dget(...) != nil` would report true
	// whenever the field is present (Go project creation writes it explicitly,
	// e.g. deletedByExternalDataSource:false), flipping the frontend flag and
	// raising the blocking "renamed or deleted by external data source" modal
	// on every editor load.
	v = append(v, primitive.E{Key: "dropboxEnabled", Value: btruth(dget(pd, "existsInDropbox"))})
	if x := dget(pd, "compiler"); x != nil {
		v = append(v, primitive.E{Key: "compiler", Value: x})
	}
	if x := dget(pd, "description"); x != nil {
		v = append(v, primitive.E{Key: "description", Value: x})
	}
	if x := dget(pd, "spellCheckLanguage"); x != nil {
		v = append(v, primitive.E{Key: "spellCheckLanguage", Value: x})
	}
	if x := dget(pd, "referenceFormat"); x != nil {
		v = append(v, primitive.E{Key: "referenceFormat", Value: x})
	}
	v = append(v, primitive.E{Key: "grammarPicky", Value: notFalse(dget(pd, "grammarPicky"))})
	if x := dget(pd, "png2pdf"); x != nil {
		v = append(v, primitive.E{Key: "png2pdf", Value: x})
	}
	v = append(v, primitive.E{Key: "deletedByExternalDataSource", Value: btruth(dget(pd, "deletedByExternalDataSource"))})
	if x := dget(pd, "imageName"); x != nil {
		v = append(v, primitive.E{Key: "imageName", Value: x})
	}

	if isRestricted {
		v = append(v, primitive.E{Key: "owner", Value: primitive.D{{Key: "_id", Value: dget(pd, "owner_ref")}}})
		v = append(v, primitive.E{Key: "members", Value: primitive.A{}})
		v = append(v, primitive.E{Key: "invites", Value: primitive.A{}})
	} else {
		om := primitive.D{}
		if ownerMember == nil {
			om = primitive.D{{Key: "_id", Value: dget(pd, "owner_ref")}}
		} else {
			om = joinUserModel(ownerMember, "owner")
		}
		v = append(v, primitive.E{Key: "owner", Value: om})
		if members == nil {
			members = primitive.A{}
		}
		v = append(v, primitive.E{Key: "members", Value: members})
		if invites == nil {
			invites = primitive.A{}
		}
		v = append(v, primitive.E{Key: "invites", Value: invites})
	}

	// accessRequestData
	ar, _ := dget(pd, "editAccessRequests").(primitive.A)
	if isOwner {
		v = append(v, primitive.E{Key: "editAccessRequests", Value: joinAccessRequestsView(ar, pd, a, ctx, db)})
	} else if !anonReq && uidHex != "" {
		v = append(v, primitive.E{Key: "myAccessRequest", Value: joinMyAccessRequest(ar, uidHex)})
	}

	feats := joinFeatures(ownerMember)
	v = append(v, primitive.E{Key: "features", Value: feats})
	if featBool(feats, "trackChanges") {
		if tc := dget(pd, "track_changes"); tc != nil && tc != false {
			v = append(v, primitive.E{Key: "trackChangesState", Value: true})
		} else {
			v = append(v, primitive.E{Key: "trackChangesState", Value: false})
		}
	}
	return v
}

// notFalse — Node `x !== false`: true for missing/true/non-boolean; false only
// when the value is boolean false.
// btruth — Node truthiness of a stored flag: true ONLY when the value is
// boolean true (absent/false/other types → false). Mirrors JS `!!x` for the
// boolean flags the project view exposes (see notFalse for the `x !== false`
// variant).
func btruth(v any) bool {
	b, _ := v.(bool)
	return b
}

func notFalse(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return true
}

func joinRootFolder(pd primitive.D) primitive.A {
	rf, ok := dget(pd, "rootFolder").(primitive.A)
	if !ok || len(rf) == 0 {
		return primitive.A{}
	}
	if root, ok := rf[0].(primitive.D); ok {
		return primitive.A{joinFolderModel(root)}
	}
	return primitive.A{}
}

func joinFolderModel(folder primitive.D) primitive.D {
	var f primitive.D
	f = append(f, primitive.E{Key: "_id", Value: dget(folder, "_id")})
	if nm := dget(folder, "name"); nm != nil {
		f = append(f, primitive.E{Key: "name", Value: nm})
	}
	f = append(f, primitive.E{Key: "folders", Value: joinFolderChildren(folder)})
	f = append(f, primitive.E{Key: "fileRefs", Value: joinFileRefs(folder)})
	f = append(f, primitive.E{Key: "docs", Value: joinDocs(folder)})
	return f
}

func joinFolderChildren(folder primitive.D) primitive.A {
	var out primitive.A
	fr, ok := dget(folder, "folders").(primitive.A)
	if !ok {
		return out
	}
	for _, s := range fr {
		if sd, ok := s.(primitive.D); ok {
			out = append(out, joinFolderModel(sd))
		}
	}
	return out
}

func joinFileRefs(folder primitive.D) primitive.A {
	var out primitive.A
	fr, ok := dget(folder, "fileRefs").(primitive.A)
	if !ok {
		return out
	}
	for _, s := range fr {
		if s == nil {
			continue // _.filter(file => file != null)
		}
		if sd, ok := s.(primitive.D); ok {
			out = append(out, joinFileModel(sd))
		}
	}
	return out
}

func joinDocs(folder primitive.D) primitive.A {
	var out primitive.A
	dr, ok := dget(folder, "docs").(primitive.A)
	if !ok {
		return out
	}
	for _, s := range dr {
		if sd, ok := s.(primitive.D); ok {
			out = append(out, joinDocModel(sd))
		}
	}
	return out
}

func joinFileModel(file primitive.D) primitive.D {
	var f primitive.D
	f = append(f, primitive.E{Key: "_id", Value: dget(file, "_id")})
	if nm := dget(file, "name"); nm != nil {
		f = append(f, primitive.E{Key: "name", Value: nm})
	}
	if x := dget(file, "linkedFileData"); x != nil {
		f = append(f, primitive.E{Key: "linkedFileData", Value: x})
	}
	if x := dget(file, "created"); x != nil {
		f = append(f, primitive.E{Key: "created", Value: x})
	}
	if x := dget(file, "hash"); x != nil {
		f = append(f, primitive.E{Key: "hash", Value: x})
	}
	return f
}

func joinDocModel(doc primitive.D) primitive.D {
	var d primitive.D
	d = append(d, primitive.E{Key: "_id", Value: dget(doc, "_id")})
	if nm := dget(doc, "name"); nm != nil {
		d = append(d, primitive.E{Key: "name", Value: nm})
	}
	return d
}

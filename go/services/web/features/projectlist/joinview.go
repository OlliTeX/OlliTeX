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

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

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
		oid, _ := bson.ObjectIDFromHex(pidHex)
		ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			details404Plain(r)
			return
		}
		var pd bson.D
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
		var ownerMember *bson.D
		var members, invites bson.A
		if !isRestricted {
			ownerMember = joinLoadUser(a, ctx, db, dget(pd, "owner_ref"))
			members = joinInvitedMembers(a, ctx, db, pd, ownerRefHex(pd))
			invites = bson.A{} // P4.13: no invites
		}

		projectView := joinProjectModelView(pd, ownerMember, members, invites, isRestricted, isAnon(isOwner, anon), uidHex, isOwner, a, ctx, db)

		var outer bson.D
		outer = append(outer,
			bson.E{Key: "project", Value: projectView},
			bson.E{Key: "privilegeLevel", Value: level},
			bson.E{Key: "isRestrictedUser", Value: isRestricted},
			bson.E{Key: "isTokenMember", Value: tokenMemb},
			bson.E{Key: "isInvitedMember", Value: !anon && invited},
		)
		r.W.Header().Set("X-Powered-By", "Express") // res.json sets X-Powered-By
		r.JSON(200, core.OrderedD(outer))
	}
}

// isAnon — whether accessRequestData should be omitted (anonymous caller).
// joinRespondCore — shared response path for the join view (pinned private
// U-API join + the session join route): project load, privilege + member
// flags, model view, JSON. Same shape as Node's joinProject 200.
func joinRespondCore(a *core.App, c *core.Cxt, r *core.Res, pidHex, uidHex string, anon bool) {
	req := c.Req
	if !delHex24(pidHex) {
		r.W.Header().Set("X-Powered-By", "Express")
		apiText(r, 404, "Not Found")
		return
	}
	oid, _ := bson.ObjectIDFromHex(pidHex)
	ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		details404Plain(r)
		return
	}
	var pd bson.D
	if err := db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&pd); err != nil {
		details404Plain(r)
		return
	}
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
	if level == "" {
		r.W.Header().Set("X-Powered-By", "Express")
		apiText(r, 403, "Forbidden")
		return
	}
	isRestricted := joinRestricted(uidHex != "", level, tokenMemb, invited)
	isOwner := !anon && ownerRefHex(pd) == uidHex

	var ownerMember *bson.D
	var members, invites bson.A
	if !isRestricted {
		ownerMember = joinLoadUser(a, ctx, db, dget(pd, "owner_ref"))
		members = joinInvitedMembers(a, ctx, db, pd, ownerRefHex(pd))
		invites = bson.A{}
	}
	projectView := joinProjectModelView(pd, ownerMember, members, invites, isRestricted, isAnon(isOwner, anon), uidHex, isOwner, a, ctx, db)
	var outer bson.D
	outer = append(outer,
		bson.E{Key: "project", Value: projectView},
		bson.E{Key: "privilegeLevel", Value: level},
		bson.E{Key: "isRestrictedUser", Value: isRestricted},
		bson.E{Key: "isTokenMember", Value: tokenMemb},
		bson.E{Key: "isInvitedMember", Value: !anon && invited},
	)
	r.W.Header().Set("X-Powered-By", "Express")
	r.JSON(200, core.OrderedD(outer))
}

// joinSessionHandler — GET /project/:Project_id/join (WEB profile,
// session-authenticated). The modern browser-side join view: replaces the
// retired socket.io bus joinProjectResponse handshake (the bus used to call
// the private U-API join with basic auth and forward the view to the socket
// client). Same model + privilege logic, same JSON shape; session user
// instead of the body userId.
func joinSessionHandler(a *core.App) func(c *core.Cxt, r *core.Res) {
	return func(c *core.Cxt, r *core.Res) {
		mm := joinPat.FindStringSubmatch(c.Req.URL.Path)
		if mm == nil {
			views.NotFoundPage(r.W, pageBase(c, strings.TrimPrefix(c.Req.URL.Path, "/")))
			return
		}
		pidHex := mm[1]
		if c.Sess == nil || !c.Sess.IsLoggedIn() {
			joinRespondCore(a, c, r, pidHex, "", true) // anonymous (public/token) join view
			return
		}
		joinRespondCore(a, c, r, pidHex, strings.ToLower(c.Sess.UserIDHex()), false)
	}
}

func isAnon(isOwner, anonReq bool) bool {
	return anonReq // for an owner caller this is always false
}

// joinVA — {"error":"Validation error: <seg; seg>","statusCode":N}.
func joinVA(status int, segs ...string) []byte {
	return []byte(`{"error":"Validation error: ` + strings.Join(segs, "; ") + `","statusCode":` + itoa(int64(status)) + `}`)
}

// --- privilege / member flags (non-site-admin path) ---

func joinPrivilegeForUser(uidHex string, d bson.D, pal string) string {
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

func joinInvitedMember(uidHex string, d bson.D, pal string) bool {
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

func joinTokenMember(uidHex string, d bson.D, pal string) bool {
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

func ownerRefHex(d bson.D) string { return strings.ToLower(oidHex(dget(d, "owner_ref"))) }

// --- user / members loading ---

func joinLoadUser(a *core.App, ctx context.Context, db *mongo.Database, o any) *bson.D {
	if a == nil || db == nil {
		return nil
	}
	hex := oidHex(o)
	if hex == "" {
		return nil
	}
	oid, err := bson.ObjectIDFromHex(hex)
	if err != nil {
		return nil
	}
	var d bson.D
	if err := db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d); err != nil {
		return nil
	}
	return &d
}

func joinInvitedMembers(a *core.App, ctx context.Context, db *mongo.Database, pd bson.D, owner string) bson.A {
	out := bson.A{}
	seen := map[string]bool{}
	privFor := map[string]string{
		"collablator_refs": "readAndWrite",
		"reviewer_refs":    "review",
		"readOnly_refs":    "readOnly",
	}
	for _, key := range []string{"collablator_refs", "reviewer_refs", "readOnly_refs"} {
		arr, ok := dget(pd, key).(bson.A)
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
func joinAccessRequestsView(ar bson.A, pd bson.D, a *core.App, ctx context.Context, db *mongo.Database) bson.A {
	out := bson.A{}
	for _, r := range ar {
		rr, ok := r.(bson.D)
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
		e := bson.D{
			{Key: "_id", Value: dget(*u, "_id")},
			{Key: "email", Value: dget(*u, "email")},
			{Key: "first_name", Value: dget(*u, "first_name")},
			{Key: "last_name", Value: dget(*u, "last_name")},
			{Key: "privilegeLevel", Value: dget(rr, "privilegeLevel")},
			{Key: "currentPrivilegeLevel", Value: cur},
		}
		if ra := dget(rr, "requestedAt"); ra != nil {
			if iso, ok := signUpDateISO(ra); ok {
				e = append(e, bson.E{Key: "requestedAt", Value: iso})
			}
		}
		out = append(out, e)
	}
	return out
}

// joinMyAccessRequest — Node getAccessRequestForUser: the caller's own request
// ({privilegeLevel, requestedAt}) or null.
func joinMyAccessRequest(ar bson.A, uidHex string) any {
	for _, r := range ar {
		rr, ok := r.(bson.D)
		if !ok {
			continue
		}
		if strings.ToLower(oidHex(dget(rr, "userId"))) != uidHex {
			continue
		}
		e := bson.D{{Key: "privilegeLevel", Value: dget(rr, "privilegeLevel")}}
		if ra := dget(rr, "requestedAt"); ra != nil {
			if iso, ok := signUpDateISO(ra); ok {
				e = append(e, bson.E{Key: "requestedAt", Value: iso})
			}
		}
		return e
	}
	return nil // null
}

// --- view builders (order-preserving, undefined dropped) ---

func joinUserModel(u *bson.D, priv string) bson.D {
	var m bson.D
	m = append(m, bson.E{Key: "_id", Value: dget(*u, "_id")})
	if s := dget(*u, "first_name"); s != nil {
		m = append(m, bson.E{Key: "first_name", Value: s})
	}
	if s := dget(*u, "last_name"); s != nil {
		m = append(m, bson.E{Key: "last_name", Value: s})
	}
	if s := dget(*u, "email"); s != nil {
		m = append(m, bson.E{Key: "email", Value: s})
	}
	m = append(m, bson.E{Key: "privileges", Value: priv})
	if su := dget(*u, "signUpDate"); su != nil {
		if iso, ok := signUpDateISO(su); ok {
			m = append(m, bson.E{Key: "signUpDate", Value: iso})
		}
	}
	return m
}

func joinFeatures(ownerMember *bson.D) bson.D {
	var feats bson.D
	if ownerMember != nil {
		if f := dget(*ownerMember, "features"); f != nil {
			if fd, ok := f.(bson.D); ok {
				feats = fd
			}
		}
	}
	// _.defaults(object, source): object's keys (stored order) first, then
	// source keys appended (in source order) for the missing ones.
	defs := []bson.E{
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

func featBool(d bson.D, key string) bool {
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

func featSet(d bson.D, key string, val any) {
	for i := range d {
		if d[i].Key == key {
			d[i].Value = val
			return
		}
	}
}

func joinProjectModelView(pd bson.D, ownerMember *bson.D, members, invites bson.A, isRestricted, anonReq bool, uidHex string, isOwner bool, a *core.App, ctx context.Context, db *mongo.Database) bson.D {
	var v bson.D
	v = append(v, bson.E{Key: "_id", Value: dget(pd, "_id")})
	if nm := dget(pd, "name"); nm != nil {
		v = append(v, bson.E{Key: "name", Value: nm})
	}
	if x := dget(pd, "rootDoc_id"); x != nil {
		v = append(v, bson.E{Key: "rootDoc_id", Value: x})
	}
	if x := dget(pd, "mainBibliographyDoc_id"); x != nil {
		v = append(v, bson.E{Key: "mainBibliographyDoc_id", Value: x})
	}
	v = append(v, bson.E{Key: "rootFolder", Value: joinRootFolder(pd)})
	if x := dget(pd, "publicAccesLevel"); x != nil {
		v = append(v, bson.E{Key: "publicAccesLevel", Value: x})
	}
	// Node (ProjectEditorHandler.mjs): `dropboxEnabled: !!project.existsInDropbox`
	// — strict truthiness. NOT existence: `dget(...) != nil` would report true
	// whenever the field is present (Go project creation writes it explicitly,
	// e.g. deletedByExternalDataSource:false), flipping the frontend flag and
	// raising the blocking "renamed or deleted by external data source" modal
	// on every editor load.
	v = append(v, bson.E{Key: "dropboxEnabled", Value: btruth(dget(pd, "existsInDropbox"))})
	if x := dget(pd, "compiler"); x != nil {
		v = append(v, bson.E{Key: "compiler", Value: x})
	}
	if x := dget(pd, "description"); x != nil {
		v = append(v, bson.E{Key: "description", Value: x})
	}
	if x := dget(pd, "spellCheckLanguage"); x != nil {
		v = append(v, bson.E{Key: "spellCheckLanguage", Value: x})
	}
	if x := dget(pd, "referenceFormat"); x != nil {
		v = append(v, bson.E{Key: "referenceFormat", Value: x})
	}
	v = append(v, bson.E{Key: "grammarPicky", Value: notFalse(dget(pd, "grammarPicky"))})
	if x := dget(pd, "png2pdf"); x != nil {
		v = append(v, bson.E{Key: "png2pdf", Value: x})
	}
	v = append(v, bson.E{Key: "deletedByExternalDataSource", Value: btruth(dget(pd, "deletedByExternalDataSource"))})
	if x := dget(pd, "imageName"); x != nil {
		v = append(v, bson.E{Key: "imageName", Value: x})
	}

	if isRestricted {
		v = append(v, bson.E{Key: "owner", Value: bson.D{{Key: "_id", Value: dget(pd, "owner_ref")}}})
		v = append(v, bson.E{Key: "members", Value: bson.A{}})
		v = append(v, bson.E{Key: "invites", Value: bson.A{}})
	} else {
		om := bson.D{}
		if ownerMember == nil {
			om = bson.D{{Key: "_id", Value: dget(pd, "owner_ref")}}
		} else {
			om = joinUserModel(ownerMember, "owner")
		}
		v = append(v, bson.E{Key: "owner", Value: om})
		if members == nil {
			members = bson.A{}
		}
		v = append(v, bson.E{Key: "members", Value: members})
		if invites == nil {
			invites = bson.A{}
		}
		v = append(v, bson.E{Key: "invites", Value: invites})
	}

	// accessRequestData
	ar, _ := dget(pd, "editAccessRequests").(bson.A)
	if isOwner {
		v = append(v, bson.E{Key: "editAccessRequests", Value: joinAccessRequestsView(ar, pd, a, ctx, db)})
	} else if !anonReq && uidHex != "" {
		v = append(v, bson.E{Key: "myAccessRequest", Value: joinMyAccessRequest(ar, uidHex)})
	}

	feats := joinFeatures(ownerMember)
	v = append(v, bson.E{Key: "features", Value: feats})
	if featBool(feats, "trackChanges") {
		if tc := dget(pd, "track_changes"); tc != nil && tc != false {
			v = append(v, bson.E{Key: "trackChangesState", Value: true})
		} else {
			v = append(v, bson.E{Key: "trackChangesState", Value: false})
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

func joinRootFolder(pd bson.D) bson.A {
	rf, ok := dget(pd, "rootFolder").(bson.A)
	if !ok || len(rf) == 0 {
		return bson.A{}
	}
	if root, ok := rf[0].(bson.D); ok {
		return bson.A{joinFolderModel(root)}
	}
	return bson.A{}
}

func joinFolderModel(folder bson.D) bson.D {
	var f bson.D
	f = append(f, bson.E{Key: "_id", Value: dget(folder, "_id")})
	if nm := dget(folder, "name"); nm != nil {
		f = append(f, bson.E{Key: "name", Value: nm})
	}
	f = append(f, bson.E{Key: "folders", Value: joinFolderChildren(folder)})
	f = append(f, bson.E{Key: "fileRefs", Value: joinFileRefs(folder)})
	f = append(f, bson.E{Key: "docs", Value: joinDocs(folder)})
	return f
}

func joinFolderChildren(folder bson.D) bson.A {
	var out bson.A
	fr, ok := dget(folder, "folders").(bson.A)
	if !ok {
		return out
	}
	for _, s := range fr {
		if sd, ok := s.(bson.D); ok {
			out = append(out, joinFolderModel(sd))
		}
	}
	return out
}

func joinFileRefs(folder bson.D) bson.A {
	var out bson.A
	fr, ok := dget(folder, "fileRefs").(bson.A)
	if !ok {
		return out
	}
	for _, s := range fr {
		if s == nil {
			continue // _.filter(file => file != null)
		}
		if sd, ok := s.(bson.D); ok {
			out = append(out, joinFileModel(sd))
		}
	}
	return out
}

func joinDocs(folder bson.D) bson.A {
	var out bson.A
	dr, ok := dget(folder, "docs").(bson.A)
	if !ok {
		return out
	}
	for _, s := range dr {
		if sd, ok := s.(bson.D); ok {
			out = append(out, joinDocModel(sd))
		}
	}
	return out
}

func joinFileModel(file bson.D) bson.D {
	var f bson.D
	f = append(f, bson.E{Key: "_id", Value: dget(file, "_id")})
	if nm := dget(file, "name"); nm != nil {
		f = append(f, bson.E{Key: "name", Value: nm})
	}
	if x := dget(file, "linkedFileData"); x != nil {
		f = append(f, bson.E{Key: "linkedFileData", Value: x})
	}
	if x := dget(file, "created"); x != nil {
		f = append(f, bson.E{Key: "created", Value: x})
	}
	if x := dget(file, "hash"); x != nil {
		f = append(f, bson.E{Key: "hash", Value: x})
	}
	return f
}

func joinDocModel(doc bson.D) bson.D {
	var d bson.D
	d = append(d, bson.E{Key: "_id", Value: dget(doc, "_id")})
	if nm := dget(doc, "name"); nm != nil {
		d = append(d, bson.E{Key: "name", Value: nm})
	}
	return d
}

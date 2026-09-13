// Package tokenaccess ports the CE token access + link-sharing consent
// surface (P2):
//
//	GET  /<rw-token>                      (token page, legacy view)
//	GET  /read/<ro-token>                 (token page, legacy view)
//	POST /<rw-token>/grant                (rate-limited rw grant)
//	POST /read/<ro-token>/grant           (rate-limited ro grant)
//	GET  /project/<id>/sharing-updates   (consent page)
//	POST /project/<id>/sharing-updates/join  (move rw→editor, 204)
//	POST /project/<id>/sharing-updates/view  (move rw→readOnly, 204)
//
// Node contract pins (source + live, 2026-09-14):
//
//   - token shapes: rw = ^[0-9]+[a-z]{6,12}$, ro = ^[a-z]{12}$
//   - project lookup (fork): rw → tokens.readAndWritePrefix (+
//     constantTimeEqual on tokens.readAndWrite), ro → tokens.readOnly
//   - token gate: publicAccesLevel === 'tokenBased'
//   - anonymous rw grant (CE disabled): session.postLoginRedirect=
//     '/restricted', body {redirect:'/restricted', anonWriteAccessDenied:true}
//   - anonymous ro grant (enabled): session.anonTokenAccess[pid]=token,
//     body {redirect:'/project/<pid>', grantAnonymousAccess:'readOnly'}
//   - higher-privilege shortcut: {redirect:'/project/<pid>',
//     higherAccess:true}
//   - unconfirmed: {requireAccept:{projectName}}
//   - confirmed rw: refs check → audit 'join-via-token' {privileges:
//     'readAndWrite'} → $addToSet tokenAccessReadAndWrite_refs →
//     {redirect:'/project/<pid>'}
//   - confirmed ro: refs check → audit 'join-via-token' {privileges:
//     'readOnly'} → $addToSet tokenAccessReadOnly_refs →
//     {redirect:'/project/<pid>', tokenAccessGranted:'readOnly'}
//   - grant limiters: 'grant-token-access-read-write' / '…-read-only',
//     10 points / 60s, key rate-limit:<name>:<ip>
//   - consent page gate: requireLogin → ensureUserCanReadProject
//     (no access → 403 Restricted view) → rw-token-member (else {redir}
//     JSON / 302) → not invited member (else same redirect)
//   - join: audit 'accept-via-link-sharing' {privileges:'readAndWrite',
//     tokenMember:true, invitedMember:<bool>} + token-ref pull +
//     membership grant → 204
//   - view: audit 'readonly-via-sharing-updates' + ($pull rw refs,
//     $addToSet ro refs) → 204
package tokenaccess

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var (
	rwPagePat  = regexp.MustCompile(`^/([0-9]+[a-z]{6,12})$`)
	roPagePat  = regexp.MustCompile(`^/read/([a-z]{12})$`)
	rwGrantPat = regexp.MustCompile(`^/([0-9]+[a-z]{6,12})/grant$`)
	roGrantPat = regexp.MustCompile(`^/read/([a-z]{12})/grant$`)
	// group 1 = project id (page / join / view each carry the same group 1)
	sharePagePat = regexp.MustCompile(`^/project/([0-9a-f]{24})/sharing-updates$`)
	shareJoinPat = regexp.MustCompile(`^/project/([0-9a-f]{24})/sharing-updates/join$`)
	shareViewPat = regexp.MustCompile(`^/project/([0-9a-f]{24})/sharing-updates/view$`)
)

func Feature(a *core.App) core.Feature {
	rwLim := core.NewRateLimiter(a.Redis, "grant-token-access-read-write", 10, 60)
	roLim := core.NewRateLimiter(a.Redis, "grant-token-access-read-only", 10, 60)

	limit := func(l *core.RateLimiter, fn func(cxt *core.Cxt, res *core.Res)) func(*core.Cxt, *core.Res) {
		return func(cxt *core.Cxt, res *core.Res) {
			if l != nil && !l.Consume(core.ClientIP(cxt.Req)) {
				core.Send429(res, "Rate limit reached, please try again later")
				return
			}
			fn(cxt, res)
		}
	}

	page := func(ro bool) func(cxt *core.Cxt, res *core.Res) {
		return func(cxt *core.Cxt, res *core.Res) {
			token := cxt.Params["1"]
			postURL := "/" + token + "/grant"
			if ro {
				postURL = "/read/" + token + "/grant"
			}
			views.TokenAccessPage(res.W, pageBase(cxt, func(d *views.PageData) {
				d.PostURL = postURL
			}))
		}
	}

	return core.Feature{
		Name: "tokenaccess",
		Routes: []core.Route{
			{Method: "GET", Pattern: rwPagePat, Handler: page(false)},
			{Method: "GET", Pattern: roPagePat, Handler: page(true)},
			{Method: "POST", Pattern: rwGrantPat, Handler: limit(rwLim, grant(a, true))},
			{Method: "POST", Pattern: roGrantPat, Handler: limit(roLim, grant(a, false))},
			{Method: "GET", Pattern: sharePagePat, Handler: consent(a, "page")},
			{Method: "POST", Pattern: shareJoinPat, Handler: consent(a, "join")},
			{Method: "POST", Pattern: shareViewPat, Handler: consent(a, "view")},
		},
	}
}

// pageBase builds the shared PageData (session slots + origin).
func pageBase(cxt *core.Cxt, tweak func(*views.PageData)) views.PageData {
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
	if tweak != nil {
		tweak(&d)
	}
	return d
}

// ---------- project helpers (shared with P3/P4 authorization) ----------

type projectDoc struct {
	ID                primitive.ObjectID `bson:"_id"`
	Name              string             `bson:"name"`
	OwnerRef          string             `bson:"owner_ref"`
	Tokens            map[string]any     `bson:"tokens"`
	PublicAccessLevel string             `bson:"publicAccesLevel"`
	RWRefs            []string           `bson:"tokenAccessReadAndWrite_refs"`
	RORefs            []string           `bson:"tokenAccessReadOnly_refs"`
	CollabRefs        []string           `bson:"collaberator_refs"`
	ReadonlyNamedRefs []string           `bson:"readOnly_refs"`
}

func (p *projectDoc) tokenRW() string  { s, _ := p.Tokens["readAndWrite"].(string); return s }
func (p *projectDoc) tokenRO() string  { s, _ := p.Tokens["readOnly"].(string); return s }
func (p *projectDoc) rwPrefix() string { s, _ := p.Tokens["readAndWritePrefix"].(string); return s }
func (p *projectDoc) inRefs(refs []string, uid string) bool {
	for _, r := range refs {
		if r == uid {
			return true
		}
	}
	return false
}

func findProjectByToken(a *core.App, ctx context.Context, rw bool, token string) (*projectDoc, bool) {
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	var pd projectDoc
	if rw {
		prefix := digitPrefix(token)
		if err := db.Collection("projects").FindOne(ctx,
			bson.M{"tokens.readAndWritePrefix": prefix}).Decode(&pd); err != nil {
			return nil, false
		}
		// Node: crypto.timingSafeEqual(token, project.tokens.readAndWrite)
		// (length mismatch → Node throws → treated as no match here)
		if len(token) != len(pd.tokenRW()) ||
			subtle.ConstantTimeCompare([]byte(token), []byte(pd.tokenRW())) != 1 {
			return nil, false
		}
	} else {
		if err := db.Collection("projects").FindOne(ctx,
			bson.M{"tokens.readOnly": token}).Decode(&pd); err != nil {
			return nil, false
		}
	}
	return &pd, true
}

func findProject(a *core.App, ctx context.Context, idHex string) (*projectDoc, bool) {
	oaid, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return nil, false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	var pd projectDoc
	if err := db.Collection("projects").FindOne(ctx, bson.M{"_id": oaid}).Decode(&pd); err != nil {
		return nil, false
	}
	return &pd, true
}

func digitPrefix(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return s[:i]
		}
	}
	return s
}

// privilegeRank mirrors the CE privilege ordering for the "user already
// has higher or same privilege" shortcut.
func privilegeRank(uid string, p *projectDoc) string {
	if uid == "" {
		return "none"
	}
	if p.OwnerRef == uid {
		return "owner"
	}
	if p.inRefs(p.CollabRefs, uid) || p.inRefs(p.RWRefs, uid) {
		return "readAndWrite"
	}
	if p.inRefs(p.RORefs, uid) || p.inRefs(p.ReadonlyNamedRefs, uid) {
		return "readOnly"
	}
	return "none"
}

func rankNum(lvl string) int {
	switch lvl {
	case "owner":
		return 3
	case "readAndWrite":
		return 2
	case "readOnly":
		return 1
	}
	return 0
}

// ---------- checkAndGet (CE parity) ----------

const (
	actNone         = ""
	actAnonRoGrant  = "anonRoGrant"
	actAnonRwDenied = "anonRwDenied"
	actHigherAccess = "higherAccess"
)

type checkResult struct {
	project  *projectDoc
	notFound bool
	action   string
}

func checkAndGet(a *core.App, cxt *core.Cxt, token string, rw bool) checkResult {
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
	defer cancel()
	uid := ""
	if cxt.Sess != nil {
		_, uid = core.PassportUser(cxt.Sess)
	}
	anon := uid == ""

	p, ok := findProjectByToken(a, ctx, rw, token)
	if !ok || p == nil {
		// CE: Settings.overleaf absent → [null, null, {action:'404'}]
		return checkResult{notFound: true}
	}
	enabled := p.PublicAccessLevel == "tokenBased"
	if !enabled {
		// Node checks the token gate right after the lookup — before the
		// anon/authed branches and the higher-privilege shortcut.
		return checkResult{notFound: true}
	}
	if anon {
		if rw {
			// CE: ANONYMOUS_READ_AND_WRITE_ENABLED false → deny
			if cxt.Sess != nil {
				cxt.Sess.Set("postLoginRedirect", "/restricted")
			}
			return checkResult{project: p, action: actAnonRwDenied}
		}
		// anonymous readOnly grant (CE: allowed) → session token access
		if cxt.Sess != nil {
			m := map[string]string{}
			if raw, okk := cxt.Sess.GetRaw("anonTokenAccess"); okk {
				_ = json.Unmarshal(raw, &m)
			}
			m[p.ID.Hex()] = token
			cxt.Sess.Set("anonTokenAccess", m)
		}
		return checkResult{project: p, action: actAnonRoGrant}
	}
	// logged in
	got := privilegeRank(uid, p)
	target := "readOnly"
	if rw {
		target = "readAndWrite"
	}
	if rankNum(got) >= rankNum(target) {
		return checkResult{project: p, action: actHigherAccess}
	}
	return checkResult{project: p}
}

// ---------- grant ----------

func readGrantBody(cxt *core.Cxt) (confirmed bool, hashPrefix string) {
	var b struct {
		ConfirmedByUser bool   `json:"confirmedByUser"`
		TokenHashPrefix string `json:"tokenHashPrefix"`
	}
	raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
	_ = json.Unmarshal(raw, &b)
	return b.ConfirmedByUser, b.TokenHashPrefix
}

func respondJSON(res *core.Res, code int, body string) {
	res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
	res.W.WriteHeader(code)
	_, _ = io.WriteString(res.W, body)
}

func write404(res *core.Res) {
	respondJSON(res, 404, `{"message":"Not found."}`)
}

func grant(a *core.App, rw bool) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		token := cxt.Params["1"]
		confirmed, _ := readGrantBody(cxt)
		cr := checkAndGet(a, cxt, token, rw)
		switch cr.action {
		case actAnonRwDenied:
			respondJSON(res, 200, `{"redirect":"/restricted","anonWriteAccessDenied":true}`)
			return
		case actAnonRoGrant:
			respondJSON(res, 200, `{"redirect":"/project/`+cr.project.ID.Hex()+`","grantAnonymousAccess":"readOnly"}`)
			return
		case actHigherAccess:
			respondJSON(res, 200, `{"redirect":"/project/`+cr.project.ID.Hex()+`","higherAccess":true}`)
			return
		}
		if cr.notFound {
			write404(res)
			return
		}
		p := cr.project
		uid := ""
		if cxt.Sess != nil {
			_, uid = core.PassportUser(cxt.Sess)
		}
		if !confirmed {
			respondJSON(res, 200, `{"requireAccept":{"projectName":`+jsonStr(p.Name)+`}}`)
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.W.WriteHeader(500)
			return
		}
		priv := "readOnly"
		refField := "tokenAccessReadOnly_refs"
		if rw {
			priv = "readAndWrite"
			refField = "tokenAccessReadAndWrite_refs"
		}
		member := p.inRefs(p.RWRefs, uid) || p.inRefs(p.RORefs, uid)
		if !member && uid != "" {
			_, _ = db.Collection("projectAuditLogEntries").InsertOne(ctx, bson.M{
				"projectId": p.ID,
				"operation": "join-via-token",
				"userId":    uid,
				"ip":        core.ClientIP(cxt.Req),
				"info":      bson.M{"privileges": priv},
				"createdAt": time.Now().UTC(),
				"updatedAt": time.Now().UTC(),
			})
		}
		_, _ = db.Collection("projects").UpdateOne(ctx,
			bson.M{"_id": p.ID},
			bson.M{"$addToSet": bson.M{refField: uid}})
		if rw {
			respondJSON(res, 200, `{"redirect":"/project/`+p.ID.Hex()+`"}`)
		} else {
			respondJSON(res, 200, `{"redirect":"/project/`+p.ID.Hex()+`","tokenAccessGranted":"readOnly"}`)
		}
	}
}

// ---------- consent (sharing-updates) ----------

func consent(a *core.App, which string) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		pidHex := cxt.Params["1"]
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
		defer cancel()
		p, ok := findProject(a, ctx, pidHex)
		if !ok || p == nil {
			respondJSON(res, 404, `{"message":"Not found."}`)
			return
		}
		uid := ""
		if cxt.Sess != nil {
			_, uid = core.PassportUser(cxt.Sess)
		}
		// ensureUserCanReadProject: owner / named member (pinned: pure
		// token-ref members get 403 Restricted).
		if uid == "" || !(p.OwnerRef == uid || p.inRefs(p.CollabRefs, uid) || p.inRefs(p.ReadonlyNamedRefs, uid)) {
			// Node renders the Restricted view with a 403
			views.Restricted403(res.W, pageBase(cxt, nil))
			return
		}
		// ensureUserCanUseSharingUpdatesConsentPage
		if !p.inRefs(p.RWRefs, uid) || p.inRefs(p.CollabRefs, uid) {
			if core.AcceptsJSON(cxt.Req) {
				respondJSON(res, 200, `{"redir":"/project/`+pidHex+`"}`)
				return
			}
			res.Redirect(cxt.Req, 302, "/project/"+pidHex)
			return
		}
		if which == "page" {
			views.SharingUpdatesPage(res.W, pageBase(cxt, nil))
			return
		}
		// join / view moves
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.W.WriteHeader(500)
			return
		}
		oaid, oerr := primitive.ObjectIDFromHex(pidHex)
		if oerr != nil {
			res.W.WriteHeader(400)
			return
		}
		isInvited := p.OwnerRef == uid || p.inRefs(p.CollabRefs, uid) || p.inRefs(p.ReadonlyNamedRefs, uid)
		_, _ = db.Collection("projects").UpdateOne(ctx,
			bson.M{"_id": oaid},
			bson.M{"$pull": bson.M{"tokenAccessReadAndWrite_refs": uid}})
		if which == "join" {
			_, _ = db.Collection("projectAuditLogEntries").InsertOne(ctx, bson.M{
				"projectId": oaid,
				"operation": "accept-via-link-sharing",
				"userId":    uid,
				"ip":        core.ClientIP(cxt.Req),
				"info":      bson.M{"privileges": "readAndWrite", "tokenMember": true, "invitedMember": isInvited},
				"createdAt": time.Now().UTC(),
				"updatedAt": time.Now().UTC(),
			})
			_, _ = db.Collection("projects").UpdateOne(ctx, bson.M{"_id": oaid},
				bson.M{"$addToSet": bson.M{"collaberator_refs": uid}})
		} else {
			_, _ = db.Collection("projectAuditLogEntries").InsertOne(ctx, bson.M{
				"projectId": oaid,
				"operation": "readonly-via-sharing-updates",
				"userId":    uid,
				"ip":        core.ClientIP(cxt.Req),
				"createdAt": time.Now().UTC(),
				"updatedAt": time.Now().UTC(),
			})
			_, _ = db.Collection("projects").UpdateOne(ctx, bson.M{"_id": oaid},
				bson.M{"$addToSet": bson.M{"tokenAccessReadOnly_refs": uid}})
		}
		res.W.WriteHeader(204)
	}
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

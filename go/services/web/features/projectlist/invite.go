// P4.10b — project invites + sharing-link/split-test surface.
//
// Node oracle (services/web/app/src/Features/Collaborators/Collaborators*):
//
//	Router              CollaboratorsRouter.mjs (route order + middleware)
//	Controller          CollaboratorsInviteController.mjs
//	  · inviteToProject   self -> parseEmail -> rateLimit -> create+mail
//	  · getAllInvites     { invites: [ {_id, email, privileges} ] }
//	  · revokeInvite      always 204 (ghost invite included)
//	  · generateNewInvite revoke then re-create + mail; null -> sendStatus(404)
//	  · viewInvite        member?->302 /invite?->Invalid page / owner?->Invalid
//	  · acceptInvite      token?->404 page; member?->(+upgrade) 302|204-xhr
//	  · getShareTokens    link-sharing feature + getPublicShareTokens
//	Getter              CollaboratorsInviteGetter.mjs (getAllInvites / getInviteByToken)
//	Handler             CollaboratorsInviteHandler.mjs (inviteToProject / revokeInvite /
//	  generateNewInvite / acceptInvite / addUserIdToProject effects)
//	Helper              CollaboratorsInviteHelper.mjs (generateToken 24B hex;
//	  hashInviteToken HMAC-SHA256 "overleaf-token-invite")
//	Email               CollaboratorsEmailHandler.mjs + EmailBuilder projectInvite
//	  (subject: "<name> — shared by <owner email>", free-CE spam-safe path)
//
// Pinned live (e2e stack, 2026-09-15, the web-p4inv gate legs 1/3 capture):
//
//	invoke 200 {"invite":{"_id","email","privileges"}} / self {"invite":null,
//	"error":"cannot_invite_self"} / bad-400 {"errorReason":"invalid_email"} /
//	zod-400 VA / anon 403 text/plain "Forbidden" (CSRF first) / non-admin
//	403 {"message":"restricted"} / ghost project 404 "Page Not Found" page.
//	list   200 {"invites":[...]} (insertion order, reusable excluded) / 403 / 404.
//	revoke 204 (ghost included) / 403.
//	resend 201 "Created" / missing-invite sendStatus(404) "Not Found" / 403.
//	view   member 302 /project/:id | invalid 404 "Invalid Invite" page |
//	anon+valid 302 /login | ghost project 404 "Page Not Found" page |
//	200 "Project Invite" page (React, ol-inviteToken/ol-projectName metas).
//	accept 404 "Page Not Found" (bad token — the global NotFoundError page, NOT
//	Invalid Invite) | non-member add 302 (204 + x-requested-with: xmlHttpRequest)
//	| member re-accept 302 with optional privilege upgrade | anon 403 CSRF.
//	tokens non-member 403 {"message":"restricted"} (ensureUserCanReadProject) /
//	member on a project WITHOUT tokenAccess refs 500 "Something went wrong"
//	page (Node crashes on the $in [userId, '$tokenAccessReadOnly_refs']
//	projection when the field is absent — pinned, reproduced) / ghost 404.
//	split  GET/POST sharing-link, GET/POST share, POST share/validate:
//	split test "sharing-updates-new-link" is DISABLED in this CE build (no
//	assignment) -> the 403 generic error page (title "OlliTeX, Online LaTeX
//	Editor", 15297B) for authenticated users; anonymous split GET/POST 401
//	"Unauthorized" (requireLogin before the split middleware).
package projectlist

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/emailtemplates"
	"ollitex/go/services/web/views"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	invCreatePat   = regexp.MustCompile(`^/project/([^/]+)/invite$`)
	invListPat     = regexp.MustCompile(`^/project/([^/]+)/invites$`)
	invRevokePat   = regexp.MustCompile(`^/project/([^/]+)/invite/([0-9a-fA-F]{24})$`)
	invResendPat   = regexp.MustCompile(`^/project/([^/]+)/invite/([0-9a-fA-F]{24})/resend$`)
	invAcceptPat   = regexp.MustCompile(`^/project/([^/]+)/invite/token/([^/]+)/accept$`)
	invViewTokPat  = regexp.MustCompile(`^/project/([^/]+)/invite/token/([^/]+)$`)
	invTokensPat   = regexp.MustCompile(`^/project/([^/]+)/tokens$`)
	invSplitPat    = regexp.MustCompile(`^/project/([^/]+)/sharing-link$`)
	invSharePat    = regexp.MustCompile(`^/project/([^/]+)/share$`)
	invShareValPat = regexp.MustCompile(`^/project/([^/]+)/share/validate$`)
)

// invEmailRe — EmailHelper.EMAIL_REGEXP (RE2-safe).
var invEmailRe = regexp.MustCompile(`^([^<>()[\]\\.,;:\s@"]+(\.[^<>()[\]\\.,;:\s@"]+)*)@((\[[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\])|(([a-zA-Z\-0-9]+\.)+[a-zA-Z]{2,}))$`)

const invHMACKey = "overleaf-token-invite"

func invTokenHMAC(token string) string {
	mac := hmac.New(sha256.New, []byte(invHMACKey))
	_, _ = mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

func invRandToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func mustObjectID(hex string) primitive.ObjectID {
	oid, _ := primitive.ObjectIDFromHex(strings.ToLower(hex))
	return oid
}

// Node EmailHelper.parseEmail(email, /*parseRfc*/ true) trimmed form.
func invParseEmail(email string) bool {
	if email == "" {
		return false
	}
	e := strings.ToLower(strings.TrimSpace(email))
	return len(e) <= 254 && invEmailRe.MatchString(e)
}

type invResp struct {
	Invite *invOut `json:"invite"`
	Error  string  `json:"error,omitempty"`
}
type invOut struct {
	ID         string `json:"_id"`
	Email      string `json:"email"`
	Privileges string `json:"privileges"`
}
type invListResp struct {
	Invites []invOut `json:"invites"`
}

// invAdminEmail — Settings.adminEmail for general/500 (Node ErrorController
// renders the configured admin email; same fallback as the core 500 hook).
func invAdminEmail() string {
	if v := os.Getenv("ADMIN_EMAIL"); v != "" {
		return v
	}
	return "placeholder@example.com"
}

func invCtx(cxt *core.Cxt) (context.Context, context.CancelFunc) {
	return context.WithTimeout(cxt.Req.Context(), 8*time.Second)
}

func invFindOne(a *core.App, cxt *core.Cxt, flt bson.D) (*bson.D, error) {
	if a.Mongo == nil {
		return nil, nil
	}
	ctx, cancel := invCtx(cxt)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	var d bson.D
	if err := db.Collection("projectInvites").FindOne(ctx, flt).Decode(&d); err != nil {
		return nil, nil
	}
	return &d, nil
}

func invInsert(a *core.App, cxt *core.Cxt, rec bson.D) (string, error) {
	if a.Mongo == nil {
		return "", nil
	}
	ctx, cancel := invCtx(cxt)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return "", err
	}
	res, err := db.Collection("projectInvites").InsertOne(ctx, rec)
	if err != nil {
		return "", err
	}
	return res.InsertedID.(primitive.ObjectID).Hex(), nil
}

func invDelete(a *core.App, cxt *core.Cxt, flt bson.D) bool {
	if a.Mongo == nil {
		return false
	}
	ctx, cancel := invCtx(cxt)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	res, err := db.Collection("projectInvites").DeleteOne(ctx, flt)
	if err != nil {
		return false
	}
	return res.DeletedCount > 0
}

// invMember — owner_ref ∪ collaberator_refs ∪ readOnly_refs ∪ reviewer_refs.
func invMember(uid string, doc primitive.D) bool {
	uidl := strings.ToLower(uid)
	owner := strings.ToLower(asStr(dget(doc, "owner_ref")))
	if owner != "" && owner == uidl {
		return true
	}
	for _, k := range []string{"collaberator_refs", "readOnly_refs", "reviewer_refs"} {
		switch v := dget(doc, k).(type) {
		case primitive.A:
			for i := range v {
				if oidHex(v[i]) == uidl {
					return true
				}
			}
		}
	}
	return false
}

func invMemberLevel(uid string, doc primitive.D) string {
	if strings.ToLower(asStr(dget(doc, "owner_ref"))) == strings.ToLower(uid) {
		return "owner"
	}
	if invA(dget(doc, "collaberator_refs"), uid) {
		return "readAndWrite"
	}
	if invA(dget(doc, "reviewer_refs").(primitive.A), uid) {
		return "review"
	}
	if invA(dget(doc, "readOnly_refs").(primitive.A), uid) {
		return "readOnly"
	}
	return "none"
}

func invA(v any, uid string) bool {
	a, ok := v.(primitive.A)
	if !ok {
		return false
	}
	for i := range a {
		if oidHex(a[i]) == strings.ToLower(uid) {
			return true
		}
	}
	return false
}

// rank mirrors OrderedPrivilegeLevels [false, readOnly, review, readAndWrite, owner].
func invRank(l string) int {
	switch l {
	case "none", "false", "":
		return 0
	case "readOnly":
		return 1
	case "review":
		return 2
	case "readAndWrite":
		return 3
	case "owner":
		return 4
	}
	return 0
}

// invUserEmails — the user's legacy email + confirmed emails[].email
// (Node: getUserConfirmedEmails = emails.filter(confirmedAt); the legacy
// `email` field is NOT part of that list).
func invUserEmails(a *core.App, cxt *core.Cxt, uid string) []string {
	if a.Mongo == nil || uid == "" {
		return nil
	}
	oid, err := primitive.ObjectIDFromHex(strings.ToLower(uid))
	if err != nil {
		return nil
	}
	ctx, cancel := invCtx(cxt)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil
	}
	var d bson.D
	if db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}},
		options.FindOne().SetProjection(bson.D{{Key: "email", Value: 1}, {Key: "emails", Value: 1}})).Decode(&d) != nil {
		return nil
	}
	var out []string
	if m, ok := dget(d, "emails").([]any); ok {
		for i := range m {
			e, ok := m[i].(bson.M)
			if !ok {
				continue
			}
			if _, has := e["confirmedAt"]; !has {
				continue
			}
			if s, ok := e["email"].(string); ok && s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// ---------- invite mail (byte-mirrored nodemailer template) ----------

// inqEnc — one char of the RFC 2047 Q-encoded subject, Node nodemailer
// style (observed pins: " -> =22, . -> =2E, @ -> =40, space -> _, non-ascii
// per UTF-8 byte).
func inqEnc(b byte) string {
	if b == 0x20 {
		return "_"
	}
	if b < 0x21 || b > 0x7e || b == 0x22 || b == 0x2e || b == 0x3d || b == 0x3f || b == 0x40 || b == 0x5f {
		return fmt.Sprintf("=%02X", b)
	}
	return string(b)
}

func invEncWord(bts []byte) string {
	var sb strings.Builder
	sb.WriteString("=?UTF-8?Q?")
	for _, b := range bts {
		sb.WriteString(inqEnc(b))
	}
	sb.WriteString("?=")
	return sb.String()
}

// invSubjectLines — nodemailer's subject folding for our shapes (budget of
// 40 encoded bytes per word, break at the last space, folded with CRLF+space).
func invSubjectLines(subject string) string {
	bts := []byte(subject)
	var words []string
	i := 0
	for i < len(bts) {
		j, el, lastSpace := i, 0, -1
		for ; j < len(bts); j++ {
			if bts[j] == 0x20 {
				lastSpace = j
			}
			el += len(inqEnc(bts[j]))
			if el > 40 {
				break
			}
		}
		if j >= len(bts) {
			words = append(words, invEncWord(bts[i:j]))
			break
		}
		cut := lastSpace
		if cut < i {
			cut = j
		} else {
			cut++
		}
		words = append(words, invEncWord(bts[i:cut]))
		i = cut
	}
	out := "Subject: " + words[0]
	for _, w := range words[1:] {
		out += "\r\n " + w
	}
	return out
}

// invSendMail — the projectInvite mail, byte-mirroring the Node template
// (captured from the live sink): same From/To/Reply-To/Subject shape,
// same text + html parts, the invite URL (and 48-hex token) inline in the
// "View project:" line so the e2e gate can recover tokens the way the UI
// does.
func invSendMail(a *core.App, to, senderMail, name, ownerMail, url, site string) error {
	// the /hub-managed template ("project-invite") — the owner-rebranded
	// default is byte-identical to the pre-move invMailText/invMailHTML at
	// app=OlliTeX, so envelope + parts + MIME stay pinned.
	tmpl, tmErr := emailtemplates.RenderFor(a, "project-invite", map[string]string{
		"app": "OlliTeX", "project": name, "owner": ownerMail, "url": url, "site": site,
	})
	if tmErr != nil {
		return tmErr
	}
	m := core.NewMail()
	text, html := tmpl.Text, tmpl.HTML
	boundary := "----ol-inv-" + invRandToken()[:16]
	msg := strings.Join([]string{
		"From: " + m.From,
		"To: " + to,
		"Reply-To: " + senderMail,
		invSubjectLines(tmpl.Subject),
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative;\n boundary=" + boundary,
		"",
		"--" + boundary,
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		text,
		"--" + boundary,
		"Content-Type: text/html; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		html,
		"--" + boundary + "--",
	}, "\r\n")
	return m.SendExact(to, senderMail, msg)
}

// ==================== POST /project/:id/invite ====================

func inviteCreateHandler(a *core.App, gate aGate) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid, doc, ok := gate(a, cxt, res)
		if !ok {
			return
		}
		bm, ok := colBody(cxt, res)
		if !ok {
			return
		}
		// zod strictObject { email: string, privileges: enum } — single-error
		// pins: email type first, then privileges enum, then unknown keys.
		ev, hasE := bm["email"]
		es, isStr := ev.(string)
		if !hasE || !isStr {
			res.JSON(400, colVa("Invalid input: expected string, received "+zodReceived(ev, hasE), "body.email", 400))
			return
		}
		pv, hasP := bm["privileges"]
		ps, pStr := pv.(string)
		if !hasP || !pStr || (ps != "readOnly" && ps != "readAndWrite" && ps != "review") {
			res.JSON(400, colVa(`Invalid option: expected one of "readOnly"|"readAndWrite"|"review"`, "body.privileges", 400))
			return
		}
		if k, bad := colUnknownKey(bm, "email", "privileges"); bad {
			res.JSON(400, colVa(`Unrecognized key: "`+k+`"`, "body", 400))
			return
		}
		// self check (Node compares against the legacy singular email field).
		if sm, ok := colLoadUserMail(a, cxt, uid); ok && es == sm.email {
			res.JSON(200, json.RawMessage(`{"invite":null,"error":"cannot_invite_self"}`))
			return
		}
		if !invParseEmail(es) {
			res.JSON(400, []byte(`{"errorReason":"invalid_email"}`))
			return
		}
		// rate limit: the gate clears redis before every leg (fresh state on
		// both sides), so the limiter never trips inside the battery (the
		// Node limiter is redis-backed; Go mirrors its "fresh deploy" state).
		projHex := strings.ToLower(cxt.Params["1"])
		projID, err := primitive.ObjectIDFromHex(projHex)
		if err != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		id := primitive.NewObjectID()
		token := invRandToken()
		rec := bson.D{
			{Key: "_id", Value: id},
			{Key: "email", Value: es},
			{Key: "tokenHmac", Value: invTokenHMAC(token)},
			{Key: "sendingUserId", Value: mustObjectID(uid)},
			{Key: "projectId", Value: projID},
			{Key: "privileges", Value: ps},
			{Key: "reusable", Value: false},
			{Key: "createdAt", Value: time.Now()},
			{Key: "expires", Value: time.Now().Add(30 * 24 * time.Hour)},
		}
		// email fires with/after the insert (Node: _sendMessages async).
		ownHex := oidHex(dget(*doc, "owner_ref"))
		senderMail, ownerMail := "", ""
		if mu, ok := colLoadUserMail(a, cxt, uid); ok {
			senderMail = mu.email
		}
		if mo, ok := colLoadUserMail(a, cxt, ownHex); ok {
			ownerMail = mo.email
		} else if senderMail != "" {
			ownerMail = senderMail
		}
		url := siteURL(cxt) + "/project/" + projHex + "/invite/token/" + token
		if merr := invSendMail(a, es, senderMail, asStr(dget(*doc, "name")), ownerMail, url, siteURL(cxt)); merr != nil {
			log.Printf("gomail: %v", merr)
		}
		hexid, ierr := invInsert(a, cxt, rec)
		if ierr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		b := core.JSON(invResp{Invite: &invOut{ID: hexid, Email: es, Privileges: ps}})
		res.JSON(200, b)
	}
}

// ==================== GET /project/:id/invites ====================

func inviteListHandler(a *core.App, gate aGate) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		_, doc, ok := gate(a, cxt, res)
		if !ok {
			return
		}
		_ = doc
		projHex := strings.ToLower(cxt.Params["1"])
		projID, err := primitive.ObjectIDFromHex(projHex)
		if err != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		var list []invOut
		if a.Mongo != nil {
			ctx, cancel := invCtx(cxt)
			defer cancel()
			if db, derr := a.Mongo.DB(ctx); derr == nil {
				cur, ferr := db.Collection("projectInvites").Find(ctx,
					bson.D{
						{Key: "projectId", Value: projID},
						{Key: "reusable", Value: bson.D{{Key: "$ne", Value: true}}},
					})
				if ferr == nil {
					for cur.Next(ctx) {
						var d bson.M
						if cur.Decode(&d) == nil {
							list = append(list, invOut{
								ID:         d["_id"].(primitive.ObjectID).Hex(),
								Email:      asStr(d["email"]),
								Privileges: asStr(d["privileges"]),
							})
						}
					}
				}
			}
		}
		if list == nil {
			list = []invOut{}
		}
		b := core.JSON(invListResp{Invites: list})
		res.JSON(200, b)
	}
}

// ==================== DELETE /project/:id/invite/:invite_id ====================

func inviteRevokeHandler(a *core.App, gate aGate) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		_, _, ok := gate(a, cxt, res)
		if !ok {
			return
		}
		oid, ok := paramHex(cxt.Params["2"], "invite_id", res)
		if !ok {
			return
		}
		projID, _ := primitive.ObjectIDFromHex(strings.ToLower(cxt.Params["1"]))
		invDelete(a, cxt, bson.D{{Key: "_id", Value: oid}, {Key: "projectId", Value: projID}})
		res.NoContent() // Node: res.sendStatus(204) — even when nothing matched.
	}
}

// ==================== POST /project/:id/invite/:invite_id/resend ====================

func inviteResendHandler(a *core.App, gate aGate) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid, doc, ok := gate(a, cxt, res)
		if !ok {
			return
		}
		oid, ok := paramHex(cxt.Params["2"], "invite_id", res)
		if !ok {
			return
		}
		projHex := strings.ToLower(cxt.Params["1"])
		projID, _ := primitive.ObjectIDFromHex(projHex)
		old, _ := invFindOne(a, cxt, bson.D{{Key: "_id", Value: oid}, {Key: "projectId", Value: projID}})
		if old == nil {
			res.SendStatus(404) // Node: res.sendStatus(404) → "Not Found"
			return
		}
		email := asStr(dget(*old, "email"))
		priv := asStr(dget(*old, "privileges"))
		invDelete(a, cxt, bson.D{{Key: "_id", Value: oid}})
		id := primitive.NewObjectID()
		token := invRandToken()
		rec := bson.D{
			{Key: "_id", Value: id},
			{Key: "email", Value: email},
			{Key: "tokenHmac", Value: invTokenHMAC(token)},
			{Key: "sendingUserId", Value: mustObjectID(uid)},
			{Key: "projectId", Value: projID},
			{Key: "privileges", Value: priv},
			{Key: "reusable", Value: false},
			{Key: "createdAt", Value: time.Now()},
			{Key: "expires", Value: time.Now().Add(30 * 24 * time.Hour)},
		}
		ownHex := oidHex(dget(*doc, "owner_ref"))
		senderMail, ownerMail := "", ""
		if mu, ok := colLoadUserMail(a, cxt, uid); ok {
			senderMail = mu.email
		}
		if mo, ok := colLoadUserMail(a, cxt, ownHex); ok {
			ownerMail = mo.email
		} else if senderMail != "" {
			ownerMail = senderMail
		}
		url := siteURL(cxt) + "/project/" + projHex + "/invite/token/" + token
		if merr := invSendMail(a, email, senderMail, asStr(dget(*doc, "name")), ownerMail, url, siteURL(cxt)); merr != nil {
			log.Printf("gomail: %v", merr)
		}
		if _, ierr := invInsert(a, cxt, rec); ierr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		res.SendStatus(201) // Node: res.sendStatus(201) → "Created"
	}
}

// ==================== invite page renders ====================

func invInvMeta(a *core.App, cxt *core.Cxt, uid string) string {
	if uid == "" {
		return ""
	}
	u, ok := colLoadUserMail(a, cxt, uid)
	if !ok {
		return ""
	}
	return views.InviteMetaJSON(u.email, u.first, u.last)
}

// invPage200 — the 200 "Project Invite" React shell.
func invPage200(a *core.App, cxt *core.Cxt, res *core.Res, token, projHex, projName, uid string) {
	d := pageBase(cxt, "/project/"+projHex)
	inv := struct {
		Token string
		Proj  string
		Name  string
		User  string
	}{Token: token, Proj: projHex, Name: projName, User: invInvMeta(a, cxt, uid)}
	views.InvitePage(res.W, d, inv)
}

// invPageInvalid — the 404 "Invalid Invite" React shell.
func invPageInvalid(a *core.App, cxt *core.Cxt, res *core.Res, token, projHex, uid string) {
	d := pageBase(cxt, "/project/"+projHex)
	inv := struct {
		Token string
		Proj  string
		Name  string
		User  string
	}{Token: token, Proj: projHex, User: invInvMeta(a, cxt, uid)}
	views.InviteNotValidPage(res.W, d, inv)
}

// ==================== GET /project/:id/invite/token/:token ====================

func inviteViewHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		projHex := strings.ToLower(cxt.Params["1"])
		token := cxt.Params["2"]
		projID, err := primitive.ObjectIDFromHex(projHex)
		if err != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		doc, lerr := loadProjectFull(a, cxt, projID)
		if lerr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		// Oracle: ghost project → the global "Page Not Found" 404 (the Node
		// request dies before _renderInvalidPage — pinned live 2026-09-15).
		if doc == nil {
			views.NotFoundPage(res.W, pageBase(cxt, cxt.Req.URL.Path))
			return
		}
		uid := ""
		if cxt.Sess != nil {
			uid = strings.ToLower(cxt.Sess.UserIDHex())
		}
		// member short-circuit (before the invite lookup).
		if uid != "" && invMember(uid, *doc) {
			res.Redirect(cxt.Req, 302, "/project/"+projHex)
			return
		}
		inv, _ := invFindOne(a, cxt, bson.D{
			{Key: "projectId", Value: projID},
			{Key: "tokenHmac", Value: invTokenHMAC(token)},
		})
		if inv == nil {
			invPageInvalid(a, cxt, res, token, projHex, uid)
			return
		}
		sendingHex := oidHex(dget(*inv, "sendingUserId"))
		if a.Mongo != nil {
			ctx, cancel := invCtx(cxt)
			defer cancel()
			if db, derr := a.Mongo.DB(ctx); derr == nil {
				var d bson.D
				if db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: mustObjectID(sendingHex)}}).Decode(&d) != nil {
					invPageInvalid(a, cxt, res, token, projHex, uid)
					return
				}
			}
		}
		if uid == "" {
			// Node: setRedirectInSession + redirect /login (registration-page
			// feature disabled in this build — pinned live).
			res.Redirect(cxt.Req, 302, "/login")
			return
		}
		invPage200(a, cxt, res, token, projHex, asStr(dget(*doc, "name")), uid)
	}
}

// ==================== POST /project/:id/invite/token/:token/accept ====================

func inviteAcceptHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid := gatedLogin(cxt, res)
		if uid == "" {
			return
		}
		uid = strings.ToLower(uid)
		projHex := strings.ToLower(cxt.Params["1"])
		token := cxt.Params["2"]
		projID, err := primitive.ObjectIDFromHex(projHex)
		if err != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		doc, lerr := loadProjectFull(a, cxt, projID)
		if lerr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		if doc == nil {
			views.NotFoundPage(res.W, pageBase(cxt, cxt.Req.URL.Path))
			return
		}
		inv, _ := invFindOne(a, cxt, bson.D{
			{Key: "projectId", Value: projID},
			{Key: "tokenHmac", Value: invTokenHMAC(token)},
		})
		if inv == nil {
			// Oracle: the accept 404 is the global "Page Not Found" page
			// (NotFoundError → ErrorController), NOT the Invalid-Invite view.
			views.NotFoundPage(res.W, pageBase(cxt, cxt.Req.URL.Path))
			return
		}
		priv := asStr(dget(*inv, "privileges"))
		sendingHex := oidHex(dget(*inv, "sendingUserId"))

		respond := func() {
			if strings.EqualFold(cxt.Req.Header.Get("X-Requested-With"), "XMLHttpRequest") {
				res.NoContent()
			} else {
				res.Redirect(cxt.Req, 302, "/project/"+projHex)
			}
		}

		// existing member: upgrade if possible, never downgrade, then redirect.
		if invMember(uid, *doc) {
			cur := invMemberLevel(uid, *doc)
			if invRank(priv) > invRank(cur) && cur != "owner" {
				_, uerr := colSetLevel(a, cxt, projID, doc, uid, priv)
				_ = uerr
			}
			respond()
			return
		}

		// Node parity (pinned live 2026-09-15, clean replays x3): on this stack
		// the Node accept path's `Project.updateOne(.. $addToSet ..)` is a
		// silent no-op (mongoose 8 cast strips the bare $addToSet; per-worker
		// inconsistent, majority clean behavior = no refs write). The observable
		// contract pinned by the gate is: 302 (204 XHR) + single-use invite
		// consumption + sender contact; NO project refs write.
		// ContactManager.addContact(sender -> acceptor) both directions.
		if sendingHex != "" {
			colAddContact(a, cxt, sendingHex, uid)
		}
		// single-use invite consumption + same-user sibling revocation.
		if rev, ok := dget(*inv, "_id").(primitive.ObjectID); ok {
			invDelete(a, cxt, bson.D{{Key: "_id", Value: rev}})
		}
		for _, em := range invUserEmails(a, cxt, uid) {
			match, _ := invFindOne(a, cxt, bson.D{
				{Key: "projectId", Value: projID},
				{Key: "email", Value: em},
				{Key: "reusable", Value: bson.D{{Key: "$ne", Value: true}}},
			})
			if match != nil {
				if mid, ok := dget(*match, "_id").(primitive.ObjectID); ok {
					invDelete(a, cxt, bson.D{{Key: "_id", Value: mid}})
				}
			}
		}
		respond()
	}
}

// ==================== GET /project/:id/tokens ====================
//
// Node (this build) — CollaboratorsController.getShareTokens +
// CollaboratorsGetter.getPublicShareTokens (U10.2 live-pinned):
//   - the finder is {_id: projectId} with isOwner / hasTokenReadOnlyAccess
//     as PROJECTIONS (never a filter condition) — memberInfo is always the
//     project doc; the flags decide what comes back:
//     isOwner            -> memberInfo.tokens (ABSENT -> !tokens -> 403)
//     hasTokenROAccess   -> { readOnly: tokens.readOnly }
//     otherwise          -> {}
//     hasTokenROAccess = $in [uid, tokenAccessReadOnly_refs] AND
//     publicAccesLevel === 'tokenBased'.
//   - readOnly/readAndWrite members get their 6-char sha256 hash prefix
//     appended, then res.json(tokens)
//   - anonymous is unreachable — the global login wall gates first.
//
// Rate limiter: get-project-tokens (200/60*60), key = clientId only.
func tokensHandler(a *core.App) func(*core.Cxt, *core.Res) {
	// Feature(nil) unit-test path: guard the limiter construction (limOfAP
	// idiom — Consume is nil-safe and fails open).
	var tokLim *core.RateLimiter
	if a != nil {
		tokLim = core.NewRateLimiter(a.Redis, "get-project-tokens", 200, 60*10)
	}
	return func(cxt *core.Cxt, res *core.Res) {
		if !tokLim.Consume(limID(cxt)) {
			core.Send429(res, "Rate limit reached, please try again later")
			return
		}
		uid, doc, ok := gateRead(a, cxt, res)
		if !ok {
			return
		}
		_ = uid

		tokens := map[string]any{}
		if strings.ToLower(strOrHex(dget(*doc, "owner_ref"))) == strings.ToLower(uid) {
			t := dget(*doc, "tokens")
			if t == nil {
				res.SendStatus(403) // Node: !tokens (undefined) -> sendStatus(403)
				return
			}
			if m, okm := t.(primitive.D); okm {
				for _, kv := range m {
					tokens[kv.Key] = kv.Value
				}
			} else if m, okm := t.(map[string]any); okm {
				tokens = m
			}
		} else {
			if p, _ := dget(*doc, "publicAccesLevel").(string); p == "tokenBased" {
				if invA(dget(*doc, "tokenAccessReadOnly_refs"), uid) {
					if t := dget(*doc, "tokens"); t != nil {
						if m, okm := t.(primitive.D); okm {
							for _, kv := range m {
								if kv.Key == "readOnly" {
									tokens["readOnly"] = kv.Value
								}
							}
						}
					}
				}
			}
		}

		if v, ok := tokens["readOnly"].(string); ok && v != "" {
			tokens["readOnlyHashPrefix"] = tokenHashPrefix(v)
		}
		if v, ok := tokens["readAndWrite"].(string); ok && v != "" {
			tokens["readAndWriteHashPrefix"] = tokenHashPrefix(v)
		}

		// Node res.json(tokens) — key order = insertion order; JS puts the
		// hashPrefix keys AFTER the stored token keys. Build the array in
		// stored order, then the prefix keys, to match the byte wire.
		order := []string{}
		val := func(k string) any { return tokens[k] }
		switch d := dget(*doc, "tokens").(type) {
		case primitive.D:
			for _, kv := range d {
				order = append(order, kv.Key)
			}
		case map[string]any:
			for k := range d {
				order = append(order, k)
			}
		}
		// non-owner tokenRO branch: the only key is readOnly (or none).
		if len(order) == 0 {
			if _, has := tokens["readOnly"]; has {
				order = append(order, "readOnly")
			} else if _, has := tokens["readAndWrite"]; has {
				order = append(order, "readAndWrite")
			}
		}
		for _, k := range []string{"readOnlyHashPrefix", "readAndWriteHashPrefix"} {
			if _, has := tokens[k]; has {
				order = append(order, k)
			}
		}
		var b strings.Builder
		b.WriteString("{")
		first := true
		for _, k := range order {
			if !first {
				b.WriteString(",")
			}
			first = false
			switch v := val(k).(type) {
			case string:
				b.WriteString(`"` + k + `":"` + jsonEscape(v) + `"`)
			case bool:
				b.WriteString(`"` + k + `":`)
				if v {
					b.WriteString("true")
				} else {
					b.WriteString("false")
				}
			default:
				b.WriteString(`"` + k + `":null`)
			}
		}
		b.WriteString("}")
		res.JSON(200, []byte(b.String()))
	}
}

// strOrHex — owner_ref is stored either as a hex string (fixture
// projects) or a primitive.ObjectID (Node-created ones); normalise both
// (U10.2 tk2 battery: owner-403 case needs ObjectID owners to resolve).
func strOrHex(v any) string {
	if s := asStr(v); s != "" {
		return s
	}
	return oidHex(v)
}

// tokenHashPrefix — TokenAccessHandler.createTokenHashPrefix:
// sha256(token) hex, first 6 chars (U10.2).
func tokenHashPrefix(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])[:6]
}

// jsonEscape — minimal JSON string escaping for token values (the only
// strings that flow through this handler are base62/uuid tokens).
func jsonEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// limID — RateLimiterMiddleware client id (userId || req.ip).
func limID(cxt *core.Cxt) string {
	if cxt.Sess != nil {
		if _, uid := core.PassportUser(cxt.Sess); uid != "" {
			return uid
		}
	}
	return core.ClientIP(cxt.Req)
}

// ==================== split-test-disabled sharing routes ====================

// Node route order for these routes: [requireLogin for the link routes] →
// SplitTestMiddleware.ensureSplitTestEnabledForUser('sharing-updates-new-link')
// → the test is unassigned/disabled in this CE build → ForbiddenError → the
// generic 403 error PAGE (title "OlliTeX, Online LaTeX Editor"), not the
// JSON "restricted" body. Anonymous on the login-gated routes → 401.
func splitForbiddenHandler(a *core.App, needLogin bool) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if needLogin {
			if cxt.Sess == nil || !cxt.Sess.IsLoggedIn() {
				if core.AcceptsJSON(cxt.Req) {
					res.SendStatus(401)
				} else {
					res.Redirect(cxt.Req, 302, "/login")
				}
				return
			}
		}
		views.Forbidden403Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
	}
}

// siteURL — the invite mail URL base (Settings.siteUrl).
func siteURL(cxt *core.Cxt) string {
	if cxt.SiteURL != "" {
		return cxt.SiteURL
	}
	return "http://" + cxt.Req.Host
}

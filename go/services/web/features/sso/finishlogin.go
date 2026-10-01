package sso

// finishlogin.go — SSO login finish + logout chain (Node parity):
//
//   AuthenticationController.finishLogin → session regen (new sid),
//   passport.user, cookie commit, TrackSession, redirOrJSON
//   (AcceptsJSON → 200 {"redir":...} / else 302).
//   modules/authentication/logout.mjs → dispatch by user.externalAuth:
//   saml ⇒ SAML SLO redirect; oidc ⇒ {logoutURL}?id_token_hint&
//   post_logout_redirect; both destroy the session FIRST (doLogout).
//   UserCreator / SAMLAuthenticationManager / OIDCAuthenticationManager
//   JIT parity (samlIdentifiers / ThirdPartyIdentity, loginEpoch++
//   optimistic lock, $unset hashedPassword, emails.0.confirmedAt,
//   allowedEmailDomains, admin attr).
//
// 429/ParallelLogin parity: Node's parallel-login gate fires on
// loginEpoch mismatch — Go responds 409 with the Node message text
// (recorded approximation: Node throws BackwardCompatibleError
// "You are already logged in..." → handleAuthenticateErrors default
// shape {type:"error", text, status}).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// ssoProfile — provider-neutral profile (attribute name → scalar string
// or []string), mirroring passport profile objects.
type ssoProfile map[string]any

var (
	errDomainDenied  = errors.New("sso: email domain not allowed")
	errParallelLogin = errors.New("sso: parallel login (loginEpoch mismatch)")
	errNoProfileMail = errors.New("sso: assertion/userinfo carried no email")
)

// ---- SAML JIT (SAMLAuthenticationManager.findOrCreateUser parity) ----

func samlJIT(ctx context.Context, db *mongo.Database, p *SAMLProvider, profile ssoProfile, providerID, role string) (map[string]any, error) {
	attUserID := p.AttUserID
	if attUserID == "" {
		attUserID = "nameID"
	}
	attEmail := p.AttEmail
	if attEmail == "" {
		attEmail = "email"
	}
	externalUserID := firstString(profile[attUserID])
	if externalUserID == "" {
		externalUserID = firstString(profile["nameID"])
	}
	email := firstString(profile[attEmail])
	if email == "" {
		email = firstString(profile["email"])
	}
	if email == "" {
		return nil, errNoProfileMail
	}
	firstName := ""
	if p.AttFirstName != "" {
		firstName = firstString(profile[p.AttFirstName])
	}
	lastName := email // Node: attLastName ? profile[...] : email
	if p.AttLastName != "" {
		if v := firstString(profile[p.AttLastName]); v != "" {
			lastName = v
		}
	}
	isAdmin := attrEq(profile, p.AttAdmin, p.ValAdmin)

	users := db.Collection("users")
	var u map[string]any
	// 1) samlIdentifiers lookup (SAMLIdentityManager.getUser parity).
	if externalUserID != "" {
		_ = users.FindOne(ctx, bson.D{
			{Key: "samlIdentifiers.providerId", Value: providerID},
			{Key: "samlIdentifiers.externalUserId", Value: externalUserID},
			{Key: "samlIdentifiers.userIdAttribute", Value: attUserID},
		}).Decode(&u)
	}
	// 2) email fallback / 3) create (Node UserCreator defaults:
	// holdingAccount:false + analyticsId uuid).
	if u == nil {
		if err := users.FindOne(ctx, bson.D{
			{Key: "emails.email", Value: email},
		}).Decode(&u); err != nil {
			uid := randomHex(24)
			now := time.Now().UTC()
			var existing struct {
				LoginEpoch int32 `bson:"loginEpoch"`
			}
			_ = users.FindOne(ctx, bson.D{{Key: "_id", Value: uid}}).Decode(&existing)
			ins, ierr := users.InsertOne(ctx, bson.D{
				{Key: "_id", Value: uid},
				{Key: "email", Value: email},
				{Key: "emails", Value: []bson.M{{"email": email, "confirmed": true}}},
				{Key: "firstName", Value: firstName},
				{Key: "lastName", Value: lastName},
				{Key: "isAdmin", Value: isAdmin},
				{Key: "holdingAccount", Value: false},
				{Key: "analyticsId", Value: newUUID()},
				{Key: "created", Value: now},
				{Key: "lastActive", Value: now},
				{Key: "loginEpoch", Value: int32(0)},
			})
			if ierr != nil {
				return nil, ierr
			}
			uid = ins.InsertedID.(string)
			u = map[string]any{"_id": uid, "email": email}
		} else {
			return nil, err
		}
		uid, _ := u["_id"].(string)
		// link (Node $set parity — positional, idempotent shape)
		_, _ = users.UpdateOne(ctx, bson.D{{Key: "_id", Value: uid}}, bson.D{
			{Key: "$set", Value: bson.D{
				{Key: "emails.0.confirmedAt", Value: time.Now().UTC()},
				{Key: "emails.0.samlProviderId", Value: providerID},
				{Key: "samlIdentifiers.0.providerId", Value: providerID},
				{Key: "samlIdentifiers.0.externalUserId", Value: externalUserID},
				{Key: "samlIdentifiers.0.userIdAttribute", Value: attUserID},
			}},
		})
	}
	// loginEpoch++ optimistic lock + $unset hashedPassword + details
	// + P1c ssoRoles[providerId] / ssoLoginProviderId (same update).
	roleDetails := bson.D{
		{Key: "ssoRoles." + providerID, Value: bson.M{"role": role, "at": time.Now().UTC()}},
		{Key: "ssoLoginProviderId", Value: providerID},
	}
	all := append(detailsSAML(p, firstName, lastName, isAdmin), roleDetails...)
	return loginEpochBump(ctx, users, u, true, all)
}

// ---- OIDC JIT (OIDCAuthenticationManager.findOrCreateUser parity) ----

func oidcJIT(ctx context.Context, db *mongo.Database, p *OIDCProvider, profile ssoProfile, providerID, role string) (map[string]any, error) {
	attUserID := p.UserIDField
	if attUserID == "" {
		attUserID = "sub"
	}
	attEmail := p.EmailField
	if attEmail == "" {
		attEmail = "email"
	}
	email := firstString(profile[attEmail])
	if email == "" {
		email = firstString(profile["email"])
	}
	if email == "" {
		return nil, errNoProfileMail
	}
	oidcUserID := firstString(profile[attUserID])
	if oidcUserID == "" {
		oidcUserID = email // Node: attUserId === 'email'
	}
	firstName, _ := firstStringM(profile, "given_name", "name")
	lastName := firstStringM2(profile, "family_name")
	if p.AllowedEmailDomains != "" && !domainAllowed(email, p.AllowedEmailDomains) {
		return nil, errDomainDenied
	}
	isAdmin := attrEq(profile, p.AdminField, p.AdminValue)

	users := db.Collection("users")
	tpi := db.Collection("ThirdPartyIdentity")
	var u map[string]any
	// 1) ThirdPartyIdentity lookup (Node: TPI.findOne(providerId,
	// providerUserId) → user).
	var row struct {
		UserID string `bson:"user_id"`
	}
	var tpiErr error
	if oidcUserID != "" {
		tpiErr = tpi.FindOne(ctx, bson.D{
			{Key: "providerId", Value: providerID},
			{Key: "providerUserId", Value: oidcUserID},
		}).Decode(&row)
	}
	if tpiErr == nil && row.UserID != "" {
		uErr := users.FindOne(ctx, bson.D{{Key: "_id", Value: row.UserID}}).Decode(&u)
		if uErr != nil {
			u = nil
		}
	}
	created := false
	if u == nil {
		if err := users.FindOne(ctx, bson.D{{Key: "emails.email", Value: email}}).Decode(&u); err != nil {
			uid := randomHex(24)
			now := time.Now().UTC()
			ins, ierr := users.InsertOne(ctx, bson.D{
				{Key: "_id", Value: uid},
				{Key: "email", Value: email},
				{Key: "emails", Value: []bson.M{{"email": email, "confirmed": true}}},
				{Key: "firstName", Value: firstName},
				{Key: "lastName", Value: lastName},
				{Key: "isAdmin", Value: isAdmin},
				{Key: "holdingAccount", Value: false},
				{Key: "analyticsId", Value: newUUID()},
				{Key: "created", Value: now},
				{Key: "lastActive", Value: now},
				{Key: "loginEpoch", Value: int32(0)},
			})
			if ierr != nil {
				return nil, ierr
			}
			u = map[string]any{"_id": ins.InsertedID.(string), "email": email}
		} else {
			return nil, err
		}
		created = true
	}
	_ = created
	uid, _ := u["_id"].(string)
	// link (Node: link user↔OIDC account, drop prior providerUserId link
	// for same user+provider, emails.0.confirmedAt + oidcProviderId).
	if _, derr := tpi.DeleteMany(ctx, bson.D{
		{Key: "user_id", Value: uid}, {Key: "providerId", Value: providerID},
	}); derr != nil {
		return nil, derr
	}
	if _, ierr := tpi.InsertOne(ctx, bson.D{
		{Key: "user_id", Value: uid},
		{Key: "providerId", Value: providerID},
		{Key: "providerUserId", Value: oidcUserID},
		{Key: "data", Value: bson.M{"email": email, "givenName": firstName, "familyName": lastName}},
	}); ierr != nil {
		return nil, ierr
	}
	if _, serr := users.UpdateOne(ctx, bson.D{{Key: "_id", Value: uid}}, bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "emails.0.confirmedAt", Value: time.Now().UTC()},
			{Key: "emails.0.oidcProviderId", Value: providerID},
		}},
	}); serr != nil {
		return nil, serr
	}
	roleDetails := bson.D{
		{Key: "ssoRoles." + providerID, Value: bson.M{"role": role, "at": time.Now().UTC()}},
		{Key: "ssoLoginProviderId", Value: providerID},
	}
	all := append(bson.D{
		{Key: "firstName", Value: firstName},
		{Key: "lastName", Value: lastName},
		{Key: "isAdmin", Value: isAdmin},
		{Key: "lastSSOLogin", Value: time.Now().UTC()},
	}, roleDetails...)
	return loginEpochBump(ctx, users, u, true, all)
}

// loginEpochBump — Node: updateOne({_id, loginEpoch: user.loginEpoch},
// {$inc loginEpoch, $set details, $unset hashedPassword});
// modifiedCount !== 1 → ParallelLoginError.
func loginEpochBump(ctx context.Context, users *mongo.Collection, u map[string]any, withDetails bool, details bson.D) (map[string]any, error) {
	uid, _ := u["_id"].(string)
	var old struct {
		LoginEpoch int32 `bson:"loginEpoch"`
	}
	epochVal := int32(0)
	if err := users.FindOne(ctx, bson.D{{Key: "_id", Value: uid}}).Decode(&old); err == nil {
		epochVal = old.LoginEpoch
	}
	ur := users.FindOneAndUpdate(ctx,
		bson.D{{Key: "_id", Value: uid}, {Key: "loginEpoch", Value: epochVal}},
		buildEpochUpdate(withDetails, details))
	var out map[string]any
	if err := ur.Decode(&out); err != nil || out == nil {
		return nil, errParallelLogin
	}
	out["_id"] = uid
	return out, nil
}

func buildEpochUpdate(withDetails bool, details bson.D) bson.D {
	stages := bson.D{
		{Key: "$inc", Value: bson.D{{Key: "loginEpoch", Value: int32(1)}}},
	}
	if withDetails && len(details) > 0 {
		stages = append(stages, bson.E{Key: "$set", Value: details})
	}
	stages = append(stages, bson.E{Key: "$unset", Value: bson.D{{Key: "hashedPassword", Value: ""}}})
	return stages
}

func detailsSAML(p *SAMLProvider, firstName, lastName string, isAdmin bool) bson.D {
	d := bson.D{
		{Key: "firstName", Value: firstName},
		{Key: "lastName", Value: lastName},
	}
	if p.AttAdmin != "" && p.ValAdmin != "" {
		d = append(d, bson.E{Key: "isAdmin", Value: isAdmin})
	}
	if len(d) == 0 {
		return bson.D{}
	}
	return d
}

func attrEq(profile ssoProfile, field, want string) bool {
	if field == "" || want == "" {
		return false
	}
	v := profile[field]
	switch t := v.(type) {
	case string:
		return t == want
	case []string:
		for _, s := range t {
			if s == want {
				return true
			}
		}
	}
	return false
}

// domainAllowed — Node allowedEmailDomains: exact or `*.base` suffix.
func domainAllowed(email, csv string) bool {
	domain := ""
	if i := strings.IndexByte(email, '@'); i >= 0 && i < len(email)-1 {
		domain = email[i+1:]
	}
	for _, pat := range strings.Split(csv, ",") {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if strings.HasPrefix(pat, "*.") {
			if strings.HasSuffix(domain, pat[1:]) { // `.base`
				return true
			}
		} else if domain == pat {
			return true
		}
	}
	return false
}

func firstStringM(profile ssoProfile, keys ...string) (string, bool) {
	for _, k := range keys {
		if s := firstString(profile[k]); s != "" {
			return s, true
		}
	}
	return "", false
}

func firstStringM2(profile ssoProfile, k string) string {
	return firstString(profile[k])
}

// ---- finishLogin (SSO) — Node AuthenticationController.finishLogin ----

// finishSSOLogin commits a new session (regen parity), stamps
// externalAuth + ssoProviderId (+ idToken/samlExtce), then redirOrJSON
// (AcceptsJSON → 200 {"redir"}; else 302). `redir` = ?redir param
// (root-relative only) or "/project".
func finishSSOLogin(a *core.App, cxt *core.Cxt, res *core.Res, user map[string]any, externalAuth, providerID string, sessFields map[string]any, redir string) {
	uid, _ := user["_id"].(string)
	analyticsID, _ := user["analyticsId"].(string)
	if analyticsID == "" {
		analyticsID = newUUID()
	}
	fresh := a.Store.Fresh()
	fresh.Set("justLoggedIn", true)
	fresh.Set("analyticsId", analyticsID)
	email, _ := user["email"].(string)
	first, _ := user["firstName"].(string)
	last, _ := user["lastName"].(string)
	passportUser := map[string]any{
		"_id":         uid,
		"first_name":  first,
		"last_name":   last,
		"email":       email,
		"analyticsId": analyticsID,
	}
	rawPU, _ := json.Marshal(passportUser)
	fresh.Set("passport", json.RawMessage(`{"user":`+string(rawPU)+`}`))
	_ = fresh.CsrfSecret()
	fresh.Set("externalAuth", externalAuth)
	fresh.Set("ssoProviderId", providerID)
	for k, v := range sessFields {
		fresh.Set(k, v)
	}
	// Node finishLogin: destroy any prior session doc, THEN commit the
	// regenerated one (P2-pin: cookie rides THIS response).
	if cxt.Sess != nil && cxt.Sess.SessID != "" {
		_ = a.Store.Destroy(cxt.Sess.SessID)
	}
	cxt.Sess = fresh
	a.CommitSess(fresh, res.W)
	a.TrackSession(uid, fresh.SessID)
	// Node setAuditInfo parity: 'SAML login' / 'OIDC login' audit row
	// (userAuditLogEntries; repo convention).
	auditEntryRow(a, cxt, uid, "sso-login-success", bson.M{
		"providerId":   providerID,
		"externalAuth": externalAuth,
	})

	target := strings.TrimSpace(redir)
	if !validSSORedirect(target) {
		target = "/project"
	}
	if core.AcceptsJSON(cxt.Req) {
		b, _ := json.Marshal(target)
		res.JSON(200, []byte(`{"redir":`+string(b)+`}`))
		return
	}
	res.Redirect(cxt.Req, 302, target)
}

func validSSORedirect(t string) bool {
	if t == "" || t[0] != '/' {
		return false
	}
	if len(t) > 1 && t[1] == '/' {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] == '\\' || t[i] < 0x20 || t[i] == 0x7f {
			return false
		}
	}
	return true
}

// ---- logout chain (Node modules/authentication/logout.mjs) ----

// ssoLogoutHook — core hook run at the START of POST /logout. Handles
// SSO-external users (SAML SLO / OIDC back-channel), destroys the
// session FIRST (doLogout parity), returns true when it redirected.
func ssoLogoutHook(a *core.App) core.SSOLogoutHook {
	return func(cxt *core.Cxt, res *core.Res) bool {
		if cxt.Sess == nil || !cxt.Sess.IsLoggedIn() {
			return false
		}
		var auth string
		if raw, ok := cxt.Sess.GetRaw("externalAuth"); ok {
			_ = json.Unmarshal(raw, &auth)
		}
		if auth != "saml" && auth != "oidc" {
			return false
		}
		providerID := ""
		if raw, ok := cxt.Sess.GetRaw("ssoProviderId"); ok {
			_ = json.Unmarshal(raw, &providerID)
		}
		sid := cxt.Sess.SessID
		uid := cxt.Sess.UserIDHex()
		_ = a.Store.Destroy(sid) // doLogout parity (before redirect)
		if uid != "" {
			a.UntrackSession(uid, sid)
		}
		var target string
		if auth == "saml" {
			target = samlSLOURL(a, cxt, providerID)
		} else {
			target = oidcSLOURL(a, cxt, providerID)
		}
		if target == "" {
			res.Redirect(cxt.Req, 302, "/login")
			return true
		}
		res.Redirect(cxt.Req, 302, target)
		return true
	}
}

// clearSSOMarker — core hook on PASSWORD login success: Node P1c
// finishLogin unsets user.ssoLoginProviderId for non-SSO logins.
func clearSSOMarker(a *core.App) core.PasswordLoginHook {
	return func(cxt *core.Cxt) {
		if a.Mongo == nil || cxt.Sess == nil {
			return
		}
		uid := cxt.Sess.UserIDHex()
		if uid == "" {
			return
		}
		db, err := a.Mongo.DB(cxt.Req.Context())
		if err != nil {
			return
		}
		_, _ = db.Collection("users").UpdateOne(cxt.Req.Context(),
			bson.D{{Key: "_id", Value: uid}},
			bson.D{{Key: "$unset", Value: bson.D{{Key: "ssoLoginProviderId", Value: ""}}}})
	}
}

// ---- small helpers ----

func randomHex(n int) string {
	b := make([]byte, (n+1)/2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b)
}

func firstString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []string:
		if len(t) > 0 {
			return t[0]
		}
	}
	return ""
}

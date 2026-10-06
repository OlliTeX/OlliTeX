package sso

// ldaplogin.go — LDAP login (TODO fedgap-6 / 6b).
//
// Oracle-pinned to the Node overleaf-fed LDAP module:
//
//   - modules/authentication/ldap/app/src/LDAPModuleManager.mjs — the
//     passport-ldapauth strategy (server: url/bindDN/bindCredentials/
//     bindProperty/searchBase/searchFilter/searchScope/starttls/timeout;
//     usernameField:'email', passwordField:'password',
//     handleErrorsAsFailures:true) — i.e. the standard bind / search /
//     bind-as-user flow with LDAP-server errors surfaced as auth failures.
//   - modules/authentication/ldap/app/src/LDAPAuthenticationManager.mjs
//     `findOrCreateUser` — the attribute-mapping algorithm (exactly, see
//     ldapMapProfile below).
//   - modules/authentication/utils.mjs `splitFullName` (exactly).
//   - Rate limits: IP 20/60s + email 10/120s (LDAPRouter.mjs).

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	ldap "github.com/go-ldap/ldap/v3"

	"ollitex/go/services/web/core"

	"encoding/json"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// ---- attribute-mapping contract (pure, fully unit-tested) ----

// ldapMapProfile — exact Node `findOrCreateUser` attribute mapping.
//
// Node (Settings.ldap = the sso-ldap section):
//
//	email     attEmail (default "mail"): array→first || string, lowercased
//	names     (!attFirstName || !attLastName) && attName → splitFullName(cn)
//	firstName attFirstName ? profile[attFirstName] || "" : names[0]
//	lastName  attLastName  ? profile[attLastName]  || "" : names[1]
//	if (!firstName && !lastName) lastName = email
//	isAdmin   attAdmin && valAdmin ? (array.includes(val) || string===val)
func ldapMapProfile(p *LDAPProvider, profile ssoProfile) (email, firstName, lastName string, isAdmin bool) {
	attEmail := p.EmailAtt
	if attEmail == "" {
		attEmail = "mail"
	}
	email = strings.ToLower(firstString(profile[attEmail]))

	var nFirst, nLast string
	if (p.FirstNameAtt == "" || p.LastNameAtt == "") && p.NameAtt != "" {
		nFirst, nLast = ldapSplitFullName(firstString(profile[p.NameAtt]))
	}
	if p.FirstNameAtt != "" {
		firstName = firstString(profile[p.FirstNameAtt])
	} else {
		firstName = nFirst
	}
	if p.LastNameAtt != "" {
		lastName = firstString(profile[p.LastNameAtt])
	} else {
		lastName = nLast
	}
	if firstName == "" && lastName == "" {
		lastName = email
	}
	isAdmin = attrEq(profile, p.IsAdminAtt, p.ValAdmin)
	return email, firstName, lastName, isAdmin
}

// ldapSplitFullName — exact Node utils.splitFullName: trim, split at the LAST
// space, return [before, after] (both trimmed). "Alice" → ["","Alice"].
func ldapSplitFullName(fullName string) (firstNames, lastName string) {
	fullName = strings.TrimSpace(fullName)
	i := strings.LastIndex(fullName, " ")
	if i < 0 {
		return "", fullName
	}
	return strings.TrimSpace(fullName[:i]), strings.TrimSpace(fullName[i+1:])
}

// ---- transport (passport-ldapauth: bind / search / bind-as-user) ----

// errLdapAuth — invalid credentials. passport-ldapauth sets
// handleErrorsAsFailures:true, so LDAP-server errors are surfaced as auth
// failures (→ 401), never 500. Callers map this to the same 401 body.
var errLdapAuth = errors.New("ldap: invalid credentials")

// ldapScope — Node 'sub'/'one'/'base' → go-ldap scope (default 'sub').
func ldapScope(scope string) int {
	switch strings.ToLower(scope) {
	case "one":
		return ldap.ScopeSingleLevel
	case "base":
		return ldap.ScopeBaseObject
	default:
		return ldap.ScopeWholeSubtree
	}
}

// ldapUsernameAttr — the attribute holding the username. Overleaf's default
// config leaves this to the directory's login attribute (passport-ldapauth
// default is "uid"); overridable via bindProperty.
func ldapUsernameAttr(p *LDAPProvider) string {
	if a := p.BindProperty; a != "" {
		return a
	}
	return "uid"
}

// ldapBuildSearchFilter — injection-safe (attr < escaped username >).
func ldapBuildSearchFilter(attr, username string) string {
	return "(" + attr + "=" + ldap.EscapeFilter(username) + ")"
}

// ldapConnectTimeout — passport connectTimeout / timeout (ms); default 10s.
// Node stores `timeout` as a STRING ("", "3000", ...) — the prod
// `sso-settings` doc carries "" — accept a numeric string and a number.
func ldapTimeout(p *LDAPProvider) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(p.Timeout))
	if err == nil && n > 0 {
		return time.Duration(n) * time.Millisecond
	}
	return 10 * time.Second
}

// ldapDial — dial (+ optional STARTTLS) with a connect timeout. Any dial or
// TLS failure is an auth failure (Node handleErrorsAsFailures parity).
func ldapDial(ctx context.Context, p *LDAPProvider) (*ldap.Conn, error) {
	_ = ctx
	conn, err := ldap.DialURL(p.URL, ldap.DialWithDialer(&net.Dialer{Timeout: ldapTimeout(p)}))
	if err != nil {
		return nil, errLdapAuth
	}
	if p.StartTLS {
		if serr := conn.StartTLS(&tls.Config{InsecureSkipVerify: false}); serr != nil {
			conn.Close()
			return nil, errLdapAuth
		}
	}
	return conn, nil
}

// ldapProbe — admin connectivity probe (Node _testLDAP): dial, then bind as
// bindDN (when set) to verify the service account. A successful bind proves
// the server + service-account credentials are usable.
func ldapProbe(p *LDAPProvider) error {
	conn, err := ldapDial(context.Background(), p)
	if err != nil {
		return err
	}
	defer conn.Close()
	if p.BindDN != "" {
		if berr := conn.Bind(p.BindDN, p.BindCredentials); berr != nil {
			return errLdapAuth
		}
	}
	return nil
}

// ldapAuthenticate — the passport-ldapauth core: dial → (bind service
// account) → search(<attr>=<escaped username>) → bind as the found entry DN
// with the supplied password → return the entry's attributes.
func ldapAuthenticate(ctx context.Context, p *LDAPProvider, username, password string) (ssoProfile, error) {
	if p.URL == "" {
		return nil, errors.New("ldap: url not configured")
	}
	if username == "" {
		return nil, errLdapAuth
	}
	conn, err := ldapDial(ctx, p)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	// 1) bind as the service account (when configured), else anonymous search.
	if p.BindDN != "" {
		if berr := conn.Bind(p.BindDN, p.BindCredentials); berr != nil {
			return nil, errLdapAuth
		}
	}
	// 2) locate the user entry.
	filter := ldapBuildSearchFilter(ldapUsernameAttr(p), username)
	req := ldap.NewSearchRequest(p.SearchBase, ldapScope(p.SearchScope), ldap.NeverDerefAliases, 0, 0, false, filter, nil, nil)
	sr, serr := conn.Search(req)
	if serr != nil {
		return nil, errLdapAuth
	}
	if len(sr.Entries) == 0 {
		return nil, errLdapAuth
	}
	entry := sr.Entries[0]
	// 3) verify the password by binding as the located entry.
	if berr := conn.Bind(entry.DN, password); berr != nil {
		return nil, errLdapAuth
	}
	profile := ssoProfile{}
	for _, at := range entry.Attributes {
		profile[at.Name] = at.Values
	}
	return profile, nil
}

// ---- find-or-create JIT (Node findOrCreateUser + P1c roles; mirrors samlJIT) ----

func ldapJIT(ctx context.Context, db *mongo.Database, p *LDAPProvider, profile ssoProfile, providerID, role string) (map[string]any, error) {
	email, firstName, lastName, isAdmin := ldapMapProfile(p, profile)
	if email == "" {
		return nil, errNoProfileMail
	}
	users := db.Collection("users")
	var u map[string]any
	if err := users.FindOne(ctx, bson.D{{Key: "emails.email", Value: email}}).Decode(&u); err != nil {
		// create (Node UserCreator defaults: holdingAccount:false, analyticsId).
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
		uid2, _ := ins.InsertedID.(string)
		u = map[string]any{"_id": uid2, "email": email}
		_, _ = users.UpdateOne(ctx, bson.D{{Key: "_id", Value: uid2}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "emails.0.confirmedAt", Value: time.Now().UTC()}}}})
	}
	// Node: userDetails = updateOnLogin ? {first,last} : {}; always sync
	// isAdmin when attAdmin+valAdmin set; always $inc loginEpoch + $unset
	// hashedPassword; Go adds the P1c ssoRoles stamp in the same update.
	details := bson.D{}
	if p.UpdateUserDetailsOnLogin && (firstName != "" || lastName != "") {
		details = append(details,
			bson.E{Key: "firstName", Value: firstName},
			bson.E{Key: "lastName", Value: lastName})
	}
	if p.IsAdminAtt != "" && p.ValAdmin != "" {
		details = append(details, bson.E{Key: "isAdmin", Value: isAdmin})
	}
	roleDetails := bson.D{
		{Key: "ssoRoles." + providerID, Value: bson.M{"role": role, "at": time.Now().UTC()}},
		{Key: "ssoLoginProviderId", Value: providerID},
	}
	all := append(details, roleDetails...)
	return loginEpochBump(ctx, users, u, true, all)
}

// ---- login route ----

// ldapLogin — POST /sso/ldap/login. Rate limits (Node): IP 20/60s + email
// 10/120s. Body {email, password} (usernameField 'email', passwordField
// 'password'). bind→map→role gate (blocked ⇒ audit + 401, before any write)
// → ldapJIT → finishSSOLogin.
func ldapLogin(a *core.App) func(*core.Cxt, *core.Res) {
	ipLim := core.NewRateLimiter(a.Redis, "login_ldap_ip", 20, 60)
	emailLim := core.NewRateLimiter(a.Redis, "login_ldap_email", 10, 120)
	return func(cxt *core.Cxt, res *core.Res) {
		ip := core.ClientIP(cxt.Req)
		if ipLim != nil && !ipLim.Consume(ip) {
			core.Send429(res, "Too many login attempts. Please try again later.")
			return
		}
		db, ok := ssoDBOr500(a, cxt, res)
		if !ok {
			return
		}
		cfg := loadSSOConfig(db, cxt)
		var p *LDAPProvider
		if cfg != nil {
			p = cfg.Ldap
		}
		if p == nil || !p.Enabled {
			res.JSON(404, []byte(`{"message":{"text":"LDAP login is not enabled","type":"error","status":404}}`))
			return
		}
		b, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		var body struct {
			Email    string `json:"email"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		_ = json.Unmarshal(b, &body)
		username := body.Email
		if username == "" {
			username = body.Username
		}
		if username == "" {
			res.JSON(400, []byte(`{"message":{"text":"Username and password are required","type":"error","status":400}}`))
			return
		}
		if emailLim != nil && !emailLim.Consume(strings.ToLower(username)) {
			core.Send429(res, "Too many login attempts. Please try again later.")
			return
		}
		profile, aerr := ldapAuthenticate(cxt.Req.Context(), p, username, body.Password)
		if aerr != nil {
			res.JSON(401, []byte(`{"message":{"text":"Could not authenticate","type":"error","status":401}}`))
			return
		}
		// P1c: role gate BEFORE any account write (blocked ⇒ refuse + audit).
		role := evaluateAttrFilter(p.AttrFilter, profile).role()
		if role == "blocked" {
			auditSsoDenied(a, cxt, "", "ldap", "ldap-attrFilter-blocked")
			res.JSON(401, []byte(`{"message":{"text":"Login denied by SSO role filter","type":"error","status":401}}`))
			return
		}
		user, jerr := ldapJIT(cxt.Req.Context(), db, p, profile, "ldap", role)
		if jerr != nil {
			if jerr == errParallelLogin {
				res.JSON(409, []byte(`{"message":{"text":"Another login for this account is in progress","type":"error","status":409}}`))
				return
			}
			res.JSON(500, []byte(`{"message":{"text":"LDAP user lookup failed","type":"error","status":500}}`))
			return
		}
		finishSSOLogin(a, cxt, res, user, "ldap", "ldap", nil, cxt.Req.URL.Query().Get("redir"))
	}
}

// Package registrationpage ports the CE registration-page module
// (services/web/modules/registration-page) for the Node→Go web cutover.
//
// Routes (module RegistrationPageRouter.mjs):
//
//	GET  /register  → ensureRegistrationEnabled → registrationPage
//	POST /register  → ensureRegistrationEnabled → rateLimit(5/60) → registerNewUser
//
// Contract pins (live Node e2e, 2026-09-14, P3.4):
//
//	GET (enabled)          → 200 text/html (React shell)
//	GET (disabled)         → 302 → disabledRedirectUrl (or /login)
//	POST anon no-csrf      → core csrf 403 "Forbidden"
//	POST disabled          → 403 text/plain "Registration is disabled on this site"
//	POST logged-in         → 302 → "/"
//	POST name bad          → 400 {"message":"Too long name."}
//	POST invalid email     → 400 {"message":"Invalid email address."}
//	POST disallowed domain → 403 {"message":"Registration is not available for this email domain."}
//	POST email registered  → 409 {"message":{"key":"account_with_this_email_exists"}}
//	POST happy             → 200 {"message":"Registration successful. Please check your email to activate your account."}
//	                         (+ user, 7-day 'password' token, "Activate your OlliTeX Account" mail)
//	POST 6th in 60s        → 429 "Rate limit reached, please try again later"
package registrationpage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

const (
	msgSuccess  = "Registration successful. Please check your email to activate your account."
	msgTooLong  = "Too long name."
	msgBadEmail = "Invalid email address."
	msgDomain   = "Registration is not available for this email domain."
	msgMailFail = "Failed to send the registration email. Please check the email address."
	msgDisabled = "Registration is disabled on this site"
	msg409      = `{"message":{"key":"account_with_this_email_exists"}}`
	oneWeekSec  = 7 * 24 * 60 * 60
)

var emailRe = regexp.MustCompile(`^([^<>()[\]\\.,;:\s@"]+(\.[^<>()[\]\\.,;:\s@"]+)*)@((\[[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\])|(([a-zA-Z\-0-9]+\.)+[a-zA-Z]{2,}))$`)

// Feature wires the registration routes (owns both /register routes).
func Feature(a *core.App) core.Feature {
	mail := core.NewMail()
	tok := core.NewOneTimeTokens(a.Mongo)
	lim := core.NewRateLimiter(a.Redis, "postRegister", 5, 60)
	return core.Feature{
		Name: "registrationpage",
		Routes: []core.Route{
			{Method: "GET", Path: "/register", NoLogin: true, Handler: getPage(a)},
			{Method: "POST", Path: "/register", NoLogin: true, Handler: postHandler(a, mail, tok, lim)},
		},
	}
}

// ---- GET /register ----

func getPage(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		sig := readSignup(a, cxt.Req.Context())
		if !sig.Enabled {
			target := sig.DisabledRedirectURL
			if target == "" {
				target = "/login"
			}
			res.Redirect(cxt.Req, 302, target)
			return
		}
		views.RegisterPage(res.W, pageData(cxt))
	}
}

func pageData(cxt *core.Cxt) views.PageData {
	d := views.PageData{Nonce: views.NewNonce(), Origin: cxt.SiteURL}
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
	}
	if email, uid := sessionUser(cxt); uid != "" {
		d.UserEmail = email
		d.UserID = uid
	}
	d.CSP = "script-src 'nonce-" + d.Nonce + "' 'unsafe-inline' 'strict-dynamic' https: 'report-sample'; object-src 'none'; base-uri 'none'"
	return d
}

func sessionUser(cxt *core.Cxt) (email, uid string) {
	if cxt.Sess == nil || !cxt.Sess.IsLoggedIn() {
		return "", ""
	}
	uid = cxt.Sess.UserIDHex()
	if raw, ok := cxt.Sess.GetRaw("passport"); ok {
		var p struct {
			User struct {
				Email string `json:"email"`
			} `json:"user"`
		}
		if json.Unmarshal(raw, &p) == nil {
			email = p.User.Email
		}
	}
	return email, uid
}

// ---- POST /register ----

func postHandler(a *core.App, mail *core.Mail, tok *core.OneTimeTokens, lim *core.RateLimiter) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		sig := readSignup(a, ctx)
		if !sig.Enabled {
			res.PlainText(403, msgDisabled)
			return
		}
		// rate limit (Node: clientId = getUserId(req) || req.ip — a logged-in
		// session is bucketed by user id, anonymous by IP; both consume before
		// the controller's logged-in guard returns 302).
		clientID := core.ClientIP(cxt.Req)
		if cxt.Sess != nil && cxt.Sess.IsLoggedIn() {
			clientID = cxt.Sess.UserIDHex()
		}
		if !lim.Consume(clientID) {
			core.Send429(res, "Rate limit reached, please try again later")
			return
		}
		if cxt.Sess != nil && cxt.Sess.IsLoggedIn() {
			res.Redirect(cxt.Req, 302, "/")
			return
		}
		raw := decodeToMap(cxt)
		fn, okFn := raw["first_name"].(string)
		ln, okLn := raw["last_name"].(string)
		if !okFn || !okLn || len(fn) > 100 || len(ln) > 100 {
			res.JSON(400, []byte(`{"message":"Too long name."}`))
			return
		}
		emailRaw, _ := raw["email"].(string)
		parsed := parseEmail(emailRaw)
		if parsed == "" {
			res.JSON(400, []byte(`{"message":"Invalid email address."}`))
			return
		}
		if !domainAllowed(sig.AllowedEmailDomains, parsed) {
			res.JSON(403, []byte(`{"message":"`+msgDomain+`"}`))
			return
		}
		switch doRegister(a, mail, tok, cxt, parsed, fn, ln) {
		case "success":
			res.JSON(200, []byte(`{"message":"`+msgSuccess+`"}`))
		case "taken":
			res.JSON(409, []byte(msg409))
		case "mailfail":
			res.JSON(422, []byte(`{"message":"`+msgMailFail+`"}`))
		default:
			res.PlainText(500, "Internal Server Error")
		}
	}
}

// doRegister — creates the user (or reuses a holding account), hashes the
// random password, mints the 7-day token and sends the activation mail.
func doRegister(a *core.App, mail *core.Mail, tok *core.OneTimeTokens, cxt *core.Cxt, email, first, last string) (out string) {
	defer func() {
		if out == "" {
			out = "error"
		}
	}()
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 12*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return "error"
	}
	coll := db.Collection("users")

	// existing? (Node getUserByAnyEmail: `email` or in `emails`)
	var ex bson.M
	err = coll.FindOne(ctx, bson.M{"$or": bson.A{
		bson.M{"email": email},
		bson.M{"emails.email": email},
	}}).Decode(&ex)
	if err != nil && err != mongo.ErrNoDocuments {
		return "error"
	}

	reuse := false
	reuseID := primitive.NilObjectID
	if err == nil {
		if holdingAccountFalse(ex) {
			return "taken"
		}
		// holding account (holdingAccount true) — Node reuses the existing user
		reuse = true
		if o, ok := ex["_id"].(primitive.ObjectID); ok {
			reuseID = o
		} else {
			return "error"
		}
	}

	return createAndMail(ctx, a, mail, tok, cxt, coll, reuse, reuseID, email, first, last)
}

func createAndMail(ctx context.Context, a *core.App, mail *core.Mail, tok *core.OneTimeTokens, cxt *core.Cxt, coll *mongo.Collection, reuse bool, reuseID primitive.ObjectID, email, first, last string) string {
	now := time.Now().UTC()
	pw := randomHex32()
	hash, herr := bcrypt.GenerateFromPassword([]byte(pw), bcryptRounds())
	if herr != nil {
		return "error"
	}
	id := reuseID
	if !reuse {
		id = primitive.NewObjectID()
	}

	doc := newUserDoc() // 42 static Node-parity defaults
	doc["_id"] = id
	doc["email"] = email
	doc["first_name"] = first
	doc["last_name"] = last
	doc["analyticsId"] = randomUUID()
	doc["hashedPassword"] = string(hash)
	doc["signUpDate"] = now
	doc["thirdPartyIdentifiers"] = bson.A{}
	doc["emails"] = bson.A{bson.M{
		"email":            email,
		"reversedHostname": reversedHostname(email),
		"createdAt":        now.Add(time.Millisecond),
		"_id":              primitive.NewObjectID(),
	}}

	if reuse {
		if _, uerr := coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
			"hashedPassword": string(hash),
			"first_name":     first,
			"last_name":      last,
		}}); uerr != nil {
			return "error"
		}
	} else {
		if _, ierr := coll.InsertOne(ctx, doc); ierr != nil {
			if mongo.IsDuplicateKeyError(ierr) {
				return "taken"
			}
			return "error"
		}
	}

	linkToken, terr := tok.NewWithExp(ctx, "password", bson.M{"user_id": id.Hex(), "email": email}, now.Add(oneWeekSec*time.Second))
	if terr != nil {
		return "error"
	}

	site := cxt.SiteURL
	if site == "" {
		site = "http://localhost"
	}
	link := site + "/user/activate?token=" + linkToken + "&user_id=" + id.Hex()
	subject := "Activate your " + appName() + " Account"
	text := "Hi,\n\n" +
		"Congratulations, you've just had an account created for you on " + appName() +
		" with the email address '" + email + "'.\n\n" +
		"Click here to set your password and log in:\n\n" +
		"Set password: " + link + "\n\n" +
		"If you have any questions or problems, please contact " + adminEmail() + "\n\n" +
		"Regards,\nThe " + appName() + " Team - " + site + "\n"
	html := `<p>Hi,</p>` +
		`<p>Congratulations, you've just had an account created for you on ` + appName() +
		` with the email address '` + email + `'.</p>` +
		`<p>Click here to set your password and log in:</p>` +
		`<p><a href="` + link + `">Set password</a></p>` +
		`<p>If you have any questions or problems, please contact ` + adminEmail() + `</p>` +
		`<p>Regards,<br/>The ` + appName() + ` Team - ` + site + `</p>`

	if mail != nil {
		if serr := mail.Send(email, subject, text, html); serr != nil {
			_, _ = coll.DeleteOne(ctx, bson.M{"_id": id})
			return "mailfail"
		}
	}
	return "success"
}

// ---- sign-up site-settings (stored section over env seeds; fail Open) ----

type signupCfg struct {
	Enabled             bool
	AllowedEmailDomains []string
	DisabledRedirectURL string
}

func readSignup(a *core.App, ctx context.Context) signupCfg {
	def := defaultSignup()
	if a.Mongo == nil {
		return def
	}
	d, err := a.Mongo.DB(ctx)
	if err != nil {
		return def // fail open
	}
	var doc struct {
		Signup struct {
			Enabled             *bool    `bson:"enabled"`
			AllowedEmailDomains *[]string `bson:"allowedEmailDomains"`
			DisabledRedirectURL *string  `bson:"disabledRedirectUrl"`
		} `bson:"signup"`
	}
	if err := d.Collection("site_settings").FindOne(ctx, bson.M{"_id": "global"}).Decode(&doc); err != nil {
		return def
	}
	s := def
	if doc.Signup.Enabled != nil {
		s.Enabled = *doc.Signup.Enabled
	}
	if doc.Signup.AllowedEmailDomains != nil {
		s.AllowedEmailDomains = *doc.Signup.AllowedEmailDomains
	}
	if doc.Signup.DisabledRedirectURL != nil {
		s.DisabledRedirectURL = *doc.Signup.DisabledRedirectURL
	}
	return s
}

func defaultSignup() signupCfg {
	s := signupCfg{Enabled: true, AllowedEmailDomains: []string{}}
	if v := os.Getenv("OVERLEAF_ENABLE_REGISTRATION_PAGE"); v != "" {
		s.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("OVERLEAF_ALLOWED_REGISTRATION_EMAIL_DOMAINS"); v != "" {
		s.AllowedEmailDomains = splitDomains(v)
	}
	if v := os.Getenv("OVERLEAF_REGISTRATION_DISABLED_REDIRECT"); v != "" {
		s.DisabledRedirectURL = v
	}
	return s
}

func splitDomains(v string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func holdingAccountFalse(doc bson.M) bool {
	h, ok := doc["holdingAccount"].(bool)
	return ok && !h
}

func parseEmail(email string) string {
	if email == "" || len(email) > 254 {
		return ""
	}
	s := strings.ToLower(strings.TrimSpace(email))
	if emailRe.MatchString(s) {
		return s
	}
	return ""
}

func domainAllowed(domains []string, email string) bool {
	if len(domains) == 0 {
		return true
	}
	i := strings.LastIndex(email, "@")
	if i < 0 || i+1 >= len(email) {
		return false
	}
	domain := email[i+1:]
	for _, pattern := range domains {
		if strings.HasPrefix(pattern, "*.") {
			if strings.HasSuffix(domain, "."+pattern[2:]) {
				return true
			}
			continue
		}
		if domain == pattern {
			return true
		}
	}
	return false
}

func reversedHostname(email string) string {
	i := strings.LastIndex(email, "@")
	hostname := email
	if i >= 0 && i+1 < len(email) {
		hostname = email[i+1:]
	}
	r := []rune(hostname)
	for a, b := 0, len(r)-1; a < b; a, b = a+1, b-1 {
		r[a], r[b] = r[b], r[a]
	}
	return string(r)
}

func randomHex32() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", 64)
	}
	return hex.EncodeToString(b)
}

func randomUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString
	return h(b[0:4]) + "-" + h(b[4:6]) + "-" + h(b[6:8]) + "-" + h(b[8:10]) + "-" + h(b[10:16])
}

func appName() string {
	if v := os.Getenv("OVERLEAF_APP_NAME"); v != "" {
		return v
	}
	if v := os.Getenv("OVERLEAF_APPNAME"); v != "" {
		return v
	}
	return "OlliTeX"
}

func adminEmail() string {
	if v := os.Getenv("OVERLEAF_ADMIN_EMAIL"); v != "" {
		return v
	}
	return "placeholder@example.com"
}

func bcryptRounds() int {
	if v := os.Getenv("BCRYPT_ROUNDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 4 {
			return n
		}
	}
	return 12
}

func decodeToMap(cxt *core.Cxt) map[string]any {
	b, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
	out := map[string]any{}
	if len(b) == 0 {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

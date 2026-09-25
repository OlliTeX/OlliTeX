package launchpad

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"golang.org/x/crypto/bcrypt"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/emailtemplates"
	"ollitex/go/services/web/features/templates"
	"ollitex/go/services/web/views"
)

// nowUTC / timeNowMillis — the two timestamp shapes Node writes:
// new Date() (stored BSONDate) and Date.now() (epoch ms int).
func nowUTC() time.Time    { return time.Now().UTC() }
func timeNowMillis() int64 { return time.Now().UnixMilli() }

// Feature wires the launchpad routes (all pinned — see package doc).
func Feature(a *core.App) core.Feature {
	mail := core.NewMail()
	return core.Feature{
		Name: "launchpad",
		Routes: []core.Route{
			{Method: "GET", Path: "/launchpad", NoLogin: true, Handler: hPage(a)},
			// Node login whitelist: anonymous reaches the controller
			// (it renders or redirects by itself).
			{Method: "POST", Path: "/launchpad/register_admin", NoLogin: true, Handler: hRegisterAdmin(a)},
			{Method: "POST", Path: "/launchpad/register_ldap_admin", NoLogin: true, Handler: hRegisterExternal(a, "ldap")},
			{Method: "POST", Path: "/launchpad/register_saml_admin", NoLogin: true, Handler: hRegisterExternal(a, "saml")},
			// NOT on the whitelist: anonymous bounces via the global
			// requireGlobalLogin gate (401 XHR / 302 /login) — same as Node.
			{Method: "POST", Path: "/launchpad/send_test_email", Handler: hSendTestEmail(a, mail)},
		},
	}
}

// ---- view plumbing ---------------------------------------------------------

func pageData(cxt *core.Cxt) views.PageData {
	d := views.PageData{Nonce: views.NewNonce()}
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
	}
	origin := cxt.SiteURL
	if origin == "" {
		origin = "http://" + cxt.Req.Host
	}
	d.Origin = origin
	d.UserEmail, d.UserID = core.PageUserSlots(cxt.Sess)
	return d
}

func render500(cxt *core.Cxt, res *core.Res) {
	views.Error500Page(res.W, pageData(cxt))
}

// ---- GET /launchpad ---------------------------------------------------------

// hPage — LaunchpadController.launchpadPage:
//
//	session user:  admin  → 200 admin page | else → 302 /restricted
//	anon:         adminExists → stash postLoginRedirect + 302 /login
//	              none        → 200 fresh (first-admin) page.
func hPage(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if cxt.Sess != nil && cxt.Sess.IsLoggedIn() {
			if templates.SessionIsAdmin(cxt.Sess) {
				views.LaunchpadAdminPage(res.W, pageData(cxt))
				return
			}
			res.Redirect(cxt.Req, 302, "/restricted")
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
		defer cancel()
		if a.Mongo == nil {
			res.SendStatus(500)
			return
		}
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.SendStatus(500)
			return
		}
		exists, err := adminExists(ctx, db)
		if err != nil {
			res.SendStatus(500)
			return
		}
		if !exists {
			views.LaunchpadFreshPage(res.W, pageData(cxt))
			return
		}
		// Node: AuthenticationController.setRedirectInSession(req) then
		// redirect('/login').
		if cxt.Sess != nil {
			cxt.Sess.Set("postLoginRedirect", "/launchpad")
		}
		res.Redirect(cxt.Req, 302, "/login")
	}
}

// ---- JSON body decode --------------------------------------------------------

// decodePair — JSON {email?, password?} (the async-form helper submits
// application/json — pinned on the launchpad page bundle).
func decodePair(cxt *core.Cxt) (email, password string, ok bool) {
	raw, err := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
	if err != nil {
		return "", "", false
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if json.Unmarshal(raw, &in) != nil {
		return "", "", false
	}
	return in.Email, in.Password, true
}

func jsonBody(res *core.Res, code int, obj string) {
	res.JSON(code, []byte(obj))
}

func objID(doc bson.M) (primitive.ObjectID, bool) {
	oid, ok := doc["_id"].(primitive.ObjectID)
	return oid, ok
}

// ---- POST /launchpad/register_admin ----------------------------------------

// hRegisterAdmin — Node registerAdmin (exact gate order, pinned):
//
//	1 email+password both present    → else 400 "Bad Request"
//	2 no admin exists                → else 403 {"message":{"type":"error",
//	                                               "text":"admin user already exists"}}
//	3 valid email                    → else 400 {"message":{"type":"error",
//	                                               "text":"email not valid"}}
//	4 valid password                 → else 400 {"message":{"type":"error","text":<rule>}}
//	5 no existing non-holding user   → else 500 view (EmailAlreadyRegistered
//	                                               — Node throws → error middleware)
//	6 create (+promote)              → 200 {"redir":"/launchpad"}
func hRegisterAdmin(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		email, password, ok := decodePair(cxt)
		if !ok || email == "" || password == "" {
			res.SendStatus(400)
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 12*time.Second)
		defer cancel()
		if a.Mongo == nil {
			res.SendStatus(500)
			return
		}
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.SendStatus(500)
			return
		}
		if exists, _ := adminExists(ctx, db); exists {
			jsonBody(res, 403, `{"message":{"type":"error","text":"admin user already exists"}}`)
			return
		}
		if msg := validateEmail(email); msg != "" {
			jsonBody(res, 400, `{"message":{"type":"error","text":"`+msg+`"}}`)
			return
		}
		if msg := validatePassword(password, email); msg != "" {
			jsonBody(res, 400, `{"message":{"type":"error","text":"`+msg+`"}}`)
			return
		}
		email = parseEmail(email) // Node: registerNewUser re-parses (trim+lower)
		if cerr := createLocalAdminOrReuse(ctx, db, email, password, randomUUID()); cerr != nil {
			if errors.Is(cerr, errEmailAlreadyRegistered) {
				render500(cxt, res)
				return
			}
			res.SendStatus(500)
			return
		}
		jsonBody(res, 200, `{"redir":"/launchpad"}`)
	}
}

// ---- POST /launchpad/register_ldap_admin + register_saml_admin --------------

// hRegisterExternal — Node registerExternalAuthAdmin(authMethod) (pinned):
//
//	1 authMethod == current method   → else 403 "Forbidden"
//	2 email present                  → else 400 "Bad Request"
//	3 no admin exists                → else 403 "Forbidden"
//	4 valid email (registerNewUser)  → else 500 view ('request is not valid')
//	5 create external admin (no hashedPassword, confirmedAt=Date.now() ms,
//	  first_name=full email, last_name="" — the controller's userDetails)
//	6 stash postLoginRedirect='/launchpad'
//	7 → 200 {"redir":"/launchpad","email":<email>}
func hRegisterExternal(a *core.App, method string) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if authMethod() != method {
			res.SendStatus(403)
			return
		}
		raw, rerr := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		if rerr != nil {
			res.SendStatus(400)
			return
		}
		var in struct {
			Email string `json:"email"`
		}
		if json.Unmarshal(raw, &in) != nil {
			res.SendStatus(400)
			return
		}
		if in.Email == "" {
			res.SendStatus(400)
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 12*time.Second)
		defer cancel()
		if a.Mongo == nil {
			res.SendStatus(500)
			return
		}
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.SendStatus(500)
			return
		}
		if exists, _ := adminExists(ctx, db); exists {
			res.SendStatus(403)
			return
		}
		email := parseEmail(in.Email)
		if email == "" {
			// Node: registerNewUser fails _registrationRequestIsValid
			// (validateEmail) → throws 'request is not valid' → 500 view.
			render500(cxt, res)
			return
		}
		existing, _ := userByEmail(ctx, db, email)
		if existing != nil {
			if holdingAccountFalse(existing) {
				render500(cxt, res)
				return
			}
			// Node reuse path: the existing user is reused; the controller's
			// updateOne applies {isAdmin, emails} (+ $unset hashedPassword),
			// registerNewUser sets holdingAccount:false + a random password
			// which is again $unsetting — final shape: NO hashedPassword,
			// first_name/last_name UNCHANGED (node does not rewrite them on
			// reuse — only fresh creation stores the controller's values).
			oid, okO := objID(existing)
			if !okO {
				res.SendStatus(500)
				return
			}
			set := bson.M{
				"holdingAccount": false,
				"isAdmin":        true,
				"emails": bson.A{bson.M{
					"email":            email,
					"reversedHostname": reversedHostname(email),
					"confirmedAt":      timeNowMillis(),
					"_id":              primitive.NewObjectID(),
				}},
			}
			filt := bson.M{"_id": oid}
			if _, uerr := db.Collection("users").UpdateOne(ctx, filt, bson.M{
				"$set":   set,
				"$unset": bson.M{"hashedPassword": ""},
			}); uerr != nil {
				res.SendStatus(500)
				return
			}
		} else {
			if cerr := createExternalAdminUser(ctx, db, email, randomUUID()); cerr != nil {
				res.SendStatus(500)
				return
			}
		}
		if cxt.Sess != nil {
			cxt.Sess.Set("postLoginRedirect", "/launchpad")
		}
		jsonBody(res, 200, `{"redir":"/launchpad","email":`+jsonQuote(email)+`}`)
	}
}

// ---- POST /launchpad/send_test_email ----------------------------------------

// hSendTestEmail — Node: ensureUserIsSiteAdmin middleware then sendTestEmail
// (pinned):
//
//	not site admin → 302 /restricted?from=%2Flaunchpad%2Fsend_test_email
//	no email       → 400 {"message":"no email address supplied"}
//	sent           → 200 {"message":"Email Sent"}  (translate('email_sent'))
//	mail error     → 500 view (Node: throw → error middleware)
func hSendTestEmail(a *core.App, mail *core.Mail) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if cxt.Sess == nil || !templates.SessionIsAdmin(cxt.Sess) {
			// AuthorizationMiddleware._redirectToRestricted:
			// res.redirect('/restricted?from='+encodeURIComponent(currentUrl))
			res.Redirect(cxt.Req, 302, "/restricted?from=%2Flaunchpad%2Fsend_test_email")
			return
		}
		raw, err := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		if err != nil {
			res.SendStatus(400)
			return
		}
		var in struct {
			Email string `json:"email"`
		}
		_ = json.Unmarshal(raw, &in)
		if in.Email == "" {
			jsonBody(res, 400, `{"message":"no email address supplied"}`)
			return
		}
		appName := "OlliTeX"
		if v := os.Getenv("OVERLEAF_APP_NAME"); v != "" {
			appName = v
		} else if v := os.Getenv("OVERLEAF_APPNAME"); v != "" {
			appName = v
		}
		site := cxt.SiteURL
		if site == "" {
			site = "http://" + cxt.Req.Host
		}
		// the /hub-managed template ("test-mail"); owner-rebranded default is
		// byte-identical to the pre-move inline strings at app=appName.
		tmpl, tmErr := emailtemplates.RenderFor(a, "test-mail", map[string]string{"app": appName, "site": site})
		if tmErr != nil {
			render500(cxt, res)
			return
		}
		if mail != nil {
			if serr := mail.Send(in.Email, tmpl.Subject, tmpl.Text, tmpl.HTML); serr != nil {
				render500(cxt, res)
				return
			}
		}
		jsonBody(res, 200, `{"message":"Email Sent"}`)
	}
}

// ---- local-admin creation with Node reuse semantics --------------------------

func holdingAccountFalse(doc bson.M) bool {
	h, ok := doc["holdingAccount"].(bool)
	return ok && !h
}

// createLocalAdminOrReuse — Node registerNewUser + the launchpad promote,
// in one of two shapes:
//
//	fresh email: insert the final document (baseline + fields + isAdmin).
//	existing holding account (holdingAccount:true): Node REUSES the user
//	  (_createNewUserIfRequired returns it) then $set holdingAccount:false,
//	  sets the password, and the controller $set {isAdmin, emails}.
//	existing non-holding user: Node throws EmailAlreadyRegistered (500).
func createLocalAdminOrReuse(ctx context.Context, db *mongo.Database, email, password, analyticsID string) error {
	existing, _ := userByEmail(ctx, db, email)
	if existing != nil {
		if holdingAccountFalse(existing) {
			return errEmailAlreadyRegistered
		}
		oid, okO := objID(existing)
		if !okO {
			return errors.New("launchpad: no user _id")
		}
		hash, herr := bcrypt.GenerateFromPassword([]byte(password), bcryptRounds())
		if herr != nil {
			return herr
		}
		set := bson.M{
			"holdingAccount": false,
			"hashedPassword": string(hash),
			"isAdmin":        true,
			"emails": bson.A{bson.M{
				"email":            email,
				"reversedHostname": reversedHostname(email),
				"createdAt":        nowUTC(),
				"_id":              primitive.NewObjectID(),
			}},
		}
		if _, uerr := db.Collection("users").UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": set}); uerr != nil {
			return uerr
		}
		return nil
	}
	return createLocalAdminUser(ctx, db, email, password, analyticsID)
}

// jsonQuote — minimal JSON string quoting for the registered email echo.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

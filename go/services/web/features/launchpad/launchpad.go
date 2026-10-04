package launchpad

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"golang.org/x/crypto/bcrypt"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/emailtemplates"
	"ollitex/go/services/web/features/templates"
)

// nowUTC / timeNowMillis — the two timestamp shapes Node writes:
func nowUTC() time.Time     { return time.Now().UTC() }
func timeNowMillis() int64  { return time.Now().UnixMilli() }

// Feature wires the launchpad routes (owner decision 2026-10-05: the
// /launchpad PAGE and its register_* bootstrap endpoints are RETIRED — the
// first-admin function moved to the toolkit CLI `toolkit bootstrap`; the
// Node-parity creation code below stays in this package as that toolkit's
// engine + the p620 parity pin). Only the session test-email route remains
// registered; /launchpad* now falls through to the generic 404.
func Feature(a *core.App) core.Feature {
	mail := core.NewMail()
	return core.Feature{
		Name: "launchpad",
		Routes: []core.Route{
			// (retired rows: GET /launchpad, POST /launchpad/register_admin,
			// register_ldap_admin, register_saml_admin → 404 now.)
			// NOT on the whitelist: anonymous bounces via the global
			// requireGlobalLogin gate (401 XHR / 302 /login) — same as Node.
			{Method: "POST", Path: "/launchpad/send_test_email", Handler: hSendTestEmail(a, mail)},
		},
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

// render500 — Node: express error middleware → the generic 500 VIEW (the
// launchpad page is gone, but a send_test_email failure still renders the
// plain 500 page — the core 500 renderer with the page nonce/origin the
// other leaves use; fallback to a bare 500 when the renderer is absent).
func render500(a *core.App, cxt *core.Cxt, res *core.Res) {
	if a != nil && a.Render500 != nil {
		a.Render500(cxt, res)
		return
	}
	res.SendStatus(500)
}

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
			render500(a, cxt, res)
			return
		}
		if mail != nil {
			if serr := mail.Send(in.Email, tmpl.Subject, tmpl.Text, tmpl.HTML); serr != nil {
				render500(a, cxt, res)
				return
			}
		}
		jsonBody(res, 200, `{"message":"Email Sent"}`)
	}
}
func jsonBody(res *core.Res, code int, obj string) {
	res.JSON(code, []byte(obj))
}
func objID(doc bson.M) (bson.ObjectID, bool) {
	oid, ok := doc["_id"].(bson.ObjectID)
	return oid, ok
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
				"_id":              bson.NewObjectID(),
			}},
		}
		if _, uerr := db.Collection("users").UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": set}); uerr != nil {
			return uerr
		}
		return nil
	}
	return createLocalAdminUser(ctx, db, email, password, analyticsID)
}
// ---- local-admin creation with Node reuse semantics --------------------------

func holdingAccountFalse(doc bson.M) bool {
	h, ok := doc["holdingAccount"].(bool)
	return ok && !h
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
// jsonQuote — minimal JSON string quoting for the registered email echo.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

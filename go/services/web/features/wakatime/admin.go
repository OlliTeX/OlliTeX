// G (owner 2026-10-09): the WakaTime ADMIN SURFACE (psintern admin-settings).
//
// Endpoints (requireLogin + site admin; settings themselves persist via
// the standard sitesettings API as the `wakatime` section {enabled,
// serverUrl}):
//
//	POST /admin/wakatime/check      {serverUrl?}
//	      → {ok, status, message} — a live GET {base}/ so the admin gets a
//	        real reachability verdict.
//	POST /admin/wakatime/provision {email}
//	      → {ok, message} — provision (or re-key) the wakapi account for
//	        the named OlliTeX user: username = the email (owner decision:
//	        "auto-create wakapi users from OlliTeX users using the email
//	        address as username"), server-generated password, key stored
//	        encrypted in wakaTimeUserCredentials — after which the user's
//	        heartbeats relay with zero further user action.
//
// Resolution of the wakapi server (check/provision/auto-link):
//	1. the request body (check only) / site_settings.global.wakatime
//	   .serverUrl  →  2. env WAKATIME_SERVER_URL  →  3. {site origin}/wakapi
//	   (the toolkit haproxy edge route — the default the admin section
//	    ships with).
package wakatime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"ollitex/go/services/web/core"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var emailPat = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)

func (s *svc) wakaAdminGate(cxt *core.Cxt, res *core.Res) bool {
	if !s.enabled(cxt.Req.Context()) {
		res.JSON(http.StatusNotFound, []byte(
			`{"message":"WakaTime integration is disabled on this instance"}`))
		return false
	}
	return cxt.A.RequireSiteAdmin(cxt, res)
}

// siteWakatimeServerURL — site_settings.global.wakatime.serverUrl ("").
func siteWakatimeServerURL(ctx context.Context, a *core.App) string {
	if a == nil || a.Mongo == nil {
		return ""
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil || db == nil {
		return ""
	}
	var doc struct {
		Wakatime struct {
			ServerURL string `bson:"serverUrl"`
		} `bson:"wakatime"`
	}
	if db.Collection("site_settings").FindOne(ctx,
		bson.D{{Key: "_id", Value: "global"}}).Decode(&doc) != nil {
		return ""
	}
	return doc.Wakatime.ServerURL
}

// resolveServerBase — the instance's wakapi base URL.
func (s *svc) resolveServerBase(ctx context.Context, cxt *core.Cxt, explicit string) (string, error) {
	b := strings.TrimSpace(explicit)
	if b == "" {
		b = strings.TrimSpace(siteWakatimeServerURL(ctx, s.a))
	}
	if b == "" {
		b = strings.TrimSpace(os.Getenv("WAKATIME_SERVER_URL"))
	}
	if b == "" && cxt != nil {
		if u, uerr := url.Parse(cxt.SiteURL); uerr == nil && u.Host != "" {
			b = strings.TrimSuffix(u.Scheme+"://"+u.Host, "/") + "/wakapi"
		}
	}
	return normBase(b)
}

func wakaJSON(res *core.Res, code int, obj map[string]any) {
	b, _ := json.Marshal(obj)
	res.JSON(code, b)
}

// adminCheck — POST /admin/wakatime/check.
func (s *svc) adminCheck(cxt *core.Cxt, res *core.Res) {
	if !s.wakaAdminGate(cxt, res) {
		return
	}
	var body struct {
		ServerURL string `json:"serverUrl"`
	}
	if raw, rerr := readBody(cxt.Req.Body); rerr == nil {
		_ = json.Unmarshal(raw, &body)
	}
	base, err := s.resolveServerBase(cxt.Req.Context(), cxt, body.ServerURL)
	if err != nil {
		wakaJSON(res, http.StatusOK, map[string]any{
			"ok": false, "status": 0, "message": err.Error(),
		})
		return
	}
	resp, gerr := provisionClient.Get(base + "/")
	if gerr != nil {
		wakaJSON(res, http.StatusOK, map[string]any{
			"ok": false, "status": 0, "message": "reachability check failed: " + gerr.Error(),
		})
		return
	}
	rb, _ := readAll(resp.Body, 4096)
	resp.Body.Close()
	_ = string(rb)
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	wakaJSON(res, http.StatusOK, map[string]any{
		"ok":     ok,
		"status": resp.StatusCode,
		"message": func() string {
			if ok {
				return "wakapi is reachable at " + base
			}
			return "wakapi answered HTTP " + itoa(int64(resp.StatusCode))
		}(),
	})
}

// adminProvision — POST /admin/wakatime/provision {email}.
func (s *svc) adminProvision(cxt *core.Cxt, res *core.Res) {
	if !s.wakaAdminGate(cxt, res) {
		return
	}
	ctx := cxt.Req.Context()
	var body struct {
		Email string `json:"email"`
	}
	if raw, rerr := readBody(cxt.Req.Body); rerr == nil {
		_ = json.Unmarshal(raw, &body)
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if !emailPat.MatchString(email) {
		wakaJSON(res, http.StatusBadRequest, map[string]any{
			"ok": false, "message": "a valid user email is required",
		})
		return
	}
	uid, uerr := s.findUserByEmail(ctx, email)
	if uerr != nil || uid == "" {
		wakaJSON(res, http.StatusNotFound, map[string]any{
			"ok": false, "message": "no OlliTeX user with that email",
		})
		return
	}
	base, berr := s.resolveServerBase(ctx, cxt, "")
	if berr != nil {
		wakaJSON(res, http.StatusOK, map[string]any{
			"ok": false, "message": berr.Error(),
		})
		return
	}
	local := strings.SplitN(email, "@", 2)[0]
	pr, perr := provisionAccount(ctx, ProvisionRequest{
		ServerBase:     base,
		Username:       local,
		Email:          email,
		InstanceSecret: s.encryptorSecret(),
	})
	if perr != nil {
		wakaJSON(res, http.StatusOK, map[string]any{
			"ok": false, "message": perr.Error(),
		})
		return
	}
	if serr := s.storeCreds(ctx, uid, apiBaseFor(base), pr.Key); serr != nil {
		wakaJSON(res, http.StatusInternalServerError, map[string]any{
			"ok": false, "message": "provisioned, but storing the key failed: " + serr.Error(),
		})
		return
	}
	wakaJSON(res, http.StatusOK, map[string]any{
		"ok":      true,
		"username": local,
		"message": "wakapi account provisioned and the API key is stored for this user",
	})
}

// findUserByEmail — the `users` collection by email ("" when absent).
func (s *svc) findUserByEmail(ctx context.Context, email string) (string, error) {
	if s.a == nil || s.a.Mongo == nil {
		return "", nil
	}
	db, err := s.a.Mongo.DB(ctx)
	if err != nil || db == nil {
		return "", nil
	}
	var doc struct {
		ID bson.ObjectID `bson:"_id"`
	}
	if db.Collection("users").FindOne(ctx, bson.D{{Key: "email", Value: email}}).Decode(&doc) != nil {
		return "", nil
	}
	return doc.ID.Hex(), nil
}

// emailForUID — the session user's canonical email ("users" by _id).
func (s *svc) emailForUID(ctx context.Context, uid string) (string, error) {
	if s.a == nil || s.a.Mongo == nil {
		return "", nil
	}
	db, err := s.a.Mongo.DB(ctx)
	if err != nil || db == nil {
		return "", nil
	}
	oid, oerr := bson.ObjectIDFromHex(uid)
	if oerr != nil {
		return "", nil
	}
	var doc struct {
		Email string `bson:"email"`
	}
	if db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&doc) != nil {
		return "", nil
	}
	return strings.ToLower(strings.TrimSpace(doc.Email)), nil
}

// jsonq — JSON-quote a string for inline bodies (stable house shape).
func jsonq(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

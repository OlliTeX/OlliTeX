package authpages

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"ollitex/go/services/web/core"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"
)

// ---- POST /login (password) ----
//
// Success (acceptsJson): 200 {"redir":<postLoginRedirect||"/project">}
// Success (form):          302 Location:<same>
// In both: Set-Cookie = REGENERATED sid (doc: cookie, justLoggedIn:true,
// passport.user lightUser, analyticsId, csrfSecret), old doc DELed.
// Pinned failures:
//   unknown user / bad password → 401 {"message":{"type":"error",
//     "key":"invalid-password-retry-or-reset"}} (+ rolling cookie;
//     lastFailedLogin written when user exists)
//   malformed email             → 401 {"message":{"message":"This SSO login
//     option is not enabled.","type":"error","key":"invalid-password-retry-
//     or-reset"}}
//   loginEpoch mismatch         → 429 {"message":{}}
//   suspended (password ok)     → /account-suspended redirect, no login

// debugAuth — temporary diagnostics (WEB_GO_DEBUG_AUTH=1).
var debugAuth = os.Getenv("WEB_GO_DEBUG_AUTH") == "1"

func keysOf(m map[string]any) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

type userDoc struct {
	ID             any      `bson:"_id"`
	Email          string   `bson:"email"`
	FirstName      string   `bson:"first_name"`
	LastName       string   `bson:"last_name"`
	HashedPassword string   `bson:"hashedPassword"`
	Suspended      bool     `bson:"suspended"`
	LoginEpoch     *int64   `bson:"loginEpoch"`
	AnalyticsID    string   `bson:"analyticsId"`
	ReferalID      *string  `bson:"referal_id"`
	MustReconfirm  *bool    `bson:"must_reconfirm"`
	LabsProgram    *bool    `bson:"labsProgram"`
	ExternalAuth   *bool    `bson:"externalAuth"`
	Admin          *bool    `bson:"admin"`
	AdminRoles     []string `bson:"adminRoles"`
	OverleafID     *string  `bson:"overleaf.id"`
	AlphaProgram   *bool    `bson:"alphaProgram"`
	BetaProgram    *bool    `bson:"betaProgram"`
}

func postLogin(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := decodeBody(cxt, &in); err != nil {
			res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
			res.W.WriteHeader(400)
			return
		}
		email := strings.ToLower(strings.TrimSpace(in.Email))
		if !validEmail(email) {
			sendLogin(res, 401, bodyInvalidEmailNested)
			return
		}

		if cxt.Sess == nil {
			res.SendStatus(500)
			return
		}
		old := cxt.Sess // the pre-login session (carries postLoginRedirect)

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
		u := userDoc{}
		err = db.Collection("users").FindOne(ctx, bson.D{{Key: "email", Value: email}}).Decode(&u)
		match := false
		if err == nil && u.HashedPassword != "" {
			match = bcrypt.CompareHashAndPassword([]byte(u.HashedPassword), []byte(in.Password)) == nil
		}
		// read loginEpoch with its NATIVE bson type (int32 vs int64 matters
		// for the equality filter — P1 pin: a type-coerced filter 429'd).
		var loginEpochRaw any
		if err == nil {
			epochHolder := struct {
				LoginEpoch any `bson:"loginEpoch"`
			}{}
			if e2 := db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: u.ID}}).Decode(&epochHolder); e2 == nil && epochHolder.LoginEpoch != nil {
				loginEpochRaw = epochHolder.LoginEpoch
			}
		}
		if err == nil && !match {
			now := time.Now().UTC()
			_, _ = db.Collection("users").UpdateOne(ctx,
				bson.D{{Key: "_id", Value: u.ID}},
				bson.D{{Key: "$set", Value: bson.D{{Key: "lastFailedLogin", Value: nowISO(now)}}}})
			sendLogin(res, 401, bodyInvalidPassword)
			return
		}
		if err != nil {
			sendLogin(res, 401, bodyInvalidPassword)
			return
		}

		// finishLogin branches
		if u.Suspended {
			redirOrJSON(cxt, res, "/account-suspended")
			return
		}
		// optimistic lock (Node ParallelLoginError → 429 {"message":{}})
		filter := bson.D{{Key: "_id", Value: u.ID}}
		if loginEpochRaw != nil {
			filter = append(filter, bson.E{Key: "loginEpoch", Value: loginEpochRaw})
		}
		ur, perr := db.Collection("users").UpdateOne(ctx, filter,
			bson.D{{Key: "$inc", Value: bson.D{{Key: "loginEpoch", Value: 1}}}})
		if debugAuth {
			log.Printf("webgo-auth: updateOne filter=%v ur=%+v err=%v", filter, ur, perr)
		}
		if debugAuth {
			rawU := map[string]any{}
			_ = db.Collection("users").FindOne(ctx, bson.D{{Key: "email", Value: email}}).Decode(&rawU)
			log.Printf("webgo-auth: user doc keys=%v epochRaw=%v", keysOf(rawU), rawU["loginEpoch"])
		}
		if perr != nil {
			res.SendStatus(500)
			return
		}
		if ur == nil || ur.ModifiedCount != 1 {
			res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
			res.W.WriteHeader(429)
			_, _ = io.WriteString(res.W, `{"message":{}}`)
			return
		}

		// ---- session regeneration (the P2 pin) ----
		created := a.Store.Fresh() // brand-new sid, unsaved
		aid := u.AnalyticsID
		if aid == "" {
			aid = idString(u.ID)
		}
		created.Set("justLoggedIn", true)
		created.Set("analyticsId", aid)
		created.Set("passport", json.RawMessage(`{"user":`+lightUserJSON(&u, aid, cxt)+`}`))
		_ = created.CsrfSecret()        // allocate (the login POST consumed a token)
		_ = a.Store.Destroy(old.SessID) // Node: old session doc destroyed
		cxt.Sess = created              // pipeline persists (XX — no-op) later
		// commit NOW (headers still open): the regenerated sid must ride
		// this response (P2 pin) — a post-handler pass would be too late.
		if debugAuth {
			log.Printf("webgo-auth: old=%s created=%s rw-writes=%d", old.SessID, created.SessID, 0)
		}
		a.CommitSess(created, res.W)

		// Node AuthenticationController post-login hook:
		// UserSessionsManager.trackSession(user, req.sessionID) → SADD the
		// session doc key into UserSessions:<uid> + PEXPIRE (5-day set pin).
		a.TrackSession(idString(u.ID), created.SessID)

		target := "/project"
		if raw, ok := old.GetRaw("postLoginRedirect"); ok && len(raw) >= 2 {
			var s string
			if json.Unmarshal(raw, &s) == nil && s != "" {
				target = s
			}
		}
		redirOrJSON(cxt, res, target)
	}
}

func redirOrJSON(cxt *core.Cxt, res *core.Res, target string) {
	if core.AcceptsJSON(cxt.Req) {
		res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
		res.W.WriteHeader(200)
		b, _ := json.Marshal(target)
		_, _ = fmt.Fprintf(res.W, `{"redir":%s}`, string(b))
		return
	}
	res.Redirect(cxt.Req, 302, target)
}

func sendLogin(res *core.Res, code int, body string) {
	res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
	res.W.WriteHeader(code)
	_, _ = io.WriteString(res.W, body)
}

// lightUserJSON renders passport.user in the EXACT Node serializeUser key
// order (P2 pin from the live login doc): undefined fields omitted,
// present falsy values kept.

// lightUserJSON renders passport.user in the EXACT Node serializeUser key
// order (P2 pin from the live login doc): undefined fields omitted,
// present falsy values kept.
func idString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if o, ok := v.(primitive.ObjectID); ok {
		return o.Hex()
	}
	// fallback: hex-ish string form
	if s2, err2 := bson.MarshalExtJSON(v, false, true); err2 == nil {
		return string(s2)
	}
	return fmt.Sprint(v)
}

func lightUserJSON(u *userDoc, analyticsID string, cxt *core.Cxt) string {
	var b strings.Builder
	w := func(kv string) { b.WriteString(kv) }
	w(`"_id":` + jstr(idString(u.ID)))
	w(`,"first_name":` + jstr(u.FirstName))
	w(`,"last_name":` + jstr(u.LastName))
	w(`,"email":` + jstr(u.Email))
	if u.ReferalID != nil && *u.ReferalID != "" {
		w(`,"referal_id":` + jstr(*u.ReferalID))
	}
	w(`,"session_created":` + jstr(nowISO(time.Now().UTC())))
	w(`,"ip_address":` + jstr(clientIP(cxt)))
	if u.MustReconfirm != nil {
		w(`,"must_reconfirm":` + fmt.Sprint(*u.MustReconfirm))
	}
	if u.OverleafID != nil && *u.OverleafID != "" {
		w(`,"v1_id":` + jstr(*u.OverleafID))
	}
	w(`,"analyticsId":` + jstr(analyticsID))
	if u.AlphaProgram != nil {
		w(`,"alphaProgram":` + fmt.Sprint(*u.AlphaProgram))
	}
	if u.BetaProgram != nil {
		w(`,"betaProgram":` + fmt.Sprint(*u.BetaProgram))
	}
	lp := false
	if u.LabsProgram != nil {
		lp = *u.LabsProgram
	}
	w(`,"labsProgram":` + fmt.Sprint(lp))
	ea := false
	if u.ExternalAuth != nil {
		ea = *u.ExternalAuth
	}
	w(`,"externalAuth":` + fmt.Sprint(ea))
	if u.Admin != nil && *u.Admin {
		w(`,"isAdmin":true`)
		roles := "[]"
		if u.AdminRoles != nil {
			rb, _ := json.Marshal(u.AdminRoles)
			roles = string(rb)
		}
		w(`,"adminRoles":` + roles)
	}
	return "{" + b.String() + "}"
}

func clientIP(cxt *core.Cxt) string {
	fwd := cxt.Req.Header.Get("X-Forwarded-For")
	if fwd != "" {
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	host := cxt.Req.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	return host
}

func nowISO(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000") + "Z"
}

func jstr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// validEmail is the EmailHelper.parseEmail bar (P1 pin: junk → the nested
// 401; anything Node would parse → normal 401 flow).

// validEmail is the EmailHelper.parseEmail bar (P1 pin: junk → the nested
// 401; anything Node would parse → normal 401 flow).
func validEmail(s string) bool {
	if len(s) < 6 || len(s) > 254 {
		return false
	}
	i := strings.LastIndex(s, "@")
	if i < 1 || i == len(s)-1 {
		return false
	}
	if !strings.Contains(s[:i], ".") && !strings.ContainsAny(s[:i], "a-zA-Z") {
		return false
	}
	dom := s[i+1:]
	if dot := strings.LastIndex(dom, "."); dot < 2 || dot == len(dom)-1 {
		return false
	}
	local := s[:i]
	for _, c := range local {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.ContainsRune(".+-_%", c):
		default:
			return false
		}
	}
	for _, c := range dom {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.ContainsRune(".-", c):
		default:
			return false
		}
	}
	return true
}

// ---- body decoding (JSON preferred; urlencoded tolerated) ----

// ---- body decoding (JSON preferred; urlencoded tolerated) ----

func decodeBody(cxt *core.Cxt, v any) error {
	ct := strings.ToLower(cxt.Req.Header.Get("Content-Type"))
	b, err := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
	if err != nil {
		return err
	}
	if strings.Contains(ct, "application/json") || strings.Contains(ct, "application/") {
		return json.Unmarshal(b, v)
	}
	if strings.Contains(ct, "x-www-form-urlencoded") {
		vals, perr := urlParseForm(string(b))
		if perr == nil {
			rv := formFiller(v)
			if rv != nil {
				rv(vals)
				return nil
			}
		}
	}
	return json.Unmarshal(b, v)
}

// formFiller maps urlencoded values onto the struct's `json` names.

// formFiller maps urlencoded values onto the struct's `json` names.
func formFiller(v any) func(map[string][]string) {
	return func(vals map[string][]string) {
		set := []struct {
			key   string
			apply string `json:"-"`
		}{}
		_ = set
		// minimal: the two known forms
		switch m := v.(type) {
		case *struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}:
			if e, ok := vals["email"]; ok && len(e) > 0 {
				m.Email = e[0]
			}
			if p, ok := vals["password"]; ok && len(p) > 0 {
				m.Password = p[0]
			}
		case *struct {
			Redirect string `json:"redirect"`
		}:
			if r, ok := vals["redirect"]; ok && len(r) > 0 {
				m.Redirect = r[0]
			}
		}
	}
}

// originOfReq = "http(s)://host" of the current request.

// originOfReq = "http(s)://host" of the current request.
func originOfReq(cxt *core.Cxt) string {
	scheme := "http"
	if cxt.Req.TLS != nil || strings.EqualFold(cxt.Req.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := cxt.Req.Header.Get("Host")
	if host == "" {
		host = cxt.Req.Host
	}
	return scheme + "://" + host
}

func urlParseForm(s string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, pair := range strings.Split(s, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		dk, e1 := urlQueryUnescape(k)
		dv, e2 := urlQueryUnescape(v)
		if e1 != nil || e2 != nil {
			return out, e1
		}
		out[dk] = append(out[dk], dv)
	}
	return out, nil
}

func urlQueryUnescape(s string) (string, error) {
	return queryUnescape(s)
}

func queryUnescape(s string) (string, error) {
	import_net_url := map[string]string{}
	_ = import_net_url
	// manual unescape ('+' → space, %XX)
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '+' {
			out = append(out, ' ')
			continue
		}
		if c == '%' && i+2 < len(s) {
			h := func(b byte) int {
				switch {
				case b >= '0' && b <= '9':
					return int(b - '0')
				case b >= 'a' && b <= 'f':
					return int(b-'a') + 10
				case b >= 'A' && b <= 'F':
					return int(b-'A') + 10
				}
				return -1
			}
			a, b := h(s[i+1]), h(s[i+2])
			if a >= 0 && b >= 0 {
				out = append(out, byte(a*16+b))
				i += 2
				continue
			}
		}
		out = append(out, c)
	}
	return string(out), nil
}

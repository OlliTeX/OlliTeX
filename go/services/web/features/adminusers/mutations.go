// mutations.go — P6.3b admin user mutation surface (admin-tools
// UserListController), ported from Node:
//
//	POST   /admin/user/create               registerNewUser (+admin extras + activation mail)
//	POST   /admin/user/:userId/send-activation  _sendActivationEmail
//	POST   /admin/user/:userId/update       updateUser (name / email / canManageTemplates)
//	POST   /admin/user/:userId/delete       UserDeleter.deleteUser (+deleteUsersProjects)
//	POST   /admin/user/:userId/restore      restoreDeletedUser (+undelete owned projects)
//	DELETE /admin/user/:userId              expireDeletedUser (purge/redact)
//
// Node oracle pins (live 2026-09-16, 29 pins, /tmp/p63b_node.json) — exact
// status/CT/body parity for the pinned battery plus the side effects
// (audit entries, deletedUsers records, 7-day password tokens, mail
// count/recipient/subject).
package adminusers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"ollitex/go/services/web/features/emailtemplates"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/projectlist"
	"ollitex/go/services/web/features/registrationpage"
	"ollitex/go/services/web/views"
)

const oneWeekSec63b = 7 * 24 * 60 * 60

var emailRe63b = regexp.MustCompile(`^([^<>()[\]\\.,;:\s@"]+(\.[^<>()[\]\\.,;:\s@"]+)*)@((\[[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\])|(([a-zA-Z\-0-9]+\.)+[a-zA-Z]{2,}))$`)

// ---------- small helpers ----------

func mailAppName63b() string {
	if v := os.Getenv("OVERLEAF_APP_NAME"); v != "" {
		return v
	}
	if v := os.Getenv("OVERLEAF_APPNAME"); v != "" {
		return v
	}
	return "OlliTeX"
}

func adminEmail63b() string {
	if v := os.Getenv("OVERLEAF_ADMIN_EMAIL"); v != "" {
		return v
	}
	return "placeholder@example.com"
}

func randomHex63b(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(b)
}

func randomUUID63b() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString
	return h(b[0:4]) + "-" + h(b[4:6]) + "-" + h(b[6:8]) + "-" + h(b[8:10]) + "-" + h(b[10:16])
}

// Node TokenGenerator.generateReferralId = _randomString(16, alphanumerics).
func referralID63b() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("a", 16)
	}
	out := make([]byte, 16)
	for i := range out {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(out)
}

func parseEmail63b(email string) string {
	if email == "" || len(email) > 254 {
		return ""
	}
	s := strings.ToLower(strings.TrimSpace(email))
	if emailRe63b.MatchString(s) {
		return s
	}
	return ""
}

func reversedHostname63b(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}
	host := []byte(email[at+1:])
	for i, j := 0, len(host)-1; i < j; i, j = i+1, j-1 {
		host[i], host[j] = host[j], host[i]
	}
	return string(host)
}

func siteURL63b(cxt *core.Cxt) string {
	if cxt.SiteURL != "" {
		return cxt.SiteURL
	}
	return "http://" + cxt.Req.Host
}

func isoMs63b(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func jsonString63b(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func truthy63b(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t != ""
	case nil:
		return false
	default:
		return true
	}
}

// Node `newValue === user[key]` for JSON-typed values.
func jsonValEqual63b(a, b any) bool {
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case float64:
		switch bv := b.(type) {
		case float64:
			return av == bv
		case int64:
			return av == float64(bv)
		case int32:
			return av == float64(bv)
		case int:
			return av == float64(bv)
		}
		return false
	case int32:
		switch bv := b.(type) {
		case int32:
			return av == bv
		case int64:
			return int64(av) == bv
		case float64:
			return float64(av) == bv
		}
		return false
	default:
		ab, errA := json.Marshal(av)
		if errA != nil {
			return false
		}
		bb, errB := json.Marshal(b)
		if errB != nil {
			return false
		}
		return string(ab) == string(bb)
	}
}

func findDE63b(d primitive.D, key string) (any, bool) {
	for _, e := range d {
		if e.Key == key {
			return e.Value, true
		}
	}
	return nil, false
}

func docString63b(d primitive.D, key string) (string, bool) {
	v, ok := findDE63b(d, key)
	if !ok {
		return "", false
	}
	s, isS := v.(string)
	return s, isS
}

func objectID63bOrZero(h string) primitive.ObjectID {
	if o, err := primitive.ObjectIDFromHex(h); err == nil {
		return o
	}
	return primitive.NilObjectID
}

func actorHex63b(cxt *core.Cxt) string {
	if cxt.Sess != nil {
		return cxt.Sess.UserIDHex()
	}
	return ""
}

// ---------- error response matrices (Node HttpErrorHandler) ----------

func errResponse63b(cxt *core.Cxt, res *core.Res, code int, msg, plain string) {
	if core.AcceptsJSON(cxt.Req) {
		res.JSON(code, []byte(`{"message":`+jsonString63b(msg)+`}`))
		return
	}
	if acc := cxt.Req.Header.Get("Accept"); strings.Contains(acc, "text/html") {
		views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
		return
	}
	res.PlainText(code, plain)
}

func legacyInternal63b(cxt *core.Cxt, res *core.Res, msg string) {
	if core.AcceptsJSON(cxt.Req) {
		res.JSON(500, []byte(`{"message":`+jsonString63b(msg)+`}`))
		return
	}
	if acc := cxt.Req.Header.Get("Accept"); strings.Contains(acc, "text/html") || acc == "" {
		views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
		return
	}
	res.PlainText(500, "internal server error")
}

// ---------- mails ----------

type mailBox63b struct {
	mail *core.Mail
	tok  *core.OneTimeTokens
	app  *core.App // for the /hub-managed mail template store (emailtemplates)
}

func newMailBox63b(a *core.App) *mailBox63b {
	return &mailBox63b{mail: core.NewMail(), tok: core.NewOneTimeTokens(a.Mongo), app: a}
}

// 'registered' template (now the /hub-managed "activate-account" slot;
// owner-rebranded default byte-identical at app=mailAppName63b()).
func (fm *mailBox63b) sendRegistered63b(cxt *core.Cxt, to, link string) error {
	app := mailAppName63b()
	tmpl, tmErr := emailtemplates.RenderFor(fm.app, "activate-account", map[string]string{
		"app": app, "email": to, "link": link,
		"adminEmail": adminEmail63b(), "site": siteURL63b(cxt),
	})
	if tmErr != nil {
		return tmErr
	}
	return fm.mail.Send(to, tmpl.Subject, tmpl.Text, tmpl.HTML)
}

// security alert (now the /hub-managed "security-note" slot; the
// owner-rebranded default keeps the pinned subject at app="OlliTeX":
// the Node port hardcoded "Overleaf" there, owner item 3 rebrands it).
func (fm *mailBox63b) sendSecurityAlert63b(cxt *core.Cxt, to, action, actionDescribed string) error {
	_ = cxt
	tmpl, tmErr := emailtemplates.RenderFor(fm.app, "security-note", map[string]string{
		"app": "OlliTeX", "action": action, "description": actionDescribed,
		"adminEmail": adminEmail63b(),
	})
	if tmErr != nil {
		return tmErr
	}
	return fm.mail.Send(to, tmpl.Subject, tmpl.Text, tmpl.HTML)
}

// ---------- body parsing (Node express semantics) ----------

// readBodyMap63b parses per content type: JSON (objects; arrays/scalars ->
// empty map per Node destructuring), urlencoded (flat strings, Node qs),
// any other CT -> {} (Node req.body stays {}). Malformed JSON -> ok=false
// (caller answers 400 {}). Raw bytes returned for JSON key-order recovery.
func readBodyMap63b(kind string, cxt *core.Cxt) (map[string]any, []byte, bool) {
	raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
	out := map[string]any{}
	switch kind {
	case "json":
		if len(strings.TrimSpace(string(raw))) == 0 {
			return out, raw, true
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return out, raw, false
		}
		if m, isM := v.(map[string]any); isM {
			for k, val := range m {
				out[k] = val
			}
		}
	case "form":
		if vals, err := url.ParseQuery(string(raw)); err == nil {
			for k, v := range vals {
				if len(v) > 0 {
					out[k] = v[0]
				}
			}
		}
	}
	return out, raw, true
}

// renameKey63b applies Node's updateUser rename before iteration.
func renameKey63b(k string) string {
	switch k {
	case "firstName":
		return "first_name"
	case "lastName":
		return "last_name"
	}
	return k
}

// jsonKeyOrder63b recovers the raw JSON body's top-level key order (Node
// Object.entries iteration order), renamed, restricted to keys in bm.
func jsonKeyOrder63b(raw []byte, bm map[string]any) []string {
	out := []string{}
	if len(raw) == 0 {
		return out
	}
	seen := map[string]bool{}
	s := string(raw)
	i := 0
	for i < len(s) {
		if s[i] != '"' {
			i++
			continue
		}
		end := i + 1
		for end < len(s) && s[end] != '"' {
			if s[end] == '\\' {
				end++
			}
			end++
		}
		if end >= len(s) || s[end] != '"' {
			i++
			continue
		}
		var kvTok any
		if uerr := json.Unmarshal(raw[i:end+1], &kvTok); uerr == nil {
			if ks, isK := kvTok.(string); isK {
				rest := end + 1
				for rest < len(s) && (s[rest] == ' ' || s[rest] == '\t') {
					rest++
				}
				if rest < len(s) && s[rest] == ':' {
					mk := renameKey63b(ks)
					if !seen[mk] {
						if _, okk := bm[mk]; okk {
							out = append(out, mk)
							seen[mk] = true
						}
					}
					i = end + 2
					continue
				}
			}
		}
		i = end + 1
	}
	return out
}

// ---------- POST /admin/user/create ----------

func createHandler63b(fm *mailBox63b, a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := gate(a, cxt, res); !ok {
			return
		}
		kind := bodyKind(cxt.Req.Header.Get("Content-Type"))
		bm, _, bok := readBodyMap63b(kind, cxt)
		if !bok {
			res.JSON(400, []byte("{}"))
			return
		}

		emailRaw, hasEmail := bm["email"]
		emailStr, emailIsStr := emailRaw.(string)
		if !hasEmail || (emailIsStr && emailStr == "") {
			errResponse63b(cxt, res, 422, "Email address is empty", "unprocessable entity")
			return
		}
		if !emailIsStr {
			errResponse63b(cxt, res, 422, "email_address_is_invalid", "unprocessable entity")
			return
		}
		email := parseEmail63b(emailStr)
		if email == "" {
			errResponse63b(cxt, res, 422, "email_address_is_invalid", "unprocessable entity")
			return
		}

		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 20*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
			return
		}
		users := db.Collection("users")

		// registerNewUser: validate + getUserByAnyEmail + create-if-missing.
		var existing primitive.D
		found := users.FindOne(ctx,
			bson.D{{Key: "emails", Value: bson.D{{Key: "$exists", Value: true}}}, {Key: "emails.email", Value: email}}).
			Decode(&existing) == nil
		if !found && users.FindOne(ctx, bson.D{{Key: "email", Value: email}}).Decode(&existing) == nil {
			found = true
		}
		if found && holdingAccountFalse63b(existing) {
			errResponse63b(cxt, res, 409, "email_already_registered", "conflict")
			return
		}

		now := time.Now().UTC()
		var id primitive.ObjectID
		createFresh := !found
		if found {
			id, _ = existing[0].Value.(primitive.ObjectID)
		} else {
			id = primitive.NewObjectID()
		}

		first, firstIsStr := bm["first_name"].(string)
		if !firstIsStr || first == "" {
			if at := strings.Index(email, "@"); at > 0 {
				first = email[:at]
			} else {
				first = email
			}
		}
		last, hasLast := bm["last_name"].(string)

		pw := randomHex63b(32)
		hash, herr := bcrypt.GenerateFromPassword([]byte(pw), 12)
		if herr != nil {
			views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
			return
		}

		emailsPush := bson.A{bson.M{
			"email":            emailStr,
			"reversedHostname": reversedHostname63b(email),
			"confirmedAt":      now.Add(time.Millisecond),
			"_id":              primitive.NewObjectID(),
			"createdAt":        now.Add(time.Millisecond),
		}}

		if createFresh {
			doc := registrationpage.NewUserDoc()
			doc["_id"] = id
			doc["email"] = email
			doc["first_name"] = first
			if hasLast {
				doc["last_name"] = last
			}
			doc["analyticsId"] = randomUUID63b()
			doc["hashedPassword"] = string(hash)
			doc["signUpDate"] = now
			doc["thirdPartyIdentifiers"] = bson.A{}
			doc["referal_id"] = referralID63b()
			doc["emails"] = emailsPush
			if _, ierr := users.InsertOne(ctx, doc); ierr != nil {
				views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
				return
			}
		} else {
			if _, uerr := users.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}},
				bson.D{{Key: "$set", Value: bson.D{
					{Key: "holdingAccount", Value: false},
					{Key: "hashedPassword", Value: string(hash)},
					{Key: "emails", Value: emailsPush},
				}}}); uerr != nil {
				views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
				return
			}
		}

		setParts := []bson.E{}
		if v, okk := bm["isAdmin"]; okk {
			if b, isB := v.(bool); isB {
				setParts = append(setParts, bson.E{Key: "isAdmin", Value: b})
			}
		}
		if truthy63b(bm["canManageTemplates"]) {
			setParts = append(setParts, bson.E{Key: "flags.canManageTemplates", Value: true})
		}
		if len(setParts) > 0 {
			if _, uerr := users.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}},
				bson.D{{Key: "$set", Value: setParts}}); uerr != nil {
				views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
				return
			}
		}

		isExternal := false
		if be, isB := bm["isExternal"].(bool); isB {
			isExternal = be
		} else if bs, isS := bm["isExternal"].(string); isS {
			isExternal = bs == "true"
		}
		if isExternal {
			if _, uerr := users.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}},
				bson.D{{Key: "$unset", Value: bson.D{{Key: "hashedPassword", Value: ""}}}}); uerr != nil {
				views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
				return
			}
		}

		// Node: _sendActivationEmail when !isExternal (token then mail; mail
		// failure -> emailIsNotSent=true, still 200).
		emailIsNotSent := false
		if !isExternal {
			tok, terr := fm.tok.NewWithExp(ctx, "password",
				bson.M{"user_id": id.Hex(), "email": email},
				now.Add(oneWeekSec63b*time.Second))
			if terr == nil {
				link := siteURL63b(cxt) + "/user/activate?token=" + tok + "&user_id=" + id.Hex()
				if serr := fm.sendRegistered63b(cxt, email, link); serr != nil {
					emailIsNotSent = true
				}
			} else {
				emailIsNotSent = true
			}
		}

		parts := []string{
			`"id":` + jsonString63b(id.Hex()),
			`"email":` + jsonString63b(emailStr),
			`"firstName":` + jsonString63b(first),
		}
		if hasLast {
			parts = append(parts, `"lastName":`+jsonString63b(last))
		}
		if v, okk := bm["isAdmin"]; okk {
			if b, isB := v.(bool); isB {
				parts = append(parts, `"isAdmin":`+strconv.FormatBool(b))
			}
		}
		parts = append(parts,
			`"signUpDate":`+jsonString63b(isoMs63b(now)),
			`"inactive":true`,
			`"deleted":false`,
		)
		if isExternal {
			parts = append(parts, `"authMethods":[]`)
		} else {
			parts = append(parts, `"authMethods":["local"]`)
		}
		body := `{"user":{` + strings.Join(parts, ",") + `},"emailIsNotSent":` + strconv.FormatBool(emailIsNotSent) + `}`
		res.JSON(200, []byte(body))
	}
}

func holdingAccountFalse63b(doc primitive.D) bool {
	v, ok := findDE63b(doc, "holdingAccount")
	if !ok {
		return false
	}
	b, isB := v.(bool)
	return isB && !b
}

// ---------- POST /admin/user/:userId/send-activation ----------

func sendActivationHandler63b(fm *mailBox63b, a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := gate(a, cxt, res); !ok {
			return
		}
		fail422 := func() {
			errResponse63b(cxt, res, 422,
				"Error sending activation email. Please check your SMTP configuration.",
				"unprocessable entity")
		}
		tid, err := primitive.ObjectIDFromHex(strings.ToLower(cxt.Params["1"]))
		if err != nil {
			fail422()
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 20*time.Second)
		defer cancel()
		db, dbErr := a.Mongo.DB(ctx)
		if dbErr != nil {
			fail422()
			return
		}
		var udoc primitive.D
		if db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: tid}}).Decode(&udoc) != nil {
			fail422()
			return
		}
		email, _ := docString63b(udoc, "email")
		tok, terr := fm.tok.NewWithExp(ctx, "password",
			bson.M{"user_id": tid.Hex(), "email": email},
			time.Now().UTC().Add(oneWeekSec63b*time.Second))
		if terr != nil {
			fail422()
			return
		}
		link := siteURL63b(cxt) + "/user/activate?token=" + tok + "&user_id=" + tid.Hex()
		if serr := fm.sendRegistered63b(cxt, email, link); serr != nil {
			fail422()
			return
		}
		res.SendStatus(http.StatusOK)
	}
}

// ---------- POST /admin/user/:userId/update ----------

func updateHandler63b(fm *mailBox63b, a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := gate(a, cxt, res); !ok {
			return
		}
		tid, err := primitive.ObjectIDFromHex(strings.ToLower(cxt.Params["1"]))
		if err != nil {
			views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
			return
		}
		kind := bodyKind(cxt.Req.Header.Get("Content-Type"))
		bm, rawBody, bok := readBodyMap63b(kind, cxt)
		if !bok {
			res.JSON(400, []byte("{}"))
			return
		}

		if s, isS := bm["firstName"].(string); isS {
			bm["first_name"] = s
			delete(bm, "firstName")
		}
		if s, isS := bm["lastName"].(string); isS {
			bm["last_name"] = s
			delete(bm, "lastName")
		}

		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 30*time.Second)
		defer cancel()
		db, dbErr := a.Mongo.DB(ctx)
		if dbErr != nil {
			views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
			return
		}
		users := db.Collection("users")
		var udoc primitive.D
		if users.FindOne(ctx, bson.D{{Key: "_id", Value: tid}}).Decode(&udoc) != nil {
			views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
			return
		}

		actor := actorHex63b(cxt)
		ip := core.ClientIP(cxt.Req)
		oldEmail, _ := docString63b(udoc, "email")

		// ---- email path (Node order: first) ----
		emailIsUpdated := false
		newEmail, hasEmail := bm["email"].(string)
		if hasEmail {
			newEmail = strings.ToLower(strings.TrimSpace(newEmail))
		}
		if hasEmail && newEmail != "" && newEmail != oldEmail {
			if !strings.Contains(newEmail, "@") {
				errResponse63b(cxt, res, 422, "Email address is invalid", "unprocessable entity")
				return
			}
			if parseEmail63b(newEmail) == "" {
				legacyInternal63b(cxt, res,
					"There was a problem changing your email address. Please try again in a few moments. If the problem continues please contact us.")
				return
			}
			var hit primitive.D
			exists := users.FindOne(ctx,
				bson.D{{Key: "emails", Value: bson.D{{Key: "$exists", Value: true}}}, {Key: "emails.email", Value: newEmail}}).
				Decode(&hit) == nil
			if !exists {
				exists = users.FindOne(ctx, bson.D{{Key: "email", Value: newEmail}}).Decode(&hit) == nil
			}
			if exists {
				errResponse63b(cxt, res, 409,
					"This email address is already associated with a different Overleaf account.",
					"conflict")
				return
			}

			nowT := time.Now().UTC()
			audit := db.Collection("userAuditLogEntries")
			_, _ = audit.InsertOne(ctx, bson.D{
				{Key: "userId", Value: tid},
				{Key: "operation", Value: "add-email"},
				{Key: "info", Value: bson.D{{Key: "newSecondaryEmail", Value: newEmail}}},
				{Key: "initiatorId", Value: objectID63bOrZero(actor)},
				{Key: "ipAddress", Value: ip},
				{Key: "timestamp", Value: nowT},
			})
			_, _ = users.UpdateOne(ctx, bson.D{{Key: "_id", Value: tid}}, bson.D{
				{Key: "$push", Value: bson.D{{Key: "emails", Value: bson.D{
					{Key: "email", Value: newEmail},
					{Key: "createdAt", Value: nowT},
					{Key: "reversedHostname", Value: reversedHostname63b(newEmail)},
				}}}},
			})
			ures, uerr := users.UpdateOne(ctx,
				bson.D{{Key: "_id", Value: tid}, {Key: "emails.email", Value: newEmail}},
				bson.D{{Key: "$set", Value: bson.D{
					{Key: "email", Value: newEmail},
					{Key: "lastPrimaryEmailCheck", Value: nowT.Add(time.Millisecond)},
				}}})
			if uerr != nil || ures.MatchedCount != 1 {
				legacyInternal63b(cxt, res,
					"There was a problem changing your email address. Please try again in a few moments. If the problem continues please contact us.")
				return
			}
			_, _ = audit.InsertOne(ctx, bson.D{
				{Key: "userId", Value: tid},
				{Key: "operation", Value: "change-primary-email"},
				{Key: "info", Value: bson.D{
					{Key: "newPrimaryEmail", Value: newEmail},
					{Key: "oldPrimaryEmail", Value: oldEmail},
				}},
				{Key: "initiatorId", Value: objectID63bOrZero(actor)},
				{Key: "ipAddress", Value: ip},
				{Key: "timestamp", Value: nowT.Add(2 * time.Millisecond)},
			})
			desc := "the primary email address on your account was changed to " + newEmail
			_ = fm.sendSecurityAlert63b(cxt, oldEmail, "change of primary email address", desc)
			_ = fm.sendSecurityAlert63b(cxt, newEmail, "change of primary email address", desc)
			_, _ = audit.InsertOne(ctx, bson.D{
				{Key: "userId", Value: tid},
				{Key: "operation", Value: "remove-email"},
				{Key: "info", Value: bson.D{{Key: "removedEmail", Value: oldEmail}}},
				{Key: "initiatorId", Value: objectID63bOrZero(actor)},
				{Key: "ipAddress", Value: ip},
				{Key: "timestamp", Value: nowT.Add(3 * time.Millisecond)},
			})
			preres, perr := users.UpdateOne(ctx,
				bson.D{{Key: "_id", Value: tid}, {Key: "email", Value: bson.D{{Key: "$ne", Value: oldEmail}}}},
				bson.D{{Key: "$pull", Value: bson.D{{Key: "emails", Value: bson.D{{Key: "email", Value: oldEmail}}}}}})
			if perr != nil || preres.MatchedCount != 1 {
				legacyInternal63b(cxt, res,
					"There was a problem changing your email address. Please try again in a few moments. If the problem continues please contact us.")
				return
			}
			emailIsUpdated = true
		}

		// ---- generic changed-key loop (body order) ----
		type updEntry63b struct {
			key string
			val any
		}
		updates := []updEntry63b{}
		ordered := jsonKeyOrder63b(rawBody, bm)
		if len(ordered) == 0 {
			keys := make([]string, 0, len(bm))
			for k := range bm {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			ordered = keys
		}
		for _, key := range ordered {
			if key == "email" || key == "canManageTemplates" {
				continue
			}
			v := bm[key]
			nv := v
			if s, isS := v.(string); isS {
				nv = strings.TrimSpace(s)
			}
			if cur, hasCur := findDE63b(udoc, key); hasCur && jsonValEqual63b(nv, cur) {
				continue
			}
			updates = append(updates, updEntry63b{key: key, val: nv})
		}

		// ---- canManageTemplates (Node: after the loop, before save) ----
		if ct, ctIsPresent := bm["canManageTemplates"]; ctIsPresent {
			if bv, isB := ct.(bool); isB && !bv {
				var full primitive.D
				okFull := users.FindOne(ctx, bson.D{{Key: "_id", Value: tid}},
					options.FindOne().SetProjection(bson.D{{Key: "isAdmin", Value: 1}})).Decode(&full) == nil
				if okFull {
					isAdminVal, okB := findDE63b(full, "isAdmin")
					if isB2, okC := isAdminVal.(bool); okB && okC && isB2 {
						res.JSON(409, []byte(`{"userId":`+jsonString63b(tid.Hex())+
							`,"message":`+jsonString63b("Site admins are template gallery admins implicitly and cannot be removed from this role here.")+`}`))
						return
					}
				}
			}
			updates = append(updates, updEntry63b{key: "flags", val: map[string]any{"canManageTemplates": truthy63b(ct)}})
		}

		if len(updates) > 0 {
			setD := bson.D{}
			for _, u := range updates {
				setD = append(setD, bson.E{Key: u.key, Value: u.val})
			}
			if _, serr := users.UpdateOne(ctx, bson.D{{Key: "_id", Value: tid}},
				bson.D{{Key: "$set", Value: setD}}); serr != nil {
				views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
				return
			}
		}

		out := []string{}
		for _, u := range updates {
			switch u.key {
			case "first_name":
				if s, isS := u.val.(string); isS {
					out = append(out, `"firstName":`+jsonString63b(s))
					continue
				}
			case "last_name":
				if s, isS := u.val.(string); isS {
					out = append(out, `"lastName":`+jsonString63b(s))
					continue
				}
			}
			var v string
			switch t := u.val.(type) {
			case string:
				v = jsonString63b(t)
			case bool:
				v = strconv.FormatBool(t)
			default:
				b, merr := json.Marshal(t)
				if merr != nil {
					v = "null"
				} else {
					v = string(b)
				}
			}
			out = append(out, jsonString63b(u.key)+":"+v)
		}
		if emailIsUpdated {
			out = append(out, `"email":`+jsonString63b(newEmail))
		}
		res.JSON(200, []byte("{"+strings.Join(out, ",")+"}"))
	}
}

// ---------- POST /admin/user/:userId/delete ----------

func deleteHandler63b(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		actor, ok := gate(a, cxt, res)
		if !ok {
			return
		}
		fail422 := func() {
			errResponse63b(cxt, res, 422, "Something went wrong. Does the account still exist?", "unprocessable entity")
		}
		tid, err := primitive.ObjectIDFromHex(strings.ToLower(cxt.Params["1"]))
		if err != nil {
			fail422()
			return
		}

		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 30*time.Second)
		defer cancel()
		db, dbErr := a.Mongo.DB(ctx)
		if dbErr != nil {
			fail422()
			return
		}
		users := db.Collection("users")
		var udoc primitive.D
		if users.FindOne(ctx, bson.D{{Key: "_id", Value: tid}}).Decode(&udoc) != nil {
			fail422()
			return
		}

		kind := bodyKind(cxt.Req.Header.Get("Content-Type"))
		bm, _, _ := readBodyMap63b(kind, cxt)
		sendEmail := false
		if b, isB := bm["sendEmail"].(bool); isB {
			sendEmail = b
		} else if bs, isS := bm["sendEmail"].(string); isS {
			sendEmail = bs == "true"
		}

		ip := core.ClientIP(cxt.Req)
		nowT := time.Now().UTC()

		// 1) audit entry (Node: before cleanup).
		_, _ = db.Collection("userAuditLogEntries").InsertOne(ctx, bson.D{
			{Key: "userId", Value: tid},
			{Key: "operation", Value: "delete-account"},
			{Key: "initiatorId", Value: objectID63bOrZero(actor)},
			{Key: "info", Value: bson.D{}},
			{Key: "ipAddress", Value: ip},
			{Key: "timestamp", Value: nowT},
		})

		// 2) _createDeletedUser (Node upsert; undefined keys omitted).
		dd := bson.D{
			{Key: "deletedAt", Value: nowT},
		}
		if o, oerr := primitive.ObjectIDFromHex(actor); oerr == nil {
			dd = append(dd, bson.E{Key: "deleterId", Value: o})
		}
		if ip != "" {
			dd = append(dd, bson.E{Key: "deleterIpAddress", Value: ip})
		}
		dd = append(dd, bson.E{Key: "deletedUserId", Value: tid})
		for _, pair := range []struct{ dk, uk string }{
			{"deletedUserLastLoggedIn", "lastLoggedIn"},
			{"deletedUserSignUpDate", "signUpDate"},
			{"deletedUserLoginCount", "loginCount"},
			{"deletedUserReferralId", "referal_id"},
			{"deletedUserReferredUsers", "refered_users"},
			{"deletedUserReferredUserCount", "refered_user_count"},
		} {
			if v, has := findDE63b(udoc, pair.uk); has {
				dd = append(dd, bson.E{Key: pair.dk, Value: v})
			}
		}
		if ov, hasOv := findDE63b(udoc, "overleaf"); hasOv {
			if m, isM := ov.(primitive.D); isM {
				if idv, isID := findDE63b(m, "id"); isID && idv != nil {
					dd = append(dd, bson.E{Key: "deletedUserOverleafId", Value: idv})
				}
			}
		}
		_, _ = db.Collection("deletedUsers").UpdateOne(ctx,
			bson.D{{Key: "deleterData.deletedUserId", Value: tid}},
			bson.D{{Key: "$set", Value: bson.D{
				{Key: "user", Value: udoc},
				{Key: "deleterData", Value: dd},
			}}}, options.Update().SetUpsert(true))

		// 3) owned-projects chain ("account-deletion") + membership pulls.
		projectlist.DeleteOwnedProjects(a, cxt, tid.Hex(), actor, ip)

		// 4) deletion mail unless skipEmail (Node throws on failure -> 422).
		if sendEmail {
			em, _ := docString63b(udoc, "email")
			fm := newMailBox63b(a)
			if serr := fm.sendSecurityAlert63b(cxt, em, "account deleted", "your Overleaf account was deleted"); serr != nil {
				fail422()
				return
			}
		}

		if _, derr := users.DeleteOne(ctx, bson.D{{Key: "_id", Value: tid}}); derr != nil {
			fail422()
			return
		}

		// Response {deletedAt} read back from the record (Node shape;
		// explicit D target so the nested doc keeps document order).
		var delRec delRec63b
		if db.Collection("deletedUsers").FindOne(ctx,
			bson.D{{Key: "user._id", Value: tid}}).Decode(&delRec) != nil {
			views.Error500Page(res.W, pageBase(cxt, cxt.Req.URL.Path))
			return
		}
		dat := "null"
		if t, isT := findDE63b(delRec.DeleterData, "deletedAt"); isT {
			switch tm := t.(type) {
			case time.Time:
				dat = jsonString63b(isoMs63b(tm))
			case int64:
				dat = jsonString63b(time.UnixMilli(tm).UTC().Format("2006-01-02T15:04:05.000Z"))
			case primitive.DateTime:
				dat = jsonString63b(time.UnixMilli(int64(tm)).UTC().Format("2006-01-02T15:04:05.000Z"))
			}
		}
		res.JSON(200, []byte(`{"deletedAt":`+dat+`}`))
	}
}

// ---------- POST /admin/user/:userId/restore ----------

func restoreHandler63b(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := gate(a, cxt, res); !ok {
			return
		}
		tid, err := primitive.ObjectIDFromHex(strings.ToLower(cxt.Params["1"]))
		if err != nil {
			legacyInternal63b(cxt, res, "Sorry, something went wrong")
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 30*time.Second)
		defer cancel()
		db, dbErr := a.Mongo.DB(ctx)
		if dbErr != nil {
			legacyInternal63b(cxt, res, "Sorry, something went wrong")
			return
		}
		var delRec delRec63b
		if db.Collection("deletedUsers").FindOne(ctx,
			bson.D{{Key: "user._id", Value: tid}}).Decode(&delRec) != nil {
			errResponse63b(cxt, res, 422, "Something went wrong. The user is purged?", "unprocessable entity")
			return
		}
		if delRec.User == nil {
			errResponse63b(cxt, res, 422, "Something went wrong. The user is purged?", "unprocessable entity")
			return
		}
		snapshot := delRec.User
		snapshotEmail, _ := docString63b(snapshot, "email")
		var hit primitive.D
		if db.Collection("users").FindOne(ctx, bson.D{{Key: "email", Value: snapshotEmail}}).Decode(&hit) == nil {
			errResponse63b(cxt, res, 409,
				"This email address is already associated with a different Overleaf account.",
				"conflict")
			return
		}
		// Node: userData.suspended = false; User.create(userData)
		restored := bson.D{}
		suspendedSet := false
		for _, e := range snapshot {
			if e.Key == "suspended" {
				restored = append(restored, bson.E{Key: "suspended", Value: false})
				suspendedSet = true
				continue
			}
			restored = append(restored, e)
		}
		if !suspendedSet {
			restored = append(restored, bson.E{Key: "suspended", Value: false})
		}
		if _, ierr := db.Collection("users").InsertOne(ctx, restored); ierr != nil {
			legacyInternal63b(cxt, res, "Sorry, something went wrong")
			return
		}
		_, _ = db.Collection("deletedUsers").DeleteOne(ctx,
			bson.D{{Key: "user._id", Value: tid}})
		projectlist.RestoreOwnedDeletedProjects(a, cxt, tid.Hex())
		res.JSON(200, []byte(`{"restoredId":`+jsonString63b(tid.Hex())+`,"email":`+jsonString63b(snapshotEmail)+`}`))
	}
}

// delRec63b — explicit D targets keep nested documents ordered (decoding a
// sub-doc into interface{} yields an unordered primitive.M).
type delRec63b struct {
	ID          primitive.ObjectID `bson:"_id"`
	User        primitive.D        `bson:"user"`
	DeleterData primitive.D        `bson:"deleterData"`
}

// ---------- DELETE /admin/user/:userId (purge) ----------

func purgeHandler63b(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if _, ok := gate(a, cxt, res); !ok {
			return
		}
		fail422 := func() {
			errResponse63b(cxt, res, 422, "Something went wrong. The user is already deleted?", "unprocessable entity")
		}
		tid, err := primitive.ObjectIDFromHex(strings.ToLower(cxt.Params["1"]))
		if err != nil {
			fail422()
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
		defer cancel()
		db, dbErr := a.Mongo.DB(ctx)
		if dbErr != nil {
			fail422()
			return
		}
		var delRec delRec63b
		if db.Collection("deletedUsers").FindOne(ctx,
			bson.D{{Key: "deleterData.deletedUserId", Value: tid}}).Decode(&delRec) != nil {
			fail422()
			return
		}
		_, _ = db.Collection("deletedUsers").UpdateOne(ctx,
			bson.D{{Key: "_id", Value: delRec.ID}},
			bson.D{{Key: "$unset", Value: bson.D{
				{Key: "user", Value: ""},
				{Key: "deleterData.deleterIpAddress", Value: ""},
			}}})
		res.SendStatus(200)
	}
}

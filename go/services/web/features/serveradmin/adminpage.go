// GET /admin — the admin shell page (U9).
//
// Node ground truth (router.mjs:1153 → AuthzMW.ensureUserIsSiteAdmin →
// AdminController.index → admin/index.pug):
//
//   - non-admin: 302 /restricted?from=<pathname> (core.RequireSiteAdmin)
//   - page data:
//     title 'System Admin'; adminOverallTheme = user.ace?.overallTheme ?? ”;
//     llmEnabled = LLM_ENABLED === 'true' || !!Settings.llm.enabled
//     (page clause — distinct from the llm MODULE gate `enabled !== false`;
//     e2e: env LLM_ENABLED=true → true);
//     systemMessages = SystemMessage.find({}) natural order;
//     openSockets — Node maps http/https.globalAgent.sockets; Go has no such
//     global agent → always EMPTY in this port (honest-oracle pinned U9:
//     the e2e capture renders the empty <ul></ul> on Node too);
//     hasFeature('saas') tabs (privileges-matrix/tpds/debug-projects) — absent
//     in this CE build (pinned off).
//   - variants: /Admin, /ADMIN, /admin/ (Express case + slash; canonical
//     alternate link reflects the requested path — Node oracle 2026-09-22).
package serveradmin

import (
	"context"
	"os"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/editorpages"
	"ollitex/go/services/web/views"
)

// adminRoute — /admin + case/trailing-slash variants (U9). Registered in
// Feature() BEFORE the longer /admin/* leaves (the dispatch is linear; the
// variants never overlap the leaves, but the valid-before-bad discipline
// stays from U2).
const adminPagePattern = `^/(?i:admin)/?$`

var adminPageRe = regexp.MustCompile(adminPagePattern)

// llmPageEnabled — AdminController.index page clause (see file doc).
func llmPageEnabled(a *core.App, ctx context.Context) bool {
	if os.Getenv("LLM_ENABLED") == "true" {
		return true
	}
	if a == nil || a.Mongo == nil {
		return false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	var doc struct {
		LLM *struct {
			Enabled bool `bson:"enabled"`
		} `bson:"llm"`
	}
	if err := db.Collection("site_settings").FindOne(ctx, bson.D{}).Decode(&doc); err != nil {
		return false
	}
	return doc.LLM != nil && doc.LLM.Enabled
}

// adminUserFields — user.doc { email, ace.overallTheme } (Node:
// AdminController.index: user?.ace?.overallTheme ?? ”; the navbar account
// pill reads udoc.email, hub-style).
func adminUserFields(a *core.App, ctx context.Context, uidHex string) (email, theme string) {
	if a == nil || a.Mongo == nil || uidHex == "" {
		return "", ""
	}
	oid, err := primitive.ObjectIDFromHex(uidHex)
	if err != nil {
		return "", ""
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return "", ""
	}
	var doc struct {
		Email string `bson:"email"`
		Ace   *struct {
			OverallTheme *string `bson:"overallTheme"`
		} `bson:"ace"`
	}
	if err := db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}},
		options.FindOne().SetProjection(bson.D{
			{Key: "email", Value: 1},
			{Key: "ace.overallTheme", Value: 1},
		})).Decode(&doc); err != nil {
		return "", ""
	}
	if doc.Ace != nil && doc.Ace.OverallTheme != nil {
		theme = *doc.Ace.OverallTheme
	}
	return doc.Email, theme
}

// adminMsgs — SystemMessage.find({}) natural order (Node order = insertion).
func adminMsgs(a *core.App, ctx context.Context) []string {
	if a == nil || a.Mongo == nil {
		return nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil
	}
	cur, err := db.Collection("systemmessages").Find(ctx, bson.D{})
	if err != nil {
		return nil
	}
	defer cur.Close(ctx)
	var out []string
	for cur.Next(ctx) {
		var m struct {
			Content string `bson:"content"`
		}
		if cur.Decode(&m) == nil {
			out = append(out, m.Content)
		}
	}
	return out
}

func adminPage(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
		defer cancel()
		sess := cxt.Sess
		uid := sess.UserIDHex()
		email, theme := adminUserFields(a, ctx, uid)
		msgs := adminMsgs(a, ctx)
		p := views.AdminShellParams{
			Nonce:          views.NewNonce(),
			CSRF:           sess.CsrfToken(),
			Email:          email,
			UID:            uid,
			OverallTheme:   theme,
			Origin:         cxt.SiteURL,
			CurrentURL:     cxt.Req.URL.Path,
			Exposed:        editorpages.ExposedSettingsJSON(cxt.SiteURL, true),
			LLMEnabled:     llmPageEnabled(a, ctx),
			SystemMessages: msgs,
		}
		views.AdminShellPage(res.W, p)
	}
}

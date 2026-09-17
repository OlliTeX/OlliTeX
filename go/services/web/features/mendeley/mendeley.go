// Package mendeley implements the Node services/web/modules/mendeley
// surface (MendeleyRouter.mjs + MendeleyController.mjs +
// MendeleyApiClient.mjs) on the Go web service.
//
// Routes (Node registration order; all requireLogin — anonymous is bounced
// by the core global chain, pinned P1: GET+json 401 / bare GET 302 → /login
// / non-GET 403):
//
//	GET  /user/mendeley/status          → 200 {"configured":c,"connected":c}
//	GET  /mendeley/groups               → 200 {"groups":[…]}; 403 not_configured;
//	                                       403 forbidden; 500 internal
//	GET  /user/mendeley/oauth           → 302 Mendeley authorize URL
//	                                       (302 /hub#mysettings.references
//	                                       when unconfigured)
//	GET  /user/mendeley/oauth/callback  → 302 /hub#mysettings.references
//	                                       (every path — state-checked,
//	                                       token-exchange, error)
//	POST /mendeley/unlink               → 200 OK (no-op when unlinked); 500
//
// Pinned 2026-09-17 (the e2e instance is mendeley-UNCONFIGURED, user
// mendeley-UNLINKED — no api.mendeley.com traffic on the gate paths):
//
//	status  → 200 application/json {"configured":false,"connected":false}
//	groups  → 403 application/json {"error":"not_configured","message":"mendeley_groups_relink"}
//	oauth   → 302 Location /hub#mysettings.references
//	callback→ 302 Location /hub#mysettings.references
//	unlink  → 200 text/plain OK
package mendeley

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"

	"ollitex/go/services/web/core"
)

// REFERENCES_PAGE — Node MendeleyController (the hub references anchor the
// OAuth popup round-trips back to).
const referencesPage = "/hub#mysettings.references"

// ctxWith/uidOf — core session context + session user id hex.
func uidOf(cxt *core.Cxt) string {
	if cxt.Sess == nil {
		return ""
	}
	return cxt.Sess.UserIDHex()
}

func hStatus(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		m, err := getMendeleySettings(ctx, a)
		if err != nil {
			// Node getMendeleySettings swallows its own failures into the
			// env-seed fallback — keep the request green.
			seedID, seedCB := mendeleyEnvSeed()
			m = mendeleySettings{Enabled: true, ClientID: seedID, ClientSecret: "", CallbackURL: orDefault(seedCB, "/user/mendeley")}
		}
		configured := m.Enabled && isServiceConfigured(m)
		connected := false
		if configured {
			if ok, _ := isLinked(ctx, a, uidOf(cxt)); ok {
				connected = true
			}
		}
		body := `{"configured":` + boolStr(configured) + `,"connected":` + boolStr(connected) + `}`
		res.JSON(200, []byte(body))
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func hGroups(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		m, err := getMendeleySettings(ctx, a)
		if err != nil {
			seedID, seedCB := mendeleyEnvSeed()
			m = mendeleySettings{Enabled: true, ClientID: seedID, ClientSecret: "", CallbackURL: orDefault(seedCB, "/user/mendeley")}
		}
		if !isServiceConfigured(m) {
			res.JSON(403, []byte(`{"error":"not_configured","message":"mendeley_groups_relink"}`))
			return
		}
		uid := uidOf(cxt)
		groups, err2 := getGroupsForUser(ctx, a, uid, m)
		if err2 != nil {
			switch err2.(type) {
			case mendeleyForbiddenErr, mendeleyExpiredErr, mendeleyNotLinkedErr:
				res.JSON(403, []byte(`{"error":"forbidden","message":"mendeley_groups_relink"}`))
				return
			}
			res.JSON(500, []byte(`{"error":"internal","message":"mendeley_groups_loading_error"}`))
			return
		}
		items := make([]string, 0, len(groups))
		for _, g := range groups {
			items = append(items, `{"id":"`+jsQuote(g.ID)+`","name":"`+jsQuote(g.Name)+`"}`)
		}
		res.JSON(200, []byte(`{"groups":[`+strings.Join(items, ",")+`]}`))
	}
}

// jsQuote — JSON.stringify(string) escaping for the two group fields.
func jsQuote(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\b':
			b.WriteString(`\b`)
		case c == '\f':
			b.WriteString(`\f`)
		case c < 0x20:
			hexd := "0123456789abcdef"
			b.WriteString(`\u00` + string(hexd[c>>4]) + string(hexd[c&0xF]))
		case c < 0x80:
			b.WriteByte(c)
		default:
			b.WriteByte(c) // UTF-8 continuation — verbatim (Node does the same)
		}
	}
	return b.String()
}

func hOAuth(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		m, err := getMendeleySettings(ctx, a)
		if err != nil {
			seedID, seedCB := mendeleyEnvSeed()
			m = mendeleySettings{Enabled: true, ClientID: seedID, ClientSecret: "", CallbackURL: orDefault(seedCB, "/user/mendeley")}
		}
		// Node: state generated + stored BEFORE getOAuthAuthorizeUrl
		// (which throws when unconfigured → the catch redirects to the
		// references page). Mirror the store-on-both-paths shape.
		stateBytes := make([]byte, 16)
		_, _ = rand.Read(stateBytes)
		state := hex.EncodeToString(stateBytes)
		if cxt.Sess != nil {
			cxt.Sess.Set("mendeleyOAuthState", state)
		}
		if !isServiceConfigured(m) {
			// _ensureConfigured threw → catch → references redirect.
			res.Redirect(cxt.Req, 302, referencesPage)
			return
		}
		res.Redirect(cxt.Req, 302, getOAuthAuthorizeURL(state, m))
	}
}

func hOAuthCallback(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		code := cxt.Req.URL.Query().Get("code")
		state := cxt.Req.URL.Query().Get("state")

		expected := ""
		if cxt.Sess != nil {
			if raw, ok := cxt.Sess.GetRaw("mendeleyOAuthState"); ok {
				var s string
				if jerr := json.Unmarshal(raw, &s); jerr == nil {
					expected = s
				}
			}
			cxt.Sess.Del("mendeleyOAuthState")
		}

		ok := code != "" && state != "" && expected != "" && state == expected
		if ok {
			m, serr := getMendeleySettings(ctx, a)
			if serr == nil && isServiceConfigured(m) {
				uid := uidOf(cxt)
				form := urlValues(
					"grant_type", "authorization_code",
					"code", code,
					"redirect_uri", m.CallbackURL,
				)
				tokens, terr := requestToken(ctx, form, m)
				if terr == nil {
					_ = storeCreds(ctx, a, uid, tokens)
				}
			}
		}
		// Every Node callback path ends at the references redirect.
		res.Redirect(cxt.Req, 302, referencesPage)
	}
}

func hUnlink(a *core.App) func(cxt *core.Cxt, res *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if err := unlinkAccount(cxt.Req.Context(), a, uidOf(cxt)); err != nil {
			res.SendStatus(500)
			return
		}
		res.SendStatus(200)
	}
}

func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "mendeley",
		Routes: []core.Route{
			{Method: "GET", Path: "/user/mendeley/status", Handler: hStatus(a)},
			{Method: "GET", Path: "/mendeley/groups", Handler: hGroups(a)},
			{Method: "GET", Path: "/user/mendeley/oauth", Handler: hOAuth(a)},
			{Method: "GET", Path: "/user/mendeley/oauth/callback", Handler: hOAuthCallback(a)},
			{Method: "POST", Path: "/mendeley/unlink", Handler: hUnlink(a)},
		},
	}
}

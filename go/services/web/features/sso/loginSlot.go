package sso

// loginSlot.go — the /login ol-auth-config SSO button slot (fedgap-2 / 6g).
//
// Node pin (SSOAdminRouter webRouter.use('/login')):
//   providers = await getEnabledProviders()
//   res.locals.ssoProviders = providers.map(p => ({
//     type, id,
//     buttonLabel: p.buttonLabel || p.name || `Log in with ${TYPE}`,
//     loginUrl: dbMode && p.id ? `/saml(login)|/oidc(login)/${p.id}` : `/saml/login|/oidc/login`,
//   }))
// Reshaped to the Go login page contract (pages/auth/login.tsx):
//   ol-auth-config = { "sso": [{label,href}...], "ldapEnabled": false }
//
// INVARIANTS (fedgap-6g, audited): ENABLED providers only; each button's href
// is typed to its OWN (type,id) — no cross-provider leakage. The pure build
// (buildLoginSlot) is unit-tested with a synthetic provider list.

import (
	"encoding/json"
	"html"
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"

	"ollitex/go/services/web/core"
)

// loginSlotBtn — one SSO login button (exactly the {label,href} the React
// login page renders).
type loginSlotBtn struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}

// buildLoginSlot — pure: raw provider docs (+ optional env providers) → the
// ordered button list. ENABLED-only; sorted by order; each href is
// `/<type>/login[/<id>]` (correctly typed to its own provider — no cross-
// provider leakage).
func buildLoginSlot(providers []bson.Raw, envSaml *SAMLProvider, envOidc *OIDCProvider) []loginSlotBtn {
	type en struct {
		order int
		typ   string
		name  string
		id    string
	}
	list := []en{}
	for _, p := range providers {
		var oo OIDCProvider
		if err := bson.Unmarshal(p, &oo); err == nil && oo.Enabled && oo.Type == "oidc" {
			list = append(list, en{oo.Order, "oidc", oo.Name, oo.ID})
			continue
		}
		var ss SAMLProvider
		if err := bson.Unmarshal(p, &ss); err == nil && ss.Enabled && ss.Type == "saml" {
			list = append(list, en{ss.Order, "saml", ss.Name, ss.ID})
		}
	}
	if envOidc != nil {
		list = append(list, en{0, "oidc", envOidc.Name, envOidc.ID})
	}
	if envSaml != nil {
		list = append(list, en{0, "saml", envSaml.Name, envSaml.ID})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].order < list[j].order })
	out := []loginSlotBtn{}
	for _, e := range list {
		label := e.name
		if label == "" {
			label = "Log in with " + strings.ToUpper(e.typ)
		}
		href := "/" + e.typ + "/login"
		if e.id != "" {
			href = href + "/" + e.id
		}
		out = append(out, loginSlotBtn{Label: label, Href: href})
	}
	return out
}

// loginSlotJSON — the /login meta content (HTML-escaped attribute value).
func loginSlotJSON(a *core.App, cxt *core.Cxt) string {
	providers := []bson.Raw{}
	if a.Mongo != nil {
		if db, err := a.Mongo.DB(cxt.Req.Context()); err == nil {
			if cfg := loadSSOConfig(db, cxt); cfg != nil {
				providers = cfg.Providers
			}
		}
	}
	// Env-mode providers (EXTERNAL_AUTH=OIDC|SAML; Node parity).
	es, eo := envSAMLProvider(), envOIDCProvider()
	buttons := buildLoginSlot(providers, es, eo)

	type shape struct {
		Sso         []loginSlotBtn `json:"sso"`
		LdapEnabled bool           `json:"ldapEnabled"`
	}
	out := shape{Sso: []loginSlotBtn{}}
	for _, b := range buttons {
		out.Sso = append(out.Sso, b)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return html.EscapeString(string(b))
}

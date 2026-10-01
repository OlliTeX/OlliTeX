package sso

// loginSlot.go — the /login ol-auth-config SSO button slot (fedgap-2).
//
// Node pin (SSOAdminRouter webRouter.use('/login')):
//   providers = await getEnabledProviders()
//   res.locals.ssoProviders = providers.map(p => ({
//     type, id,
//     buttonLabel: p.buttonLabel || p.name || `Log in with ${TYPE}`,
//     loginUrl: dbMode && p.id ? `/saml(login)|/oidc(login)/${p.id}` : `/saml/login|/oidc/login`,
//   }))
// Reshaped to the Go login page contract (pages/auth/login.tsx):
//   ol-auth-config = { "sso": [{label, href}...], "ldapEnabled": false }
// (the React page renders exactly cfg.sso[].label/.href).

import (
	"encoding/json"
	"html"
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"

	"ollitex/go/services/web/core"
)

// loginSlotJSON — the /login meta content (HTML-escaped attribute value).
func loginSlotJSON(a *core.App, cxt *core.Cxt) string {
	type btn struct {
		Label string `json:"label"`
		Href  string `json:"href"`
	}
	type shape struct {
		Sso         []btn `json:"sso"`
		LdapEnabled bool  `json:"ldapEnabled"`
	}
	type en struct {
		order int
		typ   string
		name  string
		id    string
	}
	var btns []en
	if a.Mongo != nil {
		if db, err := a.Mongo.DB(cxt.Req.Context()); err == nil {
			if cfg := loadSSOConfig(db, cxt); cfg != nil {
				for _, p := range cfg.Providers {
					var oo OIDCProvider
					if uerr := bson.Unmarshal(p, &oo); uerr == nil && oo.Enabled {
						btns = append(btns, en{oo.Order, "oidc", oo.Name, oo.ID})
					}
					var ss SAMLProvider
					if uerr := bson.Unmarshal(p, &ss); uerr == nil && ss.Enabled {
						btns = append(btns, en{ss.Order, "saml", ss.Name, ss.ID})
					}
				}
			}
		}
	}
	// Env-mode provider (EXTERNAL_AUTH=OIDC|SAML; Node parity).
	if envP := envSAMLProvider(); envP != nil {
		btns = append(btns, en{0, "saml", envP.Name, envP.ID})
	}
	if envO := envOIDCProvider(); envO != nil {
		btns = append(btns, en{0, "oidc", envO.Name, envO.ID})
	}
	out := shape{Sso: []btn{}}
	if len(btns) > 0 {
		sort.SliceStable(btns, func(i, j int) bool { return btns[i].order < btns[j].order })
		for _, e := range btns {
			prefix := e.typ
			label := e.name
			if label == "" {
				label = "Log in with " + strings.ToUpper(prefix)
			}
			href := "/" + prefix + "/login"
			if e.id != "" {
				href = href + "/" + e.id
			}
			out.Sso = append(out.Sso, btn{Label: label, Href: href})
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return html.EscapeString(string(b))
}

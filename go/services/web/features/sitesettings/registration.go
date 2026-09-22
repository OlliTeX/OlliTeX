// registration-page parity helpers (U9).
package sitesettings

import (
	"context"
	"os"

	"ollitex/go/services/web/core"
)

// RegistrationEnabled — the Node hasFeature('registration-page') truth for
// the navbar showSignUpLink and the /register family (modules/registration-
// page/index.mjs):
//
//	enableRegistrationPage = boolFromEnv(OVERLEAF_ENABLE_REGISTRATION_PAGE)
//	if (undefined) → !(Settings.ldap?.enable || Settings.saml?.enable ||
//	                Settings.oidc?.enable)
//
// Node boolFromEnv (authentication/utils.mjs): only the exact strings
// "true"/"false" are conclusive; anything else (incl. unset) → undefined
// (falls through to the SSO test). The SSO flags are the site_settings
// `sso-<provider>` section `enabled` booleans (Node's settings shim exposes
// them as Settings.<provider>.enable — the evaluated outcome is the same;
// no such sections at all → no SSO → enabled). Pinned live 2026-09-22:
// e2e stack has sso-saml.enabled=true → false (showSignUpLink:false on
// /hub captured on Node).
func RegistrationEnabled(a *core.App, ctx context.Context) bool {
	switch os.Getenv("OVERLEAF_ENABLE_REGISTRATION_PAGE") {
	case "true":
		return true
	case "false":
		return false
	}
	if a == nil || a.Mongo == nil {
		return true // no site_settings → no SSO flag set → Node: !(undefined) → true
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return true
	}
	sections := loadAllSections(ctx, db)
	for _, sec := range []string{"sso-ldap", "sso-saml", "sso-oidc"} {
		o, ok := sections[sec]
		if !ok {
			continue
		}
		if v, jok := ObjGet(o, "enabled"); jok {
			if b, isB := v.(bool); isB && b {
				return false
			}
		}
	}
	return true
}

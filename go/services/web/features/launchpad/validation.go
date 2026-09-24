// Package launchpad ports the Node web launchpad module (first-admin
// bootstrap) — services/web/modules/launchpad (LaunchpadRouter.mjs +
// LaunchpadController.mjs + views/launchpad.pug). P6.20, the last P6 flip.
//
// Routes (router, all pinned against the live Node oracle 2026-09-21):
//
//	GET  /launchpad                      (login-whitelisted)
//	POST /launchpad/register_admin       (login-whitelisted)
//	POST /launchpad/register_ldap_admin  (login-whitelisted)
//	POST /launchpad/register_saml_admin  (login-whitelisted)
//	POST /launchpad/send_test_email      (requireGlobalLogin + site-admin)
//
// authMethod (fork-specific, pinned): the SSO module
// `authentication/ldap` (moduleImportSequence, loaded by EVERY web
// process) sets `Settings.ldap` unconditionally — the full DB config or
// `{ enable: false }` when unconfigured (LDAPModuleManager.initSettings).
// Node's `if (Settings.ldap) return 'ldap'` is therefore ALWAYS taken;
// the 'saml' and 'local' branches are unreachable in this deployment.
// Oracle consequences: the fresh page renders the LDAP-branch layout
// (captured bake) and register_ldap_admin passes the method gate.

package launchpad

import (
	"regexp"
	"strings"
)

// ---- authMethod -----------------------------------------------------------

// authMethod mirrors LaunchpadController.getAuthMethod with the fork SSO
// module loaded (see package comment): Settings.ldap is always an object
// → 'ldap' selected, 'saml'/'local' unreachable.
func authMethod() string { return "ldap" }

// ---- email parsing (EmailHelper.parseEmail) -------------------------------

// emailRE — EmailHelper.EMAIL_REGEXP (identical char sets; RE2-safe).
var emailRE = regexp.MustCompile(`^([^<>()[\]\\.,;:\s@"]+)(\.[^<>()[\]\\.,;:\s@"]+)*@((\[[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\])|(([a-zA-Z\-0-9]+\.)+[a-zA-Z]{2,}))$`)

// parseEmail mirrors EmailHelper.parseEmail(email, /*parseRfc*/ false):
// trim + lowercase, ≤254, EMAIL_REGEXP; "" when invalid.
func parseEmail(email string) string {
	if email == "" || len(email) > 254 {
		return ""
	}
	email = strings.ToLower(strings.TrimSpace(email))
	m := emailRE.FindString(email)
	if m == "" {
		return ""
	}
	return m
}

// validateEmail mirrors AuthenticationManager.validateEmail: returns the
// Node InvalidEmailError message, or "" when valid.
func validateEmail(email string) string {
	if parseEmail(email) == "" {
		return "email not valid"
	}
	return ""
}

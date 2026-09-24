package editorpages

import "strings"

// Shared page-data builders, exported for page features that render the same
// ol-* bootstrap metas (P6.1 ollitex-hub /hub page). These are the SAME Node
// derivations byte-pinned in P5.1a (User.findById projection,
// UserSettingsHelper.buildUserSettings, ExpressLocals navbar/footer), so the
// hub page reuses them verbatim instead of duplicating.

// SerializeUser — the `ol-user` meta (Node User.findById projection + ace).
func SerializeUser(uid, email string, doc map[string]any) string {
	return serializeUser(uid, email, doc)
}

// BuildUserSettings — the `ol-userSettings` meta (Node
// UserSettingsHelper.buildUserSettings).
func BuildUserSettings(doc map[string]any) string {
	return buildUserSettings(doc)
}

// NavbarJSON — the `ol-navbar` meta (ExpressLocals navbar locals; isAdmin
// toggles canDisplayAdminMenu + canDisplayProjectUrlLookup).
func NavbarJSON(siteURL, currentURL, email string, isAdmin, showSignUp bool) string {
	return navbarJSON(siteURL, currentURL, email, isAdmin, showSignUp)
}

// FooterJSON — the `ol-footer` meta (static in this stack; siteUrl-substituted).
func FooterJSON(siteURL string) string {
	return withSiteURL(pinned_ol_footer, siteURL)
}

// HubFooterJSON — the HUB page's ol-footer (hub.pug sets
// `- const showThinFooter = false`, overriding the layout default that
// the shared pin carries — pinned live 2026-09-16 in the P6.1 gate).
func HubFooterJSON(siteURL string) string {
	return strings.ReplaceAll(FooterJSON(siteURL), `"showThinFooter":true`, `"showThinFooter":false`)
}

// ExposedSettingsJSON — the `ol-ExposedSettings` meta. In this stack the
// only per-user field is canManageTemplatesMenu (admin true / member false;
// pinned P6.1 oracle diff).
func ExposedSettingsJSON(siteURL string, isAdmin bool) string {
	s := withSiteURL(pinned_ol_ExposedSettings, siteURL)
	if isAdmin {
		// pinned constant is the member (false) form; admin flips one field
		// (pinned P6.1 oracle diff: canManageTemplatesMenu false → true).
		s = strings.ReplaceAll(s, `"canManageTemplatesMenu":false`, `"canManageTemplatesMenu":true`)
	}
	return s
}

// OverallThemesJSON — the `ol-overallThemes` meta (static).
func OverallThemesJSON() string {
	return pinned_ol_overallThemes
}

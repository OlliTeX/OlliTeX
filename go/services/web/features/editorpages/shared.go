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
func ExposedSettingsJSON(siteURL string, isAdmin bool, wakaEnabled bool, wakaDebug bool, mendeleyEnabled bool, pandocConversions bool, githubSync bool) string {
	s := withSiteURL(pinned_ol_ExposedSettings, siteURL)
	if isAdmin {
		// pinned constant is the member (false) form; admin flips one field
		// (pinned P6.1 oracle diff: canManageTemplatesMenu false → true).
		s = strings.ReplaceAll(s, `"canManageTemplatesMenu":false`, `"canManageTemplatesMenu":true`)
	}
	// Candidate F (owner-adopted 2026-09-29): WakaTime integration gate +
	// debug logging (reference module settings surface), appended at the
	// object end (pinned constant unchanged).
	s = strings.TrimSuffix(s, `}`)
	s += `,"wakaTimeEnabled":` + (func() string {
		if wakaEnabled {
			return "true"
		}
		return "false"
	}()) +
		`,"wakaTimeDebugLogging":` + (func() string {
		if wakaDebug {
			return "true"
		}
		return "false"
	}()) + `,"mendeleyEnabled":` + (func() string {
		if mendeleyEnabled {
			return "true"
		}
		return "false"
	}()) + `,"enablePandocConversions":` + (func() string {
		if pandocConversions {
			return "true"
		}
		return "false"
	}()) + `}`
	// audit 006: flip the pinned githubSyncEnabled at runtime (the fixture
	// constant stays `false`; same approach as canManageTemplatesMenu).
	// NOTE: the value is a JSON *boolean* — no surrounding quotes; the
	// earlier version appended a stray `"` ("githubSyncEnabled":true" —
	// invalid JSON), crashing every IDE/hub load with JSON.parse
	// SyntaxError @1413 (owner WDV-F capture 2026-10-03).
	s = strings.ReplaceAll(s, `"githubSyncEnabled":false`, `"githubSyncEnabled":`+func() string {
		if githubSync {
			return "true"
		}
		return "false"
	}())
	return s
}

// splitTestVariants — audit-013: when the owner enables pandoc
// conversions, the export-docx/export-markdown/export-html split-test flags
// flip "default" → "enabled" (front-end isSplitTestEnabled requires exactly
// "enabled" to render the Export items); otherwise the pinned constants are
// served unchanged.
func SplitTestVariants(pandocConversions bool) string {
	if !pandocConversions {
		return pinned_ol_splitTestVariants
	}
	s := strings.ReplaceAll(pinned_ol_splitTestVariants, `"export-docx":"default"`, `"export-docx":"enabled"`)
	s = strings.ReplaceAll(s, `"export-markdown":"default"`, `"export-markdown":"enabled"`)
	s = strings.ReplaceAll(s, `"export-html":"default"`, `"export-html":"enabled"`)
	return s
}

// OverallThemesJSON — the `ol-overallThemes` meta (static).
func OverallThemesJSON() string {
	return pinned_ol_overallThemes
}

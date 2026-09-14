package sitesettings

import (
	"os"
	"strconv"
	"strings"
)

// seedObj mirrors SiteSettingsManager.envSeeds(env, coreSettings, stored)[name].
// "coreSettings" was resolved out: where Node falls back to a coreSettings
// value, this build has none, so the fallback is the constant Node would
// compute (pinned live against the running e2e stack).

var defaultBlockedNetworks = []any{
	"127.0.0.0/8", "169.254.0.0/16", "10.0.0.0/8", "172.16.0.0/12",
	"192.168.0.0/16", "::1/128", "fe80::/10", "fc00::/7",
}

// seedTemplateCategories — 12 manual categories + OVERLEAF_TEMPLATE_CATEGORIES
// + "all". Per-key name/description from env (TEMPLATE_<KEY>_NAME/DESCRIPTION)
// then DEFAULT. storedCat is always {} here (Node indexes a stored ARRAY by
// string key → undefined), so enabled/publishable=true and name/description
// from env/defaults. (The authoritative per-key merge is done in getSection.)
func seedTemplateCategories() Obj {
	pushKey := func(keys []string, seen map[string]bool, k string) []string {
		if k != "" && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
		return keys
	}
	seen := map[string]bool{}
	keys := []string{}
	for _, d := range defaultTemplateCategories {
		keys = pushKey(keys, seen, d.key)
	}
	if raw := os.Getenv("OVERLEAF_TEMPLATE_CATEGORIES"); raw != "" {
		for _, k := range strings.Fields(raw) {
			keys = pushKey(keys, seen, k)
		}
	}
	keys = pushKey(keys, seen, "all")

	out := Obj{}
	for _, key := range keys {
		defName, defDesc := "", ""
		for _, d := range defaultTemplateCategories {
			if d.key == key {
				defName, defDesc = d.name, d.desc
				break
			}
		}
		base := strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
		name := os.Getenv("TEMPLATE_" + base + "_NAME")
		if name == "" {
			name = defName
		}
		if name == "" {
			if key == "all" {
				name = "All templates"
			} else {
				name = key
			}
		}
		desc := os.Getenv("TEMPLATE_" + base + "_DESCRIPTION")
		if desc == "" {
			desc = defDesc
		}
		out = append(out,
			KV{"key", key},
			KV{"enabled", true}, // storedCat.enabled !== false ({} → true)
			KV{"name", name},
			KV{"description", desc},
			KV{"publishable", true}, // storedCat.publishable !== false
		)
	}
	return out
}

func boolFromEnvS(name string) (bool, bool) {
	switch os.Getenv(name) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// envIsTrue — Node `boolFromEnv(env.X) === true`: true only when env is "true".
func envIsTrue(name string) bool { return os.Getenv(name) == "true" }

func boolOrDefault(name string, fallback bool) bool {
	if b, ok := boolFromEnvS(name); ok {
		return b
	}
	return fallback
}

func seedObj(name string) Obj {
	switch name {
	case "templates":
		enabled := false
		if b, ok := boolFromEnvS("OVERLEAF_TEMPLATE_GALLERY"); ok {
			enabled = b
		}
		return Obj{
			KV{"enabled", enabled},
			KV{"categories", seedTemplateCategories()},
			KV{"allUsersCanManageTemplates", false},
		}
	case "zotero":
		enabled := false // boolFromEnv(overleaf_zotero) ?? enabledLinkedFileTypes.includes('zotero') → false here
		if b, ok := boolFromEnvS("OVERLEAF_ZOTERO"); ok {
			enabled = b
		}
		return Obj{
			KV{"enabled", enabled},
			KV{"clientKey", os.Getenv("ZOTERO_CLIENT_KEY")},
			KV{"hasEnvSecret", os.Getenv("ZOTERO_CLIENT_SECRET") != ""},
		}
	case "mendeley":
		return Obj{
			KV{"enabled", os.Getenv("MENDELEY_CLIENT_ID") != ""},
			KV{"clientId", os.Getenv("MENDELEY_CLIENT_ID")},
			KV{"hasEnvSecret", os.Getenv("MENDELEY_CLIENT_SECRET") != ""},
		}
	case "externalUrl":
		enabled := false
		if b, ok := boolFromEnvS("OVERLEAF_EXTERNAL_URLS"); ok {
			enabled = b
		}
		return Obj{
			KV{"enabled", enabled},
			KV{"blockedNetworks", defaultBlockedNetworks},
			KV{"allowedResourcesRegex", os.Getenv("OVERLEAF_LINKED_URL_ALLOWED_RESOURCES")},
		}
	case "signup":
		// boolFromEnv(OVERLEAF_ENABLE_REGISTRATION_PAGE) ?? !(ldap||saml||oidc)
		enabled := true
		if b, ok := boolFromEnvS("OVERLEAF_ENABLE_REGISTRATION_PAGE"); ok {
			enabled = b
		}
		domains := []any{}
		if raw := os.Getenv("OVERLEAF_ALLOWED_REGISTRATION_EMAIL_DOMAINS"); raw != "" {
			for _, d := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
				if d != "" {
					domains = append(domains, d)
				}
			}
		}
		return Obj{
			KV{"enabled", enabled},
			KV{"allowedEmailDomains", domains},
			KV{"disabledRedirectUrl", os.Getenv("OVERLEAF_REGISTRATION_DISABLED_REDIRECT")},
		}
	case "sso-saml", "sso-oidc", "sso-ldap":
		return Obj{KV{"enabled", false}}
	case "sandboxed-compiles":
		return Obj{
			KV{"enabled", envIsTrue("SANDBOXED_COMPILES")},
			KV{"dockerRunner", envIsTrue("DOCKER_RUNNER")},
			KV{"hostDir", strings.TrimSpace(envVarOr("SANDBOXED_COMPILES_HOST_DIR", os.Getenv("COMPILES_HOST_DIR")))},
			KV{"socketPath", os.Getenv("DOCKER_SOCKET_PATH")},
			KV{"extraFlags", os.Getenv("TEX_COMPILER_EXTRA_FLAGS")},
			KV{"imageUser", os.Getenv("TEXLIVE_IMAGE_USER")},
			KV{"images", seedSandboxedImages()},
			KV{"defaultImage", envVarOr("TEX_LIVE_DOCKER_IMAGE", "texlive/texlive:latest-full")},
		}
	case "git-integration":
		port := 8000
		if p, err := strconv.Atoi(os.Getenv("GIT_BRIDGE_PORT")); err == nil && p > 0 {
			port = p
		}
		host := os.Getenv("GIT_BRIDGE_HOST")
		if host == "" {
			host = "git-bridge"
		}
		return Obj{
			KV{"enabled", envIsTrue("GIT_BRIDGE_ENABLED")},
			KV{"host", host},
			KV{"port", int(port)},
		}
	case "typst":
		return Obj{
			KV{"enabled", os.Getenv("COMPILE_TYPEST_ENABLED") != "false"},
			KV{"url", os.Getenv("CLSI_TYPEST_URL")},
		}
	case "github-sync":
		return Obj{
			KV{"enabled", envIsTrue("GITHUB_SYNC_ENABLED")},
			KV{"clientId", os.Getenv("GITHUB_SYNC_CLIENT_ID")},
			KV{"clientSecret", os.Getenv("GITHUB_SYNC_CLIENT_SECRET")},
			KV{"cipherFile", os.Getenv("GITHUB_TOKEN_CIPHER_FILE")},
			KV{"cipherLabel", os.Getenv("GITHUB_TOKEN_CIPHER_LABEL")},
		}
	case "email":
		return emailSeeds()
	case "linked-file-types":
		forced := []any{"project_file", "project_output_file"}
		extra := []any{}
		if raw := os.Getenv("ENABLED_LINKED_FILE_TYPES"); raw != "" {
			for _, t := range strings.Split(raw, ",") {
				t = strings.TrimSpace(t)
				if t == "" {
					continue
				}
				if t == "project_file" || t == "project_output_file" {
					continue
				}
				extra = append(extra, t)
			}
		}
		out := append([]any{}, forced...)
		out = append(out, extra...)
		return Obj{KV{"enabledTypes", out}}
	case "pandoc":
		return Obj{
			KV{"enabled", envIsTrue("ENABLE_PANDOC_CONVERSIONS")},
			KV{"image", envVarOr("PANDOC_IMAGE", "pandoc-ol:3.10.0.0")},
		}
	case "webdav":
		tmo := 60000
		if p, err := strconv.Atoi(os.Getenv("WEBDAV_REQUEST_TIMEOUT_MS")); err == nil && p > 0 {
			tmo = p
		}
		rc := 2
		if s := os.Getenv("WEBDAV_RETRY_COUNT"); s != "" {
			if p, err := strconv.Atoi(s); err == nil {
				rc = p
			}
		}
		rd := 500
		if s := os.Getenv("WEBDAV_RETRY_DELAY_MS"); s != "" {
			if p, err := strconv.Atoi(s); err == nil {
				rd = p
			}
		}
		return Obj{
			KV{"enabled", envIsTrue("WEBDAV_ENABLED")},
			KV{"rootPath", envVarOr("WEBDAV_ROOT_PATH", "/Overleaf")},
			KV{"requestTimeoutMs", int(tmo)},
			KV{"retryCount", int(rc)},
			KV{"retryDelayMs", int(rd)},
			KV{"cipherLabel", os.Getenv("WEBDAV_TOKEN_CIPHER_LABEL")},
			KV{"cipherPassword", os.Getenv("WEBDAV_TOKEN_CIPHER_PASSWORD")},
			KV{"previousCipherLabel", os.Getenv("WEBDAV_TOKEN_CIPHER_PREVIOUS_LABEL")},
			KV{"previousCipherPassword", os.Getenv("WEBDAV_TOKEN_CIPHER_PREVIOUS_PASSWORD")},
		}
	case "dropbox":
		return Obj{
			KV{"enabled", envIsTrue("DROPBOX_ENABLED")},
			KV{"appKey", os.Getenv("DROPBOX_APP_KEY")},
			KV{"appSecret", os.Getenv("DROPBOX_APP_SECRET")},
		}
	case "misc":
		appName := os.Getenv("APP_NAME")
		if appName == "" {
			appName = "Overleaf (Community Edition)"
		}
		return Obj{
			KV{"appName", appName},
			KV{"navHidePoweredBy", boolOrDefault("NAV_HIDE_POWERED_BY", false)},
			KV{"robotsNoindex", boolOrDefault("ROBOTS_NOINDEX", false)},
			KV{"allowPublicAccess", boolOrDefault("OVERLEAF_ALLOW_PUBLIC_ACCESS", false)},
			KV{"allowAnonymousReadWriteSharing", boolOrDefault("OVERLEAF_ALLOW_ANONYMOUS_READ_AND_WRITE_SHARING", false)},
			KV{"disableLinkSharing", boolOrDefault("OVERLEAF_DISABLE_LINK_SHARING", false)},
			KV{"disableChat", boolOrDefault("OVERLEAF_DISABLE_CHAT", false)},
			KV{"projectHardDeletionDelayDays", delayDaysFromMs("OVERLEAF_PROJECT_HARD_DELETION_DELAY", 90)},
			KV{"userHardDeletionDelayDays", delayDaysFromMs("OVERLEAF_USER_HARD_DELETION_DELAY", 90)},
			KV{"historyRestore", boolOrDefault("OVERLEAF_HISTORY_RESTORE", false)},
			KV{"enablePdfCaching", boolOrDefault("ENABLE_PDF_CACHING", true)},
			KV{"pythonRunner", boolOrDefault("ENABLE_PYTHON_RUNNER", false)},
			KV{"maxUploadSizeMiB", intEnvOr50(os.Getenv("MAX_UPLOAD_SIZE"))},
			KV{"maxEntitiesPerProject", intEnvOr2000(os.Getenv("MAX_ENTITIES_PER_PROJECT"))},
			KV{"defaultLatexCompiler", envVarOr("DEFAULT_LATEX_COMPILER", "pdflatex")},
		}
	}
	// languagetool / llm / branding / services / storage → no env seeds.
	return Obj{}
}

// envVarOr(name, def) — Node \`env.<NAME> || <def>\`: the first non-empty
// value of environment variable NAME, otherwise def. def may itself be a
// resolved inner chain (envVarOr("B",…)) or a literal default, mirroring
// `env.OVERLEAF_X || env.X || 'default'`.
func envVarOr(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

func intEnvOr50(raw string) int {
	if raw == "" {
		return 50
	}
	if n, err := strconv.Atoi(raw); err == nil && n != 0 {
		return n
	}
	return 50
}

func intEnvOr2000(raw string) int {
	if raw == "" {
		return 2000
	}
	if n, err := strconv.Atoi(raw); err == nil && n != 0 {
		return n
	}
	return 2000
}

func seedSandboxedImages() []any {
	list := []string{}
	for _, s := range strings.Split(os.Getenv("ALL_TEX_LIVE_DOCKER_IMAGES"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			list = append(list, s)
		}
	}
	if len(list) > 0 {
		names := []string{}
		for _, s := range strings.Split(os.Getenv("ALL_TEX_LIVE_DOCKER_IMAGE_NAMES"), ",") {
			names = append(names, strings.TrimSpace(s))
		}
		out := make([]any, 0, len(list))
		for i, image := range list {
			n := ""
			if i < len(names) {
				n = names[i]
			}
			out = append(out, Obj{KV{"image", image}, KV{"name", n}})
		}
		return out
	}
	return []any{
		Obj{KV{"image", "texlive/texlive:latest-full"}, KV{"name", "TeXLive 2025"}},
		Obj{KV{"image", "texlive/texlive:TL2024-historic"}, KV{"name", "TeXLive 2024"}},
		Obj{KV{"image", "texlive/texlive:TL2023-historic"}, KV{"name", "TeXLive 2023"}},
	}
}

// emailSeeds — envSeeds.email (long OVERLEAF_EMAIL_* first, short EMAIL_*
// fallback; coreSettings.email defaults folded in — resolved to the e2e
// values where env is absent).
func emailSeeds() Obj {
	driver := envVarOr("OVERLEAF_EMAIL_DRIVER", envVarOr("EMAIL_DRIVER", "smtp"))
	port := 587
	if raw := envVarOr("OVERLEAF_EMAIL_SMTP_PORT", os.Getenv("EMAIL_PORT")); raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			port = p
		}
	}
	return Obj{
		KV{"skipConfirmation", envIsTrue("EMAIL_CONFIRMATION_DISABLED")},
		KV{"fromAddress", envVarOr("OVERLEAF_EMAIL_FROM_ADDRESS", envVarOr("EMAIL_FROM_ADDRESS", ""))},
		KV{"replyTo", envVarOr("OVERLEAF_EMAIL_REPLY_TO", envVarOr("EMAIL_REPLY_TO", ""))},
		KV{"driver", driver},
		KV{"host", envVarOr("OVERLEAF_EMAIL_SMTP_HOST", envVarOr("EMAIL_HOST", ""))},
		KV{"port", int(port)},
		KV{"secure", envIsTrue("OVERLEAF_EMAIL_SMTP_SECURE") || envIsTrue("EMAIL_SECURE")},
		KV{"ignoreTLS", envIsTrue("OVERLEAF_EMAIL_SMTP_IGNORE_TLS") || envIsTrue("EMAIL_IGNORE_TLS")},
		KV{"name", envVarOr("OVERLEAF_EMAIL_SMTP_NAME", envVarOr("EMAIL_NAME", ""))},
		KV{"user", envVarOr("OVERLEAF_EMAIL_SMTP_USER", envVarOr("EMAIL_USER", ""))},
		KV{"pass", envVarOr("OVERLEAF_EMAIL_SMTP_PASS", envVarOr("EMAIL_PASS", ""))},
		KV{"tlsRejectUnauth", envIsTrue("EMAIL_TLS_REJECT_UNAUTHORIZED")},
		KV{"accessKeyId", envVarOr("OVERLEAF_EMAIL_AWS_SES_ACCESS_KEY_ID", envVarOr("EMAIL_SES_ACCESS_KEY_ID", ""))},
		KV{"sesSecret", envVarOr("OVERLEAF_EMAIL_AWS_SES_SECRET_KEY", envVarOr("EMAIL_SES_SECRET_ACCESS_KEY", ""))},
		KV{"sesRegion", envVarOr("OVERLEAF_EMAIL_AWS_SES_REGION", envVarOr("EMAIL_SES_REGION", ""))},
	}
}

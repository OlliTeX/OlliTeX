// Package configschema is the single source of truth for the OlliTeX
// runtime configuration key space (D6, 2026-09-25): the SQLite config DB
// holds EVERY parameter except the DB encryption key itself. This registry
// is consumed by:
//
//   - cmd/configdb       (CLI: list/set/get/import-env/init; the emergency
//     path when /hub cannot start)
//   - tools/toolkit bin/config (host-side wrapper around the CLI)
//   - go/services/web   (LoadConfig + /api/hub/config: DB value wins, then
//     the legacy env, then the default)
//
// Each entry: canonical KEY (== the SQLite key; == the legacy docker env
// name for a 1:1 bootstrap import), KIND (string|bool|int), SECRET (masked
// in listings; stored field-encrypted when a key is active), GROUP (for the
// /hub admin cards), DEFAULT (what Node/the Go code falls back to) and a
// short DESCRIPTION.
//
// Groups (ordered as the /hub admin would show them): core, boot,
// security, email, integrations, compilation, limits, services, test.
package configschema

// Kind is the value kind of a parameter.
type Kind string

const (
	KString Kind = "string"
	KBool   Kind = "bool"
	KInt    Kind = "int"
)

// Param describes one config parameter.
type Param struct {
	Key         string // canonical config key (SQLite key; legacy env name)
	Kind        Kind
	Secret      bool // masked in listings; field-encrypted in the store
	Group       string
	Default     string
	Description string
}

// Registry is the complete key space. Keep it sorted by group, then key —
// `configdb list` and the /hub cards render in this order.
var Registry = []Param{
	// ---------- core (site identity & top-level toggles) ----------
	{Key: "APP_NAME", Kind: KString, Group: "core", Default: "OlliTeX", Description: "Product name shown in the UI, e-mails and titles."},
	{Key: "SITE_URL", Kind: KString, Group: "core", Description: "Public base URL (links, e-mails, redirects)."},
	{Key: "ENABLED_SERVICES", Kind: KString, Group: "core", Default: "web,api", Description: "Which server profiles run: web and/or api."},
	{Key: "ALLOWED_ORIGINS", Kind: KString, Group: "core", Description: "Comma-separated allowed CORS origins."},
	{Key: "ADMIN_PRIVILEGE_AVAILABLE", Kind: KBool, Group: "core", Default: "false", Description: "Enable the site-admin privilege."},
	{Key: "SITE_OPEN", Kind: KBool, Group: "core", Default: "true", Description: "Site-wide 'open' toggle (false = closed-notice page)."},
	{Key: "EDITOR_OPEN", Kind: KBool, Group: "core", Default: "true", Description: "Editor-open toggle (false = closed-notice for editor loads)."},
	{Key: "EXPOSE_HOSTNAME", Kind: KBool, Group: "core", Default: "false", Description: "Expose the hostname in responses."},
	{Key: "OVERLEAF_ALLOW_PUBLIC_ACCESS", Kind: KBool, Group: "core", Default: "false", Description: "Allow anonymous public access."},
	{Key: "OVERLEAF_DISABLE_CHAT", Kind: KBool, Group: "core", Default: "false", Description: "Disable built-in chat."},
	{Key: "OVERLEAF_DISABLE_LINK_SHARING", Kind: KBool, Group: "core", Default: "false", Description: "Disable link sharing."},
	{Key: "OVERLEAF_ALLOW_ANONYMOUS_READ_AND_WRITE_SHARING", Kind: KBool, Group: "core", Default: "false", Description: "Allow anonymous read+write sharing."},
	{Key: "OVERLEAF_HISTORY_RESTORE", Kind: KBool, Group: "core", Default: "false", Description: "Enable history restore in the editor."},
	{Key: "NAV_HIDE_POWERED_BY", Kind: KBool, Group: "core", Default: "false", Description: "Hide 'Powered by' in the nav."},
	{Key: "ROBOTS_NOINDEX", Kind: KBool, Group: "core", Default: "false", Description: "Send robots noindex on anonymous pages."},
	{Key: "ADDITIONAL_TEXT_EXTENSIONS", Kind: KString, Group: "core", Description: "Extra file extensions treated as text."},
	{Key: "ADMIN_EMAIL", Kind: KString, Group: "core", Description: "Site-admin contact address (e-mails, footer)."},
	{Key: "CACHE_STATIC_ASSETS", Kind: KBool, Group: "core", Default: "true", Description: "Etag/cache static assets."},
	{Key: "COOKIE_DOMAIN", Kind: KString, Group: "core", Description: "Cookie domain (security-relevant, usually a deploy constant)."},
	{Key: "COOKIE_ROLLING_SESSION", Kind: KBool, Group: "core", Default: "true", Description: "Roll (slide) the session expiry."},
	{Key: "COOKIE_SESSION_LENGTH", Kind: KInt, Group: "core", Default: "604800", Description: "Session cookie lifetime (seconds)."},

	// ---------- boot (process/paths; usually fixed per install) ----------
	{Key: "OVERLEAF_HOME", Kind: KString, Group: "boot", Description: "Install home dir (logs, configdb, volumes)."},
	{Key: "CONFIG_DB_PATH", Kind: KString, Group: "boot", Description: "Explicit SQLite config-DB file path."},
	{Key: "WEB_GO_PUBLIC_DIR", Kind: KString, Group: "boot", Description: "Baked frontend asset dir for the Go web."},
	{Key: "NODE_ENV", Kind: KString, Group: "boot", Default: "production", Description: "Runtime environment label."},
	{Key: "OVERLEAF_STORAGE_ENV_FILE", Kind: KString, Group: "boot", Description: "Storage (persistor) env file used by filestore/blob backends."},
	{Key: "RELEASE_NOTES_PATH", Kind: KString, Group: "boot", Description: "Release-notes path surfaced in the UI."},

	// ---------- data + services (infrastructure endpoints) ----------
	{Key: "OVERLEAF_MONGO_URL", Kind: KString, Secret: true, Group: "services", Description: "MongoDB connection string (may embed credentials)."},
	{Key: "DOCSTORE_HOST", Kind: KString, Group: "services", Description: "docstore service host."},
	{Key: "WEB_DOCSTORE_URL", Kind: KString, Group: "services", Description: "docstore URL as seen by the web."},
	{Key: "DOCUMENT_UPDATER_HOST", Kind: KString, Group: "services", Description: "document-updater service host."},
	{Key: "WEB_DOCUMENT_UPDATER_URL", Kind: KString, Group: "services", Description: "document-updater URL as seen by the web."},
	{Key: "WEB_DOCUPDATER_URL", Kind: KString, Group: "services", Description: "document-updater URL (legacy alias)."},
	{Key: "WEB_FILESTORE_URL", Kind: KString, Group: "services", Description: "filestore (S3/TPDS) URL."},
	{Key: "PROJECT_HISTORY_HOST", Kind: KString, Group: "services", Description: "project-history host."},
	{Key: "PROJECT_HISTORY_URL", Kind: KString, Group: "services", Description: "project-history URL."},
	{Key: "WEB_PROJECT_HISTORY_URL", Kind: KString, Group: "services", Description: "project-history URL as seen by the web."},
	{Key: "REALTIME_URL", Kind: KString, Group: "services", Description: "real-time (socket) URL."},
	{Key: "V1_HISTORY_URL", Kind: KString, Group: "services", Description: "history-v1 URL (e.g. http://127.0.0.1:3100/api)."},
	{Key: "WEB_V1_HISTORY_URL", Kind: KString, Group: "services", Description: "history-v1 URL as seen by the web."},
	{Key: "APIS_V1_URL", Kind: KString, Group: "services", Description: "v1 API base URL surface."},
	{Key: "CLSI_LB_HOST", Kind: KString, Group: "services", Description: "compile-server (clsi) load-balancer host."},
	{Key: "CLSI_TYPEST_URL", Kind: KString, Group: "services", Description: "clsi_typst URL."},
	{Key: "WEB_CHAT_URL", Kind: KString, Group: "services", Description: "chat service URL."},
	{Key: "GIT_BRIDGE_HOST", Kind: KString, Group: "services", Default: "git-bridge", Description: "git-bridge service host."},
	{Key: "GIT_BRIDGE_PORT", Kind: KInt, Group: "services", Default: "8000", Description: "git-bridge service port."},
	{Key: "GIT_BRIDGE_ENABLED", Kind: KBool, Group: "services", Default: "false", Description: "Enable Git integration."},
	{Key: "OVERLEAF_GITBRIDGE_ENABLED", Kind: KBool, Group: "services", Default: "false", Description: "Enable Git integration (alternate switch name)."},

	// ---------- email ----------
	{Key: "OVERLEAF_EMAIL_DRIVER", Kind: KString, Group: "email", Default: "smtp", Description: "Mail driver (smtp|ses)."},
	{Key: "OVERLEAF_EMAIL_SMTP_HOST", Kind: KString, Group: "email", Description: "SMTP host (or SES endpoint)."},
	{Key: "OVERLEAF_EMAIL_SMTP_PORT", Kind: KInt, Group: "email", Default: "587", Description: "SMTP port."},
	{Key: "OVERLEAF_EMAIL_FROM_ADDRESS", Kind: KString, Group: "email", Description: "From address for outbound mail."},
	{Key: "OVERLEAF_EMAIL_REPLY_TO", Kind: KString, Group: "email", Description: "Reply-To address."},
	{Key: "OVERLEAF_EMAIL_SMTP_NAME", Kind: KString, Group: "email", Description: "SMTP display name."},
	{Key: "OVERLEAF_EMAIL_SMTP_USER", Kind: KString, Group: "email", Description: "SMTP user."},
	{Key: "OVERLEAF_EMAIL_SMTP_PASS", Kind: KString, Secret: true, Group: "email", Description: "SMTP password (field-encrypted at rest)."},
	{Key: "OVERLEAF_EMAIL_SMTP_SECURE", Kind: KBool, Group: "email", Default: "false", Description: "SMTP TLS (secure=true)."},
	{Key: "OVERLEAF_EMAIL_SMTP_IGNORE_TLS", Kind: KBool, Group: "email", Default: "false", Description: "Ignore TLS verification (insecure relays)."},
	{Key: "EMAIL_TLS_REJECT_UNAUTHORIZED", Kind: KBool, Group: "email", Default: "true", Description: "Reject unauthorized TLS certificates."},
	{Key: "EMAIL_CONFIRMATION_DISABLED", Kind: KBool, Group: "email", Default: "false", Description: "Skip account-confirmation e-mail."},
	{Key: "OVERLEAF_EMAIL_AWS_SES_ACCESS_KEY_ID", Kind: KString, Secret: true, Group: "email", Description: "AWS SES access key id."},
	{Key: "OVERLEAF_EMAIL_AWS_SES_SECRET_KEY", Kind: KString, Secret: true, Group: "email", Description: "AWS SES secret key."},
	{Key: "OVERLEAF_EMAIL_AWS_SES_REGION", Kind: KString, Group: "email", Description: "AWS SES region."},

	// ---------- integrations (reference managers, sync, external) ----------
	{Key: "OVERLEAF_ZOTERO", Kind: KBool, Group: "integrations", Default: "false", Description: "Enable Zotero integration."},
	{Key: "ZOTERO_CLIENT_KEY", Kind: KString, Secret: true, Group: "integrations", Description: "Zotero client key."},
	{Key: "ZOTERO_CLIENT_SECRET", Kind: KString, Secret: true, Group: "integrations", Description: "Zotero client secret."},
	{Key: "MENDELEY_CLIENT_ID", Kind: KString, Secret: true, Group: "integrations", Description: "Mendeley client id (present => enabled)."},
	{Key: "MENDELEY_CLIENT_SECRET", Kind: KString, Secret: true, Group: "integrations", Description: "Mendeley client secret."},
	{Key: "MENDELEY_CIPHER_LABEL", Kind: KString, Group: "integrations", Description: "Mendeley token cipher label."},
	{Key: "MENDELEY_CIPHER_PASSWORD", Kind: KString, Secret: true, Group: "integrations", Description: "Mendeley token cipher password."},
	{Key: "GITHUB_SYNC_ENABLED", Kind: KBool, Group: "integrations", Default: "false", Description: "Enable GitHub sync."},
	{Key: "GITHUB_SYNC_CLIENT_ID", Kind: KString, Secret: true, Group: "integrations", Description: "GitHub OAuth client id."},
	{Key: "GITHUB_SYNC_CLIENT_SECRET", Kind: KString, Secret: true, Group: "integrations", Description: "GitHub OAuth client secret."},
	{Key: "GITHUB_TOKEN_CIPHER_FILE", Kind: KString, Group: "integrations", Description: "GitHub token cipher file."},
	{Key: "GITHUB_TOKEN_CIPHER_LABEL", Kind: KString, Group: "integrations", Description: "GitHub token cipher label."},
	{Key: "DROPBOX_ENABLED", Kind: KBool, Group: "integrations", Default: "false", Description: "Enable Dropbox sync."},
	{Key: "DROPBOX_APP_KEY", Kind: KString, Secret: true, Group: "integrations", Description: "Dropbox app key."},
	{Key: "DROPBOX_APP_SECRET", Kind: KString, Secret: true, Group: "integrations", Description: "Dropbox app secret."},
	{Key: "WEBDAV_ENABLED", Kind: KBool, Group: "integrations", Default: "false", Description: "Enable WebDAV sync."},
	{Key: "WEBDAV_ROOT_PATH", Kind: KString, Group: "integrations", Default: "/Overleaf", Description: "WebDAV root path."},
	{Key: "WEBDAV_REQUEST_TIMEOUT_MS", Kind: KInt, Group: "integrations", Default: "60000", Description: "WebDAV request timeout (ms)."},
	{Key: "WEBDAV_RETRY_COUNT", Kind: KInt, Group: "integrations", Default: "2", Description: "WebDAV retry count."},
	{Key: "WEBDAV_RETRY_DELAY_MS", Kind: KInt, Group: "integrations", Default: "500", Description: "WebDAV retry delay (ms)."},
	{Key: "WEBDAV_TOKEN_CIPHER_LABEL", Kind: KString, Group: "integrations", Description: "WebDAV token cipher label."},
	{Key: "WEBDAV_TOKEN_CIPHER_PASSWORD", Kind: KString, Secret: true, Group: "integrations", Description: "WebDAV token cipher password."},
	{Key: "WEBDAV_TOKEN_CIPHER_PREVIOUS_LABEL", Kind: KString, Group: "integrations", Description: "WebDAV previous cipher label (rotation)."},
	{Key: "WEBDAV_TOKEN_CIPHER_PREVIOUS_PASSWORD", Kind: KString, Secret: true, Group: "integrations", Description: "WebDAV previous cipher password (rotation)."},
	{Key: "OVERLEAF_EXTERNAL_URLS", Kind: KBool, Group: "integrations", Default: "false", Description: "Enable linked (external) file types."},
	{Key: "OVERLEAF_LINKED_URL_ALLOWED_RESOURCES", Kind: KString, Group: "integrations", Description: "Regex for allowed external resources."},
	{Key: "ENABLED_LINKED_FILE_TYPES", Kind: KString, Group: "integrations", Description: "Extra enabled linked-file types (comma list)."},

	// ---------- compilation ----------
	{Key: "DEFAULT_LATEX_COMPILER", Kind: KString, Group: "compilation", Default: "pdflatex", Description: "Default LaTeX compiler."},
	{Key: "TEX_COMPILER_EXTRA_FLAGS", Kind: KString, Group: "compilation", Description: "Extra compiler flags."},
	{Key: "SANDBOXED_COMPILES", Kind: KBool, Group: "compilation", Default: "false", Description: "Enable sandboxed (docker) compiles."},
	{Key: "DOCKER_RUNNER", Kind: KBool, Group: "compilation", Default: "false", Description: "Use the docker compile runner."},
	{Key: "SANDBOXED_COMPILES_HOST_DIR", Kind: KString, Group: "compilation", Description: "Host dir for sandboxed compiles."},
	{Key: "COMPILES_HOST_DIR", Kind: KString, Group: "compilation", Description: "Compile host dir (fallback of SANDBOXED_COMPILES_HOST_DIR)."},
	{Key: "DOCKER_SOCKET_PATH", Kind: KString, Group: "compilation", Description: "Docker socket path."},
	{Key: "TEXLIVE_IMAGE_USER", Kind: KString, Group: "compilation", Description: "User inside the texlive image."},
	{Key: "ALL_TEX_LIVE_DOCKER_IMAGES", Kind: KString, Group: "compilation", Description: "Comma list of TeXLive docker images."},
	{Key: "ALL_TEX_LIVE_DOCKER_IMAGE_NAMES", Kind: KString, Group: "compilation", Description: "Comma list of display names for the images."},
	{Key: "TEX_LIVE_DOCKER_IMAGE", Kind: KString, Group: "compilation", Default: "texlive/texlive:latest-full", Description: "Default TeXLive image."},
	{Key: "COMPILE_TYPEST_ENABLED", Kind: KBool, Group: "compilation", Default: "true", Description: "Enable Typst compilation."},
	{Key: "ENABLE_PANDOC_CONVERSIONS", Kind: KBool, Group: "compilation", Default: "false", Description: "Enable pandoc (Markdown→LaTeX) conversions."},
	{Key: "PANDOC_IMAGE", Kind: KString, Group: "compilation", Default: "pandoc-ol:3.10.0.0", Description: "Pandoc compile image."},
	{Key: "FILE_IGNORE_PATTERN", Kind: KString, Group: "compilation", Description: "File ignore pattern for sync/compile."},
	{Key: "ENABLE_PYTHON_RUNNER", Kind: KBool, Group: "compilation", Default: "false", Description: "Enable the Python runner."},
	{Key: "ENABLE_PDF_CACHING", Kind: KBool, Group: "compilation", Default: "true", Description: "Enable PDF caching."},

	// ---------- limits & retention ----------
	{Key: "MAX_UPLOAD_SIZE", Kind: KInt, Group: "limits", Default: "50", Description: "Max upload size (MiB)."},
	{Key: "MAX_ENTITIES_PER_PROJECT", Kind: KInt, Group: "limits", Default: "2000", Description: "Max files per project."},
	{Key: "INSTANCE_STATS_RETENTION_DAYS", Kind: KInt, Group: "limits", Description: "Instance-stats retention (days)."},
	{Key: "OVERLEAF_BIB_LIBRARY_TRASH_RETENTION_DAYS", Kind: KInt, Group: "limits", Description: "Reference-library trash retention (days)."},
	{Key: "OVERLEAF_PROJECT_HARD_DELETION_DELAY", Kind: KInt, Group: "limits", Default: "7776000000", Description: "Project hard-deletion delay (ms)."},
	{Key: "OVERLEAF_USER_HARD_DELETION_DELAY", Kind: KInt, Group: "limits", Default: "7776000000", Description: "User hard-deletion delay (ms)."},

	// ---------- signup / SSO (identity) ----------
	{Key: "OVERLEAF_ENABLE_REGISTRATION_PAGE", Kind: KBool, Group: "core", Default: "true", Description: "Enable the self-registration page."},
	{Key: "OVERLEAF_ALLOWED_REGISTRATION_EMAIL_DOMAINS", Kind: KString, Group: "core", Description: "Allowed registration email domains (comma/space list)."},
	{Key: "OVERLEAF_REGISTRATION_DISABLED_REDIRECT", Kind: KString, Group: "core", Description: "Redirect when registration is disabled."},
	{Key: "OVERLEAF_TEMPLATES_USER_ID", Kind: KString, Group: "core", Description: "Template-gallery owner user id."},
	{Key: "OVERLEAF_TEMPLATE_CATEGORIES", Kind: KString, Group: "core", Description: "Extra template categories (space list)."},
	{Key: "OVERLEAF_TEMPLATE_GALLERY", Kind: KBool, Group: "core", Default: "false", Description: "Enable the template gallery."},

	// ---------- LLM / AI ----------
	{Key: "LLM_ENABLED", Kind: KBool, Group: "core", Description: "Enable LLM features."},
	{Key: "LLM_ADMIN_SETTINGS_PATH", Kind: KString, Group: "core", Description: "LLM admin settings path."},
	{Key: "LANGUAGE_TOOL_LEVEL", Kind: KString, Group: "core", Description: "LanguageTool integration level."},
	{Key: "LANGUAGE_TOOL_HOST", Kind: KString, Group: "services", Description: "LanguageTool host."},
	{Key: "LANGUAGE_TOOL_PORT", Kind: KInt, Group: "services", Description: "LanguageTool port."},
	{Key: "LANGUAGE_TOOL_URL", Kind: KString, Group: "services", Description: "LanguageTool base URL."},

	// ---------- security (credentials & cipher material — field-encrypted) ----------
	{Key: "OVERLEAF_SESSION_SECRET", Kind: KString, Secret: true, Group: "security", Description: "Session/cookie encryption secret."},
	{Key: "CRYPTO_RANDOM", Kind: KString, Secret: true, Group: "security", Description: "Random crypto material (legacy secret)."},
	{Key: "SESSION_SECRET_UPCOMING", Kind: KString, Secret: true, Group: "security", Description: "Upcoming session secret (rotation)."},
	{Key: "SESSION_SECRET_FALLBACK", Kind: KString, Secret: true, Group: "security", Description: "Fallback session secret (rotation)."},
	{Key: "WEB_API_USER", Kind: KString, Secret: true, Group: "security", Description: "Internal web↔api basic-auth user."},
	{Key: "WEB_API_PASSWORD", Kind: KString, Secret: true, Group: "security", Description: "Internal web↔api basic-auth password."},
	{Key: "STAGING_PASSWORD", Kind: KString, Secret: true, Group: "security", Description: "history-v1 basic-auth password."},
	{Key: "OT_JWT_AUTH_KEY", Kind: KString, Secret: true, Group: "security", Description: "history-v1 JWT signing key."},
	{Key: "OT_JWT_AUTH_OLD_KEY", Kind: KString, Secret: true, Group: "security", Description: "history-v1 JWT old signing key (rotation)."},
	{Key: "OT_JWT_AUTH_ALG", Kind: KString, Group: "security", Default: "HS256", Description: "history-v1 JWT algorithm."},
	{Key: "V1_HISTORY_USER", Kind: KString, Secret: true, Group: "security", Description: "history-v1 basic-auth user."},
	{Key: "V1_HISTORY_PASSWORD", Kind: KString, Secret: true, Group: "security", Description: "history-v1 basic-auth password (web side)."},
	{Key: "REALTIME_USER", Kind: KString, Secret: true, Group: "security", Description: "real-time API user."},
	{Key: "REALTIME_PASS", Kind: KString, Secret: true, Group: "security", Description: "real-time API password."},
	{Key: "SECRET_TOKEN", Kind: KString, Secret: true, Group: "security", Description: "Generic secret token."},
	{Key: "OVERLEAF_INVITE_TOKEN_SECRET", Kind: KString, Secret: true, Group: "security", Description: "Invite token signing secret."},
	{Key: "TOKEN_CIPHER_FILE", Kind: KString, Group: "security", Description: "Token cipher file."},
	{Key: "TOKEN_CIPHER_LABEL", Kind: KString, Group: "security", Description: "Token cipher label."},
	{Key: "TOKEN_CIPHER_PASSWORD", Kind: KString, Secret: true, Group: "security", Description: "Token cipher password."},
	{Key: "SITE_SETTINGS_CIPHER_FILE", Kind: KString, Group: "security", Description: "Site-settings cipher file."},
	{Key: "SITE_SETTINGS_CIPHER_LABEL", Kind: KString, Group: "security", Description: "Site-settings cipher label."},
	{Key: "BCRYPT_ROUNDS", Kind: KInt, Group: "security", Description: "bcrypt cost rounds."},

	// ---------- test ----------
	{Key: "SMOKE_TEST_USER_ID", Kind: KString, Group: "test", Description: "Smoke-test user id (dev)."},
	{Key: "WEB_GO_DEBUG_AUTH", Kind: KString, Group: "test", Description: "Go web debug auth override (dev)."},
}

// Find returns the param for key (case-sensitive exact match).
func Find(key string) (Param, bool) {
	for _, p := range Registry {
		if p.Key == key {
			return p, true
		}
	}
	return Param{}, false
}

// Groups returns the ordered, de-duplicated group list (registration order).
func Groups() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range Registry {
		if !seen[p.Group] {
			seen[p.Group] = true
			out = append(out, p.Group)
		}
	}
	return out
}

// IsSecret reports whether key is registered and secret.
func IsSecret(key string) bool {
	p, ok := Find(key)
	return ok && p.Secret
}

// Known reports whether key is in the registry.
func Known(key string) bool {
	_, ok := Find(key)
	return ok
}

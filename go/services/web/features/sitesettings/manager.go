package sitesettings

import (
	"os"
	"strconv"
)

// SECRET_FIELDS — in-section field names holding encrypted values
// (SiteSettingsManager.SECRET_FIELDS), in the order used for masking.
var SECRET_FIELDS = map[string][]string{
	"zotero":        {"clientSecret"},
	"mendeley":      {"clientSecret"},
	"sso-saml":      {"idpCert", "privateKey", "decryptionPvk"},
	"sso-oidc":      {"clientSecret"},
	"sso-ldap":      {"bindCredentials"},
	"github-sync":   {"clientSecret"},
	"email":         {"pass", "sesSecret"},
	"storage":       {"s3Secret"},
	"webdav":        {"cipherPassword", "previousCipherPassword"},
	"dropbox":       {"appSecret"},
}

// SECTION_KNOWN_KEYS — cleanSectionInput allow-list (unknown keys dropped).
var SECTION_KNOWN_KEYS = map[string][]string{
	"templates":         {"enabled", "categories", "allUsersCanManageTemplates", "nonAdminCanPublishTemplates"},
	"zotero":            {"enabled", "clientKey", "clientSecret"},
	"mendeley":          {"enabled", "clientId", "clientSecret"},
	"externalUrl":       {"enabled", "blockedNetworks", "allowedResourcesRegex"},
	"signup":            {"enabled", "allowedEmailDomains", "disabledRedirectUrl"},
	"sso-saml":          {"enabled", "identityServiceName", "issuer", "entryPoint", "audience", "callbackURL", "idpCert", "privateKey", "decryptionPvk", "wantAssertionsSigned"},
	"sso-oidc":          {"enabled", "identityServiceName", "issuer", "authorizationURL", "tokenURL", "userInfoURL", "clientID", "clientSecret", "scope", "logoutURL"},
	"sso-ldap":          {"enabled", "identityServiceName", "url", "searchBase", "bindDN", "bindCredentials", "searchFilter", "searchScope", "placeholder", "emailAtt", "firstNameAtt", "lastNameAtt", "isAdminAtt", "updateUserDetailsOnLogin", "timeout"},
	"sandboxed-compiles": {"enabled", "dockerRunner", "hostDir", "socketPath", "extraFlags", "imageUser", "defaultImage", "images", "names", "compileBodySizeLimitMb"},
	"git-integration":   {"enabled", "host", "port"},
	"typst":             {"enabled", "url"},
	"github-sync":       {"enabled", "clientId", "clientID", "clientSecret", "cipherFile", "cipherLabel"},
	"email":             {"driver", "host", "port", "secure", "user", "pass", "fromAddress", "skipConfirmation", "sesRegion", "sesSecret", "adminEmail", "customFooter"},
	"linked-file-types": {"enabledTypes"},
	"pandoc":            {"enabled", "image"},
	"webdav":            {"enabled", "rootPath", "requestTimeoutMs", "retryCount", "retryDelayMs", "cipherLabel", "cipherPassword", "previousCipherLabel", "previousCipherPassword"},
	"dropbox":           {"enabled", "appKey", "appSecret"},
	"misc":              {"appName", "navHidePoweredBy", "robotsNoindex", "allowPublicAccess", "allowAnonymousReadWriteSharing", "disableLinkSharing", "disableChat", "projectHardDeletionDelayDays", "userHardDeletionDelayDays", "historyRestore", "enablePdfCaching", "pythonRunner", "maxUploadSizeMiB", "maxEntitiesPerProject", "defaultLatexCompiler", "projectChangeNotificationDelayMs"},
	"languagetool":      {"enabled", "url"},
	"llm":               {"enabled", "allowUserSettings", "userRatePerMinute", "adminRatePerMinute", "userDailyTokens"},
	"branding":          {"navTitle", "leftFooter", "rightFooter"},
	"services":          {"v1HistoryUrl", "githubInterfaceUrl", "githubInterfaceWorkdirRoot", "webdavInterfaceUrl", "dropboxInterfaceUrl", "dataManipulatorUrl"},
	"storage":           {"backend", "s3Endpoint", "s3AccessKeyId", "s3Secret", "templateFilesBucket", "projectBlobsBucket", "globalBlobsBucket", "docstoreArchiveBucket"},
}

// The ordered section list of the GET response (Node Promise.all order).
var GET_SECTION_ORDER = []string{
	"templates", "zotero", "mendeley", "externalUrl", "signup",
	"sso-saml", "sso-oidc", "sso-ldap", "sandboxed-compiles", "git-integration",
	"typst", "github-sync", "email", "linked-file-types", "pandoc", "webdav",
	"dropbox", "misc", "languagetool", "llm", "branding", "services", "storage",
}

// defaultTemplateCategories — the manual's 12 example categories (node).
type tplCat struct{ key, name, desc string }

var defaultTemplateCategories = []tplCat{
	{"academic-journal", "Academic journals", "Templates for writing academic journal articles."},
	{"book", "Books", "Templates for writing books and book chapters."},
	{"presentation", "Presentations", "Templates for creating slide decks and presentations."},
	{"poster", "Posters", "Templates for conference and research posters."},
	{"cv", "CVs", "Templates for writing curricula vitae and r\u00e9sum\u00e9s."},
	{"homework", "Homework", "Templates for student assignments and homework."},
	{"bibliography", "Bibliographies", "Templates for managing and formatting bibliography."},
	{"calendar", "Calendars", "Templates for printable and digital calendars."},
	{"formal-letter", "Formal letters", "Templates for writing formal and business letters."},
	{"report", "Reports", "Templates for business, lab and project reports."},
	{"thesis", "Theses", "Templates for writing theses and dissertations, following institutional formatting and citation guidelines."},
	{"newsletter", "Newsletters", "Templates for creating newsletters and bulletins."},
}

// ---- env helpers (match envSeeds semantics) ----

func boolFromEnv(name string) (bool, bool) {
	v := os.Getenv(name)
	if v == "true" {
		return true, true
	}
	if v == "false" {
		return false, true
	}
	return false, false
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func delayDaysFromMs(envName string, defaultDays int) int {
	raw := os.Getenv(envName)
	if raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			if days := ms / (24 * 60 * 60 * 1000); days > 0 {
				return days
			}
		}
	}
	return defaultDays
}

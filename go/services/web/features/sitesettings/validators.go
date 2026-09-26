package sitesettings

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// type helpers (JSON numbers decode as float64; Go bool/string/[]any/nil)

func isStr(v any) bool  { _, ok := v.(string); return ok }
func isBool(v any) bool { _, ok := v.(bool); return ok }
func isArr(v any) bool  { _, ok := v.([]any); return ok }
func isNum(v any) bool  { _, ok := v.(float64); return ok }
func isInt(v any) bool  { f, ok := v.(float64); return ok && f == float64(int64(f)) }

// fnum — coerce any numeric Go type to float64 (JSON bodies parse to float64;
// seed/section values are often int). Returns 0 for non-numbers.
func fnum(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int8:
		return float64(n)
	case int16:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case uint:
		return float64(n)
	case uint32:
		return float64(n)
	case uint64:
		return float64(n)
	}
	return 0
}
func sval(v any) string { s, _ := v.(string); return s }

var (
	reTplKey   = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	reClientId = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	reCidrV4   = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$`)
	reCidrV6   = regexp.MustCompile(`^([0-9a-fA-F]{0,4}:){1,7}[0-9a-fA-F]{0,4}\/\d{1,4}$`)
	reIpV4     = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
	reSubdom   = regexp.MustCompile(`^\*\.[a-z0-9-]+(\.[a-z0-9-]+)*$`)
	reDomain   = regexp.MustCompile(`(?i)^[a-z0-9-]+(\.[a-z0-9-]+)*\.[a-z]{2,}$`)
	reLdap     = regexp.MustCompile(`^ldaps?://`)
	reCompiler = regexp.MustCompile(`(?i)^[a-z0-9_-]+$`)
	reBucket   = regexp.MustCompile(`^[a-z0-9][a-z0-9.\-_]{1,62}$`)
)

func notObj() []string { return []string{"body must be a JSON object"} }

func validateTemplatesSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	errs := []string{}
	// Node: if (typeof value.enabled !== 'boolean') push — a missing enabled
	// is undefined (not boolean) → push.
	if !isBool(m["enabled"]) {
		errs = append(errs, "enabled must be a boolean")
	}
	if cat, p := m["categories"]; p {
		if !isArr(cat) {
			errs = append(errs, "categories must be an array")
		} else {
			seen := map[string]bool{}
			arr := cat.([]any)
			for _, e := range arr {
				c := toObj(e)
				if c == nil || !isStr(c["key"]) || !reTplKey.MatchString(sval(c["key"])) {
					errs = append(errs, "each category needs a key matching ^[a-z0-9-]{1,64}$")
					break
				}
				k := sval(c["key"])
				if seen[k] {
					errs = append(errs, fmt.Sprintf("duplicate category key: %s", k))
					break
				}
				seen[k] = true
				name := c["name"]
				if !isStr(name) || sval(name) == "" || len(sval(name)) > 120 {
					errs = append(errs, fmt.Sprintf("category %s: name required (<=120 chars)", k))
					break
				}
				desc := c["description"]
				if !isStr(desc) || len(sval(desc)) > 500 {
					errs = append(errs, fmt.Sprintf("category %s: description must be a string (<=500 chars)", k))
					break
				}
			}
		}
	}
	return errs
}

func toObj(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	if o, ok := v.(Obj); ok {
		return ObjToMap(o)
	}
	return nil
}

func validObj(value any) (map[string]any, bool) {
	m := toObj(value)
	if m == nil {
		return nil, false
	}
	return m, true
}

func validateMendeleySection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if !isBool(m["enabled"]) {
		errs = append(errs, "enabled must be a boolean")
	}
	if v, p := m["clientId"]; p && !reClientId.MatchString(sval(v)) {
		errs = append(errs, "clientId may only contain letters, digits, _ and -")
	}
	return errs
}

func validateZoteroSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if !isBool(m["enabled"]) {
		errs = append(errs, "enabled must be a boolean")
	}
	if v, p := m["clientKey"]; p && !reClientId.MatchString(sval(v)) {
		errs = append(errs, "clientKey may only contain letters, digits, _ and -")
	}
	return errs
}

func validateExternalUrlSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if !isBool(m["enabled"]) {
		errs = append(errs, "enabled must be a boolean")
	}
	if v, p := m["blockedNetworks"]; p {
		if !isArr(v) {
			errs = append(errs, "blockedNetworks must be an array")
		} else {
			for _, e := range v.([]any) {
				cidr, isS := e.(string)
				kind := isS && (reCidrV4.MatchString(cidr) || reCidrV6.MatchString(cidr) || reIpV4.MatchString(cidr))
				if !kind {
					errs = append(errs, fmt.Sprintf("blockedNetworks: invalid IP/CIDR %q", e))
				}
			}
		}
	}
	if v, p := m["allowedResourcesRegex"]; p {
		if !isStr(v) {
			errs = append(errs, `allowedResourcesRegex must be a string ("" to clear)`)
		} else if len(sval(v)) > 0 {
			if _, err := regexp.Compile(sval(v)); err != nil {
				errs = append(errs, "allowedResourcesRegex is not a valid regular expression")
			}
		}
	}
	return errs
}

func validateSignupSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if !isBool(m["enabled"]) {
		errs = append(errs, "enabled must be a boolean")
	}
	if v, p := m["disabledRedirectUrl"]; p && !isStr(v) {
		errs = append(errs, "disabledRedirectUrl must be a string")
	}
	if v, p := m["allowedEmailDomains"]; p {
		if !isArr(v) {
			errs = append(errs, "allowedEmailDomains must be an array")
		} else {
			for _, e := range v.([]any) {
				d, isS := e.(string)
				kind := isS && (d == "*" || reSubdom.MatchString(d) || reDomain.MatchString(d))
				if !kind {
					errs = append(errs, fmt.Sprintf("allowedEmailDomains: invalid domain %q", e))
				}
			}
		}
	}
	return errs
}

func validateSsoSamlSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if !isBool(m["enabled"]) {
		errs = append(errs, "enabled must be a boolean")
	}
	if asTrue(m["enabled"]) {
		if !isStr(m["issuer"]) || sval(m["issuer"]) == "" {
			errs = append(errs, "issuer is required to enable SAML")
		}
		hasStored := isStr(m["idpCert"]) && len(sval(m["idpCert"])) > 0
		if !hasStored && !envTrueRaw("OVERLEAF_SAML_IDP_CERT") {
			errs = append(errs, "IdP certificate is required to enable SAML (stored or via OVERLEAF_SAML_IDP_CERT env)")
		}
	}
	return errs
}

func validateSsoOidcSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if !isBool(m["enabled"]) {
		errs = append(errs, "enabled must be a boolean")
	}
	if asTrue(m["enabled"]) {
		hasIssuer := isStr(m["issuer"]) && len(sval(m["issuer"])) > 0
		hasUrls := isStr(m["authorizationURL"]) && len(sval(m["authorizationURL"])) > 0 &&
			isStr(m["tokenURL"]) && len(sval(m["tokenURL"])) > 0
		if !hasIssuer && !hasUrls {
			errs = append(errs, "either the issuer URL or the authorization/token URLs are required to enable OIDC")
		}
		if !isStr(m["clientID"]) || len(sval(m["clientID"])) == 0 {
			errs = append(errs, "clientID is required to enable OIDC")
		}
	}
	if v, p := m["allowedOIDCEmailDomains"]; p && !isArr(v) {
		errs = append(errs, "allowedOIDCEmailDomains must be an array")
	}
	return errs
}

func validateSsoLdapSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if !isBool(m["enabled"]) {
		errs = append(errs, "enabled must be a boolean")
	}
	if asTrue(m["enabled"]) {
		if !isStr(m["url"]) || !reLdap.MatchString(sval(m["url"])) {
			errs = append(errs, "url must be an ldapi(s)://… server URL to enable LDAP")
		}
		if !isStr(m["searchBase"]) || len(sval(m["searchBase"])) == 0 {
			errs = append(errs, "searchBase (base DN) is required to enable LDAP")
		}
	}
	if v, p := m["searchAttributes"]; p && isStr(v) && len(sval(v)) > 0 {
		if !isJSONArrString(sval(v)) {
			errs = append(errs, "searchAttributes must be a JSON array string")
		}
	}
	if v, p := m["timeout"]; p {
		if !isNum(v) || fnum(v) <= 0 {
			errs = append(errs, "timeout must be a positive number (ms)")
		}
	}
	return errs
}

func validateSandboxedCompilesSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	for _, f := range []string{"enabled", "dockerRunner"} {
		if v, p := m[f]; p && !isBool(v) {
			errs = append(errs, fmt.Sprintf("%s must be a boolean", f))
		}
	}
	for _, f := range []string{"hostDir", "socketPath", "extraFlags", "imageUser", "defaultImage"} {
		if v, p := m[f]; p && !isStr(v) {
			errs = append(errs, fmt.Sprintf("%s must be a string", f))
		}
	}
	if v, p := m["images"]; p {
		if bad, reason := validateImages(v); reason != "" {
			errs = append(errs, reason)
		} else {
			_ = bad
		}
	}
	return errs
}

func validateImages(v any) (bool, string) {
	arr, isA := v.([]any)
	if !isA || len(arr) < 1 {
		return true, "images must be a non-empty array of { image, name? } rows"
	}
	var bad bool
	for _, e := range arr {
		r := toObj(e)
		if r == nil {
			bad = true
			break
		}
		img := r["image"]
		if !isStr(img) || len(sval(img)) == 0 {
			bad = true
			break
		}
		if n, p := r["name"]; p && !isStr(n) {
			bad = true
			break
		}
	}
	if bad {
		return true, "images must be a non-empty array of { image, name? } rows"
	}
	seen := map[string]bool{}
	for _, e := range arr {
		r := toObj(e)
		img := sval(r["image"])
		if seen[img] {
			return true, fmt.Sprintf("duplicate image: %s", img)
		}
		seen[img] = true
	}
	return false, ""
}

func validateGitIntegrationSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if !isBool(m["enabled"]) {
		errs = append(errs, "enabled must be a boolean")
	}
	if asTrue(m["enabled"]) && (!isStr(m["host"]) || len(sval(m["host"])) == 0) {
		errs = append(errs, "host is required to enable git integration")
	}
	if v, p := m["port"]; p && (!isInt(v) || fnum(v) < 1 || fnum(v) > 65535) {
		errs = append(errs, "port must be an integer between 1 and 65535")
	}
	return errs
}

func validateTypstSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if _, p := m["enabled"]; p && !isBool(m["enabled"]) {
		errs = append(errs, "enabled must be a boolean")
	}
	if _, p := m["url"]; p && !isStr(m["url"]) {
		errs = append(errs, "url must be a string")
	}
	return errs
}

func validateGithubSyncSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if !isBool(m["enabled"]) {
		errs = append(errs, "enabled must be a boolean")
	}
	if asTrue(m["enabled"]) {
		cid := m["clientId"]
		if _, p := m["clientID"]; p {
			cid = m["clientID"]
		}
		if !isStr(cid) || len(sval(cid)) == 0 {
			errs = append(errs, "clientId (GitHub OAuth App ID) is required to enable GitHub sync")
		}
		hasStored := isStr(m["clientSecret"]) && len(sval(m["clientSecret"])) > 0
		if !hasStored && !envTrueRaw("GITHUB_SYNC_CLIENT_SECRET") {
			errs = append(errs, "clientSecret is required to enable GitHub sync (stored or via GITHUB_SYNC_CLIENT_SECRET env)")
		}
	}
	for _, f := range []string{"cipherFile", "cipherLabel"} {
		if v, p := m[f]; p && !isStr(v) {
			errs = append(errs, fmt.Sprintf("%s must be a string", f))
		}
	}
	return errs
}

func validateEmailSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if v, p := m["skipConfirmation"]; p && !isBool(v) {
		errs = append(errs, "skipConfirmation must be a boolean")
	}
	if v, p := m["driver"]; p && v != "smtp" && v != "ses" {
		errs = append(errs, `driver must be "smtp" or "ses"`)
	}
	if v, p := m["port"]; p && (!isInt(v) || fnum(v) < 1 || fnum(v) > 65535) {
		errs = append(errs, "port must be an integer between 1 and 65535")
	}
	if m["driver"] == "smtp" && (!isStr(m["host"]) || len(sval(m["host"])) == 0) {
		errs = append(errs, "smtp host is required for the smtp driver")
	}
	if m["driver"] == "ses" {
		if !isStr(m["accessKeyId"]) || len(sval(m["accessKeyId"])) == 0 {
			errs = append(errs, "SES accessKeyId is required for the ses driver")
		}
		hasStored := isStr(m["sesSecret"]) && len(sval(m["sesSecret"])) > 0
		if !hasStored && !envTrueRaw("EMAIL_SES_SECRET_ACCESS_KEY") {
			errs = append(errs, "SES secret key is required for the ses driver (stored or env)")
		}
	}
	for _, f := range []string{"fromAddress", "replyTo", "user", "name"} {
		if v, p := m[f]; p && !isStr(v) {
			errs = append(errs, fmt.Sprintf("%s must be a string", f))
		}
	}
	return errs
}

var knownLinkedFileTypes = []string{"project_file", "project_output_file", "url", "zotero"}

func validateLinkedFileTypesSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	arr, isA := m["enabledTypes"].([]any)
	if !isA {
		return []string{"enabledTypes must be an array of linked file type names"}
	}
	var errs []string
	for _, fixed := range []string{"project_file", "project_output_file"} {
		found := false
		for _, e := range arr {
			if e == fixed {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("%s is always enabled and cannot be removed", fixed))
		}
	}
	for _, t := range arr {
		s, isS := t.(string)
		known := false
		for _, k := range knownLinkedFileTypes {
			if k == s {
				known = true
				break
			}
		}
		if !isS || !known {
			errs = append(errs, fmt.Sprintf("unknown linked file type %q (known: %s)", t, joinAmp(knownLinkedFileTypes)))
			break
		}
	}
	return errs
}

func joinAmp(a []string) string {
	out := ""
	for i, s := range a {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

func validatePandocSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if !isBool(m["enabled"]) {
		errs = append(errs, "enabled must be a boolean")
	}
	if asTrue(m["enabled"]) {
		if !isStr(m["image"]) || len(sval(m["image"])) == 0 {
			errs = append(errs, "image is required to enable pandoc conversions")
		}
	} else if v, p := m["image"]; p && !isStr(v) {
		errs = append(errs, "image must be a string")
	}
	return errs
}

func validateMiscSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	for _, f := range []string{
		"navHidePoweredBy", "robotsNoindex", "allowPublicAccess",
		"allowAnonymousReadWriteSharing", "disableLinkSharing", "disableChat",
		"historyRestore", "enablePdfCaching", "pythonRunner",
	} {
		if v, p := m[f]; p && !isBool(v) {
			errs = append(errs, fmt.Sprintf("%s must be a boolean", f))
		}
	}
	intMin := map[string]float64{
		"projectHardDeletionDelayDays": 1, "userHardDeletionDelayDays": 1,
		"maxUploadSizeMiB": 1, "maxEntitiesPerProject": 1,
		"projectChangeNotificationDelayMs": 0,
	}
	for _, f := range []string{"projectHardDeletionDelayDays", "userHardDeletionDelayDays", "maxUploadSizeMiB", "maxEntitiesPerProject", "projectChangeNotificationDelayMs"} {
		if v, p := m[f]; p && (!isInt(v) || fnum(v) < intMin[f]) {
			errs = append(errs, fmt.Sprintf("%s must be an integer >= %d", f, int(intMin[f])))
		}
	}
	if v, p := m["appName"]; p && (!isStr(v) || len(sval(v)) == 0 || len(sval(v)) > 64) {
		errs = append(errs, "appName must be a non-empty string (<=64 chars)")
	}
	if v, p := m["defaultLatexCompiler"]; p && (!isStr(v) || len(sval(v)) == 0 || !reCompiler.MatchString(sval(v))) {
		errs = append(errs, "defaultLatexCompiler must be a non-empty compiler name (e.g. pdflatex, lualatexmk)")
	}
	return errs
}

func validateWebdavSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if !isBool(m["enabled"]) {
		errs = append(errs, "enabled must be a boolean")
	}
	if v, p := m["rootPath"]; p && (!isStr(v) || len(sval(v)) == 0 || !hasPrefix(sval(v), "/")) {
		errs = append(errs, "rootPath must be an absolute path")
	}
	for _, f := range []string{"requestTimeoutMs", "retryCount", "retryDelayMs"} {
		if v, p := m[f]; p && (!isInt(v) || fnum(v) < 0) {
			errs = append(errs, fmt.Sprintf("%s must be an integer >= 0", f))
		}
	}
	return errs
}

func validateDropboxSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if !isBool(m["enabled"]) {
		errs = append(errs, "enabled must be a boolean")
	}
	if asTrue(m["enabled"]) {
		if !isStr(m["appKey"]) || len(sval(m["appKey"])) == 0 {
			errs = append(errs, "appKey is required to enable Dropbox")
		}
		if v, p := m["appSecret"]; p && sval(v) != "" && !isStr(v) {
			errs = append(errs, "appSecret must be a string")
		}
	}
	return errs
}

func validateLanguagetoolSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if v, p := m["enabled"]; p && !isBool(v) {
		errs = append(errs, "enabled must be a boolean")
	}
	if v, p := m["url"]; p && !isStr(v) {
		errs = append(errs, "url must be a string")
	}
	return errs
}

func validateLlmSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	for _, f := range []string{"enabled", "allowUserSettings"} {
		if v, p := m[f]; p && !isBool(v) {
			errs = append(errs, fmt.Sprintf("%s must be a boolean", f))
		}
	}
	for _, f := range []string{"userRatePerMinute", "adminRatePerMinute", "userDailyTokens"} {
		if v, p := m[f]; p && (!isInt(v) || fnum(v) < 0) {
			errs = append(errs, fmt.Sprintf("%s must be an integer >= 0", f))
		}
	}
	return errs
}

func validateBrandingSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	return checkStrings(m, []string{"navTitle", "leftFooter", "rightFooter"})
}

func validateServicesSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	return checkStrings(m, []string{
		"v1HistoryUrl", "githubInterfaceUrl", "githubInterfaceWorkdirRoot",
		"webdavInterfaceUrl", "dropboxInterfaceUrl", "dataManipulatorUrl",
	})
}

func validateStorageSection(value any) []string {
	m, ok := validObj(value)
	if !ok {
		return notObj()
	}
	var errs []string
	if v, p := m["backend"]; p && v != "" && v != "fs" && v != "s3" {
		errs = append(errs, fmt.Sprintf("backend must be 'fs' or 's3' (got '%v')", v))
	}
	errs = append(errs, checkStrings(m, []string{
		"s3Endpoint", "s3AccessKeyId", "s3Secret",
		"templateFilesBucket", "projectBlobsBucket", "globalBlobsBucket",
		"docstoreArchiveBucket",
	})...)
	for _, k := range []string{"templateFilesBucket", "projectBlobsBucket", "globalBlobsBucket", "docstoreArchiveBucket"} {
		if v, p := m[k]; p && isStr(v) && len(sval(v)) > 0 && !reBucket.MatchString(sval(v)) {
			errs = append(errs, fmt.Sprintf("%s must be a valid bucket name (lowercase letters, digits, . _ -; 3-63 chars)", k))
		}
	}
	return errs
}

func checkStrings(m map[string]any, fields []string) []string {
	var errs []string
	for _, f := range fields {
		if v, p := m[f]; p && !isStr(v) {
			errs = append(errs, fmt.Sprintf("%s must be a string", f))
		}
	}
	return errs
}

var SECTION_VALIDATORS = map[string]func(any) []string{
	"templates":          validateTemplatesSection,
	"zotero":             validateZoteroSection,
	"mendeley":           validateMendeleySection,
	"externalUrl":        validateExternalUrlSection,
	"signup":             validateSignupSection,
	"sso-saml":           validateSsoSamlSection,
	"sso-oidc":           validateSsoOidcSection,
	"sso-ldap":           validateSsoLdapSection,
	"sandboxed-compiles": validateSandboxedCompilesSection,
	"git-integration":    validateGitIntegrationSection,
	"typst":              validateTypstSection,
	"github-sync":        validateGithubSyncSection,
	"email":              validateEmailSection,
	"linked-file-types":  validateLinkedFileTypesSection,
	"pandoc":             validatePandocSection,
	"webdav":             validateWebdavSection,
	"dropbox":            validateDropboxSection,
	"misc":               validateMiscSection,
	"languagetool":       validateLanguagetoolSection,
	"llm":                validateLlmSection,
	"branding":           validateBrandingSection,
	"services":           validateServicesSection,
	"storage":            validateStorageSection,
}

// small helpers
func asTrue(v any) bool { b, _ := v.(bool); return b }

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func isJSONArrString(s string) bool {
	// Node: try { JSON.parse(value) } catch { ... } — any valid JSON passes.
	t := strings.TrimSpace(s)
	if len(t) == 0 {
		return false
	}
	switch t[0] {
	case '[', '{', '"', 't', 'f', 'n', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return json.Valid([]byte(t))
	}
	return false
}

func envTrueRaw(k string) bool { return len(os.Getenv(k)) > 0 }

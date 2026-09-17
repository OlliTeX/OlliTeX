package dropbox

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

// dropboxAppKey/Secret — Node reads the process env at request time.
func dropboxAppKey() string    { return os.Getenv("DROPBOX_APP_KEY") }
func dropboxAppSecret() string { return os.Getenv("DROPBOX_APP_SECRET") }

// dbxOauthState — Node: createNonce() saved under the session key
// 'dropbox-oauth-state' (5min TTL). The sandbox never reaches the
// state-CREATION branch (oauth2 503s before it; callback reads an empty
// expected state → 400 "Invalid Dropbox OAuth state"), so this only needs
// read-back semantics + a nonce generator for live parity.
func dbxOauthState(cxt *core.Cxt) string {
	if cxt.Sess == nil {
		return ""
	}
	if raw, ok := stringFromSession(cxt, "dropbox-oauth-state"); ok {
		return raw
	}
	nonce := views.NewNonce()
	cxt.Sess.Set("dropbox-oauth-state", nonce)
	return nonce
}

// stringFromSession — session doc lookup (Node locals.get).
func stringFromSession(cxt *core.Cxt, key string) (string, bool) {
	raw, ok := cxt.Sess.GetRaw(key)
	if !ok || len(raw) == 0 {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	return string(raw), true
}

// dropboxAuthorizeURL — Node's redirect target (live deployments only):
// https://www.dropbox.com/oauth2/authorize?client_id=<appKey>&state=<state>
// &token_access_type=offline&response_type=code&scope=files.all...&redirect_uri=<origin>/user/dropbox/oauth/callback
func dropboxAuthorizeURL(appKey, appSecret, state string) string {
	origin := envSiteOrigin()
	scopes := strings.Join([]string{
		"files.metadata.read",
		"files.content.read",
		"files.content.write",
		"folder.search.read",
		"users.read_name",
		"users.read_email",
	}, " ")
	q := url.Values{}
	q.Set("client_id", appKey)
	q.Set("state", state)
	q.Set("token_access_type", "offline")
	q.Set("response_type", "code")
	q.Set("scope", scopes)
	q.Set("redirect_uri", origin+"/user/dropbox/oauth/callback")
	return "https://www.dropbox.com/oauth2/authorize?" + q.Encode()
}

func envSiteOrigin() string {
	if u := os.Getenv("SITE_URL"); u != "" {
		return strings.TrimSuffix(u, "/")
	}
	return ""
}

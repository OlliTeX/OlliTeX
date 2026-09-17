package zotero

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"ollitex/go/services/web/core"
)

const zoteroOAuthURL = "https://www.zotero.org/oauth"

// ZOTERO_OAUTH_URL endpoints (Node ZoteroOAuth.mjs).
const (
	zoteroOAuthRequest   = zoteroOAuthURL + "/request"
	zoteroOAuthAccess    = zoteroOAuthURL + "/access"
	zoteroOAuthAuthorize = zoteroOAuthURL + "/authorize"
)

var oauthHTTP = &http.Client{Timeout: 30 * time.Second}

// zoteroAppCreds — Node ZoteroOAuth.getCredentials: site_settings zotero
// section (clientKey plaintext, clientSecret resolved), else the ZOTERO_*
// env. ok=false when neither key nor secret is present.
type appCreds struct {
	ClientKey    string
	ClientSecret string
}

func zoteroAppCreds(ctx context.Context, a *core.App) (*appCreds, bool) {
	// site_settings section (P3c path). clientKey is stored plaintext;
	// clientSecret is stored encrypted ("ss::...") and resolved via the
	// shared cipher (Node: getSection already decrypts for display).
	if a.Mongo != nil {
		if db, dbErr := a.Mongo.DB(ctx); dbErr == nil {
			var doc struct {
				Zotero *struct {
					ClientKey    string `bson:"clientKey"`
					ClientSecret string `bson:"clientSecret"`
				} `bson:"zotero"`
			}
			if err := db.Collection("site_settings").FindOne(ctx,
				bson.D{{Key: "_id", Value: "global"}}).Decode(&doc); err == nil && doc.Zotero != nil {
				key := strings.TrimSpace(doc.Zotero.ClientKey)
				sec := resolveZoteroClientSecret(doc.Zotero.ClientSecret)
				if key != "" && sec != "" {
					return &appCreds{ClientKey: key, ClientSecret: sec}, true
				}
			}
		}
	}
	// Legacy env fallback (CE setups).
	key := strings.TrimSpace(os.Getenv("ZOTERO_CLIENT_KEY"))
	sec := strings.TrimSpace(os.Getenv("ZOTERO_CLIENT_SECRET"))
	if key != "" && sec != "" {
		return &appCreds{ClientKey: key, ClientSecret: sec}, true
	}
	return nil, false
}

// resolveZoteroClientSecret — the site_settings zotero.clientSecret is
// stored encrypted (OL_CEP-v3, string-value form: "ss::<tok>"). The
// P3c sitesettings store already decrypts for display; here we decrypt
// the raw stored value when it carries the ss:: token format, and fall
// back to the raw value (for CE env-seeded plaintext) otherwise.
func resolveZoteroClientSecret(stored string) string {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return ""
	}
	if strings.HasPrefix(stored, "ss::") {
		if c, cerr := zcipherInst(); cerr == nil && c != nil {
			if dec, ok2 := c.DecryptText(stored); ok2 {
				return dec
			}
		}
		return ""
	}
	return stored
}

func callbackURLFrom(cxt *core.Cxt) string {
	proto := cxt.Req.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "https"
	}
	proto = strings.TrimSpace(strings.Split(proto, ",")[0])
	host := cxt.Req.Host
	if h := cxt.Req.Header.Get("X-Forwarded-Host"); h != "" {
		host = strings.TrimSpace(strings.Split(h, ",")[0])
	}
	return proto + "://" + host + "/user/zotero/oauth/callback"
}

// ---------- OAuth 1.0a (HMAC-SHA1) ----------

// percentEncode — RFC 5849: unreserved [A-Za-z0-9-_.~] pass through, every
// other byte %XX (uppercase). Spaces → %20 (not '+').
func percentEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			b.WriteByte(c)
		case c == '-' || c == '_' || c == '.' || c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func randNonce() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return base64.StdEncoding.EncodeToString(buf)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// oauthAuthHeader builds the `Authorization: OAuth ...` header value for the
// given data params + optional token (oauth-1.0a authorize + toHeader).
func oauthAuthHeader(method, rawURL string, clientKey, clientSecret string,
	tokenKey, tokenSecret string, data map[string]string) string {

	params := map[string]string{
		"oauth_nonce":            randNonce(),
		"oauth_timestamp":        strconv.FormatInt(time.Now().Unix(), 10),
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_consumer_key":     clientKey,
		"oauth_version":          "1.0",
	}
	if tokenKey != "" {
		params["oauth_token"] = tokenKey
	}
	for k, v := range data {
		params[k] = v
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if percentEncode(keys[i]) != percentEncode(keys[j]) {
			return percentEncode(keys[i]) < percentEncode(keys[j])
		}
		return percentEncode(params[keys[i]]) < percentEncode(params[keys[j]])
	})
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(percentEncode(k))
		sb.WriteByte('=')
		sb.WriteString(percentEncode(params[k]))
	}
	baseString := method + "&" + percentEncode(rawURL) + "&" + percentEncode(sb.String())

	signingKey := percentEncode(clientSecret) + "&"
	if tokenSecret != "" {
		signingKey = percentEncode(clientSecret) + "&" + percentEncode(tokenSecret)
	}
	mac := hmac.New(sha1.New, []byte(signingKey))
	mac.Write([]byte(baseString))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	params["oauth_signature"] = sig

	// Header: emit the known params then the data params, all quoted, with
	// the signature last (oauth-1.0a toHeader order).
	names := make([]string, 0, len(params))
	for _, k := range []string{"oauth_nonce", "oauth_timestamp", "oauth_signature_method", "oauth_consumer_key", "oauth_version"} {
		if _, okk := params[k]; okk {
			names = append(names, k)
		}
	}
	if tokenKey != "" {
		names = append(names, "oauth_token")
	}
	for k, _ := range data {
		names = append(names, k)
	}
	var parts []string
	for _, k := range names {
		parts = append(parts, k+"=\""+percentEncode(params[k])+"\"")
	}
	parts = append(parts, "oauth_signature=\""+percentEncode(sig)+"\"")
	return "OAuth " + strings.Join(parts, ", ")
}

// urlencodedParse — Node parseOAuthResponse (URLSearchParams → object).
func urlencodedParse(body string) map[string]string {
	out := map[string]string{}
	v, err := url.ParseQuery(body)
	if err != nil {
		return out
	}
	for k, vs := range v {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

type oauthToken struct {
	OAuthToken       string
	OAuthTokenSecret string
}

func oauthPost(ctx context.Context, rawURL, authHeader string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("User-Agent", "Overleaf-CEP-Zotero")
	resp, err := oauthHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return urlencodedParse(string(body)), nil
}

func zoteroRequestToken(ctx context.Context, clientKey, clientSecret, callbackURL string) (*oauthToken, error) {
	auth := oauthAuthHeader(http.MethodPost, zoteroOAuthRequest,
		clientKey, clientSecret, "", "",
		map[string]string{"oauth_callback": callbackURL})
	data, err := oauthPost(ctx, zoteroOAuthRequest, auth)
	if err != nil {
		return nil, err
	}
	if data["oauth_token"] == "" {
		return nil, fmt.Errorf("zotero request token missing")
	}
	return &oauthToken{OAuthToken: data["oauth_token"], OAuthTokenSecret: data["oauth_token_secret"]}, nil
}

type oauthAccess struct {
	AccessToken  string
	APIKeySecret string
	ZoteroUserID string
	Username     string
}

func zoteroExchangeToken(ctx context.Context, clientKey, clientSecret,
	token, tokenSecret, verifier string) (*oauthAccess, error) {
	auth := oauthAuthHeader(http.MethodPost, zoteroOAuthAccess,
		clientKey, clientSecret, token, tokenSecret,
		map[string]string{"oauth_verifier": verifier})
	data, err := oauthPost(ctx, zoteroOAuthAccess, auth)
	if err != nil {
		return nil, err
	}
	if data["oauth_token"] == "" {
		return nil, fmt.Errorf("zotero access token missing")
	}
	return &oauthAccess{
		AccessToken:  data["oauth_token"],
		APIKeySecret: data["oauth_token_secret"],
		ZoteroUserID: data["userID"],
		Username:     data["username"],
	}, nil
}

// zoteroAuthorizationUrl — Node getAuthorizationUrl (library_access=1,
// all_groups=read).
func zoteroAuthorizationURL(oauthToken string) string {
	q := url.Values{}
	q.Set("oauth_token", oauthToken)
	q.Set("library_access", "1")
	q.Set("all_groups", "read")
	return zoteroOAuthAuthorize + "?" + q.Encode()
}

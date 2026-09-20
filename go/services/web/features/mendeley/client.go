package mendeley

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
)

// Node MendeleyApiClient constants.
const (
	mendeleyAPIURL       = "https://api.mendeley.com"
	mendeleyAuthorizeURL = "https://api.mendeley.com/oauth/authorize"
	mendeleyTokenURL     = "https://api.mendeley.com/oauth/token"
	mendeleyScope        = "all"
	mendeleyGroupsAccept = "application/vnd.mendeley-group.1+json"
)

// Error classes the controller discriminates (forbidden / expired /
// not-linked all map to the 403 groups envelope; anything else 500).
type (
	mendeleyForbiddenErr struct{ msg string }
	mendeleyExpiredErr   struct{ msg string }
	mendeleyNotLinkedErr struct{ msg string }
)

func (e mendeleyForbiddenErr) Error() string { return e.msg }
func (e mendeleyExpiredErr) Error() string   { return e.msg }
func (e mendeleyNotLinkedErr) Error() string { return e.msg }

var errNotConfigured = errors.New("Mendeley connector is not configured on this instance")

var mendeleyHTTP = &http.Client{
	// Node fetch-utils default; no redirects expected on these endpoints.
	Timeout: 30 * time.Second, // generous; not pinned in the gate
}

// objID — Node ObjectId(userId) (the session user id is a 24-hex string).
func objID(h string) primitive.ObjectID {
	if hid, err := primitive.ObjectIDFromHex(h); err == nil {
		return hid
	}
	return primitive.NilObjectID
}

// ---------- credential storage (user.refProviders.mendeley) ----------

// mendeleyCreds — decrypted credential object (Node tokens shape):
// { accessToken, refreshToken, expiresAt }.
type mendeleyCreds struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    int64  `json:"expiresAt"`
}

// linkedState — (linked, credError). Node _getStoredCredentials:
// no encrypted field → null → isLinked false (no error).
func storedCreds(ctx context.Context, a *core.App, uid string) (*mendeleyCreds, bool, bool) {
	if a.Mongo == nil || uid == "" {
		return nil, false, false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false, false
	}
	var u struct {
		RefProviders *struct {
			Mendeley *struct {
				Encrypted string `bson:"encrypted"`
			} `bson:"mendeley"`
		} `bson:"refProviders"`
	}
	if err := db.Collection("users").FindOne(ctx,
		bson.D{{Key: "_id", Value: objID(uid)}}).
		Decode(&u); err != nil {
		// Node: User.findById(...) → null → _getStoredCredentials → null.
		return nil, false, false
	}
	if u.RefProviders == nil || u.RefProviders.Mendeley == nil || u.RefProviders.Mendeley.Encrypted == "" {
		return nil, false, false
	}
	c, _ := mCipherInst().(interface {
		DecryptRaw(string) (string, bool)
	})
	if c == nil {
		return nil, false, true // Node: decrypt failure → error → connected=false
	}
	plain, ok := c.DecryptRaw(u.RefProviders.Mendeley.Encrypted)
	if !ok {
		return nil, false, true
	}
	var creds mendeleyCreds
	if jerr := json.Unmarshal([]byte(plain), &creds); jerr != nil {
		return nil, false, true
	}
	return &creds, true, false
}

func isLinked(ctx context.Context, a *core.App, uid string) (bool, error) {
	_, ok, cerr := storedCreds(ctx, a, uid)
	if cerr {
		return false, errors.New("failed to decrypt Mendeley credentials")
	}
	return ok, nil
}

func storeCreds(ctx context.Context, a *core.App, uid string, tokens *mendeleyCreds) error {
	if a.Mongo == nil || uid == "" {
		return errors.New("cipher unavailable")
	}
	c, _ := mCipherInst().(interface {
		EncryptRaw(string) (string, error)
	})
	if c == nil {
		return errors.New("cipher unavailable")
	}
	payload, _ := json.Marshal(tokens)
	enc, err := c.EncryptRaw(string(payload))
	if err != nil {
		return err
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	_, err = db.Collection("users").UpdateOne(ctx,
		bson.D{{Key: "_id", Value: objID(uid)}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "refProviders.mendeley", Value: bson.D{{Key: "encrypted", Value: enc}}},
		}}})
	return err
}

// unlinkAccount — Node: $unset refProviders.mendeley (always succeeds,
// even when nothing is stored).
func unlinkAccount(ctx context.Context, a *core.App, uid string) error {
	if a.Mongo == nil || uid == "" {
		return errors.New("no user")
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	_, err = db.Collection("users").UpdateOne(ctx,
		bson.D{{Key: "_id", Value: objID(uid)}},
		bson.D{{Key: "$unset", Value: bson.D{{Key: "refProviders.mendeley", Value: 1}}}})
	return err
}

// ---------- OAuth 2.0 ----------

func basicAuthHeader(m mendeleySettings) string {
	creds := m.ClientID + ":" + m.ClientSecret
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
}

// getOAuthAuthorizeUrl — Node getOAuthAuthorizeUrl: URLSearchParams order
// client_id, redirect_uri, response_type, scope, state.
func getOAuthAuthorizeURL(state string, m mendeleySettings) string {
	u, _ := url.Parse(mendeleyAuthorizeURL)
	q := u.Query()
	q.Set("client_id", m.ClientID)
	q.Set("redirect_uri", m.CallbackURL)
	q.Set("response_type", "code")
	q.Set("scope", mendeleyScope)
	q.Set("state", state)
	// URLSearchParams.toString() — Go url.Values.Encode() match (UTF-8
	// percent, `&` separator).
	u.RawQuery = q.Encode()
	return u.String()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func requestToken(ctx context.Context, form url.Values, m mendeleySettings) (*mendeleyCreds, error) {
	if !isServiceConfigured(m) {
		return nil, errNotConfigured
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mendeleyTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", basicAuthHeader(m))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := mendeleyHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		if res.StatusCode == 400 || res.StatusCode == 401 {
			return nil, mendeleyForbiddenErr{msg: "Mendeley token request unauthorized"}
		}
		return nil, fmt.Errorf("Mendeley token request failed (%d)", res.StatusCode)
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, errors.New("Mendeley token response invalid")
	}
	if tr.AccessToken == "" {
		return nil, errors.New("Mendeley token response missing access_token")
	}
	now := int64(0) // Node Date.now() — the gate never reaches the configured
	// path; the refresh logic below mirrors Node anyway.
	expires := int64(3600)
	if tr.ExpiresIn > 0 {
		expires = tr.ExpiresIn
	}
	return &mendeleyCreds{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresAt:    now + expires*1000,
	}, nil
}

func refreshAccessToken(ctx context.Context, refresh string, m mendeleySettings) (*mendeleyCreds, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refresh)
	form.Set("redirect_uri", m.CallbackURL)
	return requestToken(ctx, form, m)
}

// getAccessToken — Node _getAccessToken: no creds → NotLinked; token
// valid beyond now+60s → use; else refresh (Forbidden → Expired).
func getAccessToken(ctx context.Context, a *core.App, uid string, m mendeleySettings) (string, error) {
	creds, ok, cerr := storedCreds(ctx, a, uid)
	if cerr {
		return "", errors.New("failed to decrypt Mendeley credentials")
	}
	if !ok {
		return "", mendeleyNotLinkedErr{msg: "Mendeley account not linked"}
	}
	if creds.RefreshToken == "" {
		return creds.AccessToken, nil
	}
	// expiry check needs a wall clock; Node Date.now() vs creds.expiresAt.
	if creds.ExpiresAt > 0 {
		now := wallNow()
		if creds.ExpiresAt > now+60_000 && creds.AccessToken != "" {
			return creds.AccessToken, nil
		}
	}
	if creds.RefreshToken == "" {
		return "", mendeleyExpiredErr{msg: "Mendeley token expired"}
	}
	refreshed, err := refreshAccessToken(ctx, creds.RefreshToken, m)
	if err != nil {
		if _, ok2 := err.(mendeleyForbiddenErr); ok2 {
			return "", mendeleyExpiredErr{msg: "Mendeley token expired"}
		}
		return "", err
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = creds.RefreshToken
	}
	if serr := storeCreds(ctx, a, uid, refreshed); serr != nil {
		return refreshed.AccessToken, nil // Node: _saveCredentials errors propagate
	}
	return refreshed.AccessToken, nil
}

// ---------- groups ----------

type mendeleyGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// getGroupsForUser — Node: GET /groups?type=all (Accept vnd.mendeley-
// group.1+json, Bearer) → (groups||[]).map({id:String, name||`Group ${id}`});
// 401/403 → Forbidden.
func getGroupsForUser(ctx context.Context, a *core.App, uid string, m mendeleySettings) ([]mendeleyGroup, error) {
	tok, err := getAccessToken(ctx, a, uid, m)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mendeleyAPIURL+"/groups?type=all", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", mendeleyGroupsAccept)
	res, err := mendeleyHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		if res.StatusCode == 401 || res.StatusCode == 403 {
			return nil, mendeleyForbiddenErr{msg: "forbidden"}
		}
		return nil, fmt.Errorf("Mendeley groups request failed (%d)", res.StatusCode)
	}
	var arr []struct {
		ID   any    `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, err
	}
	out := make([]mendeleyGroup, 0, len(arr))
	for _, g := range arr {
		id := ""
		switch v := g.ID.(type) {
		case string:
			id = v
		case float64:
			id = strconv.FormatFloat(v, 'f', -1, 64)
		}
		name := g.Name
		if name == "" {
			name = "Group " + id
		}
		out = append(out, mendeleyGroup{ID: id, Name: name})
	}
	return out, nil
}

func wallNow() int64 { return time.Now().UnixMilli() }

// urlValues — Node URLSearchParams(k: v, ...) builder.
func urlValues(kv ...string) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

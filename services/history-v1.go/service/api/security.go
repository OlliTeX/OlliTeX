// security.go — Go port of services/history-v1/api/middleware/security.js.
//
// Auth modes (Node getAuthHandlers):
//
//	"basic"  handleBasicAuth: 401 + WWW-Authenticate: Basic realm="Application"
//	"jwt"    configureJWTAuth('jwt'):    Bearer header only
//	"token"  configureJWTAuth('token'):  ?token= query only
//	"either" configureJWTAuth('either'): Bearer header OR ?token=
//
// Every JWT-mode handler ALSO accepts basic auth with valid staging
// credentials ("for now" — see the auth acceptance test
// 'accepts basic auth in place of JWT (for now)').
//
// JWT: pure-Go HS256 verification (no external deps). The payload carries
// project_id; on a jwt/token route with a project_id path param, the claim
// must match (403 Wrong project_id otherwise). A primary key first, then the
// old key (Node jwtAuth.oldKey) when configured.
package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Auth error — rendered by the API root handler with the carried HTTP status
// and headers (Node err.statusCode + err.headers).
type AuthError struct {
	Status  int
	Headers map[string]string
	Msg     string
}

func (e *AuthError) Error() string { return "api: " + e.Msg }

// --- Basic auth (Node hasValidBasicAuthCredentials) ---

// hasValidBasicAuthCredentials — user must be "staging"; the password must be
// the current or (when set) old password. Constant-time comparison (Node
// tsscmp).
func (a *API) hasValidBasicAuthCredentials(r *http.Request) bool {
	user, pass, ok := r.BasicAuth()
	if !ok || user != "staging" {
		return false
	}
	cmp := func(candidate string) bool {
		if candidate == "" {
			return false
		}
		// subtle.ConstantTimeCompare needs equal lengths; a length check
		// first leaks nothing (it only compares against a constant-time
		// result, mirroring tsscmp's contract).
		return len(candidate) == len(pass) && constantTimeEqual(candidate, pass)
	}
	if cmp(a.cfg.BasicHttpAuthPassword) {
		return true
	}
	return cmp(a.cfg.BasicHttpAuthOldPassword)
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	diff := 0
	for i := 0; i < len(a); i++ {
		diff |= int(a[i]) ^ int(b[i])
	}
	return diff == 0
}

// --- JWT (HS256, pure Go; Node jwt.verify equivalent) ---

// jwtVerifyErrors — the Node jwt.JsonWebTokenError/TokenExpiredError family.
var jwtVerifyErrors = []string{"malformed", "signature", "expired", "not before", "header", "payload"}

type jwtClaim struct {
	ProjectID json.RawMessage `json:"project_id"`
	Exp       *int64          `json:"exp"`
	Nbf       *int64          `json:"nbf"`
}

// projectIDString — Node decoded.project_id.toString(): normalize the
// claim the way JS would stringify it (string claim as-is, numeric claim
// as its JSON number).
func (c jwtClaim) projectIDString() string {
	raw := string(c.ProjectID)
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		if s, err := strconv.Unquote(raw); err == nil {
			return s
		}
	}
	return raw
}

// decodeJWT — Node decodeJWT: verify against the configured key, then the
// old key when present. Returns the claims.
func (a *API) decodeJWT(token string) (jwtClaim, error) {
	for i, key := range []string{a.cfg.JWTAuthKey, a.cfg.JWTAuthOldKey} {
		if key == "" {
			if i > 0 {
				break
			}
			continue
		}
		cl, err := verifyHS256(token, key)
		if err == nil {
			return cl, nil
		}
	}
	return jwtClaim{}, errors.New("invalid token")
}

func verifyHS256(token, key string) (jwtClaim, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtClaim{}, errors.New("token malformed: expected 3 segments")
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return jwtClaim{}, errors.New("token malformed: bad header encoding")
	}
	payloadRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaim{}, errors.New("token malformed: bad payload encoding")
	}
	sigRaw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return jwtClaim{}, errors.New("token malformed: bad signature encoding")
	}
	var header struct {
		Algorithm string `json:"alg"`
	}
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		return jwtClaim{}, errors.New("token malformed: bad header json")
	}
	if header.Algorithm != "HS256" {
		return jwtClaim{}, fmt.Errorf("token signature algorithm is invalid: %s", header.Algorithm)
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sigRaw, mac.Sum(nil)) {
		return jwtClaim{}, errors.New("signature verification failed")
	}
	var claims jwtClaim
	if err := json.Unmarshal(payloadRaw, &claims); err != nil {
		return jwtClaim{}, errors.New("token malformed: bad payload json")
	}
	now := time.Now()
	if claims.Nbf != nil && now.Unix() < *claims.Nbf {
		return jwtClaim{}, errors.New("token used before issued at")
	}
	if claims.Exp != nil && now.Unix() >= *claims.Exp {
		return jwtClaim{}, errors.New("token is expired")
	}
	return claims, nil
}

// --- Route auth (Node configureJWTAuth / handleBasicAuth) ---

type authMode string

const (
	authBasic  authMode = "basic"
	authJWT    authMode = "jwt"
	authToken  authMode = "token"
	authEither authMode = "either"
)

// authorize — Node handleBasicAuth / configureJWTAuth for one request.
// Returns a non-nil *AuthError with the status + headers the Node middleware
// would have next()'d, and the message rendered in the error body.
func (a *API) authorize(mode authMode, r *http.Request, projectID string) *AuthError {
	if a.hasValidBasicAuthCredentials(r) {
		return nil
	}
	// Node handleBasicAuth: the error is a plain `new Error()` (empty
	// message) with statusCode 401 + WWW-Authenticate: Basic realm.
	if mode == authBasic {
		return &AuthError{
			Status:  401,
			Headers: map[string]string{"WWW-Authenticate": `Basic realm="Application"`},
			Msg:     "",
		}
	}
	var token string
	if (mode == authEither || mode == authToken) && r.URL.Query().Get("token") != "" {
		token = r.URL.Query().Get("token")
	} else if (mode == authEither || mode == authJWT) && r.Header.Get("Authorization") != "" {
		parts := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
		if len(parts) == 2 && parts[0] == "Bearer" {
			token = parts[1]
		}
	}
	if token == "" {
		return &AuthError{Status: 401, Headers: map[string]string{"WWW-Authenticate": "Bearer"}, Msg: "jwt missing"}
	}
	claims, err := a.decodeJWT(token)
	if err != nil {
		return &AuthError{
			Status:  401,
			Headers: map[string]string{"WWW-Authenticate": `Bearer error="invalid_token"`},
			Msg:     err.Error(),
		}
	}
	// Node: decoded.project_id.toString() !== params.project_id.toString()
	// (string comparison after JS toString on both sides).
	if projectID != "" {
		if claims.projectIDString() != projectID {
			return &AuthError{Status: 403, Msg: "Wrong project_id"}
		}
	}
	return nil
}

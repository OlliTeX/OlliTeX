// oauth2.go — ports Oauth2Filter.java 1:1 (the git-context auth filter).
//
// Java dispatch order (doFilter, applied to EVERY method incl. OPTIONS):
//
//  1. requestUri.startsWith("/project")  → 404 55B  (invalid prefix)
//  2. projectId = removeAllSuffixes(uri.split("/")[1], ".git");
//     Java "/".split("/") = [""] → split("/")[1] throws AIOOBE → 500 grid.
//  3. credential parse (StringTokenizer, LENIENT Base64):
//       header absent / blank / wrong scheme / decodes without ':'
//           → null → handleNeedAuthorization 401 287B + WWW-Authenticate
//       "Basic" with no second token → nextToken() throws NoSuchElement
//           → 500 grid
//  4. isLinkSharingId(projectId)          → 404 335B
//  5. !isProjectId(projectId)             → 404 188B
//  6. username == "git" → checkAccessToken(oauth2Server):
//       429                    → 429
//       401 + "token_expired"  → 401 (expired token)
//       401                    → 401 (bad token)
//       other >= 400           → 500
//       else (2xx/3xx)         → pass-through with the raw token
//  7. username != "git":
//       isUserPasswordEnabled → 403 deprecation (config-gated)
//       else                  → 401 (handleNeedAuthorization)
//
// Ordering note: step 3 (credential parse) fires BEFORE steps 4/5, so a bad
// project id with no creds → 401 (not 404); with creds → the project-id path.

package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"ollitex/go/services/gitbridge/util"
)

// OAuthClient is the token-check seam (Java Oauth2Filter.checkAccessToken):
// (token, clientIp) → (statusCode, errorCode). errorCode is the "error_code"
// JSON field of a 4xx response body ("" when absent / no error_code), read
// only in the 400 <= code < 500 range per Java parseErrorCode.
type OAuthClient func(token string, clientIp string) (int, string)

// oauthHTTPFunc adapts a raw func into the seam (identity conversion; keeps
// the shared.go call site unchanged).
func oauthHTTPFunc(f func(token string, clientIp string) (int, string)) OAuthClient {
	return OAuthClient(f)
}

// checkAccessTokenHTTP performs GET <oauth2Server>/oauth/token/info?client_ip=
// <ip> with "Authorization: Bearer <token>". Java sets
// setThrowExceptionOnExecuteError(false): transport-level failures surface
// as whatever the library reports — Go maps an HTTP/exec failure to 500
// (the "unexpected OAuth server" branch).
func checkAccessTokenHTTP(oauth2Server string, token, clientIp string) (int, string) {
	u := oauth2Server + "/oauth/token/info?client_ip=" + url.QueryEscape(clientIp)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return 500, ""
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 500, ""
	}
	defer resp.Body.Close()
	code := resp.StatusCode
	errorCode := ""
	if code >= 400 && code < 500 {
		// Java parseErrorCode: parse body as a JSON object, read the
		// "error_code" string, null on any parse error / missing field.
		body, _ := io.ReadAll(resp.Body)
		var obj map[string]interface{}
		if err := json.Unmarshal(body, &obj); err == nil {
			if v, ok := obj["error_code"].(string); ok && v != "" {
				errorCode = v
			}
		}
	}
	return code, errorCode
}

// credResult is the tri-state from the Basic-auth credential parse.
type credResult int

const (
	credNone credResult = iota // missing / malformed → 401 (handleNeedAuthorization)
	cred500                    // "Basic" with no token → StringTokenizer throw → 500 grid
	credOK                     // valid; user/pass returned
)

// parseBasicAuth ports getBasicAuthCredentials (StringTokenizer + lenient
// Base64 + split(":", 2)):
//
//	header absent / blank          → credNone   (Java hasMoreTokens false)
//	first token != "Basic"          → credNone   (equalsIgnoreCase)
//	"Basic" with no second token     → cred500    (nextToken throws NoSuchElement)
//	decode fails / decodes w/o ':'   → credNone   (split length != 2)
//	else                             → credOK     (user, pass)
func parseBasicAuth(r *http.Request) (user, pass string, res credResult) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", "", credNone
	}
	// Java StringTokenizer: whitespace-delimited, empty tokens dropped.
	f := strings.Fields(auth)
	if len(f) == 0 {
		return "", "", credNone
	}
	if !strings.EqualFold(f[0], "Basic") { // Java equalsIgnoreCase
		return "", "", credNone
	}
	if len(f) < 2 {
		// Java: st.nextToken() throws NoSuchElementException → propagates out
		// of doFilter → ProductionErrorHandler grid 500 (live-verified 28B).
		return "", "", cred500
	}
	decoded, ok := b64Lenient(f[1])
	if !ok {
		return "", "", credNone
	}
	// Java credentials.split(":", 2): limit 2, trailing empties kept.
	creds := string(decoded)
	idx := strings.Index(creds, ":")
	if idx < 0 {
		return "", "", credNone
	}
	return creds[:idx], creds[idx+1:], credOK
}

// b64Lenient approximates commons-codec decodeBase64 (ignores chars outside
// the Base64 alphabet). Go: strict StdEncoding, then RawStd (unpadded) as a
// fallback — an approximation (documented).
func b64Lenient(s string) (b []byte, ok bool) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, true
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, true
	}
	// commons-codec is lenient: a decode that yields nothing → empty → the
	// caller's split(":",2) yields length 1 → credNone (401). Approximate.
	return []byte{}, false
}

// handleOauth2 ports Oauth2Filter.doFilter.
//
// Returns (proceed, token):
//   - (false, "") — the filter short-circuited and has written a response.
//   - (true, token) — proceed to the servlet; token is the bearer password
//     (Java passes this as a servlet attribute consumed by the resolver).
func (s *Server) handleOauth2(w http.ResponseWriter, r *http.Request, path string) (proceed bool, token string) {
	// Step 1: "/project" prefix (checked before credential parse).
	if strings.HasPrefix(path, "/project") {
		sendOauthText(w, http.StatusNotFound,
			"Invalid Project ID (must not have a '/project' prefix)")
		return false, ""
	}
	// Step 2: projectId from the first URI segment minus ".git".
	// Java "​".split("/") drops trailing empties, so "/" alone → [""] and
	// split("/")[1] throws AIOOBE → the ProductionErrorHandler 500 grid.
	segs := util.SplitURIPath(path)
	if len(segs) < 2 {
		// ProductionErrorHandler 500 grid (28B, NO Content-Type — Java/Jetty
		// writes no CT; grid() keeps Go from defaulting to text/plain).
		grid(w, http.StatusInternalServerError)
		return false, ""
	}
	project := util.RemoveAllSuffixes(segs[1], ".git")

	// Step 3: credential parse (before the project-id branching).
	user, pass, res := parseBasicAuth(r)
	switch res {
	case cred500:
		grid(w, http.StatusInternalServerError)
		return false, ""
	case credNone:
		sendNoAuth(w)
		return false, ""
	}
	// credOK: user/pass set.

	// Step 4: link-sharing id.
	if util.IsLinkSharingID(project) {
		sendOauthText(w, http.StatusNotFound,
			"Git access via link sharing link is not supported.",
			"",
			"You can find the project's git remote url by opening it in your browser",
			"and selecting Git from the left sidebar in the project view.",
			"",
			"If this is unexpected, please contact us at support@overleaf.com, or",
			"see https://www.overleaf.com/learn/how-to/Git_integration for more information.")
		return false, ""
	}
	// Step 5: not a valid project id.
	if !util.IsProjectID(project) {
		sendOauthText(w, http.StatusNotFound,
			"This Overleaf project does not exist.",
			"",
			"If this is unexpected, please contact us at support@overleaf.com, or",
			"see https://www.overleaf.com/learn/how-to/Git_integration for more information.")
		return false, ""
	}

	// Step 6: username == "git" → token-validity check.
	if user == "git" {
		code, errorCode := s.oauth()(pass, util.ClientIp(r))
		switch {
		case code == 429:
			sendOauthText(w, http.StatusTooManyRequests,
				"Rate limit exceeded. Please wait and try again later.")
			return false, ""
		case code == 401 && errorCode == "token_expired":
			sendOauthText(w, http.StatusUnauthorized,
				"Your Overleaf Git authentication token has expired.",
				"",
				"Generate a new authentication token in your Overleaf Account Settings,",
				"then run the git command again.")
			return false, ""
		case code == 401:
			sendOauthText(w, http.StatusUnauthorized,
				"Enter your Git authentication token when prompted for a password.",
				"",
				"You can generate and manage your Git authentication tokens in",
				"your Overleaf Account Settings.",
				"",
				"See our help page for more support:",
				"https://www.overleaf.com/learn/how-to/Git_integration")
			return false, ""
		case code >= 400:
			sendOauthText(w, http.StatusInternalServerError,
				"Unexpected server error. Please try again later.")
			return false, ""
		}
		// 2xx/3xx → pass-through with the bearer token.
		return true, pass
	}

	// Step 7: username != "git".
	if s.cfg.IsUserPasswordEnabled() {
		// 403 deprecation (config-gated; live config defaults false).
		line1 := "Overleaf now only supports Git authentication tokens to access git. See: https://www.overleaf.com/learn/how-to/Git_integration_authentication_tokens"
		if strings.Contains(user, "@") {
			sendOauthText(w, http.StatusForbidden, line1)
		} else {
			sendOauthText(w, http.StatusForbidden,
				line1,
				"Please make sure your Git URL is correctly formatted. For example: https://git@git.overleaf.com/<YOUR_PROJECT_ID> or https://git:<AUTHENTICATION_TOKEN>@git.overleaf.com/<YOUR_PROJECT_ID>")
		}
		return false, ""
	}
	// userPasswordEnabled not set (live grid) → need-authorization 401.
	sendNoAuth(w)
	return false, ""
}

// oauth resolves the token-check seam (default: HTTP to cfg.Oauth2Server).
func (s *Server) oauth() OAuthClient {
	if s.oauthClient != nil {
		return s.oauthClient
	}
	u := s.cfg.Oauth2Server
	return oauthHTTPFunc(func(token string, clientIp string) (int, string) {
		return checkAccessTokenHTTP(u, token, clientIp)
	})
}

// sendOauthText ports Oauth2Filter.sendResponse: CT text/plain;charset=
// iso-8859-1 (Java Jetty default charset — set explicitly so Go does not
// sniff), each line followed by "\n".
func sendOauthText(w http.ResponseWriter, code int, lines ...string) {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	w.Header().Set("Content-Type", "text/plain;charset=iso-8859-1")
	w.WriteHeader(code)
	io.WriteString(w, b.String()) //nolint:errcheck // write failure: connection-level, no parity value
}

// sendNoAuth ports handleNeedAuthorization: WWW-Authenticate header + 401 +
// the 287B "log in with git/…" body.
func sendNoAuth(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Git Bridge"`)
	w.Header().Set("Content-Type", "text/plain;charset=iso-8859-1")
	w.WriteHeader(http.StatusUnauthorized)
	io.WriteString(w,
		"Log in with the username 'git' and enter your Git authentication token\n"+
			"when prompted for a password.\n"+
			"\n"+
			"You can generate and manage your Git authentication tokens in\n"+
			"your Overleaf Account Settings.\n"+
			"\n"+
			"See our help page for more support:\n"+
			"https://www.overleaf.com/learn/how-to/Git_integration\n") //nolint:errcheck
}

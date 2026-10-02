package gsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// ---------- errors (Node GitSyncErrors.mjs) ----------

type gsError struct {
	Status  int
	Message string
	Detail  string // bridge error detail passthru (string or JSON)
}

func (e *gsError) Error() string { return e.Message }

var (
	errGsNoToken   = &gsError{Status: 400, Message: "no user token"}
	errGsBadCred   = &gsError{Status: 400, Message: "invalid token"}
	errGsNotLinked = &gsError{Status: 404, Message: "Project is not linked with GitHub"}
)

// gsInvalidToken — Node InvalidTokenError(message, {status}).
func gsInvalidToken(msg string, status int) *gsError {
	if status == 0 {
		status = 400
	}
	return &gsError{Status: status, Message: msg}
}

// gsDefaultServerUrl — Node getDefaultServerUrl.
func gsDefaultServerUrl(provider string) string {
	switch provider {
	case "gitlab":
		return "https://gitlab.com"
	case "gitea":
		return "https://gitea.io"
	case "forgejo":
		return "https://forgejo.org"
	default:
		return "https://github.com"
	}
}

var gsValidProviders = map[string]bool{"github": true, "gitlab": true, "gitea": true, "forgejo": true}

func gsNormURL(u string) string { return strings.TrimSuffix(strings.TrimSpace(u), "/") }

// ---------- TokenManager port ----------

// gsSavePAT — Node _saveUserPAT (read-modify-write under the user lock).
func gsSavePAT(ctx context.Context, a *core.App, uid, provider, serverUrl, username, pat string) error {
	return gsWithUserLock(uid, func() error {
		enc, err := gsEncrypt(pat)
		if err != nil {
			return fmt.Errorf("failed to encrypt token: %w", err)
		}
		nurl := gsNormURL(serverUrl)
		cleanU := strings.TrimSpace(username)
		now := time.Now().UTC()

		doc := gsGetCredsDoc(ctx, a, uid)
		if doc == nil {
			doc = &gsCredsDoc{UserID: uid, CreatedAt: now}
		}
		if doc.Tokens == nil {
			doc.Tokens = map[string]map[string]any{}
		}
		if doc.Tokens[provider] == nil {
			doc.Tokens[provider] = map[string]any{}
		}
		bucket := doc.Tokens[provider][nurl]
		usernameMap := map[string]any{}
		switch b := bucket.(type) {
		case string:
			lu := gsLegacyUsername(doc, provider, nurl)
			usernameMap[lu] = b
		case map[string]any:
			for k, v := range b {
				usernameMap[k] = v
			}
		}
		usernameMap[cleanU] = enc
		doc.Tokens[provider][nurl] = usernameMap

		// servers bookkeeping
		if doc.Servers == nil {
			doc.Servers = map[string]map[string]any{}
		}
		if doc.Servers[provider] == nil {
			doc.Servers[provider] = map[string]any{}
		}
		sb := doc.Servers[provider][nurl]
		serverMap := map[string]any{}
		switch s := sb.(type) {
		case map[string]any:
			if _, isLegacy := s["username"]; isLegacy {
				lu, _ := s["username"].(string)
				if lu != "" {
					ca, _ := s["createdAt"].(time.Time)
					if ca.IsZero() {
						ca = now
					}
					serverMap[lu] = gsServerEntry{CreatedAt: ca, LastUsedAt: now}
				}
			} else {
				for k, v := range s {
					serverMap[k] = v
				}
			}
		}
		var prev gsServerEntry
		if pe, ok := serverMap[cleanU].(gsServerEntry); ok {
			prev = pe
		}
		if prev.CreatedAt.IsZero() {
			prev.CreatedAt = now
		}
		serverMap[cleanU] = gsServerEntry{CreatedAt: prev.CreatedAt, LastUsedAt: now}
		doc.Servers[provider][nurl] = serverMap

		return gsUpsertCredsDoc(ctx, a, doc)
	})
}

// gsLegacyUsername — servers[p][url].username (legacy single-entry shape).
func gsLegacyUsername(doc *gsCredsDoc, provider, nurl string) string {
	if doc.Servers[provider][nurl] != nil {
		switch m := doc.Servers[provider][nurl].(type) {
		case map[string]any:
			if u, ok := m["username"].(string); ok {
				return u
			}
		case bson.D:
			for i := range m {
				if m[i].Key == "username" {
					if u, ok := m[i].Value.(string); ok {
						return u
					}
				}
			}
		}
	}
	return ""
}

// gsGetPATCred — Node getUserPATCredentials: resolve the token for
// (provider, serverUrl, username) with the GitHub OAuth slot as the
// github.com fallback.
type gsResolvedCred struct {
	Provider  string
	Token     string
	ServerURL string
	Username  string
	Source    string // "pat" | "oauth"
}

func gsGetPATCred(ctx context.Context, a *core.App, uid, provider, serverUrl, username string) (*gsResolvedCred, error) {
	nurl := gsNormURL(serverUrl)
	wantU := strings.TrimSpace(username)

	doc := gsGetCredsDoc(ctx, a, uid)
	if doc == nil {
		return nil, gsInvalidToken("no user token", 400)
	}

	var bucket any
	if mp := doc.Tokens[provider]; mp != nil {
		bucket = mp[nurl]
	}
	legacyU := gsLegacyUsername(doc, provider, nurl)
	entries := gsEnumerateBucket(bucket, legacyU)

	var match *gsPatEntry
	if wantU != "" {
		for i := range entries {
			if entries[i].Username == wantU {
				match = &entries[i]
				break
			}
		}
	} else if len(entries) == 1 {
		match = &entries[0]
	} else if len(entries) > 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			n := e.Username
			if n == "" {
				n = "(unknown)"
			}
			names = append(names, n)
		}
		return nil, gsInvalidToken(fmt.Sprintf("multiple git accounts are linked for %s %s; specify one of: %s", provider, nurl, strings.Join(names, ", ")), 400)
	}

	var token string
	resolvedU := ""
	if match != nil {
		resolvedU = match.Username
	}
	source := "pat"
	if match != nil && match.Value != "" {
		t, ok := gsDecrypt(match.Value)
		if !ok {
			return nil, errGsBadCred
		}
		token = t
	} else if provider == "github" && nurl == gsDefaultServerUrl("github") {
		if slotTok, linkedU := gsOAuthSlotToken(doc); slotTok != "" {
			t, ok := gsDecrypt(slotTok)
			if !ok {
				return nil, errGsBadCred
			}
			token = t
			resolvedU = wantU
			if resolvedU == "" {
				resolvedU = linkedU
			}
			source = "oauth"
		}
	}

	if token == "" {
		return nil, gsInvalidToken(fmt.Sprintf("no token for %s server %s", provider, nurl), 400)
	}
	return &gsResolvedCred{Provider: provider, Token: token, ServerURL: nurl, Username: resolvedU, Source: source}, nil
}

// gsOAuthSlotToken — returns (encryptedToken, username) from the github
// slot (string legacy or object).
func gsOAuthSlotToken(doc *gsCredsDoc) (string, string) {
	if doc == nil {
		return "", ""
	}
	switch v := doc.GitHub.(type) {
	case string:
		return v, ""
	case map[string]any:
		tok, _ := v["token"].(string)
		uname, _ := v["username"].(string)
		if tok != "" {
			return tok, uname
		}
	case bson.D:
		var tok, uname string
		for i := range v {
			switch v[i].Key {
			case "token":
				tok, _ = v[i].Value.(string)
			case "username":
				uname, _ = v[i].Value.(string)
			}
		}
		if tok != "" {
			return tok, uname
		}
	}
	return "", ""
}

// gsGetOAuth — Node getOAuth: {linked, username}.
// nil-safe: a user WITHOUT a credential doc (fresh account) must read as
// unlinked, not panic (statusHandler passes the raw query result).
func gsGetOAuth(doc *gsCredsDoc) (bool, string) {
	if doc == nil {
		return false, ""
	}
	switch v := doc.GitHub.(type) {
	case string:
		return true, ""
	case map[string]any:
		if tok, ok := v["token"].(string); ok && tok != "" {
			uname, _ := v["username"].(string)
			return true, uname
		}
	}
	return false, ""
}

// gsGetPublicServers — Node getPublicServers: the wire rows.
func gsGetPublicServers(doc *gsCredsDoc) []gsServerRow {
	out := []gsServerRow{}
	if doc == nil {
		return out
	}
	// providers in stable (sorted) order
	providers := dedupeSorted(func() []string {
		out := make([]string, 0, len(doc.Tokens))
		for p := range doc.Tokens {
			out = append(out, p)
		}
		return out
	}())

	_ = json.Marshal
	for _, p := range providers {
		urls := doc.Tokens[p]
		for u := range urls {
			legacyU := gsLegacyUsername(doc, p, u)
			for _, e := range gsEnumerateBucket(urls[u], legacyU) {
				uname := e.Username
				if uname == "" {
					uname = legacyU
				}
				out = append(out, gsServerRow{
					ID:       gsServerId(p, u, uname),
					Provider: p,
					URL:      u,
					Username: uname,
					Source:   "pat",
				})
			}
		}
	}
	if tok, uname := gsOAuthSlotToken(doc); tok != "" {
		u := gsDefaultServerUrl("github")
		out = append(out, gsServerRow{
			ID:       gsServerId("github", u, uname),
			Provider: "github",
			URL:      u,
			Username: uname,
			Source:   "oauth",
		})
	}
	return out
}

type gsServerRow struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Source   string `json:"source"`
}

// gsRemovePAT — Node _removeUserPAT.
func gsRemovePAT(ctx context.Context, a *core.App, uid, provider, serverUrl, username string, removeUsername bool) error {
	return gsWithUserLock(uid, func() error {
		nurl := gsNormURL(serverUrl)
		doc := gsGetCredsDoc(ctx, a, uid)
		if doc == nil {
			return nil
		}
		// remove from tokens
		if doc.Tokens[provider] != nil {
			bucket := doc.Tokens[provider][nurl]
			tu := strings.TrimSpace(username)
			if !removeUsername {
				delete(doc.Tokens[provider], nurl)
			} else if isStr := func() bool { _, ok := bucket.(string); return ok }(); isStr {
				delete(doc.Tokens[provider], nurl)
			} else if m, isM := bucket.(map[string]any); isM {
				delete(m, tu)
				if len(m) == 0 {
					delete(doc.Tokens[provider], nurl)
				}
			} else if d, isD := bucket.(bson.D); isD {
				kept := bson.D{}
				for i := range d {
					if d[i].Key == tu {
						continue
					}
					kept = append(kept, d[i])
				}
				if len(kept) == 0 {
					delete(doc.Tokens[provider], nurl)
				} else {
					doc.Tokens[provider][nurl] = kept
				}
			}
		}
		// remove from servers
		tu := strings.TrimSpace(username)
		if doc.Servers[provider] != nil {
			sb := doc.Servers[provider][nurl]
			if !removeUsername {
				delete(doc.Servers[provider], nurl)
			} else if m, isM := sb.(map[string]any); isM {
				if _, isLegacy := m["username"]; isLegacy {
					delete(doc.Servers[provider], nurl)
				} else {
					delete(m, tu)
					if len(m) == 0 {
						delete(doc.Servers[provider], nurl)
					}
				}
			} else if d, isD := sb.(bson.D); isD {
				kept := bson.D{}
				legacy := false
				for i := range d {
					if d[i].Key == "username" {
						legacy = true
						continue
					}
					if d[i].Key == tu {
						continue
					}
					kept = append(kept, d[i])
				}
				if legacy || len(kept) == 0 {
					delete(doc.Servers[provider], nurl)
				} else {
					doc.Servers[provider][nurl] = kept
				}
			}
		}
		return gsUpsertCredsDoc(ctx, a, doc)
	})
}

// gsRemoveGitHubOAuth — Node removeUserToken (the github OAuth slot).
func gsRemoveGitHubOAuth(ctx context.Context, a *core.App, uid string) error {
	return gsWithUserLock(uid, func() error {
		doc := gsGetCredsDoc(ctx, a, uid)
		if doc == nil {
			return nil
		}
		doc.GitHub = nil
		return gsUpsertCredsDoc(ctx, a, doc)
	})
}

// gsSaveGitHubOAuth — Node saveOAuth (the github slot).
func gsSaveGitHubOAuth(ctx context.Context, a *core.App, uid, tokenEncrypted, username string) error {
	return gsWithUserLock(uid, func() error {
		doc := gsGetCredsDoc(ctx, a, uid)
		if doc == nil {
			doc = &gsCredsDoc{UserID: uid, CreatedAt: time.Now().UTC()}
		}
		doc.GitHub = map[string]any{
			"token":    tokenEncrypted,
			"username": username,
			"linkedAt": time.Now().UTC(),
		}
		return gsUpsertCredsDoc(ctx, a, doc)
	})
}

// helpers
func dedupeSorted(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

var _ = errors.New

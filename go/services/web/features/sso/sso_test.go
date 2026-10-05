package sso

// sso_test.go — unit tests for the N-provider SSO surface (oracle-
// pinned). Mongo-dependent paths (JIT, admin save) are exercised live
// (E2E); these cover the pure/plumbing logic: attrFilter role matrix,
// domain gate, deflateRaw round-trip, SAML wire builders (AuthnRequest
// via crewjam + LogoutRequest SLO URL), OIDC endpoints resolution,
// env-provider resolve, SLO URL builders.

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// --- roles (P1c oracle matrix) ---

func TestRoles_Matrix(t *testing.T) {
	cases := []struct {
		name  string
		rules []AttrRule
		prof  ssoProfile
		want  string
	}{
		{"no rules ⇒ local", nil, ssoProfile{"eduPersonScope": "x"}, "local"},
		{"equals scalar match",
			[]AttrRule{{Attribute: "ent", Values: []string{"urn:staff"}, Role: "guest"}},
			ssoProfile{"ent": "urn:staff"}, "guest"},
		{"equals no match",
			[]AttrRule{{Attribute: "ent", Values: []string{"urn:staff"}, Role: "guest"}},
			ssoProfile{"ent": "urn:other"}, "local"},
		{"caseInsensitive",
			[]AttrRule{{Attribute: "ent", Values: []string{"URN:Staff"}, Role: "blocked", CaseSensitive: false}},
			ssoProfile{"ent": "urn:staff"}, "blocked"},
		{"includes scalar substring",
			[]AttrRule{{Attribute: "cn", Values: []string{"research"}, Role: "guest", Match: "includes"}},
			ssoProfile{"cn": "Research Group A"}, "guest"},
		{"includes list membership",
			[]AttrRule{{Attribute: "ents", Values: []string{"urn:student"}, Role: "guest", Match: "includes"}},
			ssoProfile{"ents": []string{"urn:foo", "urn:student"}}, "guest"},
		{"regex match",
			[]AttrRule{{Attribute: "mail", Values: []string{`.+@staff\.ex$`}, Role: "blocked", Match: "regex"}},
			ssoProfile{"mail": "j@staff.ex"}, "blocked"},
		{"regex invalid ⇒ no match",
			[]AttrRule{{Attribute: "x", Values: []string{"(**bad**"}, Role: "blocked", Match: "regex"}},
			ssoProfile{"x": "v"}, "local"},
		{"first row wins (order)",
			[]AttrRule{
				{Attribute: "a", Values: []string{"1"}, Role: "guest"},
				{Attribute: "a", Values: []string{"1"}, Role: "blocked"},
			},
			ssoProfile{"a": "1"}, "guest"},
		{"local row inert",
			[]AttrRule{
				{Attribute: "a", Values: []string{"1"}, Role: "local"},
				{Attribute: "a", Values: []string{"1"}, Role: "guest"},
			},
			ssoProfile{"a": "1"}, "guest"},
		{"absent attribute",
			[]AttrRule{{Attribute: "missing", Values: []string{"1"}, Role: "blocked"}},
			ssoProfile{}, "local"},
		{"array claim equals single value",
			[]AttrRule{{Attribute: "scopes", Values: []string{"urn:geant:staff"}, Role: "guest"}},
			ssoProfile{"scopes": []string{"urn:geant:staff"}}, "guest"},
	}
	for _, tc := range cases {
		if got := evaluateAttrFilter(tc.rules, tc.prof).role(); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestSanitizeAttrRule(t *testing.T) {
	out := sanitizeAttrRule(AttrRule{Attribute: "", Role: "blocked"})
	if out.Attribute != "" {
		t.Errorf("row without attribute should stay inert")
	}
	out2 := sanitizeAttrRule(AttrRule{Attribute: "a", Values: []string{"x", "y", "z"}, Role: "admin"})
	if out2.Role != "local" || out2.Match != "equals" {
		t.Errorf("unexpected sanitize: %+v", out2)
	}
}

func TestPersistedRoleForProvider(t *testing.T) {
	roles := map[string]map[string]any{
		"p1": {"role": "guest", "at": 0},
		"p2": {"role": "local", "at": 0},
	}
	if got := persistedRoleForProvider(roles, "p1"); got != "guest" {
		t.Errorf("p1: %q", got)
	}
	if got := persistedRoleForProvider(roles, "p2"); got != "local" {
		t.Errorf("p2: %q", got)
	}
	if got := persistedRoleForProvider(roles, "nope"); got != "local" {
		t.Errorf("absent: %q", got)
	}
}

// --- domain gate ---

func TestDomainAllowed(t *testing.T) {
	if !domainAllowed("a@staff.unibremen.de", "staff.unibremen.de") {
		t.Error("exact domain should pass")
	}
	if !domainAllowed("a@sub.unibremen.de", "*.unibremen.de") {
		t.Error("*.base suffix should pass")
	}
	if domainAllowed("a@evil.com", "staff.unibremen.de") {
		t.Error("other domain must fail")
	}
	if domainAllowed("a@unibremen.de.attacker.io", "*.unibremen.de") {
		t.Error("suffix-with-more must fail")
	}
}

// --- deflateRaw round-trip (Node zlib.deflateRawSync parity) ---

func TestDeflateRaw_RoundTrip(t *testing.T) {
	in := []byte(`<samlp:AuthnRequest xmlns="urn:oasis:names:tc:SAML:2.0:protocol">x</samlp:AuthnRequest>`)
	out := deflateRaw(in)
	if len(out) == 0 {
		t.Fatal("deflateRaw empty")
	}
	// raw DEFLATE must NOT start with the zlib header 0x78 (Node
	// deflateRaw contract — the decompress heuristic keys on 0x78 for
	// zlib frames).
	if out[0] == 0x78 {
		t.Error("deflateRaw output looks like a zlib frame (must be raw)")
	}
	// raw stream: inflate with a raw-DEFLATE reader (the Node
	// decompress heuristic keys on the 0x78 zlib header; raw streams
	// are inflateRaw'd):
	raw, err := rawInflate(out)
	if err != nil {
		t.Fatalf("raw inflate: %v", err)
	}
	if string(raw) != string(in) {
		t.Error("round trip mismatch")
	}
}

// rawInflate — raw DEFLATE (no zlib header) inflate for tests.
func rawInflate(b []byte) ([]byte, error) {
	r := rawDeflateReader(b)
	out := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if err != nil {
			if err == io.EOF {
				return out, nil
			}
			return out, err
		}
	}
}

// rawDeflateReader — raw-DEFLATE reader (compress/flate).
func rawDeflateReader(b []byte) io.Reader {
	return newFlateReader(b)
}

func newFlateReader(b []byte) io.Reader {
	r, _ := newFlateReaderRaw(b)
	return r
}

// --- env providers ---

func TestEnvSAMLProvider(t *testing.T) {
	t.Setenv("EXTERNAL_AUTH", "saml,oidc")
	t.Setenv("OVERLEAF_SAML_ISSUER", "https://idp.example/idp")
	t.Setenv("OVERLEAF_SAML_ENTRYPOINT", "https://idp.example/sso")
	t.Setenv("OVERLEAF_SAML_IDP_CERT", "PEM")
	p := envSAMLProvider()
	if p == nil {
		t.Fatal("env saml provider expected")
	}
	if p.ID != "1" || p.Issuer != "https://idp.example/idp" || p.EntryPoint != "https://idp.example/sso" {
		t.Errorf("unexpected env saml: %+v", p)
	}
	q := envOIDCProvider()
	if q == nil || q.ID != "1" || q.Scope != "openid profile email" {
		t.Errorf("unexpected env oidc: %+v", q)
	}
}

func TestEnvProvidersDisabled(t *testing.T) {
	t.Setenv("EXTERNAL_AUTH", "ldap")
	if envSAMLProvider() != nil || envOIDCProvider() != nil {
		t.Error("no saml/oidc env providers expected when EXTERNAL_AUTH lacks them")
	}
}

// --- resolve precedence (DB beats env) ---

func TestResolveSAMLProvider_DBBeatsEnv(t *testing.T) {
	t.Setenv("EXTERNAL_AUTH", "saml")
	t.Setenv("OVERLEAF_SAML_ENTRYPOINT", "https://env.idp/sso")
	p, ok := resolveSAMLProvider(nil, "saml")
	if !ok || p.EntryPoint != "https://env.idp/sso" {
		t.Errorf("env fallback failed: %+v", p)
	}
	p2, ok2 := resolveSAMLProvider(nil, "dbid")
	if ok2 {
		t.Error("unknown DB id must not resolve without DB config")
	}
	_ = p2
}

// --- SLO URL builders ---

func TestSamlSLOURL_TShape(t *testing.T) {
	p := &SAMLProvider{
		ID:        "1",
		Type:      "saml",
		Issuer:    "https://idp.example",
		LogoutURL: "https://idp.example/slo",
	}
	_ = p
	got := samlSLOURLRaw(p, "https://idp.example", "")
	if !strings.HasPrefix(got, "https://idp.example/slo?SAMLRequest=") {
		t.Fatalf("SLO url shape: %q", got)
	}
	i := strings.Index(got, "?SAMLRequest=")
	if i < 0 {
		t.Fatalf("no SAMLRequest query in %q", got)
	}
	req := got[i+len("?SAMLRequest="):]
	if req == "" {
		t.Fatal("SAMLRequest param empty")
	}
	var perr error
	req, perr = url.QueryUnescape(req)
	if perr != nil {
		t.Fatalf("unescape: %v", perr)
	}
	rawReq, berr := base64.StdEncoding.DecodeString(req)
	if berr != nil {
		t.Fatalf("b64: %v", berr)
	}
	expanded, eerr := rawInflate(rawReq)
	if eerr != nil {
		t.Fatalf("inflate: %v", eerr)
	}
	req = string(expanded)
	if !strings.Contains(req, "<samlp:LogoutRequest") || !strings.Contains(req, "<saml:Issuer>https://idp.example</saml:Issuer>") {
		t.Errorf("LogoutRequest body: %q", req)
	}
}

func TestOidcSLOURL_TShape(t *testing.T) {
	got := oidcSLOURLRaw(&OIDCProvider{LogoutURL: "https://idp.example/logout"}, "https://site.internal", "tok123")
	if got != "https://idp.example/logout?id_token_hint=tok123&post_logout_redirect_uri=https%3A%2F%2Fsite.internal" {
		t.Errorf("OIDC SLO url: %q", got)
	}
}

// --- OIDC endpoints (discovery) ---

func TestOidcEndpoints_Discovery(t *testing.T) {
	var tsu string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			t.Fatalf("path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issuer": "` + tsu + `",
			"authorization_endpoint": "` + tsu + `/auth",
			"token_endpoint": "` + tsu + `/token",
			"userinfo_endpoint": "` + tsu + `/user",
			"jwks_uri": "` + tsu + `/jwks",
			"response_types_supported": ["code"]
		}`))
	}))

	defer ts.Close()
	tsu = ts.URL
	auth, token, userinfo, err := oidcEndpoints(context.Background(), &OIDCProvider{Issuer: ts.URL, ClientID: "c", ClientSecret: "s"})
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if auth != ts.URL+"/auth" || token != ts.URL+"/token" || userinfo != ts.URL+"/user" {
		t.Errorf("endpoints: %q %q %q", auth, token, userinfo)
	}
}

func TestOidcEndpoints_ExplicitWins(t *testing.T) {
	auth, token_, userinfo, err := oidcEndpoints(context.Background(), &OIDCProvider{
		Issuer:           "https://ignored",
		AuthorizationURL: "https://a/auth",
		TokenURL:         "https://a/token",
		UserInfoURL:      "https://a/me",
	})
	if err != nil || auth != "https://a/auth" || token_ != "https://a/token" || userinfo != "https://a/me" {
		t.Errorf("explicit endpoints: %q %q %q err=%v", auth, token_, userinfo, err)
	}
}

// --- route surface (Node parity) ---

func TestRoutePatterns_T(t *testing.T) {
	expect := []struct {
		re   func() (s bool)
		path string
	}{
		{func() bool { return samlLoginPattern.MatchString("/saml/login") }, "/saml/login"},
		{func() bool { return samlProviderPattern.MatchString("/saml/login/uni-bremen") }, "/saml/login/uni-bremen"},
		{func() bool { return !samlProviderPattern.MatchString("/saml/login/a b") }, "/saml/login/a b"},
		{func() bool { return samlCBPattern.MatchString("/saml/login/callback") }, "/saml/login/callback"},
		{func() bool { return samlMetaPattern.MatchString("/saml/meta") }, "/saml/meta"},
		{func() bool { return oidcLoginPattern.MatchString("/oidc/login") }, "/oidc/login"},
		{func() bool { return oidcProviderPattern.MatchString("/oidc/login/geant") }, "/oidc/login/geant"},
		{func() bool { return oidcCBPattern.MatchString("/oidc/login/callback") }, "/oidc/login/callback"},
		{func() bool { return samlLogoutPattern.MatchString("/saml/logout/callback") }, "/saml/logout/callback"},
		{func() bool { return oidcLogoutPattern.MatchString("/oidc/logout/callback") }, "/oidc/logout/callback"},
		{func() bool { return unlinkPattern.MatchString("/user/oauth-unlink") }, "/user/oauth-unlink"},
	}
	for i, e := range expect {
		if !e.re() {
			t.Errorf("route %d (%s) did not assert", i, e.path)
		}
	}
}

// --- newSamlID ---

func TestNewSamlID_TShape(t *testing.T) {
	id := newSamlID()
	if !strings.HasPrefix(id, "_") || len(id) != 1+40 {
		t.Fatalf("id shape %q (want _ + 40 hex)", id)
	}
	for i := 1; i < len(id); i++ {
		c := id[i]
		ok := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !ok {
			t.Fatalf("non-hex char %q in %q", c, id)
		}
	}
}

var _ = os.Getenv

package sso

// samlogin.go — SAML login/ACS/meta/SLO handlers (routes in sso.go).
//
// Node pins (overleaf-fed modules/authentication/saml + 6.3.0_post base):
//
//   - GET /saml/login[/:providerId]
//       session samlProviderId = <id> (env: '1' | DB row id)
//       302 {entryPoint}?SAMLRequest=b64(deflate(AuthnRequest))[&RelayState]
//       (authnRequestBinding HTTP-POST ⇒ auto-submit form, 200 text/html)
//       unknown id ⇒ 404 "SAML provider '<id>' not found or disabled"
//       no entryPoint ⇒ 400 {"message":{"text":"Missing SAML entryPoint",…}}
//   - POST /saml/login/callback — SAMLResponse (b64 XML) parse+verify
//     (crewjam == node-saml semantics), status success, profile map
//     (nameID + attributes + SessionIndex), P1c role evaluation
//     (blocked ⇒ 401 {'message':{text:'Login denied by SSO role
//     filter',type:'error',status:401}} + audit sso-login-denied,
//     BEFORE any account write), JIT (samlJIT), session samlExtce
//     {nameID,sessionIndex}+provider, externalAuth 'saml',
//     ssoRoles[ssoLoginProviderId] stamp, finish (302/{"redir"}).
//   - GET /saml/logout/callback — IdP SLO tail ⇒ 302 /login.
//   - GET /saml/meta — SP metadata XML, attachment, filename
//     '{entityID-ish}-meta.xml'.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"

	"ollitex/go/services/web/core"

	saml "github.com/crewjam/saml"
)

// pathProviderID — named capture from the route (empty for the bare path).
func pathProviderID(cxt *core.Cxt) string {
	if cxt.Params == nil {
		return ""
	}
	return cxt.Params["providerId"]
}

// samlLogin — GET /saml/login and /saml/login/:providerId.
func samlLogin(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		pathID := pathProviderID(cxt)
		var cfg *SSOConfig
		if db, err := ssoDB(a, cxt); err != nil {
			if pathID == "" || pathID == "saml" {
				// env mode does not need Mongo — continue with cfg nil.
			} else {
				dbErr500(cxt, res)
				return
			}
		} else {
			cfg = loadSSOConfig(db, cxt)
		}
		p, ok := resolveSAMLProvider(cfg, pathID)
		if !ok {
			id := pathID
			if id == "" {
				id = "saml"
			}
			res.PlainText(404, "SAML provider '"+id+"' not found or disabled")
			return
		}
		if p.EntryPoint == "" {
			res.JSON(400, []byte(`{"message":{"text":"Missing SAML entryPoint","type":"error","status":400}}`))
			return
		}
		// Node: session.samlProviderId (ACS dispatch).
		if cxt.Sess != nil {
			cxt.Sess.Set("samlProviderId", p.ID)
			cxt.Sess.Set("ssoProviderId", p.ID)
		}
		sp := buildSP(p, cfgSPConfig(cfg), cxt.SiteURL)
		relay := cxt.Req.URL.Query().Get("RelayState")
		// Request binding: node-saml defaults to HTTP-Redirect (302);
		// 'http-post' ⇒ auto-submit form.
		if strings.EqualFold(p.AuthnRequestBinding, "http-post") {
			body, err := sp.MakePostAuthenticationRequest(relay)
			if err != nil {
				dbErr500(cxt, res)
				return
			}
			res.HTML(200, string(body))
			return
		}
		u, err := sp.MakeRedirectAuthenticationRequest(relay)
		if err != nil {
			dbErr500(cxt, res)
			return
		}
		res.Redirect(cxt.Req, 302, u.String())
	}
}

func cfgSPConfig(cfg *SSOConfig) *SPConfig {
	if cfg == nil {
		return nil
	}
	return cfg.SPMetadata
}

// samlACS — POST /saml/login/callback (single ACS endpoint).
func samlACS(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		providerID := ""
		var reqID string
		if cxt.Sess != nil {
			if raw, ok := cxt.Sess.GetRaw("samlProviderId"); ok {
				_ = json.Unmarshal(raw, &providerID)
			}
			if raw, ok := cxt.Sess.GetRaw("ssoSamlReqId"); ok {
				_ = json.Unmarshal(raw, &reqID)
			}
		}
		var cfg *SSOConfig
		db, err := ssoDB(a, cxt)
		if err != nil {
			dbErr500(cxt, res)
			return
		}
		cfg = loadSSOConfig(db, cxt)
		p, ok := resolveSAMLProvider(cfg, providerID)
		if !ok {
			res.JSON(401, []byte(`{"message":{"text":"SAML provider from session not found","type":"error","status":401}}`))
			return
		}
		rawSAMLResponse := cxt.Req.FormValue("SAMLResponse")
		if rawSAMLResponse == "" {
			res.JSON(400, []byte(`{"message":{"text":"Missing SAMLResponse","type":"error","status":400}}`))
			return
		}
		xmlBytes, decErr := base64.StdEncoding.DecodeString(rawSAMLResponse)
		if decErr != nil {
			res.JSON(400, []byte(`{"message":{"text":"Invalid SAMLResponse encoding","type":"error","status":400}}`))
			return
		}
		// node-saml decompress heuristic: 0x78 ⇒ zlib, else raw deflate,
		// else plaintext (crewjam expects plaintext XML — apply the
		// heuristic first).
		if len(xmlBytes) >= 2 && xmlBytes[0] == 0x78 {
			if z, zerr := nodeSamlInflate(xmlBytes); zerr == nil {
				xmlBytes = z
			}
		}
		sp := buildSP(p, cfgSPConfig(cfg), cxt.SiteURL)
		possIDs := []string{}
		if reqID != "" {
			possIDs = append(possIDs, reqID)
		}
		acsURL, _ := url.Parse(cxt.SiteURL + "/saml/login/callback")
		assertion, perr := sp.ParseXMLResponse(xmlBytes, possIDs, *acsURL)
		if perr != nil {
			// Node: error ⇒ 401 {"message":{…}} (handleAuthenticateErrors
			// default shape) + audit saml failure (Node 'saml-failed' log).
			samlLog(a, cxt, p.ID, "fail", perr.Error())
			res.JSON(401, []byte(`{"message":{"text":"SAML authentication failed","type":"error","status":401}}`))
			return
		}
		profile := assertionToProfile(assertion)
		// P1c: role BEFORE any account write (blocked ⇒ refuse + audit).
		role := evaluateAttrFilter(p.AttrFilter, profile).role()
		if role == "blocked" {
			samlLog(a, cxt, p.ID, "denied", "attrFilter blocked")
			auditSsoDenied(a, cxt, "", p.ID, "saml-attrFilter-blocked")
			res.JSON(401, []byte(`{"message":{"text":"Login denied by SSO role filter","type":"error","status":401}}`))
			return
		}
		user, jerr := samlJIT(cxt.Req.Context(), db, p, profile, p.ID, role)
		if jerr != nil {
			res.JSON(500, []byte(`{"message":{"text":"SAML user lookup failed","type":"error","status":500}}`))
			return
		}
		// Node session fields (logout.mjs + getProfile parity):
		if cxt.Sess != nil {
			cxt.Sess.Set("samlProviderId", p.ID)
			cxt.Sess.Set("ssoProviderId", p.ID)
		}
		sessFields := map[string]any{
			"samlExtce": profileSubset(profile, "nameID", "sessionIndex"),
		}
		finishSSOLogin(a, cxt, res, user, "saml", p.ID, sessFields, cxt.Req.URL.Query().Get("redir"))
	}
}

// assertionToProfile — Node profile shape: nameID + attributes (Name →
// scalar | []string) + sessionIndex (AuthnStatement).
func assertionToProfile(assertion *saml.Assertion) ssoProfile {
	profile := ssoProfile{}
	if assertion.Subject != nil && assertion.Subject.NameID != nil {
		profile["nameID"] = assertion.Subject.NameID.Value
	}
	for _, st := range assertion.AttributeStatements {
		for _, at := range st.Attributes {
			values := make([]string, 0, len(at.Values))
			for _, v := range at.Values {
				values = append(values, v.Value)
			}
			if len(values) == 1 {
				profile[at.Name] = values[0]
			} else {
				profile[at.Name] = values
			}
		}
	}
	for _, s := range assertion.AuthnStatements {
		if s.SessionIndex != "" {
			profile["sessionIndex"] = s.SessionIndex
		}
	}
	return profile
}

func profileSubset(profile ssoProfile, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		if v, ok := profile[k]; ok {
			out[k] = v
		}
	}
	return out
}

// samlLogoutCallbackH — GET /saml/logout/callback (Node SLO tail).
func samlLogoutCallbackH(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		res.Redirect(cxt.Req, 302, "/login")
	}
}

// samlSPMetadata — GET /saml/meta (Node getSPMetadata: attachment,
// filename '{issuer}-meta.xml').
func samlSPMetadata(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		var cfg *SSOConfig
		if db, err := ssoDB(a, cxt); err == nil {
			cfg = loadSSOConfig(db, cxt)
		}
		p, ok := resolveSAMLProvider(cfg, "")
		if !ok {
			res.PlainText(404, "SAML provider 'saml' not found or disabled")
			return
		}
		sp := buildSP(p, cfgSPConfig(cfg), cxt.SiteURL)
		md := sp.Metadata()
		var metaBuf bytes.Buffer
		enc := xml.NewEncoder(&metaBuf)
		enc.Indent("", "  ")
		if merr := enc.Encode(md); merr != nil {
			res.PlainText(500, "error building SAML metadata")
			return
		}
		_ = enc.Flush()
		name := p.Issuer
		if name == "" {
			name = "sp"
		}
		res.W.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-meta.xml"`, sanitizeFilename(name)))
		res.BareWrite(200, append([]byte("<?xml version=\"1.0\"?>\n"), metaBuf.Bytes()...))
	}
}

func sanitizeFilename(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == ':' || c == '/' {
			out = append(out, c)
		} else {
			out = append(out, '-')
		}
	}
	return string(out)
}

// nodeSamlInflate — zlib (0x78) inflate for the Node decompress path.
func nodeSamlInflate(b []byte) ([]byte, error) {
	zr, err := newZlibReader(b)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var out []byte
	buf := make([]byte, 32*1024)
	for {
		n, rerr := zr.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if rerr != nil {
			break
		}
	}
	return out, nil
}

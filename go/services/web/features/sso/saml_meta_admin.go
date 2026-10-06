package sso

// A3 (owner 2026-10-06; the fedgap-6d residual): the SAML SP-metadata admin
// page. The GET /saml/meta attachment route already existed; /admin had no
// place to SEE the metadata. IdP administrators need this XML (entityID,
// ACS binding, SP signing certificate) to configure their side, so the
// admin page renders it inline + links the raw download.
//
// Wire: GET /admin/saml/metadata (site-admin gated, like the other
// /admin/sso/* routes) — text/html. An unconfigured instance renders an
// honest "not configured" page with setup guidance (no 503 in the UI; the
// /saml/meta attachment keeps its parity-pinned 503-JSON wire).

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"regexp"
	"strings"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var adminSAMLMetaPattern = regexp.MustCompile(`^/(?i:admin)/saml/metadata$`)

// samlSPMetaXML — the ONE builder for both surfaces (attachment route +
// admin page), so the XML can never drift between them.
//
// Returns (provider, xmlBody, err): err != nil means "no SAML provider
// resolvable on this instance".
func samlSPMetaXML(a *core.App, c *core.Cxt) (*SAMLProvider, string, error) {
	var cfg *SSOConfig
	if db, err := ssoDB(a, c); err == nil {
		cfg = loadSSOConfig(db, c)
	}
	p, ok := resolveSAMLProvider(cfg, "")
	if !ok {
		if p2 := samlFromSiteSettings(a, c.Req.Context()); p2 != nil {
			p, ok = p2, true
		}
	}
	if !ok {
		return nil, "", fmt.Errorf("saml: no provider configured (ssoConfigs or site_settings 'sso-saml')")
	}
	sp := buildSP(p, cfgSPConfig(cfg), c.SiteURL)
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(sp.Metadata()); err != nil {
		return p, "", err
	}
	_ = enc.Flush()
	return p, buf.String(), nil
}

// samlMetaAdminHTML — the configured-page body (pure; unit-testable).
func samlMetaAdminHTML(entityID, xmlBody string) string {
	return `<div class="row-spaced">
<h3>SAML SP metadata — ` + html.EscapeString(entityID) + `</h3>
<p class="text-muted">Hand this document to your IdP administrator (it pins the service-provider entityID, the assertion consumer service binding, and the SP signing certificate). The same XML is downloadable as an attachment at <a href="/saml/meta" rel="nofollow">/saml/meta</a>.</p>
<div class="row-spaced">
<button type="button" class="btn btn-secondary" id="saml-meta-copy">Copy XML</button>
<a class="btn btn-primary" href="/saml/meta" rel="nofollow">Download (attachment)</a>
</div>
<pre id="saml-meta-xml" style="background:#f7f7f9;border:1px solid #ddd;border-radius:4px;padding:12px;overflow:auto;max-height:60vh;">` + html.EscapeString(xmlBody) + `</pre>
<script nonce="__NONCE__">
(function () {
  var btn = document.getElementById('saml-meta-copy');
  var pre = document.getElementById('saml-meta-xml');
  if (!btn || !pre) return;
  btn.addEventListener('click', function () {
    var t = pre.textContent;
    var done = function () { btn.textContent = 'Copied'; setTimeout(function () { btn.textContent = 'Copy XML'; }, 1500); };
    if (navigator.clipboard && navigator.clipboard.writeText) { navigator.clipboard.writeText(t).then(done, function () { window.prompt('Copy XML', t); }); }
    else { window.prompt('Copy XML', t); }
  });
})();
</script>
</div>`
}

// samlMetaUnconfiguredHTML — honest empty state (pure).
const samlMetaUnconfiguredHTML = `<div class="row-spaced">
<h3>SAML SP metadata</h3>
<p class="text-muted">SAML single sign-on is not configured on this instance yet, so there is no SP metadata to show. After you enable SSO &rarr; SAML (site settings), this page renders the full EntityDescriptor XML and a download link.</p>
<p class="small">Check: site settings &rarr; SSO &rarr; SAML (entityID + SSO entry point).</p>
</div>`

// samlMetaAdmin — GET /admin/saml/metadata (site-admin only).
func samlMetaAdmin(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !adminGate(a, cxt, res) {
			return
		}
		body := samlMetaUnconfiguredHTML
		if p, xmlBody, err := samlSPMetaXML(a, cxt); err == nil && xmlBody != "" {
			body = samlMetaAdminHTML(p.Issuer, xmlBody)
		}
		nonce := views.NewNonce()
		body = strings.ReplaceAll(body, "__NONCE__", nonce)
		w := res.W
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", views.ReactorCSP(nonce))
		w.Header().Set("Permissions-Policy", core.PinnedPermissionsPolicy)
		w.Header().Set("ETag", core.EtagWeakBody(body))
		w.WriteHeader(200)
		_, _ = w.Write([]byte(body))
	}
}

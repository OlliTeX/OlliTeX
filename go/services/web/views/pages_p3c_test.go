package views

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Render the P3.3 views with pinned dynamic values and assert every
// renderer-controlled slot splices (the slot set is the contract; the
// generator pins the full document against Node captures).
func TestP3CSettingsSlots(t *testing.T) {
	rec := httptest.NewRecorder()
	d := PageData{
		CSRFToken:  "csrf-token-xyz",
		Nonce:      "nonce",
		Origin:     "http://127.0.0.1:7420/",
		UserEmail:  "e2e-user@e2e.test",
		UserID:     "6aa4b8b573ef0e5094f4cbc0",
		UserMetaJSON: `{"id":"6aa4b8b573ef0e5094f4cbc0","isAdmin":false,"email":"e2e-user@e2e.test","first_name":"Zoe","last_name":"P33","alphaProgram":false,"betaProgram":true,"labsProgram":false,"features":{},"refProviders":{"mendeley":false,"zotero":false,"papers":false}}`,
		SamlBeta:       "true",
		HasPassword:    true,
		ShowAiFeatures: true,
	}
	SettingsPage(rec, d)
	out := rec.Body.String()
	for _, c := range []string{
		`<meta name="ol-csrfToken" content="csrf-token-xyz">`,
		`ol-usersEmail" content="e2e-user@e2e.test"`,
		`ol-user_id" content="6aa4b8b573ef0e5094f4cbc0"`,
		`ol-hasPassword" data-type="boolean" content>`,
		`ol-showAiFeatures" data-type="boolean" content>`,
		// ExposedSettings splice: leading AND trailing commas intact
		`15,&quot;hasSamlBeta&quot;:&quot;true&quot;,&quot;hasAffiliationsFeature&quot;`,
		`ol-samlBeta" content="true">`,
		// ol-user meta (JSON-escaped inside the attribute)
		`first_name&quot;:&quot;Zoe&quot;`,
		`betaProgram&quot;:true`,
	} {
		if !strings.Contains(out, c) {
			t.Errorf("missing slot content: %q", c)
		}
	}
	if len(out) < 10000 {
		t.Errorf("render suspiciously small: %d bytes", len(out))
	}
}

func TestP3CSettingsSlotsEmpty(t *testing.T) {
	// flag-less state: boolean metas ABSENT (pug `content != false && content`
	// convention pinned on the capture set), ExposedSettings key ABSENT.
	rec := httptest.NewRecorder()
	d := PageData{
		CSRFToken: "t", Nonce: "n",
		Origin:     "/",
		UserEmail:  "x@y.test",
		UserID:     "deadbeef0000000000000001",
		UserMetaJSON: `{"id":"deadbeef0000000000000001","isAdmin":false,"email":"x@y.test","first_name":"","last_name":"","alphaProgram":false,"betaProgram":false,"labsProgram":false,"features":{},"refProviders":{"mendeley":false,"zotero":false,"papers":false}}`,
	}
	SettingsPage(rec, d)
	out := rec.Body.String()
	if strings.Contains(out, `ol-hasPassword" data-type="boolean" content`) {
		t.Error("hasPassword content flag must be absent when false (name stays)")
	}
	if strings.Contains(out, `ol-showAiFeatures" data-type="boolean" content`) {
		t.Error("showAiFeatures content flag must be absent when false (name stays)")
	}
	if strings.Contains(out, `ol-samlBeta" content`) {
		t.Error("samlBeta content attr must be absent when empty (name stays)")
	}
	if strings.Contains(out, `hasSamlBeta&quot;`) {
		t.Error("ExposedSettings hasSamlBeta key must be absent when empty")
	}
	// valid JSON around the splice point either way
	if i := strings.Index(out, `ieeeBrandId&quot;:15`); i != -1 {
		after := out[i+len(`ieeeBrandId&quot;:15`):]
		if !strings.HasPrefix(after, `,&quot;hasAffiliationsFeature&quot;`) {
			t.Errorf("expected clean JSON splice, got: %s", after[:120])
		}
	}
}

func TestP3CSessionsSlots(t *testing.T) {
	rec := httptest.NewRecorder()
	d := PageData{
		CSRFToken: "tok", Nonce: "n",
		Origin:             "http://127.0.0.1:7420/",
		UserEmail:          "e2e-user@e2e.test",
		UserID:             "6aa4b8b573ef0e5094f4cbc0",
		SessionsCurrentRow: "<tr><td>1.2.3.4</td><td>14th Sep 2026, 8:56 am UTC</td></tr>",
		SessionsOtherRows:  "<tr><td>9.9.9.9</td><td>1st Jan 2000, 12:00 pm UTC</td></tr>",
	}
	SessionsPage(rec, d)
	out := rec.Body.String()
	for _, c := range []string{
		"<tr><td>1.2.3.4</td><td>14th Sep 2026, 8:56 am UTC</td></tr>",
		"<tr><td>9.9.9.9</td><td>1st Jan 2000, 12:00 pm UTC</td></tr>",
	} {
		if !strings.Contains(out, c) {
			t.Errorf("missing: %q", c)
		}
	}
	// the capture-stale rows must be gone
	if strings.Contains(out, "172.20.0.1") {
		t.Error("capture-stale session rows leaked into render")
	}
}

package instancestats

// D22 (8cbc1526) phase D — webhook pure-surface tests (no Mongo/SMTP needed:
// parse + summary fallbacks + the mail template the handler renders).

import (
	"strings"
	"testing"

	"ollitex/go/services/web/features/emailtemplates"
)

func TestParseWebhookBodyFiringOnly(t *testing.T) {
	payload := `{"version":"4","status":"firing","alerts":[
	  {"status":"firing","labels":{"alertname":"OllitexServiceDown","job":"ollitex-web"},
	   "annotations":{"summary":"ollitex-web is down"}},
	  {"status":"resolved","labels":{"alertname":"OllitexHigh5xxRate"},
	   "annotations":{"summary":"was high"}}
	]}`
	got := parseWebhookBody([]byte(payload))
	if len(got) != 1 {
		t.Fatalf("firing alerts = %d, want 1 (resolved must be filtered)", len(got))
	}
	if got[0].Labels["alertname"] != "OllitexServiceDown" {
		t.Errorf("alertname = %q", got[0].Labels["alertname"])
	}
}

func TestParseWebhookBodyInvalid(t *testing.T) {
	if got := parseWebhookBody([]byte(`not json`)); got != nil {
		t.Errorf("invalid body = %v, want nil", got)
	}
	if got := parseWebhookBody([]byte(`{"alerts":[]}`)); got != nil {
		t.Errorf("empty alerts = %v, want nil", got)
	}
}

func TestAlertSummaryFallbacks(t *testing.T) {
	a := webhookAlert{Labels: map[string]string{"alertname": "X"},
		Annotations: map[string]string{"summary": "s1"}}
	if got := alertSummary(a); got != "s1" {
		t.Errorf("summary = %q", got)
	}
	a.Annotations = map[string]string{"description": "d1"}
	if got := alertSummary(a); got != "d1" {
		t.Errorf("description fallback = %q", got)
	}
	a.Annotations = map[string]string{}
	if got := alertSummary(a); got != "X" {
		t.Errorf("alertname fallback = %q", got)
	}
}

func TestAlertTemplateRender(t *testing.T) {
	tmpl, err := emailtemplates.Render(emailtemplates.Registry, map[string]emailtemplates.Override{},
		"instance-stats-alert", map[string]string{
			"app":       "OlliTeX",
			"alertname": "OllitexServiceDown",
			"summary":   "ollitex-web is down",
		})
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Subject != "[OlliTeX] ALERT: OllitexServiceDown" {
		t.Errorf("subject = %q", tmpl.Subject)
	}
	if !strings.Contains(tmpl.Text, "ollitex-web is down") ||
		!strings.Contains(tmpl.Text, "OllitexServiceDown") {
		t.Errorf("text missing alert fields: %q", tmpl.Text)
	}
	if !strings.Contains(tmpl.HTML, "<strong>OlliTeX observability alert fired.</strong>") {
		t.Errorf("html missing pinned phrase: %q", tmpl.HTML)
	}
}

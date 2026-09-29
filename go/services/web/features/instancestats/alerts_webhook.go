package instancestats

// D22 (8cbc1526) phase D — Prometheus alert webhook.
//
// Prometheus (the d22 sidecar) POSTs its `alerting.webhook_configs` payload
// (alertmanager-webhook v4 shape) to POST /internal/alerts on the Go web.
// The handler mirrors the instance-stats alert configuration (the same
// instanceStatAlertConfigs singleton the /admin API manages) and sends one
// mail per FIRING alert through the same email pipeline as the test button
// (emailtemplates + core.Mail → SMTP).
//
// Deliberately plain: no basic-auth (docker-internal route; the HAProxy
// edge never forwards /internal/*), no CSRF (machine caller), idempotent
// (a re-fired alert re-notifies — intended).

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"time"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/emailtemplates"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// webhookAlert — the per-alert shape of the Prometheus/Alertmanager
// webhook v4 payload (only the fields we render).
type webhookAlert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

type webhookPayload struct {
	Version string         `json:"version"`
	Status  string         `json:"status"`
	Alerts  []webhookAlert `json:"alerts"`
}

// parseWebhookBody — pure; returns only firing alerts (resolved/expired are
// informational: the OlliTeX mail pipeline has no "clear" concept and the
// owner scope is firing alerts only).
func parseWebhookBody(body []byte) []webhookAlert {
	var p webhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil
	}
	var out []webhookAlert
	for _, a := range p.Alerts {
		if a.Status == "firing" {
			out = append(out, a)
		}
	}
	return out
}

// alertSummary — annotations.summary first (the rules carry it), then the
// alertname as a bare fallback.
func alertSummary(a webhookAlert) string {
	if s := a.Annotations["summary"]; s != "" {
		return s
	}
	if s := a.Annotations["description"]; s != "" {
		return s
	}
	return a.Labels["alertname"]
}

// alertWebhook — POST /internal/alerts.
func alertWebhook(a *core.App, mail *core.Mail) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if cxt.Req.Method != "POST" {
			res.SendStatus(405)
			return
		}
		body, err := io.ReadAll(cxt.Req.Body)
		if err != nil {
			res.SendStatus(400)
			return
		}
		alerts := parseWebhookBody(body)
		if len(alerts) == 0 {
			res.JSON(200, []byte(`{"ok":true,"delivered":0,"alerts":0}`))
			return
		}

		// Recipients: the instance-stats alert configuration (same config
		// doc the /admin site-settings API writes). No config → no-op
		// success (the route is additive; an unconfigured site must not
		// 500 the sidecar).
		emails, err := configuredAlertEmails(a)
		if err != nil {
			log.Printf("instancestats: alert webhook: read config: %v", err)
			res.SendStatus(500)
			return
		}
		if len(emails) == 0 {
			res.JSON(200, []byte(`{"ok":true,"delivered":0,"alerts":0,"note":"no alert emails configured"}`))
			return
		}

		delivered := 0
		for _, al := range alerts {
			tmpl, tmErr := emailtemplates.RenderFor(a, "instance-stats-alert", map[string]string{
				"app":       istAppName(),
				"alertname": al.Labels["alertname"],
				"summary":   alertSummary(al),
			})
			if tmErr != nil {
				log.Printf("instancestats: alert webhook: render %q: %v", al.Labels["alertname"], tmErr)
				continue
			}
			okAll := true
			for _, to := range emails {
				if mail != nil && mail.Send(to, tmpl.Subject, tmpl.Text, tmpl.HTML) != nil {
					log.Printf("instancestats: alert webhook: send to %s failed", to)
					okAll = false
					break
				}
			}
			if okAll {
				delivered++
			}
		}
		res.JSON(200, core.JSON(map[string]any{
			"ok":        true,
			"alerts":    len(alerts),
			"delivered": delivered,
		}))
	}
}

// configuredAlertEmails — the instanceStatAlertSingleton alertEmails (same
// read as getAlertConfig; 5s ctx cap so a wedged Mongo cannot hold a
// sidecar POST indefinitely).
func configuredAlertEmails(a *core.App) ([]string, error) {
	if a == nil || a.Mongo == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	doc := map[string]any{}
	if err := db.Collection("instanceStatAlertConfigs").
		FindOne(ctx, bson.D{{Key: "_id", Value: alertConfigID}}).Decode(&doc); err != nil {
		return nil, nil // no config doc → no recipients (Node: config = null)
	}
	return configEmails(doc), nil
}

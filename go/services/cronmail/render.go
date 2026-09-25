// Package cronmail is the Go 1:1 port of the Node email-notification
// dispatcher chain:
//
//	server-ce/cron/notification-email-dispatch.sh
//	  → services/web/scripts/process_notifications.mjs
//	    → modules/notifications/app/src/ProcessNotifications.mjs   (claim loop)
//	      → app/src/Features/Email/EmailHandler.mjs                (send)
//	        → EmailBuilder.mjs (templates) + EmailSender.mjs (SMTP)
//
// Scope: claims due docs from the `emailNotifications` Mongo collection and
// sends the two cron emailTypes that producers in this stack write:
//   - `projectNotification`         (produced by the live Go chat service)
//   - `trackedChangesNotification`  (produced by the legacy Node scheduler)
//
// Rendering parity strategy (same honest-oracle method as the P3 mail pins):
// the static regions of the message live in oraclebase_go.go as VERBATIM
// bytes captured from the real Node EmailBuilder (generated, not
// hand-transcribed). The Go renderer computes only the dynamic slots —
// title, message, ctaURL, gmail JSON block, logo app-name, text footer —
// and splices them into the oracle base via exact-value replacement.
// templates_test.go asserts byte-exact subject/html/text for the full
// fixture matrix (safe / escaping / fallback names, comment vs reply),
// against goldens also produced by the Node oracle.
package cronmail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Settings — the node `settings` values these templates consume
// (services/web/config/settings.defaults.js: appName/siteUrl/env).
type Settings struct {
	AppName string // APP_NAME || 'OlliTeX'
	SiteURL string // PUBLIC_URL || 'http://127.0.0.1:3000'
	Env     string // node settings.env — 'server-ce' here ('saas' gates the logo-image branch)
}

// Env — indirection so tests can swap the resolver (mirrors os.Getenv).
var Env = func(k string) string { return os.Getenv(k) }

// NewSettingsFromEnv mirrors the node env defaults (1:1).
func NewSettingsFromEnv() Settings {
	s := Settings{
		AppName: envOr("APP_NAME", "OlliTeX"),
		SiteURL: envOr("PUBLIC_URL", "http://127.0.0.1:3000"),
		Env:     "server-ce", // node settings.defaults.js `env: 'server-ce'`
	}
	if e := strings.TrimSpace(Env("ENVIRONMENT")); e != "" {
		s.Env = e
	}
	return s
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(Env(k)); v != "" {
		return v
	}
	return def
}

// intEnv — node `Number(process.env.X) || fallback` (non-positive → fallback).
func intEnv(k string, fallback int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(Env(k))); err == nil && v > 0 {
		return v
	}
	return fallback
}

// durEnvMS — same semantics for millisecond envs.
func durEnvMS(k string, fallback time.Duration) time.Duration {
	if v, err := strconv.ParseInt(strings.TrimSpace(Env(k)), 10, 64); err == nil && v > 0 {
		return time.Duration(v) * time.Millisecond
	}
	return fallback
}

// Opts — the string view of the per-notification options bag used for
// render inputs (node `opts.to/projectName/projectId` are all strings here).
type Opts map[string]string

// ---------------------------------------------------------------------------
// Dynamic value functions (ported 1:1 from the Node template helpers).
// ---------------------------------------------------------------------------

// escape — lodash `_.escape`: & < > " ' → &amp; &lt; &gt; &quot; &#39; (& first).
func escape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	).Replace(s)
}

// cleanEntityText — the oracle-pinned effect of
// `sanitizeHtml(text, {allowedTags: []})` (plain mode) — and of the html-mode
// option on the exact domain these templates produce (lodash-escaped strings
// with no tags): `&quot;`/`&#39;` decode to literal quote chars while
// `&amp;`/`&lt;`/`&gt;` survive. Verified against the Node oracle goldens.
func cleanEntityText(s string) string {
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	return s
}

// gmailAction — the node `gmailGoToAction` object.
type gmailAction struct {
	Target      string
	Name        string
	Description string
}

// jsonForScript — node StringHelper.stringifyJsonForScript: compact JSON with
// JS insertion key order, then & > < \u2028 \u2029 escaped for <script> safety.
func jsonForScript(g gmailAction) string {
	type pa struct {
		Type   string `json:"@type"`
		Target string `json:"target"`
		URL    string `json:"url"`
		Name   string `json:"name"`
	}
	type msg struct {
		Context         string `json:"@context"`
		Type            string `json:"@type"`
		PotentialAction pa     `json:"potentialAction"`
		Description     string `json:"description"`
	}
	b, err := marshalNoHTMLEsc(&msg{
		Context:         "http://schema.org",
		Type:            "EmailMessage",
		PotentialAction: pa{Type: "ViewAction", Target: g.Target, URL: g.Target, Name: g.Name},
		Description:     g.Description,
	})
	if err != nil {
		panic(err) // impossible for this fixed shape
	}
	return strings.NewReplacer(
		"&", `\u0026`,
		">", `\u003e`,
		"<", `\u003c`,
		"\u2028", `\u2028`,
		"\u2029", `\u2029`,
	).Replace(b)
}

func marshalNoHTMLEsc(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil // Encoder appends \n; JS doesn't
}

// ---------------------------------------------------------------------------
// Renderers (slot-spliced into the oracle base bytes).
// ---------------------------------------------------------------------------

type rendered struct {
	Subject string
	HTML    string
	Text    string
}

// oracleEnv — the env the oracle base was captured under (see
// oraclebase_go.go header / testdata golden); the renderer replaces the
// values derived from it when the runtime settings differ.
const (
	oracleApp    = "OlliTeX"
	oracleSite   = "http://127.0.0.1:4000"
	oraclePNID   = "abc123def456abc123def456"
	oraclePNName = "My Great Project"
	oraclePNMsg  = "A new comment has been posted in My Great Project."
	oracleTCID   = "p4"
	oracleTCName = "Tracking Project"
	oracleTCMsg  = "New tracked changes are pending your review in Tracking Project."
)

// render — the full message for one dispatch doc. `isComment` only applies
// to projectNotification (the trackedChanges template ignores it).
func render(s Settings, emailType, projectName, projectId string, isComment bool) (*rendered, error) {
	es := escape(projectName)
	url := strings.TrimRight(s.SiteURL, "/") + "/project/" + projectId

	switch emailType {
	case "projectNotification":
		event := "reply"
		if isComment {
			event = "comment"
		}
		title := "New Reply"
		msg := "A new reply has been posted in " + cleanEntityText(es) + "."
		if isComment {
			title = "New Comment"
			msg = "A new comment has been posted in " + cleanEntityText(es) + "."
		}
		gm := gmailAction{
			Target:      url,
			Name:        "View comment",
			Description: "View the recent " + event + " on " + es,
		}
		h, t := pnHTML(s, title, msg, url, gm), pnText(s, msg, url)
		return &rendered{
			Subject: "New project " + event + " on " + es + " — " + s.AppName,
			HTML:    h,
			Text:    t,
		}, nil
	case "trackedChangesNotification":
		msg := "New tracked changes are pending your review in " + cleanEntityText(es) + "."
		gm := gmailAction{
			Target:      url,
			Name:        "Review changes",
			Description: "Review tracked changes in " + es,
		}
		h, t := tcHTML(s, msg, url, gm), tcText(s, msg, url)
		return &rendered{
			Subject: "New tracked changes in " + es + " — " + s.AppName,
			HTML:    h,
			Text:    t,
		}, nil
	}
	return nil, fmt.Errorf("unsupported emailType %q (this stack's producers only write the two pinned types)", emailType)
}

// splice — replace the oracle-derived old value with the runtime value.
// (When runtime env == oracle env the replacement is a byte-identity no-op.)
func splice(base, old, new string) string {
	if old == new {
		return base
	}
	return strings.ReplaceAll(base, old, new)
}

// buildPN parts — projectNotification: PN oracle base + dynamic slots.
// Replacement ORDER is pinned by the golden tests:
//  1. title (before any value containing 'New Comment' can be spliced in)
//  2. gmail JSON block (contains old url + old name)
//  3. cta url (3 occurrences: two hrefs + display link)
//  4. message paragraph
//  5. logo app-name (raw, as node's lodash raw interpolation renders it)
func pnHTML(s Settings, title, msg, url string, gm gmailAction) string {
	oldJSON := `{"@context":"http://schema.org","@type":"EmailMessage","potentialAction":{"@type":"ViewAction","target":"` + oracleSite + `/project/` + oraclePNID + `","url":"` + oracleSite + `/project/` + oraclePNID + `","name":"View comment"},"description":"View the recent comment on ` + oraclePNName + `"}`
	oldURL := oracleSite + "/project/" + oraclePNID
	h := baseHTMLPN
	h = splice(h, "New Comment", title) // reply → 'New Reply'; comment → byte-identity
	h = splice(h, oldJSON, jsonForScript(gm))
	h = splice(h, oldURL, url)
	h = splice(h, oraclePNMsg, msg)
	h = splice(h, oracleApp, s.AppName) // logo slot
	return h
}

func pnText(s Settings, msg, url string) string {
	t := baseTextPN
	t = splice(t, "View comment: "+oracleSite+"/project/"+oraclePNID, "View comment: "+url)
	t = splice(t, oraclePNMsg, msg)
	t = splice(t, "The "+oracleApp+" Team - "+oracleSite, "The "+s.AppName+" Team - "+s.SiteURL)
	return t
}

// buildTC parts — trackedChangesNotification: TC oracle base + dynamic slots.
func tcHTML(s Settings, msg, url string, gm gmailAction) string {
	oldJSON := `{"@context":"http://schema.org","@type":"EmailMessage","potentialAction":{"@type":"ViewAction","target":"` + oracleSite + `/project/` + oracleTCID + `","url":"` + oracleSite + `/project/` + oracleTCID + `","name":"Review changes"},"description":"Review tracked changes in ` + oracleTCName + `"}`
	oldURL := oracleSite + "/project/" + oracleTCID
	h := baseHTMLTC
	h = splice(h, oldJSON, jsonForScript(gm))
	h = splice(h, oldURL, url)
	h = splice(h, oracleTCMsg, msg)
	h = splice(h, oracleApp, s.AppName) // logo slot
	return h
}

func tcText(s Settings, msg, url string) string {
	t := baseTextTC
	t = splice(t, "Review changes: "+oracleSite+"/project/"+oracleTCID, "Review changes: "+url)
	t = splice(t, oracleTCMsg, msg)
	t = splice(t, "The "+oracleApp+" Team - "+oracleSite, "The "+s.AppName+" Team - "+s.SiteURL)
	return t
}

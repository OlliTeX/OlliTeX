// export_wizard.go — A-side federated export wizard (content-bridge 2b,
// plan 09 §4.1, oracle invite/FederatedExportController.mjs).
//
// The wizard is the A-side thin authenticated proxy (LOCKED decision Q1):
// the user picks the peer B (approved outbound origin) + the B-side
// project id + an optional TTL; A re-signs a client assertion for B via
// its S2SCall. B is the authority (09 §2.1): owner B-native + a LIVE
// consent grant to home A's client. A persists NOTHING (re-run = fresh
// S2S; B's ledger upsert is idempotent).
//
// ROUTES (09 §4.1 — the existing pinned patterns in userfed.go):
//
//	GET  /federation/export — the wizard form (approved outbound peers,
//	  B project id, optional TTL)
//	POST /federation/export — ① local validation (400/403 form
//	  re-render, no wire) ② S2S `export-project` (PeerRefusal → 502 +
//	  denied audit; business ok:false → 403 + denied audit) ③ success →
//	  `federation_export_requested` audit (meta { origin, scope, gitUrl,
//	  expiresAt } — the PAT is NEVER in the audit, 04 §8) + the result
//	  view (the PAT rendered once into the HTML).
//
// PAT handling (plan 09 §2 risk note + LOCKED decision Q2): the minted
// token is rendered into the single result view only. It is NEVER
// written to the audit log and only appears in A-side logs redacted.
package federation

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"ollitex/go/services/web/core"
)

// defaultExportTTLSeconds — the wizard's default (one transfer session).
const defaultExportTTLSeconds = 3600

// exportDeps — the wizard's injectable surface (unit scope drives the
// full flow with a fake Call; production wires the app's store + audit).
type exportDeps struct {
	Store  Store
	Caller *S2SCall
	MaxTTL int
	AuditW AuditWriter
	Now    func() time.Time
}

// peersForWizard — approved outbound|both peers (Node
// `_approvedOutboundPeers`: direction in [outbound, both], sorted by
// origin; display name = displayName || origin).
func peersForWizard(store Store) []wizardPeer {
	if store == nil {
		return nil
	}
	peers, err := store.ApprovedPeers(context.Background(), PeerDirectionOutbound)
	if err != nil {
		return nil
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Origin < peers[j].Origin })
	out := make([]wizardPeer, 0, len(peers))
	for _, p := range peers {
		if p.Direction != "outbound" && p.Direction != "both" {
			continue
		}
		name := p.DisplayName
		if name == "" {
			name = p.Origin
		}
		out = append(out, wizardPeer{origin: p.Origin, displayName: name})
	}
	return out
}

type wizardPeer struct {
	origin      string
	displayName string
}

func (d *exportDeps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// ttlClamp — Node `_ttlSeconds`: non-finite/<=0 → default 1 h; else
// clamp [1, maxTTL] (maxTTL unset → 86400).
func (d *exportDeps) ttlClamp(rawSeconds int) int {
	maxTTL := d.MaxTTL
	if maxTTL <= 0 {
		maxTTL = 86400
	}
	if rawSeconds <= 0 {
		return defaultExportTTLSeconds
	}
	if rawSeconds < 1 {
		return 1
	}
	if rawSeconds > maxTTL {
		return maxTTL
	}
	return rawSeconds
}

// exportAudit — 04 §8 A-side audit (redacted — `reason` is the B-side
// code string, an allow-list field; the PAT never appears).
func (d *exportDeps) exportAudit(op, origin, reason, gitUrl string, expiresAtSec int64) {
	if d.AuditW == nil {
		return
	}
	info := map[string]any{"origin": origin, "scope": exportScope}
	if reason != "" {
		info["reason"] = reason
	}
	if gitUrl != "" {
		info["gitUrl"] = gitUrl
	}
	if expiresAtSec > 0 {
		info["expiresAt"] = expiresAtSec
	}
	_ = d.AuditW.WriteAudit(context.Background(), op, nil, info, "", "")
}

// findPeer — the wizard's local gate (peer origin in the approved
// outbound|both set).
func (d *exportDeps) findPeer(origin string) (wizardPeer, bool) {
	for _, p := range peersForWizard(d.Store) {
		if p.origin == origin {
			return p, true
		}
	}
	return wizardPeer{}, false
}

// formHTML — the wizard form (Node pug federation-export rendered the
// same fields: peer <select>, B project id <input>, optional TTL <input>,
// csrf-exempt POST — the Go router mounts it NoCSRF per the pinned
// userfed route).
func (d *exportDeps) formHTML(errorMsg string, peers []wizardPeer, form map[string]string) string {
	var b strings.Builder
	b.WriteString(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="federation" content="export">
<title>Federation export</title>
</head>
<body class="federation-export-view">
<main class="federation-export">
<form class="export-form" action="/federation/export" method="POST">
`)
	if len(peers) > 0 {
		b.WriteString(`<label for="origin">Instance</label>
<select name="origin" id="origin" required>
`)
		if form["origin"] != "" {
			b.WriteString(`<option value="" disabled>Choose an instance…</option>\n`)
		} else {
			b.WriteString(`<option value="" disabled selected>Choose an instance…</option>\n`)
		}
		for _, p := range peers {
			sel := ""
			if p.origin == form["origin"] {
				sel = " selected"
			}
			b.WriteString(`<option value="` + html.EscapeString(p.origin) + `" data-name="` + html.EscapeString(p.displayName) + `"` + sel + `>` + html.EscapeString(p.displayName) + `</option>
`)
		}
		b.WriteString(`</select>
`)
	}
	if errorMsg != "" {
		b.WriteString(`<div class="export-error" role="alert">` + html.EscapeString(errorMsg) + `</div>
`)
	}
	b.WriteString(`<label for="projectId">B-side project id</label>
<input name="projectId" id="projectId" type="text" maxlength="128" required value="` + html.EscapeString(form["projectId"]) + `">
<label for="expiresAt">Optional TTL (seconds)</label>
<input name="expiresAt" id="expiresAt" type="number" min="1" value="` + html.EscapeString(form["expiresAt"]) + `">
<button type="submit">Export</button>
</form>
</main>
</body>
</html>
`)
	return b.String()
}

// resultHTML — the single result view (Q2): the PAT is rendered once,
// here, never anywhere else.
func (d *exportDeps) resultHTML(gitUrl, pat string, expiresAtSec int64, peers []wizardPeer) string {
	exp := d.now().UTC().Add(time.Duration(expiresAtSec-d.now().Unix()) * time.Second).UTC().Format(time.RFC3339)
	var b strings.Builder
	b.WriteString(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="federation" content="export">
<title>Federation export — result</title>
</head>
<body class="federation-export-result">
<main class="federation-export">
<h1>Export ready</h1>
<ol class="export-instructions">
<li>On your workstation: <code>git clone https://&lt;PAT&gt;@` + html.EscapeString(strings.TrimPrefix(gitUrl, "https://")) + `</code></li>
<li>Create a NEW project on this instance and push the cloned history into it (bidirectional git).</li>
</ol>
<dl>
<dt>git url</dt><dd><code>` + html.EscapeString(gitUrl) + `</code></dd>
<dt>token (short-lived, read-only)</dt><dd><code class="export-pat">` + html.EscapeString(pat) + `</code></dd>
<dt>expires</dt><dd>` + html.EscapeString(exp) + `</dd>
</dl>
<p><a href="/federation/export">Export another</a></p>
</main>
</body>
</html>
`)
	_ = peers
	return b.String()
}

// exportFormHandler — GET /federation/export.
func exportFormHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		d := prodExportDeps(a, cxt)
		res.HTML(http.StatusOK, d.formHTML("", peersForWizard(d.Store), map[string]string{}))
	}
}

// exportPostHandler — POST /federation/export (the Node
// `handleExport` ordering: validate → S2S → refusal views / result view,
// with the 04 §8 audit legs).
func exportPostHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		d := prodExportDeps(a, cxt)
		peers := peersForWizard(d.Store)

		origin := formString(cxt, "origin")
		projectId := formString(cxt, "projectId")
		rawTTL := formString(cxt, "expiresAt")

		form := map[string]string{
			"origin":    origin,
			"projectId": projectId,
			"expiresAt": rawTTL,
		}
		// ① local validation (form re-render, no wire).
		if _, ok := d.findPeer(origin); !ok {
			d.exportAudit(AuditTypes.ExportDenied, origin, "peer-not-approved", "", 0)
			res.HTML(http.StatusForbidden, d.formHTML("peer not approved for outbound calls", peers, form))
			return
		}
		if projectId == "" || len(projectId) > 128 {
			res.HTML(http.StatusBadRequest, d.formHTML("B-side project id is required", peers, form))
			return
		}
		rawInt, _ := strconv.Atoi(strings.TrimSpace(rawTTL))
		ttl := d.ttlClamp(rawInt)
		expiresAt := d.now().Unix() + int64(ttl)

		// ② S2S wire (03 §2) — the 2a B-side action.
		envelope, err := d.Caller.Call(context.Background(), origin, "export-project", map[string]any{
			"projectId": projectId,
			"expiresAt": float64(expiresAt),
		})
		if err != nil {
			p, isRefusal := AsPeerRefusal(err)
			reason := "peer-unreachable"
			if isRefusal && p.Code != "" {
				reason = p.Code
			}
			d.exportAudit(AuditTypes.ExportDenied, origin, reason, "", 0)
			res.HTML(http.StatusBadGateway, d.formHTML(fmt.Sprintf("export refused by home instance (%s)", reason), peers, form))
			return
		}
		okVal, _ := envelope["ok"].(bool)
		if !okVal {
			reason, _ := envelope["code"].(string)
			if reason == "" {
				reason = "export-denied"
			}
			d.exportAudit(AuditTypes.ExportDenied, origin, reason, "", 0)
			res.HTML(http.StatusForbidden, d.formHTML("export refused: "+reason, peers, form))
			return
		}
		payload, _ := envelope["payload"].(map[string]any)
		if payload == nil {
			d.exportAudit(AuditTypes.ExportDenied, origin, "export-denied", "", 0)
			res.HTML(http.StatusForbidden, d.formHTML("export refused: export-denied", peers, form))
			return
		}
		gitUrl, _ := payload["git_url"].(string)
		pat, _ := payload["pat"].(string)
		expSec := expiresAt
		if v, ok := payload["expires_at"].(float64); ok {
			expSec = int64(v)
		}

		// ③ success — audit { gitUrl, expiresAt } (NOT the PAT, 04 §8) +
		// the result view (the PAT lives in this HTML only).
		d.exportAudit(AuditTypes.ExportRequested, origin, "", gitUrl, expSec)
		res.HTML(http.StatusOK, d.resultHTML(gitUrl, pat, expSec, peers))
	}
}

// formString — the wizard reads the (NoCSRF) form body fields; the core
// app already parses form values into PostForm (express req.body shape).
func formString(cxt *core.Cxt, field string) string {
	if cxt == nil || cxt.Req == nil || cxt.Req.PostForm == nil {
		return ""
	}
	return cxt.Req.PostForm.Get(field)
}

// prodExportDeps — production wiring (the app's store, audit + key
// provider keystore for the assertion).
func prodExportDeps(a *core.App, cxt *core.Cxt) *exportDeps {
	d := &exportDeps{
		Caller: &S2SCall{Store: fedStore(a, cxt), Site: cxt.SiteURL, Now: time.Now},
		Now:    time.Now,
	}
	if db := dbHandle(a); db != nil {
		d.Store = fedStore(a, cxt)
		d.AuditW = mongoAuditWriter{db: db}
	}
	d.MaxTTL = loadSettings().ExportMaxTTLSeconds
	return d
}

package instancestats

import (
	"context"
	"time"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/sitesettings"
)

// grafanaEmbedHandler — GET /admin/instance-stats/api/grafana.
//
// The D22 kiosk opt-in surface for the hub stats pane: resolves the
// Grafana base URL (env HUB_GRAFANA_EMBED_URL ⇒ site_settings
// `monitoring.grafanaEmbedURL` ⇒ off) and returns the two shipped
// dashboards with their kiosk (embed) + full URLs. Site-admin only
// (a1z — the pane is an admin section and the URL itself is a
// credential-adjacent internal address).
//
// Dashboards (server-ce d22 + toolkit monitoring, same UIDs):
//
//	ollitex-overview            — OlliTeX — overview
//	ollitex-toolkit-mongo-redis — MongoDB + Redis overview
//
// embed URL form: {base}/d/{uid} (Grafana 11+ removed the Kiosk app) (Grafana "Kiosk" embed mode —
// read-only dashboard, no chrome; requires GF_AUTH_ANONYMOUS_* +
// GF_SECURITY_CSP_FRAME_ANCESTORS on the Grafana side for cross-origin
// framing — the toolkit monitoring overlay renders both from the
// store, off by default).
// grafanaDashboards — the two shipped dashboards + kiosk/full URL forms
// (pure — unit-pinned). kiosk URL form: {base}/kiosk-d/{uid}.
func grafanaDashboards(base string) []struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kiosk string `json:"kiosk"`
	Full  string `json:"full"`
} {
	// Contract (hub frontend instance-stats-section.tsx, owner break 2026-10-06):
	// dashboards is ALWAYS a JSON array — an empty [] when the base is unset,
	// never null — because the browser does dashboards.find(...) unguarded.
	out := []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Kiosk string `json:"kiosk"`
		Full  string `json:"full"`
	}{}
	if base == "" {
		return out
	}
	for _, d := range []struct{ id, title string }{
		{"ollitex-overview", "OlliTeX — overview"},
		{"ollitex-toolkit-mongo-redis", "MongoDB + Redis overview"},
	} {
		// AJ-3 (owner 2026-10-08): Grafana 11.x REMOVED the Kiosk app
		// (kiosk-d → 404, verified live on grafana-oss 11.6). The modern
		// embed = the dashboard URL itself with anonymous Viewer auth
		// (GF_AUTH_ANONYMOUS_ENABLED) + CSP frame-ancestors, so "kiosk"
		// and "full" collapse onto the same /d/{id} form.
		out = append(out, struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Kiosk string `json:"kiosk"`
			Full  string `json:"full"`
		}{d.id, d.title, base + "/d/" + d.id, base + "/d/" + d.id})
	}
	return out
}

func grafanaEmbedHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
		defer cancel()
		base := sitesettings.GrafanaEmbed(a, ctx)

		out := struct {
			Enabled    bool `json:"enabled"`
			Dashboards []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
				Kiosk string `json:"kiosk"`
				Full  string `json:"full"`
			} `json:"dashboards"`
		}{Enabled: base != ""}
		out.Dashboards = grafanaDashboards(base)
		res.JSON(200, core.JSON(out))
	}
}

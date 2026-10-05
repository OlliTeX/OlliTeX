package sitesettings

import (
	"context"
	"os"
	"strings"

	"ollitex/go/services/web/core"
)

// GrafanaEmbed — the D22 kiosk opt-in (server-ce/grafana/README.md:
// "Embedding the dashboard as an iframe surface on /hub changes a product
// surface … flagged to the owner instead of building it. The compose
// service, provisioning and dashboard are the default; the kiosk remains
// an explicit opt-in.").
//
// Resolves the Grafana base URL the hub stats pane (Site settings →
// General → Instance statistics → "Live dashboards") iframes in kiosk
// mode. Source chain (the GHSyncEnabled pattern — env conclusive first,
// then the stored section):
//
//  1. env HUB_GRAFANA_EMBED_URL (set by the toolkit env plane from the
//     configstore key of the same name — the single-source render path),
//  2. site_settings section `monitoring` key `grafanaEmbedURL`
//     (admin-managed at runtime),
//
// otherwise "" — the pane stays hidden (off by default, per the owned
// d22 decision). Returned base is normalized: absolute http(s), trimmed
// of trailing slashes.
func GrafanaEmbed(a *core.App, ctx context.Context) string {
	if v := cleanEmbedURL(os.Getenv("HUB_GRAFANA_EMBED_URL")); v != "" {
		return v
	}
	if a == nil || a.Mongo == nil {
		return ""
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return ""
	}
	sections := loadAllSections(ctx, db)
	sec, ok := sections["monitoring"]
	if !ok {
		return ""
	}
	v, jok := ObjGet(sec, "grafanaEmbedURL")
	if !jok {
		return ""
	}
	s, _ := v.(string)
	return cleanEmbedURL(s)
}

// cleanEmbedURL — only an absolute http(s) URL counts (a relative path or
// a bare host would iframe the app origin — never intended); disabled
// markers and empties ⇒ "".
func cleanEmbedURL(raw string) string {
	v := strings.TrimSpace(raw)
	switch strings.ToLower(v) {
	case "", "0", "false", "no", "off", "disabled":
		return ""
	}
	if !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
		return ""
	}
	return strings.TrimRight(v, "/")
}

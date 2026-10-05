package instancestats

import (
	"encoding/json"
	"testing"

	"ollitex/go/services/web/features/sitesettings"
)

func TestGrafanaEmbedDashboardURLs(t *testing.T) {
	// off by default (the d22 kiosk is an explicit owner opt-in) — and the
	// CONTRACT is "always an array": no doc ⇒ an EMPTY slice (JSON [] …)
	// never nil (JSON null), because the hub browser does
	// dashboards.find(...) unguarded (owner break 2026-10-06: "Cannot
	// read properties of null (reading 'find')").
	if ds := grafanaDashboards(""); ds == nil || len(ds) != 0 {
		t.Fatalf("empty base must yield an empty (non-nil) dashboard list, got %#v", ds)
	}
	dsJSON, _ := json.Marshal(json.RawMessage("nil"))
	_ = dsJSON
	ds := grafanaDashboards("http://grafana.example.org:3180")
	if len(ds) != 2 {
		t.Fatalf("want 2 dashboards, got %d", len(ds))
	}
	if ds[0].ID != "ollitex-overview" || ds[1].ID != "ollitex-toolkit-mongo-redis" {
		t.Fatalf("dashboard uids drifted: %v / %v", ds[0].ID, ds[1].ID)
	}
	if ds[0].Kiosk != "http://grafana.example.org:3180/kiosk-d/ollitex-overview" {
		t.Fatalf("kiosk form wrong: %s", ds[0].Kiosk)
	}
	if ds[0].Full != "http://grafana.example.org:3180/d/ollitex-overview" {
		t.Fatalf("full form wrong: %s", ds[0].Full)
	}
	if ds[1].Kiosk != "http://grafana.example.org:3180/kiosk-d/ollitex-toolkit-mongo-redis" {
		t.Fatalf("mongo/redis kiosk wrong: %s", ds[1].Kiosk)
	}
}

func TestGrafanaEmbedBaseCleaning(t *testing.T) {
	// env path (a == nil ⇒ only env can be conclusive here)
	cases := map[string]string{
		"":                                     "",
		"0":                                    "",
		"false":                                "",
		"off":                                  "",
		"disabled":                             "",
		"grafana.example.org:3180":             "", // relative/bare host must not iframe the app origin
		"http://grafana.example.org:3180/":     "http://grafana.example.org:3180",
		"  https://ollitex.example.org:3180  ": "https://ollitex.example.org:3180",
	}
	for in, want := range cases {
		t.Setenv("HUB_GRAFANA_EMBED_URL", in)
		if got := sitesettings.GrafanaEmbed(nil, t.Context()); got != want {
			t.Fatalf("GrafanaEmbed(env=%q) = %q, want %q", in, got, want)
		}
	}
}

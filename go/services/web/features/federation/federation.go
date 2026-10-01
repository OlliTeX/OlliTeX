// Package federation — OIDF 1.0 pairwise identity federation (1:1 port of
// Node `services/web/modules/federation`).
//
// A user on instance A is invited to a project that lives on instance B. A
// acts as OIDC **relying party** (rp), B acts as OIDC **provider** (oidc).
// Instances exchange authenticated S2S calls (s2s) under OIDF client
// assertions (oidf). The unit of trust is the user identity
// (origin, localName); the unit of data is the project invite + mirror
// account.
//
// Scope (v1): identity federation only (no content/stream).
//
// Gating: every mount guards on the `FEDERATION_ENABLED` env (default off);
// when off, the S2S endpoint still answers (the 200 `federation-off`
// envelope, Node router-always-mounted per 03 §8).
package federation

import (
	"ollitex/go/services/web/core"
)

// Feature returns the federation core.Feature. Mounted in
// `cmd/web/main.go` via `webApp.Features = []core.Feature{ ...
// federation.Feature(webApp) }`. Routes are appended by the per-slice
// files; S0 scaffold returns the skeleton (no routes until S2 lands).
func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name:   "federation",
		Routes: sRoutes(a),
	}
}

// sRoutes assembles every route from the per-slice builders. Each builder
// returns nil Routes when its slice is not yet implemented, keeping the
// Feature constructible from S0 onward.
func sRoutes(a *core.App) []core.Route {
	var routes []core.Route
	routes = append(routes, s4Routes(a)...)
	routes = append(routes, s5Routes(a)...)
	routes = append(routes, s2Routes(a)...)
	routes = append(routes, s10Routes(a)...)
	routes = append(routes, s11Routes(a)...)
	routes = append(routes, s12Routes(a)...)
	return routes
}

// Stub route builders for slices that have not landed yet. Each returns
// nil (no routes) until the slice is implemented; the Feature assembles
// them so the package builds from S0 onward.
//
//	s2  → B-side OIDC provider (oidcprovider.go)
//	s5  → S2S router (s2s.go)
//	s10 → RP A-side (rp.go)
//	s11 → invite + export wizard (invite.go / export_wizard.go)
//	s12 → admin surface (admin.go / wizard.go)
package federation

import (
	"ollitex/go/services/web/core"
)

func s2Routes(a *core.App) []core.Route  { return nil } // S2
func s5Routes(a *core.App) []core.Route  { return nil } // S5
func s10Routes(a *core.App) []core.Route { return nil } // S10
func s11Routes(a *core.App) []core.Route { return nil } // S11
func s12Routes(a *core.App) []core.Route { return nil } // S12

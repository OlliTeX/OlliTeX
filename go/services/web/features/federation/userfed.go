package federation

// userfed.go — A-side user-facing federation routes (overleaf-fed
// invite/FederatedInviteRouter + invite/FederatedExportRouter +
// rp/CallbackRouter parity).
//
// Node pins:
//   GET  /api/federation/invite/preview   (soft preview, 03 §4.2)
//   POST /api/federation/invite/authorize (01 §5 steps 1–3: authorize +
//        OIDC grant initiation)
//   GET  /federation/export               (wizard form: approved
//        outbound peers + B project id + TTL)
//   POST /federation/export               (S2S export to B; PAT view,
//        2b Q2)
//   GET  /federation/oidc/rp/callback     (A-side grant callback:
//        code exchange, state verify, mirror session, 302)
//
// All require a logged-in A-side user (except the RP callback — the
// mirror is anonymous to the A session; the callback carries the
// HMAC state + PKCE). S10 landed: the authorize starter + RP callback
// are live (rp_state.go / rp_exchange.go / rp_mint.go / rp_routes.go);
// the deep B-side consent mint stays the OP engine (S4b) and the export
// transfer the fedgap-5 slice (2b) — those routes keep the honest
// pending envelopes.

import (
	"regexp"

	"ollitex/go/services/web/core"
)

var (
	patInvitePreview   = regexp.MustCompile(`^/api/federation/invite/preview$`)
	patInviteAuthorize = regexp.MustCompile(`^/api/federation/invite/authorize$`)
	patExportGet       = regexp.MustCompile(`^/federation/export$`)
	patExportPost      = regexp.MustCompile(`^/federation/export$`)
	patRpCallback      = regexp.MustCompile(`^/federation/oidc/rp/callback$`)
)

func sUserFedRoutes(a *core.App) []core.Route {
	return []core.Route{
		{Method: "GET", Pattern: patInvitePreview, Handler: s2sPending(a, "preview-pending")},
		// S10 (S9 starter leg): the wizard entry now mints the signed
		// state + PKCE and 302s to B's authorization endpoint (rp_routes.go).
		{Method: "POST", Pattern: patInviteAuthorize, NoCSRF: true, Handler: rpAuthorizeStarter(a)},
		{Method: "GET", Pattern: patExportGet, Handler: s2sPending(a, "export-form-pending")},
		{Method: "POST", Pattern: patExportPost, NoCSRF: true, Handler: s2sPending(a, "export-pending")},
		// S10: the A-side grant callback — state verify + code exchange
		// (PKCE) + mirror + grant + 302 (rp_routes.go).
		{Method: "GET", Pattern: patRpCallback, NoLogin: true, NoCSRF: true, Handler: rpCallback(a)},
	}
}

// s2sPending — the honest "envelope pinned, dispatch pending" answer
// (feature-off → federation-off; feature-on → the pending code).
func s2sPending(a *core.App, code string) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !loadSettings().Enabled {
			res.JSON(200, []byte(`{"ok":false,"code":"federation-off","detail":"federation disabled"}`))
			return
		}
		res.JSON(200, []byte(`{"ok":false,"code":"`+code+`","detail":"S9–S11 slice"}`))
	}
}

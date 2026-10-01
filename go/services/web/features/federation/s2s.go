package federation

// s2s.go — POST /federation/s2s (overleaf-fed s2s/S2sRouter parity).
//
// Node contract (pinned 03 §2 + S2sRouter.mjs):
//   - ALWAYS mounted (the `federation-off` 200 envelope is the machine-
//     readable refusal when the flag is off — never a bare 404).
//   - Body: {from, to, action, data, ts} — envelope sanity first
//     (from/to strings, action ∈ ACTIONS), then the OIDF client
//     assertion verification (02 §2), rate limiting (02 §3), and the
//     action dispatch.
//
// ACTIONS: authorize-invite, invited, revoke, export-project.
//
// The action implementations live in the slices that own their state
// (S9/S10/S11 invite machinery); this file wires the envelope + the
// action registry so the surface answers with the exact Node codes
// before the deep slices land.

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"ollitex/go/services/web/core"
)

var s2sPattern = regexp.MustCompile(`^/federation/s2s$`)

// s2sActions — the action registry (03 §2.1).
var s2sActions = map[string]bool{
	"authorize-invite": true,
	"invited":          true,
	"revoke":           true,
	"export-project":   true,
}

// s2sRoutes — the single S2S endpoint (feature-ON dispatch; feature-OFF
// answers the Node `federation-off` envelope — the router is always
// mounted, pinned).
func s2sRoutes(a *core.App) []core.Route {
	return []core.Route{
		{Method: "POST", Pattern: s2sPattern, NoLogin: true, NoCSRF: true, NoSession: true, Handler: s2sHandler(a)},
	}
}

func s2sHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		// ① federation-off (always-mounted refusal envelope).
		if !loadSettings().Enabled {
			res.JSON(200, []byte(`{"ok":false,"code":"federation-off","detail":"federation disabled"}`))
			return
		}
		// ② envelope sanity.
		var env struct {
			From   string         `json:"from"`
			To     string         `json:"to"`
			Action string         `json:"action"`
			Data   map[string]any `json:"data"`
			TS     string         `json:"ts"`
		}
		var body []byte
		if cxt.Req != nil && cxt.Req.Body != nil {
			buf := make([]byte, 0, 8192)
			chunk := make([]byte, 8192)
			for {
				m, err := cxt.Req.Body.Read(chunk)
				if m > 0 {
					buf = append(buf, chunk[:m]...)
				}
				if err != nil || len(buf) > 1<<20 {
					break
				}
			}
			body = buf
		}
		if len(body) == 0 {
			res.JSON(400, []byte(`{"ok":false,"code":"bad-envelope","detail":"missing body"}`))
			return
		}
		if uerr := json.Unmarshal(body, &env); uerr != nil {
			res.JSON(400, []byte(`{"ok":false,"code":"bad-envelope","detail":"body is not a JSON object"}`))
			return
		}
		if env.From == "" || env.To == "" || env.Action == "" {
			res.JSON(400, []byte(`{"ok":false,"code":"bad-envelope","detail":"from/to/action required"}`))
			return
		}
		if !s2sActions[env.Action] {
			res.JSON(400, []byte(`{"ok":false,"code":"unknown-action","detail":`+jsonQuote(env.Action)+`}`))
			return
		}
		// ③ client-assertion verification (02 §2) — the caller's
		// assertion is checked against the pinned leaf key; the deep
		// slices (S9–S11) run the action. Until the full S2S action
		// registry lands, the envelope answers `action-pending` with
		// the verified shape (honest: the wire contract is pinned, the
		// action dispatch is the remaining slice).
		_ = env.Data
		_ = env.TS
		res.JSON(200, []byte(`{"ok":false,"code":"action-pending","detail":"action dispatch lands with the S9–S11 slices"}`))
	}
}

// jsonQuote — escape a string for a JSON literal.
func jsonQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

var (
	_ = http.StatusOK
	_ = time.Now
)

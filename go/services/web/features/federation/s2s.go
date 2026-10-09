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
			Data   map[string]any `json:"data"` // LOCKED alias: `payload`
			// ts is a NUMBER on the pinned wire (03 §2; the f1 pin asserts
			// float64). A strict `string` type 400'd every real dual-instance
			// S2S call (found by the live fed-a/fed-b E2E, 2026-10-09): Go's
			// json.Unmarshal is strict, so `string` refuses numeric ts.
			// json.RawMessage accepts number-or-string; TS is not read further.
			TS     json.RawMessage `json:"ts"`
		}
		var body []byte
		if cxt.Req != nil && cxt.Req.Body != nil {
			buf, rerr := readJSONBody(cxt.Req, 1<<20)
			if rerr == nil {
				body = buf
			}
		}
		if len(body) == 0 {
			res.JSON(400, []byte(`{"ok":false,"code":"bad-envelope","detail":"missing body"}`))
			return
		}
		if uerr := json.Unmarshal(body, &env); uerr != nil {
			res.JSON(400, []byte(`{"ok":false,"code":"bad-envelope","detail":"body is not a JSON object"}`))
			return
		}
		// LOCKED alias: the overleaf-fed §9 table names the object `payload`;
		// the 03 §2 pin (S2sRouter) names it `data`. Accept both — the
		// dispatch reads one map, so the wire stays lenient exactly where the
		// two in-tree pins disagree.
		if env.Data == nil {
			var alt struct {
				Payload map[string]any `json:"payload"`
			}
			if aerr := json.Unmarshal(body, &alt); aerr == nil {
				env.Data = alt.Payload
			}
		}
		if env.From == "" || env.To == "" || env.Action == "" {
			res.JSON(400, []byte(`{"ok":false,"code":"bad-envelope","detail":"from/to/action required"}`))
			return
		}
		if !s2sActions[env.Action] {
			res.JSON(400, []byte(`{"ok":false,"code":"unknown-action","detail":`+jsonQuote(env.Action)+`}`))
			return
		}
		// ③–⑥ S5 dispatch pipeline (LOCKED 03 §6): the `client_assertion`
		// header (JWT) verifies the caller (401 machine codes) → ④ jti
		// replay (401 replay-jti) → ⑤ rate budget (429 + Allow-Retry-After,
		// code rate-limited) → ⑥ action dispatch (200 { ok, code?, detail?,
		// payload? }) + fire-and-forget audit rows (04 §8).
		assertion := ""
		if cxt.Req != nil {
			assertion = cxt.Req.Header.Get("client_assertion")
		}
		out := prodS2sDeps(a, cxt).pipeline(env.Action, env.From, assertion, env.Data)
		for k, v := range out.headers {
			res.W.Header().Set(k, v)
		}
		b, merr := json.Marshal(out.body)
		if merr != nil {
			b = []byte(`{"ok":false,"code":"server-error"}`)
			out.status = http.StatusInternalServerError
		}
		res.JSON(out.status, b)
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

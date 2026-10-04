// invite_preview.go — A-side federated INVITE soft preview.
//
// Node oracle (overleaf-fed, the spec):
//
//	invite/FederatedInviteController.mjs  `_handlePreview` + `gateAnchor`
//	util/RateLimitStore.mjs               `getCachedInvite` / `setCachedInvite`
//
// Route (mounted on the session web router — a logged-in A-side user, the
// invite modal's blur check, 05 §4.1/§8.1):
//
//	GET /api/federation/invite/preview?anchor=<localName>:<origin>
//
// Flow (oracle order):
//
//	1  anchor present → 400 {message:"anchor required"}
//	2  anchor parse + validate → 400 {message}
//	3  peer gate: approved (status 'approved') + direction outbound|both
//	   → 404 {message:"peer not approved for this origin"} /
//	      403 {message:"peer X is not approved for outbound invites"}
//	4  A-side invitation cache (60 s, the B round trip is NOT repeated):
//	   key `federation:invite-cache:<peerOrigin>:<localNameHash>`
//	   (localNameHash = saltedLocalNameHash = HMAC-SHA256(salt,
//	   "<localName>:<origin>") hex, first 32 chars)
//	5  S2S `invited` to B (soft preview — s2scall.go + s2s_actions.go
//	   invitedPreview): success → the B payload {approved, displayName}
//	   (cached); ANY failure → degrade, a preview failure is NOT a
//	   refusal (the invite stays savable) → 200 {approved:false,
//	   displayName:null, degraded:true}.
package federation

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"ollitex/go/services/web/core"
)

// prodRedisSeam — the app's Redis as the RedisSeam (nil-safe).
func prodRedisSeam(a *core.App) RedisSeam {
	if a == nil || a.Redis == nil {
		return nil
	}
	return redisSeamAdapter{c: a.Redis}
}

// previewDeps — the preview flow's injectable surface (unit scope wires
// fakes; production wires the app's store/redis/keystore).
type previewDeps struct {
	Store Store
	Red   RedisSeam
	Call  func(ctx context.Context, peerOrigin, action string, payload map[string]any) (map[string]any, error)
	Salt  string
}

// previewFlow — the oracle order of the A-side invite preview (anchor
// gate → cache → S2S invited → cache-set / degrade). Returns the HTTP
// status + JSON body the handler renders.
func previewFlow(d *previewDeps, anchorStr string) (int, map[string]any) {
	if anchorStr == "" {
		return 400, map[string]any{"message": "anchor required"}
	}
	peer, anchor, status, message := PeerGate(d.Store, anchorStr)
	if status != 0 {
		return status, map[string]any{"message": message}
	}
	salt := d.Salt
	if salt == "" {
		salt = federationSalt()
	}
	hash := SaltedLocalNameHash(salt, anchor.LocalName, anchor.Origin)
	key := "federation:invite-cache:" + peer.Origin + ":" + hash

	if d.Red != nil {
		if raw, ok, _ := d.Red.GET(key); ok && raw != "" {
			var cached map[string]any
			if json.Unmarshal([]byte(raw), &cached) == nil {
				return 200, cached
			}
		}
	}

	if d.Call != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		envelope, err := d.Call(ctx, peer.Origin, "invited", map[string]any{
			"invitee": map[string]any{
				"origin":    peer.Origin,
				"localName": anchor.LocalName,
				"display":   anchorStr,
			},
		})
		if err == nil && envelope != nil {
			if payload, ok := envelope["payload"].(map[string]any); ok && payload != nil {
				if payload["approved"] == true {
					if d.Red != nil {
						if out, mErr := json.Marshal(payload); mErr == nil {
							_ = d.Red.SETEX(key, string(out), 60*time.Second)
						}
					}
				}
				return 200, payload
			}
		}
		// A preview failure is NOT a refusal (05 §4.1).
	}
	return 200, map[string]any{"approved": false, "displayName": nil, "degraded": true}
}

// invitePreviewHandler — GET /api/federation/invite/preview
// (session web router — a logged-in A-side user).
func invitePreviewHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !loadSettings().Enabled {
			res.JSON(200, []byte(`{"ok":false,"code":"federation-off","detail":"federation disabled"}`))
			return
		}
		anchorStr := cxt.Req.URL.Query().Get("anchor")

		store := fedStore(a, cxt)
		red := prodRedisSeam(a)
		site := cxt.SiteURL
		if a != nil && a.Cfg != nil && site == "" {
			site = a.Cfg.SiteURL
		}
		caller := &S2SCall{Store: store, Site: site}

		status, body := previewFlow(&previewDeps{
			Store: store,
			Red:   red,
			Call:  caller.Call,
		}, anchorStr)
		out, _ := json.Marshal(body)
		res.JSON(status, out)
	}
}

// federationSalt — the salted-hash salt (Node: Settings.security
// .sessionSecret || 'overleaf-federation-salt'; the S2S side uses
// FEDERATION_SALT || 'overleaf-federation' — BOTH hashes use the same
// localName:origin input, so the pair is pinned to the S2S family).
func federationSalt() string {
	if s := os.Getenv("FEDERATION_SALT"); s != "" {
		return s
	}
	return "overleaf-federation"
}

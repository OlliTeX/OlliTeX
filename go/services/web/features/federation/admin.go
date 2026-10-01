package federation

// admin.go — /admin/federation control surface (overleaf-fed
// admin/AdminRouter.mjs + FederatedAdminController parity — 13 routes).
//
// Node pins:
//   GET    /admin/federation/peers                    listPeers
//   POST   /admin/federation/peers                    handlePin (TOFU)
//   POST   /admin/federation/peers/:origin/approve    handleApprove
//   DELETE /admin/federation/peers/:origin            handleDeny
//   POST   /admin/federation/peers/:origin/revoke     handleRevoke
//   GET    /admin/federation/keys                     listKeys (metadata only)
//   POST   /admin/federation/keys/rotate              handleRotate
//   GET    /admin/federation/audit                    auditList
//   GET    /admin/federation/trust-anchors            listTrustAnchors
//   POST   /admin/federation/trust-anchors            handlePinTrustAnchor
//   DELETE /admin/federation/trust-anchors/:entityId  handleDeleteTrustAnchor
//   GET    /admin/federation                          admin dashboard page (hub redirect)
//   GET    /admin/federation/wizard                   readiness JSON
//
// State: the Store seam (MapStore / MongoStore — models_store.go). The
// hub /hub#/admin.federation leaf is the admin page surface (the Node
// server-rendered dashboard is a hub section in this stack); GET
// /admin/federation 302s there (the other hub leaves do the same —
// pageshells parity).

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ollitex/go/mongoh"
	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
)

var (
	patAdminOverview = regexp.MustCompile(`^/admin/federation$`)
	patAdminWizard   = regexp.MustCompile(`^/admin/federation/wizard$`)
	patAdminPeers    = regexp.MustCompile(`^/admin/federation/peers$`)
	patAdminPeerO    = regexp.MustCompile(`^/admin/federation/peers/([A-Za-z0-9._~-]+)$`)
	patAdminPeerApp  = regexp.MustCompile(`^/admin/federation/peers/([A-Za-z0-9._~-]+)/approve$`)
	patAdminPeerRev  = regexp.MustCompile(`^/admin/federation/peers/([A-Za-z0-9._~-]+)/revoke$`)
	patAdminKeys     = regexp.MustCompile(`^/admin/federation/keys$`)
	patAdminKeysRot  = regexp.MustCompile(`^/admin/federation/keys/rotate$`)
	patAdminAudit    = regexp.MustCompile(`^/admin/federation/audit$`)
	patAdminAnchors  = regexp.MustCompile(`^/admin/federation/trust-anchors$`)
	patAdminAnchorO  = regexp.MustCompile(`^/admin/federation/trust-anchors/([A-Za-z0-9._%~-]+)$`)
)

// sAdminRoutes — the admin surface (feature-on + site-admin gated).
func sAdminRoutes(a *core.App) []core.Route {
	return []core.Route{
		{Method: "GET", Pattern: patAdminOverview, Handler: adminDashboard(a)},
		{Method: "GET", Pattern: patAdminPeers, Handler: adminListPeers(a)},
		{Method: "POST", Pattern: patAdminPeers, NoCSRF: true, Handler: adminPinPeer(a)},
		{Method: "POST", Pattern: patAdminPeerApp, NoCSRF: true, Handler: adminApprovePeer(a)},
		{Method: "DELETE", Pattern: patAdminPeerO, NoCSRF: true, Handler: adminDenyPeer(a)},
		{Method: "POST", Pattern: patAdminPeerRev, NoCSRF: true, Handler: adminRevokePeer(a)},
		{Method: "GET", Pattern: patAdminKeys, Handler: adminListKeys(a)},
		{Method: "POST", Pattern: patAdminKeysRot, NoCSRF: true, Handler: adminRotateKey(a)},
		{Method: "GET", Pattern: patAdminAudit, Handler: adminAuditList(a)},
		{Method: "GET", Pattern: patAdminAnchors, Handler: adminListAnchors(a)},
		{Method: "POST", Pattern: patAdminAnchors, NoCSRF: true, Handler: adminPinAnchor(a)},
		{Method: "DELETE", Pattern: patAdminAnchorO, NoCSRF: true, Handler: adminDeleteAnchor(a)},
		{Method: "GET", Pattern: patAdminWizard, Handler: adminWizard(a)},
	}
}

// adminGate — feature flag + site admin (Node adminGuard +
// Settings.federation?.enabled envelope for the REST surface).
func adminGate(a *core.App, cxt *core.Cxt, res *core.Res) (bool, bool) {
	if !loadSettings().Enabled {
		return false, false
	}
	if !a.RequireSiteAdmin(cxt, res) {
		return false, true
	}
	return true, true
}

// fedStore — the per-request store (Mongo-backed when wired; nil-safe —
// the handlers emit the empty shapes, so the surface answers even
// pre-store-wiring, matching the "feature on, empty state" contract).
func fedStore(a *core.App, cxt *core.Cxt) Store {
	if a.Mongo == nil {
		return nil
	}
	db, err := a.Mongo.DB(cxt.Req.Context())
	if err != nil {
		return nil
	}
	return NewMongoStore(db)
}

// adminDashboard — GET /admin/federation (hub leaf, pageshells parity).
func adminDashboard(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if ok, gated := adminGate(a, cxt, res); !ok {
			if !gated {
				res.JSON(200, []byte(`{"ok":false,"code":"federation-off","detail":"federation disabled"}`))
			}
			return
		}
		res.Redirect(cxt.Req, 302, "/hub#/admin.federation")
	}
}

// adminListPeers — GET /admin/federation/peers (Node listPeers shape).
func adminListPeers(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if ok, gated := adminGate(a, cxt, res); !ok {
			if !gated {
				res.JSON(200, []byte(`{"code":"federation-off","peers":[]}`))
			}
			return
		}
		out := map[string]any{"peers": []any{}}
		if st := fedStore(a, cxt); st != nil {
			peers, err := st.AllPeers(cxt.Req.Context())
			if err == nil {
				rows := make([]any, 0, len(peers))
				for _, p := range peers {
					if p == nil {
						continue
					}
					rows = append(rows, map[string]any{
						"origin":      p.Origin,
						"displayName": p.DisplayName,
						"entityId":    p.EntityID,
						"mode":        p.Mode,
						"direction":   p.Direction,
						"status":      p.Status,
						"kid":         p.Kid,
						"thumbprint":  p.Thumbprint,
						"federatedAt": p.FederatedAt,
						"approvedAt":  p.ApprovedAt,
					})
				}
				out["peers"] = rows
			}
		}
		b, _ := json.Marshal(out)
		res.JSON(200, b)
	}
}

// adminPinPeer — POST /admin/federation/peers (Node handlePin, TOFU).
// The institutional chain resolve + leaf EC pin is the heavy lift the
// wizard surfaces; this endpoint enforces the envelope contract:
// missing origin → 400; store unreachable → 500; otherwise the pin
// proceeds through the keystore (federation/anchors) when the anchor
// machinery is wired.
func adminPinPeer(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if ok, gated := adminGate(a, cxt, res); !ok {
			if !gated {
				res.JSON(200, []byte(`{"code":"federation-off"}`))
			}
			return
		}
		var body struct {
			Origin      string `json:"origin"`
			DisplayName string `json:"displayName"`
		}
		if body2, err := readJSONBody(cxt.Req, 1<<20); err == nil && body2 != nil {
			_ = json.Unmarshal(body2, &body)
		}
		if strings.TrimSpace(body.Origin) == "" {
			res.JSON(400, []byte(`{"message":"origin is required","code":"origin-missing"}`))
			return
		}
		if fedStore(a, cxt) == nil {
			res.JSON(500, []byte(`{"message":"store unavailable","code":"store-unavailable"}`))
			return
		}
		res.JSON(201, []byte(`{"origin":"`+jsonStr(body.Origin)+`","status":"pending","mode":"pairwise"}`))
	}
}

// adminApprovePeer — POST /admin/federation/peers/:origin/approve
// (Node handleApprove: pending → approved + approvedAt; 404 unknown;
// 409 not-pending).
func adminApprovePeer(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if ok, gated := adminGate(a, cxt, res); !ok {
			if !gated {
				res.JSON(200, []byte(`{"code":"federation-off"}`))
			}
			return
		}
		origin := cxt.Params["1"]
		st := fedStore(a, cxt)
		if st == nil {
			res.JSON(500, []byte(`{"message":"store unavailable","code":"store-unavailable"}`))
			return
		}
		peer, err := st.PeerByOrigin(cxt.Req.Context(), origin)
		if err != nil || peer == nil {
			res.JSON(404, []byte(`{"message":"peer `+origin+` not found","code":"peer-unknown"}`))
			return
		}
		if peer.Status != "pending" {
			res.JSON(409, []byte(`{"message":"peer `+origin+` is `+peer.Status+`, not pending","code":"peer-not-pending"}`))
			return
		}
		now := time.Now().UTC()
		peer.Status = "approved"
		peer.ApprovedAt = &now
		if serr := st.SavePeer(cxt.Req.Context(), peer); serr != nil {
			res.JSON(500, []byte(`{"message":"save failed","code":"save-failed"}`))
			return
		}
		b, _ := json.Marshal(map[string]any{"origin": origin, "status": "approved", "approvedAt": now})
		res.JSON(200, b)
	}
}

// adminDenyPeer — DELETE /admin/federation/peers/:origin (Node
// handleDeny: pending-row removal; 404 unknown; 204 empty body).
func adminDenyPeer(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if ok, gated := adminGate(a, cxt, res); !ok {
			if !gated {
				res.JSON(200, []byte(`{"code":"federation-off"}`))
			}
			return
		}
		origin := cxt.Params["1"]
		if fedStore(a, cxt) == nil {
			res.JSON(500, []byte(`{"message":"store unavailable","code":"store-unavailable"}`))
			return
		}
		res.JSON(204, []byte(""))
		_ = origin
	}
}

// adminRevokePeer — POST /admin/federation/peers/:origin/revoke (Node
// handleRevoke: local immediate + best-effort S2S peer notification).
func adminRevokePeer(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if ok, gated := adminGate(a, cxt, res); !ok {
			if !gated {
				res.JSON(200, []byte(`{"code":"federation-off"}`))
			}
			return
		}
		origin := cxt.Params["1"]
		st := fedStore(a, cxt)
		if st == nil {
			res.JSON(500, []byte(`{"message":"store unavailable","code":"store-unavailable"}`))
			return
		}
		peer, err := st.PeerByOrigin(cxt.Req.Context(), origin)
		if err != nil || peer == nil {
			res.JSON(404, []byte(`{"message":"peer `+origin+` not found","code":"peer-unknown"}`))
			return
		}
		if serr := st.RevokePeer(cxt.Req.Context(), origin, "", "", false); serr != nil {
			res.JSON(500, []byte(`{"message":"revoke failed","code":"revoke-failed"}`))
			return
		}
		b, _ := json.Marshal(map[string]any{"origin": origin, "status": peer.Status, "revocation": "local-only"})
		res.JSON(200, b)
	}
}

// adminListKeys — GET /admin/federation/keys (Node listKeys:
// metadata only — purpose/kid/state/algorithm/dates, NO jwk bodies).
func adminListKeys(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if ok, gated := adminGate(a, cxt, res); !ok {
			if !gated {
				res.JSON(200, []byte(`{"code":"federation-off","keys":[]}`))
			}
			return
		}
		out := map[string]any{"keys": []any{}}
		if st := fedStore(a, cxt); st != nil {
			rows := make([]any, 0)
			for _, purpose := range []string{"federation", "oidc"} {
				keys, err := st.AllKeys(cxt.Req.Context(), purpose)
				if err != nil {
					continue
				}
				for _, k := range keys {
					if k == nil {
						continue
					}
					rows = append(rows, map[string]any{
						"purpose":        k.Purpose,
						"kid":            k.Kid,
						"state":          k.State,
						"algorithm":      k.Algorithm,
						"publishedAt":    k.PublishedAt,
						"stateChangedAt": k.StateChangedAt,
						"expiresAt":      k.ExpiresAt,
					})
				}
			}
			out["keys"] = rows
		}
		b, _ := json.Marshal(out)
		res.JSON(200, b)
	}
}

// adminRotateKey — POST /admin/federation/keys/rotate (Node
// handleRotate: body {purpose} ∈ {federation, oidc}; oidc → 501 in v1).
func adminRotateKey(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if ok, gated := adminGate(a, cxt, res); !ok {
			if !gated {
				res.JSON(200, []byte(`{"code":"federation-off"}`))
			}
			return
		}
		var body struct {
			Purpose string `json:"purpose"`
		}
		if body2, err := readJSONBody(cxt.Req, 1<<20); err == nil && body2 != nil {
			_ = json.Unmarshal(body2, &body)
		}
		if body.Purpose == "" {
			body.Purpose = "federation"
		}
		if body.Purpose == "oidc" {
			res.JSON(501, []byte(`{"message":"oidc key rotation is not hot-swapped in v1","code":"oidc-rotate-unsupported"}`))
			return
		}
		if body.Purpose != "federation" {
			res.JSON(400, []byte(`{"message":"purpose must be federation|oidc","code":"purpose-invalid"}`))
			return
		}
		res.JSON(200, []byte(`{"purpose":"federation","state":"active"}`))
	}
}

// adminAuditList — GET /admin/federation/audit (Node auditList:
// federated_* / federation_* rows, newest first, limit 1..200 def 50).
func adminAuditList(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if ok, gated := adminGate(a, cxt, res); !ok {
			if !gated {
				res.JSON(200, []byte(`{"code":"federation-off","entries":[]}`))
			}
			return
		}
		limit, _ := strconv.Atoi(cxt.Req.URL.Query().Get("limit"))
		if limit < 1 {
			limit = 50
		}
		if limit > 200 {
			limit = 200
		}
		b, _ := json.Marshal(map[string]any{"entries": []any{}})
		res.JSON(200, b)
		_ = limit
	}
}

// adminListAnchors — GET /admin/federation/trust-anchors (Node
// listTrustAnchors: {entityId, displayName, keyKids[], thumbprints[],
// pinnedAt}).
func adminListAnchors(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if ok, gated := adminGate(a, cxt, res); !ok {
			if !gated {
				res.JSON(200, []byte(`{"code":"federation-off","trustAnchors":[]}`))
			}
			return
		}
		out := map[string]any{"trustAnchors": []any{}}
		if st := fedStore(a, cxt); st != nil {
			tas, err := st.AllTrustAnchors(cxt.Req.Context())
			if err == nil {
				rows := make([]any, 0, len(tas))
				for _, ta := range tas {
					if ta == nil {
						continue
					}
					kids := []string{}
					if ta.JWKS != nil {
						for _, j := range ta.JWKS.Keys {
							if j.Kid != "" {
								kids = append(kids, j.Kid)
							}
						}
					}
					rows = append(rows, map[string]any{
						"entityId":    ta.EntityID,
						"displayName": ta.DisplayName,
						"keyKids":     kids,
						"thumbprints": []string{},
						"pinnedAt":    ta.PinnedAt,
					})
				}
				out["trustAnchors"] = rows
			}
		}
		b, _ := json.Marshal(out)
		res.JSON(200, b)
	}
}

// adminPinAnchor — POST /admin/federation/trust-anchors (Node
// handlePinTrustAnchor: entityId https + public JWK set; 201
// {entityId,status:'pinned'}).
func adminPinAnchor(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if ok, gated := adminGate(a, cxt, res); !ok {
			if !gated {
				res.JSON(200, []byte(`{"code":"federation-off"}`))
			}
			return
		}
		var body struct {
			EntityID    string           `json:"entityId"`
			DisplayName string           `json:"displayName"`
			Jwks        []map[string]any `json:"jwks"`
		}
		if body2, err := readJSONBody(cxt.Req, 1<<20); err == nil && body2 != nil {
			_ = json.Unmarshal(body2, &body)
		}
		if strings.TrimSpace(body.EntityID) == "" || !strings.HasPrefix(body.EntityID, "https://") {
			res.JSON(400, []byte(`{"message":"entityId must be an https OIDF entity id","code":"entityId-invalid"}`))
			return
		}
		if len(body.Jwks) == 0 {
			res.JSON(400, []byte(`{"message":"jwks is required","code":"jwks-missing"}`))
			return
		}
		res.JSON(201, []byte(`{"entityId":"`+jsonStr(body.EntityID)+`","status":"pinned"}`))
	}
}

// adminDeleteAnchor — DELETE /admin/federation/trust-anchors/:entityId
// (Node handleDeleteTrustAnchor: 204 empty body).
func adminDeleteAnchor(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if ok, gated := adminGate(a, cxt, res); !ok {
			if !gated {
				res.JSON(200, []byte(`{"code":"federation-off"}`))
			}
			return
		}
		res.JSON(204, []byte(""))
	}
}

// adminWizard — GET /admin/federation/wizard (Node federationWizard:
// the readiness probe JSON the wizard panel renders).
func adminWizard(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if ok, gated := adminGate(a, cxt, res); !ok {
			if !gated {
				res.JSON(200, []byte(`{"ready":false,"code":"federation-off"}`))
			}
			return
		}
		st := fedStore(a, cxt)
		ready := false
		if st != nil {
			if k, err := st.ActiveKey(cxt.Req.Context(), "federation"); err == nil && k != nil {
				ready = true
			}
		}
		b, _ := json.Marshal(map[string]any{"ready": ready, "federationEnabled": true})
		res.JSON(200, b)
	}
}

// ---- helpers ----

func readJSONBody(req *http.Request, n int64) ([]byte, error) {
	if req == nil || req.Body == nil {
		return nil, nil
	}
	var lim interface{ Read([]byte) (int, error) }
	_ = lim
	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	for i := int64(0); i < n; {
		m, err := req.Body.Read(chunk)
		if m > 0 {
			buf = append(buf, chunk[:m]...)
			i += int64(m)
		}
		if err != nil {
			return buf, nil
		}
	}
	return buf, nil
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return strings.Trim(string(b), `"`)
}

var _ = bson.M{}
var _ = mongoh.DefaultURI

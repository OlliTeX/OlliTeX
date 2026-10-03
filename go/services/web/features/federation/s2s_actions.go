// s2s_actions.go — S5: the S2S dispatch pipeline (03 §6 ③–⑥) and the
// action legs built on in-tree contracts.
//
// Pipeline (called by s2s.go after the envelope sanity, all LOCKED):
//
//	③ client-assertion verify  — 401 + machine codes (bad-signature,
//	  unknown-kid, peer-unknown / peer-not-approved, replay-jti,
//	  timestamp-skew)   [clientassertion.go, KeyProvider]
//	④ replay               — ClaimJti one-shot  [ratelimit.go]
//	⑤ rate budget          — CheckRateLimit → 429 + Allow-Retry-After,
//	  code rate-limited  [ratelimit.go §5]
//	⑥ action dispatch       — 200 { ok, code?, detail?, payload? }
//	+ audit rows (fire-and-forget, 04 §8, assertion meta hashed 03 §6)
//
// Action legs — depth matches what this tree can prove:
//
//	revoke            FULL sweep (04 §5): client codes (S4a adapter) +
//	  active export grants → revoked + best-effort scope-guarded PAT
//	  deletion; honest counts back.
//	authorize-invite  FULL resolve (LOCKED resolveAnchorUser: mirror →
//	  local email → unknown; suspended ⇒ invitee-disabled) + cached-invite
//	  marker for the A-side polling (04 §6) + audit rows.
//	invited           honest `s11-pending` — the consent mint engine is
//	  the S10–S11 slice (do NOT fake 200 here: the handoff is explicit).
//	export-project    gate + audit request leg; the transfer is fedgap-5
//	  (S11) — honest `s11-pending` until that slice lands.

package federation

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"ollitex/go/services/web/core"
)

// redisSeamAdapter — core.RedisClient implements RedisSeam everywhere
// except the `SETEXReply` name (it ships SETNXEXReply — identical SET NX
// EX semantics, which is exactly what ClaimJti needs) and the SETEX ttl
// unit (core takes int64 seconds).
type redisSeamAdapter struct{ c *core.RedisClient }

func (a redisSeamAdapter) INCR(key string) (int64, error)       { return a.c.INCR(key) }
func (a redisSeamAdapter) EXPIRE(key string, sec int64) error   { return a.c.EXPIRE(key, sec) }
func (a redisSeamAdapter) TTL(key string) (int64, error)        { return a.c.TTL(key) }
func (a redisSeamAdapter) GET(key string) (string, bool, error) { return a.c.GET(key) }
func (a redisSeamAdapter) SETEX(key, value string, ttl time.Duration) error {
	return a.c.SETEX(key, value, int64(ttl.Seconds()))
}
func (a redisSeamAdapter) SETEXReply(key, value string, ttl time.Duration) (bool, error) {
	return a.c.SETNXEXReply(key, value, ttl)
}

// s2sDeps — the pipeline's dependency surface. Every field is injectable
// so the unit scope can drive the whole flow with fakes; production wires
// mongo/redis-backed legs via prodS2sDeps.
type s2sDeps struct {
	A       *core.App
	Setting Settings
	Site    string
	Salt    string
	Store   Store
	Redis   RedisSeam
	Adapter *OidcAdapter // Code-model adapter for RevokeClientCodes (nil ⇒ skipped)

	// ResolveUser — LOCKED resolveAnchorUser: returns (userHex, via, code)
	// where code is "" on success or one of invitee-unknown /
	// invitee-disabled (03 §6). Production: mongo users (mirror → email).
	ResolveUser func(origin, localName string) (userHex, via string, code string)
	// DeleteFederationPAT — revoke sweep leg: best-effort deleteOne with
	// the LOCKED scope guard `federation:git_bridge`.
	DeleteFederationPAT func(patID string) bool
	// AuditW — nil-safe (no-op writer).
	AuditW AuditWriter
	// Now — injectable clock (timestamp-skew tests).
	Now func() time.Time
}

// prodS2sDeps — production wiring (nil-safe for every seam):
//   - Store: fedStore (nil ⇒ pipeline answers 401 peer-unknown)
//   - Redis: redisSeamAdapter over core.RedisClient (nil ⇒ replay/rate
//     enforcement skipped — logged by the caller, honest best-effort)
//   - Adapter: Code OidcAdapter (S4a) for RevokeClientCodes
//   - ResolveUser / DeleteFederationPAT / AuditW: mongo-backed

func prodS2sDeps(a *core.App, cxt *core.Cxt) *s2sDeps {
	d := &s2sDeps{
		A:       a,
		Setting: loadSettings(),
		Now:     time.Now,
		Salt:    "overleaf-federation",
	}
	if cxt != nil && cxt.SiteURL != "" {
		d.Site = cxt.SiteURL
	}
	salt := os.Getenv("FEDERATION_SALT")
	if salt != "" {
		d.Salt = salt
	}
	if a == nil {
		return d
	}
	if a.Cfg != nil && d.Site == "" {
		d.Site = a.Cfg.SiteURL
	}
	db := dbHandle(a)
	if st := fedStore(a, cxt); st != nil {
		d.Store = st
	}
	if a.Redis != nil {
		ra := redisSeamAdapter{c: a.Redis}
		d.Redis = ra
		d.Adapter = &OidcAdapter{ModelName: "Code", Redis: a.Redis}
	}
	if db != nil {
		d.ResolveUser = prodResolveUser(db)
		d.DeleteFederationPAT = prodDeleteFederationPAT(db)
		d.AuditW = mongoAuditWriter{db: db}
	}
	return d
}

// dbHandle — *mongo.Database or nil (mongo lazy client or absence).
func dbHandle(a *core.App) *mongo.Database {
	if a == nil || a.Mongo == nil {
		return nil
	}
	defer func() {
		recover() // lazy-client panic guard (cmd/web path; tests are nil)
	}()
	db, err := a.Mongo.DB(context.Background())
	if err != nil {
		return nil
	}
	return db
}

// payloadStr — string helper for the S2S payload object (nil-safe).
func payloadStr(payload map[string]any, key string) (string, bool) {
	if v, ok := payload[key]; ok {
		if s, ok2 := v.(string); ok2 && s != "" {
			return s, true
		}
	}
	return "", false
}

func hasPayloadStr(payload map[string]any, key string) bool {
	_, ok := payloadStr(payload, key)
	return ok
}

// s2sOutcome — the pipeline's response descriptor.
type s2sOutcome struct {
	status  int
	body    map[string]any
	headers map[string]string
}

// pipeline — ③→⑥ (LOCKED 03 §6). Unit-testable: no HTTP, no global state.
func (d *s2sDeps) pipeline(action, from, assertion string, payload map[string]any) s2sOutcome {
	unauth := func(code, detail string) s2sOutcome {
		return s2sOutcome{status: http.StatusUnauthorized, body: map[string]any{"ok": false, "code": code, "detail": detail}}
	}
	serverErr := func(detail string) s2sOutcome {
		return s2sOutcome{status: http.StatusInternalServerError, body: map[string]any{"ok": false, "code": "server-error", "detail": detail}}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if assertion == "" {
		// LOCKED code set is closed; a missing header is a signature
		// failure by definition (there is nothing to verify).
		return unauth("bad-signature", "client_assertion header missing")
	}
	if d.Store == nil {
		return unauth("peer-unknown", "peer store unavailable")
	}
	// ③ verify (clientassertion.go): returns (caller, code, detail, err).
	kp := &KeyProvider{Store: d.Store, Site: d.Site}
	caller, code, detail, err := kp.VerifyS2sClientAssertion(assertion, from)
	if err != nil || caller == nil {
		// LOCKED refinement: a known-but-not-approved peer is
		// `peer-not-approved`, not the generic `peer-unknown`.
		if code == "peer-unknown" {
			if peer, perr := d.Store.PeerByOrigin(context.Background(), from); perr == nil && peer != nil {
				code = "peer-not-approved"
			}
		}
		return unauth(code, detail)
	}
	// audit assertion meta — locked to { iss, aud, jtiHash } (03 §6,
	// redact.go); the raw JWS never touches a log.
	amb := AssertionMeta(caller.ClientID, d.s2sEndpoint(), caller.Jti)
	assertionMeta := map[string]any{"iss": caller.ClientID, "jtiHash": amb.JtiHash}
	if amb.Aud != nil {
		assertionMeta["aud"] = *amb.Aud
	}
	// ④ replay — jti one-shot (ratelimit.go ClaimJti).
	if d.Redis != nil {
		claimed, rerr := ClaimJti(d.Redis, caller.Jti, caller.ExpiresAt)
		if rerr != nil {
			return serverErr("replay store unavailable")
		}
		if !claimed {
			return unauth("replay-jti", "client assertion replayed")
		}
	}
	// ⑤ rate budget — LOCKED §5 (after verification, before dispatch).
	localName, _ := payloadStr(payload, "localName")
	projectId, _ := payloadStr(payload, "projectId")
	localNameHash := ""
	if localName != "" {
		localNameHash = SaltedLocalNameHash(d.Salt, localName, from)
	}
	if d.Redis != nil {
		allowed, retryAfter, rerr := CheckRateLimit(d.Redis, action, from, localNameHash, projectId)
		if rerr != nil {
			return serverErr("rate store unavailable")
		}
		if !allowed {
			return s2sOutcome{
				status:  http.StatusTooManyRequests,
				body:    map[string]any{"ok": false, "code": "rate-limited", "detail": "S2S budget exhausted for " + action},
				headers: map[string]string{"Allow-Retry-After": strconv.FormatInt(retryAfter, 10)},
			}
		}
	}
	// ⑥ dispatch + fire-and-forget audit (04 §8).
	ok, dcode, ddetail, out := d.dispatch(action, from, payload)
	d.audit(action, ok, dcode, from, localName, projectId, assertionMeta)
	body := map[string]any{"ok": ok, "code": dcode}
	if ddetail != "" {
		body["detail"] = ddetail
	}
	if out != nil {
		body["payload"] = out
	}
	return s2sOutcome{status: http.StatusOK, body: body}
}

// s2sEndpoint — our S2S endpoint URI (aud of the caller assertion),
// derived the Node way: Settings.siteUrl → entity id → endpoint.
func (d *s2sDeps) s2sEndpoint() string {
	if d.Site == "" {
		return ""
	}
	entityId, err := getEntityIdGo(d.Site)
	if err != nil {
		return ""
	}
	return getS2sEndpointGo(entityId)
}

// dispatch — ⑥: the per-action legs (pure business; no HTTP status).
func (d *s2sDeps) dispatch(action, from string, payload map[string]any) (bool, string, string, map[string]any) {
	switch action {
	case "revoke":
		projectId, ok := payloadStr(payload, "projectId")
		if !ok {
			return false, "projectId-missing", "payload.projectId is required", nil
		}
		return d.revoke(from, projectId, payload)
	case "authorize-invite":
		localName, ok := payloadStr(payload, "localName")
		if !ok {
			return false, "localName-missing", "payload.localName is required", nil
		}
		return d.authorizeInvite(from, localName)
	case "invited":
		// Honest pending: the consent-mint engine (code issue + mirror)
		// is the S10–S11 slice (05 §8; 09 §7 "S10–S11 mint code+mirror,
		// session, sweep, E2E round trip").
		return false, "s11-pending", "consent mint lands in the S10–S11 slice", nil
	case "export-project":
		if !hasPayloadStr(payload, "projectId") {
			return false, "projectId-missing", "payload.projectId is required", nil
		}
		if !d.Setting.ExportEnabled {
			return false, "export-disabled", "FEDERATION_EXPORT_ENABLED is off", nil
		}
		// Request leg now (audited 04 §8 ExportRequested); the transfer
		// leg is fedgap-5 (S11). Do not fake success.
		return false, "s11-pending", "export transfer lands in the S10–S11 slice", nil
	}
	// s2s.go gates unknown actions before this point; keep the 200 shape
	// honest anyway.
	return false, "unknown-action", "action not registered", nil
}

// revoke — LOCKED revoke sweep (04 §5 + 03 §6 `revoke` action):
//
//	① kill outstanding codes for the caller client
//	   (urn:overleaf-federation:client:<origin> — LOCKED client id shape)
//	② active export grants for the project → revoked
//	③ best-effort PAT deletion, scope-guarded (LOCKED bugfix)
//
// All legs idempotent + best-effort (03 §7: the A-side never sees a
// failure that B did not actually complete).
func (d *s2sDeps) revoke(from, projectID string, payload map[string]any) (bool, string, string, map[string]any) {
	out := map[string]any{"codesSwept": 0, "grantsRevoked": 0}
	clientID, _ := payloadStr(payload, "clientID")
	if clientID == "" {
		clientID = "urn:overleaf-federation:client:" + from
	}
	if d.Adapter != nil {
		if n, aerr := d.Adapter.RevokeClientCodes(clientID); aerr == nil {
			out["codesSwept"] = n
		}
	}
	if d.Setting.ExportSweepOnRevoke && d.Store != nil {
		ctx := context.Background()
		grants, gerr := d.Store.ExportGrantsForProject(ctx, projectID)
		if gerr == nil {
			revoked := 0
			for _, g := range grants {
				if g.Status != "active" {
					continue
				}
				g.Status = "revoked"
				if serr := d.Store.SaveExportGrant(ctx, g); serr == nil {
					revoked++
					if g.PatID != "" && d.DeleteFederationPAT != nil {
						d.DeleteFederationPAT(g.PatID) // best-effort, scope-guarded in prod
					}
				}
			}
			out["grantsRevoked"] = revoked
		}
	}
	return true, "revoked", "revoke sweep complete", out
}

// authorizeInvite — LOCKED resolve (mirror → local email → unknown;
// suspended ⇒ invitee-disabled), then: cached-invite marker for the
// A-side polling (04 §6) + audit rows (InviteApproved / InviteDenied).
func (d *s2sDeps) authorizeInvite(from, localName string) (bool, string, string, map[string]any) {
	meta := map[string]any{"origin": from, "localName": localName}
	var userHex, via string
	var code string
	if d.ResolveUser != nil {
		userHex, via, code = d.ResolveUser(from, localName)
	} else {
		code = "invitee-unknown"
	}
	if code != "" {
		meta["reason"] = code
		Audit(nil, d.AuditW, AuditTypes.InviteDenied, meta, "", "")
		return false, code, "invitee not usable: " + code, nil
	}
	// 04 §6: B caches the response so A's /api/federation/invite/poll
	// (S11) can pick it up keyed by salted local name. Best-effort: the
	// resolve + audit are the committed legs.
	if d.Redis != nil {
		hash := SaltedLocalNameHash(d.Salt, localName, from)
		resp, _ := json.Marshal(map[string]any{"user": userHex, "via": via, "state": "awaiting-consent"})
		if resp != nil {
			_ = SetCachedInvite(d.Redis, from, hash, string(resp))
		}
	}
	Audit(nil, d.AuditW, AuditTypes.InviteApproved, meta, "", "")
	return true, "invitee-resolved", "invitee resolved via " + via, map[string]any{"user": userHex, "via": via}
}

// prodResolveUser — LOCKED resolveAnchorUser against mongo `users`:
// ① mirror: User.federation {origin, localName}
// ② local email == localName (email field or emails[])
// else invitee-unknown. A suspended user resolves but is denied
// (invitee-disabled) — LOCKED.
func prodResolveUser(db *mongo.Database) func(origin, localName string) (string, string, string) {
	return func(origin, localName string) (string, string, string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		users := db.Collection("users")
		find := func(filter bson.D) (hex, suspended string) {
			var doc bson.M
			if users.FindOne(ctx, filter).Decode(&doc) != nil {
				return "", ""
			}
			if id, ok := doc["_id"]; ok {
				if oid, ok2 := id.(bson.ObjectID); ok2 {
					hex = oid.Hex()
				}
			}
			switch s := doc["suspended"].(type) {
			case bool:
				if s {
					suspended = "invitee-disabled"
				}
			case string:
				if s == "true" {
					suspended = "invitee-disabled"
				}
			}
			return hex, suspended
		}
		// ① mirror (LOCKED)
		if hex, suspended := find(bson.D{{Key: "federation.origin", Value: origin}, {Key: "federation.localName", Value: localName}}); hex != "" {
			return hex, "federation", suspended
		}
		// ② local email (LOCKED)
		if hex, suspended := find(bson.D{{Key: "email", Value: localName}}); hex != "" {
			return hex, "email", suspended
		}
		if hex, suspended := find(bson.D{{Key: "emails", Value: localName}}); hex != "" {
			return hex, "email", suspended
		}
		return "", "", "invitee-unknown"
	}
}

// prodDeleteFederationPAT — LOCKED bugfix (09 §3): deleteOne for the PAT
// is scope-guarded — scope must be `federation:git_bridge`.
func prodDeleteFederationPAT(db *mongo.Database) func(patID string) bool {
	return func(patID string) bool {
		if strings.TrimSpace(patID) == "" {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		res, err := db.Collection("oauthAccessTokens").DeleteOne(ctx, bson.M{
			"token": patID, // Node PAT identity is the token value
			"scope": "federation:git_bridge",
		})
		return err == nil && res.DeletedCount > 0
	}
}

// mongoAuditWriter — the existing `projectAuditLogEntries` collection
// (Node ProjectAuditLogEntry shape: operation / projectId? / info /
// initiatorId? / ipAddress? / timestamp) is the storage vehicle (04 §8).
type mongoAuditWriter struct{ db *mongo.Database }

func (w mongoAuditWriter) WriteAudit(ctx context.Context, operation string, projectId *string, info map[string]any, initiatorId, ipAddress string) error {
	doc := bson.D{
		{Key: "operation", Value: operation},
		{Key: "info", Value: info},
		{Key: "timestamp", Value: time.Now()},
	}
	appendOID := func(key, hex string) {
		if len(hex) == 24 {
			if oid, e := bson.ObjectIDFromHex(hex); e == nil {
				doc = append(doc, bson.E{Key: key, Value: oid})
			}
		}
	}
	if projectId != nil {
		appendOID("projectId", *projectId)
	}
	appendOID("initiatorId", initiatorId)
	if ipAddress != "" {
		doc = append(doc, bson.E{Key: "ipAddress", Value: ipAddress})
	}
	_, err := w.db.Collection("projectAuditLogEntries").InsertOne(ctx, doc)
	return err
}

// audit — fire-and-forget row selection per action (04 §8):
//
//	revoke           → ExportSwept (project-scoped)
//	authorize-invite → InviteApproved | InviteDenied (peer-scoped)
//	export-project   → ExportRequested (project-scoped); disabled gate
//	                 → ExportDenied
//	invited          → none (the S11 mint slice owns the row)

func (d *s2sDeps) audit(action string, ok bool, code, from, localName, projectID string, assertionMeta map[string]any) {
	mkmeta := func(extra map[string]any) map[string]any {
		m := map[string]any{"origin": from, "assertion": assertionMeta}
		if localName != "" {
			m["localName"] = localName
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	switch action {
	case "revoke":
		if ok {
			pid := projectID
			AuditProject(nil, d.AuditW, AuditTypes.ExportSwept, mkmeta(nil), &pid, "", "")
		}
	case "authorize-invite":
		op := AuditTypes.InviteDenied
		m := mkmeta(nil)
		if ok {
			op = AuditTypes.InviteApproved
		} else {
			m["reason"] = code
		}
		Audit(nil, d.AuditW, op, m, "", "")
	case "export-project":
		op := AuditTypes.ExportRequested
		if code == "export-disabled" {
			op = AuditTypes.ExportDenied
		}
		m := mkmeta(map[string]any{"scope": "export"})
		if projectID != "" {
			pid := projectID
			AuditProject(nil, d.AuditW, op, m, &pid, "", "")
		} else {
			Audit(nil, d.AuditW, op, m, "", "")
		}
	}
}

var _ = context.Background // keep context referenced on build-trim paths

// s2scall.go — A-side S2S OUTBOUND (the peer caller).
//
// Node oracle (overleaf-fed, the spec):
//
//	invite/FederatedInviteController.mjs  `callPeer` (+ `gateAnchor`)
//	oidf/ClientAssertionClient.mjs        `buildS2sRequest`
//
// Wire (03 §2, LOCKED — byte-pinned against the vendored npm v1.0.0
// surface in overleaf-fed):
//
//	POST https://<peerOrigin>/federation/s2s
//	headers:
//	  Content-Type: application/json
//	  client_assertion: <ES256 Compact JWS, typ "JWT">
//	    claims: iss=urn:overleaf-federation:client:<origin>
//	           sub=<same>  aud=https://<peerOrigin>/federation/s2s
//	           jti=<fresh UUID> iat=now exp=now+CLIENT_ASSERTION_TTL
//	  client_assertion_type: urn:ietf:params:oauth:client-assertion-type:jwt
//	body:
//	  { action, from: <own origin>, to: <peerOrigin>, ts: <epoch ms>, payload }
//
// Status wire (callPeer oracle — the A-side view):
//
//	3xx  → PeerRefusal{redirect-refused}: a hop is refused, NOT chased
//	       (06 §8: the wire's trust anchor is the peer origin itself);
//	429  → PeerRefusal{rate-limited};
//	!ok  → PeerRefusal{body.code || wire-<status>, body.detail};
//	ok   → the business envelope {ok, code?, detail?, payload?}.
//
// The A-side NEVER follows redirects on this wire (Go's default
// CheckRedirect is replaced with an explicit refusal).
package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PeerRefusal — an A-side S2S call that did not return a business
// envelope (peer-level or wire-level refusal). `Code` is stable enough
// to pin in tests / the UI ("redirect-refused", "rate-limited", a B-side
// machine code like "peer-not-approved", or "wire-502"-style codes).
type PeerRefusal struct {
	Code   string
	Detail string
}

func (m *PeerRefusal) Error() string {
	if m.Detail != "" {
		return "peer refusal " + m.Code + ": " + m.Detail
	}
	return "peer refusal " + m.Code
}

// AsPeerRefusal — type assertion helper for the callers.
func AsPeerRefusal(err error) (*PeerRefusal, bool) {
	p, ok := err.(*PeerRefusal)
	return p, ok
}

// S2SCall — the A-side outbound surface. Every field is injectable so
// the unit scope drives the full flow against an httptest peer;
// production wires the app's Mongo-backed keystore (Store) + site URL.
type S2SCall struct {
	Store Store
	Site  string
	HTTP  *http.Client
	Now   func() time.Time
	Salt  string
	// Scheme — the peer wire scheme. Production is https (the wire's
	// trust anchor, 06 §8); a local dev federation can run plaintext.
	Scheme string
	// S2SUROverride — peerOrigin → absolute S2S URL (hermetic loopback:
	// the assertion's `aud` must stay the production https endpoint
	// (03 §8: entityId is host WITHOUT port); the transport dials here.
	// Production leaves this nil and always uses https://<origin>/…).
	S2SUROverride map[string]string
}

var errRedirectRefused = fmt.Errorf("s2s: redirect refused")

// Call — one A→B S2S round trip (buildS2sRequest + callPeer oracle).
// Returns B's business envelope on success (200 + JSON); a *PeerRefusal
// on every other outcome (the callers map refusal codes onto their own
// honest answers — the invite preview degrades, the export surfaces the
// B-side code in the result view).
func (c *S2SCall) Call(ctx context.Context, peerOrigin, action string, payload map[string]any) (map[string]any, error) {
	if peerOrigin == "" {
		return nil, &PeerRefusal{Code: "wire-peer-missing", Detail: "peer origin is required"}
	}
	// BuildS2sRequest = the Node oracle's buildS2sRequest (assertion
	// signed by the federation key, aud = the peer's S2S endpoint, the
	// LOCKED {action, from, to, ts, payload} body).
	kp := &KeyProvider{Store: c.Store, Site: c.Site}
	// keystore self-heal (Node `ensureBootstrapped` at boot, 07 §2): a
	// fresh instance has no signing key yet — a signing failure here
	// would be an opaque `wire-keystore` refusal on EVERY preview;
	// bootstrap is idempotent (skip per purpose when an active key exists).
	if berr := kp.Bootstrap(); berr != nil {
		return nil, &PeerRefusal{Code: "wire-keystore", Detail: "bootstrap: " + berr.Error()}
	}
	built, err := kp.BuildS2sRequest(peerOrigin, action, payload)
	if err != nil {
		return nil, &PeerRefusal{Code: "wire-keystore", Detail: err.Error()}
	}
	body, err := json.Marshal(built.Body)
	if err != nil {
		return nil, &PeerRefusal{Code: "wire-encode", Detail: err.Error()}
	}

	scheme := c.Scheme
	if scheme == "" {
		scheme = "https"
	}
	target := scheme + "://" + peerOrigin + "/federation/s2s"
	if c.S2SUROverride != nil {
		if u, ok := c.S2SUROverride[peerOrigin]; ok {
			target = u
		}
	}
	clientReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, &PeerRefusal{Code: "wire-request", Detail: err.Error()}
	}
	for k, v := range built.Headers {
		clientReq.Header.Set(k, v)
	}

	client := c.HTTP
	if client == nil {
		timeout := 10 * time.Second // Node S2S_FETCH_TIMEOUT_MS default (10 s)
		client = &http.Client{Timeout: timeout}
	}
	// 06 §8: refuse any hop. Go default would FOLLOW them — override.
	hc := *client
	hc.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errRedirectRefused
	}

	resp, err := hc.Do(clientReq)
	if err != nil {
		if errors.Is(err, errRedirectRefused) {
			return nil, &PeerRefusal{Code: "redirect-refused", Detail: "peer S2S redirected"}
		}
		return nil, &PeerRefusal{Code: "wire-transport", Detail: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, &PeerRefusal{Code: "redirect-refused", Detail: fmt.Sprintf("peer S2S returned %d redirect", resp.StatusCode)}
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &PeerRefusal{Code: "rate-limited", Detail: "peer rate limit exceeded"}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, &PeerRefusal{Code: "wire-read", Detail: err.Error()}
	}
	var data map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &data)
	}
	if data == nil {
		data = map[string]any{}
	}
	if resp.StatusCode != http.StatusOK {
		code, _ := data["code"].(string)
		if code == "" {
			code = "wire-" + fmt.Sprint(resp.StatusCode)
		}
		detail, _ := data["detail"].(string)
		if detail == "" {
			detail = fmt.Sprintf("wire %d", resp.StatusCode)
		}
		return nil, &PeerRefusal{Code: code, Detail: detail}
	}
	return data, nil
}

// PeerGate — the A-side anchor gate (Node `gateAnchor` oracle):
// anchor parse + validate → approved peer (status 'approved') →
// direction outbound|both. Returns the errors as (status, message)
// pairs exactly as the Node controller renders them (400 invalid
// anchor / 404 peer not approved / 403 not outbound).
func PeerGate(store Store, anchorStr string) (peer *FederationPeer, anchor AnchorResult, status int, message string) {
	a := ParseAnchor(anchorStr)
	if a == nil {
		return nil, AnchorResult{}, 400, `invalid anchor: expected "<localName>:<origin>"`
	}
	v, err := ValidateAnchor(a.LocalName, a.Origin)
	if err != nil {
		return nil, AnchorResult{}, 400, err.Error()
	}
	anchor = v
	if store == nil {
		return nil, AnchorResult{}, 404, "peer not approved for this origin"
	}
	p, err := store.PeerByOrigin(contextBackground(), anchor.Origin)
	if err != nil || p == nil || p.Status != "approved" {
		return nil, AnchorResult{}, 404, "peer not approved for this origin"
	}
	if p.Direction != "outbound" && p.Direction != "both" {
		return nil, AnchorResult{}, 403, fmt.Sprintf("peer %s is not approved for outbound invites", anchor.Origin)
	}
	return p, anchor, 0, ""
}

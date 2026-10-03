// rp_state.go — S10 (A-side RP): the HMAC-signed state that binds the
// cross-origin round trip (overleaf-fed 01 §5 invite/authorize).
//
// The A side (this server) redirects the invited user's browser to B's
// authorization endpoint. The only cross-origin credentials it holds are
// (a) the HMAC-signed state below and (b) the PKCE verifier carried
// INSIDE that state — both travel with the browser, so ANY worker can
// complete the exchange without shared storage (multi-web safe).
//
// Wire shape (pinned, same "value.sig" family as the redis sign/verify
// helpers — core/redis.go:40):
//
//	b64url(JSON) + "." + b64url(HMAC-SHA256(key = site-scoped, msg = b64url(JSON)))
//
// TTL: 600 s (the invitation window, 01 §5).

package federation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
)

// RPState — signed state payload. Every field is public context except
// the verifier (the PKCE secret).
type RPState struct {
	Peer      string `json:"peer"`              // B's origin (host[:port])
	LocalName string `json:"local"`             // invited user's local part at B
	ProjectID string `json:"project,omitempty"` // A project to grant the mirror into
	Inviter   string `json:"inv,omitempty"`     // A-side user hex (addedBy)
	Nonce     string `json:"nonce"`             // bound to the id_token nonce
	Verifier  string `json:"iv"`                // PKCE code_verifier (SECRET)
	TS        int64  `json:"ts"`                // mint time, epoch seconds
	Exp       int64  `json:"exp"`               // hard expiry, epoch seconds
}

// RPStateTTL — 10-minute invitation window (01 §5).
const RPStateTTL = 600

func rpStateKey(site string) []byte {
	return []byte("overleaf-federation-rp-state:" + site)
}

// SignRPState — b64url(JSON) + "." + b64url(HMAC).
func SignRPState(site string, st *RPState) (string, error) {
	if st == nil || st.Peer == "" || st.LocalName == "" || st.Nonce == "" || st.Verifier == "" {
		return "", errors.New("federation rp: state missing peer/local/nonce/verifier")
	}
	b, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	msg := b64url.EncodeToString(b)
	mac := hmac.New(sha256.New, rpStateKey(site))
	mac.Write([]byte(msg))
	return msg + "." + b64url.EncodeToString(mac.Sum(nil)), nil
}

// VerifyRPState — parse, HMAC-verify (constant-time), TTL-gate.
//
// exp is STRICT (an expired invitation is dead); a state whose window
// extends more than 24 h into the future is rejected (mint-side bug
// guard — honest failure, not silent acceptance).
func VerifyRPState(site, signed string, now int64) (*RPState, error) {
	msg, sig, ok := strings.Cut(signed, ".")
	if !ok || msg == "" || sig == "" {
		return nil, errors.New("federation rp: malformed state")
	}
	raw, err := b64url.DecodeString(msg)
	if err != nil {
		return nil, errors.New("federation rp: state payload not b64url")
	}
	mac := hmac.New(sha256.New, rpStateKey(site))
	mac.Write([]byte(msg))
	want, err := b64url.DecodeString(sig)
	if err != nil || !hmac.Equal(want, mac.Sum(nil)) {
		return nil, errors.New("federation rp: state signature mismatch")
	}
	var st RPState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, errors.New("federation rp: state payload decode")
	}
	if st.Peer == "" || st.LocalName == "" || st.Nonce == "" || st.Verifier == "" {
		return nil, errors.New("federation rp: state fields missing")
	}
	if now > st.Exp {
		return nil, errors.New("federation rp: state expired")
	}
	if st.Exp > now+86400 {
		return nil, errors.New("federation rp: state window too large")
	}
	return &st, nil
}

// Log redaction for the federation module (Node `util/Redact.mjs`, 06 §6).
//
// Structured logs and audit must NEVER contain:
//   - `id_token` (token endpoint response)
//   - Client-assertion values (the audit row holds { iss, aud, jtiHash })
//   - The federation private-key material (private JWK `d`, PEM)
//   - `code` (OIDC code), `pat` (content-bridge export PAT)
//
// All helpers are pure and TOTAL (always return something usable) so a
// redaction bug can never 500 an audit/log write.
package federation

import (
	"crypto/sha256"
	"encoding/hex"
)

// AssertionMetaShape — the S2S assertion audit envelope (03 §6):
// ONLY { iss, aud, jtiHash }. The raw JWS is never logged.
type AssertionMetaShape struct {
	Iss     *string `json:"iss"`
	Aud     *string `json:"aud"`
	JtiHash *string `json:"jtiHash"`
}

// ClaimLogAllowlist — the claim allow-list a redaction step may surface in
// logs (04 §8 meta allow-list, 06 §8). Everything else on a claims object
// is dropped.
var ClaimLogAllowlist = []string{
	"sub", "origin", "localName", "displayName", "institution",
}

// privateJWKFields mark a JWK (or object holding one) as private (06 §6).
var privateJWKFields = []string{"d", "p", "q", "dp", "dq", "qi"}

// whole-object secret keys (redacted wholesale).
var secretWholeKeys = map[string]struct{}{
	"id_token":       {},
	"code":           {},
	"privatekey":     {},
	"private_key":    {},
	"secret":         {},
	"client_secret":  {},
	"access_token":   {},
	"encryptedtoken": {},
	"pat":            {},
}

// Redact returns a copy of value with known secret keys redacted. Never
// mutates the input.
func Redact(value map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range value {
		if isSecretKey(k, v) {
			out[k] = "[REDACTED]"
		} else {
			if m, ok := v.(map[string]any); ok {
				out[k] = Redact(m)
			} else {
				out[k] = v
			}
		}
	}
	return out
}

func isSecretKey(key string, v any) bool {
	if _, ok := secretWholeKeys[key]; ok {
		return true
	}
	s, isStr := v.(string)
	if !isStr {
		return false
	}
	for _, f := range privateJWKFields {
		if key == f && s != "" {
			return true
		}
	}
	return false
}

// PublicJwks reduces a JWKS doc to PUBLIC halves only. Any key carrying a
// private field is DROPPED entirely (safer than field-stripping: a future
// private field can't slip through a whitelist).
func PublicJwks(jwks map[string]any) map[string]any {
	keysIn, ok := jwks["keys"].([]any)
	if !ok {
		return jwks
	}
	keys := []any{}
	for _, k := range keysIn {
		kv, ok := k.(map[string]any)
		if !ok {
			continue
		}
		if hasPrivateField(kv) {
			continue // private key: drop, not redact
		}
		pub := map[string]any{}
		for _, field := range []string{"kty", "kid", "alg", "use", "crv", "x", "y", "n", "e"} {
			if val, present := kv[field]; present && val != nil {
				pub[field] = val
			}
		}
		keys = append(keys, pub)
	}
	return map[string]any{"keys": keys}
}

func hasPrivateField(kv map[string]any) bool {
	for _, f := range privateJWKFields {
		if v, present := kv[f]; present && v != nil {
			return true
		}
	}
	return false
}

// AssertionMeta computes the S2S client-assertion audit/log envelope
// (03 §6, 06 §6): ONLY { iss, aud, jtiHash } (sha256 32-hex of the raw
// jti); the raw JWS is never logged.
func AssertionMeta(iss, aud, jti string) AssertionMetaShape {
	var meta AssertionMetaShape
	if iss != "" {
		meta.Iss = &iss
	}
	if aud != "" {
		meta.Aud = &aud
	}
	if jti != "" {
		sum := sha256.Sum256([]byte(jti))
		hash := hex.EncodeToString(sum[:])[:32]
		meta.JtiHash = &hash
	}
	return meta
}

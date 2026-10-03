// exportttl.go — 2d (09 §3): the export credential TTL cap.
//
// Acceptance (e91d1eaf): `maxExportTtlSeconds HARD cap enforced
// (TTL = min(request, grant remaining, cap))`.
//
// Where it applies: every export credential (PAT / session token) minted
// under a FederationExportGrant. The S2S `export-project` mint leg is the
// S11 slice (f2 scope); this file pins the cap arithmetic now so the mint
// leg has exactly one source of truth. Cap source:
// Settings.ExportMaxTtlSeconds (env FEDERATION_EXPORT_MAX_TTL_SECONDS,
// default 86400; 0 = unset → no cap, legacy behavior).

package federation

import (
	"time"
)

// exportTTL — effective credential TTL in seconds:
//
//	TTL = min(requested, grant remaining, cap)
//
// Rules (all deterministic, unit-pinned):
//   - requested <= 0 → 0 (nothing was requested / malformed)
//   - cap <= 0 → no cap (unset = legacy behavior)
//   - grantExpiresAt zero → no existing grant (fresh issuance)
//   - grant remaining (grantExpiresAt - now) shorter than the rest →
//     remaining (a credential must not outlive its grant)
//   - remaining <= 0 (grant already expired) → 0 (nothing issuable)
func exportTTL(requestedSeconds, capSeconds int, grantExpiresAt, now time.Time) int {
	if requestedSeconds <= 0 {
		return 0
	}
	effective := requestedSeconds
	if capSeconds > 0 && effective > capSeconds {
		effective = capSeconds
	}
	if !grantExpiresAt.IsZero() {
		remaining := int(grantExpiresAt.Sub(now) / time.Second)
		if remaining < effective {
			effective = remaining
		}
	}
	if effective < 0 {
		effective = 0
	}
	return effective
}

// grantExpiryAt — B-side grant issuance (2a): the grant's own expiry =
// now + min(requested, cap) (a new grant has no "remaining" to shorten it).
// Returns zero for unusable inputs (requested <= 0) — the caller must not
// persist such a grant.
func grantExpiryAt(requestedSeconds, capSeconds int, now time.Time) time.Time {
	ttl := 0
	if requestedSeconds > 0 {
		ttl = requestedSeconds
	}
	if capSeconds > 0 && ttl > capSeconds {
		ttl = capSeconds
	}
	if ttl <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(ttl) * time.Second)
}

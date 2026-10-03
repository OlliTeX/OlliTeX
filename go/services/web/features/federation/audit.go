// Audit wrappers for the federation module (Node `util/Audit.mjs`, 04 §8,
// 03 §6).
//
// The existing `projectAuditLogEntries` collection is reused UNCHANGED as
// the storage vehicle (04 §8); `operation` (a free-form String) carries
// the `federated_*` / `federation_*` machine type and `info` carries the
// allow-listed `meta`.
//
// NEVER in `info` (04 §8, 06 §6): JWS raw, key material, password hashes,
// project bytes, any claim beyond `displayName`. The S2S assertion rides
// as `{ iss, aud, jtiHash }` (hashed, 03 §6) — built by Redact.AssertionMeta.
package federation

import (
	"context"
	"log/slog"
)

// metaFields — 04 §8 meta allow-list (06 §8). `assertion` is the special
// S2S slot (hashed, 03 §6).
var metaFields = []string{
	"origin", "localName", "displayName", "kid", "anchorThumbprint",
	"registrationJtiHash", "direction", "assertion", "reason",
	// content-bridge v2 (plan 09 §3): scope marker + exported facts.
	"scope", "gitUrl", "expiresAt",
}

// AuditTypes — machine types (04 §8 v2 list).
var AuditTypes = struct {
	// peer-level
	PeerRegistered    string
	PeerApproved      string
	PeerRevoked       string
	TrustAnchorPinned string
	TrustRevoked      string
	KeyRotated        string
	PeerDenied        string
	// project-scoped
	InviteApproved string
	InviteDenied   string
	SessionIssued  string
	// content-bridge v2 (plan 09 §3)
	ExportGranted   string
	ExportDenied    string
	ExportRequested string
	ExportSwept     string
}{
	PeerRegistered:    "federation_peer_registered",
	PeerApproved:      "federation_peer_approved",
	PeerRevoked:       "federation_peer_revoked",
	TrustAnchorPinned: "federation_trust_anchor_pinned",
	TrustRevoked:      "federation_peer_trust_revoked",
	KeyRotated:        "federation_key_rotated",
	PeerDenied:        "federation_peer_denied",
	InviteApproved:    "federated_invite_approved",
	InviteDenied:      "federated_invite_denied",
	SessionIssued:     "federation_session_issued",
	ExportGranted:     "federation_export_granted",
	ExportDenied:      "federation_export_denied",
	ExportRequested:   "federation_export_requested",
	ExportSwept:       "federation_export_swept",
}

// AuditWriter — the Mongo seam for audit rows. Production wires a
// `*core.App` Mongo-backed impl (S2); tests wire a recorder. The write is
// FIRE-AND-FORGET from the caller's point of view: a write failure is
// logged, never thrown (06 §7: an audit bug must not brick an S2S receipt
// or an admin action).
type AuditWriter interface {
	// WriteAudit persists one row. `operation` = the machine type;
	// `info` = the allow-listed meta (already filtered by the caller via
	// FilterMeta). `projectId` may be nil (peer-level rows).
	// `initiatorId` = authenticated session user hex (or ""); `ipAddress`
	// = req remote IP (or "").
	WriteAudit(ctx context.Context, operation string, projectId *string, info map[string]any, initiatorId, ipAddress string) error
}

// FilterMeta projects `meta` down to the 04 §8 allow-list (unknown keys
// dropped).
func FilterMeta(meta map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range metaFields {
		if v, ok := meta[key]; ok && v != nil {
			out[key] = v
		}
	}
	return out
}

// Audit — write one federation audit row (04 §8). Return values:
// (false, nil) when an optional write was skipped (nil writer); the writer
// error is logged and swallowed per 06 §7.
func Audit(log *slog.Logger, w AuditWriter, operation string, meta map[string]any, initiatorId, ipAddress string) {
	if w == nil {
		return
	}
	info := FilterMeta(meta)
	if log != nil {
		log.Info("federation audit", "operation", operation)
	}
	if err := w.WriteAudit(context.Background(), operation, nil, info, initiatorId, ipAddress); err != nil && log != nil {
		log.Error("federation: audit write failed", "operation", operation, "err", err)
	}
}

// AuditProject — project-scoped rows (federation_invite_*,
// federation_export_*): like Audit, but carries the optional projectId
// (Node ProjectAuditLogEntry.projectId is an optional indexed ObjectId).
func AuditProject(log *slog.Logger, w AuditWriter, operation string, meta map[string]any, projectId *string, initiatorId, ipAddress string) {
	if w == nil {
		return
	}
	info := FilterMeta(meta)
	if log != nil {
		log.Info("federation audit", "operation", operation)
	}
	if err := w.WriteAudit(context.Background(), operation, projectId, info, initiatorId, ipAddress); err != nil && log != nil {
		log.Error("federation: audit write failed", "operation", operation, "err", err)
	}
}

{
  "id": "fbdcc07d",
  "title": "fedgap-4 Federation S2S inbound + admin + A-side invite (Go has data+OP only; S2S/admin/invite behavior LAYER MISSING)",
  "tags": [
    "fedgap-4",
    "federation",
    "s2s",
    "admin",
    "invite",
    "parity",
    "owner-queue"
  ],
  "status": "done",
  "created_at": 1790888711785,
  "last_change": 1791001018762,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

CORE SURFACE DONE 2026-10-02 — commit d899e59f3a: 13 /admin/federation routes (peer CRUD: list/pin/approve/deny/revoke, keys list+rotate(oidc→501), audit, trust-anchors list/pin/delete, /admin/federation → 302 /hub#/admin.federation, wizard readiness JSON) + POST /federation/s2s always-mounted (fed-off envelope 200; on: 400 bad-envelope/unknown-action; action registry pinned) + A-side user routes (/api/federation/invite/preview+authorize, /federation/export GET+POST, /federation/oidc/rp/callback). Real Store reads (MongoStore: AllPeers/AllKeys/AllTrustAnchors/PeerByOrigin/SavePeer/RevokePeer/ActiveKey); 404 peer-unknown, 409 peer-not-pending, 204 deny/anchor-delete. fedgap4_test.go 4/4; web 37/37 green.
REMAINING (honestly reported as pending codes in handlers): (1) institutional TOFU chain resolve on peer PIN (discoverEntity vs trust-anchor set), (2) S2S action dispatch (authorize-invite/invited/revoke/export-project full flows) — needs code exchange (exchange mjs), State HMAC, mirror mint (req.session.user direct, LOCKED decision 11), grant via collaborator; (3) RP callback success path (code exchange → mirror session → 302 project). These depend on the A↔B full round-trip which needs a second live instance (f2 scope) — unit-level slices can land ahead of f2. f2 dual-instance E2E still the proof step.

## CLOSED 2026-10-03 (autonomous, S5 slice delivered — commit b63708f653)
**S5 dispatch pipeline LANDING** (the "REMAINING (2) S2S action dispatch" item):
- **s2s.go**: `client_assertion` header (LOCKED name) required on known actions —
  the `action-pending` stub pin is RETIRED (fedgap4_test pin updated; known-action
  no-assertion → 401 bad-signature). `payload` envelope alias accepted (09 §9 table
  vs 03 §2 pin — lenient where the two in-tree pins disagree).
- **s2s_actions.go** (new): the full LOCKED 03 §6 pipeline —
  ③ KeyProvider.VerifyS2sClientAssertion (401 machine codes; known-but-not-approved
  peer → `peer-not-approved` refinement) → ④ ClaimJti replay (401 replay-jti) →
  ⑤ CheckRateLimit (429 + Allow-Retry-After header, code rate-limited) →
  ⑥ action dispatch (200 {ok,code,detail?,payload?}) — + fire-and-forget audit
  rows (04 §8; assertion meta locked to {iss,aud,jtiHash} via AssertionMeta, 03 §6).
- **Action legs** (depth = what this tree can prove, honest codes otherwise):
  - `revoke`: FULL sweep — client codes (S4a OidcAdapter.RevokeClientCodes),
    active export grants → revoked, best-effort PAT deleteOne with the LOCKED
    `federation:git_bridge` scope guard, honest counts {codesSwept, grantsRevoked},
    ExportSwept project-scoped audit row.
  - `authorize-invite`: LOCKED resolveAnchorUser (mirror `federation.{origin,localName}`
    → local email → unknown; suspended ⇒ invitee-disabled) + cached-invite marker
    for A-side polling (04 §6) + InviteApproved/InviteDenied audit rows (reason audited).
  - `invited` / `export-project`: honest `s11-pending` (mint engine = f2 scope) —
    export-project answers `export-disabled` + ExportDenied row when the gate is off,
    audits ExportRequested otherwise.
- **audit.go**: +`AuditProject` (project-scoped rows; existing projectAuditLogEntries
  vehicle, Node ProjectAuditLogEntry field shape, 04 §8).
- **Tests** (s2s_actions_test.go, 8): verify+dispatch+replay round trip, 6th revoke
  → 429 + Allow-Retry-After, pending-peer → peer-not-approved, missing assertion →
  bad-signature, revoke sweep (grant revoked + PAT swept + ExportSwept row),
  authorize-invite legs ×3 (mirror/suspended/unknown; cached marker + audit),
  export gate legs ×2, wired-handler 401 + payload alias.
- Gates: federation package **44/44 green**; `go build ./...` + `go vet` clean
  (whole repo builds). sso TestClassifyCertExpiry failure = PRE-EXISTING wall-clock
  flake (time.Until vs fixed now), untouched package — noted, not mine.

**CARRY-FORWARD (owned elsewhere, not silently dropped):**
- (1) institutional TOFU chain resolve on peer PIN + (3) RP-callback mint
  (code exchange → mirror session) = f2 dual-instance scope → e91d1eaf acceptance
  ("2-origin live smoke"); the invited-mint engine rides with it.
- export-project transfer + git-bridge 403 guard + sweep caps = e91d1eaf (fedgap-5).
- 6a configschema knob still the open question logged (owner one-word: wanted or not).


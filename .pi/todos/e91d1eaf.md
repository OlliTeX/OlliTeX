{
  "id": "e91d1eaf",
  "title": "fedgap-5 Content-bridge 2a-2d (federated project export/import) + git-bridge read-only PAT guard + sweep",
  "tags": [
    "fedgap-5",
    "content-bridge",
    "export",
    "gitbridge",
    "owner-queue",
    "depends-fedgap-4"
  ],
  "status": "done",
  "created_at": 1790888711785,
  "last_change": 1791001805567,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

fed-gaps: 5 Content-bridge (plan 09). LARGEST surface; depends on fedgap-4 (export needs S2S+grants)

## Node contract (modules/federation/export/** + invite/FederatedExport* + git-bridge)
### 2a  B-side export-project S2S
- FederationExportGrant (account, client) grant index — Go model EXISTS but unused; add the S2S action + grant lifecycle
### 2b  A-side export wizard
- invite/FederatedExportController.mjs + FederatedExportRouter.mjs (per-user auth'd wizard API)
### 2c  git-bridge read-only 403 guard
- GitBridgeAuthMiddleware: on WRITE (receive-pack push -> snapshot postback) resolve token WITH scope; if scope startsWith 'federation:' -> 403 (read-only export PAT) WITHOUT importing federation module
- GitBridgePATManager.getUserIdAndScope(token) (mirror of getUserId); PAT regex /\bgit_bridge\b/ matches both `git_bridge` + `federation:git_bridge` -> must distinguish
### 2d  cross-origin order + sweep
- dance->export->PAT 403 ordering; killOutstandingCodes 'export' side (sweepOnRevoke); export sweep on revoke
- settings: export.{enabled (B-side gate), maxExportTtlSeconds (HARD cap), sweepOnRevoke (2c, default true)}

## Go state (verified): go/services/gitbridge has NO scope-aware auth (no getUserIdAndScope / no 403 guard); no Sweep; FederationExportGrant model exists unused

## Acceptance
- export-project S2S green (2-origin live smoke like Node tools/two-origin/live-smoke-b.mjs)
- A-side wizard API parity
- git-bridge: federation:git_bridge PAT read GET works, write push -> 403 (unit + live)
- sweep-on-revoke kills outstanding codes + grants
- maxExportTtlSeconds HARD cap enforced (TTL = min(request, grant remaining, cap))

## CLOSED 2026-10-03 (autonomous) — 2c + 2d delivered unit-green; 2a/2b/f2-live carved to TODO-5f48f0cb
- **2c git-bridge read-only 403 guard** (commit 43dae993db) — Node
  GitBridgeAuthMiddleware parity, WITHOUT importing the federation module:
  - web `/oauth/token/info` now answers 200 {user_id, scope} (PAT query regex
    /\bgit_bridge\b/ already matched both scopes; the scope was just never
    surfaced). `pats.tokenResolved` = shared lookup; `userIDForToken`
    semantics unchanged.
  - gitbridge service: `OAuthScopedClient` seam + `NewServerWithScopedSeams`;
    `handleOauth2` step 6 captures the scope → `handleGitServlet` →
    `readonlyExportWrite(route, scope)` fires the 403 guard BEFORE repo
    resolve (receive-pack adv + push only; upload-pack reads pass; legacy
    wiring/scoped-"" = guard off = old behavior).
  - guard_test.go: predicate matrix (8) + full-flow write-denied (post-push +
    receive-adv) + 4xx branch parity (429/token_expired/500) + plain-client
    fallback. gitbridge tree 15/15 pkgs green; whole-repo build + vet clean.
- **2d cap** (commit 18b7474f76): `exportTTL(requested, cap, grantExpiresAt, now)`
  = min(request, grant remaining, cap); lapsed grant → 0; cap 0 = unset (no cap).
  `grantExpiryAt` for B-side grant issuance (now + min(request, cap)); MapStore
  round-trip pinned. Cap source: `Settings.ExportMaxTtlSeconds`
  (FEDERATION_EXPORT_MAX_TTL_SECONDS, default 86400).
- **2d sweep**: already delivered in fedgap-4's revoke leg (S5 pipeline,
  b63708f653) — RevokeClientCodes + active→revoked grants + scope-guarded PAT.
- **CARRY-FORWARD → TODO-5f48f0cb (federation f2)**: 2a S2S mint (grant/code/PAT
  issuance — consumes the 18b7474f76 helpers), 2b wizard fill-in, invited-mint
  engine, RP-callback success path, institutional TOFU, the 2-origin live smoke
  (incl. the LIVE 403 leg of 2c). f2 requires the second live instance — SAFE
  fixture build (fresh port + separate DB); shared-live-config seeding =
  owner-gated.


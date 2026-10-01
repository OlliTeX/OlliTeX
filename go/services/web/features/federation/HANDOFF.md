# Federation — Go port HANDOFF / Living Task List

> **This file is the takeover point.** It is ALWAYS updated: read ① this file,
> ② check the `git log` for this package, ③ the "Current position" at the
> bottom. A new session must be able to `git status`-clean resume from here
> without re-doing any completed slice.

**Goal** (owner 2026-09-26): convert
`~/federation/overleaf-fed/services/web/modules/federation`
(from commit `239986621531ef579386b0ac5c4d3cb41c9d1c96`, i.e. *including*
everything on top of it at overleaf-fed `HEAD`) into a 1:1 (+ bug fixes) Go
package at `~/federation/OlliTeX/go/services/web/features/federation`, AND
integrate the SAML/OIDC (SSO) changes those commits carry into OlliTeX's Go
web stack. The Node module in overleaf-fed is the **spec**; overleaf-fed's
`plan/*.md` + `FINDINGS.md` are the design authority (they override Go
comments when they conflict).

## 0. Sources of truth

| What | Where |
|---|---|
| Node module (spec, 5,712 LOC prod + 16 test files, 4 pug views) | `~/federation/overleaf-fed/services/web/modules/federation/` at overleaf-fed `HEAD` |
| Design authority (wins over code comments) | `.../federation/plan/` (00–11) + `FINDINGS.md` (npm v1.0.0 surface pins) + `README.md` (route inventory) |
| SSO (SAML/OIDC) Node modules, same delta | `~/federation/overleaf-fed/services/web/modules/authentication/{saml,oidc,admin,test}` + `app/src/Features/Authentication/{ssoRoleEvaluator,AuthenticationController,AuthenticationManager}` + `app/src/Features/Project/ProjectCreationHandler` |
| OlliTeX Go framework contracts | `go/services/web/README.md`, `go/services/web/core/README.md` (route table, NoSession/NoLogin/APIOnly flags, `core.Cxt`/`core.Res`, `core.App`) |
| OlliTeX env contract | `core/config.go` (`LoadConfig` reads the same env names as Node) |

**Baseline commit**: `239986621531ef579386b0ac5c4d3cb41c9d1c96`
("Backup federation design documents", 2026-09-18). Everything in
`2399866..overleaf-fed HEAD` (40 commits, ~24k insertions) is in scope:
P0 keystore bootstrap through V2 content-bridge 2d, V1-SSO
(P1a DB-backed SSO framework, P1b N-provider, P1c attrFilter roles,
R1 synthetic-email JIT), Plan 11 SAML SP metadata (S17), Phase-4 hardening
(cert-expiry sweep, no-cross-linking audit, SAML metadata probe S19),
export 2a–2d, all live-smoke catches.

## 1. Scope map (Node file → Go package)

Target layout (mirrors Node module; one Go package `federation`):

```
go/services/web/features/federation/
  federation.go          Feature(a) core.Feature  (all route registrations + gates)
  config.go              FEDERATION_* env knobs → Settings struct (settings.defaults.js `federation` block)
  models.go              Mongo docs: federationKeys / federationPeers / federationTrustAnchors / federationExportGrants + User.federation + ProjectInvite.federated
  keystore.go            oidf/keystore: bootstrap, rotation state machine, grace sweep, jwks payloads
  anchor.go              util/Anchor: parse/format/validate anchor, salted hash, invitee hash
  redact.go              util/Redact: redact/publicJwks/assertionMeta (pure)
  audit.go               util/Audit: fire-and-forget audit rows (reuses projectauditlogentries)
  ratelimit.go           util/RateLimitStore: INCR+EXPIRE budgets (pure-logic over a small Redis seam)
  leaf.go                oidf/leaf: entity id, leaf EC build (ES256 self-signed JWT), GET /.well-known/openid-federation
  clientassertion.go     oidf/ClientAssertionClient + verify.mjs: create/verify client assertion (jose), replay dedup
  oidcprovider.go        oidc-provider v9 equivalent: /federation/oidc auth/token/jwks/resume + interaction (see §3 decision D-OP)
  s2s.go                 s2s/S2sRouter: POST /federation/s2s (envelope 401/429/200)
  s2s_actions.go         s2s/actions/*: invited, authorize-invite, revoke, export-project
  rp.go                  rp/CallbackRouter + CodeExchange + State (HMAC-signed state, PKCE, mirror row, grant)
  invite.go              invite/FederatedInviteRouter+Controller (preview + authorize)
  export_wizard.go       invite/FederatedExportRouter+Controller (A-side wizard)
  export_sweep.go        export/Sweep
  admin.go               admin/AdminRouter + FederationAdminController (peers/keys/TAs/audit)
  wizard.go              admin/FederationWizardController
  views.go               4 HTML views (consent, admin dashboard, export form, export result) — Go-rendered, byte-parity with pug output
  projectguard.go        app/ProjectCreationGuard (B-side gate on POST /project/new)
  _test.go files         per-file unit tests mirroring Node test/unit + contract tests
```

The **OIDC provider** (`oidc-provider` v9 — `createProvider.mjs`,
`RedisOidcProviderAdapter.mjs`, `clients.mjs`, `bridge.mjs`) is a big
decision: see **Decision D-OP** in §3 (needs owner sign-off before
Slice 4 starts; everything else is unblocked).

### SSO (SAML/OIDC) delta to integrate (commit range on `authentication/`)

Node files changed by `2399866..HEAD` **outside** the federation module —
these are OlliTeX Node-side files that must be reconciled into Go
(OlliTeX's own Node `ssoConfigLoader.mjs` is the D7 **stored site-settings
sections** variant, different shape from overleaf-fed's DB
`sso-settings` doc + env-fallback loader — the Go port should key off the
DB `ssoConfigs` model, which both repos share):

| Node surface | Go landing spot | Slice |
|---|---|---|
| `ssoConfigLoader` (N-provider, `getProviderById`, `getEnabledProviders`) | `features/federation/sso_config.go` (or its own `features/ssoconfig`) | S7 |
| `ssoRoleEvaluator` (attrFilter local/guest/blocked + audit denied) | `ssorole.go` (pure, easy) | S6 |
| `ssoCertExpiry` (X509 notAfter parse + boot sweep) | `certexpiry.go` (pure + sweep) | S7 |
| `samlMetadataProbe` (fetch + XML-DSig verify + cert extract/pin) | `samlprobe.go` — **needs a Go XML-DSig impl (see D-XMLDSIG)** | S8 |
| `SSOAdminController/Router` + `sso-admin.pug` (provider CRUD, attrFilter UI, keys, meta probe) | `ssoadmin.go` + `ssoadmin_view` | S7 |
| SAML module (SAMLController/Manager/ModuleManager/Routers) | `saml.go` — passport-saml equivalent (see D-SAML) | S8 |
| OIDC module (OIDCController/Manager/ModuleManager/Routers) | `oidc_sso.go` — stock OIDC login | S9 |
| R1 synthetic-email JIT (SAML+OIDC managers, env-gated) | folds into S8/S9 (JIT `<userpart>@<domain>` when no email claim) | S8/S9 |
| `login.pug` SSO button rendering (136 diff) | `go/services/web/views/pages_data*.go` slots | S7 |
| `ProjectCreationHandler` ssoGuestGuard (guest cannot create) | hook from `features/federation` into `projectlist` create path | S6 |
| git-bridge PAT scope (`getUserIdAndScope`, `federation:` 403 guard) | `features/gitbridge` | S5 |
| migrations: trust-anchor index, export-grant indexes | none (Go creates indexes at boot like Node autoIndex off) | S0 |

## 2. Slice plan (green-slice discipline per slice)

Gate per slice: `go build ./go/... && go vet ./go/services/web/... &&
gofmt -l <dirs> == 0 && go test -race <pkg>` green, HANDOFF `Progress Log`
updated **before** moving on. Never stage unrelated files.

| # | Slice | Contents | Status |
|---|---|---|---|
| S0 | Package scaffold | `federation.go` Feature stub + `HANDOFF.md` + README; compiles, registered in `cmd/web/main.go` **behind the `federation` config gate (default off)** | ✅ 794771a |
| S1 | `util/` pure core | `anchor.go` `redact.go` `ratelimit.go` `audit.go` (audit over an injected seam); unit tests pinning Node behavior incl. `sha256:`/HMAC-32-hex forms | ✅ 794771a |
| S2 | Models | `models.go` (4 collections: Keys/Peers/TrustAnchors/ExportGrants, epoch-sec + Date-oracle fields), `models_store.go` (MapStore + MongoStore + `EnsureIndexes` 6-boot incl. partial-unique users mirror index, 04 §9), `errors.go` (Errorf variadic, ErrNotFound=mongo.ErrNoDocuments) | ✅ 7c1f630 |
| S3 | Keystore + leaf | `jws.go` (ES256 Compact JWS, RFC 8550 kid thumbprint, r‖s 64-byte, kid pre-check, UUIDv4), `leaf.go` (entity id/origin/client id/S2S endpoint/OIDC endpoints), `clientassertion.go` (KeyProvider: bootstrap/active-key/leaf-EC builder pairwise/HistoricalKeySetPayload/BuildS2sRequest/VerifyS2sClientAssertion); oracle-pinned against npm @oidfed/core v1.0.0 fixtures | ✅ e141548 |
| S4 | **D-OP** (DECIDE'd: port the v9 subset, owner Q2) | B-side OIDC provider. **S4a** (done, ddf3b4c1): `oidcadapter.go` 7-method adapter + `RevokeClientCodes` (04 §5 killOutstandingCodes sweep) + `FindByAccountAndClient` (content-bridge v2) + `core.RedisClient` SETNX/SETEX/SETPX/SADD/SREM/SMEMBERS/SCARD/PTTL. **S4b-1** (done, aa21e4a0): `oidcprovider.go` clients[] rebuild (clients.mjs oracle: approved peers, direction NOT filtered, scope STRING) + `/jwks` (oidc-purpose only) + discovery metadata + `s4Routes` (2 NoLogin routes). **S4b-2/3** (in progress): `oidcengine.go` engine — auth/interaction/resume/token (see session-5 entry for the full pinned contract list + draft state) | 🟨 S4b-2/3 |
| S5 | S2S | `s2s.go` + 4 actions (invited/authorize/revoke/export-project), 401/429/200 envelope, rate limits, audit rows; git-bridge PAT scope guard in `features/gitbridge` | ⬜ |
| S6 | SSO role + project guard (Go seam) | `ssorole.go` (attrFilter evaluator + audit denied), `projectguard.go` (B-side `allowFederatedProjectCreate` + guest-create refusal on POST /project/new) | ⬜ |
| S7 | SSO admin + config + cert-expiry + login slots (Go) | `sso_config.go` (N-provider id resolution), `ssoadmin.go` + admin view, `certexpiry.go` X509 notAfter sweep, login-page SSO slots | ⬜ |
| S8 | SAML — **Node-sync** (overleaf-fed → OlliTeX) + Go seam | into OlliTeX Node: N-provider lazy registration, `samlMetadataProbe`, `/saml/meta` SP metadata (plan 11), R1 synthetic-email JIT; Go seam: synthetic-email flag on project-create guard | ⬜ |
| S9 | OIDC stock-SSO — **Node-sync** + Go seam | into OlliTeX Node: N-provider `attAdmin` non-standard claims; Go seam: sso-admin OIDC row parity | ⬜ |
| S10 | RP (A-side) | `rp.go`: redirect/state/PKCE + callback + CodeExchange (fetch B token endpoint, verify vs B `/federation/oidc/jwks`, mirror row, grant, 302) | ⬜ |
| S11 | Invite + export wizard | `invite.go` (preview + authorize, S2S outbound via client assertion), `export_wizard.go`, `export_sweep.go` | ⬜ |
| S12 | Admin surface | `admin.go` (peer
pin/approve/deny/revoke incl. institutional TA chain, key rotate, audit GET), `wizard.go` readiness probe, `views.go` admin dashboard | ⬜ |
| S13 | Integration + live smoke | two-instance Go test (real OIDC dance + S2S round-trip, mirror Node `test/unit/integration`), live-smoke harness equivalent, e2e parity gate wiring | ⬜ |
| S14 | Flip (owner-gated) | nginx flip of served federation surfaces Node→Go; e2e parity battery; junk retired Node surfaces | ⬜ |
| M0–M6 | **D-GOFED migration** (DECIDE'd session 6) | OIDF trust layer: hand-rolled → `go-oidfed/lib` v0.11.x (M0 spike, M1 kid pin, M2 keystore→lib KMS, M3 leaf EC, M4 S2S assertion claims (D-G4), M5 historical keys `SignedJWKS`, M6 flip+delete). Full plan: `MIGRATION-GOFED.md`. Runs AFTER S4b-2/3 baseline lands | ⬜ |

**Recommended order for a fresh VRAM-constrained session**: S0 → S1 → S2 →
S3 (all self-contained, high value, zero external-infra); then D-OP call;
then S5 (S2S is the module's spine and unblocks S3's verify tests); S6 →
S7 (SSO admin) → S10 → S11 → S12 → S8 → S9 (SAML/OIDC need live IdP for
e2e; unit-testable first) → S13.

**Owner decisions (2026-09-26, this session):**
- **Q1 → S8/S9 = Node-sync + Go-parity, NOT a Go SAML/OIDC port.**
  overleaf-fed's SAML/OIDC improvements (N-provider, attrFilter, JIT,
  SP metadata, cert-expiry, probe, SSO admin UI, login-button slots) sync
  into OlliTeX's own Node modules (kept-alive per D1 scope); Go carries
  only the parity bits its served surfaces need (login-page SSO slots,
  sso-admin row parity, synthetic-email seam). Go does NOT serve
  `/saml/*`, `/oidc/*`, or SLO in this arc.
- **Q2 → D-OP = port the v9 subset into Go** (`oidcprovider.go`):
  auth/token/jwks/resume/interaction + 7-method Redis adapter + Grant/PKCE,
  oracle-pinned against `oidc-provider` v9.12.2. No maintained Go OIDC-OP
  library matches v9's Grant/interaction model, so a bespoke subset port is
  the path (S4 in scope).

## 3. Decisions

### D-OP (NEEDS OWNER) — the B-side OIDC provider

The Node module embeds `oidc-provider` v9.12.2 (a Koa app: auth, token,
jwks, resume, interaction, grants with Redis adapter, PKCE, scopes/claims).
Go options:
1. **Port the required v9 subset** into `oidcprovider.go` — full 1:1 control
   (adapter TTLs, `findAccount`, interactions), ~1–2k LOC; highest
   oracle-pinning fidelity; most work.
2. Adopt a Go OIDC **provider** lib (e.g. `go-oauth2/oauth2/v4` has no OP;
   `oidc-provider` port is bespoke) — no maintained Go OIDC-OP lib exists
   that matches v9's interaction/Grant model; expect bespoke glue either way.
**Leaning: option 1** (subset port, oracle-pinned against Node provider),
because the whole v1 design (client assertions, mirror accounts, consent
bridge, export grants) is written against v9's exact Grant/interaction
semantics and there is no Go equivalent to swap in. The Redis 7-method
adapter + sub-indexes (`federation:oidc:account:*`) must be reproduced 1:1
since S2S export-consent and no-re-consent build on them.

### D-GOFED (DECIDE'd session 6) — OIDF trust layer: hand-rolled → `go-oidfed/lib`

Owner: "not a scope extension" — replace the handmade OIDF part with the
community-maintained Go packages so we are NOT spinning crypto ourselves.
Full plan: **`MIGRATION-GOFED.md`** this directory (M0–M6 slices, dep delta,
wire-verification table, decision pins D-G1..D-G6, ~4.75 sessions est.).

- Adopt **only** `github.com/go-oidfed/lib` (MIT, v0.11.x, OIDF **Final 1.0**
  wire verified in lib source 2026-09-26: `openid_provider`,
  `federation_fetch_endpoint`, `entity-statement+jwt`, `SignedJWKS` §5.2.1,
  `oidfedconst` content types — matches our npm @oidfed/core v1.0.0 oracle).
- **Not** adopted (surveyed, wrong shape): `offa` (forward-auth proxy),
  `lighthouse` (TA/IA tool, 56k LOC), `resolve-browser` (dev tool). All three
  cloned at `~/federation/{offa,lighthouse,resolve-browser}` if ever needed.
- Re-land: S3's `jws.go`/`leaf.go` keystore half/`clientassertion.go` → lib
  (M2–M5); **S4 B-side OP engine stays hand-rolled** (lib is RP/TA-side, not
  an OIDC provider); S2S actions (S5) + SSO (S6–S9) untouched; S10 RP
  fetch/verify can later use lib `ResolveOPMetadata` (optional, low value —
  we fetch B's OIDC JWKS, not OIDF metadata).
- Open pins (see MIGRATION-GOFED §6): D-G1 kid byte-equality (M1 gate),
  D-G4 S2S assertion claim set (default: overleaf-spec wins), D-G5 lib pin
  (v0.9 tags may predate RC3→Final renames; default master), D-G6 dep-closure
  approval (jwx v4, resty, zerolog, gocache).
- Migration runs AFTER the current S4b-2/3 engine lands (keeps a green
  baseline); no file is deleted before M6.
- **M0 DONE (d6ac2a9e)**: dep pinned (pseudo v0.11.3-0.20260831 @ 3130444),
  fiber NOT in build graph, KMS seam shape + lib assertion/EC round-trip
  green (`libspike_m0_test.go`). **M1 DONE**: D-G1 CLOSED — lib kid == npm
  fixture kid three-way (`libm1_kid_test.go`). **M2 REDIRECTED (D-G7)**:
  Node keystore KEPT (lib kms is a different state machine); lib supplies
  the artifact layer (kid M1, EC signing M3, historical JWKS M5).

### D-XMLDSIG (open) — SAML metadata probe (S19)

`samlMetadataProbe.mjs` verifies XML-DSig `<ds:Signature>` over the
metadata and extracts the signing KeyDescriptor cert. Go has `xml.Signature`
only for *verification via a trusted key* — no full XML-DSig signature
envelope handling in stdlib. Options: hand-roll the small X.509 cert-extract
first (S8 phase-1, covers the "extract + pin" 80%), full DSig verify as a
phase-2 follow-up (possibly a thin wrapper over the Node tool for ops, or a
Go dep). **Not blocking** the rest of S8.

### D-SAML (open) — SAML 2.0 SP

`@node-saml/passport-saml` v5 (SAML metadata parsing, assertion decrypt/
verify, NameID, SLO) has **no Go equivalent** in the offline module cache.
Port scope: metadata XML parse (own + remote IdP), ACS POST assertion
decrypt (EncryptedAssertion, AES-128/256-CBC + SHA1/256 HMAC), NameID
parsing, attribute mapping, SLO logout (SOAP SLO). Est. 1.5–2.5k LOC.
This is the single largest SSO piece. Can ship **after** S7 with the Node
stack kept for live IdP flows until e2e-green.

### ENV knobs (config.go additions, from `settings.defaults.js` `federation`)

```
FEDERATION_ENABLED (default off — mirrors Node default)
FEDERATION_ALLOW_FEDERATED_PROJECT_CREATE (false)
FEDERATION_REQUIRE_ADMIN_APPROVAL (true)
FEDERATION_KEY_ROTATION_GRACE_DAYS (14)
FEDERATION_INSTITUTION_ID (""→nil)
FEDERATION_INSTITUTION_AUTHORITY_HINTS (comma list)
FEDERATION_S2S_FETCH_TIMEOUT_MS (10000)
FEDERATION_TOKEN_FETCH_TIMEOUT_MS (30000)
FEDERATION_JWKS_FETCH_TIMEOUT_MS (5000)
FEDERATION_EXPORT_ENABLED (false)
FEDERATION_EXPORT_MAX_TTL_SECONDS (86400)
FEDERATION_EXPORT_SWEEP_ON_REVOKE (true)
# SSO (overleaf-fed plan/10 R1):
OVERLEAF_SAML_SYNTHETIC_EMAIL_DOMAIN (""→ host of SITE_URL)
SSO_CERT_EXPIRY_WARN_DAYS (30)
```

### LOCKED wire contracts (from overleaf-fed HANDOFF §9 LOCKED DECISIONS — do NOT deviate)

- S2S envelope: `client_assertion` header (JWT client assertion) + JSON body
  `{ action, from, to, ts, payload }`; **401** for assertion-level failures
  (`bad-signature | unknown-kid | peer-unknown | peer-not-approved |
  replay-jti | timestamp-skew`); **429** + `Allow-Retry-After` (code
  `rate-limited`); **200** + `{ ok, payload?, code?, detail? }` for business
  (`federation-off` when gated off — *router always mounted*).
- S2S action rate budgets: authorize-invite/invited **30/120s** keyed
  `(caller, saltedLocalNameHash)`; revoke **5/1200s** keyed `(caller)`;
  export-project **10/120s** keyed `(caller, projectId)`.
- Client id: `urn:overleaf-federation:client:<origin>` (derived from the
  *approved peer's* origin, never body field).
- Key state machine: `published → active → retiring → revoked`; grace
  `keyRotationGraceDays` (14); leaf TTL 48h; two purposes: `federation`
  (signs leaf EC + client assertions) and `oidc` (signs id_tokens).
- Anchor tuple `(origin, localName)`; display string `localName:origin`
  split on **last** colon; `localName` may contain `@`, **never** `:`;
  FQDN regex on origin (see `anchor.go` test pins).
- Audit rows reuse **projectauditlogentries** collection (Node
  `ProjectAuditLogEntry`), free-form `operation` = the `federation_*` /
  `federated_*` type string, `info` allow-list (04 §8), assertion meta
  **hashed** (`jtiHash` sha256-32-hex), fire-and-forget (a write failure
  logs, does not throw).
- Identity claims (id_token): `sub, origin, localName, displayName,
  institution` (institution nullable). Mirror row = `User.federation`
  subdoc presence (NO `kind` field).
- `resolveAnchorUser`: mirror match `federation.{origin,localName}` → else
  local `User.email == localName` → else `{ ok:false, code:'invitee-unknown' }`.
  Suspended user ⇒ `invitee-disabled`.

### Bug fixes already in Node (1:1 — port them, don't re-litigate)

- verify.mjs: `iss` checked against peer's *pinned* origin-derived client id
  (not body `from`).
- `CodeExchange`: JWKS null-guard (fetch-fail-then-verify path).
- Admin `handlePin`: `redirect: manual` on outgoing fetch (Node default was
  `follow` — redirect-smuggling); `iss`/`sub` validation of returned EC.
- Callback `target` validation: single-root relative path only; `//`,
  backslash, `scheme` rejected → fallback `/`.
- Revoke sweep: `revokeClientCodes` (adapter) + export sweep
  `sweepExportGrants` (ledger → `revoked`, PAT deleteOne scope-guarded
  `scope: 'federation:git_bridge'`, best-effort).
- oidc-provider adapter: GRANTABLE Set, sub-indexes, `killOutstandingCodes`
  peer-flag sweep.
- Session 5 fetch timeouts on every outgoing (`AbortSignal.timeout`).

## 4. Testing conventions (OlliTeX Go)

- Every Go file has a `<name>_test.go` in the same package (no `test/` dirs).
- Oracle pins: test names/tables cite the **Node file + behavior** (e.g.
  `TestSaltedLocalNameHash_32hex` pins `util/Anchor.mjs saltedLocalNameHash`).
- No live Mongo/Redis in unit tests: handlers take seams
  (`Store` interfaces / `*core.App` fields) — mirror how `features/consent`
  and `features/consent/legal_test.go` construct `&core.Cxt{Req: httptest.NewRequest(...)}`.
- Race-enabled: `go test -race -count=1 ./go/services/web/features/federation/...`.
- Green slice: build + vet + gofmt + tests before the next slice; update
  Progress Log; commit per logical slice (stage only that slice's files).

## 5. Progress Log

### 2026-09-26 (session 1 — recon + scaffold)
- Read overleaf-fed `federation` HANDOFF (2238 lines), plan 00–11, FINDINGS,
  README (route inventory), all prod .mjs, 4 pug views, ssoCertExpiry,
  ssoRoleEvaluator, samlMetadataProbe scope.
- Recon OlliTeX Go: core route contract (`NoLogin/NoSession/APIOnly/Pattern`),
  redis (RESP2 handwritten client + `core.RedisClient`), mongo (`MongoLazy`),
  views slot system, config env contract, feature test shape.
- **Found**: OlliTeX's Node SAML/OIDC/SSO modules (D7 stored site-settings
  variant) **exist and are kept alive** (not retired); Go does NOT yet serve
  the `/saml`, `/oidc`, `/auth/external/*` surfaces (they run through the
  Node shadow web). overleaf-fed's SAML/OIDC delta (N-provider framework,
  attrFilter, synthetic-email JIT, SP metadata, cert-expiry, metadata probe,
  SSO admin UI) is the integration target.
- Wrote this HANDOFF + slice plan.
- 2026-09-26: S2 committed (7c1f630). Models 1:1 from Node module-local Mongoose schemas. Note: Go mongo-driver is v1 (go.mongodb.org/mongo-driver v1.17.10, repo root go.mod) — v1 `FindOne` returns `*SingleResult` (`.Decode(&v)`), `FindOne`/`UpdateOne`/`DeleteOne` return `(res, err)` tuples; not-found sentinel is `mongo.ErrNoDocuments` (NO exported `mongo.ErrNotFound` in v1).
	- Bug fixes carried: IsPrivateJWK (was dead expression), Errorf variadic, RotateKey two-write state machine (02 §5), matchesDirection shared helper.
- **Next: S3 keystore + leaf + client assertion — OIDF ES256 (crypto/ecdsa P-256 stdlib, JWS hand-rolled per HANDOFF §D-OP); oracle: `oidf/keystore.mjs`, `oidf/leaf.mjs` (leaf builder: OIDF entity config + authority hints + kid thumbprint), `oidf/clientassertion.mjs` (signed client assertion + jti replay via S1 ClaimJti). Node @oidfed/core is oracle for wire format; Go stdlib is the impl (no JOSE dep per go.mod discipline). Check keystore.mjs first for the exact bootstrap/rotate sweep + ES256 JWK→PEM encoding before porting.**

### 2026-09-26 (session 2 — S3 keystore + leaf + client assertion)
- S3 committed (`e141548`): `jws.go` (ES256 Compact JWS: GenerateES256, KeyThumbprint = RFC 8550 §2.1 thumbprint — npm v1.0.0 kid byte-match via testdata/key.json, SignJWT/VerifyJWT Compact 3-segment {alg:ES256, typ, kid}, r‖s 64-byte per RFC 7518 §3.2, kid pre-check → ErrUnknownKid, UUIDv4, mustMarshal). `leaf.go` (getEntityIdGo portless https, getOriginGo bare FQDN, getClientIdGo urn, getS2sEndpointGo, oidcEndpointsGo). `clientassertion.go` (KeyProvider{Store,Site}: ActiveKey/Bootstrap(fed+oidc)/LeafSigningKey/leafJwksPayload(non-revoked)/HistoricalKeySetPayload/BuildLeafEntityConfiguration pairwise NO authority_hints (check 16 openid_provider.issuer==OP issuer) / buildLeafMetadata / BuildS2sRequest(S2S headers+envelope)/VerifyS2sClientAssertion — npm oracle machine codes peer-unknown/unknown-kid/bad-signature/timestamp-skew, iss from peer origin (never body), aud==our S2S, jti, iat/exp clock skew 60s).
- **Bug found+fixed (wire-level)**: `b64url` was declared as `base64.RawStdEncoding` (STD `+/` alphabet) — npm v1.0.0 fixtures proved it must be `base64.RawURLEncoding` (`-_`). Without the fix every Go S2S assertion / leaf would be rejected by npm peers (kid mismatch `+` vs `-`, malformed r‖s base64). Also `oidcEndpointsGo` now derives from portless entity id (was raw host with port).
- testdata fixtures (npm @oidfed/core v1.0.0, dist extracted to /tmp/oidf-core and deleted after): key.json (pub/priv JWKs), ca.txt (signed S2S assertion). leaf fixture dropped (leaf.txt was generated in-process; round-trip covers it).
- Gate: go build ./... && go vet ./services/web/... && gofmt -l == 0 && go test -race -count=1 → 22/22 PASS (S1 9 + S2 8 + S3 5 incl. TestVerifyJWSOracleCA Go-vs-npm byte oracle).
- **Next: S4 = B-side OIDC provider (owner Q2 DECIDE: port oidc-provider v9 subset into Go) — the largest slice: auth/token/jwks/resume/interaction + 7-method Redis adapter + Grant/PKCE, oracle-pin v9.12.2.**

### 2026-09-26 (session 3 — S4a OIDC Redis adapter)
- S4a committed (`ddf3b4c1`): `oidcadapter.go` (OidcAdapter{ModelName, Redis} with the v9 7-method contract: Find/FindByUID/FindByUserCode/Upsert/RevokeByGrantId/Destroy/Consume + top-level RevokeClientCodes 04 §5 killOutstandingCodes + FindByAccountAndClient 09 §2.1 live-grant lookup). Key layout 05 §8.2: `federation:oidc:<Model>:<id>` doc + `sub:`/`usercode:` sub-indices + `grant:`/`client:`/`account:` SETs (SCARD==0 → DEL). Consume sets `payload.consumed` (epoch sec) + re-sets with remaining PX ms (NOT delete; revocation cascade, 02 §7.5). Sweep gates on `grantable` (token docs, NOT the Grant record — a consent Grant carries a clientId but is never swept, 06 §174). No cross-linking (sub-indexes only, 06 §178).
- `core/redis.go`: +SETEX(key,value,sec) SEtEX / SETPX(key,value,ms) / SADD / SREM / SMEMBERS / SCARD / PTTL — all hand-written RESP2. PTTL is the fake-redis parity primitive (stale-member skip).
- s4_test.go: fakeOidcRedis w/ TTL simulation (SETEX expiry via injected `now`); tests pin upsert cascade (sub/grant/client indices), consume flag-not-delete, revoke-by-grant cascade (token sweep survives consent Grant + other clients), destroy secondary-index trim, expired-member skip in FindByAccountAndClient, second-sweep idempotence (0).
- Gate: go build ./... && go vet ./services/web/... && gofmt -l == 0 && go test -race -count=1 → all PASS (S1 9 + S2 8 + S3 5 + S4 7 = 29).
- **Next: S4b = `oidcprovider.go` engine — the oidc-provider v9.12.2 subset. Mount order (bridge BEFORE provider, plan 05 §1.1): `/federation/oidc/interact/:uid` (GET + /consent + /deny) on the WEB router, then `/federation/oidc/{auth,token,jwks,resume/:uid}`. Oracle files: createProvider.mjs (ttl: AuthorizationCode 120 s, Grant 30d, Interaction 600 s, Session 8h; subjectTypes ['public']; claims.openid = [sub,origin,localName,displayName,institution] — WITHOUT it v9 prunes the custom claims from id_token, verified e2e), clients.mjs (federationClientId(origin) = urn:overleaf-federation:client:<origin>, static clients[] rebuild from approved peers, SNAKE_CASE metadata, scope STRING, ES256 mandatory), bridge.mjs (prompt.login→auto finishLogin w/ mergeWithLastSubmission; prompt.consent→Grant dedup via FindByAccountAndClient silent-reuse else render consent.pug; deny→error:access_denied). v9 interaction model: uid is a JTI (not the uid), resume path issues code, redirect to A with error/redirect params.  Grant payload must round-trip through `OidcAdapter` (payload keys = pickPayload: jti, accountId, clientId, scope/claimNames). Redis adapter TTL: SECONDS, the factory is invoked without `new` (Go seam is direct `&OidcAdapter{...}`). v9 `findAccount` contract: `(sub, source) => account{accountId, claims(use,scope,allowedClaims,rejected)->map}` Go seam: the provider's `OidcUserSource` interface (`Account(ctx, userID) (OidcUserClaims, bool)`, live in oidcprovider.go).

### 2026-09-26 (session 4 — S4b-1 provider core: clients/jwks/discovery)
- S4b-1 committed (`aa21e4a0`): `oidcprovider.go` — `oidcPurpose = "oidc"` (the id_token signing key, 02 §5 TWO key purposes), `OidcUserClaims{Origin,LocalName,DisplayName,Institution}` + `OidcUserSource` seam (User.findById equivalent, suspended/unknown → ok=false), `BuildOidcProviderClients` (clients.mjs oracle: approved peers, direction NOT filtered — both roles get browser-facing clients, client_id `urn:overleaf-federation:client:<origin>`, redirect `https://<origin>/federation/oidc/rp/callback`, scope STRING "openid"), `ProviderClientByID` (mint gate 06 §3.1), `OIDCJwksPayload` (oidc-purpose non-revoked, PublicHalf only), `OidcDiscoveryMetadata` (v9 discovery doc). `oidcprovider_routes.go`: `s4Routes` with GET /federation/oidc/jwks + GET /federation/oidc/.well-known/openid-configuration, both NoLogin (cross-origin OP surface); `federStore(c)` builds MapStore-free prod `MongoStore` + `KeyProvider` per request.
- core parity landed in `098e879c`: `Route.NoCSRF` flag (Node `csrf.disableDefaultCsrfProtection()` parity) — the OP token/consent/deny POSTs + S2S land here in S4b-2/3.
- s4b_test.go: clients oracle (3 approved / pending+revoked excluded), JWKS purpose (federation key + revoked excluded, d never served, CodeExchange kty/crv/x/y resolve shape), discovery metadata, jwks route handler via httptest.
- Gate: build/vet/gofmt/test -race clean (29 tests + 7 new = 36... S1 9 + S2 8 + S3 5 + S4 7 + S4b1 4 = 33 total). Next: S4b-2/3 engine.

### 2026-09-26 (session 5 — S4b-2/3 oracle re-pin + engine draft)
**RESOLVED (this rewrite session, verbatim from vendored source):**

- **err_out.js** (17 lines — the definitive wire): `({expose, message, error_description: description, scope}, state)` → expose: `{error: message, error_description?, scope?, state?}` (each spread only when !== undefined); non-expose: `{error:'server_error', error_description:'oops! something went wrong', state?}`. All OIDCProviderErrors set `.error = .message = <code>` at construction, and `.error_description` is a per-class constant (InvalidGrant: 'grant request is invalid'; InvalidRequest: 'request is invalid'; CustomOIDCProviderError takes both) — so the AUTH-flow wire (redirect) carries `error` + (class) `error_description` + `state` + `iss`; TOKEN-flow 400 JSON = `{error, error_description, state?}` (state only echoed for token route, normally absent). **All InvalidGrant anywhere → `{"error":"invalid_grant","error_description":"grant request is invalid"}`** — per-cause messages (PKCE etc.) are error_detail/log only. `AccessDenied = E('access_denied')` → wire `{error:'access_denied', error_description: (class default... AccessDenied ctor: `E` sets error_description? E(name, description) — AccessDenied has NO description → wire OMITs error_description for access_denied}`.
- **Auth-side error redirect** (shared/authorization_error_handler.js, verbatim): catch → `out = {...errOut(err, state), iss: issuer}`; redirect only when client resolved (or no client_id) AND redirect_uri known AND `err.allow_redirect` (AccessDenied/InteractionRequired/login_required etc. = true) → response mode (default query) → 303 redirect to `redirect_uri?error=...&error_description?&state&iss` (undefined-filtered). Else `renderError`. Deny flow → 303 to A's callback with `?error=access_denied&state&iss` (NO error_description — AccessDenied has none).
- **query.js response mode**: `ctx.status = 303; ctx.redirect(uri)` — 303 confirmed (NOT 302). formatUri appends payload keys as query (URL-encoded, sorted? NO — insertion order... formatUri = redirect_uri + `?${encodeURIComponent(k)}=...` per key in object order... Go: build via url.Values (Go sorts alphabetically on Encode; wire-key ORDER is observation-relevant only for oracle diffs — use explicit string build or sorted — RESOLVE at test time: live-smoke parses params, order-independent).
- **interactionDetails vs cookie**: provider.js #getInteraction reads the `_interaction` COOKIE (cookies.get with cookies.short options), then Interaction.find(cookie id). Bridge routes are at interact/:uid but v9 resolves the interaction FROM THE COOKIE. The dance's cookie jar (live-smoke driveDance carries `Cookie:` header forward) guarantees match. **Go port: bridge handlers must ALSO read the `_interaction` cookie and 404 SessionNotFound when cookie missing/mismatched with URL uid.** RESUME is the same: resume.js reads `ctx.cookies.get(cookieName('resume'))` AND checks `cookieId === interactionSession.uid` (URL param uid must equal cookie uid → else SessionNotFound). So resume URL uid == cookie uid == interaction jti. The mint sets BOTH cookies to the SAME uid (interactions.js:120-135) — one uid, two cookies, two paths.
- **interaction mint params**: `params: oidc.params.toPlainObject()` = all authorization params (client_id, redirect_uri, scope, state, nonce, code_challenge, code_challenge_method, response_type, ...). The Go doc `opInteraction.Params` carries these (draft already has the right shape).
- **interaction session subdoc**: `{accountId, uid: session.uid, cookie: session.jti}` (when session.accountId set; else `session: undefined` in payload — models/interaction.js:4-15). Draft stores `SessionUID` + `AccountID` flat — ACCEPTABLE (behavioral parity: resume checks `originSession?.uid && originSession.uid !== session.uid`; Go: skip check when session subdoc nil). RESUME session mismatch branch: if `interaction.session.uid` set AND ≠ current session.uid → SessionNotFound 400 (no redirect). DRAFT MUST PRESERVE this: on resume, load cookie→Session (cookie `_session`), and if interaction.SessionUID set + session doc not loaded/uid mismatch → 400.
- **Account-switch logout redirect (resume.js:24-40, rarer path)**: `result.login && session.accountId && session.accountId !== result.login.accountId` → rewrite interaction (clear session.uid, save) + formPost to end_session_confirm (XSRF). Our overleaf-fed flow: the bridge finishLogin uses the B-side logged-in user's accountId; a mismatch means the visitor switched accounts in another tab → end_session_confirm form (POST, NOT a GET redirect). Go port: implement as 200 HTML form render (the v9 formPost default) — low priority (the A-side dance can't trigger it without manual account switching; live-smoke doesn't test it). RESOLVE at test-time: minimal 200 form stub is oracle-acceptable for this arc.
- **NON_REJECTABLE_CLAIMS**: consts/non_rejectable_claims.js — RESOLVE: file path is `lib/consts/non_rejectable_claims.js`? (ls showed no consts/ dir — verify exact path during rewrite; value is `{sub, sid, auth_time, acr, amr, iss}` per grant.js import).
- **loadAccount in resume chain**: actions/authorization/session.js loadAccount runs AFTER resume.js in the stack for route R: `resume` → ... → `loadAccount` (findAccount via ctx.oidc.session.accountId; if null → throws? v9 loadAccount: `if (accountId) { validateAccount(await findAccount...) }; ctx.oidc.entity(...)` — a NULL findAccount result makes validateAccount throw... configuration_result.account(value): if (!value) throw → 400 invalid? NO — loadAccount catches? NO. v9: `const account = validateAccount(await ...findAccount(ctx, accountId))` — validateAccount (configuration_result.account) throws `Error('invalid account...')` for null → unhandled → BUT createProvider.mjs findAccount returns null only for suspended/unknown — in the DANCE after login, the account is alice (known, not suspended) → loadAccount OK. For a suspended account this would be a 500. Go port: after resume, resolve account via Users.Account(ctx, accountId); on false → 400 invalid_grant-style... RESOLVE at test-time with a minimal 400; live-smoke never suspends mid-dance.
- **checkPKCE AUTH-side** (sender_constraints.js): public client (token_endpoint_auth_method='none' → pkce.required() = true per defaults.js:312 pkceRequired) → NO code_challenge → InvalidRequest 'Authorization Server policy requires PKCE to be used for this request'. WITH code_challenge: method must be S256 ('unsupported code_challenge_method'... actually default method IS S256 via defaults — no need to check). codeChallenge FORMAT: 43-128 chars `[\w.~-]` (pkce_format) → InvalidRequest on violation. DRAFT auth-side check must implement both presence + format.
- **CombinedScope (process_response_types codeHandler)**: scope = grant.getOIDCScope() ∪ requestParams ∪ resources — for openid-only: code.scope = 'openid' (when grant has scope='openid' after consent). Grant with NO scope yet (loadGrant minted it but policy didn't run yet... NO — in the NO-INTERACTION path, grant was JUST loaded and scope-filtered; the consent bridge minted `addOIDCScope(missingOIDCScope)` BEFORE saving → grant.openid.scope='openid'). DRAFT MUST: on resume no-interaction path, code.scope = grant.getOIDCScope() (which after consent = 'openid'). The token endpoint then returns this scope.
- **codeHandler sessionUid**: `sessionUid: ctx.oidc.session.uid` — the session UID (NOT jti). Draft already has this.
- **id_token claims filter (id_token.js payload())**: `Claims mask/Claims` — filter = claimConfig['openid'] (from createProvider.mjs claims) = ['sub','origin','localName','displayName','institution']; available = account.claims() + extras (nonce, sid, auth_time...); Claims.result() picks available for keys in filter AND non-rejected AND in claimsSupported — the createProvider claims list IS the supported set. So payload = `{sub: accountId, origin, localName, displayName, institution, nonce, auth_time, acr?, amr?}` — acr/amr undefined→dropped; nonce always present (token.set('nonce', source.nonce) after filter — extras survive the filter). **RESOLVED: Go port payload keys EXACTLY: sub, origin, localName, displayName, institution, nonce, auth_time, iss, iat, exp, aud. (NO sid, NO acr, NO amr — the live-smoke id_token is verified against these exact claims. CONFIRM no extra `scope` key — v9 IdToken.payload() does NOT include scope, that's a separate field.) DRAFT IS CORRECT here.
- **id_token signed by OIDC key** (id_token.js issue for idtoken): alg = client.idTokenSignedResponseAlg = ES256; keystore = provider instance keystore (NOT client symmetric); `signOptions.fields = {kid: jwk.kid}` (id_token.js:120-127). `JWT.sign(payload, key, alg, signOptions)` → payload+iss/aud/iat/exp + typ='JWT' (NOT 'id+jwt'... signOptions for idtoken: no typ → default 'JWT'). **DRAFT MUST verify its `buildIDToken` uses `typ="JWT"` (draft uses this — CORRECT).**
- **id_token expiresIn**: `expiresIn || this.constructor.expiresIn(ctx, this, client)` = IdTokenTTL = 3600 (vendor default). exp = iat + 3600. CORRECT in draft.
- **Opaque id format (RESOLVED — v9 byte-exact)**: ALL opaque ids (session jti/uid, interaction jti/uid, AuthorizationCode jti, AccessToken value) = `nanoid(length)` from `models/formats/index.js` → `opaque.generateTokenId`: length = `ceil(bitsofOpaqueRandomness/6)` with vendor default `formats.bitsOfOpaqueRandomness: 256` (defaults.js:2867, NOT overridden by createProvider.mjs) → **43 chars** from nanoid's customAlphabet over the 64-symbol alphabet `useandom-26T198340PX75pxJACKVERYMINDBUSHWOLF_GQZbfghjklqvwyzrict` (helpers/nanoid.js — byte-preserving transcription of nanoid). **Go rewrite: one `opNanoidID()` that reads 64 random bytes, maps each byte to the 64-symbol alphabet, takes ceil(43*6/8)=33... NO — v9 reads bytes in 6-bit groups: 43 symbols * 6 bits = 32.25 bytes → 33 bytes, each masked to 6 bits (mod bias accepted by nanoid's transcription). Simpler Go impl (behavioral parity): `for i:=0;i<43;i++ { alphabet[rand.Int31()%64] }` — same distribution family, wire-observable length = 43 exactly.** Draft's `nanoidID` (50-hex) must be replaced with a 43-char alphabet id. A-side never decodes; length is wire-observable and our own tests pin it.

**v9 oracle fully re-pinned this session** (vendored 9.12.2 at `/home/davrot/federation/node-oidc-provider/lib/`, all files re-read):

- **Routes** (initialize_app.js:215): `GET /auth` (routes.authorization), `GET /auth/:uid` (resume — note resume path is a SUFFIX of auth, NOT /resume), `POST /token`, `GET /jwks`. devInteractions DISABLED (createProvider.mjs) → interaction UI is the overleaf-fed **bridge** at `/federation/oidc/interact/:uid` (mounted on the WEB router BEFORE the provider per plan 05 §1.1). Go pattern routes: `auth$` + `auth/[0-9a-f]+` (uid = nanoid), bridge GET/POST consent/deny + token POST.
- **TTLs** (defaults.js:344-409 + createProvider.mjs override — final values): AccessToken 60*60, **AuthorizationCode 120** (vendor 60*1 overridden), IdToken 60*60, Grant 30*86400 (vendor 14d overridden), Interaction 600 (vendor 3600 overridden), Session 8*3600 (vendor 14d overridden), RefreshToken 14d (vendor default, NOT overridden — irrelevant: no offline_access scope → refresh_token NEVER issued, defaults.js:305 issueRefreshToken = `client.grantTypeAllowed('refresh_token') && source.scopes.has('offline_access')` and client grant_types=['authorization_code'] only).
- **Cookie names/paths** (defaults.js:917-925, cookie options cookies.long/short = {httpOnly:true, sameSite:'lax'}; maxAge = ttl seconds × 1000): `_session` Path=`<issuer>` (8h), `_interaction` Path=`<bridge interact path>` (interaction ttl), `_interaction_resume` Path=`<returnTo>` (interaction ttl). **Session cookie is NOT written on a fresh session unless `touched`** (shared/session.js:43-57 — `(!session.new || session.touched) && !session.destroyed`). Session model IN_PAYLOAD: `[iat,exp,jti,kind,uid,acr,amr,accountId,loginTs,transient,state,authorizations]` (models/session.js:18-29).
- **Session.get / resetIdentifier** (models/session.js:41-77,139-145): cookie → find (gone → `instantiate(undefined)` fresh, `session.new=true`); `loginAccount` sets accountId+loginTs+acr+amr + marks touched (shared/session.js Proxy, sets 'touched'=true on accountId write); `resetIdentifier()` = `oldId=id; id=nanoid(); touched=true` (save() destroys oldId first — models/session.js:103-110); `authTime()` returns loginTs. Resume ALWAYS calls resetIdentifier when `!session.new` (resume.js:120-122) before next().
- **Interaction** (models/interaction.js): `uid === jti` (constructor `uid` param IS the jti); `lastSubmission` = previous `oidc.result` (initially undefined); session subdoc = `{accountId, uid, cookie, acr, amr}` (only when session.accountId set); `grantId` in payload if grant exists. `interactionFinished` (provider.js:239-262): merge = `interaction.result = {...lastSubmission, ...result}` (mergeWithLastSubmission || error in result); save(exp - now remaining); responds **303** + Location=returnTo (NOT 302), empty body. `interactionDetails` = Interaction lookup for bridge rendering.
- **Authorization flow** (actions/authorization/*): checkClient (unknown client → error 400, NO redirect per errOut `allow_redirect:false` for InvalidRedirectUri-class... actually **checkClient throws `errors.InvalidClient` = 400 no redirect**); checkPrompt; checkScope (provider scopes=['openid'] — scope='openid' only); checkRedirectUri (must be in client.redirectUris); checkPKCE **REQUIRED for public clients** (client.clientAuthMethod='none' → pkce.default `pkce.required()` returns true: sender_constraints.js); **PKCE verifier format (pkce_format.js)**: 43-128 chars `[\w.~-]` (i.e. `[A-Za-z0-9._~-]`) for code_verifier at TOKEN time (checkPKCE in helpers/grant_common via pkce.js → checkFormat on verifier); checkResponseMode (default 'query').
- **Interaction policy loop** (interactions.js:15-80): for each prompt (login, then consent), run checks in order — first failing check produces `{prompt:{name, reasons}, failure: failedCheck}`. **login.js checks**: `no_session` (session.accountId unset) → always first; `max_age` / `id_token_hint` / `claims_id_token_sub_value` / `essential_acrs/acr` (skip if params absent) — our dance only hits `no_session`. **consent.js checks**: `native_client_prompt` (client.applicationType='native' → skip, ours is 'web'), `op_scopes_missing` (requested scope set minus grant.getOIDCScopeEncountered → `missingOIDCScope` in details), `op_claims_missing` (requested claims minus grant.getOIDCClaimsEncountered minus NON_REJECTABLE_CLAIMS → `missingOIDCClaims`). NON_REJECTABLE_CLAIMS = `{sub, sid, auth_time, acr, amr, iss}` (consts — re-verify exact path during rewrite). `failedCheck` carries `{error: check.error || 'interaction_required', error_description: check.description}` → this is the wire error (e.g. `login_required` / `consent_required`... actually the class names: errors.login... verify class names: `LoginRequired` error='login_required'? — the prompt name becomes error via `errors[upperFirst(camelCase(name))]` → `loginRequired`... RESOLVE at rewrite: read interaction_policy/login.js + consent.js check() error strings verbatim). If a check fails AND `prompt` param was 'none' → throw with redirect (303 if redirect allowed / 400 if not); else MINT interaction: `new Interaction(nanoid(), {returnTo: urlFor('resume', uid), prompt, lastSubmission, accountId, params, session, grant, trusted, cid})`, save(ttl=600), set `_interaction`+`_interaction_resume` cookies (path-scoped to destination/returnTo, see cookie contract above), `ctx.status=303; ctx.redirect(interactions.url(ctx, interaction))` → OUR createProvider.mjs overrides `interactions.url` = `${siteOrigin}/federation/oidc/interact/${uid}`.
- **NO-INTERACTION path** (interactions.js:85-107, when prompt undefined after all checks pass): requires session.accountId (else AccessDenied 400 no-redirect... actually error 400 "authorization request resolved without requesting interactions but no account id was resolved") AND `grant.getOIDCScopeFiltered(requestParamOIDCScopes)` non-empty (else AccessDenied "no scope was granted"); then `respond(ctx)`.
- **respond (respond.js + process_response_types.js)**: response_type='code' → codeHandler: AuthorizationCode payload = `{accountId, sessionUid: session.uid, authTime: session.authTime(), grantId: session.grantIdFor(clientId), acr, amr, nonce: params.nonce, codeChallenge, codeChallengeMethod, redirectUri, scope: combinedScope(grant, requestParams, resources) joined ' ', sid}` — save() → `{code: saved value}` (opaque = jti nanoid). out = `{code, scope, state (if params.state), iss: issuer (when NOT id_token and NOT jwt mode)}` → 303 redirect to redirect_uri with query (query mode) — the dance's final hop 303→A /federation/oidc/rp/callback?code&state&iss.
- **loadExistingGrant** (defaults.js:544-551): `grantId = oidc.result?.consent?.grantId || oidc.session.grantIdFor(clientId); if (grantId) return Grant.find(grantId); return undefined`. **loadGrant** (actions/authorization/session.js:17-43): if oidc.account (from loadAccount via findAccount) AND grant found → check `grant.accountId === account.accountId` (throw 'accountId mismatch') AND `grant.clientId === client.clientId` (throw 'clientId mismatch') → `session.ensureClientContainer(clientId); session.grantIdFor(clientId, grant.jti)`; else `new Grant({accountId, clientId})` (no scope/claims yet).
- **Grant model scope** (models/grant.js): `openid: {scope, claims}` subdoc (NOT a flat scope field); `getOIDCScope()` returns `openid.scope` string (minus rejected); `getOIDCScopeEncountered` = granted ∪ rejected; `getOIDCScopeFiltered(Set)` = granted ∩ filter. Bridge consent: `grant.addOIDCScope(prompt.details.missingOIDCScope.join(' '))` + `addOIDCClaims(missingOIDCClaims)` + save. Grant IN_PAYLOAD = `[accountId, clientId, resources, openid, rejected, ...base(iat,exp,jti,kind)]` → our adapter round-trips the `openid` subdoc (models.go Grant doc: `OpenID{Scope,Claims}`).
- **Bridge** (bridge.mjs, full re-read): GET interact/<uid> uses `provider.interactionDetails(req,res)` (reads `_interaction` cookie, NOT the URL uid! — verify: interactionDetails uses #getInteraction which reads cookie... RE-CONFIRM during rewrite: provider.js:426 #getInteraction reads `ctx.cookies.get(cookieName('interaction'))` then `Interaction.find(id)`... actually reads cookie id then Interaction.find(id). The bridge mount is at interact/:uid so the cookie must match or it's SessionNotFound → this is why the dance carries cookies. **RESOLVE at rewrite: does #getInteraction trust the cookie or the :uid param? Vendored source: cookie id.** Hmm but bridge.mjs passes (req,res) to interactionDetails — cookie must be present in the dance (driveDance carries the cookie jar forward — CONFIRMED the dance sends cookies). handleInteractGet: not logged-in → `res.redirect('/login')` (Node express 302, sets postLoginRedirect via AuthenticationController.setRedirectInSession); prompt.login → finishLogin: `interactionFinished({lastSubmission?... : {}, login:{accountId: user._id, ts: floor(Date.now()/1000)}}, {mergeWithLastSubmission:true})`; prompt.consent → `findByAccountAndClient` (our adapter method) → grant? `interactionFinished({lastSubmission..., consent:{grantId}}, merge)` (silent reuse — NO re-consent) : render consent.pug (view returns 200 HTML with `<form ... grantUrl denyUrl>` — our Go render must emit HTML containing the marker `consent-form` (live-smoke: `html.includes('consent-form')`)). POST consent/:uid: 401 if not logged-in (overleaf session!), load interaction, `new Grant({accountId: userId, clientId: params.client_id})`, addOIDCScope/Claims from prompt.details, grant.save(), `interactionFinished({lastSubmission..., consent:{grantId}}, {mergeWithLastSubmission:true})`. POST deny/:uid: `interactionFinished({...lastSubmission, error:'access_denied', error_description:'End-User denied consent'}, {mergeWithLastSubmission:true})`.
- **Token** (actions/grants/authorization_code.js EXACT order + errOut): (1) presence 'code','redirect_uri' (→ invalid_request); (2) `findGrantSource`: AuthorizationCode.find(code, {ignoreExpiration:true}) → not found → InvalidGrant 'authorization code not found'; client mismatch → InvalidGrant 'client mismatch'; (3) code.isExpired → InvalidGrant 'authorization code is expired'; (4) validateGrant: Grant.find(grantId,{ignoreExpiration:true}) → not found → 'grant not found'; expired → 'grant is expired'; client mismatch → 'client mismatch'; (5) checkPKCE: verifier format → InvalidGrant (cause → error_detail), S256: `crypto.hash('sha256', verifier,'base64url')` compared constant-time to code.codeChallenge, mismatch → InvalidGrant (ALL → wire `{error:'invalid_grant', error_description:'grant request is invalid'}` — CONFIRMED via err_out.js verbatim: exposed errors serialize `{error: <code>, error_description: <class const>, state? (only if in params)}` at 400; per-cause PKCE messages are error_detail (log only). (6) redirectUri mismatch → InvalidGrant; (7) checkMtlsCert (N/A), checkDpopRequired (N/A — feature off); (8) consumeGrantSource: if consumed → revoke(ctx, grantId) + InvalidGrant 'authorization code already consumed'; else source.consume() (adapter.consume sets `consumed` = epoch, keeps doc alive for cascade); (9) issueTokens: validateAccount (findAccount(ctx, code.accountId, code) → null → InvalidGrant 'authorization code invalid (referenced account not found)'); checkAccountMismatch (code.accountId !== grant.accountId → InvalidGrant 'accountId mismatch'); createAccessToken (opaque: accessTokenFormat default 'opaque', value = generateTokenId nanoid 160 bits... **RE-VERIFY exact length at rewrite: generateTokenId → helpers/nanoid length**; our A-side NEVER decodes the access_token — opaque string, pin shape but length only matters for our own tests); save AT (expiresIn 3600); refresh: skipped (no offline_access); issueIdToken: filterClaims(source.claims='id_token'... claims param is undefined → {} mask... the id_token claims = getCtxAccountClaims(ctx,'id_token',scope,claims=[],rejected=[]) = `{...account.claims('id_token', scope, allowedClaims, rejected), sub: accountId}` → findAccount returns claims fn returning `{origin, localName, displayName, institution}`; **claim-name filtering**: Claims mask — `mask.scope('openid')` → claimConfig['openid'] = the createProvider `claims: {openid: [...]}` → allowed names = [sub,origin,localName,displayName,institution] intersected with available → `{sub,origin,localName,displayName,institution}` + extra `{nonce: source.nonce, sid: session.sidFor(clientId) (N/A — includeSid false)}` + auth_time (source.authTime) + `acr`/`amr` (undefined, dropped). Payload signed ES256 (client.idTokenSignedResponseAlg='ES256' per clients.mjs) with kid=active oidc key kid, `{iss: issuer, aud: client_id, iat, exp: iat+3600, nonce, auth_time, sub, origin, localName, displayName, institution}`. **institution: `user.institution || null`** — wire is `null` when empty (NOT omitted) per createProvider.mjs findAccount!
- **buildTokenResponse** (grant_response.js): `{access_token, expires_in: 3600, id_token, scope: <source.scope ? at.scope : ...>, token_type: 'Bearer'}` — `scope: source.scope ? at.scope : (at.scope || undefined)` — scope IS included when source.scope truthy (our code always has scope='openid' since scope='openid' requested and granted) → wire token response = `{access_token, expires_in, id_token, scope:'openid', token_type:'Bearer'}` (NO refresh_token key — undefined filtered, `...parameters` empty). JSON via Koa → content-type application/json.
- **Revoke cascade** (helpers/revoke.js, on consumed-code replay): AccessToken.revokeByGrantId + RefreshToken (skip: grantTypes... **client.grantTypeAllowed('refresh_token') = false for our client (grant_types=['authorization_code']) → refresh_token NOT in cascade** — but AccessToken + AuthorizationCode + (device/ciba/preauth: NOT allowed either) → so cascade = `{AccessToken.revokeByGrantId, AuthorizationCode.revokeByGrantId}` and `revokeGrant? Grant.adapter.destroy(grantId) : undefined` — revokeGrantPolicy default = true EXCEPT revocation-route+AccessToken → here NOT revocation route → **Grant.destroy(grantId) IS called**. Our adapter Destroy on the Grant doc: removes `federation:oidc:Grant:<id>` + trims `account:` SET + SREM from... our adapter Destroy trims grant/client SETs + account SET (S4a) — MATCHES. So full cascade: AT docs gone + AC docs gone + Grant doc gone. **This is what S5 killOutstandingCodes differs from (RevokeClientCodes skips Grant).**
- **v9 error wire for token** (errOut + errors.js): OIDCProviderError has `.error` (code string e.g. 'invalid_grant'), `.error_description` (class-level e.g. 'grant request is invalid'), `.error_detail` (cause message, internal). err_out.js: `({expose, message, error_description, scope, state), state)` → hmm re-read at rewrite — the summary pins: wire = `{error: <code>, error_description: <class description>}` for exposed errors 400; state echoed if in params. All InvalidGrant in the token flow → `{"error":"invalid_grant","error_description":"grant request is invalid"}` status 400 (NOT per-cause descriptions — cause goes to error_detail = LOG only). **This matches the summary's "All InvalidGrant wire: ... grant request is invalid". RESOLVE-CONFIRM during rewrite by reading err_out.js verbatim (14 lines).**
- **auth-side redirect errors** (respond/query mode error path `query.js` responseModes): on error with redirect allowed (e.g. AccessDenied 'access_denied' from deny bridge — wait, deny sets result.error and resume re-throws it: `errorMap[upperFirst(camelCase(result.error))]` = AccessDenied... no — 'access_denied' → className='AccessDenied' → errorMap has AccessDenied (errors.js exports). Then auth error handler → 303 redirect to redirect_uri with `?error=access_denied&error_description=...&state&iss` (errOut with state). **Re-read shared/authorization_error_handler.js + response_modes/query.js at rewrite for the exact denial-redirect wire (denied dance in live-smoke... live-smoke has no deny scenario — but the A-side rp/callback must handle `?error=` → RESOLVE from respond.js error branch).**

### 2026-09-26 (session 6 — S4b-2/3 final wire recon; ALL conflicts RESOLVED)

Re-verified the vendored v9.12.2 (at `/home/davrot/federation/node-oidc-provider/lib/`,
Koa **3.2.1** installed in overleaf-fed) + the overleaf-fed oracle
(`oidc/createProvider.mjs`, `oidc/bridge.mjs` 217 lines, `tools/live-smoke.mjs`)
end to end. **Session 6 resolutions override session-5's conflicting
"RESOLVED" pins.** Every open RESOLVE item is now closed:

- **id_token payload (RESOLVED — session 5's list was WRONG)**: NO `auth_time`,
  NO `sid`, NO `acr`, NO `amr`. Wire payload = `{sub, origin, localName,
  displayName, institution (null when unset — NOT omitted), nonce}` + signOptions
  `{iss, aud: client_id, iat, exp: iat+3600}`. Why: `mask.scope('openid')` limits
  to claimConfig['openid'] keys (createProvider claims); grant_common's
  `auth_time` sits in `available` but is filtered out; `extra` = nonce only
  (`set('sid', undefined)` → dropped). Header = `{alg:'ES256', kid}` — **NO
  typ** (helpers/jwt.js leaves typ undefined for idtoken). Go: `SignJWT` always
  writes `typ` → need a separate `signIdToken(priv, payloadJSON)` helper.
- **Token response wire (RESOLVED)**: `200 {access_token: <43-char nanoid
  jti>, expires_in: 3600, id_token: <JWS>, scope: "openid", token_type: "Bearer"}`
  — scope IS present (buildTokenResponse `scope` key = at.scope, defined for
  openid grants). NO refresh_token (client grant_types=['authorization_code']).
- **Revoke cascade** (consumed-code replay at /token): adapter `Consume` sets
  `consumed=epochSec` (doc KEPT); second delivery → AccessToken.RevokeByGrantId
  + AuthorizationCode.RevokeByGrantId + **Grant.Destroy** (revokeGrantPolicy
  default true; NOT a revocation route). RefreshToken/device/others are NOT in
  the cascade (client grantTypes single). All → `400 {error:"invalid_grant",
  error_description:"grant request is identical...", }`... EXACT wire:
  `{"error":"invalid_grant","error_description":"grant request is invalid"}`.
- **PKCE split** (helpers/pkce.js: `checkFormat` is OUTSIDE the try/catch):
  token-side verifier FORMAT (43–128 chars, `[\w.-~]`) → `400 invalid_request`
  with that description; mismatch/missing → `400 invalid_grant`. Auth-side:
  public client (clientAuthMethod 'none') → PKCE required; missing
  code_challenge or method≠S256 → invalid_request (redirect if error-handler
  can, else 400 page).
- **err_out / expose (RESOLVED)**: all E-factory errors status **400** (hard-
  coded), `allow_redirect = true` is the OIDCProviderError CLASS DEFAULT (so
  every error is redirect-able when client+redirect_uri resolved in params).
  Wire = `{error: <code>, error_description? (only when defined), scope?,
  state?}` (each spread only when defined); non-expose → `{error:
  "server_error", error_description: "oops! something went wrong", state?}`.
  `AccessDenied` has NO default description — the deny flow sets it: resume
  re-throws via errorMap → `AccessDenied('End-User denied consent')` → deny
  wire HAS `error_description='End-User denied consent'`.
- **Routes (RESOLVED)**: `GET /federation/oidc/auth`; `GET /federation/oidc/auth/:uid`
  (**resume** — initialize_app.js:166 `get('resume', \`${routes.authorization}\n  // :uid\`)`; the createProvider.mjs header comment "GET /resume/:uid" is
  **DOC DRIFT — do not implement /resume/:uid**); `POST /federation/oidc/token`;
  `GET /jwks`; discovery. Cookie paths: `_session` Path=`/federation/oidc` (8h
  TTL cookie, HttpOnly SameSite=Lax, +`; expires=` patch from session.exp),
  `_interaction` Path=`/federation/oidc/interact/<uid>` (600s),
  `_interaction_resume` Path=`/federation/oidc/auth/<uid>` (600s).
- **Session cookie lifecycle** (shared/session.js finally): written ONLY when
  `(!new || touched) && !destroyed` → a fresh unmodified auth GET emits NO
  `_session` cookie; loginAccount on resume marks touched → cookie written
  (value = jti); `resetIdentifier()` on resume when `!session.new`: new jti,
  old doc destroyed first, **uid persists**. uid ≠ jti (separate nanoids).
- **interactionFinished (RESOLVED)**: `303 + Location(<absolute returnTo>) +
  Content-Length: 0` (raw res.end(), empty body). returnTo =
  `<issuer>/auth/<uid>` (absolute, urlFor('resume') mountPath-aware).
- **Resume chain (resume.js order)**: cookie `_interaction_resume` →
  Interaction.find(cookieId) → `cookieId === interaction.uid` →
  `interaction.session?.uid` cross-check vs current OP session uid (mismatch →
  400 SessionNotFound) → **interaction.destroy()** → ctx.oidc.params = stored
  → clear `_interaction_resume` cookie → `result.error` → throw (error handler
  redirects: client resolvable from stored params → 303 to redirect_uri with
  error/state/iss) → `session.loginAccount({accountId, loginTs})` →
  resetIdentifier → continue stack (loadAccount: findAccount null → 500 —
  suspended mid-dance, A-side never triggers it — loadGrant via
  loadExistingGrant = `result.consent?.grantId || session.grantIdFor(clientId)`
  → policy loop → respond).
- **Policy loop subset** (only live checks for our dance): login
  `no_session` (no accountId); consent `op_scopes_missing` (requested ∖
  granted → details.missingOIDCScope = ['openid'] for fresh grant). All others
  (max_age/id_token_hint/claims/acr/native_client) inert (no params, web
  client, no claims param).
- **Code mint (process_response_types codeHandler)**: doc = `{accountId,
  authTime: session.loginTs, grantId: session.grantIdFor(clientId), nonce,
  scope: <combined — 'openid'>, sessionUid: session.UID (uid, NOT jti),
  codeChallenge, codeChallengeMethod, redirectUri, expiresWithSession: true}`;
  final 303 → `redirect_uri?code=<jti>&state?<state>&iss=<issuer>` (state only
  when present in params).
- **Bridge oracle (bridge.mjs re-read — THE interaction UI spec)**:
  - GET `/federation/oidc/interact/<uid>`: B not logged in → express `302
    Location: /login` + B-session postLoginRedirect (absolute bridge URL);
    logged in → `provider.interactionDetails` (reads `_interaction` COOKIE —
    Go must read cookie + cross-check with URL uid; #getInteraction ALSO
    cross-checks interaction.session.uid/accountId)
    - prompt.login → `interactionFinished({login:{accountId: <B user id>,
      ts: epochSec}}, merge=true)` → 303 returnTo
    - prompt.consent → `findByAccountAndClient` (S4a adapter) → hit:
      `Grant.instantiate(stored)` + `grant.save()` (TTL refresh) → silent
      `interactionFinished({consent:{grantId}})`; miss → **200 HTML** consent
      view: `<meta name="federation" content="consent">`, `body.consent-view`,
      TWO `<form class="consent-form">` (allow → POST …/consent, deny → POST
      …/deny). A-side (live-smoke) detects consent by body string
      `'consent-form'`.
  - POST consent: B not logged in → 401; else new Grant {accountId: B user,
    clientId: params.client_id} + addOIDCScope(details.missingOIDCScope,
    "openid") → save → interactionFinished({…lastSubmission (when no error),
    consent:{grantId}}, merge=true) → 303 returnTo.
  - POST deny: `{…lastSubmission, error:'access_denied', error_description:
    'End-User denied consent'}` merge → 303 returnTo → resume throws → 303
    callback?error=access_denied&error_description=…&state&iss.
- **Dance shape (re-confirmed from live-smoke driveDance)**:
  `GET auth → 303 interact/<u1>` (login) `→ GET interact (B logged-in) →
  finishLogin 303 auth/<u1>` `→ (resume: login applied; op_scopes_missing)
  → 303 interact/<u2>` `→ consent-form 200 → POST consent → 303 auth/<u2>`
  `→ (resume: loadExistingGrant result.consent.grantId; policy pass) → 303
  <redirect_uri>?code&state&iss`. Cookie jar forward-only (live-smoke jar is
  name→value, no path); A-side token POST body = `grant_type=authorization_code
  &client_id=…&code=…&redirect_uri=…&code_verifier=…` (urlencoded, NO auth
  header — public client). A-side id_token checks: origin, localName, nonce,
  aud==client_id, iss (ES256 vs B JWKS kid).
- **Go seam survey (done)**: `core.Route{Method, Path, Pattern *regexp.Regexp
  (named group fills cxt.Params), NoLogin, NoCSRF, NoSession, APIOnly}`;
  `Res.JSON/SendStatus/BareWrite/PlainText`; `Res.Redirect(req,code,url)` emits
  ACCEPT-negotiated body → **NOT usable for OP 303s** (vendored is raw
  empty-body 303) → Go: `res.W.Header().Set("Location", u); res.WriteHeader(303)`
  (+ nothing else written → Go emits Content-Length: 0 automatically... verify
  empty-body claim against vendored Koa GET-redirect body once — A-side
  never consumes 303 bodies; Go decision: empty).
- **Draft state confirmed**: `oidcengine.go` (1082 lines, UNTRACKED) —
  `go build` FAILS (syntax 585/709), plus fidelity gaps (skips consent
  round after login-resume, double-load in opBridgeGet, dead mintSession /
  opResume placeholder blocks). No committed file references its identifiers
  → **delete + clean rewrite**.

### 2026-09-26 (session 6b — Go build confirmed broken; oracle fully sourced)

Re-located every vendored source file the engine encodes (initialize_app.js
route table, shared/error_handler.js + err_out.js + authorization_error_handler
.js + defaults renderError HTML, models/{base,session,interaction,grant,
access_token,id_token}, helpers/{grant_common,grant_source,pkce,pkce_format,
combined_scope,filter_claims,claims,account_claims,nonce}, actions/{
authorization/*,token,grants/authorization_code}, shared/{session,client_auth},
response_modes/query, consts NON_REJECTABLE_CLAIMS={sub,sid,auth_time,acr,amr,
iss}). Confirmed vendored `Session.get`: cookie present + doc gone →
`instantiate({})` (new=FALSE → cookie rewritten even if untouched); no cookie
→ `instantiate()` (new=TRUE). `Interaction.find` = jti lookup (uid getter =
jti); `access_token IN_PAYLOAD` (grantId, gty, sessionUid, sid?,
expiresWithSession, scope, ...). Confirmed Koa version (3.2.1) + that
overleaf-fed's own `node_modules/oidc-provider` = 9.12.2 (lib only, no
`test/`; test suite lives in the vendored tree at
`/home/davrot/federation/node-oidc-provider/test/`).
- `oidcengine.go` (untracked, 1082 lines) **build state confirmed BROKEN**
  before rewrite (go build fails at 585:51 / 709:47). All other package files
  compile. Delete + clean rewrite next.

### 2026-09-27 (session 7 — S4b-2/3 engine rewrite)
- Disk verified: the broken `oidcengine.go` stub is ABSENT (deleted),
  `git status` clean in the package, `go build ./go/services/web/...`
  green, 38 unit tests PASS (S1 9 + S2 8 + S3 5 + S4 7 + S4b1 4 +
  libspike M0-M6 4... — the lib M0-M6 spike tests live in the same
  package; count verified this session).
- Oracle re-verified a third time against vendored v9.12.2
  (`/home/davrot/federation/node-oidc-provider/lib/`, Koa 3.2.1 under
  overleaf-fed): all models (session/interaction/grant/authorization_code/
  access_token/id_token/base_model/base_token/token_helpers), actions
  (authorization/index + respond + resume + session + client +
  sender_constraints, grants/authorization_code), shared (session.js
  cookie gate, authorization_error_handler, error_handler, client_auth,
  no_cache), helpers (grant_common, grant_source, revoke, pkce,
  pkce_format, combined_scope, redirect_uri, err_out, validate_presence),
  response_modes/query, and the overleaf-fed bridge (bridge.mjs,
  createProvider.mjs FULL re-read, clients.mjs, consent.pug). Session 6
  wire pins re-confirmed against `errors.js` source THIS SESSION:
  - `E.InvalidRequest(description)` wire = `{error:'invalid_request',
    error_description: description || 'request is invalid'}` — the SPECIFIC
    thrown message rides the wire (e.g. 'code_verifier must be a string with
    a minimum length of 43 characters', 'client_id must be provided'); the
    fallback text applies only when no description is passed.
  - `E.InvalidGrant` wire = ALWAYS `{error:'invalid_grant',
    error_description:'grant request is invalid'}` (class property, per-cause
    messages are `error_detail` = LOG ONLY, never on wire).
  - `E.AccessDenied(desc)` wire = `{error:'access_denied',
    error_description:desc}` (NO class default — the deny bridge's 'End-User
    denied consent' DOES ride the wire; a bare AccessDenied without description
    sends NO error_description key).
  - `err_out.js`: `{error, error_description?, scope?, state?}` each spread
    only when defined; non-expose errors → `{error:'server_error',
    error_description:'oops! something went wrong', state?}`.
- Go 1.27 `http.Cookie` probe CONFIRMED (Go 1.27.1 + this toolchain):
  `Expires time.Time` is a struct FIELD (the `.Set` method is gone). Render
  rules (probed): `Expires` set → `Expires=Fri, 01 Jan 2027 00:00:00 GMT`;
  `MaxAge>0` → `Max-Age=<n>`; `MaxAge==0` with `Expires==zero` → `Max-Age` is
  OMITTED entirely. Session cookie (long-lived): set `Expires`, leave
  MaxAge 0. Interaction/resume cookies: `MaxAge: 600`. Cookie CLEARS
  (MaxAge 0, no Expires) render as `name=; Path=...; HttpOnly; SameSite=Lax`
  — wire-acceptable (browsers treat missing expiry identically to the
  vendored koa clear).
- Next: write the engine (Next-actions 1-5 unchanged), s4b2_test.go
  battery, gates, HANDOFF Progress Log + commit.

### 2026-09-30 (session 8 — S4b-2/3: clean core landed; auth subfile rewrite + vendor pins)
- `oidcengine.go` (482 lines) **clean rewrite COMPLETE, compiles standalone**:
  TTLs, `opOpaqUID` (43 chars, 64-symbol alphabet, `byte & 0x3F`),
  `oidcSiteOrigin`, `opErr` + constructors (`opInvalidRequest/Grant/AccessDenied/
  InvalidClientAuth/InvalidRedirectURI/UnsupportedGrantType/InvalidClient`),
  `opErrOut` (ordered) + `opRenderTokenError` (400 JSON), cookie helpers
  (`opMintCookies/opSetSessionCookie/opClearResumeCookie/opCookieValue`; Go
  1.27 render pins: Expires-only session cookie, MaxAge-600 short cookies,
  clear = value "" + MaxAge 0), doc ops (`opSave/Find/Destroy/Consume`),
  IN_PAYLOAD lists + `pickPayload`, client surface
  (`opClient/opRedirectAllowed/opClientGrantAllowed`), session model `opSess`
  (vendored shared/session.js Proxy semantics: cookie absent → new=true → NO
  cookie unless touched; cookie present + doc gone → `instantiate({})` NOT new
  → cookie rewritten). `oidcengine_auth.go` (210 lines) on disk is a
  PRIOR-GENERATION draft referencing dead symbols (`opErr400`, `e.clientParams`,
  `clientRedirectAllowed`, `cres`) — **build RED; full rewrite is the next
  step** (delete-and-rewrite, not patch).
- NEW vendor pins re-verified byte-for-byte this session (vendored v9.12.2
  at `/home/davrot/federation/node-oidc-provider/lib/`):
  - **checkSessionBinding wiring** (models/authorization_code.js:39 +
    token_helpers.js): the `AC.find` STATIC OVERRIDE wraps `super.find` → at
    TOKEN time `findGrantSource → AC.find(code, {ignoreExpiration:true})`
    runs `checkSessionBinding(token, {ignoreExpiration:true})` —
    `ignoreSessionBinding=false` (the option key is `ignoreSessionBinding`,
    `ignoreExpiration` is different) → for `expiresWithSession` codes:
    `Session.findByUid(code.sessionUid)` (via `sub:<uid>` index), then
    `token.accountId === session.accountId` AND `token.grantId ===
    session.grantIdFor(token.clientId)` — else the AC is treated as
    NOT-FOUND → wire `invalid_grant`/'grant request is invalid'. Go:
    `opACFind` must run this three-way check (session doc → doc-level
    `grantIdFor` since Go has no session model instance).
  - **E factory exact** (errors.js:117-137): class property
    `error_description = <default arg>`; `constructor(description, options)`
    overrides only when description truthy. `AccessDenied` = no default →
    bare `AccessDenied()` wire OMITS description; the deny bridge
    (`AccessDenied('End-User denied consent')`) DOES ride it.
    `UnsupportedResponseType/UnsupportedGrantType` have class-prop defaults
    'unsupported response_type|grant_type requested'. `InvalidClient` NO
    default; checkClient throws `InvalidClient('client is invalid',
    'client not found')` → wire desc 'client is invalid' (NO redirect —
    `nocclient=true` in ctx). `InvalidGrant` class prop 'grant request is
    invalid' (per-cause msgs = error_detail = LOG ONLY, never wire).
  - **Token route pre-checks EXACT** (actions/token.js + client_auth.js):
    no client_id → 400 `invalid_request`/"no client authentication mechanism
    provided"; client miss → 401 `invalid_client` "client authentication
    failed" (wire — 'client not found' is error_detail); presence(ctx,
    'grant_type') → "missing required parameter 'grant_type'";
    `!supported.has(gt) || gt==='implicit'` → 400 `unsupported_grant_type`
    "unsupported grant_type requested"; `!grantTypeAllowed(gt)` → 400
    `invalid_request` "requested grant type is not allowed for this client".
    Static client: no Authorization header → methods ['none',...] →
    'none' matches clientAuthMethod='none' → pass.
  - **checkPKCE** (helpers/pkce.js): `checkFormat(verifier, 'code_verifier')`
    OUTSIDE the try/catch → 400 `invalid_request` "code_verifier must be a
    string with a minimum length of 43 characters" / "...maximum length of
    128 characters" / "code_verifier contains invalid characters" (43-128,
    `[\w.-~]`, pkce_format.js); missing verifier + challenge present →
    InvalidGrant wire; S256: sha256(verifier) b64url === codeChallenge
    (constant-time) else InvalidGrant wire (cause log-only). AUTH-side
    (web static client, `pkceRequired`=applicationType==='native' → FALSE):
    PKCE NOT enforced; if `code_challenge` PRESENT → validated: method absent
    → defaults S256; method ≠ S256 → invalid_request "not supported value of
    code_challenge_method"; format → invalid_request.
  - **#getInteraction** (provider.js:264-283): reads the `_interaction`
    COOKIE (not URL uid) → missing → `SessionNotFound` "interaction session
    id cookie not found" (extends InvalidRequest → wire invalid_request);
    Interaction.find(cookieId) missing → "interaction session not found";
    `interaction.session?.uid` → Session.findByUid missing → "session not
    found"; accountId mismatch → "session principal changed". Bridge AND
    resume resolve the interaction FROM THE COOKIE; the mint sets BOTH
    cookies to the SAME uid (interactions.js:120-135); resume.js uses the
    `_interaction_resume` cookie + `cookieId === interaction.uid` cross-check.
  - **interactionFinished** (provider.js:242-248): `303 + Location=<absolute
    returnTo> + Content-Length: 0 + EMPTY body` (raw res.end()).
  - **revoke.js cascade** (consumed replay at /token, revokeGrantPolicy true
    for non-revocation routes): `[AccessToken, AuthorizationCode]
    .revokeByGrantId` + `Grant.adapter.destroy(grantId)` — RefreshToken/
    DeviceCode/BA/PAC NOT in cascade (static client grantTypes single).
- Go engine layout (user-requested split; each sub-file compiles against
  the 482-line core): `oidcengine_auth.go` (opAuthorize + 400 renderError
  HTML + error redirect), `oidcengine_resume.go` (15-step resume),
  `oidcengine_bridge.go` (3 bridge handlers + consent HTML +
  MongoOidcUserSource), `oidcengine_token.go` (opToken + opSignIdToken
  NO-typ + cascade).

### 2026-09-30 (session 15 — engine sub-file split: auth + resume GREEN; token + bridge pending)
- **Split landed (user ask: "split oidcengine.go into sub-file if possible and work iteratively")**: core `oidcengine.go` (490, compiles standalone: engine struct + opOpaqUID/opEpoch + opSave/opFind/opConsume/opDestroy + opSess + cookie helpers + 5 opErr constructors + opInvalidRequest/GRANT/ACCESSDENIED/INVALIDCLIENTAUTH/UNSUPPORTEDGRANTTYPE + opRenderTokenError/opRenderAuthError wire handlers). `oidcengine_auth.go` (518, GREEN: opAuthorize full vendor chain + opPromptPolicy + mint/mintInteraction/opMintCode + opGrantDoc/FromGrantDoc + opAuthParams/opParseAuthParams + opErrRedirect + opRenderErrHTML). `oidcengine_resume.go` (387, GREEN: opAuthCtx + opAuthLoadGrant (vendored 3-way grant guard, nil error = silent continue) + opAuthRespond (vendor respond.js: prompt→302 interaction, no-account AccessDenied, no-scope AccessDenied) + opAuthTail (shared opAuthorize/resume tail: loadAccount nil-account no-throw → loadGrant → policy → respond) + opAuthResume 15-step (cookie uid→interaction uid→doc→stored session uid→result.error map→interaction.destroy→clear cookie→result.login→!sess.new resetIdentifier)). `opAuthorize`'s tail now DELEGATES to `opAuthTail` (opAuthCtx{result: nil}), resume passes full ctx — shared tail per vendored chain shape. Build: `go build ./go/services/web/...` GREEN (no token/bridge files yet).
- **PKCE S256 algorithm confirmed from jws.go** (NOT from vendor — vendor has no ES256 impl): `sha256.Sum256(signingInput)` over raw `hdr.b64 + "." + body.b64` (base64url encoding of the RAW bytes is applied to the digest bytes; signingInput bytes = concatenated B64 strings), r/s → 32-byte r||s → single base64url. `opSignIdToken` must follow jws.go's exact pattern (same-package helper `jwkPrivateKey` is available from token.go). PKCE S256 challenge verification (token.go): `sha256.Sum256([]byte(code_verifier))` → base64url(digest) == stored code_challenge string (standard S256). Format validation per pkce_format.js (session-14 pins above: minLen/maxLen/charset regex, wire messages byte-for-byte).
- **(f)/(g)/(h) battery wires RESOLVED** (session 14 closure above pins all three). (f) fresh GET = NO `_session` cookie (opSess untouched → sessFinally cookie=false) vs resume WRITE `_session=<sub>` (loginAccount touched) — Go core pins this exactly. (g) cookie paths = `/federation/oidc` (session) + `/federation/oidc/interact/<uid>` + `/federation/oidc/auth/<uid>` (interaction family) — opMintCookies/opMintInteraction/opMintCode in core already emit these. (h) unknown client RENDER 400 (session-13 pin above: InvalidClient render path when no redirect target).
- **NEXT (unchanged order)**: (1) `oidcengine_token.go` (form → client → grant_type → allowOmitting → presence → opACFind WITH checkSessionBinding → isExpired → validateGrant → checkPKCE (format→InvalidRequest wire / mismatch→InvalidGrant wire) → redirect match → consume+replay cascade (opRevokeCascade + opInvalidGrant) → Consume → issueTokens (Users.Account nil→InvalidGrant; mint AT via opSave TTL 3600 IN_PAYLOAD aud/jti/expiresWithSession/scope; opSignIdToken NO-typ; 200 {access_token, expires_in:3600, id_token, scope:"openid", token_type:"Bearer"})). (2) `oidcengine_bridge.go` (opBridgeGet/not-logged-in 302 →/login + postLoginRedirect stash, prompt.login finishLogin, prompt.consent findExistingGrant→303 OR render consent HTML 200, unknown prompt 501; opBridgeConsent (401 if !IsLoggedIn; create Grant via opSave TTL 2592000 + client/account grant SETs; opInteractionFinished merge+save+303 Location); opBridgeDeny (NO login check per vendored bridge.mjs: opInteractionFinished {error:'access_denied', errorDescription:'End-User denied consent'} → 303 resume); MongoOidcUserSource (users.FindOne({_id}) → OidcUserClaims; suspended/unknown → false); opInteractionDoc (cookie→document cross-check per vendored `#getInteraction`: `_interaction` → opFind → sess.Load → sess.AccountID match → else SessionNotFound(400 InvalidRequest wire))). (3) 6 routes in `s4Routes` (oidcprovider_routes.go patterns per session-12 pins above, ALL NoLogin:true, uid param name `uid`, NoCSRF true on 3 POST patterns). (4) `s4b2_test.go` battery (a)—(h). (5) gates → HANDOFF S4 ✅ → commit.
- **(f)/(g) battery distinction RESOLVED (phantom blocker)**: battery (f) is "fresh auth GET emits NO `_session` cookie + resume writes it", (g) is "cookie paths (Path=/federation/oidc{,/interact/<uid>,/auth/<uid>})" — NOT an expired-vs-missing AC wire distinction. Vendor `actions/grants/authorization_code.js:44` (re-verified byte-for-byte this session): `if (code.isExpired) throw new InvalidGrant('authorization code is expired')` — **both** missing AC (findGrantSource → `InvalidGrant('authorization code not found')`) and expired AC → wire `{error:'invalid_grant', error_description:'grant request is invalid'}`; per-cause message (`'authorization code is expired'`) → `error_detail` (LOG ONLY, vendored err_out). No Go-side adapter expiry signal needed. `opACFind` = `opFind("AuthorizationCode", code)` (adapter GET returns "" for both TTL-expired and missing; Go adapter PTTL-driven) → both map to the one wire.
- **Resume route error wrapper = `authError`** (initialize_app.js: `get('resume', routes.authorization + '/:uid', authError, ...resume)` — the SAME `authorization_error_handler.js` wrapping `authorization` and `resume`): on throw, if `params.client_id` + `oidc.client` + `params.redirect_uri` + `err.allow_redirect` → 303 redirect with ordered out (error → error_description? → state? → iss); else **render oops! HTML** 400. `AccessDenied` allow_redirect TRUE, `InvalidClient` TRUE, `InvalidRedirectUri` FALSE (render even when all present), all vendor OIDCProviderError default allow_redirect true. So the deny bridge (bridge → interactionFinished → resume → throw `{error:'access_denied'}`) lands `?error=access_denied&error_description=End-User%20denied%20consent&state&iss` at 303 when the stored params carried client_id+redirect_uri (they do) — this is the battery (h) path pinned in §6 bullet 4.
- **`getResume` resume flow vendor re-verified byte-for-byte** (actions/authorization/resume.js): 15-step — cookie `_interaction_resume` (cookieId) → miss → `SessionNotFound('authorization request has expired')`; `Interaction.find(cookieId)` → miss → `SessionNotFound('interaction session not found')`; `cookieId !== interactionSession.uid` → `SessionNotFound('authorization session and cookie identifier mismatch')` (note vendor's actual message); `interactionSession.session?.uid && !== session.uid` → `SessionNotFound('interaction session and authentication session mismatch')` (vendored message — Go core pins 'session principal changed' from session 13, **keep vendor string on wire**); result.login vs session.accountId mismatch → form_post end_session_confirm (OUT OF SCOPE for Go battery — rpInitiatedLogout disabled in overleaf-fed createProvider.mjs; skip); `interactionSession.destroy()`; `ctx.oidc.params = storedParams` + trusted + redirectUriCheckPerformed=true; clear `_interaction_resume` cookie (path=/federation/oidc/auth/<uid>); result.error → throw `errorMap[className](result.error_description)` (AccessDenied for `access_denied`); result.login → `session.loginAccount({accountId, loginTs, amr, acr, transient:!remember})`; `if (!session.new) session.resetIdentifier()`; next → auth chain (loadAccount → loadGrant → interactions → respond). Go pin: **resetIdentifier UNCONDITIONAL when `!session.new`** (vendored `if (!session.new)`), session.new = cookie absent; fresh-OP cookie case = new=true → no resetIdentifier.
- **`loadAccount` (actions/authorization/session.js)**: `if (accountId) entity('Account', validateAccount(findAccount(ctx, accountId)))` — NO throw on findAccount null (vendored just doesn't populate the entity). Go: `e.Users.Account(ctx, accountId)` → nil = no-op continue, NOT an error.
- **`loadGrant` (actions/authorization/session.js:30-50)**: `if (ctx.oidc.account)` → `loadExistingGrant` (overleaf-fed: UNSET → falsy) → `grant = new Grant({accountId, clientId})` (in-memory, unsaved until bridge consent) → `entity('Grant', grant)`. No accountId → no grant entity (policy then throws AccessDenied 'no session' — no wait: `checkOpenidScope` + interactions.js `throw new errors.AccessDenied(undefined, 'authorization request resolved without requesting interactions but no account id was resolved')` when !session.accountId). **The no_account guard is in interactions.js after policy** (line: `if (!prompt)` branch — NOT a standalone policy prompt).
- **Interactions policy `if (!prompt)` branch** (interactions.js:48-59): `if (!oidc.session.accountId) throw AccessDenied(undefined, 'authorization request resolved without requesting interactions but no account id was resolved')`; `if (!oidc.grant.getOIDCScopeFiltered(oidc.requestParamOIDCScopes) && every resource !getResourceScopeFiltered && !params.authorization_details)` → `AccessDenied(undefined, 'authorization request resolved without requesting interactions but no scope was granted')` — **overleaf-fed policy has only login + consent prompts; no scope param + no grant → this second check → AccessDenied** (battery: scope 'openid' always present in the dance, so only the first AccessDenied fires on fresh login).
- **interactionFinished + interactionDetails** (provider.js:239-257 vendor byte-for-byte): `interactionResult` → interaction merge + `interaction.save(interaction.exp - epochTime())` (TTL refresh from original iat → **opSave MUST re-use stored exp, not mint a new one**); returnTo = `interaction.returnTo` (stored at mint: `oidc.urlFor('resume', {uid})` = `<issuer>/auth/<uid>` — **issuer is portless**, so returnTo = `e.Issuer + "/auth/" + uid` ✓ core already pins this). `interactionFinished` → 303 + Location + Content-Length: 0 + res.end() (vendor `res.end()` after Content-Length:0 → Go: `res.W.WriteHeader(303)` + `res.W.Header().Set("Location", returnTo)` + NO body write — Go auto-emits CL:0 on 303/NoContent; use `res.W.WriteHeader(303)` then set Location header, call NoContent to be safe). `interactionDetails` → `#getInteraction.call(this, req, res)` (vendor provider.js:426 — **the cookie-driven cross-check Go core pins: `_interaction` → find → Session.findByUid → accountId match**).
- **`interactionDetails` vendored `#getInteraction`** — Go's `opInteractionDoc` must do the vendor cross-check (not just a raw GET): `_interaction` cookie → Interaction.find(cookieId) → `interaction.session?.uid` → `Session.findByUid` → `session.uid === opUID` match → else `SessionNotFound` (400 invalid_request wire per errors.js SessionNotFound extends InvalidRequest, description = specific message). Battery: B is logged in → session uid IS known (from GET interact after login), so cross-check passes when the session is loaded with accountId.
- **NEXT (unchanged)**: rewrite `oidcengine_auth.go` (core API + 15-check chain + render oops!/opAuthRedirect helpers), `oidcengine_resume.go` (15 steps above), `oidcengine_bridge.go` (3 handlers + consent HTML + `MongoOidcUserSource` + `opInteractionDoc` cross-check), `oidcengine_token.go` (form → client → grant_type → allowOmitting → presence → opACFind WITH checkSessionBinding → isExpired → validateGrant → checkPKCE → redirect match → consume/replay-cascade → issueTokens → 200), 6 routes in `s4Routes`, `s4b2_test.go` battery (a)—(h), gates, HANDOFF S4 → ✅, commit.

### 2026-09-30 (session 13 — S4b-2/3: oracle closure + core rewrite landed)
- **Core `oidcengine.go` REWRITTEN CLEAN** (471 lines, compiles
  standalone): TTLs, opOpaqUID, oidcSiteOrigin, opErr + constructors,
  cookie helpers, doc ops, IN_PAYLOAD, pickPayload, client surface, opSess
  session model (load/reset/loginAccount/sessFinally). Added `opInvalidClient`
  (NoClient field for the noclient render path). `oidcengine_auth.go`
  (210 lines) on disk = prior-generation draft with ~10 dead identifiers —
  **full rewrite against the core API is step 1**; core build-RED until
  then (only source errors).
- Pins VERIFIED this session (byte-level, vendored tree):
  - `allowOmittingSingleRegisteredRedirectUri: TRUE` (defaults.js)
    → auth: `oneRedirectUriClients` auto-fills params.redirect_uri when
    omitted and client has exactly one registered URI; TOKEN: same rule in
    `actions/grants/authorization_code.js` (presence check AFTER the fill).
  - `renderError` (defaults.js:472) = FULL functional default (NOT a stub):
    `shouldChange` only LOGS a warning (mustChange logs warning too —
    neither throws). overleaf-fed does NOT override it (grep: zero hits).
    → wire = `<!DOCTYPE html>` page, `<title>oops! something went wrong</title>`,
    h1 "oops! something went wrong", one `<pre><strong>KEY</strong>: VALUE</pre>` per
    out entry (htmlSafe). Go engine implements the Go equivalent.
  - `pkceRequired` (defaults.js:312) = `client.applicationType === 'native'`
    → FALSE for web static clients → auth-side does NOT auto-inject a
    code_challenge; PKCE params, if present, are still validated (method,
    43-128 format). Client applicationType default = 'web' (client_defaults
    + vendored Client constructor).
  - `models/base_model.js` getValueAndPayload: iat/exp PRESERVED on re-save
    (only minted when absent) → Grant/Session re-saves keep original
    iat/exp (opSave payload must be the original doc map, NOT regenerated).
  - Check class (helpers/interaction_policy/index.js) semantics: the
    `Check` constructor shifts a 2-arg `(condition, error)` ctor to a 1-arg
    check fn; policies are lists of Check instances with `.condition()` and
    `.REQUEST_PROMPT`/`NO_NEED_TO_PROMPT` flags; login `no_session`
    (condition: `!accountId` → REQUEST_PROMPT); consent `op_scopes_missing`
    (requested set minus grant.getOIDCScopeEncountered → REQUEST_PROMPT,
    details missingOIDCScope).
  - `#getInteraction` (provider.js:264-283) — FULL re-read this session:
    cookie `_interaction` → Interaction.find(cookieId) → `interaction.session?.uid
    → Session.findByUid` → accountId match. Bridge AND resume use it; the
    mint sets BOTH cookies to the SAME uid.
  - grant_source.js: `findGrantSource` = AC.find(code, {
    ignoreExpiration:true}) → client-id cross-check → wire 'client
    mismatch' (log/wire desc = class 'grant request is invalid'; per-cause
    'client mismatch'/'authorization code not found' etc. are error_detail). `consumeGrantSource`: consumed → revoke + 'already
    consumed'; else source.consume() (adapter flag).
  - `checkSessionBinding` surfaces AT the `AC.find` static (NOT in
    grant_source) — the token path finds the AC through it, so an
    AC with expiresWithSession + wrong accountId/grantId resolves as
    'not found' → wire 'grant request is invalid'.
  - Token response scope: vendor `grant_response.js` buildTokenResponse —
    `scope: source.scope ? at.scope : (at.scope || undefined)` → wire
    `"scope": "openid"` (AC always has scope for the dance).
  - errors.js E factory + ALL class wires re-verified byte-for-byte
    (see session-8 bullet above; no deltas).
- Battery (a)-(h) detail for s4b2_test.go (per the §6 next-action 6 pin
  already on record).
- NEXT (this arc): rewrite `oidcengine_auth.go` (core API, all checks in
  vendor order), `oidcengine_resume.go`, `oidcengine_bridge.go`,
  `oidcengine_token.go` (sub-file split per user request), 6 routes in
  `s4Routes`, s4b2_test.go battery (a)-(h), gates, HANDOFF S4 → ✅,
  commit (stage engine + routes + test only).

### 2026-09-30 (session 16 — S4b-2/3 COMPLETE: token + bridge sub-files, 6 routes, battery (a)-(h) GREEN)
- **This session completed the S4b-2/3 engine sub-files and battery.** Fixed the one build error (bridge `ObjectIDFromHex` v1 `(ObjectID, error)` shape). Wrote `oidcengine_token.go` (opToken full chain: form → client(401 invalid_client) → grant_type → allowOmittingSingleRegisteredRedirectUri → presence → opACFind WITH checkSessionBinding (three-way accountId + grantIdFor via sub:<uid>) → isExpired → validateGrant → opCheckPKCE (format→invalid_request wire / S256→invalid_grant wire) → redirect match → consume+replay (opRevokeCascade: AT + AC + Grant.destroy + uniform invalid_grant) → issueTokens (Users.Account nil→invalid_grant; mint AT TTL 3600 IN_PAYLOAD; opSignIdToken NO-typ ES256; 200 {access_token, expires_in:3600, id_token, scope:"openid", token_type:"Bearer"}). Wrote `oidcengine_bridge.go` (opBridgeGet not-logged-in 302 →/login + postLoginRedirect stash / prompt.login finishLogin merge / prompt.consent FindByAccountAndClient silent-reuse 303 OR renderConsentHTML 200 TWO <form class="consent-form">, unknown prompt 501; opBridgeConsent 401-if-not-logged + create Grant TTL 2592000 + client/account SETs + opInteractionFinished merge+save+303; opBridgeDeny NO login check + access_denied 303 resume; MongoOidcUserSource (users.FindOne({_id}) → OidcUserClaims; suspended/deleted/unknown → false); opInteractionDoc cookie→document→session.findByUid→accountId cross-check per vendored #getInteraction). 6 routes appended `s4Routes` (oidcprovider_routes.go) all NoLogin + NoCSRF. `s4b2_test.go` battery (a)-(h) + full A→B dance GREEN.
- **Bug found + fixed (engine, reproducible panic in battery)**: `renderConsentHTML` did `details["missingOIDCClaims"].([]any)` + `details["missingOIDCScope"].([]any)` UNCONDITIONAL type assert on the minted prompt details — but `opPromptPolicy` mint `missingOIDCScope` only (NO `missingOIDCClaims` key) → `nil.([]any)` PANICS on every consent render. Vendored oracle (interaction_policy/prompts/consent.js:184-185 + bridge.mjs) is `details?.missingOIDCScope || []` + `details?.missingOIDCClaims || []` (optional chaining + default). Fixed with `if v, ok := details[...].([]any); ok` on BOTH — the engine was never caught by live-smoke because the first bridge GET rendered the SILENT-303 (the account already had a grant), NOT the 200 consent-form path (a first-time consent is exactly what the dance hits).
- **Gates GREEN**: `go build ./go/services/web/...` + `go vet ./go/services/web/...` + `gofmt -l` empty + `go test -race -count=1` (35 federation tests: S1 9 + S2 8 + S3 5 + S4 7 + S4b1 4 + S4b-2/3 7 — ALL PASS).
- **Battery (a)-(h) result — (b) nonce clarification**: the vendored `authorization_code` redirect (vendored respond `code` + `state?` + `iss` + AD_ACTA) carries `code` + `state` + `iss`; `nonce` is NOT re-rendered into the redirect (it rides the AC doc + id_token; the A-side `RPInitiated` nonce is recovered from the AC, not the redirect query). Test pins `nonce` absent in the redirect query. (e) silent-reuse grant: opBridgeGet prompt.consent mint a fresh consent Interaction + opGrantDoc{AccountID,ClientID} (NO grantId) → bridge POST consent mints a Grant via opSave TTL 30d + client/account SETs → opAuthTail loadGrant via `opGrantDoc{AccountID, ClientID}` (NO scope) → `opPromptPolicy` sees `grantScope=""` + `grant.ID=""` → `opPromptPolicy` `requested OIDC scopes = ["openid"]`, `encountered={}` → missing=["openid"] → prompt again (correct: first-time consent, NOT silent-reuse). On RE-visit the opBridgeGet FindByAccountAndClient hit → 303 silent (no consent form). (f)/(g) cookie paths minted by opMintInteraction (`_interaction` Path=/federation/oidc/interact/<uid>, `_interaction_resume` Path=/federation/oidc/auth/<uid> Max-Age 600) + `_session` (Path=/federation/oidc, Expires 8h) only when loginAccount touched (fresh GET /auth = NO _session cookie; resume WRITEs it). (h) unknown client RENDER 400 oops! + deny-redirect wire (access_denied + End-User denied consent + state + iss → 303 redirect_uri?error&error_description&state&iss).
- S4 slice status → **✅ COMPLETE** (S4a adapter + S4b-1 provider core + S4b-2/3 engine + 6 routes + battery all green). Next slice: S5 (S2S admin revoke → `killOutstandingCodes` = `RevokeClientCodes`, session-9 plan). Takeover point: read this HANDOFF, then the §6 CURRENT POSITION.

## 6. CURRENT POSITION (takeover point)

**Where:** S4 COMPLETE (session 16). Engine sub-files all landed: `oidcengine.go`, `oidcengine_auth.go`, `oidcengine_resume.go`, `oidcengine_token.go`, `oidcengine_bridge.go` + 6 routes + `s4b2_test.go` battery (a)-(h) + full A->B dance GREEN. S4 slice status = DONE. Next slice: **S5** (S2S admin revoke -> `RevokeClientCodes` = `killOutstandingCodes`, session-9 plan).

**State:**
- HEAD: `806d84b4` (last committed slice before S4); working tree: 5 engine sub-files + `oidcprovider_routes.go` (6 routes) + `s4b2_test.go` + HANDOFF.md (new/modified this session).
- **Gates: GREEN** - `go build ./go/services/web/...` + `go vet ./go/services/web/...` + `gofmt -l` empty + `go test -race -count=1` -> 35 federation tests (S1 9 + S2 8 + S3 5 + S4 7 + S4b1 4 + S4b-2/3 7) ALL PASS.
- Oracle recon: COMPLETE (sessions 5+6+7+8+14+15+16; 15/16 pins win over 13/earlier where they conflict).

**Resume order (S5, per session-9 plan):**
1. S5 S2S admin revoke -> `RevokeClientCodes` (killOutstandingCodes): wire the S4a-tested `OidcAdapter.RevokeClientCodes(clientID)` into the S2S admin revoke handler; session doc + client sweep index trims (06 S174-178 pins).
2. Battery for S5 (not over-cross: second client's tokens survive; idempotent second sweep = 0; consent Grant NOT swept - S4a adapter already has `TestRevokeClientCodesKillOutstanding`).
3. Gates: build + vet + gofmt + `go test -race -count=1`; HANDOFF S5 -> DONE; commit.

**(carried, completed in session 16) session-15 NEXT block:** (1) `oidcengine_token.go` DONE (2) `oidcengine_bridge.go` DONE (3) 6 routes in `s4Routes` DONE (4) `s4b2_test.go` battery (a)-(h) DONE (5) gates + HANDOFF S4 DONE + commit (this step, stage engine + bridge + token + routes + test + HANDOFF).

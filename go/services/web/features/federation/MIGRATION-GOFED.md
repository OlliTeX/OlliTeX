# Migration — hand-rolled OIDF trust layer → `go-oidfed/lib`

**Owner decision (session 6, 2026-09-26):** replace the hand-made OIDF layer
(`jws.go`, `leaf.go`, `clientassertion.go`, keystore in `models.go`) with the
community-maintained `go-oidfed/lib` (MIT, cloned at `~/federation/lib`).
Goals are UNCHANGED — this substitutes implementation of S3 (+S10 RP fetch),
not scope. OffA, LightHouse, and resolve-browser are surveyed and **NOT
adopted** (separate proxy/TA-tool products, irrelevant to OlliTeX).

## 1. Why / what changes

- Today: ~900 LOC of stdlib OIDF (ES256 JWS, RFC 8550 kid thumbprint, keystore
  rotation, leaf EC, S2S assertion build/verify, HistoricalKeySetPayload).
  Oracle-pinned in S3 against npm @oidfed/core v1.0.0 (`testdata/`).
- After: the same surfaces are provided by `go-oidfed/lib` — community
  maintained, same spec target (OIDF **Final 1.0** wire, verified 2026-09-26
  from lib source §3 below).
- Untouched: S2S actions (S5), B-side OP engine (S4, stays hand-rolled v9
  port), SSO/SAML (S6–S9), invite/export (S11), admin (S12).

## 2. Library inventory (cloned, v0.11.2, HEAD tag v0.9.x + master commits)

| Repo | Module | Used? |
|---|---|---|
| `~/federation/lib` | `github.com/go-oidfed/lib` | **YES** — the OIDF trust layer |
| `~/federation/offa` | forward-auth proxy | NO |
| `~/federation/lighthouse` | TA/IA/issuer tool (56k LOC) | NO |
| `~/federation/resolve-browser` | dev tool | NO |

## 3. Verified wire facts (lib v0.11.2 source, 2026-09-26)

| Item | lib (OIDF Final 1.0) | Our S3 (npm v1.0.0 oracle) | Match |
|---|---|---|---|
| well-known | `/.well-known/openid-federation` | same | ✅ |
| entity type OP | `openid_provider` | check 16: `openid_provider` | ✅ |
| metadata keys | `openid_provider{issuer, authorization_endpoint, token_endpoint, ...}` (metadata_generated.go) | same names | ✅ |
| top-level endpoints | `federation_fetch_endpoint`, `federation_list_endpoint`, `federation_resolve_endpoint`, `federation_trust_mark_*`, `federation_historical_keys_endpoint` (+ `_auth_methods`) | npm: fetch/list + historical keys | ✅ superset |
| content types | `application/entity-statement+jwt`, `jwk-set+jwt`, `jwk-set+json`, `trust-chain+json`, `resolve-response+jwt` (oidfedconst) | npm: entity-statement + jwk-set | ✅ |
| signed JWKS §5.2.1 | `SignedJWKS` / `ParseSignedJWKS` (structural: typ/kid/keys/iss/sub, `Verify`) | our HistoricalKeySetPayload | ✅ |
| kid | `jwx.SignPayload` → `jwx.AssignKeyID` (jwx re-export) — RFC 8550 must be confirmed (D-G1) | RFC 8550 thumbprint (oracle-pinned) | ⚠ D-G1 |
| S2S assertion | `RequestObjectProducer.ClientAssertion(aud, alg...)` → `{iss=sub=entity id, iat, exp, aud, jti}` | Node assertion (overleaf spec: `iss/sub` may differ — diff claims in M-WIRE) | ⚠ M-WIRE |
| RP metadata fetch | `FederationLeaf.ResolveOPMetadata` / `FetchOPMetadataFromOIDF` (own trust resolver) | we roll our own in S10 (fetch JWKS + verify) | ✅ replaceable |
| key rotation | `kms.KeyManagementSystem` (RotateKey/RotateAll/StartAutomaticRotation/ChangeAlgs), `KeyRotationConfig` (overlap, announcement lead) | models.go keystore bootstrap/rotation | ✅ |
| public key store seam | `public.PublicKeyStorage` interface + `kms.KMSToVersatileSignerWithPKStorage` | our Mongo-backed store implements it | ✅ clean seam |
| trust chain | `TrustResolver` + `TrustAnchors` (TA pins) + constraints (policy operators) | we do depth-1 only | ✅ superset (we can skip chain walk) |
| content-type constants | `oidfedconst.ContentType*` (exact Final strings) | we hardcode | ✅ |
| **HTTP fetch contract** | custom `httpClient`/`performFetch` seam (FINDINGS-verified, injectable) | net/http stdlib | ✅ no forced resty |
| Go version | `go 1.26.0` required (jsonv2 experiment, zerolog, jwx v4) | ours: 1.27 | ✅ |

**Dependency additions** (lib `go.mod`): `lestrrat-go/jwx v4`, `resty v2`,
`gocache v2` (in-mem cache, NOT redis — lib's redis entry is for tooling),
`zerolog v1.35`, `fatih/structs`, small utils. **fiber does NOT leak** unless
the default metadata-resolver HTTP path is used — we inject our stdlib fetcher
(see D-G2). New deps must pass the project's third-party review gate.

## 4. File map (Go package)

| Current file | After migration |
|---|---|
| `jws.go` | **DELETE** (ES256 sign/verify → lib `jwx`/`jws` via signers). KEEP: b64url helpers + UUIDv4 if used outside OIDF. |
| `leaf.go` | endpoints/entityId/origin/clientId **kept** (overleaf wire ids). Leaf EC build → lib `NewFederationEntity/Leaf` + `EntityConfigurationJWT`; check 16 becomes lib metadata equality test. |
| `clientassertion.go` | `BuildS2sRequest` → `RequestObjectProducer.ClientAssertion` (via M-WIRE claim diff — overleaf assertion uses `urn:overleaf-federation:client:<origin>` iss/sub, lib sets `iss=sub=entity id`; keep an adapter layer that writes overleaf's claim set, or adopt lib's claim set and re-pin S2S wire). `VerifyS2sClientAssertion` → lib verify against anchor JWKS + jti replay. `HistoricalKeySetPayload` → `SignedJWKS`. |
| `models.go` (keystore) | **kept as storage**; rotation state machine → lib `kms` (config-driven overlap/announcement, grace sweep). KMS impl = our Mongo-backed `public.PublicKeyStorage`. |
| `anchor.go` | **kept** (runtime depth-1 pin verify stays ours — lib's `TrustAnchors`/chain-walk optional, see D-G3). |
| new `oidcfed.go` | adapter file: KMS→lib signer wiring, our Store→lib storage, fetcher injection, claim adapter. |
| tests `s3_test.go` | RE-PIN: round-trip through lib (sign→verify, kid stable, claim equality, cross-verify Node ca.txt against lib verifier and vice-versa). Keep npm fixture files verbatim. |

## 5. Slices + gates (after current S3 state; S4 OP engine NOT touched)

**Gate per slice** (unchanged): `go build ./go/... && go vet ./go/services/web/... && gofmt -l && go test -race -count=1 ./go/services/web/features/federation/...`.
**Global invariant:** every S3-locked fixture (key.json pub/priv JWKs, ca.txt
S2S assertion, leaf EC payload) must round-trip through the lib AND keep
byte-level kid/jwk equality with the old implementation until M6 flips the
byte-equal check off.

- **M0 — Spike (DONE, commit d6ac2a9e).** go.mod += `go-oidfed/lib v0.11.3-0.20260831-3130444ef6a3` (cloned HEAD); fiber absent from our build graph (`go mod why` → not needed); Mongo-KMS seam shape proven (`libES256Signer`: our JWK priv → `*ecdsa.PrivateKey` → lib `SigningKey`; `SingleKeyVersatileSigner`); lib `RequestObjectProducer.ClientAssertion` (iss/sub/iat/exp/aud/jti, ES256) + lib leaf EC build/parse/verify round-trip (typ `entity-statement+jwt`) both green in `libspike_m0_test.go` (-race). Logger silenced via `oidf.DisableDebugLogging`.
- **M1 — kid (DONE, D-G1 CLOSED).** lib kid (lestrrat jwx v4 `AssignKeyID`, RFC 8550) == npm fixture kid `A2P6TlLJVZBG-wa9pxzHYAtoS-hO6AN7-4eIuK9EHnQ` byte-for-byte AND == our own recompute — three-way pinned on the npm @oidfed/core v1.0.0 fixture (`libm1_kid_test.go`). No override needed.
- **M2 — REDIRECTED (owner decision 2026-09-26, recorded this pass).** The lib `kms.KeyManagementSystem` (RotateKey/RotateAll/StartAutomaticRotation, nbf/exp, key-announcement lead-time) is a DIFFERENT state machine than our Node keystore oracle (`published/active/retiring/revoked` + `Settings.keyRotationGraceDays` grace sweep). Adopting it would silently break S3 keystore tests and change audit wire. Decision: **keystore KEPT as Node-oracle; lib supplies the artifact layer instead** — kid (M1), EC signing (M3), historical JWKS (§5.2.1, M5). The `public.PublicKeyStorage` bridge (our Mongo Store → lib KMS) remains documented for a possible M2b; explicitly NOT wired in this migration.
- **M3 — leaf EC (DONE 2026-09-26).** Wire-preserving: `oidcfed.go` `libEntityStatementSign(payload, priv)` signs OUR marshalled leaf payload with lib `jwx.SignWithType(payload, typ=oidfedconst "entity-statement+jwt", ES256)`; kid = lib RFC 8550 (M1). lib `EntityStatementSigner.JWT([]byte)` REJECTS pre-marshalled bytes ("must be a map") → raw `SignWithType` is the seam (D-G8 quirk #2). s3_test.go leaf-EC pins green unchanged.
- **M4 — S2S assertion (DONE 2026-09-26, D-G4 CLOSED).** Hand-rolled BUILD stays (overleaf wire: `urn:overleaf-federation:client:<origin>` iss/sub, typ JWT, S3-pinned). lib adopted for the VERIFY seam: `libm4_m5_test.go` proves lib-derived kid == our pinned kid on the npm fixture AND a lib `RequestObjectProducer` assertion verifies on our S3-pinned `VerifyJWT` (implementation-interchangeable verify direction).
- **M5 — historical keys (DONE 2026-09-26, D-G8 wire note).** lib §5.2.1 `ParseSignedJWKS`/`SignedJWKS.Verify` (typ `jwk-set+jwt`, kid header, iss/sub/keys — keys MUST carry non-empty unique kids) proven over the npm fixture; it is the TA-side verify path for standard peers' `signed_jwks_uri`. Our SERVED `GET /federation/federation-keys` wire ({iss, iat, keys}, no sub) STAYS Node-shape (peer wire = contract, S3 pins it); D-G8: lib parser requires sub — do not feed our served shape into it.
- **M6 — flip + delete + re-pin (DONE 2026-09-26).** S1–S4 + M0–M5 gates green (`go build`, `go vet`, `gofmt -l`, `go test -race`). Wire pins intact: npm fixtures verify unchanged; S4 OP engine (v9 port) untouched. Remaining live two-instance smoke = S13 gate (not unit-testable here).

**Explicit not-doing:** LightHouses TA/IA/TrustMark endpoints (S12 admin
surface remains overleaf's), OffA proxy (we're the server, not reverse
proxy), resolve-browser (dev only), and **any change to overleaf's S2S wire
claims without owner sign-off** (M4 is a decision point, silent default =
keep overleaf claim set).

## 6. Decisions (owner sign-off per item)

| # | Decision | Status | Default if unaddressed |
|---|---|---|---|
| D-G1 | `AssignKeyID` == RFC 8550 byte-equality (M1) | **CLOSED (libm1_kid_test.go)** | n/a |
| D-G2 | custom fetcher (no resty/fiber) | **CLOSED (M0: fiber not in build graph)** | n/a |
| D-G3 | use lib `TrustAnchors` chain-walk at pin-time (currently Node-only) | open — scope expansion only if yes | keep depth-1 (anchor.go) |
| D-G4 | S2S assertion claim set: keep overleaf-spec (M4 revision, 2026-09-26) | **CLOSED (default: wire contract wins; lib build would change peer wire)** | n/a |
| D-G5 | lib version pin | **CLOSED (pseudo v0.11.3-0.20260831 at cloned HEAD 3130444)** | n/a |
| D-G6 | dep closure approval (jwx v4, resty, zerolog, gocache, fatih/structs) | **CLOSED via M0 gate (added to go.mod, `go.sum`)** | n/a |
| D-G7 (new) | keystore redirect: keep Node keystore, lib supplies artifact layer (M2 revision) | **CLOSED (owner 2026-09-26)** | n/a |
| D-G8 (new) | lib quirks: (1) `SingleKeySigner.JWKS()` ES512-pin upstream bug (M2b if ever wired must avoid it); (2) `EntityStatementSigner.JWT(bytes)` rejects pre-marshalled payload bytes — use `jwx.SignWithType(payload, typ, alg, key)` for raw payload wires | **CLOSED (recorded 2026-09-26; M3 uses SignWithType)** | n/a |

## 7. Test & interop gates

- **Unit (re-pin, same fixtures):** S3 suite now asserts lib produces
  identical kid/JWKs and accepts our npm fixtures.
- **Two-instance (S13, unchanged contract):** lib-signed A ↔ lib-verified B
  and Node-produced ca.txt ↔ Go verifier.
- **Rollback:** no prod file is deleted until M6; revert = single commit back.

## 8. Estimated effort

| Slice | Est. (1 session ≈ 1 green slice + tests) |
|---|---|
| M0 spike + gate | 0.5 |
| M1 kid | 0.25 |
| M2 keystore | 1 |
| M3 leaf | 0.5 |
| M4 S2S assertion | 1 |
| M5 historical keys | 0.5 |
| M6 flip/delete/gates | 1 |
| **Total** | **~4.75 sessions** (after S4b current work completes)

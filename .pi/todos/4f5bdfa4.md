{
  "id": "4f5bdfa4",
  "title": "SSO: allow multiple providers (SAML/OIDC/LDAP) per reference sso@fe4ceb6",
  "tags": [
    "psintern",
    "admin-settings",
    "sso",
    "federation",
    "multi-provider"
  ],
  "status": "closed",
  "created_at": 1791511305704,
  "last_change": 1791551536528,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

Owner request (2026-10-09). The federation module's SSO surface should allow **multiple SSO providers** per instance — that capability is currently missing.

**Reference implementation (original code)**: `/data_1/image_mining/the_diff/sso` at commit `fe4ceb6ba20a9e07cc3e954134988b3bf720d157` ("Initial files"). Key files to study/mirror:

- `services/web/modules/authentication/ssoConfigLoader.mjs` (multi-provider config loading)
- `services/web/modules/authentication/oidc/{index.mjs,app/src/OIDCModuleManager.mjs,OIDCAuthenticationController.mjs}` (multi-OIDC support)
- `services/web/modules/authentication/saml/{index.mjs,app/src/SAMLModuleManager.mjs,SAMLAuthenticationController.mjs}` (multi-SAML)
- `services/web/modules/authentication/ldap/{index.mjs,app/src/LDAPModuleManager.mjs}` (multi-LDAP, incl. searchAttributes parity with passport-ldapauth 3.0.1)
- `services/web/modules/authentication/admin/app/src/SSOAdminController.mjs` + `SSOAdminRouter.mjs` + `views/sso-admin.pug` (admin UI: add/list/disable multiple providers, editable timeouts)
- `services/web/modules/authentication/emailConfigLoader.mjs` + `EmailAdminController.mjs` (companion email settings)
- `server-ce/bin/emergency-enable-local-login.sh` (safety valve)
- `services/web/app/views/user/login.pug` (login page listing providers)

TODO scope:
1. Diff what the OlliTeX federation/SSO section (admin-settings site.sso: "SSO (SAML / OIDC / LDAP with editable timeouts)") currently supports (appears single-provider) vs the multi-provider model in the sso checkout.
2. Port/restore the missing multi-provider functionality into the OlliTeX admin-settings SSO section + backend config store (provider list, per-provider enable, per-provider endpoints/keys/timeouts, add/remove provider, login page shows all enabled providers).
3. Keep the emergency local-login path working (parity with the reference script).
4. Verify on psintern: at least two providers (e.g. two OIDC or SAML+OIDC) configured, login page lists both, each provider selectable, disabling one leaves the other working.

Dev server: psintern.neuro.uni-bremen.de; reference code: /data_1/image_mining/the_diff/sso @ fe4ceb6.

**CLOSED 2026-10-09 — evidence (deployed image g-2026-10-09-v12):**
- Admin UI: `/admin-settings/site.integrations.sso-providers` — multi-provider manager (OIDC/SAML/LDAP sections, add/remove/reorder, enable toggles, per-provider fields, test-connection, LDAP searchAttributes, local-login safety-valve note, secret sentinel round-trip). Verified rendering + Add + Save + login-page button on v9c; unchanged frontend in v10–v12.
- **Three real bugs found via live verification and fixed:**
  1. `randID()` int64 overflow → `index out of range [-5]` panic → HTTP 500 on every POST /admin/sso/provider. Fixed with crypto/rand.Read + safe modulo; regression test `randid_test.go` (2000 iterations, format + collision).
  2. **Clobber bug (critical):** mongo-driver v2 decodes stored doc-arrays as NAMED type `bson.A` with `bson.D` elements; every old `.( []any )` / `.( bson.M )` assertion silently failed → "Add provider" ReplaceOne'd a ONE-element list (wiped all earlier providers), deleteProvider wiped the list, GET masking + POST secret-restore loops never ran. Fixed with reflection-based `providersField` + `toMap` now accepting `bson.D` (live-shape captured from ollitex-mongo). Regression tests `providers_field_test.go` incl. the exact live shape.
  3. Disabled providers were still routable by explicit id (302 instead of 404) — `samlProviderByID`/`oidcProviderByID` now require `enabled`.
- **Live matrix (v12, all PASS):** add A+B → both survive (GET returns both); GET `/saml/login/<A>` → 302 `https://IDPA-HOST/...?SAMLRequest=...`; GET `/saml/login/<B>` → 302 `https://IDPB-HOST/...`; unknown id → 404 "not found or disabled"; disable A → 404 while B still 302; login `/login` slot lists exactly the enabled providers, order-sorted, env FB1 button always present (EXTERNAL_AUTH=oidc env provider `https://sso.fb1.uni-bremen.de`); OIDC clientSecret set → GET masks `••••••••` → save with sentinel echo → real value retained (GET still masks ⇒ non-empty stored); DELETE one provider → others untouched; test-endpoint returns clean 200 diagnostic JSON for unreachable IdP; final state: providers=0, login page = FB1-only. Local email/password login works in every session (safety valve intact).
- Go: `go build ./go/services/web/...` + `go vet` + `go test ./go/services/web/features/sso/...` all green (one unrelated pre-existing env-dependent failure in projectinspection: needs `node` binary in the test container — not present in the golang test image; the full app image has node).

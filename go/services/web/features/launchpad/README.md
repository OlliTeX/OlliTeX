# `launchpad` feature — first-admin bootstrap (P6.20)

The Go port of `services/web/modules/launchpad` (the "fresh instance" first-admin
flow) — the last P6 module flip. Oracle-pinned against the live Node service
(captures in `tools/capture-p620-raw/`, battery in
`tests/e2e/specs/parity/web-go-p620-flip.test.e2e.ts`).

## Routes (5)

| method + path | login state | behaviour |
| --- | --- | --- |
| `GET /launchpad` | anon / logged | admin page (200) if a site-admin is logged in; fresh page (200) if **no** admin exists yet; anon+admin-exists → `302 /login`; non-admin logged → `302 /restricted` |
| `POST /launchpad/register_admin` | anon | create the first local admin; exact Node gate order: 400 empty → 403 admin-exists → 400 email → 400 password → 200 `{"redir":"/launchpad"}` (500 view on duplicate non-holding email) |
| `POST /launchpad/register_ldap_admin` | anon | external-auth (LDAP) admin; 403 unless the fork `authMethod()` is `"ldap"` (it is — the SSO `authentication/ldap` module always loads); then 400 empty → 403 admin-exists → 200 external-admin doc (no `hashedPassword`, `confirmedAt` ms) |
| `POST /launchpad/register_saml_admin` | anon | 403 `Forbidden` (method gate — `"saml"` is unreachable in this deployment) |
| `POST /launchpad/send_test_email` | site-admin | `core.Mail` test email; 400 no-email → 200 `{"message":"Email Sent"}`; non-admin → `302 /restricted?from=…`; anon bounces via the global login gate |

## Files

| file | what |
| --- | --- |
| `launchpad.go` | `Feature` wiring + the 5 handlers (Node-exact gate order in the comments) + 500 path |
| `validation.go` | `parseEmail`/`validateEmail` (Node `EmailHelper.parseEmail` + `InvalidEmailError` text) |
| `password.go` | `validatePassword` (min 8/max 72 + char-sets + contains-email + Node `stringSimilarity` multiset ratio > 0.7 with the length-exemption) |
| `users.go` | user creation (local/external) via `registrationpage.NewUserDoc` baseline + `adminExists`/`userByEmail` Mongo primitives |
| `launchpad_test.go` | the validator + similarity + shape pins |

View renderers: `go/services/web/views/pages.go` (`LaunchpadAdminPage` /
`LaunchpadFreshPage`, CSP `cspReact`) over the bakes in
`go/services/web/views/pages_data_p620.go`. Flip conf:
`server-ce/nginx/flips/web-p620.conf`.

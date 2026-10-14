# BUG HUNT REPORT — 2026-10-14

**Mandate (owner):** intensive bug hunt over the project — bugs, security problems, structural problems.
**Method:** (a) full route-table extraction from the Go web plane (322 routes: 181 pattern + 141 static),
(b) per-plane static security reads (authz gates, CSRF wiring, shell sinks, SSRF sinks, file handling,
crypto, session handling, anonymous-mutation surface), (c) **live adversarial probes on ag-v66**
(disposable e2e world, throwaway fixtures, self-clean), (d) structural/dead-code + hygiene pass,
(e) cross-check against the 673-green battery (incl. `ce-idor-matrix` 7 routes × 5 identity classes).

**Scope note:** the Go plane (1329 files / 354k LOC, 18 services) + frontend render surface + e2e
infrastructure were the targets; the world under test is `olpsint/overleaf:ag-v66` at
`/data_1/image_mining/the_diff/overleaf`.

---

## CONFIRMED FINDINGS (live-verified unless noted)

### BH-1 — LLM provider SSRF with content/status oracle — HIGH
**Surface:** `POST /user/llm-providers/check` (and `/scan`) — `llmsettings/client.go:30 assertPublicLlmBaseUrl`.
**Guard as-is:** blocks `localhost/*.local/*.internal/*.lan/0.0.0.0/::1` by *name*, `127/8`, `169.254/16`,
IPv6 ULA/link-local by literal. 
**Gap:** `10/8`, `172.16/12`, `192.168/16` and DNS-resolved private targets pass. The compose network
(172.20.0.0/16) sits inside 172.16/12.
**Live proof (ag-v66, admin session):**
- `http://172.20.0.3:27017` (postgres wire port) → guard PASS, server dials it (`Could not reach … 172.20.0.3:27017/v1/models (dial tcp …)` — reachability proven).
- `http://172.20.0.9:8888` (sw-filer) → `Backend returned 404` (semantic echo).
- `http://172.20.0.11:8025` (smtpsink internal API) → **internal JSON body echoed to the caller**: `Backend returned 404: { "error": "not found" }`.
- `http://172.20.0.10:8333` (sw-s3) → **S3 XML error envelope echoed**: `<Error><Code>NoSuchBucket</Code>…`.
- loopback-by-name and hex-notation attempts blocked/defeated ✓ (guard + resolver).
**Impact:** any authenticated user can fingerprint and read response bodies of internal services on the
container network (and, in production layouts, RFC1918 internal APIs). The in-tree *correct* pattern
already exists for the identical problem: templates remote-import policy
(`tplExternalUrlSection`: allowlist regex + **blocked-networks CIDR list** + per-redirect-hop re-check)
— the LLM client simply never adopted it.
**Recommendation (owner-gated — product change):** apply the templates-style policy to
`assertPublicLlmBaseUrl` (RFC1918 + link-local + loopback + DNS pre-resolution check, per-hop redirect
re-check). Pin the current behavior in specs until decided.

### BH-2 — `POST /internal/alerts` accepts UNAUTHENTICATED alert injection — MED (live-confirmed)
**Surface:** `instancestats/alerts_webhook.go:75` — `NoSession` by design (docker-network sink for the
cron alert relay). Guard: `X-Internal-Token` constant-time compare **only when `INTERNAL_ALERTS_TOKEN`
env is set** — *"Unset → legacy open behaviour (back-compat for images deployed before this guard)."*
**Live proof:** env check → token unset in the e2e world; `POST /internal/alerts` (no auth, crafted
`alerts[]` body) → **200 `{"ok":true,…}` accepted**. Today it no-ops (no recipients configured); once an
admin configures alert recipients, **any network peer can inject arbitrary `alertname`/`summary` into
mail sent to those recipients** (spam/phishing vector from the instance's own domain) and pollute the
alert history. The sibling sink `/internal/collect-instance-stats` is properly gated
(`APIBasicGate` → 302) ✓.
**Mitigation applied (e2e world):** `INTERNAL_ALERTS_TOKEN` set in `tests/e2e/docker-compose.test.yml`
(world hardening, green-lit e2e-local infra) → unauthenticated probe now 401 (verified).
**Owner call (product):** keep open-by-default back-compat (pin) or fail-closed when the env is unset
(product change).

### BH-3 — WebDAV base URL stored with NO SSRF guard (latent; fires on sync) — MED (code-proven)
**Surface:** `webdav/webdav.go hConnect` — `POST /user/webdav/connect` stores the user-supplied base
URL (only `url.Parse` at `webdav/client.go:79`; **no host/range checks at all**); the client then
PROPFINDs/pulls/pushes to it (sync-conflict-webdav legs exercise this path against the live Nextcloud
instance). Live probe: `connect` → `{"success":true}` for an internal target (172.20.0.11:8025);
status/state showed no immediate PROPFIND (the dial happens in pull/push — the sink is latent, the
guard is absent).
**Recommendation (owner-gated):** same range-guard as BH-1 (ideally one shared helper with the
templates policy).

---

## INFORMATIONAL / STRUCTURAL (owner's discretion, no defect claimed)

- **BH-4 (deployment note):** session cookie `overleaf.sid` = `HttpOnly; SameSite=Lax; Path=/` — no
  `Secure` flag (the e2e world is intentionally plain-HTTP per stack comment; for TLS deployments the
  cookie should carry `Secure`). Login **rotates the sid** (fixation-safe, verified).
- **BH-5 (fingerprint):** `Server: nginx/1.30.4`; CSRF 403 body = plain `Forbidden` (no CT). Baseline
  header posture otherwise strong: nonce CSP + `strict-dynamic`, `Referrer-Policy`, `nosniff`; **no
  ACAO** emitted (cross-origin JS cannot read responses — CSRF on JSON mutations leans on the CORS
  preflight, which holds).
- **BH-6 (pinned semantics):** login brute-force → 8×401 faithful then **429 Node-pinned body**
  (redis `rate-limit:login_attempts:<ip>`, 10/60s; LDAP gets dual limitters IP 20/60 + email 10/120).
  Lockout semantics are opaque (no `Retry-After`) — Node-faithful as pinned.
- **BH-7 (hygiene):** `history/zz_live_probe_d_test.go` — dev live-probe test living in the product
  package (self-skipping, harmless); `cmd/sscipher-probe`, `cmd/seaweed-migrate` — in-image dev/ops
  tools (container-level access equivalence only).
- **BH-8 (process rule, recorded):** playwright `testMatch = /.*\.test\.e2e\.ts$/` silently picks up
  any such file under `specs/` — throwaway probes MUST be deleted after run (practiced this session;
  rule now on record) or named outside the glob.
- **BH-9 (pinned design):** federation admin surfaces are `NoCSRF: true` (mitigated by preflight,
  admin-gated, feature-flagged; dormant when federation disabled).

---

## CLEARED SURFACES (close-read or live-probed — NOT findings)

| Surface | Verdict / evidence |
|---|---|
| IDOR: library references (create/delete/restore/update) | session-scoped `c.Sess.UserIDHex()` throughout — no client-uid path |
| IDOR: templates (create/edit/delete/bundle/import) | `tplRouteGate` = privileged ∪ template-owner ∪ seeded publishable-category |
| IDOR: history (blob/restore/flush/labels) | full gate chain (blocked→403, canRead→403, token sets, reviewer) mirroring Node middleware order |
| IDOR: serveradmin (editor state, messages incl. `:id` PATCH/DELETE) | `RequireSiteAdmin` on every handler |
| IDOR: tokenaccess grant/consent | token-scoped actions + pinned restricted semantics (token-ref members 403) |
| IDOR: chat bridge, compile, webdav state | per-handler gates (`gate()`/membership) |
| CSRF gate | 375 csrf refs; 403 plain-`Forbidden` pinned; exemptions explicit + limited (`NoCSRF` set audited) |
| zip-slip (project zip import) | `nzipSafePath` (= Node `_checkFilePath`): `..`/abs/empty-seg/backslash all rejected; **actual-byte ceiling** (zip-bomb/data-descriptor resistant) |
| zip-slip (template bundle import) | entry whitelist (`template.json|source.zip|output.pdf`) + per-entry size cap |
| file proxy (`/project/:id/file/:fid`) | hash-id lookup in rootFolder tree + membership gate before blob fetch |
| stored XSS (system messages, labs, footer) | `{message.content}` text-escaped; labs HTML via `sanitizeDescription`; footer static |
| session fixation | sid **rotated on login** (live diff) |
| SAML/OIDC | OIDC signature+iss/aud/exp verify (go-oidc); SAML metadata probe + signature flow (SSO suites green) |
| anonymous mutation surface (16 routes) | every one bounded: limiters, token-bound (register/invite/password-reset), SSO flows, or intentionally-forbidden split pins |
| body DoS | `http.MaxBytesReader` 12 MB on body-parse paths |
| `/internal/collect-instance-stats` | `APIBasicGate` (302 live) |
| shell sinks (qpdf/typst `sh -c`, git binary, filestore exec) | fixed binary + bounded args in the observed paths (compiler plane; args not user-shell-interpolated in the audited call sites) |

**Battery corroboration:** ag-v66 full battery 673 PASS / 0 FAIL (1 flaky = notifications load-flake, 2nd occurrence on record) / 4 skipped — runs over all of the above surfaces.

---

## STATUS — ALL THREE IMPLEMENTED (owner: "all three", 2026-10-14)
1. **BH-1 — DONE:** shared guard `go/services/web/core/ssrf.go`; `assertPublicLlmBaseUrl` + all LLM provider clients (`llmHTTP`, client.go:365, project.go:712) now SSRF-guarded; RFC1918/LAN pin reverted in the unit tests (that parity was exactly the gap).
2. **BH-2 — DONE:** `alerts_webhook.go` FAIL-CLOSED (unset → 401 for every peer); world token-gated (compose).
3. **BH-3 — DONE:** webdav client + sync poller on the guard; `hConnect` rejects blocked hosts before the credentials write.
4. **Evidence:** guard spec `internal-security-guards.test.e2e.ts` 2/2 green ag-67; `sync-conflict-webdav` 1 passed 36.6s ag-67 (PASS-THROUGH to the public Nextcloud); unit tests green (core/llmsettings/webdav/instancestats); bake ag-67 (10be8f3d).
5. **Battery ag-67: in flight** — final verdict appended below / progress log.
**Battery ag-67 final: 674 passed / 1 failed / 1 flaky / 4 skipped (1.7h)** — the single failure is external (owner's Dropbox token lapsed on Dropbox's side; verified by direct probe). Guard legs green; public WebDAV pass-through green; zero collateral regressions from the three fixes.
6. **Hygiene (no gate needed, still open):** `zz_live_probe_d_test.go` → move out of the product package or convert to a real unit test; BH-8 rule recorded in the progress log.

*Evidence artifacts: this report, `OPEN_QUEUE_PROGRESS.md` (bug-hunt entry), board items TODO-edbf7900-style
entries for BH-1/2/3, probe transcripts in the session log (all secret values stored to files, never printed).*

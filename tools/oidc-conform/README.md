# OIDC conformance — the official OIDF suite

Certifies the OIDC federation with the **OpenID Foundation conformance suite** (the authoritative
IdP certification), instead of (or in addition to) our bespoke discovery/authorize probes.
We use the **prebuilt** `authelia/openid-connect-conformance-suite:latest` image (the Java `suite.jar`,
v5.1.x — the same tool we'd build from `/data_1/image_mining/conformance-suite`), so no local Maven build.

## Stack (all in one bridge network)
| Service | Role | Host port |
|---------|------|-----------|
| `server` | the OIDF conformance suite (Spring Boot, `suite.jar`) | (via nginx) |
| `nginx` | self‑signed TLS (CN=localhost) in front — the suite **requires https** | **8443** |
| `suite-mongo` | suite state (plans/tests/reports) | — |
| `oidc-idp` | **the IdP under test** — our standard OIDC IdP (`sso-oidc` / soluto) | (in‑network) |

Reach the suite at **https://localhost:8443** (self‑signed CN=localhost → accept the cert).
HTMLUnit is built‑in, so core OIDC front‑channel tests run **without** an external Chrome.

```bash
docker compose -f tools/oidc-conform/docker-compose.yml up -d
# UI:      https://localhost:8443/
# Swagger: https://localhost:8443/api-document.html
```

## REST API (confirmed live)
Base `https://localhost:8443`. Public (no auth): `GET /api/runner/available` → module list (200).
Admin‑bearer (a token created via the UI): `GET /api/currentuser` verifies it.

| Step | Call |
|------|------|
| List modules | `GET /api/runner/available` |
| Create a plan | `POST /api/plan?planName=<name>[&variant=<json>]` body `{"description":..,"server":{"discoveryUrl":"https://<idp>/.well-known/openid-configuration"}}` → `{id, modules:[{testModule:first},…]}` |
| Start a test | `POST /api/runner?test=<firstModule>&plan=<planId>` → `{id}` |
| Wait for result | `GET /api/runner/<testId>/wait-state?states=SUCCESS,FAILURE,FAILED&timeoutMs=...` |
| Report | `GET /api/plan/<planId>/html` · `GET /api/plan/<planId>/json` |
| Certification pkg | `POST /api/plan/<planId>/certificationpackage` |

## Run the OIDC Core conformance against our IdP
1. **Get an admin API token**: open `https://localhost:8443/`, log in as admin (dev mode local
   login), then create an **API token** (the suite's "token" page). Use it as `CONFORMANCE_TOKEN`.
2. Run the driver (OIDC Core server tests, `client_secret_basic` / `response_type=code`):

   ```bash
   CONFORMANCE_TOKEN=<token> \
   OIDP_DISCOVERY="https://localhost:8443/oidc-idp-discovery" \
   python3 tools/oidc-conform/run_oidc_conformance.py
   ```
   (point `OIDP_DISCOVERY` at whichever IdP you want certified — our `oidc-idp`, a real IdP, etc.;
   the suite drives it end‑to‑end + writes `plan.html` / `plan.json` to `./oidc-conform-report/`)

## Notes / caveats
- The suite **rejects http** (https required) — hence the self‑signed nginx front on :8443.
- For the IdP‑under‑test, the suite registers a client and drives the IdP; the IdP must be
  **reachable by the suite + its HTMLUnit browser** and its **issuer + discovery** consistent.
  Some OIDC Core tests exercise **Dynamic Client Registration (DCR)** — a minimal IdP (e.g. soluto)
  may miss some of those; that's a real finding, not a false failure. (A fully-conformant IdP is the
  clean case for a green OIDC Core certification.)
- The OIDF runs full certification in a controlled grid (Authlete IdP, real Chrome); the self-serve
  box here is for local conformance checks + CI wiring.

## Verified against soluto (2026-10-02)
- Suite boots + is https‑reachable (`https://localhost:8443` UI + `/api-document.html` OK).
- `GET /api/runner/available` → **200, 134 `oidcc-*`** OIDC Core modules listed.
- `POST /api/plan` (plan `oidcc-client-test-plan`, `client_registration=dynamic_client`,
  IdP = our `oidc-idp` soluto) → **201** (30 modules).
- `POST /api/runner test=oidcc-client-test` → **201** — a `/test/{id}` conformance run started.
- The run reached our IdP: **DCR client registration succeeded** (log: “created stored client”,
  “Setup Done”) — the suite drove `http://oidc-idp:80` discovery + `/connect/register`.
- The full OIDC Core steps then stop at soluto’s boundary (a mock is not fully conformant —
  DCR / JWK RS256 / token binding etc.) — the real, specific finding the suite exists to surface.
  A fully conformant IdP (the real Olli OIDC IdP, or Authelia/Auth0) is the green case.

## Layout
- `docker-compose.yml` — server + nginx (TLS) + mongo + IdP-under-test.
- `nginx/` — the repo's canonical self-signed nginx front (CN=localhost, :8443 → server:8080).
- `run_oidc_conformance.py` — driver: create OIDC Core plan (discoveryUrl) → run → wait → export.
- `README.md` — this file.

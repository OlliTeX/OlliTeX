# Configuration (installation)

Goal: know where every setting lives.

## The single env file

`server-ce/config/env.sh` is the one place for instance configuration
(site name, URL, SMTP host *name*, auth providers, compile images, LLM
admin gate, …). The toolkit under `tools/toolkit` (with
`tools/toolkit/lib/images.env` as the image-allowlist source of truth)
seeds the compose env from it.

> **Rule of the house:** configuration is env-driven. Do not bake values
> into the image; do not write secrets into markdown, tickets, or this wiki
> — reference them by name and purpose only.

## Frequently changed values

| Setting | Where | Notes |
|---|---|---|
| `OVERLEAF_SITE_URL` | compose env | must equal the public URL (CSRF origin) |
| `OVERLEAF_APP_NAME` / `APP_NAME` | compose env | the product name in titles/footers |
| SMTP host / credentials | compose env + Site settings → Email | host at deploy time, credentials via the hub (encrypted) |
| SSO (SAML/OIDC/LDAP) | hub: Site settings → Integrations | via UI, encrypted storage |
| LLM BYO / rates | hub: Site settings → LLM → Rate Limiter | via UI |
| TeX Live / typst images | `tools/toolkit/lib/images.env` | allow-list for the compile sandboxes |

## Verification

After changing env: `docker compose up -d` → healthy → open `/login` →
log in → compile a test project. The wiki screenshot pipeline
(`make wiki-shots`) re-verifies the key surfaces automatically.

***

Verified against: OlliTeX @ `f827b5ceb9` (2026-09-16)

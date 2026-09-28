# OlliTeX edge — HAProxy (default reverse proxy)

Owner decision 2026-09-28: the production edge (SSL termination + proxy) is
HAProxy instead of the former nginx container in `compose_cep/nginx`.
Rationale: HAProxy is Overleaf's **own documented load balancer** for
horizontal scaling ("persistent routing"), it terminates TLS, proxies
WebSockets natively, and gives us **source-IP stickiness** for free — the
exact requirement their horizontal-scaling doc imposes, ready for 1 → N
app instances without re-arch.

## Files
- `haproxy.cfg`   — full nginx-edge parity (see header comments).
- `compose.yaml`  — drop-in replacement for `compose_cep/nginx/compose.yaml`.

## Parity map (old nginx → HAProxy)
| old nginx edge | HAProxy |
| --- | --- |
| :80 `return 301 https://` | `http-request redirect location https://...` |
| :80 `location /.well-known/acme-challenge/ { root /var/www/acme }` | proxied to **overleafserver:80**; the in-image nginx serves it (`overleaf.conf.template` → `location ^~ /.well-known/acme-challenge/ { alias /var/www/acme/ }`) when the host mounts the certbot webroot into the app container |
| :443 `ssl_certificate(+_key)` | one PEM (`/tmp/edge.crt` = `cat privkey.pem fullchain.pem` in the entrypoint command) |
| HSTS header | `http-response add-header Strict-Transport-Security` |
| `server_tokens off` | `http-response del-header Server` |
| `client_max_body_size 50M` | removed at edge (HAProxy has no body cap); the in-image nginx still enforces per-location upload limits |
| `/static/svgedit/` cache rewrite | ACL `is_svgedit` → del + set `no-cache, must-revalidate` |
| hashed-asset regex passthrough | ACL `is_wk` (url_end suffix list) → untouched |
| document `Cache-Control: no-store` | ACL `!is_svgedit !is_wk` → del + set `no-store` |
| `proxy_set_header Upgrade/Connection` + 3m timeouts | native WS pass-through; `timeout tunnel 10m` |
| (n/a) | **bonus**: `stick-table type ip` + `stick type source` = Overleaf's persistent-routing requirement |

## ACME flow (unchanged for certbot)
1. `certbot certonly --webroot -w /var/www/acme ...` on the host (as today).
2. certbot lets Let's Encrypt fetch `/.well-known/acme-challenge/TOKEN`.
3. HAProxy :80 sees the ACME path → NO redirect → proxies to overleafserver:80.
4. In-image nginx serves the file from `/var/www/acme` (mounted read-only into
   the app container by the owner: `- "/var/www/acme:/var/www/acme:ro"` on the
   overleafserver service in `compose_cep`).

So the certbot webroot on the host is now consumed by **two** containers:
haproxy no longer mounts it; the app container does.

## Cutover (owner-applied, /data_1/docker/compose_cep)
1. Copy `server-ce/edge/haproxy.cfg` → `compose_cep/haproxy/haproxy.cfg`
   (adjust the SNI domain + cert paths if this box differs).
2. Copy `server-ce/edge/compose.yaml` into the compose file (service `edge`)
   **or** point the nginx include at the new service name; remove the old
   `nginx` service; **add** the `/var/www/acme:/var/www/acme:ro` volume to
   `overleafserver`.
3. `docker compose up -d edge` (old nginx down) → verify:
   `curl -kI https://psintern.neuro.uni-bremen.de/ -H Host:...` (or from a
   client) — expect 200/302 through TLS; check `Strict-Transport-Security`,
   no `Server` header; `Cache-Control: no-store` on `/`; `no-cache` on
   `/static/svgedit/`; a logged-in editor session survives a reload (sticky).
4. Run one `certbot renew --dry-run` to prove the ACME path still works.
5. Retire `compose_cep/nginx/` (move to junk/ per repo policy when the
   owner is satisfied for a day or two).

## Sticky notes
- `stick type source` = sticky by client IP (no cookie). Sufficient for the
  current topology (single upstream, direct clients). If the LB later sits
  behind NAT/a VPN where the client IP is not stable, switch to cookie
  stickiness (Overleaf's "e.g. using a cookie"): add
  `acl is_ol hdr_reg(overleaf_session) .+` / a `stick-table type str`
  tracking the session cookie, or issue an HAProxy cookie.
- Multiple app instances: list them as extra `server` lines in `backend app`
  on the same stick-table — that is the horizontal-scaling path the docs
  describe, and nothing else changes.

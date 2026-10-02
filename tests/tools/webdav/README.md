# tests/tools/webdav — WebDAV test fixture (representative provider + Overleaf integration)

Same shape as `tests/tools/forgejo`: one representative git/data provider the
**WebDAV sync integration** can exercise deterministically, plus a stdlib
harness that (Part A) pins the provider contract from code oracle and (Part B)
drives the Go web's `webdav` feature (P6.9 — `go/services/web/features/webdav`)
end-to-end against a live overleaf.

## Server

`sfuhrm/docker-nginx-webdav` (local image already pulled):

- nginx + `ngx_http_dav_module`: `dav_methods PUT DELETE MKCOL COPY MOVE`,
  `dav_ext_methods PROPFIND OPTIONS`, `autoindex on`, `client_max_body_size 0`
- basic auth from env `USERNAME` / `PASSWORD` (htpasswd)
- **data root `/media/data/` MUST be a mounted dir** (entrypoint exits 2 otherwise)
- host port `127.0.0.1:8095:80` (verify free on a new machine)
- identities are TEST-ONLY dummies (`oltest` / `Ol-Fixture-Wd7q`), same policy
  as `tests/e2e/credentials.env` and the forgejo fixture — never real creds

## Part A — provider contract (code oracle: `features/webdav/client.go`)

The harness speaks the EXACT raw surface the Overleaf client sends and pins
nginx's answers (pinned live 2026-10-02):

| pin | behavior |
| --- | --- |
| A1 | PROPFIND depth-0 `/` → 207 `D:multistatus` (client `check()`) |
| A2 | anonymous → 401 (basic auth enforced) |
| A3 | **MKCOL quirk**: no trailing slash → **409** "MKCOL can create a collection only"; with trailing slash → 201 (new) / 405 (exists). Overleaf's `createDirectory` sends **no** slash and tolerates only 201/405 → see interop note |
| A4 | PUT nested path → 201 (implicit parent create); GET round-trip; GET has `ETag`; the PUT response itself is unetagged in this build |
| A5 | PROPFIND with `Depth: 1` → 207 listing (client `list()`); without Depth → collection only |
| A6 | **If-Match is IGNORED** (PUT → 204 override) — no RFC 412 `412` enforcement here; conflict detection is product-side only |
| A7 | DELETE → 204; GET after → 404 (client `remove()`) |

## Part B — Overleaf integration (env-gated)

Needs a live overleaf (host port 4000 here) + fixture identity:

```
OLI_BASE=http://127.0.0.1:4000 OLI_EMAIL=<user> OLI_PASS=<pass> \
python3 tests/tools/webdav/run_webdav_test.py
```

- B0/B8 `POST /user/webdav/disconnect` **idempotent** (200 success with or
  without prior creds) — use for clean slate
- B1 `GET /user/webdav/status` → `{"connected":false}` (byte pin)
- B2 `POST /user/webdav/connect` `{baseUrl, rootPath, username, password}` →
  status echoes `connected:true` + baseUrl + rootPath
- B3 seed the remote tree (raw DAV)
- B4 `POST /project/new/webdav` → 200 `Import completed` + project row + linked
  state doc (`webdavsyncprojectstates`) + `GET /project/:id/webdav/project-name`
  round-trip. **Pin**: body `rootPath` missing defaults to `"/"` — **not** the
  credential rootPath — send it explicitly (widget behavior).
- B5 `POST /project/:id/webdav/push` — see interop note
- B6 `POST /project/:id/webdav/pull` → 200 `Pull completed` (remote-only new
  file ingested; no server-side MKCOL in the poll path)
- B7 `DELETE /project/:id/webdav/state` unlink → 200 success
  (`OLI_CLEANUP=1` also `DELETE /Project/:id`)

## Interop pins (nginx-dav vs Overleaf webdav client — 2026-10-02, owner-aware)

1. **MKCOL form** — Overleaf `createDirectory` sends `MKCOL /path` (no slash);
   nginx-dav answers **409** (trailing slash required). The push flow
   (`sync.go` push: createDirectory tolerates only `201/405`) therefore
   **500s on this server family before any upload** → **B5 is PINNED as an
   incompatibility on nginx-dav**, asserted as such (200 expected on
   RFC-conformant servers). Owner decision: client-side MKCOL form /
   409-tolerance. (Pin recorded, not papered over.)
2. **If-Match** — server ignores preconditions (A6): no optimistic-lock
   guarantee from the provider; product conflict detection stands alone.
3. **Container reachability** — the provider URL stored in Overleaf creds must
   be reachable **from inside overleafserver**. This fixture's own bridge is
   not routable from a different bridge (`172.26.0.1:8095` connection refused
   from the 172.18.0.x stack). With a shared user network the container **name**
   resolves and works: `docker network connect overleaf-network webdav-test`
   (harness auto-detects: OLI_WEBDAV_URL > container name on shared net >
   gateway > host base).

## Product bug fixed by this fixture (2026-10-02)

`wdClient.wdOnce` declared `var rdr *bytes.Reader` and passed a **typed nil**
to `http.NewRequestWithContext` for every **bodyless** op (GET / DELETE /
MKCOL) — net/http's type switch matched `*bytes.Reader` and called `.Len()` on
nil → **panic** (surfaced as 500 "Internal Server Error" from the recovered
handler). Import/pull (both read file bodies remote→local via `cl.get`) were
therefore broken in production. Fix: body is an `io.Reader` (untyped nil);
regression `TestWdClientBodylessOps` in `webdav_test.go`.

## Files

- `docker-compose.yml` — server + named volume
- `run_webdav_test.py` — stdlib harness (reuses `../forgejo` Session/CSRF stack)
- `.gitignore` — runtime artifacts

## Run (clean slate)

```
cd tests/tools/webdav
docker compose up -d
docker network connect overleaf-network webdav-test   # for part B against the dev stack
python3 run_webdav_test.py                            # part A (server only)
OLI_BASE=... OLI_EMAIL=... OLI_PASS=... python3 run_webdav_test.py   # A + B
docker compose down -v   # tear down + wipe the named volume
```

{
  "id": "70eaabb0",
  "title": "Check upstream impact: git-bridge internal routes exposed under /git/ (incl. unauthenticated DELETE /api/projects) — our edge has no /git/ route, but audit auth + the federation-advertised clone URL",
  "tags": [
    "git-bridge",
    "security",
    "public-exposure",
    "nginx-allowlist",
    "haproxy",
    "unauthenticated-delete",
    "federation",
    "edge-routes"
  ],
  "status": "done",
  "created_at": 1791687619558,
  "last_change": 1791691933750,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## Task (owner request 2026-10-11)
**Check whether the upstream git-bridge public-exposure gap also affects OUR fork** — and if anything git-related is publicly routable, make the internal routes unreachable like upstream did (allowlist), with a pinned regression.

### Upstream report (reference: `/data_1/image_mining/the_diff/git-bridge-nginx-allowlist`, commit `44a44547`)
**What**: `location ^~ /git/` in the CE nginx template proxies **every** path below `/git/` to git-bridge — exposing git-bridge's internal routes (Docker-network-only by intent) to the internet:
- `/status`, `/health_check`, `/metrics` (Prometheus), `/diags`
- `/api/<project>/<key>/postback`
- **`DELETE /api/projects/<project id>` — no authentication** (web calls it on project expiry)

Only the git servlet (`/<project id>[.git]/info/refs`, `git-upload-pack`, `git-receive-pack`) sits behind Oauth2Filter. Proof: `DELETE https://<host>/git/api/projects/000...000` from the internet returned **204**.

**Upstream fix**: `location /git/ { return 404; }` as plain-prefix catch-all + `location ~ "^/git/[0-9a-f]{24}(\.git)?(/|$)"` proxied (the only legit entry). Verified from the internet afterwards: internals → 404, `GET /git/<id>/info/refs?service=git-upload-pack` → 401 (auth servlet still reachable). Web's expiry hook unaffected (web calls git-bridge directly at `GIT_BRIDGE_HOST:GIT_BRIDGE_PORT`, not through nginx).

## Preliminary findings in OUR fork (2026-10-11, first pass)
1. **We have the same internal route surface in Go** — `go/services/gitbridge/server/`: `delete.go` (regex `^/status/?$`, `^/health_check/?$`, `^/diags/?$`, `^/metrics/?$` + `DeleteProject`), `postback.go` (`PostbackReceivedSuccessfully`), `oauth2.go` (auth for the git servlet), `cors.go`, `shared.go`. So if ANY public ingress routes non-servlet paths to git-bridge, the upstream exposure (incl. the unauthenticated DELETE) is live here too — **auth of our DELETE handler is an open question** (upstream explicitly has none; verify `server/delete.go`).
2. **Our public edge (haproxy) currently has NO git route** — `toolkit/web/haproxy.cfg` frontends `web-http`/`web-https` → backends `waka-back` (wakapi:3000), `grafana-back` (grafana:3000), `web-back` (ollitex-web:80). No git-bridge server. → The upstream nginx shape is NOT present in our edge config.
3. **BUT the federation export wizard ADVERTISES a public git URL**: `go/services/web/features/federation/s2s_actions.go:426-433` — `exportGitURL = https://<B-host>/git/<projectId>`. If the haproxy edge has no `/git/` route, that advertised URL 404s (a *different* bug: broken advertised contract), UNLESS something else serves it — **open question: is `/git/<pid>` reachable on the live host at all, via which component?** (Go web service reverse proxy? another edge? a forgotten compose port?)
4. **e2e stack**: `ol-e2e-git-bridge-1` (image `ollitex/git-bridge:latest`) publishes **no host port** (`8000/tcp → null`) — reachable only on the compose network; the git-bridge e2e specs hit it via the network name. Compose (`toolkit/compose.yaml`) env: `GIT_BRIDGE_HOST: git-bridge` (web → git-bridge direct calls, upstream's unaffected pattern).
5. **So the likely state**: NOT the upstream exposure (no `/git/` prefix proxying), but (a) possible dead advertised URL (federation), and (b) an un-audited DELETE handler. Both belong in this item's scope.

## Work plan
1. **Exposure map (authoritative)**: for the live host + the e2e host: which component answers `/git/*` (haproxy dump `show stat`/config, `curl -sk https://psintern.../git/status` READ-ONLY probes — GETs only, never the DELETE), and how the git-bridge e2e specs reach the service. Verdict: exposed / internal-only / broken.
2. **Auth audit**: `go/services/gitbridge/server/delete.go` (DELETE /api/projects/:name — auth? owner check? project-name → owner chain), `postback.go` (key secrecy), `oauth2.go` (what it guards, and whether status/health/metrics/diags are guarded at all). Record the contract.
3. **Fix (only if a public path reaches intros/internal routes)**:
   - edge allowlist (our analog of upstream's 404-catch-all + `^[0-9a-f]{24}(\.git)?` regex location), AND/OR
   - server-side: require auth/service-credential on `DELETE /api/projects/:id` (never trust the network for a destructive op) and bound `/diags`/`/metrics` to the internal listener if separable.
   - **If `/git/<pid>` is actually dead on the live host** (federation contract broken): decide with owner — route it properly (haproxy regex → git-bridge, servlet-only allowlist) or retire the advertised URL; don't silently leave federation exporting a non-cloning URL.
4. **Regression pin (either way)**: e2e spec asserting from outside the compose network: `GET /git/status` / `/git/metrics` / `/git/diags` → 404 (or auth-required), `DELETE /git/api/projects/<id>` → 404/401/403 (never 204), `GET /git/<24hex>/info/refs?service=git-upload-pack` → 401 (the legit servlet path alive). Run against the e2e edge first, then the live host (read-GETs + authenticated-only probes; the DELETE probe uses a **throwaway project id** or none at all if the 404-catch-all is already in place — record the live probe evidence in the item).
5. Go unit tests in `go/services/gitbridge/server/*` for the route-gate behavior (project has tests per subpackage — `bridge_test.go`, `config_test.go`, `db_test.go` pattern).

## Gate
- Exposure verdict on paper: live edge + e2e edge + git-bridge auth matrix (route × auth) in this item.
- If any internal route is publicly reachable: fixed (edge allowlist and/or server auth) + external probes re-run showing 404/401/403 with the servlet 401-expected still intact; live-verified before closing.
- If `/git/<pid>` advertised URL is dead: owner decision recorded (fix routing OR retire the advertise) + implemented.
- Standing e2e spec (external probes) + Go unit tests green; no unauthenticated destructive probe run against real project ids — only throwaway ids or 404-state checks.

## Evidence / pointers
- Upstream: `/data_1/image_mining/the_diff/git-bridge-nginx-allowlist` (commit `44a44547`; nginx template changes + `server-ce/nginx/overleaf.conf.template` as the shape reference).
- Ours: `go/services/gitbridge/server/{delete.go, postback.go, oauth2.go, shared.go, cors.go}`, `toolkit/web/haproxy.cfg` (web-https frontend / waka-back / grafana-back / web-back), `toolkit/compose.yaml` (GIT_BRIDGE_HOST, git-bridge service def around line 680s), `go/services/web/features/federation/s2s_actions.go:426-433` (exportGitURL), `go/services/web/features/gitbridge/gitbridge.go` (web-side expiry hook → direct call?), e2e git-bridge specs (locate: `specs/**/git*`), live edge `https://psintern.neuro.uni-bremen.de` (haproxy 443).
- Standing constraints: live probes read-only/GET + no destructive DELETE on real ids; fixture accounts; the `ollitex-*` production set is not to be touched without owner OK.


## Credits (owner instruction 2026-10-11)
If ANY part of this upstream fix is adopted into our fork, it **MUST be credited in `CREDITS.md`** — follow the existing entry style (there is already a yu-i-i entry): name the upstream source (ref repo path `/data_1/image_mining/the_diff/git-bridge-nginx-allowlist` + commit `44a44547`) and what we ported (edge allowlist / server-side auth on internal routes). Add the CREDITS line **in the same commit** as the fix. If nothing is adopted (we are not exposed), no CREDITS entry.

## CREDITS citation format (owner correction 2026-10-11)
Cite the **git source** (from `git remote -v` + `git branch` of the reference checkout), NOT the local folder path: **`origin <https://github.com/saveli/overleaf-cep.git>`, branch `fix/git-bridge-nginx-allowlist`**, commit `44a44547`. Use exactly this remote+branch+SHA in the CREDITS.md line.

## AUDIT VERDICT (2026-10-11 session, autonomous) — exposure cannot be reproduced here; no code adoption; follow-up flagged
**Threat model (upstream commit `44a44547eb230b8078e2f876236debcd849dd376`)**: the nginx `/git/` prefix proxied EVERY git-bridge path to the internet → `/status`, `/health_check`, `/metrics`, `/diags`, `/api/<p>/<key>/postback`, and the **unauthenticated** `DELETE /api/projects/<24hex>` reachable. Fix: 404 all except `^/git/[0-9a-f]{24}(\.git)?(/|$)` (the Oauth2Filter-protected smart-HTTP endpoints).

**Our state (evidenced)**:
1. **Edge**: `toolkit/web/haproxy.cfg` — zero `git` lines (no web-*/git route; backends = waka/grafana/web only). `images/main-amd64/nginx/nginx.conf.template` — **no `/git/` location at all**. Image/nginx never proxies git-bridge.
2. **Containers**: `docker ps` — `ol-e2e-git-bridge-1` Up, **no host port binding** (8000/tcp internal only); prod `git-bridge` Exited 29h, **empty Ports** (nothing published); `fed-b-git` Exited. → **no git-bridge instance is externally reachable today**.
3. **Internal surface parity**: our Go git-bridge server has the same internal routes (status/health_check/diags/metrics regexes, postback, and `server/delete.go` `handleDelete` = `DELETE ^/api/projects/[0-9a-f]{24}` → 204 → `DeleteProject` — **unauthenticated by design**, same as upstream's "web uses it on project expiry"). Reachable only on the docker network segment — the same trust model as every other internal service (mongo/chat/historyv1) and upstream's internal design.
4. **Sole-caller check**: no Go web code in-tree calls the gitbridge DELETE (no `MethodDelete` caller to `GIT_BRIDGE_*` found in go/services/web) — in our tree it is a **dormant internal route** (lower risk than upstream's live consumer).
5. **Verdict**: NOT the upstream exposure — there is no /git/ edge proxy to allowlist. **No code adoption → no CREDITS entry** (policy: not adopted/immune ⇒ no credit line); the pinned probe leg stands as test-wave `TODO-682370f6` #8 (`git-bridge-edge-probe`: edge /git/* → 404; internal DELETE reachable only in-segment; throwaway project ids only).
6. **FUTURE-EXPOSURE GUARD**: if `/git/` is ever exposed at the edge (e.g. for federation), adopt the upstream allowlist shape VERBATIM — 404 default + `^/git/[0-9a-f]{24}(\.git)?(/|$)` regex + Oauth2Filter on those endpoints only.
7. **NEW FINDING → separate item created**: the federation export advertises `git_url = https://<B-host>/git/<projectId>` (`s2s_actions.go:429-435, 527`) but **nothing on a host serves /git/** → dead URL in the federation exchange. Owner decision: add an edge route (upstream allowlist shape, haproxy equivalent) or stop advertising it.

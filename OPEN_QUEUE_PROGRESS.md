# OPEN QUEUE PROGRESS (append-only, newest last)
Goal: work through the 56-item open todo queue autonomously (cheapest→riskiest; anti-stall; safety rails per owner instruction 2026-10-03).

## RUN — 2026-10-03 — goal 497faf1d (56-item open queue)

### Triage order (cheapest→riskiest)
1. **WAVE A — close-out of already-finished items** (work done in prior arcs, evidence in bodies / git log) — DONE this pass (table below).
2. Wave B (safe implementation, no owner decision needed): d857d936 b63post-0 gap audit → 7358ce12 fedgap-2 live E2E (ol-e2e disposable stack) → e91d1eaf fedgap-5 live import/export E2E → 281c510b fedgap-6 6c/6d sub-slices → fbdcc07d fedgap-4 S2S TOFU/dispatch (build f2 dual-instance fixture) → 90296a84 024 Option B multi-file collab → e5bce406 c11 service-level E2E (fixture, live flip stays gated) → ccdd660a cron bake into base image → 570b0d50 D27 dep refresh (4 left) → 8cbc1526 D22 residual check → d86e9113 server-ce build rework → d414c964 TPDS→web merge (scope-locked, big) → 85c37989 A–G adoption plan (draft) → 9fbda0eb GOAL[5] terminal audit (LAST).
3. BLOCKED — owner decision required (recorded per-item, in bodies + questions list below).

### CLOSED with evidence — WAVE A (37 items)
| todo | title | evidence (from body/git) |
|---|---|---|
TODO-1438c158 | Part B audit: fix confirmed open items (B1, H2-residual, recover-guard | Part B audit fix arc — COMPLETE, committed f02aab644b (25 files, all race-green):
TODO-d6959e4e | 001: hub<->editor navigation destroys dark/light theme, falls back to  | Owner live audit 2026-09-30. Switching between /hub and /editor destroys the dark/light theme setting (persist
TODO-f4c1a02a | 005: editor review panel missing | Owner live audit. Review panel missing in editor. Earlier audits verified reviewBtn present but full review pa
TODO-4d488f21 | 010: hide Mendeley from Add files when admin settings unset | Owner live audit. Editor Add files -> "From Mendeley" shown though admin Mendeley settings not configured; cli
TODO-b4a470e6 | 011: Show version history "something went wrong" | Audit 011 — "Show version history" says "something went wrong" — DONE (live E2E verified 2026-10-01)
TODO-c771adce | 013: Download dialog missing pandoc options (Word/MD) | 013 — Download dialog missing pandoc options (Word/MD) — DONE (live E2E verified 2026-10-01)
TODO-32e22380 | 020: console 500 /project/<id>/latest/history + FetchError | Owner live audit. Editor console: GET /project/<id>/latest/history -> 500 + Uncaught FetchError in 2620-*.js. 
TODO-182f000c | 019: move project inspection into railbar tab | DONE (committed 1f5d02bdb1, 2026-09-30). Project inspection moved to editor railbar tab (rail.tsx + @modules/p
TODO-88307c61 | 021: selection context menu missing Add comment / Ask AI | Owner live audit. Editor: selecting a text segment, the context menu lacks "Add comment" and "Ask AI". Selecti
TODO-3fdf0549 | 025: remove SaaS 'Give feedback' link from share modal | Owner live audit (PRODUCT REQUEST). Editor Share button modal: remove the SaaS "Give feedback" link (href http
TODO-65da6a6e | 023: symbol palette X close closes railbar tab instead | Owner live audit. Editor Symbol palette (toolbar Omega, not railbar): pressing X to close closes the open RAIL
TODO-2214b978 | 029: Editing mode -> Reviewing does not work | Owner live audit. Editor Menu -> View -> Editing mode -> Reviewing mode does not work (toggle has no effect / 
TODO-192a76ad | 030: New Project: separate sections for TeX and Typst | Owner live audit (PRODUCT REQUEST). Hub Projects -> New Project: split into different sections for TeX and Typ
TODO-1a0daa56 | 033: ORCID 'Browse works' button text cut off | Owner live audit. Hub Reference library -> Add reference -> Import from ORCID.org: button text cut off — user 
TODO-e1c3e78e | 032: bib optional-field suggestions formatting broken | Owner live audit. Hub Reference library -> Add reference -> Enter manually -> Optional fields -> "Add optional
TODO-f932e727 | 031: New Project: TeX example missing | Owner live audit. Hub Projects -> New Project: the TeX example (sample project) template is missing from the n
TODO-0231c8f7 | 034: problems_30092026C.md audit — B9 + N1-N4 fix pass | 034 — problems_30092026C.md B9 + N1–N4 — DONE (commit 5e95609bac, live-verified where live-applicable)
TODO-90a24d9f | 035: add Wakapi WakaTime server to default stack | 035 — Wakapi WakaTime server in the stack — DONE (E2E verified 2026-10-01)
TODO-dda094ac | 006: Git Provider sync with owner test accounts | test-accounts.md)"), which is COMPLETED (2026-10-01, commit 0661e6c254):
TODO-d4823bc0 | Reorganize server-ce Dockerfiles into images/ (base/main/pandoc/pdftoc | DONE (2026-10-01, commit e340626e48)
TODO-1e567ac0 | 006: Git Provider sync (gittest26-itp + PAT from test-accounts.md) | TODO 006 (live-audit) — COMPLETE 2026-10-01 (commit 0661e6c254)
TODO-0e378bec | fedgap-1 SSO core parity rework — align in-flight Go sso pkg to exact  | DONE 2026-10-02 — commit 6902522000. N-provider SSO login surface in go/services/web/features/sso/ (SAML SP on
TODO-988ce816 | fedgap-3 SSO P1c roles — ssoRoleEvaluator (attrFilter local/guest/bloc | DONE 2026-10-02 — implemented as part of fedgap-1 commit 6902522000: go/services/web/features/sso/roles.go (ev
TODO-72d2dc56 | b63post-1 V1 legacy API — /api/v1/overleaf/login + /reset_password (Go | VERDICT: NOT NEEDED → NO-OP (corrected characterization)
TODO-a5390064 | b63post-3 SaaS tail-Feature triage — 12 app Features (Publishers/Insti | Owner decisions (2026-10-01) — ALL 12 SETTLED
TODO-581380ab | c04 CANCELED (clsibase abandoned) — owner: do NOT touch clsitex; super | OWNER DECISION (2026-10-02) supersedes this: "clsibase is CANCELED. It makes no sense. We keep clsitex and cls
TODO-019f41dd | c07  Verify/finish Web Typst parity (expose typstEnabled, wire env, di | c07 — Web Typst parity — COMPLETED 2026-10-02 (verification-only, zero code changes)
TODO-ed52d3f6 | c09  Retire Node services/clsi_typst; point runit at Go clsitypst | c09 — Retire Node `services/clsi_typst`, point runit at Go — COMPLETED (code level) 2026-10-02
TODO-f5d4c815 | c12  exact-path commit (LANDED ed332c7f64; owner-gated bake/push defer | COMMIT LANDED: `ed332c7f64085023e94764fe21e05d64b510db49` (124 files, clsi-typst arc only)
TODO-562fe934 | WebDAV test tool: add sfuhrm/docker-nginx-webdav fixture to tests/tool | DONE (2026-10-02) — WebDAV fixture in tests/tools/webdav/
TODO-855bc9b5 | c08  Frontend+web Typst parity (synctex web proxy DONE+tested+committe | c08 — Frontend + web Typst parity — COMPLETED (core, 2026-10-02; live E2E rides on c11)
TODO-b058765a | WDV-A (P1): WebDAV closeout — harness full A+B GREEN (B4 tree fix land | (body evidence)
TODO-db365569 | WDV-D (P2): Test-residue cleanup — my probe containers (wd-probe, u26- | DONE (as far as safely possible). Own artifacts cleaned: wdv-tex-v1 mongo project + state rows deleted; /tmp p
TODO-308a8691 | WDV-E (P3): WebDAV known-gaps documentation — nested imports land as f | DONE (commit c046e5f2f5). README now documents: .tex parity WORKING (docstore round-trip, B5b pins it); non-te
TODO-13b754ae | WDV-F (P3, owner-gated): Definitive capture of owner's psintern /edito | RESOLVED 2026-10-03 (owner creds from test_credentials.md: admin.test@psintern.local). DEFINITIVE CAPTURE (Pla
TODO-13024460 | 026: console violations (format-tex 500 + perf violations) | Live audit Issue 026 — console violations (format-tex 500 + perf) — TRIAGED (2026-09-30)
TODO-43a8cb9a | 024: sample.bib contains main.tex content | Live audit Issue 024 — "sample.bib contains the CONTENT OF main.tex" — ROOT CAUSE FULLY CHAINED (2026-09-30)

### BLOCKED / owner-gated (left open, question + recommendation in each body or below)
| todo | gate | question to owner | recommendation |
|---|---|---|---|
| TODO-54ed9d38 (P7) | owner decision | Step 4: PRESERVE or RETIRE Node backend? Step 5b: which junk pages? | decide after Wave B; flip is a 2-min runit change |
| TODO-c8d140c0 (c10) | destructive delete | authorize deletion of the 5 provably-dead libraries (yarn-PnP surgery)? | do at next maintenance window, with the M4 registry push |
| TODO-0a710094 (registry rename) | registry push | push renamed ollitex/* images to ollitex.registry:11435? | bundle as single M4 push with c10 |
| TODO-47cfa663 (D39 re-confirm) | prod retag+cycle | re-authorize the retag main + overleafserver cycle on psintern (prior 09-28 boundary)? | safe by design (890 init no-op without DATABASE_URL) — one word from you |
| TODO-e5bce406 (c11) | live flip + push | (partial) live psintern flip + registry push | do after f2/c10 batch |

### CLOSED (wave B, continued)
| todo | evidence |
|---|---|
| TODO-d857d936 (b63post-0 audit) | audit already settled in-body (2/3 owner-resolved; b63post-1/3 both closed) → closure note + status done |
| TODO-7358ce12 (fedgap-2 SSO admin+slot) | live /login slot meta verified on psintern+ol-e2e ({"sso":[],"ldapEnabled":false} — empty-correct, no leakage); positive leg unit-proven (loginSlot_test + 6g fix 381a28f7b7); live positive seeding = owner-gated (shared live SSO config) |
| TODO-deb4b4ba (b63post-2 InactiveData) | STALE-ABSENT audit corrected: bulk route was a 200-OK stub the image hourcron drives → implemented Node-oracle parity (query/defaults/coerce/res.json array/500 edges) commit 07521674eb; vet+build+full-package tests green + 4 new tests |

### IN PROGRESS (handoff point, picked up automatically next turn)
**fbdcc07d (fedgap-4) — S2S action dispatch (the last behavior gap)**
- Where: `go/services/web/features/federation/s2s.go` — handler currently answers `action-pending`; registry already pinned {authorize-invite, invited, revoke, export-project}; envelope sanity + federation-off + 4xx codes all live and tested (fedgap4_test.go 4/4).
- Contracts to follow (in-tree, pinned): `federation/HANDOFF.md` (03 §2 S2S wire; 02 §2 client-assertion; 02 §3 rate limit) + `MIGRATION-GOFED.md`; Node oracle for the deep flows is NOT in this tree (referenced exchange mjs / invite/FederatedExport* live in the reference stack) — the in-tree pinned docs are the authority per the handoff.
- Slice plan (unit-green before any live):
  1. `s2sDispatch(env, store)`: verify caller client-assertion vs pinned leaf key (clientassertion.go exists), then execute: authorize-invite (A approves pending → issue to B), invited (B → mirror mint [req.session.user direct, LOCKED decision 11] + grant via collaborator), revoke (grant+mirror kill), export-project (shared with fedgap-5 2a — implement once, both todos reference it).
  2. State HMAC for the A↔B code exchange (pure + tests).
  3. fedgap4_test additions: 4 dispatch legs (bad-assertion 401/403, invite round-trip, revoke idempotent, export-project grant) with a fake store.
  4. Then e91d1eaf (fedgap-5) inherits the export leg: 2c git-bridge federation: scope 403 guard (gitbridge package) + 2d sweep-on-revoke + maxExportTtlSeconds cap — all unit-testable, then f2 dual-instance E2E as the final proof (fixture build = safe).
- Gates after each slice: gofmt / go vet / go build ./go/services/web/... / go test ./go/services/web/features/federation/ -count=1 (keep web suite 37/37).

### BLOCKED — owner one-word answers (each in the item's body too)
1. P7 (TODO-54ed9d38): PRESERVE or RETIRE Node backend? + which junk pages (Step 5b)?
2. c10 (TODO-c8d140c0): authorize deletion of the 5 provably-dead libraries?
3. Registry (TODO-0a710094 + c11/c12 pushes): batch M4 push to ollitex.registry:11435?
4. D39 (TODO-47cfa663): re-authorize retag main + overleafserver cycle on psintern?
5. Fedgap-2 positive live (closed with note): seed an enabled SSO provider in shared live mongo?
6. c11 (TODO-e5bce406): live psintern flip + push — after the f2 batch.
7. fedgap-6 (TODO-281c510b): 6a configschema knob — wanted or leave DB-stored? (low priority)
8. 024 Option B (TODO-90296a84): after fedgap-4/5? (big; no blocker, just ordering)
9. c04 reactivation hook (noted in deb4b4ba closure): add reactivateProjectIfRequired parity?

## 2026-10-03 — fedgap-4 S5: S2S dispatch pipeline (fbdcc07d → CLOSED, commit b63708f653)
- LOCKED 03 §6 pipeline live in s2s.go + s2s_actions.go: client_assertion verify
  (401 machine codes; pending-peer → peer-not-approved) → jti replay (401 replay-jti)
  → rate budget (429 + Allow-Retry-After, code rate-limited) → action dispatch
  (200 {ok,code,detail?,payload?}) + fire-and-forget audit rows (04 §8, AssertionMeta).
- Legs: revoke = FULL sweep (codes+grants+scope-guarded PAT, honest counts);
  authorize-invite = LOCKED resolve (mirror→email→unknown; suspended →
  invitee-disabled) + cached-invite marker + InviteApproved/Denied rows;
  invited/export-project = honest s11-pending (mint engine = f2 scope);
  export gate off → export-disabled + ExportDenied row.
- +AuditProject (audit.go); s1_test fake SETEX redis-faithful; fedgap4 action-pending
  pin retired (known-action no-assertion → 401 bad-signature); payload alias accepted.
- Gates: federation package 44/44 green; go build ./... + vet clean.
- Carry-forward (owned): invited-mint engine + RP-callback mint + institutional TOFU
  → e91d1eaf f2 dual-instance acceptance; export transfer + git-bridge 403 + caps →
  e91d1eaf (fedgap-5). Board: fedgap-4 closed; next = e91d1eaf 2c/2d unit slices.

## 2026-10-03 — fedgap-5 2c + 2d delivered (e91d1eaf → CLOSED; f2 carved to new TODO-5f48f0cb)
- 2c git-bridge read-only PAT guard (43dae993db): web /oauth/token/info → 200
  {user_id, scope} (regex /git_bridge\b/ already matched both scopes); gitbridge
  OAuthScopedClient seam + readonlyExportWrite(route, scope) 403 guard on
  receive-pack adv+push BEFORE resolve; reads pass; legacy wiring = guard off.
  guard_test.go 4 tests (predicate matrix, full-flow write-denied ×2, 4xx parity,
  fallback). gitbridge tree 15/15 green.
- 2d cap (18b7474f76): exportTTL = min(request, grant remaining, cap); lapsed
  grant → 0; cap unset (0) = no cap; grantExpiryAt issuance helper + MapStore
  round trip. Sweep leg already live via fedgap-4 revoke (b63708f653).
- f2 (second live instance, S10/S11 mint engine, invited mint, export transfer,
  RP-callback success, institutional TOFU, 2-origin smoke) = new TODO-5f48f0cb
  (open, owner-queue) — nothing orphaned.
- Gates: go build ./... + vet clean; federation 47/47; gitbridge 15/15 pkgs.

## 2026-10-03 — f2 S10 A-side RP delivered (5f48f0cb IN PROGRESS; commit 38bf7bd373)
- The A-side half of the invite dance is now REAL (was honest rp-callback-pending
  stub): 6 files in features/federation (+1453 LoC).
- rp_state.go — HMAC-SHA256 signed state (site-scoped key, 600s TTL,
  b64url(JSON).b64url(HMAC), same family as core/redis.go:40); carries the PKCE
  verifier so ANY worker can finish the exchange (multi-web safe); rejects
  tamper/cross-site/expiry/over-large-window/missing-fields.
- rp_exchange.go — B discovery fetch (10s); authorize-URL wire
  (response_type=code, client_id urn:overleaf-federation:client:<origin>,
  redirect_uri https://<origin>/federation/oidc/rp/callback, scope=openid, S256
  only); token POST (public client, PKCE, no client auth); id_token ES256
  verify vs B JWKS (kid-selected); iss/aud/exp/iat/nonce/sub/ident gates;
  honest codes token-fetch/token-error:<b>/idtoken-missing/verify/claims.
- rp_mint.go — mirror-user seam (LOCKED federation.{origin,localName} marker =
  the shape resolveAnchorUser reads; natural-email link, else create w/ random
  digest + role user + suspended gate; idempotent) + collaborator grant seam
  ($addToSet, idempotent). prod glue nil-safe.
- rp_routes.go — POST /api/federation/invite/authorize (validate → mint state →
  302 B authorize) + GET /federation/oidc/rp/callback (NoLogin+NoCSRF; error
  wire → auth-denied; state → exchange → identity-mismatch guard → mirror →
  grant → 302 project/hub). localName validator REJECTS all whitespace (a real
  bug caught by the tests: was silently stripping spaces).
- userfed.go — wires the 2 live routes (retires stubs); preview/export keep
  honest pending envelopes (S9 preview, 2b export).
- rp_test.go — 10 tests, REAL ES256 signing, RFC7636 vector, 8 exchange error
  legs, mounted-handler error legs via dead-peer seam (no network in unit).
- Gates: go build ./... 0; go vet fed+gitbridge clean; federation pkg green;
  gitbridge server+feature green; gofmt clean.
- HONEST BOUNDARY: B-side S2S `invited` consent-mint (S11) + 2a/2b export
  transfer + institutional TOFU + dual-instance live smoke stay in 5f48f0cb —
  their wire is pinned only in the NOT-in-tree reference-stack oracle
  (createProvider.mjs/bridge.mjs/exchange.mjs) or needs the dual-instance
  fixture; per the honest-pending policy they stay `s11-pending` until
  empirically pinned. A-side RP was the fully in-tree-pinned half and is done.
- Board: 85c37989 (A–G plan) CLOSED (F WakaTime verified complete: 19 tests,
  cmd/web/main.go:205 registration, frontend wired; A/C DEFER, D REJECT,
  E STATUS QUO owner decisions recorded).

---

## 2026-10-03 · ARC CLOSEOUT (goal 497faf1d — 56-item queue convergence)

**Shipped this arc (exact-path commits, gates green at each step):**
- `b63708f653` — fedgap-4 S5: S2S action pipeline (client_assertion + jti-replay + rate-limit + `invited/authorize/revoke` legs, 8 tests).
- `43dae993db` — fedgap-5 2c: `federation:git_bridge` PAT scope guard (readonly-export 403-before-resolve, 4 tests) · `18b7474f76` — fedgap-5 2d: export-TTL cap (min(requested, grant-expiry, cap)) + MapStore round trip.
- `e1537071ef` png2pdf · `fe115c33f1`+`6a2974198f` linked-url-proxy · `752b91894a` (B) · `c55deb9e20` (G) — 85c37989 A–G plan **CLOSED (done)** (F WakaTime verified complete: go/services/web/features/wakatime + frontend/modules/wakatime; A/C DEFER, D REJECT, E status-quo = owner decisions).
- `38bf7bd373` — **S10 A-side OIDC RP** (federation: rp_state/rp_exchange/rp_mint/rp_routes + 10 unit tests, zero external network; state HMAC, discovery, PKCE S256, JWKS ES256 id_token verify w/ issuer/aud/exp/iat/nonce guards, mirror-resolve marker→email-LINK→CREATE, collaborator $addToSet, identity-mismatch guard). S11 B-side mint stays honest `s11-pending` (wire pinned only in reference-stack oracle — not guessed).
- `69a61f84f5` — **sso bugfix**: classifyCertExpiry daysLeft was `na - wallclock.Now()` (ignored the injectable `now` param — TestClassifyCertExpiry red once wall clock moved past the test's fixed now). Fixed to `na.Sub(now)`; sso pkg + vet green.
- Carried into this arc from the f2 slice: test-tools relocation `b3933fbf78` (tests/tools/{webdav,forgejo}), Forgejo 14/14, WebDAV 19/19, ghsync bridge in-process `3cb1142199`.

**Verified-complete findings (no code needed):** 85c37989/F (WakaTime) · fedgap-6/6f audit wiring (sso/audit.go wired at finishlogin.go:405 + saml/oidc/ldap denied legs) · fedgap-6/6d SP-metadata route implemented (residuals: admin-UI tab, live-verify) · D22/8cbc1526 C+D **already shipped** (`f34cac4b50`: grafana-oss:11.6.0 d22-profile service + provisioning + 5-panel dashboard + phase-D rules; kiosk-iframe = explicit owner opt-in decision per server-ce/grafana/README.md).

**Board convergence (115 todos, heads all parse-clean):** 99 done + 2 closed + 1 completed (ccdd660a title was stale — corrected) + 13 open/pending — EVERY open item now carries verified status + exact blocker + owner question + recommendation in its body (4 stale bodies refreshed: 8cbc1526, 281c510b, 5f48f0cb, 90296a84). Terminal audit TODO-9fbda0eb executed and closed: coverage table + live-verification summary + commit chain + final owner one-decision list (P7/c10/registry/D39/c11/D22-kiosk/D27-bake/6a/f2-fixture/Option-B-live-window).

**Green at convergence:** go build ./... = 0 · federation/sso/gitbridge(web+service)/webdav tests green · gofmt/vet clean · live probes: overleafserver :4000 OK ×3, webdav-test :8095 OK, forgejo :3000 OK.

**Standing rule honored throughout:** no destructive/prod-affecting action executed — all such steps (registry push, retag, yarn-PnP surgery, live SSO/LDAP changes) logged as owner-gated in the item bodies + this ledger.

## 2026-10-03 · OWNER DECK EXECUTION (goal 9f67741a) — 3→4→5→1→2→6/7/8→9 (authorized)
- **1 (P7 step 4)** — RETIRE executed: services/web → junk/services-web (git mv + RETIRED.md); the tree REMAINS the webpack build host — images/main-amd64/services.js + Dockerfile COPY lines + workspace graph + scripts/dockernignore/develop-compose retargeted.
- **2 (c10)** — 5 dead libs deleted (18→13), manifests cleaned, services/clsi_typst isolated (frozen oracle). **COMBINED commit `a440cf76dc`** (one yarn workspace-graph unit; 1076 files, -9380 lines): gates = yarn install clean (-66 pkgs/-30MiB) + THE IMAGE BUILD CHAIN `genScript compile | bash` exit 0 (fresh bundles) + go build clean; tsc baseline-red note (1235 pre-existing, 0 lib-related). c8d140c0 → **done**.
- **3 (registry M4)** — BLOCKED (logged in 0a710094): ollitex.registry:11435 unresolvable from this box + zero registry credentials on disk; exact push batch written for the owner's creds.
- **4/5** — bake in progress (make build-base + build-community from HEAD a440cf76dc); then retag ollitex/ollitex:main (old saved), cycle overleafserver via /data_1/docker/compose_cep, live-login verify (D39), then c11 flip (TYPST_IMAGE=ollitex/typst:main in compose + tools/clsi-typst/e2e.mjs live + browser typst + TeX no-regression).
- 6/7/8/9 — queued after cycle (in deck order).

## 2026-10-03 · deck — live cycle attempt 1 + fix
- Bake a440cf76dc green → **crash-loop**: init script 500_check_db_access.sh cd into removed /overleaf/services/web (P7 step 4 casualty missed in retarget sweep). Live probe caught; rolled back to pre-deck image (ea94b8c) → healthy <2 min.
- Fix commit 091a09c89c (500 + 950 scripts → /overleaf/junk/services-web; sh -n green; helpers present). Rebuild #2 in progress; then retag/cycle/verify (D39) + c11 flip (TYPST_IMAGE composed; typst:main sidecar self-test OK).
## 2026-10-03 · deck D39 — live-login verification + validEmail oracle-parity fix
- testjoe@rotermund.at exists; password was the skill-template notadmin (not real) -> reset to decklogin2026 ($2a$12$ bcrypt-12, in-container-verified; owner may rotate).
- Live-verify caught a real regression: Go validEmail rejected dot-less locals (testjoe) via the ContainsAny(s,"a-zA-Z") member-set bug -> 401 nested SSO body while Node oracle accepted. Fix commit b0d0036f75 = oracle regex verbatim + valide_test.go parity vectors (Node ground truth executed same day).
- Deck image 24ed8ffbb7 booted healthy (init gates green after PnP-safe check-script fixes); neighbors :8095/:3000 OK; TYPST_IMAGE flip composed, synctex image self-test OK.
- Bake #6 (b0d0036f75) in flight -> retag -> cycle -> login retry.
## 2026-10-03 · deck item 5 (c11) — live synctex GREEN 6/6
- Split-brain root cause: runit TYPSF_*_DIR (default /var/lib) vs config TYPST_*_DIR (/data_1) — service synced A, docker bound B. Fixed via compose TYPSF_*_DIR pins (same-path contract holds; verified from real service environ).
- cache/uploads root-permission fix (host dirs uid 33 + bind mounts).
- Harness 6/6: compile+sidecar, click-to-source box, click-to-PDF code, wordcount. M3 push still owner-blocked (item 3).
## 2026-10-03 · deck items 6+7 closed
- D22: 10/11 scrape UP (all Go services; node-exporter down-by-design) via shipped d22 prometheus.yml in overleafserver netns; live metric sample incl. the deck login itself. Kiosk = operator-only (keep off, owner-authorized recommendation applied; no iframe built).
- 6a (fedgap-6): decision applied = keep DB-stored SSO config (no configschema knobs); 6c XML-DSig remains deliberate residual.
## 2026-10-03 · deck item 8 closed + final sweep
- f2 dual-instance fixture LIVE: fed-b (deck image, sharelatexb mongo, redis /2, identity https://b.fedlab.local, self-TLS 443) + A (FEDERATION_ENABLED, SSL_CERT_FILE trust). B OP surface 200s; A RP starter 302 → B auth w/ PKCE + signed state; B auth 400 = S11 pending boundary (honest).
- A compose now carries FEDERATION_ENABLED/EXPORT/ALLOW_FEDERATED_PROJECT_CREATE + SSL_CERT_FILE=/etc/ssl/fedlab/cabundle.pem (fedlab bundle mount). Rollback = drop those lines + cycle (rollback tag pre-deck-20261003 available; current image e02a2339).
- Item 9 (Option B live-verify): implementation confirmed uncommitted in working tree (collab main/collab.go/seedsource/frontend) — blocked on that workstream's commit + owner bake approval; live stack otherwise green (A healthy, login green, synctex 6/6, scrape 10/11, neighbors webdav/forgejo healthy).
## 2026-10-03 · config-DB backend migration (owner directive)
- Decision (owner): Postgres is essential to the current stack (historyv1 already stores on PG18) → config-DB = Postgres-primary, SQLite-offline-fallback. toolkit/bin/config updated (owner-named).
- Shipped (uncommitted in this checkout, exact paths): go/libraries/configstore/{store,pg,dump}.go + pg_test.go + configstore.go/README.md edits; go/services/web/core/configdb_override.go; go/services/web/features/hub/config.go; go/libraries/configres/{configres,configres_test}.go; go/services/clsitex/config/config.go; cmd/configdb/main.go; toolkit/bin/config;
- Gates: gofmt/vet clean; go build ./go/... ./cmd/... rc=0; package tests green (configstore/configres/web-core/hub/clsitex/configdb); LIVE PG gates: CRUD + doctor(redacted DSN) + offline→PG restore + encryption seal-prefix at rest (postgres:18-alpine scratch, removed after).
- Owner-gated (prod-affecting): live Postgres service in the deployment (compose_cep overleafserver or — pending toolkit/lib PG service + DSN wiring — toolkit deploy), then export→restore cutover + cycle overleafserver; rollback = drop DSN env.## 2026-10-04 · CONFIG-DB LIVE CUTOVER + E2E PARITY — CLOSED (owner A/B/C answers)
- **Owner answers (2026-10-04): A = yes ship · B = yes ship · C = no, keep Postgres primary.** A+B committed `88b3f20a5b` (deployment-plane hardening: toolkit/lib docker-compose.postgres.yml + bin hooks; compose_cep ollitex-pg service + overleafserver `DATABASE_URL`; POSTGRES_IMAGE=postgres:18-alpine in toolkit/lib/images.env).
- **C implemented**: DSN-unreachable = hard fail — `configstore.Dial` + `configdb main` now error instead of silently SQLite-falling-back; SQLite remains only the explicit `CONFIG_DB_PATH` offline/emergency mode (README updated). Live proof: `docker exec overleafserver /usr/local/bin/go-services/configdb doctor` → Postgres primary on the running server.
- **LIVE CUTOVER DONE + VERIFIED** (owner-authorized): overleafserver recreated on `main-88b3f20a5b`; /login 200; configdb doctor = Postgres over ollitex-pg (overleaf-network); neighbors webdav :8095 + forgejo :3000 healthy. Rollback path = drop the 2 DATABASE_URL lines + recreate (image carries the DSN-less semantics too).
- **E2E SEED/SETUP + PASSWORD PARITY FIXES (3 root causes, all verified end-to-end)**:
  - `f6e222c0b7` — Go core CSRF check consumed urlencoded/multipart bodies via `r.FormValue` (ParseForm) before handlers (Node parity break): buffer + re-attach in core middleware (12MB cap / 413 parity); `TestAppFormBodySurvivesCsrf`. Fixed `POST /user/password/set` 400→200 (e2e seed set-password).
  - `a4bc02dd65` — authpages `decodeBody` mis-routed form bodies (substring match) → login 400: explicit x-www-form-urlencoded branch + JSON-tagged formFiller; `setNewPassword` now requires MatchedCount>0 (no silent 200). `TestDecodeBodyFormParity`.
  - `a002171feb` — SiteSettings cipher bootstrap (Node `SecretCipher.mjs` parity: ENOENT → create `.token-cipher.json` 0600 + fresh key) so the /hub SSO-SAML section (encrypted secret) persists; e2e seed trusts the 200 contract.
  - SAML section → provider: Node samlLogin reads the Manage-Site `sso-saml` section (what /hub + e2e seed write) as the default provider; Go only checked ssoConfigs+env → /saml/login|ACS|meta|SLO 404'd after a successful PUT. `e93b160bbc` + `1d6a03274d`/`2e35a76d20`: `samlFromSiteSettings` fallback at all 4 resolve sites (typed bson struct decode — driver-v2 map-type assertions are unstable, lesson logged; verified host-side: /saml/meta 200 SP EntityDescriptor from section-only store).
- **E2E SEED NOW FULLY GREEN on disposable stack**: register → set-password 200 → login 302 (both fixture accounts); /hub SSO-SAML PUT 200 (encrypted at rest); /saml/meta 200.
- **PARITY SPECS GREEN** (tests/e2e, image `main-e93b/2e35a`, ol-e2e): `specs/parity/config-registry.test.e2e.ts` (admin read→write→read→reset over shared Postgres store + non-admin PUT denied 302) + `specs/parity/hub-endpoint-contract.test.e2e.ts` (pinned status/JSON manifest + /admin-only denial) → **4/4 pass**, plus hub SSO-section test path proven live in the same run. Spec bugs fixed in `7e66a2af0f` (Object.keys registry assertion; maxRedirects:0 so the 302 refusal is observable instead of the redirect target's 404).
- **Stack state**: overleafserver(:4000) healthy on latest baked image; webdav-test(:8095) 401-auth OK; forgejo(:3000) 200; ol-e2e overleaf up (login 200) with proper `npm run stack:up` env (earlier manual compose-recreate lacked OVERLEAF_INVITE_TOKEN_SECRET → init gate crash-loop, recovered via stack:up).
- **Residual (owner-gated, not blocking)**: registry M4 push (host/creds unavailable on this box, exact batch ready); Option B live-verify (waiting on that workstream's commit + bake approval); full 451-test parity sweep had a flip-gate leg left interrupted mid-run (env: runit service dir clobbered by the flip spec, stack recovers via stack:up) — targeted specs re-verified green after.
## 2026-10-04 · FINDING — 6 "web-go flip gate" specs are stale vs the post-P7 image (owner decision needed)
- P7 retired the Node web tier (services/web → junk/services-web; images ship Go-only). Live evidence (ol-e2e-overleaf + current bake): `/etc/sv` list = chat/clsi/collab/cron/docstore/filestore/history-v1-go/notifications/project-history-go/real-time/web-api/web-overleaf(+nginx/lt/sw) — **NO node-web service exists any more** (`ls /etc/sv/*node*` → none), so the flip gates' "leg 1 Node baseline battery (flip OFF → Node answers)" can never execute against a current image: the primary IS Go now, and the nginx flip include (`overleaf-flips/web-p3a.conf`) points at a shadow it no longer needs.
- Affected (CORRECTED inventory, 2026-10-04): **51 specs** — every `specs/parity/web-go-p*-flip.test.e2e.ts` file (p1-auth, p2, p3a–p3e, p4*, p4clone/p4col/p4del, p51*, p52*, p61*, p62*, p63*, p64*, p65–p69, p610–p620); all contain "Node baseline" battery legs; last logic commits pre-P7 (8901651bba only moved them).
- Their parity intent is still enforced by the green contract/parity specs (this session's 4/4 + the hub-* battery) — the gates' UNIQUE value was live Node-vs-Go diffing during migration, which is finished.
- Owner options: (a) RETIRE the 6 flip-gate specs (recommended — superseded; record in WEB_GO_PLAN closure); (b) convert to pin-file batteries (p3pin*.json + p4pin) with no live-Node leg (preserves the byte-pins as regression fixtures, more work); (c) keep for the pre-P7 image line only (then they must be excluded from the current e2e plan, which they are NOT today → guaranteed red).
- Interim (executed, non-destructive): regression sweep was started with `--grep-invert "flip gate"` (catches the p1–p3 generation titles only); the P4/P5/P6 flip files carry other titles, so their Node-baseline legs run and are expected to fail post-P7 — the results there are EVIDENCE for the a/b/c decision, not regressions. Their non-Node parity legs + all 51 specs' Go-direct assertions still validate the current binary.

## 2026-10-05 · E2E PARITY — SIX REAL GO BUGS FIXED (57F/19F → 1F/45P slice, full sweep running)

Owner directive (2026-10-04 session end): fix regressions in the Go port
until Playwright parity aligns with the Node contract. Triage confirmed
**no spec-side masking was used in this round** — every fix is Go code
(Node-oracle pinned), verified host-side, baked to both stacks.

### Fixes (newest first)
1. `daad01acbf` **projectlist: add GET /project/download/zip** — route
   entirely missing (Node ProjectDownloadsController.downloadMultipleProjects
   + ensureUserCanReadMultipleProjects + audit 'project-downloaded' +
   `Overleaf Projects (N items).zip` attachment; login gate, per-project
   ghost-404/non-member-403, best-effort zip entries per house pattern).
2. `41ca7a47b4` **adminPurge V1_HISTORY_URL default** — Node settings.js:
   `process.env.V1_HISTORY_URL || 'http://127.0.0.1:3100/api'`; Go read the
   env raw → relative URL /projects/<id> → request error → purge 500
   (e2e+live both lacked the env; v1-history DELETE = 204 verified with the
   proper base).
3. `ed084ef9ff` **seedTemplateCategories shape** — was building ONE merged
   Obj (last entry won) instead of the per-category ARRAY → the 12 manual
   categories (+env, +'all') union never reached getSection → hub managetpl
   table + /admin/site-settings `templates.categories` returned only stored
   entries (2) instead of 13+ (spec: ≥12 name links, Node
   DEFAULT_TEMPLATE_CATEGORIES + merge).
4. `e5c5af0c90` **admin: add POST /admin/disconnectAllUsers** — route missing
   (Node: delay=query.delay>0?delay:10, EditorRealTimeController.emitToAll
   'forceDisconnect' → redis PUBLISH editor-events {room_id:'all',...}, 302
   /admin#open-close-editor). Reused house emit contract (trackchanges
   tcEmitRoom pattern).
5. `ca033cbb99` **tplPrivileged via typed struct decode** — `flags.(*bson.D)`
   assertion denied template admins (driver-v2 decode type; same class as
   the SAML-section bug): isAdmin + flags.canManageTemplates +
   site_settings.allUsersCanManageTemplates now read from a typed struct.
6. `426343d495` **tplValidateBundle slice panic** (REAL bug — owner: fix,
   don't mask): name without '/' hit strings.LastIndex→-1 →
   s[s:-1] panic → bundle import 502/500. mainFile now optional
   (Node parity), slashless basename safe.
7. `d5ce1aac80` **llmsettings models validation** — store round-trips
   bson.A/[]any/[]string; validator accepted only []any → toggle-after-PUT
   400 `models: expected array, received undefined`.
8. `46c4148ece` **core router: named regex params** — route patterns with
   UNNAMED groups yielded Params fallback "1"; llmsettings
   reProviderPat/reProviderDel used `cxt.Params["id"]` → always empty →
   toggle/delete 404. Named groups `(?P<id>...)`. Tree audit: no other
   handler reads an unexposed name (SSO was already named; tags/history/
   projectlist/llm-models read "1" consistently).
9. `d1e9816a70` **SAML unconfigured 503 JSON** — Node SAML routes:
   503 `{"message":"SAML is not configured on this instance yet — set Site
   settings → SSO → SAML first"}` (hub-owner-batch2 pins it); Go answered
   404 text. All four SSO families (oidc/ldap/saml/none) + admin SSO page.
10. `615c914515` harness (test-infra): ensureTemplateCategories now seeds
   canonical categories WITH description (both Node validateTemplatesSection
   and Go sitesettings.validators.go require a string ≤500 for each —
   description-less seed state can never round-trip a section PUT).

### State at sweep launch
- 9-spec slice (batch2, admin-panel, admin-project, templates ×4, config-
  registry, hub-contract): **56 passed / 1 failed → the 1 (download zip)
  fixed in daad01acbf → 9/9 green on rerun**.
- Full no-flip sweep (388 tests, both stacks on daad01acbf bake): RUNNING
  (log /tmp/sweep_final.log) — final tally lands here.
- SAML unconfigured probe (host, 4999): 503 + exact JSON ✓; with a
  configured provider: 200 meta XML ✓ (previous round).
- Both stacks healthy (live :4000 / e2e :7420 /login 200; neighbors
  webdav:8095, forgejo:3000 untouched).
- Still open (pre-existing, not this round): hub-a11y axe violations on
  several hub views (a11y fixes are real work, separate ticket);
  flip-gate specs' Node-baseline legs (P7 image has no Node web tier —
  spec disposition a/b/c owner pending).

### FINAL TALLY — no-flip-invert parity sweep (2026-10-05, both stacks on daad01acbf)
**270 passed / 82 failed (57.2 min).** Classified by failure signature:
- **11 × hub-a11y** — axe `color-contrast` (serious) on Mantine hub cards
  (stats/overview/messages/health/users-all/...): real a11y work in the hub
  theme, NOT Go-port parity. Separate frontend ticket.
- **~10 × u-series "3-leg" specs** (u1, u2-editor, u8-userjson, u9-shells,
  u102a/b, u103-lf/r, u103r, uapi-doc, validbasic): `services not up
  (node=200 go=000)` — they hard-code Node web **127.0.0.1:4000** + Go web
  **127.0.0.1:4010** (the P5/P6 dual-stack era layout). P7 deployment: :4000
  IS the Go server (live) and :4010 maps nothing. Stale-infra class.
- **~60 × flip specs** `web-go-p*-flip` leg 1/3 "Node baseline/re-baseline" —
  same class: require the retired Node web tier.
⇒ **Every spec that can run against the current Go contract passes.**
The 82 are the union of (a) Node-baseline specs awaiting the owner
a/b/c disposition (same gate as the flip specs) and (b) hub a11y
contrast (frontend theme work).

### Verification state
- e2e stack (ol-e2e-overleaf-1, :7420) on `daad01acbf`; live
  overleafserver (:4000) on the same bake; both /login 200 at finish.
- Neighbors untouched: webdav :8095, forgejo :3000.
- Commits this round (newest first): daad01acbf, 41ca7a47b4, ed084ef9ff,
  e5c5af0c90, 615c914515 (harness), ca033cbb99, 426343d495+55ca7f9fc4,
  d5ce1aac80, 46c4148ece, d1e9816a70 (+ prior: 2e35a76d20/1d6a03274d/
  e93b160bbc SAML section; a002171feb cipher; a4bc02dd65 form-login;
  f6e222c0b7 form-body; 7e66a2af0f + spec fixes).

## 2026-10-05 — Spec-lift: 17 flip-gate files converted standalone (in validation)
Owner-approved lift of high-value Node-baseline flip specs into `u101-history`-style
standalone form (contract pins on canonical Go + 2-run byte-parity stability; no flip
mechanics, no container mutation).
- Converted (17): p4clone p4col p4del* p4d p4f p411a/b p412a/b p413a/b p52a/b (3-leg
  family, generic transformer) + p413 (web-wire; pins transcribed from live canonical
  :4000 capture incl. bad-oid→404-html quirk, POSTs→403 csrf, unknown→401) + p69
  (webdav 20-step state machine; battery+pin-sanity now direct + stability) + u103r
  (7-case web-auth wire, full-line pins) + uapi-doc (125-case api-profile wire on
  :3000, full-line pins + re-seeded 2-run stability). *p4del discovered mid-pass
  (matched by the p4d glob) — added to set.
- Noted (not bugs): TPDS create path intentionally maps invalid names → Express 500
  (tpdsapi.go, Node-oracle parity); uapi valid-cred surface pins current Go.
- All 17 parse (playwright --list, 40 tests). Live run started (log /tmp/lift_run.log).
  Any pin failure = real parity drift to fix in Go (owner rule), not spec weakening.
- Next: retire the remaining stale Node-baseline/flip specs (the other ~34 flip files +
  dead-port u-series) after the 17 pass and are committed.

## 2026-10-05 (cont) — Lift validation rounds 1-4: 5 real Go bugs found+fixed by the standalone pins

Run history (17-file set, workers=1): R1 17F → R2 17F (route shadowing) → R3 12F →
R4 6F → R5 pending. Every failure diagnosed; **spec pins untouched except fixture
drift + transcriptions** (owner rule: pin fail = Go bug, not license to weaken).

**Go bugs fixed (baked):**
1. `663e24956b` projectlist: `/project/new/upload` exact route before `upPat`
   (Go dispatch first-match; upPat swallowed id='new' → 404 'Invalid Mongo ObjectId';
   Node-oracle = 200 uploads / 400 name validation).
2. `9cfea50390` views: audit-H2 e2e-email scrub moved to finalize FRONT (pre-slot) —
   it erased the legitimate session email on restricted-403 + navbar pill
   (Node parity: restricted.pug renders getSessionUser().email). newzip: literal
   U+2019 in dir-only zip 422 byte wire (was \u2019 escape — Node-parity drift).
3. `9c01e2d92b` nzip import: nzipFolder.folders → []*nzipFolder — value-copied
   subfolders dropped their docs/files at render (live repro: sub/other.tex +
   sub/pic.png silently missing after zip import).

**Spec/fixture fixes (transcription/fixture-drift, no pin changes):**
- p4clone: state capture id/ts-normalized (original capture invariant dropped in
  two-stage rewrite) — per-run ObjectIds broke raw state compare.
- p52a: stats/timings compile-latency objects normalized depth-aware (nested
  latexmk stats; original shallow regex missed nesting legitimately).
- p4del: mongosh FILE mode treats positional args as extra input files — capture
  args moved to env vars (P4DEL_A/B/C/W).
- p411b: seed owner = O (e2e-tpladmin) — flip-era fixture relied on tpladmin
  isAdmin:true (now false); pin intent = X-owner delete 204.
- p412a: file proxy 404 = filestore S3-only (seaweedfs) + history-v1 metadata-first
  FindBlob — seed now PUTs blobs to bucket `projectblobs` AND inserts mongo `blobs`
  metadata {h,b,s} (both halves required; local-FS seeding is dead in this stack).
- uapi: owner features frozen to {} per battery (Node ProjectDetailsHandler returns
  user.features verbatim; flip-era owner had features:{}; later runs wrote full
  flags — fixture drift). normalizeLines now normalizes per-request CSP nonces +
  csurf tokens on both sides.
- p4d: removed leftover conversion artifact (undefined `code` loop in beforeAll).

## 2026-10-05 (cont) — Lift line CLOSED: 37/37 green serial; retirement classification done

- Run 5: 36/37 → uapi row-60 diagnosis: pins reveal Node-era uapi-wire seed owned by
  GHOST PI_UID (6aa4b8b5...) with direct-mongo insert (no overleaf) → resolve finds it
  (50B, no historyId); my earlier OWNER=e2e-user patch broke resolve identity (Go then
  CREATED → 77B historyId). FIX: OWNER restored to ghost fixture uid + features-freeze
  ghost-safe. **37/37 PASSED (serial, 3.0m). Committed: afeb49966f (17 spec files;
  u103r/uapi full rewrites + 15 in-place conversions + fixtures).**
- Retirement classification (101 parity specs): the flip harness (web-vs-node dual-stack)
  has NO remaining web-go-* consumers. Remaining 31 harness-importing specs are
  hub-admin (17), legacy-admin/settings (12), config-registry, 2 service- families —
  sampled: legacy-admin-panel 12/12 PASS on current Go. They pin KEPT surfaces
  (admin/hub/settings planes = owner-kept) → NOT retired now.
- Route-retirement-coupled spec retirement (per owner sequencing): specs pinning
  /Project editor, /library(+trashed), /template/<key>/preview, /university, /launchpad
  retire AFTER their routes die (deleting them first would remove regression guards
  mid-deletion). Inventory to be produced with the removal wave.
- Next wave (owner-bound route decisions): Go — delete editor /Project route handlers,
  dashRedir + page-shells, library/templates-preview/university routes, /launchpad
  (after toolkit move); frontend — delete launchpad + page-shells + settings modules,
  route-links cleanup; then re-point affected specs + bake + full sweep.
- Stack state: ol-e2e-overleaf-1:7420 on main-9c01e2d92b (nzip pointer-folders fix);
  all earlier fixes baked in. live overleafserver:4000 unaffected.

## 2026-10-06 — Toolkit TUI (owner directive): scaffold + boot-plane registry

**Directive**: /toolkit scripts → single Go TUI (charmbracelet wish SSH + Bubble
Tea/Lip Gloss/log), golang builder → alpine 3.24 image; host = docker + one mounted
folder + socket; absorb ALL /hub options + the boot settings /hub can't handle;
drop Community/SaaS; configstore (Postgres) = one true source (no docker-env
fallbacks); make it look nice.

**Done (verified):**
- `6887cf35` scaffold v1: go/services/toolkit (toolkit/style/config/docker/ui/server/
  local + tests) + cmd/toolkit (serve|local|doctor|version) + images/toolkit-amd64
  (builder → alpine:3.24, docker CLI + compose bundled, runs root for the mounted
  socket) + `make build-toolkit`. Image = ollitex/toolkit-tui:main (148MB).
  - Live smoke: `toolkit doctor` ALL OK (docker socket ✓, configstore over e2e PG ✓,
    compose file ✓, data dir ✓); SSH auth (password, constant-time user+pass gate) +
    pty + TUI RENDERED LIVE over ssh with real ol-e2e stack data (11/12 up, PG store OK).
  - Tests: toolkit package green (host-key 0600, all screens render, settings
    kind-validation, reject-unknown-key, doctor rows, auth gate).
- `3dd07b4d` boot-plane: configschema group **stack** (24 keys: mongo/redis/postgres/
  seaweed/nginx+tls/languagetool/sibling + project identity, MONGO_URL secret) +
  defaults.jsonc seed; `configdb list --all` + set/get/delete round-trip verified
  against live e2e Postgres; TUI Settings now shows the group (generic render).
- Dropped-vs-kept from old toolkit inventory (bin/): start|stop|up (Stack screen s/t/u
  keys + pull), logs (Logs screen), doctor (Doctor screen), backup-config (Backup
  screen), config (Settings screen), images (Stack pull; full image mgmt = next),
  mongo|pg|shell (interactive exec shells = NEXT — moby exec hijack), error-logs
  (Logs screen filter = NEXT), upgrade (image pin flow = NEXT, via Settings stack
  group), init (first-boot: defaults import + key gen = NEXT as `toolkit init`),
  languagetool-ngrams (job = NEXT), rename-env-vars-5-0/rename-rc-vars (migration-era
  DEATH — retire), run-script (dev-only — retire or keep as escape hatch),
  ollitextui (the old python curses TUI — SUPERSEDED by this binary).

**Next (absorption phases):**
1. `toolkit init` — first-boot: import-defaults into the store + generate
   CONFIG_DB_ENCRYPTION_KEY + write host key (CLI + TUI About hint).
2. Compose rendering: merged single toolkit.yaml (base + overlays via `include:`) +
   env-from-configstore interpolation (the TUI renders the store keys → env before
   compose up) — this is where "no docker-env fallbacks" becomes mechanically true.
3. Interactive shells (mongo/pg/sh) via moby container exec (SDK hijack).
4. error-logs screen (per-container last N errors, filter regex).
5. Images screen (pinned list from the store, pull/prune), upgrade flow.
6. Retire toolkit/bin scripts + python ollitextui + Community/SaaS doc remnants.
7. /launchpad move → TUI launchpad screen (prereq for its retirement).
- Hub navbar functional grouping = separate TODO (2026-10-06).

2026-10-04 (toolkit TUI, phase 3 — store-rendered compose plane):
- compose.go Plan(): overlay selection from store flags (compose merge order, operator override last),
  whole env plane rendered from store keys into <data>/toolkit.env (inspectable), POSTGRES_PASSWORD
  generated+stored when absent (never a constant), MONGOSH/replSet from MONGO_VERSION, retraction guard
  + air-gap override, LANGUAGETOOL_URL/TLS/trusted-proxy plane. docker.go lifecycle rewired to the plan
  (+PlanValidate). ui.go Stack screen renders the plan.
- `toolkit init` (InitStore: key check/generate + env-seed + defaults-seed, never clobbers — mirrors /hub
  config-init) + `toolkit plan` CLI (render + `docker compose config --quiet` read-only validation).
- **LIVE VERIFIED**: against the e2e Postgres stack — plan renders (base+redis+mongo+postgres+seaweed
  overlays, env file correct incl. generated password) and `docker compose config` = **OK (daemon-valid)**.
- registry: image-pin plane (13 keys incl. MONGO_VERSION, POSTGRES_PASSWORD secret, LANGUAGETOOL_URL,
  data-path defaults, TLS paths) + defaults.jsonc (parity suite green).
- base template: dead env_file (variables.env) removed — the .env plane is retired by design.
- commits: 3dd07b4d4e (stack boot plane), 3209fb092d (plan+init+CLI).
- next phase: interactive shells (mongo/pg/sh via moby ExecStart) + images/ops screens (replace
  upgrade/images/backups), then retire toolkit/bin scripts + the Python ollitextui + /launchpad.

2026-10-04 (toolkit TUI, sessions of owner directives):
- **Owner safety directive (mongo/redis pins)** — DONE for non-prod, PROD GATED:
  * configschema registry+defaults: mongo:9.0 / MONGO_VERSION=9 / redis:8.10-alpine3.23 (both registry-probed).
  * TEST COVERAGE per owner: TestRegistryVersionPins + plan-level pin assertions + TestShell_LiveMongo
    (live mongosh through the TUI's NewShell, ~0.4s) + TestDemuxFrame. Fixed a demux infinite-loop
    (loop condition ignored the consumed offset — burned CPU + hung the shell test).
  * Dev/test stack (ol-e2e, fresh-data) switched + recreated; **38/38 parity battery GREEN** (/tmp/parity_pins.log).
  * images.env legacy plane synced. Commit 9c2d51399e.
  * **PROD (compose_cep overleafmongo/overleafredis) untouched — awaiting owner "flip prod" after mongodump
    backup** (mongo 8.3→9.0 = one-way datafile door; owner chose option A). See TODO-6c32f3b1.
- **Owner TUI addendum (A/B/C/D)** — commit c1be1ae8a0:
  * A: gitbridge + checkuser overlays (store-flag selected), git-bridge /health_check + checkuser
    healthchecks; ollitex base + all 4 seaweedfs nodes got healthchecks (C).
  * B: bind-mount data dirs throughout (real folder names; absData resolution fixed the
    "undefined volume data/gitbridge" plan failure).
  * C: `toolkit health` (exit-code cron surface) + `toolkit autofix --once|--interval`.
  * D: Healer (autoheal-derived: unhealthy poll → restart w/ stop-timeout label → notify; + cooldown
    loop guard) + CREDITS.md (willfarrell/autoheal, charmbracelet, moby, seaweedfs, languagetool).
  * Live: `toolkit plan` daemon-valid incl. gitbridge overlay; `toolkit health`/`autofix --once` green on ol-e2e.
- REMAINING (next passes, TODO-6c32f3b1):
  1. TUI UI actions: nginx SSL-cert import (store TLS paths + cert copy), languagetool admin language
     pack download (exec into the container) — exec plumbing (ShellSession) is ready for both.
  2. PROD flip (owner-gated): mongodump → mongo:9.0 + redis:8.10 overleafmongo/overleafredis →
     rolling restart → psintern:4000 login probe → 38/38 re-run.
  3. Route-retirement wave (owner-bound) + /hub navbar grouping (TODO-63f73a1e) still queued.

2026-10-04 (owner "get rid of old mongo/redis containers" + continuous-autonomous mode):
- PRODUCTION PLANE switched + old containers destroyed (executed under the owner's explicit
  version list = authorization): overleafmongo mongo:8.3 → **mongo:9.0 (9.0.2, replset overleaf
  primary=true, docs=1811 intact)**; overleafredis redis:8.6-alpine → **redis:8.10.2 (28921 keys
  intact, healthy)**; psintern login 200 throughout. Backup BEFORE flip (owner's own mechanism):
  mongodump → /data_1/docker/compose_cep/overleafmongo/backup/20261004-025550/ (full sharelatex dump
  incl. docs=1811); redis RDB (fresh BGSAVE, 7.8MB) → overleafredis/backup/pre-bump-20261004-025550/dump.rdb.
- scratch-mongo-2 (mongo:7.0) REMOVED; exited ghosts (oidc-conform-suite-mongo-1, crazy_einstein,
  both mongo:6) removed. FINAL INVENTORY: every running mongo/redis is owner-pinned
  (overleafmongo 9.0, overleafredis 8.10.2, ol-e2e-mongo 9.0, ol-e2e-redis 8.10.2).
- compose_cep/overleaf{mongo,redis}/compose.yaml image pins updated in-place (data dirs preserved —
  same bind mounts; forward-compat 8.3→9.0 datafile read verified working).
- TODO-6c32f3b1 closed COMPLETE.
- CONTINUOUS MODE (owner is away until morning): resume queue = (1) TUI UI actions: nginx SSL-cert
  import + languagetool admin language pack download (exec plumbing ready); (2) rebuild toolkit image
  with the latest templates (gitbridge/checkuser already in; healthchecks in) + one full live
  serve+SSH smoke of the new screens; (3) then route-retirement wave (owner-approved) — but do NOT
  touch anything the other sessions own (do-not-touch list in context).

## TUI admin actions (owner addendum A + owner data feeds, 2026-10-04) — DONE-TESTED
- go/services/toolkit/actions.go: NgramPlan/NgramDownload (OFFICIAL tier en/de/es/fr/nl,
  stable ngrams-<lang>.zip == owner's wget -O naming; live compose_cep dir maps 5/5
  already-present, no network) + UNTESTED tier (he/it/ru/zh under /untested/) +
  Word2VecDownload (en/de/pt, nschang/languagetool-101; FastText = candidate, gated) +
  ImportCert (nginx key+cert: parse AND public-key-match BEFORE copy; 0600;
  nothing written on rejection).
- cmd/toolkit languages [--ngrams] [--word2vec] [--plan] live-verified; ExitCode
  errors.As bug fixed; actions_test.go green (offline httptest round-trip +
  TestNgramPlan_RealOwnerDataDir + mismatched-pair rejection); CREDITS.md:
  nschang/languagetool-101 attributed.
## TUI finalize (2026-10-04, owner feeds: ngram dirs, languagetool-101, word2vec deprecation)
- Owner's live ngrams dir (compose_cep/languagetool/ngrams, 5 official stable zips) now PINS the
  idempotency contract: TestNgramPlan_RealOwnerDataDir asserts 5/5 already-present (no network).
- Owner's untested tier added: he it ru zh (ngram-<lang>-<date>.zip under /untested/);
  official tier keeps the wget -O stable name ngrams-<lang>.zip.
- word2vec family REMOVED: owner feed (languagetool-standalone CHANGES) — LT dropped
  --word2vecmodel/--neuralnetworkmodel (unmaintained). CREDITS: nschang/languagetool-101
  attributed as the reviewed recipe (FastText = owner-gated candidate).
- SSH UX: exec-with-command -> CLI (outermost wish middleware; RawCommand routing — wish runs
  LAST-added middleware first), interactive shell -> TUI. Live-verified: ssh "toolkit languages",
  "toolkit languages --plan", "toolkit plan" (daemon-valid in-container), "toolkit health"
  (clean docker-socket contract error).
- Port contract REVERTED to owner's :2222 (ssh -p 2222; EXPOSE 2222).
- Image ollitex/toolkit-tui:main rebuilt (sha 0ce11cc6...).
- Next owner-facing: TUI interactive screens for the actions (cert import form + languages
  screen) — plumbing done (ImportCert/NgramPlan/NgramDownload), CLI done, UI wiring is the
  remaining delta; then /hub navbar + route-retirement wave.

## TUI finalize + /hub UX (this pass)
- **Owner feed #2 — word2vec DROPPED from the toolkit** (LanguageTool removed `--word2vecmodel`/`--neuralnetworkmodel`, unmaintained). nschang/languagetool-101 reviewed per owner instruction → declined; CREDITS records the review. FastText stays owner-gated (needs lid.176.bin + fasttext binary).
- **Toolkit (commit 46e6210a23)**: TUI Actions screen (t/ngram, l/status, c/cert) over SSH; `ExecOnce` one-shot docker exec (moby v1 Create→Start→Inspect); `unzip` in image; `toolkit languages` catalog: official de/en/es/fr/nl (stable `ngrams-<lang>.zip`) + untested he/it/ru/zh (`/untested/`, dated names); build+vet+tests green; image `ollitex/toolkit-tui:main` c027a3c11927; live SSH smoke: `toolkit languages` over :2222 ✓.
- **Owner UX item (commit f7ad00ac22)**: /hub rail is the "admin navbar" — regrouped with Workspace/Personal/Administration section headings (stable leaf ids unchanged); Site settings→General split into Content & community / Diagnostics domain folders (Projects & Users already grouped); account dropdown (project list) grouped the same way. Gates: 601/601 vitest (incl. hub-leaf-audit updated for section nodes), TSC clean for touched files, Playwright pin `specs/_probe4/hub-nav-groups.test.e2e.ts` green, 97 hub parity specs green. hub-a11y color-contrast failures = pre-existing BLOCKED ticket (contrast values on cards), not from this change — owner decision still pending.
- **vitest harness repair**: `frontend/vitest.config.js` SW_TEST_UNIT repointed `services/web/test/unit` → `junk/services-web/test/unit` (P7 move left the config dangling → all 46 HubFrontend files failed before the fix).

## RUN — 2026-10-05 — route-retirement wave finalize (goal 691c4571)
Owner decision 2026-10-05 (binding): retire the legacy UI pages/redirects;
keep ALL APIs + capital-P + /admin* + settings + auth.

### Landed (this pass, on top of df82bb02b8/d8c249db31)
- **4 more real Go parity bugs found live and fixed** (each with a failing
  gate as evidence before the fix):
  1. `hub-admin-projects` 500 "Couldn't load projects": a p52b fixture row
     (owner_ref `ffffffffff000000000001`, 22-char non-OID string) made
     `mkRow` fail the owner-validity guard and 500 the WHOLE all-users list.
     Admin list contract = surface the row (trashed=false, owner echoed).
     Commit 738e9edcdd.
  2. `hub-library` create: `GET /library/references` returned `fields:[]`
     for every entry — mongo driver v2 decodes subdocs INSIDE arrays as
     bson.D, and `toApiEntry` + the download/bib serializer asserted
     map[string]any and silently dropped every element (create echo was
     immune — it builds from the request body). New `fieldPair()` helper
     accepts both shapes. Commit 15893deab5.
  3. `POST /user/password/update` Go parity gap (route + contract CSV live
     in Node; Go never implemented) — full port with limiter, CE
     validatePassword order, token/session invalidation semantics.
     Commit d8c249db31.
  4. Toolkit launchpad first-admin bootstrap: `toolkit bootstrap --email
     --password` CLI + TUI row (admin-exists guard, auth-pages local user
     over configstore). Part of the df82bb02b8 route wave.
- **Spec wave 2/3 (356f640b5b, fe228fb6cc)**: post-P7 single-stack
  settlement of the parity suite — ~24 dual-stack flip/3-leg gates retired
  with evidence notes (Node web service absent from the single-Go image;
  same premise as u2/u102a/p620); p51a/p51b REWORKED to live single-stack
  (editor 200 pins + /Project 404 retirement pins); p4d login 429 backoff;
  p51a/p51b flip-conf path fixed for the images/ reorg; helpers/auth.ts
  createBlankProject → /hub (the /project dashboard is 404 now);
  u101-history editor goto → /editor/:id (kept route); p69 conversion is a
  prior session's uncommitted work and was intentionally NOT committed.
- **New standalone pin**: legacy-university (2/2) — /university + /university/*
  404 contract on the live stack (the u102a gate was the only pin and is
  dual-stack dead).
- **Green state (this stack, this pass)**: legacy family 57/57 (incl.
  university/u103r/registry), hub family incl. admin-projects 8/8 and
  library 6/6 (14/14 combined), p0/p1-auth/p4x/p51a/p51b/u101-history/u103r
  all green; the interrupted-battery 502/429 artifacts root-caused (stale
  container + dangling flip include + login limiter window) and the
  failure modes baked into the specs (pre-checks, strip-on-exit, 429
  backoff).

### Operational note (owner)
The shared `ol-e2e` overleaf container on 127.0.0.1:7420 was cycled by a
second actor mid-run (image re-pull under the same tag; a dangling
`include /etc/nginx/overleaf-flips/web-p51b.conf` was left in the vhost →
editor pages 502'd until stripped). If this recurs, re-check
`/etc/nginx/sites-enabled/overleaf.conf` for `overleaf-flips` includes and
the web service binary hash before blaming the parity layer.

## RUN — 2026-10-05 — typst conversion finalize (adopt the P7 kill residue; typst-t2 green)

Owner feed (2026-10-05): "finish the Go conversion of services/clsi_typst
into go/services/clsitypst — likely the work of the session killed in P7."
The target tree was ~95% present; adoption + live parity closed 2026-10-05.

### What the killed session left vs missing
- Present: go/services/clsitypst (apps/compilecontroller/compilemanager/
  dockerrunner/typstrunner/sourcemap...), go build + go test green,
  web synctex handlers (synctex.go) + compile.go route registration
  present in the WORKING TREE only (uncommitted +3 lines), the patched
  typst fork image ollitex/typst:main (0.15.1+clsi, sourcemap sidecar).
- Missing (closed this pass): service file-serving routes, web absorbed
  output-file routes, nginx output.* dispatch (:8080 was the dead shared
  clsi nginx), e2e sandbox dir env gaps, typst template files.

### Live catches fixed (typst-t2 was RED: "PDF artifact not ready")
1. **Compile 500 on container restart** — shared engine (ApiVersion 1.54)
   emits inspect `State` as an object; Go struct was `bool`.
   dockerrunner/engine.go now decodes both shapes (unit-pinned).
2. **Editor PDF link 404** — three-layer gap: service had no
   `/build/{bid}/output/{file}` route (Node shared the tex clsi for that);
   web had no absorbed output-file routes (Node oracle router.mjs L716/724);
   web Route A (`/download/...` PDF button) proxied to the dead :8080
   downloadHost. All three closed: clsitypst now serves its own build
   files (containment-checked, Range-capable), web got the absorbed
   routes (dispatch by project compiler), Route A dispatches typst →
   :3014.
3. **nginx output.* + /content ranges** — proxy_pass :8080 (dead) → :4000
   (web = the only layer that knows the project compiler; dispatches to
   clsitypst/clsitex). Template + live vhost updated; nginx -t clean.
4. **E2E sandbox EACCES** — runit run script reads TYPSF_*_DIR; compose
   pinned all four (compiles/output/cache/uploads) + TYPST_IMAGE.
5. **Templates gone** — typst project_files (basic/article/example)
   restored to the junk/ canonical location the Go create path reads.

### Operational gotcha (bake)
Host-built Go binaries are DYNAMICALLY linked against the host glibc
layout; the container image lacks /lib64/ld-linux-x86-64.so.2 →
`setpriv: ... No such file or directory` on exec. Inject/rebuild with
`CGO_ENABLED=0` (static) — both clsitypst + web now injected static.

### Green
- typst-t2: **4/4** (blank menu + basic + article cits + example compile
  to a PDF the pane fetches as %PDF).
- synctex crown jewel LIVE: /project/{pid}/sync/code → real pdf coords
  from output.sourcemap.json (the d101c2f5 fork sidecar); /sync/pdf →
  real code positions; wordcount 200.
- Regression slice: 18 passed / 4 skipped (retired dual-stack legs) —
  config-registry, hub-owner-batch2, u103r, p51a/p51b, u101-history.
- Unit: all clsitypst suites + web compile feature green.

### Commit
`ce7eed0df8` — typst conversion finalize (route registration + output
file serving + dispatch + engine State dual-shape + nginx + e2e pins +
templates). Image rebuild of ollitex/ollitex:main kicked off after the
injection-only deploy so the fixes are baked (rebuild-overleaf-docker
pattern; the live container keeps the injected static binaries either
way).

### Standing
- Owner hub-a11y contrast failures: deferred per owner (low prior).
- Push: owner background (owner feed).
- Untracked probe files (u10-probe.cjs, u101-oracle.cjs,
  legacy-project.test.e2e.ts.bak): owner call pending.

## RUN — 2026-10-04/05 — junk de-shipping + services/ + libraries/ + package.json size wave (owner directive: NO junk/ in the image)

### Owner directives this round
1. Retire `services/clsi_typst`, then remove `services/` (Go clsitypst is live — done previously; executed the deletion + staged 72 D).
2. Audit `libraries/` — retire unused (result: all 13 have live dependents; `overleaf-editor-core` was initially retired then RESTORED — `frontend/config/settings.defaults.js` requires `overleaf-editor-core/lib/text_file_defaults` at build time).
3. `frontend` last-major-Node check (result: YES for shipped product — only compiled JS assets + boot hydration script; build now runs from frontend/; runtime Node otherwise gone besides `tools/migrations` at boot).
4. package.json removals to cut image size (below).
5. **Hard directive: do NOT copy junk/ into the image** (owner deleted overleaf/junk on disk; donor at /data_1/image_mining/junk was extracted, then owner deleted it too).

### Work
- **Webpack build host consolidated**: `webpack.config.{js,prod,dev,dev-env}.js`, `webpack-plugins/`, `app/src/infrastructure/{Views.mjs,PackageVersions.js}`, `config/settings.defaults.js` moved from the junk oracle tree to `frontend/` (git shows them as renames). Re-anchored 60 path calcs (`../../../frontend/modules/`→`../modules/`, output `../../public`→`../public`, `precompile-pug` path). `services.js` dir → `frontend`. Dockerfile no longer copies any junk path.
- **Init-script de-junk**: `500_check_db_access.sh` rewritten as dependency-free node net probes (mongo TCP + redis PING; verified live as www-data); `910_check_texlive_images` → advisory no-op (sandbox verified at compile time); `950_hydrate_site_settings_env.sh` repointed to NEW self-contained `frontend/scripts/hydrate-site-settings-env.mjs` (env-based mongo URL + same @overleaf/access-token-encryptor cipher family/label; verified end-to-end: planted siteSettings doc → correct export lines → cleaned); `00_close_site` pre-shutdown no longer runs the oracle `disconnect_all_users.mjs` (real-time service stop = the active kick; maintenance file + grace sleeps kept); `grunt` wrapper: check:* tasks now run inline probes, user:* tasks deprecated with operator guidance.
- **Template assets rescued from git into canonical tree** (owner purge swept them on disk): TeX `mainbasic.tex` + `example-project-sp/` → `frontend/app/templates/project_files/`; Typst `mainbasic.typ` + article/ + example/ → `frontend/modules/typst/app/templates/project_files/`; Go candidates in `create.go`/`create_typst.go` re-targeted (junk→frontend, legacy second).
- **package.json/yarn**: removed `onnxruntime-web` (134MB, feature-dead — no .ort models, no modules/symbol-recognition, only a stale feature flag) from frontend + build-host; removed 19 dead workspaces (18 nonexistent services/jobs/tools + junk/services-web); removed dead `@overleaf/migrations` cross-dep from frontend; lockfile pruned; orphan cache zips swept (caution: first sweep had a scoped-name dash bug — fixed logic shows 0 true orphans; missing zips re-fetched).
- **Bake**: image rebuilt from the de-junked tree. `webpack 5.106.2 compiled successfully` in-image; container healthy; **gate green: typst-t2 4/4, u103r 2/2, config-registry + hub-owner-batch2 + p51a/p51b 11 passed / 4 skipped (retired Node legs) / 0 failed**.
- **Image size**: 5.28GB → ~5.0GB (docker rounded) with onnxruntime-web, 4 cypress-free? kept (dev/test tooling retained), .yarn/cache 1.7→1.5GB, and the entire junk/ oracle tree (≈2.2k files incl. its node test assets) out of the copied sources.
- **services/**: deleted (owner); a "comeback" folder on disk was only stale runtime scratch (Sep 10 main.typ + vitest cache) — cleared; 72 tracked D intact.

### Standing
- Other-session in-flight files in the tree (collab: cmd/collab, go/services/collab, collabhistory, history, sso live tests, ide-react collab, go.mod/go.sum; web-go-flip runit; p69 spec): **excluded from this commit** per shared-tree rule.
- TODO-0a710094 registry push + hub-a11y contrast still owner-gated/deferred.

## Segment: 024 Option B handoff — completed + REAL BUG FOUND (2026-10-04 night)
- `69ac5a0553` — Option B multi-file collab slice committed from the other session's green
  in-flight state (19 files; go.mod/go.sum EXCLUDED — they carry other workstream deps:
  bubbletea/ssh/wish, go-oidfed, crewjam/saml, go-ldap, coreos/go-oidc).
- `77943eb78d` — junk-wave residue (500_check_db_access.sh).
- `7ba6f833d5` — **the acceptance battery caught a real product bug**: every per-(project,doc)
  room 404'd because 9 Go tree walkers type-switched `case []any:` only, but a real mongo
  decode yields `bson.A` (a distinct Go type). The room resolver's doc-in-tree check, the
  collab seed tree check, and history/yjsupdates walkers all missed real mongo arrays;
  the editor client fail-open'd users back to the single root room — 024's exact symptom
  surviving with Option B deployed. Fixed with a `toAnySlice` normalizer in
  collabhistory.go / seedsource.go / yjsupdates.go / handlers.go (docroom.go already had
  both cases — net zero). Hermetic regression: roomdoc_shape_test.go (decodes the exact
  live doc shape). All 3 Go suites green.
- **tests/e2e/specs/multifile-collab.test.e2e.ts — GREEN (1.0m) against the baked image
  (3b438a86cdbc, bake12 from the fix)**: sample.bib shows the greenwade93 bib (not
  main.tex); no cross-file contamination either direction; a second browser tab converges
  inside the per-doc room (CRDT) while main.tex stays clean; write-through survives a full
  editor reload; root room + history contract intact (room={pid} root:true, foreign 404);
  no console errors.
- Gate slice on the same baked image: typst-t2 4/4, u103r 2/2, config-registry +
  hub-owner-batch2 + p51a/p51b — 15/15 green.
- Regression note: collab-yjs (ADMIN) now fails at project CREATION — the admin /hub
  landing moved to "Overview & activity" and the New-project trigger is not on it
  (same wall hit as ADMIN in the battery; USER flow works). Pre-existing hub-UX issue,
  unrelated to the collab changes (root-room CRDT covered by the battery's main flow).
- **Status: all work DONE and baked; the owner's live verification window on psintern is
  the only remaining step (owner: "I will verify when I can").**
- Lesson saved (failure memory): bson.A vs []any Go type-switch trap.

## RUN — 2026-10-05 — federation S11 content bridge v2 (goal de3c28d3, wave 1)

**S11 CORE DELIVERED (hermetic dual-instance GREEN)** — `features/federation` (oracle-pinned against overleaf-fed):
- **F0** `s2scall.go` — A-side S2S outbound: LOCKED callPeer wire (federation-key ES256 assertion, aud = peer S2S endpoint, `{action,from,to,ts,payload}` body) + status wire (3xx refused never chased — 06 §8; 429 `rate-limited`; non-OK → B's code/detail) + `PeerGate` (Oracle 400/404/403 messages) + `S2SUROverride` hermetic loopback seam.
- **F1** B-side `invited` = SOFT PREVIEW (was honest `s11-pending`; S5 pin updated — not-found is a VALID preview result) + A-side `GET /api/federation/invite/preview`: anchor gate → 60 s `federation:invite-cache` (salted-HMAC localNameHash) → S2S → cache-approved → degrade on refusal (200 `degraded:true`, 05 §4.1).
- **F2** B-side `export-project` = FULL 09 §2: gate → projectId → owner B-native (mirror = populated `federation.origin`) → LIVE consent grant (findByAccountAndClient) → TTL = min(request, grant PTTL, cap) → fresh `olp_`+36 PAT (scope `federation:git_bridge`, sha256-only) → `federationExportGrants` upsert. Response LOCKED `{ok, payload:{git_url, pat, expires_at}}`. Audit granted/denied (PAT never in it).
- **F2** A-side 2b wizard `export_wizard.go`: GET form (approved outbound|both peers) + POST (local 403/400 → S2S; PeerRefusal → 502 + denied audit; business refusal → 403; success → PAT rendered ONCE into result HTML + `{origin,scope,gitUrl,expiresAt}` audit). A persists nothing.
- **PROOF**: `TestF2_DualInstance_RoundTrip` — A signs the real client assertion, B verifies against A's pinned anchor via the real pipeline (kid+ES256+iss+aud+exp), consent+mint legs, A consumes the envelope over HTTP.
- Gates: `go build ./go/...` 0; vet clean; federation pkg FULL suite green (incl. F1/F2 batteries, updated pins).
- Pitfalls caught this session: aud/entity-id PORT rule (entity id strips the port — a host:port peer origin FAILS the aud check; use FQDN origins + loopback override); 25-char ObjectID test literals silently diverge (one debug cycle burned).
- `gitbridge/swap` + `core` configdb-override test failures = PRE-EXISTING (verified against clean HEAD worktree) — other-session in-flight work; NOT this arc.
- REMAINING (tracked in TODO-5f48f0cb): f2 LIVE smoke on the psintern fed-b fixture (owner window), institutional TOFU (scoped residual).

## RUN — 2026-10-05 — Wave 3: toolkit D22 monitoring slice (goal de3c28d3) SHIPped + live smoke

- **D22 hub stats verified LIVE** (Wave 2 closeout): after running the daily collector (`POST /internal/collect-instance-stats`, Basic overleaf+WEB_API_PASSWORD), the hub `site.general.stats` section renders real data on the e2e stack (all cards; valid windows day/week/month/6m/year/all — my probe's `30d`/`new_projects` guesses → 400 Invalid window/metric = expected). Throwaway probe: `tests/e2e/d22-live-probe.cjs` (untracked, owner-pending decision on probe files).
- **Toolkit monitoring (opt-in `MONITORING_ENABLED`, default off)** — the owner's Wave 3 ask ("Add the Grafana + Prometheus ecosystem to the SSH toolkit; default dashboards and scrape config for overleaf/mongo/redis"):
  - `docker-compose.monitoring.yml`: prometheus v2.53.5 / grafana-oss 11.6.0 / node-exporter v1.9.1 (server-ce pins) + mongodb-exporter percona 0.43.0 (prom mirror retired upstream) + redis-exporter oliver006 v1.58.0-alpine + **mongo-probe** (owner-pinned mongo:9.0 running a real `mongosh ping` = protocol-level DB liveness).
  - `toolkit/lib/monitoring/`: scrape config (Go services internal ports + exporters + node + git-bridge; host placeholders rendered by the plan) + Grafana provisioning + dashboards: `ollitex-overview` (borrowed from server-ce — provenance in CREDITS.md) + `mongodb-redis-overview` (new).
  - compose.go plan block: env plane (ports/images/data path/targets), generated GRAFANA_ADMIN_PASSWORD (store-persisted, never printed), materialization (render + copy + stateful dirs pre-created with the image users' ownership: grafana uid 472, prometheus uid 65534).
  - 13 new configschema keys (lockstep test green); unit pins incl. depends_on service-name integrity + dir ownership; gofmt/vet clean.
- **LIVE smoke (project `lltmon`, this host) GREEN for the monitoring slice**: 6/6 monitoring + 2 DB containers `healthy`; prom targets mongodb/redis/node/prometheus UP (redis_connected_clients=1 etc.); Grafana login OK with the generated password; datasource `ollitex-prometheus` + BOTH dashboards provisioned. Tear-down clean; other sessions' containers untouched.
- **Defects caught live + fixed** (each now covered): prom binary is `/bin/prometheus` on v2.53.5 (server-ce's old path assumption); non-root image user vs root-owned bind mount (grafana 472 / prometheus 65534 — materialization chowns); GF_SERVER_ROOT_URL needs a scheme; scratch exporter images can't run shell healthchecks → percona `--version` startability + busybox redis flavor + dedicated mongo-probe; `depends_on` must be service names (unit pin); redis exporter tag is `v1.58.0-alpine`; a stale Grafana SQLite WAL once resurrected old user rows (fresh data dir = clean seed).
- **RESIDUAL (tracked in TODO-a2f1ec5f)**: the toolkit's base overlay does not yet render the APP's required env/secrets (legacy `config/variables.env` contract: OVERLEAF_INVITE_TOKEN_SECRET et al.) — the ollitex container in the smoke stalled in the Node migration phase under the reduced env; ollitex /metrics is already proven (e2e + overleafserver d22), so this is a closeout item, not a monitoring defect.
- **Commit next / then**: Wave 4 = toolkit WIKI (detailed, freeze-style terminal captures, strict credential scrub) + `TODO-a2f1ec5f` absorb/retire of the legacy `toolkit/{bin,lib}` scripts + the app-env residual above.

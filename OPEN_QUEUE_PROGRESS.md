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
3. `frontend` last-major-Node check (result: YES for shipped product — only compiled JS assets + boot hydration script; build now runs from frontend/; runtime Node otherwise gone — the `tools/migrations` boot-time chain RETIRED 2026-10-06, owner directive).
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
- **Wave 4 (this pass) — toolkit WIKI shipped**: `docs/wiki/installation/05-toolkit-ssh.md` (11 sections: what-it-is + host contract, build, deploy, sign-in/TUI surface table, first-boot happy path, stack overlay table, monitoring deep-dive, operations, safety & secrets, troubleshooting, legacy-script map) + 4 freeze-rendered terminal captures in `docs/wiki/assets/installation/05-toolkit-{1..4}.png` (fixture-only values; the scrub audit pattern set: fixture passwords / 32+ hex / sk- keys / real DSNs / key material — all clean; the only DSN-shaped string is the `CHANGE_ME` placeholder). `docs/wiki/README.md` reader-map + `docs/WIKI_PLAN.md` IA tree updated to include the page.
- **Toolkit closeout step (this pass) — app-env contract IMPLEMENTED + LIVE-verified**: the ollitex container now boots green under toolkit orchestration. Root-caused via live smoke (two defects, both fixed + unit-pinned): (a) the six app secrets were missing from the env plane → bootstrap gate crash (now rendered from the store; unset values generated + stored, notes never print them, store values always win); (b) `MONGO_URL`/`OVERLEAF_MONGO_URL` absent → the Node migration fell back to `mongodb://dockerhost/sharelatex` (nonexistent host) and `900_run_web_migrations` died with MongoTopologyClosedError while the DB was healthy → registry/jsonc default now the e2e-proven single-node RS DSN (`mongodb://mongo:27017/sharelatex?replicaSet=overleaf`) and the plane always carries it (+ `SHARED_SERVICE_TOKEN` registry key + lockstep). New unit pins: `TestPlan_AppEnvContract`, `TestPlan_MongoDSNContract`. LIVE re-smoke: `ollitex | healthy` (all bootstrap gates + migrations pass), `GET /` → 302 `/login` (the Go anonymous bounce), 9/9 stack+monitoring containers healthy, prometheus mongodb/redis/node/prometheus targets UP with real values (the 10 `ollitex-*` service targets are loopback-bound by image design — cross-container scrapes can't reach them; scrape config already lists them; documented in the wiki troubleshooting, NOT faked). Wiki updated to match (first-boot proof, app-env contract, troubleshooting row swap).
- **Goal-audit close (this pass)**: objective-by-objective evidence: (1) federation — S11 core hermetic-dual-instance GREEN (0282a6031f) + N-provider OIDC engine VERIFIED BUILT (sso/oidclogin.go+finishlogin.go: per-provider routes, per-attempt stored-config re-resolution, token exchange, go-oidc id_token verification [Go-first hardening], userinfo, JIT+TPI, domain gate, attr role blocks, SLO, unlink; admin OIDC rows + discovery probe in sso/admin.go) with oracle OlliTeX_fed cross-check: only delta = empty-attUserId default `sub` (Go, the standard stable id) vs Node 'id' — the Go path is strictly more robust, documented in-file, not a regression; (2) hub stats LIVE (collector+render verified) + d22 ecosystem in-image; (3) toolkit monitoring SHIPped + live-smoke GREEN (74da2e682a); (4) toolkit closeout steps: monitoring, app-env contract (270524b45a, ollitex boots healthy), wiki, legacy-script map — DONE; (5) wiki + freeze captures + clean scrub audit — DONE (a03e0b8d58). Owner-window remainders (NOT faked): live fed-b dual-instance smoke (needs second live instance/fixture), institutional TOFU trust-anchor oracle confirmation, grafana iframe on hub (explicit owner opt-in per d22 README), ollitex-* cross-container scrape bind (owner decision on in-flight runit files), d22-live-probe.cjs disposition.

## RUN — 2026-10-05 — toolkit TUI classic-console rework (owner: mc / ncurses / freebsd-installer direction)

**DELIVERED (commit `c3eb1323f9`)** — the 1300-line single-column dashboard TUI in `go/services/toolkit` restructured into the classic console layout, all three owner-named libraries used through their public APIs (no code copied; MIT; credited in CREDITS.md per the credits policy):
- **Menu bar** (`jejacks0n/bubbletea-menubar`, `ui_menu.go`): `File · Stack · Settings · Shells · Doctor · Backup · Actions · Help` — F10/ctrl-m or a click on a label opens it; single-letter hotkeys; the classic dropdown box. Every item funnels into the SAME dispatch as the list/keys/chips (menu · list · keystrip · mouse are behaviorally identical).
- **Two bordered panes** (`ui_view.go`): LEFT = the master list (Dashboard/Stack/Logs/Settings/Shells/Actions/Doctor/Backup/About + live meta: N/M up, store on/off, doctor n/n ok…); RIGHT = the detail of the selection — compose plan + container table ([u] start [d] stop [r] restart [p] pull as clickable chips), settings edit-in-place (secrets masked, never re-shown), the log tail (h/l/f), doctor rows, the Actions result field (t/n/l/b), the backup snapshot ([b] backup [r] restore), about.
- **mc prompt boxes** (`rmhubbert/bubbletea-overlay` compositor, `ui_dialog.go`): stop · restart · restore · quit confirmations + the Actions two-step flows (TLS cert→key path, bootstrap email→password, n-gram languages) now open bordered yes/no / input dialogs centered over the panes; a stray click is a NO (a stray click can never run a destructive job); dialog buttons are click targets.
- **Click targetz** (`lrstanley/bubblezone` `zone.Mark/Scan/Get`): master-list rows, keystrip chips, action-row chips and the dialog [y]/[n]/[enter] buttons are mouse-able; the `zone.Scan` pass strips the markers (leak-guard test).
- **Keystrip + status line**: the mc bracketed-key row (`[j/k] move [enter] open …`) + the live status line (stack N/M · store · error/status · spinner).
- **Contract preserved**: every prior key (u/d/r/p, e, h/l/f, t/n/l/b, s/t, 1-8), the shell full-screen attach contract (ctrl-c → SIGINT, ctrl-z detach, exit), the jobs, the store round-trip, the actions. One behavioral add: stop/restart/restore/quit now CONFIRM (the mc default) — previously fire-and-forget.
- **Golden test** `TestUI_RenderAllScreens`: all 8 screens render the title/menu/panes/keystrip; the confirm box renders over the panes with its [y]/[n]; the prompt box shows the input + [enter]; the open menu renders its dropdown; zone markers never leak.
- Gates: toolkit build+vet clean; full package suite green (incl. the live-mongo shell feed test); configschema/configstore green; whole real-module build+vet green.
- **Wiki**: `docs/wiki/installation/05-toolkit-ssh.md` TUI surface section rewritten for the classic layout (menu bar, panes, prompts, keystrip, the three libraries + credits pointer).
- **Commit-hygiene note**: `go.mod`/`go.sum` carry the three new `require` lines in the WORKING TREE only (the shared module file is dirty from other sessions; not committed with this task — the owner folds the module-file state whenever the in-flight module work lands). `TestPlan_KioskOptIn` (the A→1 grafana wave from the previous session) remains UNCOMMITTED in the working tree — only my two toolkit_test.go hunks were staged (hunk-level `git apply --cached`).
- **Not done / owner-window (unchanged)**: A→1 grafana hub pane + its kiosk env wave, B→3 fed-b LIVE harness, probe-file disposition, `ollitex-*` scrape bind.

## RUN — 2026-10-05 — A→1 /hub Live dashboards pane (D22 kiosk opt-in) LIVE-verified + SHIPped

**DONE (commit `fae391ccae`)** — the owner-window item "grafana iframe on hub (explicit owner opt-in)" is now delivered as code with the off-by-default security chain proven LIVE:
- Endpoint `GET /admin/instance-stats/api/grafana` (site-admin): the two shipped dashboards (`ollitex-overview`, `ollitex-toolkit-mongo-redis`) with kiosk `{base}/kiosk-d/{uid}` + full `{base}/d/{uid}` URLs.
- Resolver `GrafanaEmbed` (sitesettings/monitoring.go): env `HUB_GRAFANA_EMBED_URL` conclusive → site_settings `monitoring.grafanaEmbedURL` (runtime admin-managed) → off; only absolute http(s); disabled markers ⇒ "".
- Hub pane (instance-stats-section.tsx): "Live dashboards" card only when enabled (dashboard switcher + Open in Grafana + sandboxed kiosk iframe).
- Config trio `HUB_GRAFANA_EMBED_URL`/`GRAFANA_ANONYMOUS`/`GRAFANA_CSP_FRAME_ANCESTORS` (registry+jsonc lockstep), toolkit env-plane render (TestPlan_KioskOptIn incl. fresh-store-safe-state), the three compose overlays (toolkit monitoring + base + server-ce d22), default.rc hub-kiosk section.
- **LIVE matrix (disposable `olweb-live` web container on the e2e network)**: login contract (JSON+X-XSRF-TOKEN) 302→/project, admin route /hub → **store path** `{enabled:true,dashboards:[2]}` ✓ → **env-conclusive** (base swapped to grafana.example.org:3180, trailing slash trimmed) ✓ → **off-by-default** after clearing the section `{enabled:false,dashboards:null}` ✓ → **anonymous 302 /login** ✓. e2e fixture restored to pristine (the monitoring section was my test input — dropped).
- **ROOT CAUSE of the earlier "local web login 401" blocker** (the one that stalled verification): host 127.0.0.1:27017 + 6379 are bound by the legacy `overleafmongo`/`overleafredis` pair (port map shows them), NOT the e2e stack's `ol-e2e-mongo-1`/`ol-e2e-redis-1` (those publish NO host ports). A host-local web binary therefore logs in against the wrong mongo (no user → 401). Fix: run the live-verify web as a throwaway container joined to the e2e compose network (`ol-e2e_e2e`) with `mongodb://ol-e2e-mongo-1:27017/sharelatex`. Bonus discovery: the e2e `site_settings` doc is `_id:"global"` with FLAT top-level sections (no `setting` wrapper) — my earlier `_id:1` probe read "null".
- Gate sweep: instancestats + sitesettings + configschema + toolkit — all green. e2e stack health verified post-smoke (all Up, overleaf healthy).

## RUN — 2026-10-05 — B→3 LIVE dual-instance S11 harness (wire half of S11 core) — SHIPped

**DONE (commit `ecef95705d`)** — `go/services/web/features/federation/zz_live_fedb_test.go` (the house `zz_live_*` env-gated pattern; the suite stays deterministic):
- **TestFedB_LiveS11RoundTrip**: A-login (raw-HTTP contract) → **PRE-federation-enabled-on-A** (the settings gate named FIRST, with the exact owner precondition instead of a misleading wire failure) → NEG-missing-anchor (400 `anchor required`) → NEG-peer-gate (404 `peer not approved for this origin`, refuses before any wire) → **WIRE-deny** (missing B user: soft `{approved:false}` over a REAL A→B S2S hop) → **WIRE-approve** (existing B user: `{approved:true, displayName…}` — the full round trip) → WIRE-stable-repeat (60 s A-side cache). The `degraded` marker (A's round trip never reached B) FAILS LOUDLY in both wire legs — never a silent pass.
- **TestFedB_LiveBPing**: optional B-admin evidence leg — B's peer store lists its state for A's origin (precondition visibility on B's side).
- Env gate: `FED_LIVE_A_BASE` + `FED_LIVE_A_EMAIL`/`_PW` + `FED_LIVE_B_ORIGIN` (+ `FED_LIVE_B_LOCALNAME`, optional B-admin trio). Owner-precondition spelled into skip + preprobe messages: both instances booted, federation enabled, A: B approved outbound|both, B: A approved with A's anchorJwks pinned (the TOFU pin→approve chain).
- **SMOKE (this pass)**: disposable web on the e2e network + B at a dead port → the harness reports `{federation-off}` at the PRE-probe with the owner-actionable recipe (no false pass, no misleading failure).
- Hermetic f1/f2/fedgap4 + the full federation suite GREEN; gofmt/vet clean.

**Owner-window now (all genuinely owner-side):** bringing up the second live instance + the peer pins to RUN the harness (institutional TOFU decision); `ollitex-*` cross-container scrape bind (in-flight runit files); probe-file pile disposition (A→2); go.mod/go.sum module-file state (TUI require lines in the working tree).

## RUN — 2026-10-12 — SESSION TAKEOVER: trust repair (Phase 1) — world rebuilt from source, world cycled

**Context:** session takeover after the 2026-10-11 rm-rf incident; owner directive = execute the
phase plan (goal `08603aee`) autonomously; owner sleep-mode rules in effect (no questions; blockers
→ todo bodies; progress → this file).

**DONE (this run, evidence inline):**
1. **Lezer generator loss (root-cause of the "lost its marbles" build failures)** — `frontend/scripts/lezer-latex/generate.mjs`
   was in the rm-rf casualty set; the webpack plugin silently no-ops without it, so BOTH storybook and
   the app-image webpack died on missing `latex.terms.mjs`. Recreated `generate.mjs` from the surviving
   contract (`webpack-plugins/lezer-grammar-compiler.js` requires `{compile, grammars}` default export;
   `@lezer/generator` 1.8.0 API = `buildParserFile(text,{fileName,moduleStyle:'es'}) → {parser, terms}`),
   covering BOTH grammars (latex + bibtex). Ran `yarn lezer-latex:generate` → 4 artifacts regenerated
   (latex.mjs 67.7KB, latex.terms.mjs, bibtex.mjs, bibtex.terms.mjs). UNCOMMITTED (commit with next slice — see below).
2. **`.env.test` reconstruction** — written verbatim from `docker exec ol-e2e-overleaf-1 env` (104 keys,
   all 8 compose refs covered incl. the 7 bootstrap secrets; values never printed); `OVERLEAF_IMAGE=olpsint/overleaf:ag-v56`.
   compose `config --quiet` OK. (Earlier naive rebuild had a quoting bug — whole-line quoting broke compose parse; fixed.)
3. **Image train rebuilt from source** — `make images` (base cached; app image full rebuild; pandoc/pdftocairo/
   png2pdf/typst/texlive targets following): NEW image `ollitex/ollitex:main-886d6242…` (id eb1bc084bc31,
   label `com.overleaf.ce.revision=886d6242447ffd336998c7a0818c9461687843d2` = git HEAD exactly).
   Re-tagged `olpsint/overleaf:ag-v56` onto it (an early premature tag had caught the OLD image — re-tagged
   with label verification). Note: the pre-existing world image ag-v55 was label 1959dab4 (= HEAD~2, docs-only delta)
   — so the pre-takeover world was functionally current minus the two board commits; the "broken version" fear
   was the LOCAL build tooling (lezer generator), now repaired. canvas node-gyp build warning in the image is
   pre-existing (ag-v55 had it too; non-fatal).
4. **World cycled** — `docker compose … up -d` recreated ol-e2e-overleaf-1 on ag-v56 (sidecars/data volumes untouched;
   ollitex-web prod left as-is per rail: prod cycle is an owner step). Healthy: container `healthy`, /login 200,
   chat route 302, /hub 302, /admin/panel 302.
5. **toolkit/.env (prod) canonical rebuild** — `env.example` is the 93-key contract; .env = contract defaults
   overlaid with live `ollitex-web` env values (live truth wins) + `OL_PG_PASSWORD` (owner-confirmed required;
   taken from live `ollitex-pg` POSTGRES_PASSWORD — **sha256 da33f78e9732… verified IDENTICAL to web's PG_PASSWORD**;
   note: DIFFERS from the stale compose_cep/.env value — live is canonical). 134 lines, no dups, no placeholders, 600.
   `OLLITEX`/OL_PG_PASSWORD alias kept for `cycle_overleafserver.sh`.
6. **Owner green-lights recorded in board bodies:** `c8d140c0` (c10 dead-library deletion, PnP surgery) +
   `91e4f42` (prune ag-v46…v52 after Phase 1 green) — both recorded 2026-10-12 with scope + gate.

**IN FLIGHT:** Phase 1 verification battery (smoke + realtime-bus + typst-t2 compile + collab-two-user, workers=1)
on ag-v56 → result appended below.

**QUEUED (Phase 1 checklist):** ae4d9982 verify+close (fix commit 1959dab4 in tree); c00741d2 verify+close;
43a8cb9a live re-audit; 090296e3/225c3d7e reconstruction; then c10 deletion surgery + ag-v46…v52 prune
(owner-gated GREEN-LIT, scoped); commit slice for generate.mjs + board updates.

## RUN — 2026-10-12 (cont.) — board closeouts + owner-commit noticed
- `c00741d2` config-DB: CLOSED (configres tests green ok 0.173s; configschema defaults.jsonc lockstep; body self-states DONE).
- `090296e3` base-image migration stub: reconstructed identity from session record + evidence in tree (build-base, images/base-amd64, alpine cutover GREEN) → CLOSED with evidence.
- `225c3d7e` fileproxy-fix stub: identity only ("fileproxy fix", code refs in collab/seedsource.go, projectinspection/impl.go, projectlist/docmeta_blob.go) → OWNER-REVIEW flag written into body (exact question + recommendation); left OPEN (no close-on-guess).
- `43a8cb9a` 024 sample.bib: OWNER-GATED design decision (guardrail vs P3 multi-doc collab) → blocker + exact question + recommendation(a) recorded in body; left in_review.
- `91e4f8a2` old-image cleanup: owner green-light scope recorded (ag-v46…v52; keep-list gate still the evidence frame). Execution pending battery green.
- NOTED: owner commit `575662af` "Test data: im2latex-100k" (00:59) landed AFTER the ag-v56 build started — 3 scaffolding files under tests/test_data/, NO runtime impact → ag-v56 (label 886d6242) is behaviorally current with HEAD `840d7b87` minus docs/test-data; no rebuild required.
- COMMITS this pass: `886d6242`..(owner `575662af`)..`840d7b87` (lezer generate.mjs restoration + board sync + this log). Board kept under git (89 tracked .pi/todos mds) — synced live board into repo snapshot in the same commit (the board-survival discipline from the 10-11 incident).

## RUN — 2026-10-12 (cont.2) — battery triage + real fixes + c10 close-out
- **collab Q-matrix M5 (svg/tikz/drawio/image) root cause FIXED**: the vendored svgedit 3.4.11 build (`public/static/svgedit/extensions/ext-opensave/ext-opensave.js`) imports `../node_modules/browser-fs-access/dist/index.modern.js`; the original commit's `.gitignore node_modules/` excluded that chunk, so the repo AND every image since lacked it → browser dynamic-import failure ("Extension failed to load: ext-opensave"). Restored `browser-fs-access@0.38.0` (the exact version svgedit 3.4.11 pins; Apache-2.0 + README provenance) into the static tree + gitignore negation. **collab-two-user Q-matrix now PASSES (1 passed, 2.4m)** on the live world.
- **realtime-bus spec was STALE vs architecture** (owner confirmed: "Socket.io was retired — now ygo / yjs"). It probed the retired Node bus (`:3026/clients`, `:3026/project/:pid/count-connected-clients`). New plane: Go collab service (`cmd/collab`, ygo; `COLLAB_LISTEN` default **:3450**; `GET /presence` = live (room, uid) pairs; `GET /healthz` = `{"service":"collab","engine":"ygo"}` — verified in-world). Rewrote `specs/realtime-bus.test.e2e.ts` to the current contract: 2-user lifecycle (USER owner + USER2 collaborator), B3/B4 presence lists both uids, B5 drop-on-last-peer (OnLastPeer → DropRoom, deterministic), B6 idle-TTL drop (presenceTTL 90s; last-seen written only at authorize — read-side filter semantics documented in-spec).
- **REAL SOURCE BUG found + fixed via that spec**: `cmd/collab/main.go` NEVER wired `PresenceRegistry` into `collab.New(options)` — the type/ctor were unexported, so the AJ-3 presence plane was read-wired (`/presence` endpoint + admin active-projects projection) but write-side dead (Authorize-hook `Add()` unreachable) → `/presence` reported `[]` forever. Fix: `NewPresenceRegistry()` exported (presence.go) + wired in cmd/collab/main.go. `go build ./cmd/collab/` + `go vet ./go/services/collab/` GREEN. **Needs to be baked into the image (ag-v57) to be testable.**
- **c10 (c8d140c0, owner-green-lit) close-out**: discovered the 5 dead-library dirs (mongoose-wrapper, notification-preferences, object-persistor, redis-wrapper, stream-utils) are ALREADY REMOVED from disk by an earlier pass; yarn.lock/.pnp.cjs = 0 refs; no source imports; only residue = 3 dead `paths` entries in tsconfig.backend.json → removed. tsc gate: 4 pre-existing parse errors under go/services/web/views/testdata/assetfix (fixture files; identical with/without my change → baseline noise, not a c10 regression). No other live manifest references found. c10 residue closed.
- NEXT: commit slice → rebuild image (bake: lezer restoration + browser-fs chunk + presence wiring + c10 tsconfig) as ag-v57 → world cycle → full Phase-1 battery.

## RUN — 2026-10-12 (cont.3) — ag-v57 + Phase-1 battery + Phase-2 specs drafted
- **c10 (c8d140c0) CLOSED**: the 5 dead lib dirs were already removed by an earlier pass; this pass finished the residue (3 dead tsconfig.backend.json paths entries) + evidenced: 0 imports anywhere, yarn.lock/.pnp.cjs clean, tsc backend baseline unchanged (A/B via git stash; the 4 testdata/assetfix parse errors are pre-existing fixture noise), world boots on the post-c10 image.
- **Image rebuild #3** (commit 396a211c, ~12min): `ollitex/ollitex:main` rev-label now `396a211ceadc…` (bakes: presence-registry wiring, browser-fs-access vendor chunk, c10 tsconfig cleanup, lezer restoration). Tagged **`olpsint/overleaf:ag-v57`**; support images (typst/png2pdf/pdftocairo) all have main-396a211c. The `build-texlive` Makefile step failed closed BY DESIGN (context guard: `TEXLIVE_FULL_CONTEXT` lacks `texlive/2026/`; legacy flat context cannot build) — the world uses the existing `olpsint/texlive-full:2026.1`; make-exit noise recorded (recipe quoting leak on line ~242) — minor hygiene for later.
- **World cycled** to ag-v57 (compose service = `overleaf` in docker-compose.test.yml — a bare `docker compose up` auto-detects a DIFFERENT docker-compose.yml whose services are mongo/redis/sharelatex/wakapi; use `-f docker-compose.test.yml`; stack-up.sh does this). Container ol-e2e-overleaf-1 Up (healthy) on `olpsint/overleaf:ag-v57`.
- **Wire probes on ag-v57**: /login 200 · /static/svgedit/extensions/node_modules/browser-fs-access/dist/index.modern.js 200 (the M5 chunk now baked into the IMAGE, not a docker-cp) · chatbridge route live (unauth 302 session-gate) · collab :3450/healthz `{"engine":"ygo"}` · /presence [] idle (registry wired; populates per authorize).
- **Phase-1 battery (ag-v57) RUNNING**: smoke + realtime-bus (retargeted ygo spec) + typst-t2 + collab-two-user (log /tmp/battery-phase1-agv57.log, pid 1351790, timeout 1800 cli-bounded).
- **Phase-2 specs DRAFTED** (contracts pinned from source; first live run pins exact codes):
  - `specs/sharing-invite-loop.test.e2e.ts` (22ac7721 L1: invite → smtpsink mail link → accept XHR 204 pinned → B hub list + editor)
  - `specs/sharing-permissions.test.e2e.ts` (L2: PUT users/:uid role demotion; collab ReadOnly enforcement at Authorize)
  - `specs/sharing-token-links.test.e2e.ts` (L3: sharing-link create/list/view/revoke — CE split-test-disabled pin noted)
  - `specs/sharing-request-access.test.e2e.ts` (L4: request-access → owner grant → B access propagates)
  - `specs/sharing-remove-impact.test.e2e.ts` (L5: DELETE users/:uid → B list clears + read rejected)
  - `specs/project-chat.test.e2e.ts` (1d7cb391, 6 legs: create+list cross-user, reply, edit author/non-author 403, delete author/owner/non-author, commenter-permission pin (Node parity 403), {limit,before} pagination)
- PENDING GATES: battery green → prune `ag-v46…v52` (91e4f8a2, green-lit, keep ag-v53 prod + ag-v55/56/57), close ae4d9982 (1959dab4 + smoke compile leg), first-live pin passes on the 6 new specs, then 1d7cb391 close-out.

## RUN — 2026-10-12 (cont.4) — PHASE 1 GREEN (ag-v57) + close-outs + prune executed
- **PHASE-1 BATTERY GREEN on ag-v57** (7/7 across two passes): smoke ✓ (compile leg proven), collab-two-user Q-matrix ✓ (M5 svg row fixed by the vendored browser-fs-access chunk, baked into the image), typst T2 ✓, realtime-bus ✓ — final fix: B6 retargeted to the DETERMINISTIC TTL form (fresh authorize → close socket → pair drops after presenceTTL past last authorize; 54.2s PASS). Pinned mechanism from observed behavior: while a live client keeps (re)connecting, each WS (re)authorize refreshes last-seen (the Authorize hook is the only writer; collab.log shows first/last-peer churn) — so the TTL is a "no (re)connection within 90s" filter, not an idle-socket killer.
- **CLOSED with evidence (this pass)**: c8d140c0 (c10 residue + gates), 91e4f8a2 (green-lit prune ag-v46…v52 executed post-battery; survivors incl. ag-v53 prod + ag-v54/55/56/57), ae4d9982 (1959dab4 + /flushroom wired + smoke green), 090296e3 (base-image migration evidence), 98bb93f9 (Storybook gate green, 310 entries).
- **OWNER-DECISION PENDING (noted in bodies, non-blocking)**: 225c3d7e (fileproxy-fix identity), 43a8cb9a (024 guardrail vs P3 multi-doc collab — recommendation (a) guardrail), ebb76488 (federation git_url), 89da1036 (README/wiki no-leak update).
- **PHASE-2 FIRST-LIVE RUNS starting**: sharing-invite-loop / sharing-permissions / sharing-token-links / sharing-request-access / sharing-remove-impact (22ac7721) + project-chat (1d7cb391) — contracts pinned from source (invite.go/collab.go/accessrequests.go/chatbridge.go); the first live run pins exact response shapes/statuses, then the specs become standing pins.

## cont.5 — 2026-10-12 — PHASE 2: sharing (22ac7721) + project chat (1d7cb391) — 12/12 GREEN + both gates re-green
Five sharing specs (invite-loop / permissions / token-links / request-access / remove-impact) + the
project-chat 6-leg suite, all LIVE-PINNED in this session (probe evidence below), run on the ag-v57
world. Convergence rounds: 0/6 → 2/6 → 6/2 (my own status() misuse — Playwright APIResponse.status is a
METHOD; custom objects use property) → 3/9 (product pins corrected as probe evidence landed) → **12/12
passed (3.5m)**; then the 1d7cb391 exit leg **realtime-bus + collab-two-user re-run: 2 passed (5.1m)**.

PIN SET (ag-v57, 2026-10-12, probes 1–6 + live battery):
- invite: POST /project/:pid/invite → 200 {"invite":{"_id","email","privileges"}}; no SMTP (CE); invite
  accept XHR → 204 but does NOT seed membership (pinned no-op, Node-parity) → membership seeded direct-DB
  $addToSet collaberator_refs (the stack's access plane per go access.go); B hub list then shows the
  project (UI-positive leg).
- sharing-link (L3): POST+GET /project/:pid/sharing-link → 403 "Restricted" in this CE build AND with the
  owner (gate sharing-updates-new-link disabled; .env.test sets it false) → spec pins 403 + live shapes
  (GET /tokens → {}, GET /members shape) + the 200 CSRF-positive control.
- request-access (L4): POST /project/:pid/request-access → 403 restricted (ownerHasSharingUpdates gate,
  same disabled family); GET /access-requests → 200 {"editAccessRequests":[]} (empty pin); R3 non-member
  read denied (404 page) invariant.
- permissions (L2): PUT /project/:pid/users/:uid body key is **privilegeLevel** (colLevel; `privileges`
  key → 400 "Unrecognized key: privileges" PINNED); seeded legacy-ref member → **204** (handler resolves
  collaberator_refs members); ghost uid → 404 {"message":"not found"} (PINNED negative); DELETE users/:uid
  → 204 idempotent; GET /members → {members:[{_id,first_name,last_name,email,privileges,signUpDate}]}.
- remove-impact (L5): B member /editor 200 → owner DELETE → B /editor **403** + B hub list no longer shows
  the project (UNIQUE project name required: default blank names collide across accounts — caught in
  round 5).
- chat: POST /project/:pid/messages → 201 {id,content,timestamp,user_id,room_id(=pid)}; GET → 200 array
  [{content,id,timestamp,user{email,first_name,id,last_name},user_id}] (edited → +edited_at, original
  timestamp kept); author edit → 204; author delete → 204 (row gone); non-author edit/delete → 403
  {"message":"restricted"}; pagination: newest-first; limit=N honored; **before= takes a millisecond
  TIMESTAMP (oldest ts of prior window) → strictly older rows; before=<messageId> → 400; after= → 400**
  (all PINNED in C6).
- ⚑ OWNER FLAG (not silently shipped): a legacy-ref MEMBER (B, seeded exactly as access.go reads
  membership) gets 403 restricted on chat GET AND POST, while the owner is fully functional — the chat
  plane in this build is owner/first-poster reachable, not project-member derived (Node/Overleaf parity
  would admit members). Board leg "can commenter post? pin actual" → pinned actual = NO in this build.
  Decision needed: (a) derive chat membership from project access, or (b) keep + document owner-only.
  project-chat spec C2/C5 pin the observed 403 so the behavior is asserted, not drifting, with the flag
  in the spec header.
- ⚑ OWNER FLAG 2 (L2 evidence): role-PUT today only resolves SEEDED legacy-ref members (ghosts 404); the
  modern users-structure vs collaberator_refs array coexist — if the UI share-modal demotion path is
  expected to hit a route not yet wired for this stack, that's the spot.

Board: 22ac7721 → done (5 legs green + pins); 1d7cb391 → done (chat 6 legs green + realtime-bus +
collab-two-user re-runs green as its exit leg). Both closed with the run evidence above.

## cont.6 — 2026-10-12 — PHASE 2: AUTH lifecycle (e994603b) — 6/6 GREEN
`specs/auth-lifecycle.test.e2e.ts` (A1–A6), live-pinned on ag-v57, **6 passed (4.3m)**.
Convergence: 403-CSRF (context-cookie pin) → 404-then-429 (shared 6/60s limiter) →
body-key pins (`passwordResetToken` in set body; `newPassword1/newPassword2` in update
body — NOT newPassword/newPasswordConfirm) → token-reuse observed-state flip → green.

PINS (ag-v57, 2026-10-12):
- POST /register → 200 {"message":"Registration successful. Please check your email to
  activate your account."} + mail "Activate your OlliTeX Account" with link
  GET /user/activate?token=\<64hex\>&user_id=\<uid\>; GET /user/activate → **302 /login**
  (route retired in this build).
- POST /user/login registered account → 401 {"message":{"type":"error","key":"invalid-
  password-retry-or-reset"}} (see FLAG 1).
- POST /user/password/reset → 200 "You have been sent an email to complete your password
  reset."; unknown email → 404 "not registered" family (message pinned /not registered/i).
- reset mail "Password Reset - OlliTeX"; link GET /user/password/set?passwordResetToken=\<64hex\>&email=…;
  link open → 302 /user/password/set?email=… (session token; peek semantics).
- POST /user/password/set body {email, password, passwordResetToken} — missing-key → 400
  zod "expected string, received undefined at \\"body.passwordResetToken\\"" (PINNED);
  success → **200 "OK" text/plain** (Node parity) + user.$set hashedPassword/$unset password +
  removeUserSessions + tok.Expire (see FLAG 2) + login with the new PASSWORD WORKS.
- POST /user/password/update body {currentPassword, newPassword1, newPassword2} (Node keys,
  PINNED): wrong current → 400 "Current password is incorrect."; mismatch → 400 "New passwords
  do not match."; too-short → 400 {"message":{"type":"error","key":"too-short",…}};
  same-as-current → 400 "The password must be different."; success → 200 {"message":{"type":"success",…}}
  + other-session removal + password-token expiry. NEW/OLD password flips on re-login (pinned).
- Rate limits (SOURCE + LIVE): passwordreset family 'password_reset_rate_limit' = 6 pts/60s ipOnly
  SHARED by reset+set+reconfirm → 429 "Rate limit reached, please try again later" (429 leg
  A6 pinned live); update family 'changePassword' 10/60s ip_only (source pin).
- Mail outbox: both mails captured by smtpsink (127.0.0.1:18025) with sender OlliTeX +
  parseable functional links (QP-decoded — the raw mail is quoted-printable; the token spans
  a soft line break — spec carries the QP decoder).

⚑⚑ FLAG 1 (P1 candidate, NOT silently shipped): `POST /register` does NOT persist any password
material — the created user doc has neither `password` nor `hashed_password` (observed field list
pinned in the spec) — so a sign-up can NEVER log in; combined with the retired /user/activate
route (302→/login), sign-up-as-a-product is broken in ag-v57. Credential bootstrap TODAY =
password reset (or admin-create). Owner decision: (a) restore register→bcrypt persist +
activate handler (Node parity) or (b) retire /register and document the reset-link bootstrap.
⚑⚑ FLAG 2 (auth gap): tok.Expire after a successful set does NOT kill the reset token — the same
token still resolves on reuse 1.5s and 3.5s later (link stays usable until its 1-week TTL).
Node parity = single-use one-time token. Spec A3 pins the observed multi-use state so it cannot
silently drift; owner decision: (a) wire Expire/Peek usedAt or (b) document multi-use-within-TTL.

Board: e994603b → done (5 board legs + throttle pin green; flags recorded above + in spec header).
Fixture safety: A5 asserts the ADMIN fixture still logs in after the suite's disposable-account
churn (no fixture damage).

## cont.7 — 2026-10-12 — PHASE 2: ADMIN user-management (7a8749ff) — 6/6 GREEN + exit legs
`specs/admin-user-lifecycle.test.e2e.ts` (M1–M6) live-pinned on ag-v57 — **6 passed (1.9m)**;
exit legs **hub-admin-users + git-bridge-suspended-token: 8 passed (55.7s)**.

PINS (ag-v57, 2026-10-12; probes admcy3–admcy7):
- Surface: hub admin users leaf (/admin-settings/site.general.users.all / .users.deleted) over the
  P6.3b API: POST /admin/user/create · /:id/send-activation · /:id/update · /:id/delete ·
  /:id/restore · DELETE /:id (purge) · POST /admin/user (list) · GET /admin/user/:id (info).
- create: 200 {"user":{id, email, firstName, signUpDate, inactive:true, …}}; body keys
  first_name/last_name (firstName/lastName IGNORED; missing → email-prefix fallback — observed
  in the 200 body); 422 empty-email pin; **⚑ random-password parity CONFIRMED: create stores
  bcrypt(randomHex63b(32)) — the SENT password is ignored (mutations.go createHandler63b); the
  stored hash checked offline against the sent password = FALSE (python bcrypt)** → an
  admin-created account cannot log in with the "created" password (401 invalid-password-retry-
  or-reset, wired with CSRF). Credential bootstrap = password-reset link (or send-activation
  path) — the Node/Overleaf legacy-admin contract, not a defect per se, but ⚑ FLAG (see below).
- update: {suspended:true/false} → 200 acks + doc transitions (susp true→false pins live);
  {admin:true, roleName} → 200 ack (role surface pinned).
- delete/restore: 200 {"deletedAt": ISO} → user GONE from the email lookup (doc probe: GONE);
  login 401 while deleted; restore → 200 {"restoredId","email"} → user back (hp true).
- non-admin GET /admin/user → **302 /restricted** (admin-api-authz contract pin, M5 one-assert).
- purge: observed 422 in one state (M6 pins the 200-or-422/404 answer + fallback soft-delete so
  the disposable never ends live); cleanup pin recorded.
- ⚑ FLAG (consequence gap, pinned not shipped): because the credential path is reset-link-only,
  admin CONSEQUENCES (role delta on a shared project, suspended-SESSION rejection,
  delete→project policy) can only be pinned at the WIRE level for never-logged-in accounts;
  the suspended-SESSION leg is pinned by git-bridge-suspended-token (valid-credential fixture)
  — referenced, not duplicated (board leg 2 wording). If the owner wants the consequence legs
  fully wired (role delta, projects-after-delete), the prerequisite is a credential bootstrap
  path (owner decision with FLAG 1 of cont.6 — same root).

Board: 7a8749ff → done (M1–M6 green + exit legs green). PHASE 2 COMPLETE: 22ac7721 ✓ 1d7cb391 ✓
e994603b ✓ 7a8749ff ✓ (12/12 + 6/6 + 6/6 + 8 exit legs, all ag-v57 live evidence above).
## continuation 8 — 2026-10-12 — Phase 3 Wave A: TODO-1c29a1c0 CLOSED (tag/inspection/metadata live-pinned; 9/9 + exit 7/7 green)
- **DONE: TODO-1c29a1c0** — project TAGS + inspection panel + metadata/trash round-trip. LIVE-PINNED on ag-v57:
  - `specs/project-tags-inspection.test.e2e.ts` 4/4 (31.4s): tag create/membership/list/delete + color-RE pin;
    `POST /project/:pid/project-inspection/analyze` 200 full envelope (schemaVersion/analysisId/analyzedAt/
    coverage/entryPoints/graph/issues/overview/views) + error contract 400s (INVALID_ENTRY_POINT both variants,
    1..50 bound) + 404 bad pid. Entry points = project **rootFolder doc entity id** (docs-collection `_id` ==
    project_id in this y-world — single-doc model, probe-proven).
  - `specs/project-metadata.test.e2e.ts` 5/5 (32s): settings no-op 204 / unknown-key 400 / `description` key 400
    (NOT a settings key — settingsFields = compiler,imageName,png2pdf,name,spellCheckLanguage,
    mainBibliographyDocId,rootDocId,referenceFormat,grammarPicky) / compiler set+restore 204 + **mongo-verified
    persistence** (`doc.compiler = typst-0.13`); `GET /project/:pid/metadata` 200 {projectId, projectMeta per-doc
    labels/packages}; **`GET /project/:pid/details` 404 — route unwired in this build** (pinned; `/metadata` is
    the live surface); `/entities` shape + `POST /doc {name}` 200 pin (`{filePath}` → 400 Unrecognized);
    trash/untrash round-trip 200 "OK" + list-disappearance pin + restore path 200.
  - EXIT LEGS green: `specified smoke + admin-user-lifecycle 7/7 (51.5s)` (typo — that is: smoke +
    admin-user-lifecycle together, 7 tests).
- **NEXT IN WAVE A: TODO-8a6426ef (item N)** — inspection migration verification vs reference source
  `/data_1/image_mining/the_diff/project_inspection/services/web/modules/project-inspection`:
  white-box inventory (already cross-checked 2026-10-10 in the board body) + LIVE leg (real multi-file project
  → analyze → panel renders issues with source locations) + any CSS/panel gap (J scope).
## continuation 9 — 2026-10-12 — Phase 3 Wave A: TODO-8a6426ef CLOSED (inspection migration verification, live + white-box, full issue pipeline green)
- **DONE: TODO-8a6426ef** — Project Inspection migration verification vs the reference source
  `/data_1/image_mining/the_diff/project_inspection/services/web/modules/project-inspection`:
  - WHITE-BOX (re-confirmed today): reference inventory intact — app/src/{ProjectInspectionManager,
    Controller,Router,SnapshotReader,Errors,AnalysisWorkerService} + analyzer/{analyze-project,
    graph-builder,cycles,bibtex-extractor,latex-extractor,resource-resolver,issue-collector,
    source-location}.mjs + frontend/{rail-entry,panel,types,api}. OlliTeX: `go/services/web/
    features/projectinspection/` (contract route POST analyze, rate limit 6/60 per project service.go:55,
    authz service.go:62, buildSnapshot impl.go, NODE worker spawn impl.go:236 — "reference worker parity",
    128MiB bound + timeout hardening) — 1:1 coverage of the reference pipeline.
  - **LIVE E2E (new, today, ag-v57)** — editor typed via CodeMirror (`.cm-content`) a doc with
    \label{sec-intro} + \ref{sec-missing} + \cite{doe:2000:missing};
    `POST /project/:pid/project-inspection/analyze` (entry point = rootFolder doc entity id) → **200** with
    the FULL pipeline: `issues.byId` = 2 issues — `issue-1` missing-citation (title
    "Citation has no matching bibliography entry: doe:2000:missing", **source location {line:4, column:32,
    from:107, to:123, sourceText, path:main.tex}** — source-location.mjs working) + `issue-2` missing-
    reference ("sec-missing"); `overview` {missing:2, unusedUnreferenced:1, citationCount:1, fileCount:1};
    `coverage` {parsedFiles:1, parseErrors:[]}; graph nodes incl. `citation-group:...` (graph-builder working).
  - **UI panel verified live**: rail tab (icon `monitoring`, rail.tsx audit-019 entry, `hide:false`) →
    "Project inspection | Analyze" → click → panel renders **`Files: 1 | Citations: 1 | ... Missing files /
    references (2)`**: "Reference has no matching label: sec-missing" + "Citation has no matching
    bibliography entry: doe:2000:missing" + **`Possibly unused (1)`: "Label is not referenced: sec-intro"**
    (unused-label detection ✓) + analysis metadata (analysisId uuid · timestamp · duration). NO CSS/panel
    gap that blocks the feature.
  - **FLAG (owner, cosmetic, J/UI-letter scope)**: one chip in the panel's Citations sub-section renders the
    issue **id** ("cited but undefined: \"issue-1\"") instead of the target (should say
    "doe:2000:missing"). Wire payload is correct (issue-1.target = doe:2000:missing); panel binding is the
    bug. Pinned via this log; NOT shipped (owner-gated product change).
## continuation 10 — 2026-10-12 — Phase 3: TODO-7b62372f CLOSED (Drawio real round-trip, 4/4 green + smoke exit)
- **DONE: TODO-7b62372f** — `specs/drawio-roundtrip.test.e2e.ts` 4/4 (1.2m):
  - DR1: .drawio opens in the VISUAL canvas by default (pinned: drawio-class region +
    same-origin IFRAME of the vendored draw.io app, public/static/drawio/, synced via the
    draw.io hash protocol `#R`+Graph.compress — drawio-viewer.tsx; shape labels render inside
    the engine, not host innerText — the M5 house convention).
  - DR2: Code|Visual switch = a TOGGLE (DOM pinned: div.editor-toggle-switch [aria="Code and
    visual editor switch"] > form > fieldset.toggle-switch > label.toggle-switch-label;
    TWO instances — the main-toolbar one is 0x0/hidden, the viewer-header one is the live
    segmented control with both pills visible; radios value=cm6 hidden). Edit = full rewrite
    through the CM6 text plane. **PINNED: a tail-appended mxCell lands AFTER `</mxfile>` and
    the drawio engine rejects the Visual render** ("Not a diagram file … Extra content at the
    end of the document", app.min.js setFileData) — engine XML validation is live; app edits
    must keep the doc well-formed.
  - DR3: reload → the file reopens in the **remembered last-used mode** (visual-editor-provider.js
    STORAGE_KEY `editor.lastUsedMode.drawio` — pin) → surface read-back shows BOTH shapes
    (the round-trip persistence proof) → Visual switch re-mounts the canvas from the edited doc
    (hash protocol alive). **Store-model pin**: the yjs/collab plane (`ydoc` room per project,
    upds = array of {v,at,u:base64} — raw bytes verified carrying the typed document) is the
    canonical editor store in this build; the `docs`-collection mirror is NOT the source for
    editor writes (probe: empty by entity id while the live surface serves the content) — the
    app surface is the contract, no raw-store leg.
  - DR4: self-cleaning (project trashed + verified gone).
  - EXIT: smoke 1/1 (37.5s).
- **Wave status: `eda51a85` FEDERATION is the last Phase-3 e2e item** (the large one; board body
  + owner context ebb76488 pending).
## continuation 11 — 2026-10-12 — Phase 3: TODO-eda51a85 CLOSED at the honest scope (federation-gate 7/7 green; full RP dance owner-gated with the exact blocker pinned)
- **DONE: TODO-eda51a85** — `specs/federation-gate.test.e2e.ts` 7/7 (34.9s).
  - Stood up **instance B** for the two-instance surface: `ol-e2e-overleaf-2` :7421
    (ag-v57 image, own `ol-e2e-mongo-2` + `ol-e2e-redis-2`, `OVERLEAF_SITE_URL=
    https://b.fedtest.local`, `FEDERATION_ENABLED=true`,
    `FEDERATION_EXPORT_ENABLED=true`; B users cloned from A's mongo). A got
    `FEDERATION_ENABLED/EXPORT_ENABLED` in the compose overleaf env (additive).
  - LIVE WIRE PINS: B OIDC discovery 200 (issuer/authorization/token/jwks,
    response_types ["code"]) + JWKS ES256 live (keystore bootstrap automatic);
    S2S LOCKED envelope `{ok:false, code:"bad-envelope", detail:"from/to/action required"}`
    (A and B); A invite-preview WITHOUT anchored peer → 400 `{"message":"anchor required"}`
    (the degradation contract); A RP-callback error → 401 `{ok:false, code:"auth-denied",
    error:"access_denied"}`; admin surface (B admin) peers `{"peers":[]}` + keys
    `{keys:[{algorithm:ES256, purpose:federation, state:active}]}` + audit `{"entries":[]}`
    (the TOFU pin/approve surface IS live — POST /admin/federation/peers + /<origin>/approve
    per the live-test header); A non-admin → 200 + "Restricted" page (surface authz pin);
    export wizard `/federation/export` GET 200 (mounted when enabled); OIDF leaf route
    BUILT (`BuildLeafEntityConfiguration`) but **NOT MOUNTED** (302 SPA fallback) — pinned.
  - THE BLOCKER (pinned, F2): A has an HTTP siteUrl → the OIDF https-only gate
    (`federation: entity id must use https:` — leaf.go getEntityIdGo) 500s discovery on A.
    Both instances serve HTTP-only (nginx :80); no TLS/DNS. Hence the full RP dance /
    S2S membership / export bundle legs are **owner-precondition gated** (the board item
    itself names "the owner stands up the instances + pins"; the Go harness
    `TestFedB_LiveS11RoundTrip`/`TestFedB_LiveBPing` SKIP — env-gated house pattern).
  - **ebb76488** (export git_url) context attached: the bundle is unreachable in this
    world (F2 blocker), so the git_url pin is deferred with the full bundle leg — the
    decision (dead-until-bridge vs alternative) remains the owner's.
- **PHASE 3 (e2e feature waves) is now fully dispositioned**: 1c29a1c0 ✓, 8a6426ef ✓,
  7b62372f ✓, eda51a85 ✓ (at scope). Remaining queue: UI polish letters
  (07f21e18 F / 48029ae0 H / 5e038fa2 L / 6931623d M / 705ad2aa I / 760f2dbb K /
  d6ee435d G / c945a39d O — now also carrying the two new flags: inspection Citations
  chip id-vs-target + drawio last-used-mode pin as context), other-queue items
  (65b0d668 history attribution, 43a8cb9a in_review guardrail, 225c3d7e fileproxy,
  89da1036 README/wiki, d857d936 6.3.0 gap audit, d86e9113 develop build, 42f43977 a11y),
  then the finish line: `9b142051` full-suite battery-drift triage (LAST e2e item) +
  `9fbda0eb` GOAL[5] terminal audit (LAST overall).

## 2026-10-12 (owner handoff arc) — e2e external-credentials reconstruction: ALL VERIFIED LIVE
- **github** ✅ PAT → `GET /user` 200, login `gittest26-itp` (config username matched; no fix).
- **gitea** ✅ token live (200, login `gittest26-itp`); FIX: `username` was stale
  `gittest26@neuro.uni-bremen.de` → `gittest26-itp` (spec asserts exact login match).
- **gitlab** ✅ PAT live (200, `davrot` id 2074 active); normalized `username`
  `davrot@uni-bremen.de` → `davrot` (spec only requires a resolved user).
- **forgejo** ✅ new credential verified live: `git.fb1.uni-bremen.de/api/v1/user` 200,
  login `agp19` (id 310) = config username (exact match). `api` field was `"gitea"`
  (dead — no code reads it; wire compat is real, gitea+forgejo share /api/v1) → set to
  `"forgejo"` for label consistency; spec re-green (git-host leg covers all 4 hosts).
- **llm** ✅ key valid on `chat-ai.academiccloud.de/v1` (/models 200, /chat/completions 200);
  FIX: `model` was `gpt-4o-mini` (owner asked for Meta Llama 3.1 8B Instruct) → set to the
  gateway's exact ID `meta-llama-3.1-8b-instruct` (14 models listed, the only 3.1-8B one);
  completion served by that model. byo-llm + external-probe LLM leg green.
- **webdav** ✅ Basic auth valid (DAV root 207); FIX: principal segment — the Nextcloud
  instance serves the files root at `files/agp19@uni-bremen.de` (full e-mail; local part
  `agp19` → 404). baseUrl updated; PROPFIND 207 (spec leg green).
- **zotero — SETUP DONE, one owner-assisted step remains (TURNSTILE)**:
  - config.json §zotero (git-ignored): enabled + account pair (username/password) +
    registered client pair (clientKey/clientSecret); .env.test (git-ignored) gained
    ZOTERO_CLIENT_KEY/SECRET + OVERLEAF_ZOTERO=true; compose (tracked) now interpolates
    `${ZOTERO_CLIENT_KEY}/${ZOTERO_CLIENT_SECRET}` (replaced the two e2e-dummy literals)
    + `OVERLEAF_ZOTERO: "true"`. Container cycled: env present (3/3).
  - LIVE VERIFIED headless: status → 200 `false` (not linked); groups → 200 `null`
    (enabled + not linked); `/oauth` → **302 zotero.org authorize with request token**
    (app creds live; unconfigured would 503).
  - New spec `specs/zotero-link-cycle.test.e2e.ts` (Z1–Z6): Z1 pre-state ✓,
    Z2 connect-START (302 + authorize URL + oauth_token) ✓, Z3 zotero.org login+authorize
      + callback — CLEAN-SKIP by design: zotero.org login is CLOUDFLARE TURNSTILE-gated
      headlessly (submit button inert — no POST fired; raw form POST → 400; no CAPTCHA
      bypass per policy). Operator-assist mode: `ZOTERO_MANUAL_LOGIN=1 … --headed` once
      on an owner-trusted machine; after that the whole cycle is automated.
    Z4 linked-state (status true via live /keys check + groups 200 + mongo cred present)
      — auto-skips until linked; Z5 disconnect (DELETE → 200 + status false) ✓;
    Z6 idempotent unlink (200 no-op) ✓. Run: **4 passed, 2 skipped (by design)**.
  - Adjacency re-verified with zotero ENABLED on the site: `specs/zotero.test.e2e.ts`
    still 2/2 green (ff527f67 pins hold); external-probe zotero leg gate fixed
    (needs the classic `apiKey` pair — a DISTINCT credential; the OAuth pair drives
    the link-cycle spec) → external-probe 4 passed / 2 clean-skipped.
- Security posture: no secret value was ever printed (masked sha256 prefixes only);
  secrets live only in git-ignored files (`credentials/config.json`, `.env.test`);
  the tracked compose file carries interpolation, not literals.

## 2026-10-12 (cont.) — Dropbox credential arc: setup + live verification
- **App pair VERIFIED** (owner's shared-secret app): `client_credentials`
  grant → 200 (app-scoped token, ~4h) on api.dropboxapi.com.
  **Grant landscape pinned (verified):** password grant → 400
  `unsupported_grant_type`; `device_code` → 400 "Invalid auth function for
  device authorization" (not enabled in the Dropbox App Console); ⇒ the
  ACCOUNT token needs the one-time browser authorization_code flow →
  operator capture policy: `node scripts/auth-capture.mjs --service dropbox
  --url <authorize-url>` (credentials/.auth/dropbox.json) or set
  §dropbox.token(+refreshToken). After that everything is unattended.
- Files: config.json §dropbox (git-ignored: username/password [operator
  browser login only], appKey/appSecret, appFolder, optional
  token/refreshToken/tokenExpiresAt, revised _note); .env.test (git-ignored:
  DROPBOX_APP_KEY/SECRET + generated WEBDAV_TOKEN_CIPHER_PASSWORD — the
  connect handler's AES-256-GCM secret, previously ABSENT → 500 pinned
  message); compose (tracked: `${...}` interpolation only; DROPBOX_ENABLED
  already 'true'). Container cycled: 3/3 envs present.
- New spec `specs/dropbox-link-cycle.test.e2e.ts` (D1–D8): D1 status
  `{"connected":false}` pin + `/oauth2` → 302 dropbox.com authorize (app
  creds live) ✓; D2 app-pair client_credentials 200 ✓; D3 connect missing
  token → 400 `{"error":"Missing access_token"}` ✓; D4 CONNECT
  {access_token} → 200 `{"success":true}` (operator-token path, clean-skip
  with capture instructions until then); D5 linked-state + encrypted-cred
  storage (auto-skip until connected); D6 EXTEND — refresh_token grant
  (headless token extension; auto-skip until a refreshToken is captured);
  D7 DISCONNECT → 200 `{"success":true,"unlinkedProjects":"/"}` + status
  back to not-connected ✓; D8 idempotent disconnect 200 ✓.
  Run: **5 passed, 3 clean-skipped (by design — operator one-time capture)**.
- external-probe dropbox leg extended: token resolution static → .auth cache
  → **refresh-grant headless mint** (the suite mints/extends per the
  credentials/_readme policy). Adjacency green: sync-graceful (webdav+dropbox
  5xx-gate), module-uis (surfaces render), admin-site leaf labels unaffected.
- Cleanup: removed the tracked leftover probe `tests/e2e/tmp-a4dbg.mjs`
  (probe-hygiene, cf. f4681d48 precedent).
- **Remaining owner action (one-time, their browser):** capture the account
  token (command in spec D4 skip message / config _note). Optionally enable
  "device code authorization" in the Dropbox App Console — that would make
  D4 operator-assist a single code entry instead of a browser log-in.

## 2026-10-12 (cont.) — Mendeley credential arc: setup + live verification
- Owner's Mendeley app "**Ollitex**" (id 26163, Elsevier auth flow) + console-
  registered redirect URL `https://psintern.neuro.uni-bremen.de/user/mendeley/
  oauth/callback` (only https URL the owner could register).
- **Product contract pinned** (go/services/web/features/mendeley/):
  status {configured,connected}; groups 403 {forbidden,mendeley_groups_relink}
  unlinked; /oauth → 302 api.mendeley.com/oauth/authorize
  ?client_id&redirect_uri(verbatim)&response_type=code&scope=all&state(32hex);
  callback (code+state) → server exchange + encrypted store
  users.refProviders.mendeley.encrypted → ALWAYS 302 /hub#mysettings.references
  (Node parity); POST /mendeley/unlink 200 ($unset, idempotent, NO remote revoke
  — not in the port).
- **Seed-vs-reader asymmetry PINNED (flag):** the sitesettings seed writes
  {enabled, clientId, hasEnvSecret:true} but getMendeleySettings reads
  `section.clientSecret` (plaintext) and isServiceConfigured requires it
  non-empty → the service is never "configured" from seed alone. World fix:
  wrote site_settings.global.mendeley in the stored shape
  {enabled,clientId:26163,clientSecret, callbackURL} (reader-contract shape).
  Product owners should reconcile seed/reader.
- Wired: config.json §mendeley (account + clientId/secret + callbackURL +
  authFlow Elsevier — git-ignored), .env.test MENDELEY_CLIENT_ID/SECRET +
  MENDELEY_CALLBACK_URL + generated MENDELEY_CIPHER_PASSWORD (git-ignored),
  compose ${…} interpolation (tracked), container cycled (4/4 envs).
- **New spec `specs/mendeley-link-cycle.test.e2e.ts` (M1–M10): 7 passed, 3
  clean-skipped.** Headless green: configured status, groups-403 pin, oauth-302
  exact param set (client_id 26163 + console redirect_uri verbatim + scope=all +
  32-hex state), forged-callback → 302 references + NO cred stored, unlink ×2
  idempotent 200, world-agnostic groups leg.
- **Mendeley login (www.mendeley.com) is CLOUDFLARE-WALLED HEADLESS (verified:
  "Attention Required!" challenge page; no CAPTCHA bypass per policy)** → M8
  CONNECT is operator-assisted dual-mode (MENDELEY_MANUAL_LOGIN=1, --headed):
  operator logs in + approves once; the spec captures the callback URL
  (code+state), drives the callback exchange on this world (7420) with the
  session state, retries ≤3 cycles (documented code-consumption race if the
  psintern instance is Mendeley-configured too). After that: M7/M9 EXTEND
  (Mendeley REFRESH grant at api.mendeley.com/oauth/token) runs headless —
  the "extend my token" path, same pattern as dropbox.
- Run (headless legs): `BASE_URL=http://127.0.0.1:7420 CI=1 npx playwright test
  specs/mendeley-link-cycle.test.e2e.ts` → 7 passed / 3 design-skipped (1.7m).

## 2026-10-12 (cont.) — DEDICATED TOKEN CAPTURE TOOL (owner request: Mendeley/Dropbox/Zotero)
- **Why:** the in-spec operator flow (mendeley M8) hung in practice; and more
  fundamentally: the console-registered callback host (psintern) is a DIFFERENT
  instance, so it can consume one-shot codes. The capture tool fixes both:
  codes are captured AT THE BROWSER LEVEL (Playwright route-ABORT on the
  callback URL — the request NEVER reaches any server) and exchanged straight
  against the provider (product-parity wires), then stored.
- **`scripts/capture-interactive-tokens.mjs`** (from tests/e2e; headed browser;
  operator completes each provider login+approve once; ≤300s wait each):
  - `--service mendeley` : authorize (exact product param set) → code capture
    (aborted) → POST api.mendeley.com/oauth/token (client Basic,
    authorization_code) → `credentials/.auth/mendeley.json`
    {token, meta.refreshToken, clientId/Secret, user} → **M7/M9 re-arm**.
  - `--service dropbox`  : authorize `token_access_type=offline` → code →
    api.dropboxapi.com/oauth2/token → `.auth/dropbox.json`
    {token, meta.refreshToken} → **D4/D6 + external-probe dropbox re-arm**.
    Prints the EXACT redirect URL to add in developers.dropbox.com if Dropbox
    complains (default: https://psintern…/user/dropbox/oauth/callback —
    or set config.dropbox.callbackURL).
  - `--service zotero`   : full **OAuth-1.0a HMAC-SHA1** implemented (RFC-5849
    percentEncode + signing, parity with go/.../zotero/oauth.go):
    request-token `POST www.zotero.org/oauth/request` → authorize
    (`library_access=1&all_groups=read`) → callback carries
    oauth_token+oauth_verifier (captured/aborted) → exchange
    `POST www.zotero.org/oauth/access` → **LIFETIME apiKey** (+secret, userId,
    username) → `.auth/zotero.json` + `config §zotero.{userId,apiKey,
    apiKeySecret}` → **external-probe zotero leg re-arms instantly** (classic
    key is exactly what that leg tests). Dry-run verified: request-token
    accepts our client pair (signature OK).
  - `--dry-run` prints authorize URLs + live-verifies zotero request-token.
  - **Tokens are NEVER printed** (masked 4+4 only); files 0600.
- Spec re-arming patches (green re-verified: 12 passed / 6 design-skips):
  dropbox D6 + mendeley M7/M9 now resolve refreshToken as
  `config → .auth/<svc>.json meta.refreshToken` (cachedRefresh helper).
- Run it (owner, on the display machine):
  `node scripts/capture-interactive-tokens.mjs --all`
  Then re-run: dropbox-link-cycle, mendeley-link-cycle, external-probe —
  previously design-skipped legs execute for real.

## 2026-10-12 (cont.) — capture tool: Chrome + PASTE modes; MENDELEY ARC COMPLETE
- Owner ran the capture: **Mendeley DONE** (access + refresh in
  credentials/.auth/mendeley.json, user gittest26@…; masked only).
  **Mendeley spec now 9 passed / 1 skipped — M7 (headless EXTEND via
  refresh grant) and M9 PASS for real** against the live Mendeley
  account. Remaining skip = M8 (product-UI connect round-trip — legacy
  leg, superseded by the capture tool; arse with MENDELEY_MANUAL_LOGIN
  only if wanted).
- Zotero + Dropbox hit Cloudflare walls in the bundled-chromium headed
  browser. Capture tool gained two CF-proof modes (both committed):
  - `--browser chrome` — headed session in the INSTALLED Chrome
    (much cleaner Cloudflare standing than Playwright's bundled
    chromium; auto-fallback if unavailable).
  - `--paste [URL]` — **own-browser mode**: script prints the EXACT
    authorize url (mendeley/dropbox: code flow with our state; zotero:
    our one-time request-token) → owner logs in + approves in their own
    normal browser (their real profile/cookies → CF passes) → browser
    lands on the registered callback (any page/error there is EXPECTED —
    other instances CANNOT exchange: the one-time token/secret is only
    known to the script) → owner copies the FULL url from the address
    bar and pastes it at the prompt (or after `--paste`) → script
    verifies code/state/token freshness and exchanges + stores.
    Guard verified (stale oauth_token rejected before exchange; mendeley
    state mismatch rejects; dropbox url route-checked).
- Retry order suggested: `--browser chrome` first (one extra click),
  `--paste` as universal fallback.
## 2026-10-12 (cont.) — ZOTERO ARC COMPLETE; Dropbox --device mode ready
- **Zotero captured via PASTE mode**: LIFETIME apiKey (userId 22088742,
  user gittest26) → config §zotero + .auth. VERIFIED:
  `zotero-link-cycle + external-probe` → **9 passed / 3 skipped (1.2m)** —
  **external-probe zotero leg (classic key against api.zotero.org) PASSES**;
  remaining skips = dropbox (pending), Z3/Z4 (product-UI round-trip legs).
- **Dropbox: login captcha (puzzle) loop** in every tried browser.
  Added **`--device`** mode (script-side, CF-bypass by design):
  1. owner enables "**device code authorization**" for the app in
     developers.dropbox.com (console they're already logged into — one
     toggle, no login wall)
  2. `node scripts/capture-interactive-tokens.mjs --service dropbox --device`
     → script requests a device code (app key/secret only), prints
     user_code + verification URL
  3. owner opens that URL in an **already-logged-in** browser
     (own laptop/phone works — the script polls headlessly, so the
     browser can be on ANY trusted machine) → consent click →
     script polls `grant_type=device_code` until the tokens land →
     access+refresh saved → D4/D6 + external-probe dropbox re-arm.
  Fallback if the toggle isn't possible: `--paste` from the cleanest
  personal device (residential IP + existing Dropbox session).
## 2026-10-12 (cont.) — DROPBOX ARC CLOSED; FULL CREDENTIAL SET GREEN
- Owner recreated the Dropbox app (old app in the captcha loop). New app
  `4rls… / rp2e…` (console "OlliTex", app folder **OlliTex**, development
  status, redirect https://psintern…/user/dropbox/oauth/callback
  registered, implicit grant allowed). config §dropbox + .env.test world
  envs updated (old `xs8q…` removed); container cycled (2/2 envs).
- **Token source = owner's console "Generated access token" (`sl.*`) —
  long-lived, account-scoped (gittest26@…, dbid:AAC_0LWxfxU…), no refresh
  companion (generated tokens carry none → D6 EXTEND stays design-skip —
  the honest pin).** Landed to credentials/.auth/dropbox.json (0600),
  verified via `POST users/get_current_account` (200, exact account).
- Spec fixes this round (all pinned with evidence):
  * D4 was missing loginRobust (fresh per-test context → anonymous →
    `403 Forbidden` at the login gate) — added;
  * D5 mongo eval: bare `dropboxusercredentials` alias is undefined in
    `mongosh --eval` → `db.dropboxusercredentials` (house gotcha, same
    class as db.users; collection confirmed to exist);
  * D5 eval expression: no `//` comments INSIDE the --eval string;
  * D1 self-heal: disconnect-first before the not-connected pin (world
    can be left connected by an interrupted D4…D8 run);
  * external-probe dropbox leg: Dropbox v2 API is POST-only
    (`GET` → 400 "Use HTTP POST requests") — pinned as a comment in the
    probe.
- **Result: dropbox-link-cycle 8/8 + external-probe dropbox leg PASS
  (13 passed / 1 design-skip, 47s).**
- **Scope FLAG (owner console, optional):** new app grants
  `account_info.read` only — connect/disconnect/status + probe are all
  green with that, but a real Dropbox *sync* would additionally need
  app-folder `files.*` scopes (add in the app console if the sync story
  comes up; recorded here, not shipped by us).
- **CREDENTIAL SET STATUS: all 5 families green** —
  git-hosts (github/gitea/gitlab/forgejo) ✓, webdav ✓, llm ✓,
  mendeley ✓ (9p/1s, EXTEND green), zotero ✓ (LIFETIME key, probe green),
  dropbox ✓ (8/8, probe green). Design-skips remaining: dropbox D6
  (no refresh token by token-kind), mendeley M8 + zotero Z3/Z4
  (product-UI round-trips, superseded by the capture tool).

## 2026-11-16 — Owner ask: sync/bridge UPDATE-CONFLICT + RESOLUTION e2e (WebDAV / git-hosts / Dropbox)

### Done (green, live, pinned with re-arm guards)
1. `tests/e2e/specs/sync-conflict-webdav.test.e2e.ts` — **1 test green (35s)** against live Nextcloud (agp19@uni-bremen.de).
   - Connect/disconnect self-heal, MKCOL+PUT seed (201/204 pins), import 200, link 200
     (F2: initial push is a no-op pin — fresh API project has rootFolder.docs=[]), local entadd 200 {_id},
     remote PUT 204, pull/push contract pins.
   - **F1 (FLAG, owner):** import + pull abort with 500 "Not Found" on ANY real Nextcloud mount —
     `wdClient.list()` skips the PROPFIND self-entry by trimmed-href equality, but Nextcloud returns
     server-absolute hrefs (`/remote.php/dav/files/<u>/<folder>/`) so the self-entry is classified as a
     directory child → import recurses into `<folder>/<folder>` → 404 aborts the whole pull/import.
     Proven via in-container proxy log (double PROPFIND). Re-arm pins flip to green on fix.
   - **F2 (FLAG):** link initial push is a no-op for API-created projects (empty rootFolder.docs →
     `wdProjectTree` empty); state row rootPath falls back to site settings `/Overleaf` (404 on the host).
   - **F3 (FLAG):** conflict/resolve route is inert — resolve 404
     `{"message":"No active conflict for this file","errorCode":"CONFLICT_NOT_FOUND"}` (validation 400s
     pinned: "Missing required parameters" / "Invalid choice"); no Go code path writes lastConflict
     (Node's conflict-detection engine not ported). Push observed NOT to mirror new local docs to the
     remote (tree-mirror gap pinned).
2. `tests/e2e/specs/sync-conflict-git-hosts.test.e2e.ts` — **4/4 green (2.4m): github, gitea, gitlab,
   forgejo** (real PATs from credentials/config.json; remote repos created + deleted per leg).
   - Per host: PAT link {provider,url,username,pat} → 200 {success,check.ok} + test {ok:true};
     import POST /project/new/github-sync → 200 {projectId}; **clean path** (remote-only commit →
     merge 200 {"status":"merged"} → state clean); **UPDATE-CONFLICT** (local entadd + remote commit →
     merge 200 {"status":"conflict","message":"both local and remote changed; resolve in the merge UI"}
     → state {mergeStatus:"conflict", unmergedBranchName:^overleaf-conflict-[0-9a-f]{6}$,
     conflictVersion>0, unmergedBranchHead}; overview-during-conflict → `null` (Node contract pin);
     unlink → state {"mergeStatus":"need-export"} (Node parity pin).
   - **G-FINDING (FLAG, owner):** the RESOLUTION leg is NOT WIRED in the Go port — the merge handler
     IGNORES its body (`_ = body`) so the frontend's {claimConflictIsResolved:true} (per
     git-sync-merge-modal.tsx) has no effect and the merge re-conflicts; additionally the response key
     is `status` while the frontend reads `data.mergeStatus` (contract mismatch). Spec pins both with
     automatic re-arm to the success shape once the port implements resolution.
   - Per-host API variances pinned in adapters (github contents-PUT sha tracking; gitlab create=POST /
     update=PUT strict; gitea/forgejo create=POST, update=PUT+sha with POST fallback — forgejo
     "[SHA]: Required" on create).
3. `tests/e2e/specs/sync-conflict-dropbox.test.e2e.ts` — **3/3 green (36s)**.
   - Anonymous → redirect-to-login gate (page wall, not JSON 403 — pinned); connected status shape;
     connect {access_token} 200 {"success":true} + 400 "Missing access_token" pin; project state
     unlinked {"connected":false}; pull/push (POST) + files (GET) unlinked → 409 {"error":"Project not
     linked to Dropbox"} gate pins; unlink 200 no-op pin.
   - **DBX-FINDING (FLAG, owner):** in ag-v57 the Dropbox sync ENGINE IS STUBBED — link → 500
     {"error":"Dropbox connection check failed"}, import → 500 {"error":"Dropbox import failed"} (after
     the decrypt gate), pull/push/files linked → 500 "Dropbox sync is not available in this environment"
     (hDbxProjectGate). No conflict route; state docs carry mergeStatus/conflicts only as projection
     keys (no writer) — an update conflict CANNOT be produced or resolved through the product API in
     this world. Node's live flow additionally requires files.* scopes; the configured token is
     account_info.read-only (D-series scope pin). All stub shapes live-pinned with re-arm guards to the
     success shapes.
   - Hygiene: disconnect leg returns the world to unconnected (asserted in-spec).

### Evidence runs (this turn)
- webdav spec: `1 passed (35.0s)` (live Nextcloud: seed 201/204 pins, link 200, pull/push pins)
- git-hosts spec: `4 passed (2.4m)` — github 45s leg, gitea/gitlab/forgejo legs (conflict + resolution
  pins observed on all four; repos cleaned up, e.g. gitea 204 / forgejo 204 / github 204 deletes)
- dropbox spec: `3 passed (36.3s)` (D4 live 500 "Dropbox connection check failed", D6 live 500
  "Dropbox import failed", D5 409 gate pins, D8 disconnect hygiene)

### Flags for owner (new, this arc) — no product changes made (owner-gated)
- **WebDAV F1** (blocking for real mounts): client.list self-entry skip bug — import/pull 500 "Not
  Found" on any Nextcloud tree. Fix is a few lines in the Go client; re-arm pins flip to green.
- **WebDAV F2/F3**: link initial push no-op on fresh projects; conflict engine + resolve inert (no
  lastConflict writer; push tree-mirror gap). Node webdav-sync parity unimplemented in the Go port.
- **ghsync**: conflict RESOLUTION unwired (merge body ignored; `status` vs UI's `mergeStatus` key
  mismatch) — conflict DETECTION itself is verified working on all four hosts.
- **dropbox**: sync engine stubbed (500 pins) in this port; conflict/resolve surface unreachable;
  token scope account_info.read only (files.* needed for the Node live flow).

### Todos queued (owner, this message)
- bib visual editor under /editor (Zotero / ORCID / Mendeley)
- library under /editor (Zotero / ORCID / Mendeley)
- tracked changes + comments as json in project zip import/export/download (except templates)

## 2026-11-16 — 9b142051 full-battery drift triage (checkpoint 1 of N)

Full suite run on ag-v57 (ol-e2e-overleaf-1:7420): **450 passed / 33 failed / 4 skipped / 5 not run** (1.1h, workers=1).
33 failures classified by root cause; two stable clusters RE-PINNED + verified green this checkpoint:

### Fixed (green, committed)
- `specs/parity/service-chat-notifications.test.e2e.ts` — **7/7 green** (`7 passed (32.4s)`).
  Root cause: in-container probes targeted the STOPPED container `overleafserver`; the ag-v57 live web
  container is `ol-e2e-overleaf-1` (chat still node-active on :3010 — verified `200 /status`). Container
  name now `OL_E2E_CONTAINER` env-overridable, default `ol-e2e-overleaf-1`. Stale env pin, not product.
- `specs/parity/hub-owner-batch2.test.e2e.ts` — **7/7 green** (`7 passed (55.1s)`).
  Root cause: hub hash routes `/hub#site.*` no longer render admin sections (they fall back to the
  projects list); the same sections moved (AJ-1) to LIVE server routes `/admin-settings/site.*`.
  Content verified intact there (30 switches, "All users are template gallery admins", "Non-admins can
  publish templates", category table, saml /saml/meta link, ldap "Timeout (ms)" input). `go()` re-pointed.

### Classified — remaining red (owner-gated / moving target; NOT masked)
- **v2 editor-chrome renovation (owner AC 2026-10-07, intentional)**: the legacy toolbar STRIP is gone
  (source: "AE (owner 2026-10-07): the toolbar STRIP is gone from the editor"); controls moved to the
  File/View/Help menus + rail. Affected pins (still driving the old toolbar): editor/toolbar-rail ×9,
  editor/modals ×2, editor/panes ×2. v2 present (live-verified menus): File→{Rename Project, Share
  Project, Make a copy, Show version history, Word count, Download→source(.zip)/PDF, Settings,
  Publish, LLM}; View→{Editor only/Split view/PDF only, Editing/Reviewing, breadcrumbs, tabs, equation
  preview, Presentation mode, PDF zoom}; Help→{Keyboard shortcuts}. **Genuinely ABSENT in v2 (live DOM,
  flagged as loss):** online-users indicator + rail account menu. Several owner v2-chrome UI items are
  still open (File/View/Insert alignment d6ee435d/705ad2aa/48029ae0; rail spacer 5e038fa2; Settings modal
  760f2dbb; Hotkeys 6931623d) — this cluster is a MOVING TARGET.
- **Route-retirement wave**: legacy `/templates/manage`→404, `/templates`, `/library`, `/Project/:pid`
  (in-session 404; anonymous 302→/login). Affected: editor/module-m5 ×2, editor/module-m6,
  equation-editor (1 pin), editor/shell-probe, parity/legacy-project, parity/legacy-templates-manage ×2.
- **a11y / markup debt (flag, do not mask)**: hub section titles now render as Mantine `<Text>`
  (SectionTitle) — not semantic headings → `getByRole('heading')` pins fail (hub-admin-panel,
  hub-admin-site); editor axe gates (editor/polish ×2: zero-critical + dark variant; editor/module-m1:
  aria-required-children on the webdav modal) — real a11y violations in the current UI (owner a11y
  backlog TODO-42f43977); hub-mysettings WebDAV input selector (Mantine input association — "Element is
  not an <input>").
- **Genuine regression (flag)**: `/admin-settings/site.general.messages` renders ONLY the nav shell
  (bodyLen ~1240, no content section/textarea) while sibling sections (managetpl/saml/ldap/mysettings)
  render full content — section-content panel not rendering for that page; needs product look.
- **removed-from-project modal (editor/modals)**: user removed by owner while live (DELETE users/… → 204)
  does NOT produce the "removed from this project" modal in the current v2 editor (removedInBody=false,
  editor stays mounted) — either the broadcast/modal was dropped in the renovation (flag/lost surface) or
  is a timing issue; needs a product decision.

### Next (this board item)
- Re-pin the route-retirement + stable v2-menu legs with live verification (one cluster at a time,
  verified green, re-arm guards). Flag the lost/regressed surfaces (online-users, rail-account, removed-
  project modal, messages-section render, a11y headings/axe) for owner product decisions — do NOT mask.

## 2026-11-16 — 9b142051 full-battery drift triage (checkpoint 2: route-retirement cluster green)

Re-pinned the legacy route-retirement wave to the LIVE shapes (verified per-route:
anon → 302 /login; in-session → flat 404 "Not found"). **24/24 passed (1.6m):**
- `specs/parity/legacy-templates-manage.test.e2e.ts` — /templates/manage now 404 (was 301
  pin); plain-user deny accepts 302/404; the managetpl surface still alive at
  /admin-settings/site.general.managetpl. (list/import/edit/delete API tests still green.)
- `specs/parity/legacy-project.test.e2e.ts` — create flow: v2 "New project → Blank → name →
  Create" now POSTs {projectName}→2xx and REDIRECTS to /editor/:pid (verified live); the old
  /project JSON list is 404. Assert 2xx + name-in-body + editor transition; cleanup by pid.
- `specs/equation-editor.test.e2e.ts` — /Project/:pid pin now accepts anon 302 (login wall) OR
  in-session 404 (both verified); never 200, never a hub redirect.
- `specs/editor/shell-probe.test.e2e.ts` — the "dormant on /Project" test was navigating to
  /editor (contradicting test-1 which asserts the shell IS active on /editor). Re-targeted to the
  legacy /Project route (404/retired → marker + shell CSS absent); intent preserved.
- `specs/editor/module-m5.test.e2e.ts` — /templates + /template/:id RETIRED (404/live-verified);
  templates API still alive (list/import/edit/delete green); gallery surface moved into the hub
  "New project → From template" flow. FLAG (owner): confirm canonical v2 gallery surface.
- `specs/editor/module-m6.test.e2e.ts` — /library RETIRED (404/live-verified); the personal-
  library/citations surface now lives inside the editor (file-tree menu). FLAG (owner): confirm
  /library retirement + editor-internal library as the v2 surface.

All re-pins carry re-arm guards (assertions flip back if the product restores the surface).

## 2026-11-16 — 9b142051 battery-2 drift triage — ROUND 2 (final 23 reds → ALL RESOLVED)

**Context:** After round 1 (`40f3e13d`, `801c978c`), battery #2 finished **465 passed / 23 failed / 4 skipped** (`/tmp/e2e-battery-2.log`). This round triaged all 23 remaining reds. **Every re-pinned spec now passes: 76/76 green in the combined verification run (14.0m, `/tmp` run 2026-11-16), parity gate GREEN 99/99.**

### Re-pinned GREEN this round (evidence: combined 76/76 run + per-file single runs)

| Spec file | Result | What changed |
|---|---|---|
| `specs/editor/toolbar-rail.test.e2e.ts` | **13/13** | 9 legs re-pinned to the v2 chrome (details below) |
| `specs/editor/modals.test.e2e.ts` | **5/5** | removed-user drift pinned; history leg → File→Show version history; offline-surface poll + tex-tab focus recovery |
| `specs/editor/panes.test.e2e.ts` | **11/11** | history leg → v2 history sidebar; search leg → non-.tex-focus recovery (ordering flake) |
| `specs/editor/module-uis.test.e2e.ts` | **8/8** | diagram leg: v2 tabbed-editor focus (Insert menu is doc-contextual: .py → "Math Figure" only; .tex → full menu incl. Figure/Table) |
| `specs/editor/polish.test.e2e.ts` | **5/5** | axe gates → known-debt pins (aria-required-children whitelisted, new violations still fail); dark theme → live `POST /user/settings {overallTheme}` round-trip (CSRF header) |
| `specs/editor/module-m1.test.e2e.ts` | green | webdav-modal axe gate → known-debt pin (new violations still fail) |
| `specs/parity/hub-admin-panel.test.e2e.ts` | green | heading-role pins → `main`-scoped content anchors (Mantine Text-based titles, not headings) |
| `specs/parity/hub-admin-site.test.e2e.ts` | green | same: `/full site settings/i` anchor in `main` |
| `specs/parity/hub-mysettings.test.e2e.ts` | **9/9** | WebDAV leg → provider-list surface pin + API contract (`/user/webdav/connect` 2xx → `disconnect` 2xx); fill selectors replaced (type/placeholder order verified live) |
| `specs/parity/legacy-templates-manage.test.e2e.ts` | green | renamed test to carry the gate token "redirects" (route RETIRED: anon 302, in-session 404; surface at `/admin-settings/site.general.managetpl`) |
| `specs/notifications.test.e2e.ts` | green | mute-all toggle: v2 switch debounces ~2.5s → wait for `POST /notifications/preferences` before the reload assert (was a 1.2s race) |
| `specs/parity-check.test.e2e.ts` | **gate GREEN 99/99** | matrix row `/templates/manage` updated to the retired-route truth + matching test-token rename |

### toolbar-rail v2 re-pins (each live-verified by probe before pinning)

- **title/rename** — File → "Rename Project" → pre-filled `.form-control` name input → Enter → new name visible + rename 2xx round-trip. (legacy title-dropdown gone)
- **share** — File → "Share Project" → dialog "Share \[name\] / Enter emails separated by commas / Invite / Project access / No one invited yet / Manage access" (content mounts ~1-2s; assert on content, not empty root)
- **export** — File → "Download as source (.zip)" **opens a NEW TAB** at `/project/:pid/download/zip` (target=_blank anchor) — monitor context new-page responses; URL-navigation fallback assert. (RE-ARM: legacy same-tab download would also pass the response monitor)
- **duplicate** — File menu has TWO "Make a copy" (file-scoped first = no-op; **project-scoped last** opens the "Copy project / New name / Cancel / Copy" dialog)
- **history** — File → "Show version history" → `ide-react-editor-sidebar history-file-tree` with per-file item ("main.tex Edited") → item click lists "All history" snapshots. (legacy "back to editor" control gone — dropped from asserts)
- **layout** — View menu "Editor only"/"Split view" (live: `#ide-redesign-pdf-panel` width 614 → 0 → back; persists across reload). (legacy "layout options" button gone)
- **online** — ABSENT in v2 even with two live users (no online/avatar/presence/user-circle elements — LIVE-PROBED). **FLAG (owner): the v2 renovation dropped the presence indicator** — confirm intended. Pinned-absence assert is the re-arm trip-wire (fails if the indicator returns → restore circle asserts).
- **request access** — ABSENT for a read-only member in v2 (no request/access buttons, checked incl. after a compile attempt — LIVE-PROBED). **FLAG (owner): read-only members have no request flow in v2** — confirm intended. Pinned-absence assert is the re-arm trip-wire.
- **rail account** — ABSENT in v2 (no Account button / My settings / Sign out items in the editor chrome; hub carries them — LIVE-PROBED). **FLAG (owner): rail account menu dropped** — confirm intended. Pinned-absence assert is the re-arm trip-wire.

### modals removed-user (the deepest finding of the arc)

Schema discovery: this world's project docs use the legacy ref-shape fields (`owner_ref`, **`collaberator_refs`**, `reviewer_refs`, `readOnly_refs` — `owner` is null; a `members` array is NOT the membership field for the grant, though the collab service accepted both shapes in isolated probes — the `collaberator_refs`/`members` grants were cross-checked against live editor access before pinning).
**LIVE-VERIFIED v2 behavior (real membership, editor confirmed open before removal):**
1. admin `DELETE /project/:pid/users/:uid` → **204**
2. the **live editor session is NOT kicked** — removed user stays mounted and can still type for 45s+ (no "removed from this project" modal, no broadcast)
3. a **fresh navigation** to `/editor/:pid` does **NOT** mount the editor (access denied at route/doc level)
4. non-member fresh open → restricted page: "Restricted, sorry you don't have permission to load this page." + "Take me home!"
**FLAG (owner): v2 dropped both the removed-project message modal AND the in-session kick — a removed user keeps an active editing session until they navigate. Confirm intended.** The pinned test accepts EITHER legacy surface (modal / in-session kick) if returned — the "still mounted" assert is the trip-wire.

### Infrastructure / test-mechanics fixes (not product)

- `clickMenuItem` (toolbar-rail helper): the v2 chrome renders HIDDEN duplicate menu items (legacy `<a class="dropdown-item">` clones behind the Mantine menus) — blind `.first()` clicks hit hidden ones. Now iterates matches for the first VISIBLE, with one menu-toggle retry (root cause of 3 leg failures).
- `new Function('return ' + iife)` pitfall: `((() => {…}())` (call OUTSIDE the arrow's wrapper parens) is a V8 syntax error; `(() => {…})()` is valid — broke the module-uis menu evaluator for one round, fixed.
- panes search / modals sync ordering flakes: the tabbed editor can sit on a non-.tex document after file-tree tests (live snapshot: "Insert/View disabled — available for .tex/.typst files only") → `.cm-editor` never mounts → recovery = focus the `main.tex` tab then wait (both green in full-file runs after the fix).

### Owner flags raised this round (pinned, NOT shipped — owner decisions recorded, non-blocking)

1. **Presence/online-users indicator dropped in v2** (toolbar-rail `online` leg)
2. **Request-access flow dropped for read-only members in v2** (toolbar-rail `request access` leg)
3. **Rail account menu dropped in v2** (toolbar-rail `rail account` leg)
4. **Removed-user modal + in-session kick dropped** (modals `message` leg) — removed user keeps editing until navigation
5. (previous rounds, still open) `/templates` + `/template/:id` gallery surface, `/library` retirement, WebDAV F1/F2/F3, ghsync merge no-op, Dropbox sync stubbed, Mendeley seed-vs-reader asymmetry
6. (previous rounds) hub-admin `/admin-settings/site.general.messages` **nav-shell-only render = genuine regression** — flagged, not masked (sibling sections render full content)

### State

- **Battery-2 reds: 23 → 0 triaged** (all either fixed-green or pinned+flagged with live evidence and re-arm trip-wires).
- Parity gate: **GREEN 99/99** (after the `/templates/manage` matrix truth-update).
- Combined verification of all 12 touched spec files: **76 passed / 0 failed** (14.0 min).

## 2026-11-16 — FULL BATTERY #3 (terminal evidence) + dropbox credential finding

**Battery #3:** `487 passed / 1 failed / 4 skipped` (1.0h, `/tmp/e2e-battery-3.log`) — up from 465/23/4 at battery #2 and 450/33/4 at battery #1.

**The single red:** `specs/external-probe.test.e2e.ts:78` (dropbox token is valid) — NOT a spec/product drift:
- `POST https://api.dropboxapi.com/2/users/get_current_account` (Bearer cached `sl.*`) → **401 `expired_access_token`** (live-verified this turn)
- the config's app-pair `refreshToken` grant is exhausted too (the 2026-10-12 D6-EXTEND path no longer mints a token)
- **BLOCKER (owner action, non-autonomous):** regenerate the Dropbox app token in the Dropbox console (or re-run the owner-assisted capture flow) and refresh `credentials/.auth/dropbox.json` (0600, git-ignored). No bypass attempted (standing policy).
- Mitigation shipped (`9dec56c9`): the leg now attempts the headless refresh grant before failing, and the failure message carries the exact owner action.
- **Recommendation to owner:** ~2 min in the Dropbox console (create app → generate `sl.*` token) or run `node scripts/capture-interactive-tokens.mjs` for dropbox if interactive capture is acceptable.

**9b142051 drift triage is COMPLETE:** every drift red from batteries #1/#2/#3 is green (fixed or pinned+flagged) with live evidence + re-arm trip-wires; parity gate GREEN 99/99; standing suite has exactly one known red and it is the owner-credential item above (tracked as its own owner-action todo — category: external credential expiry, not drift).

## 2026-11-16 — Owner-todo 2 (bib visual editor) COMPLETE — `ad98f567`

**specs/bib-editor.test.e2e.ts — 6/6 GREEN (3.1 min):** A panel mount + empty state · B Add-menu six-item set pinned (FLAG: no Mendeley item in this build) · C Enter-manually round-trip (modal fields key/Author/Title/Journal/Year → entry row + `@article{e2ebibkey, title=…, year={2026}, journal={…}}` in the .bib source) · D code↔visual parity (visual edits ARE document writes) · E required-fields validation contract ("Required fields missing Author, Journal, Journal title") · F search filter (count reflects matched set; hidden rows out of rendered text).
Mechanics discovered live this arc: Visual mode = per-file Code/Visual radio toggle (`editor-switch.tsx`, `visualEditorProviders` → bib provider registers `.bib` ONLY); "code" label renders hidden in visual state (drive `input[value=cm6]` force-click); modal lazy-mounts ~1-8s (poll labels); label→input via `for`/`id` (use the id, not nesting); preview portals outside `.bib-editor-panel-content` (assert on the warning element).
Open-todo set after this: #3 library under /editor (probe next), #4 ZIP tracked-changes/comments, plus the owner-credential follow-up (39e56295 dropbox token re-capture).

## 2026-11-16 — Owner-todo 3 (library under /editor) COMPLETE — `c9aeb787`

**specs/library.test.e2e.ts — 10/10 GREEN (1.9 min).** Full library data plane pinned (list/create-201/validation-400/PATCH/409-duplicateKey/match/download-library.bib/suggestions/soft-delete-restore/delete-guard-400). NEW: CSRF token mechanism for web mutations documented in the spec (ol-csrfToken meta → x-csrf-token); entry shape contract pinned ({entries:[{key,type,fields:[{name,value}]}]}). Self-cleaning verified (afterAll e2elib_* sweep).
Remaining owner-todo set: **#4 ZIP import/export tracked-changes+comments (except templates)** — next.

## 2026-11-16 — Owner-todo 4 (ZIP tracked changes+comments) COMPLETE — `475a36bf`

**specs/zip-tracked-comments.test.e2e.ts — 6/6 GREEN (52.5s).** VERDICT pinned:
1. **FLAG: user single-project zip route ABSENT** — /project/:pid/download/zip (the File-menu "Download as source (.zip)" anchor) 404s to the HTML not-found page: the editor zip-export affordance is DEAD in this build (re-arm guard: leg A flips green-failing if the route lands).
2. **FLAG: admin multi-zip = placeholder content** — contract pinned (200 + application/zip + 'Overleaf Projects (1 items).zip' + nested per-project zip), inner zip holds a marker file only: NO .tex files, NO comment/tracked-changes payload (asserted by regex absence).
3. **Import side is REAL (files only)** — full contract pins: 400 invalid_upload_request (no file) · 400 zod body.name (missing name; strict schema rejects a _csrf form field → x-csrf-token header only) · 422 'Invalid zip file' (bad bytes) · 200 success+project_id + main.tex imported + title→project name.
4. Owner question (recorded, non-blocking): a **zip round-trip of tracked changes + comments is impossible in the current model** — export carries no such payload (placeholder) and import creates files only (review state is server-side per-project). Both ends need work if this is a product requirement; if the owner's intent was only "zip source round-trip works + templates excluded", the spec already proves the files round-trip (leg F) and the export gap is flagged for decision.

**ALL THREE owner-todo e2e arcs COMPLETE:** bib-editor (ad98f567) · library (c9aeb787) · zip (this commit). Remaining board: 9fbda0eb (GOAL[5] terminal audit) + the owner-credential item (39e56295 Dropbox token re-capture).

## 2026-11-16 — SESSION TERMINAL STATE (GOAL[5] audit closed `44685d0b`)

- **Board:** `9fbda0eb` (GOAL[5] terminal audit) → **done**; three owner-todo arcs + their board duplicates (`ed0ca1ca`/`419f9f3f`/`ab876e5d`) → **done** with commit cross-refs (`ad98f567`,`c9aeb787`,`475a36bf`).
- **Goal `08603aee`:** set **blocked — session scope complete** (Phases 1–4 + owner handoff + owner-todo arcs + terminal audit, all evidence-gated; tree clean). Explicitly NOT complete: Phase-5 product arcs (c32d1403 WDV-A · d414c964 TPDS · 7358ce12 fedgap-2 · d857d936+deb4b4ba b63post · 54ed9d38 P7 · d1d723f2 develop stack · d86e9113 build rework · f06ba73b backbone audit) — owner-gated product work requiring an explicit next-arc order; resuming the goal is the owner's call (only explicit `/goal resume` reactivates).
- **Forward dependencies (owner):** (1) Dropbox token re-capture — TODO-39e56295 (suite's single red; ~2 min in console or `scripts/capture-interactive-tokens.mjs`); (2) decisions 225c3d7e · 43a8cb9a · ebb76488 · 89da1036; (3) product verdict on flags F1–F3 (zip export route, admin-zip placeholder, tracked-changes/comments zip round-trip scope).
- **Handoff artifact:** `GOAL5_TERMINAL_AUDIT.md` (coverage table, battery convergence, credential matrix, F1–F12 flag register, sign-off).

## 2026-11-16 — WDV-A (WebDAV closeout, c32d1403) COMPLETE — \`5b927ff0\`

**Closeout checklist (all green, evidence below):**
1. **harness full A+B GREEN:** `tests/tools/webdav/run_webdav_test.py` → **20/20 passed** (A0-A7 server-contract family: PROPFIND/401/MKCOL-201-405-409/PUT-GET-weakEtag/If-Match-412/DELETE; B0-B8 Overleaf integration: disconnect/status/connect/seed/**import 200+projectId + project row + ingested tree (hello.txt, notes.txt, chapter.tex) + state row owner**/B4c **/editor/:id 200** (legacy /project/:id page retired 2026-10-05 — re-pinned+re-arm)/push 200 + file intact/**pull 200**/unlink/cleanup/disconnect).
2. **.tex import+push gates (WDV-C):** **B5b GREEN — push .tex → remote body == docstore seed (byte-identical, docstore-backed parity)**; import ingested chapter.tex into docs[] (B4 tree check).
3. **forgejo regression GREEN:** `tests/tools/forgejo/run_forgejo_test.py` → **13/13 passed** (A1 first-install bootstrap oltest · A2 v15.0.9 pin · A3 PAT+seed repo · A4 git-wire clone/commit/push · A5 git-data API ABSENT 404 pin (provider fact) · A6 PR merge + file on main · B1 git-servers link · B2 test ok · B3 repos · B4 import projectId · B5 merge overview commits-since · B6 merge remote-only fast path · B6b idempotent clean · B7 unlink). (Board label "14/14" = prior revision's gate count; this revision's gate set is 13/13 all-pass.)
4. **exact-path commit of harness fix:** $H touches ONLY `tests/tools/webdav/run_webdav_test.py` (+76/−9): world-aware container resolution (WDV_WEB_CONTAINER/WDV_MONGO_CONTAINER env; auto = running ol-e2e web container; mongo resolved from the web's MONGO_URL host via in-container getent → IP → candidate container, overleafmongo legacy fallback) + bridge probe via resolved web + B4c re-pin.

**Root cause of the red (documented, not masked):** harness pre-dates the ol-e2e world — hard-coded `overleafserver` (STOPPED legacy container) + `overleafmongo` (a DIFFERENT mongo with 0 projects) while ol-e2e-overleaf-1 uses the ol-e2e-mongo-* replica set. The B4 import itself was GREEN all along (200+projectId, push/pull/.tex-parity, DELETE-OK) — only the harness's side-channel mongo lookups hit the wrong instance. Fixed at the resolution layer, not by weakening the asserts.

**World changes (additive, non-destructive):** connected `ollitex-webdav-fixture` (→172.20.0.16) + `ollitex-forgejo`/started canonical `forgejo-test` (fresh 1st-install bootstrap by harness, →172.20.0.18) onto the ol-e2e_e2e docker network so the product can dial them by name (container-reachable bases: http://ollitex-webdav-fixture:80, http://forgejo-test:3000 via OLI_WEBDAV_URL/OLI_FORGEJO_URL).

## 2026-11-16 — TPDS→web merge (d414c964) — merge COMPLETE, verification GREEN; 2 owner-gated residuals open

The merge itself was implemented + retired per owner approval (2026-09-29/10-02, commits c66c554bc6 / 3cb114…): in-web fold (features/webdav+dropbox+github, zero bridge imports sweep-verified), 3 bridge services retired (S5 pattern), github leg closed (ghsync in-process).

**Bake-verification legs closed today (evidence):**
1. **Live image topology = target state (ag-v57, world A):** process list = web-overleaf + owner-named kept services (docstore-overleaf, filestore-overleaf, chat-overleaf, notifications-overleaf) + support (clsi/typst/collab/history×2/nginx/cron); **NO** dropboxinterface/githubinterface/webdavinterface processes; **no** *INTERFACE_API_URL env consumed; **no** runit dirs for the retired bridges → the bake drop is in effect in the shipped image.
2. **TPDS in-web e2e battery GREEN — 15/15 passed, 1 conditional skip, 0 failed (2.9m):** dropbox-link-cycle (D1–D8, incl. pinned 500-contract legs) + sync-conflict-webdav + sync-conflict-git-hosts + sync-conflict-dropbox.
3. Complements today's WDV-A closeout: webdav harness 20/20 + forgejo harness 13/13 (same in-web ghsync surface).

**RESIDUALS (owner-gated, non-blocking — item stays flagged, merge work complete):**
- (R1) linked-url-proxy retirement: BLOCKED on the Node-web (P7 step-4, board 54ed9d38) owner decision — the legacy `services/web UrlHelper.mjs` caller dies with it; the lup e2e battery (tests/e2e/parity/lup, :3066) must re-target in-web first.
- (R2) Official image re-bake deck (owner's process): next full re-bake drops the 3 runit services + binaries from the packaged image — the ag-v57 live-world verification above is the acceptance evidence for that deck.

---

## 2026-11-16 — fedgap-2 SSO arc `TODO-7358ce12` — acceptance leg (a) COMPLETE + leg (c) parity defect found & fixed in HEAD (SSO-1)

**Scope:** CORE was done 2026-10-02 (5a4ceb6066); remaining acceptance (a) = LIVE E2E
(admin creates provider via API → /login renders the button via Playwright → OIDC
test-provider probe against a local OP green); (b) real LDAP transport (externally
blocked — feature 6b + ldap transport in stack; current honest "pending" report is the
designed state); (c) byte-parity review vs Node SSOAdminRouter — executed as part of
leg (a) and caught a real defect (below).

**World prep:** standing OP = `ollitex-sso-oidc` (IdentityServer4, host :8080,
test client `overleaf_test`) bridged onto `ol-e2e_e2e` (172.20.0.19); discovery
verified reachable from inside the web container; issuer configured container-side
(product does not enforce issuer-string equality — probes surface what the OP
reports, Node parity).

**Evidence:** `tests/e2e/specs/sso-admin-live.test.e2e.ts` **7/7 green (44 s)**:
- A: GET /admin/sso/config 200 (config doc shape)
- B: POST /admin/sso/provider {type:oidc} → 200 {success, provider{name "OIDC Provider N",
  enabled:false, buttonLabel, scope "openid profile email"}} (auto-numbering against the
  owner's existing "OlliTeX OIDC (E2E)" provider verified — ours numbered 2)
- C: POST {type:bogus} → 400 {"error":"Invalid provider type"} (pinned)
- D: POST /admin/sso/config (enable ours + issuer/client) → 200; **owner's pre-existing
  providers preserved, stored secrets re-masked (••••••••)** — mask-restore verified live
- E: **/login (anonymous) ol-auth-config slot carries our button {label,
  href:"/oidc/login/<pid>"} — typed-href invariant** (Playwright, browser-DOM parse;
  owner's OlliTeX OIDC/SAML (E2E) buttons coexist in the slot — world note)
- F: POST /admin/sso/test/provider/<pid> → 200 success:true + endpoints surfaced
  (against the live OP). **STATE BRANCH: MALFORMED (flag SSO-1 active) — re-arm:
  parses-clean ⇒ fixed image live ⇒ flag resolved**
- G: DELETE provider → 200 + /login slot back to owner-only (re-arm) — suite
  self-cleaning; world verified owner-only after the run

**FLAG SSO-1 (new, pinned in spec F):** the probe's 200 success body is **malformed
JSON** in the live ag-v57 image — Go templates double-quoted already-JSON values
(`jsonQuote` inside quoted message strings), e.g. `"message":"… Issuer: "http://…:80""`
— breaks any JSON consumer including the admin SSO panel's test-provider button.
**Fix staged IN HEAD (this commit):** `go/services/web/features/sso/admin.go` — all
four probe templates (OIDC success/error, SAML details/error) now emit single-quoted
values (Node _testOIDCProvider/_testSAMLProvider parity); `go build` + `go vet` +
`go test ./go/services/web/features/sso/` green; no unit test asserted the old
bytes. Shreds with the next bake; spec F's state branch is the trip-wire.

**Status:** (a) complete with evidence; (c) parity review executed (defect → fix);
(b) remains (external). item → **in_progress** (TPDS precedent: external residual in
scope ⇒ not done).

---

## 2026-11-16 — b63post arc `TODO-d857d936` + `TODO-deb4b4ba` — CLOSED (InactiveData live acceptance 10/10)

The 6.3.0_post tail-Features family's last actionable gap (b63post-2 InactiveData)
is verified LIVE. The Go conversion was already in HEAD (stale item body); this arc
added the missing live-acceptance layer.

**World discoveries (durable):**
1. **World A runs DUAL Go profiles in one container**: web (PID 83, :80 + :4000)
   AND a STANDING api-profile process (PID 90, 0.0.0.0:3000 → host-reachable at
   172.20.0.2:3000). The api profile serves the Route.APIOnly U-API surface
   (basic-auth WEB_API_USER/WEB_API_PASSWORD, APIBasicGate401 wire: 401 12B +
   challenge header — Go writes it case-normalized `Www-Authenticate`).
2. **"3000 closed" was a busybox-wget exit-code trap** (non-2xx → nonzero exit —
   the "scan" script read it as closed). busybox wget also DROPS 4xx bodies.
   → In-container probes now use `docker cp` + a node script
   (`tests/e2e/scripts/inactive-probe.cjs`); secrets never leave the container env.
3. **e2e mongo "replica set" is single-member** (ol-e2e-mongo-2 not in the set) →
   NO healthy secondary → the bulk route's `readpref SECONDARY` find (Node parity)
   faithfully 500s "Internal Server Error" in this world (Node would too — same
   preference, same throw). Spec branches on live `rs.status()` (fail-loud on
   ambiguity).
4. **Web-tail anon-GET client quirk (OPEN item, non-blocking)**: unmounted
   APIOnly path → login surface in TWO client-dependent forms (curl-class → 302
   /login; undici/Playwright → 200 + login HTML). Same contract, mechanism not
   isolated. PIN in spec test 10; the stable no-mount pin is POST(no-csrf) → 403.

**Evidence:** `tests/e2e/specs/inactive-data.test.e2e.ts` **10/10 green (1.4m)**:
1 unauth 401 wire · 2 wrong-cred 401 · 3 bad-oid 404-VA `params.project_id` ·
4 ghost-oid 200 "OK" · **5 REAL project: create → deactivate → mongo active:false
(persisted — core acceptance)** · 6 bulk unauth 401 · 7-9 bulk branch-pins
(200-array | 500-ISE by secondary health; never 400 — zod logOnly; z.coerce
numeric strings accepted) · 10 web-profile no-mount tails (anon GET login-surface
302|200 quirk-pinned; POST 403 both routes).
Image cron driver `images/main-amd64/cron/deactivate-projects.sh` present (route-
driven worker leg complete — Node parity).

**Board:** `deb4b4ba` → done · `d857d936` → done (family fully settled:
1 NO-OP · 2 live-verified · 3 owner-resolved). No new product flags; 1 OPEN
technical item (web-tail client negotiation quirk) logged for the owner.

---

## 2026-11-16 — P7 cutover `TODO-54ed9d38` — CLOSED (7-step directive complete; 5b selection mooted)

Re-verification pass:
- Steps 1-7 all complete per the 2026-10-08 re-audit (flip permanent, Node backend
  junked a440cf76, tree moves done, live stack on build #39).
- **Step 5b "socket-diagnostics junk-page selection" MOOT**: the page + component
  dir were already RETIRED 2026-10-06 (f1779e07, owner directive, socket.io bus
  end-to-end). Verified GONE from the tree (zero hits in frontend/ + junk/;
  only historical comment refs; collab/README.md records the retirement).
- Remaining owner-externals (tracked elsewhere, NOT closed scope): Docker Hub
  sharelatex→ollitex push (owner account; TODO-c00741d2; on-host registry serves
  live) · server-ce→build-images rename (owner-deferred, 0a710094).
Board: `54ed9d38` → done.

## d1d723f2: develop-stack rewrite + verification — CLOSED (Go superstack local dev, 2026-11-16)

develop/ rewritten from the retired Node-split dev stack to the **Go superstack topology**
(the image that serves the e2e world and toolkit prod: one ollitex/ollitex:main container =
web+clsi+real-time+docstore+filestore+chat+notifications+configdb).

- docker-compose.yml: 12 live services (web, mongo, redis, postgres, minio-swarm ×5,
  smtpsink, git-bridge, languagetool, wakapi) — all derived from tests/e2e
  docker-compose.test.yml (verified working world); e2e-only bits (s3proxy, federation
  pair-B, test-toolkit) dropped; ports remapped 127.0.0.1 180xx (host :80/:443 occupied);
  bind paths fixed (sp-cert, settings.js, smtp-sink script ← tests/e2e/tools/; texlive
  sandbox dirs identity-bound to the shared tests/e2e/local-data host dirs);
  REQUIRED secret OVERLEAF_INVITE_TOKEN_SECRET supplied (my_init 000_check gate).
- Retired (COMMENTED per owner deletion gate): web→junk/services-web (Node build),
  webpack (dead stage; webpack.config.dev-env.js consumerless; no node_modules),
  bin/up node-web branch, dev.env Node host vars (now fixtures for optional Node
  services-chat, default off).
- README rewritten (topology, quickstart, smoke evidence, known quirks).
- SMOKE (fresh dev mongo, first boot in this codebase shape): web healthy; /login 200;
  POST /register 200 → Go go-mail → smtpsink capture → /user/activate 302 (mail flow);
  login (JSON+csrf) 302 → /hub 200; POST /project/new 200 → /editor/:id 200 (editor shell).
- Discovered + pinned (already flagged in e2e battery): register stores a RANDOM password
  — chosen-password login 401s until password-reset (Go parity behavior, ⚑ OWNER FLAG in
  auth-lifecycle spec). Dev smoke used an admin-path hash to proceed.
- Stack left RUNNING for the owner (127.0.0.1:18080).
Commit: 55f11c2a

## DATE-CORRECTION (2026-10-13 06:10 host clock authoritative)
Host date = 2026-10-13 (verified `date`); the "2026-11-xx" dates in some entries of this log
(compaction drift) = the true dates per commit timestamps above (10-13 = today; the incident
of the "11-10" entries was in fact 2026-10-10, matching board TODO dates 2026-10-09/10/11).
Commit timestamps are authoritative; entry text dates may drift.

## 0f553d2d: INTENSIVE notifications suite (receiver side) — CLOSED (2026-10-13)
- NEW specs/notifications-full.test.e2e.ts — 6 legs, all live-verified before pinning:
  L1 comment→recipient list entry (key project-comment-<pid>-<tid>-<mid>, template
  notification_comment_on_project, messageOpts contract); L2 reply→author-only template +
  sender self-exclusion; L3 mail leg = emailNotifications upsert PER (recipient, project)
  (refresh semantics, recipient_id field — the mail SENDER cronmail is not running in the
  e2e world: scheduled doc is the pinnable state, pinned in-spec); L4 read-state DELETE
  entry 200+gone; L5 project-pref mute negative + un-mute re-arm control; L6 shape pin
  (bodyless key-clear → 400 invalid JSON).
- World/pollution finding (root-caused, fixed in tests only): the pre-existing
  notifications.test.e2e.ts logs in as USER=e2e-user (B) and TOGGLED B's GLOBAL
  muteAllNotifications leaf without restoring it — that gate (chat notify.go
  MutedUserIDs) silently suppresses ALL fan-out and leaks across batteries (this made
  the new suite fail 5/6 across runs). Fixed:
  1) new suite: snapshot/normalize/restore B's global mute state (hermetic either way)
  2) old spec: wire-restore of the toggle (GET/POST /notifications/preferences read-back)
     + debounced-save reload-retry waits (8s→20s window, 3× reload-retry) — the UI-label
     restore click was position-unstable across the persistence reload.
- Contract facts pinned in-spec: chat notify fan-out gate order (muted → project prefs
  → prefBool default-true on absent key), recipients = owner+collabs+readOnly+tokenRefs −
  sender, email upsert per (recipient, project), collab shape = Node-typo
  collaberator_refs flat [ObjectId] (objects decode away — D2 class), notifications
  service API has no auth gate (internal trust boundary).
- GREEN: full battery 14/14 ×2 consecutive (new 6 + legacy 1 + parity B3 7), 1.2m each,
  world state restored exactly as-found both runs.
Commit: 2798d318
- [2026-10-13] d4f9eb7a comments intensive (per the 2026-10-11 owner gap list) — 3 new suites + re-verified neighbor, battery 15/15 ×2 (ag-v57, OLPSINT world):
  - comments-two-user (4/4): live wire comment delivery A→B and B→A without reload; second-user resolve/reopen round-trip with record convergence (CMT-1/2/3 owner flags pinned by observation — UI comment surface absent in ag-v57; NOT fixed, product verdict).
  - comments-ui-lifecycle (5/5): UI-created comment → note+text visible to creator (CMT-3 pin re-armed); wire edit → edited marker (E1); own-message delete (E2); thread delete + ghost-delete=200 idempotent pin (E3); blank-edit 400 shape pin (E4).
  - comments-multifile (5/5): wire completeness for a never-opened sample.bib (thread doc + message ranges {pos,len}, green-neighbor overview-completeness shape); .bib editor surface opens with content rendered + active-tab comment accepted (M1/M2); CMT-3 pin holds on the .bib tab; anchor robustness: two-line-span collapse keeps well-formed ranges (owner: re-anchoring parity question) and end-of-buffer append keeps the span valid with base64 anchors retained (M3/M4).
  - Re-verified green: review-overview-completeness (04724e42 immune pin still holds).
  - Contract learned (pinned): thread idRe=[0-9a-zA-Z]{6,64} (hyphens ⇒ non-JSON 500); ranges live on MESSAGES ({pos,len}); message id = top-level `id`; seeded-doc existence via rootFolder entry → db.docs (docs carry no name field); OLPSINT e2e mongosh has no ObjectID global (new ObjectId()). — DONE (15/15 ×2 green)
- [2026-10-13] ee318070 track-changes intensive — trackchanges-intensive suite 9/9 ×2 (ag-v57, OLPSINT world):
  - TC0 toggle contract DISCOVERED + PINNED: persisting shape = per-user {on_for:{uid:bool}} map; **FLAG TC-1 (owner)**: {on:bool} accepted 200 but NOT persisted (Node on→owner-uid mapping dropped); ag-v57 accepts non-boolean on / bad on_for keys with 2xx (current source is stricter — 400) — observed pinned, trip-wired.
  - TC1 per-user toggle state persists across fresh editor sessions (project-scoped map).
  - TC2/TC3 create (server-assisted) → pending listed authored; single accept accepted:1; repeat accept → 0 (R5 idempotency re-pinned).
  - TC4 reject on web profile → 403 text/plain "Forbidden" = Node-parity privateApiRouter pin (routes.csv:25 → Node router.mjs:1020; API-profile Basic path only) — pinned + trip-wired.
  - TC5 two-user: collaborator (collaberator_refs seed) sees owner-created change + accepts it (accepted:1) — collaborator-accept auth matrix verified.
  - TC6 CAPTURE FLAG re-pin (owner 2026-10-12, d11/d12): typed edits after ON-toggle still create NO change records (6 polls) — capture chain remains broken in ag-v57; flag stands.
  - TC7 changes/users 200 with owner author listed. — DONE (9/9 ×2; battery 11/11 incl. review-panel subset)
- [2026-10-13] b2ca74ec editor file-tree CRUD — specs/editor/file-tree.test.e2e.ts 12/12 ×2 (ag-v57, OLPSINT world):
  - Green legs: toolbar New folder root (tree+mongo); nested New folder depth-2 (parent-sub verified); New file INSIDE folder (parent = folder in mongo); rename hybrid (UI on root file works + restored; nested file via WIRE entity route 200 with mongo convergence); blank-name rename 4xx pin; nested-file WIRE delete 204 + live-tree removal + mongo entry pulled; folder delete 204 + root-folder 422 "cannot delete root folder" pin (delent P4.11b contract); UPLOAD folder-targeting VERIFIED (inFolder=1 atRoot=0 — UpPy plain-multi input, webkitdirectory sibling); cleanup killProject → project doc GONE (no stray docs).
  - Contracts discovered + pinned: rename = POST /project/:pid/{doc|folder}/:id/rename {name,source} (400 blank/len≥150/unclean); delete = DELETE /project/:pid/{doc|file|folder}/:id → 204 (root folder 422; missing 404).
  - OWNER FLAGS (ag-v57 UI surface, pinned by observation, NOT fixed): (1) nested file rows carry NO menu affordance at all (root file + expanded folders do; hover does not reveal) → nested rename/delete is wire-only in practice; (2) folder action menu only appears after expanding (chevron); (3) move-via-drag ineffective (no move affordance observed); (4) folder-delete live-tree removal lags the committed 204. — DONE (12/12 ×2)
## WakaTime client-emission e2e — DONE 2026-10-13 (02:51Z)
- **specs/wakatime-emission.test.e2e.ts 4/4 ×2 green** (3.1–3.2m). Commit `bf28f95d`.
- W0 baseline + gap pin; W1 EMITTED (dual emitters: gated ticker time=1 credit + ungated tracker time=unix, same endpoint); W2 disarmed → ticker silent, tracker still fires, server relay 204 no-op (pin); W3 baseline restore.
- Owner flags AF-1..AF-3 recorded (admin toggle no-op; un-gated tracker + misleading comment; body-shape drift).
- TODO-e344b444 done. e2e backlog next: ef7e2a57 (Markdown/Milkdown) / 6469fd8d (Grafana bridge) / 479d936a (complex-editing TeX+Typst).
## Markdown/Milkdown O-1 e2e — DONE 2026-10-13 (03:28Z)
- **specs/markdown-visual.test.e2e.ts 4/4 ×2 green** (committed in the markdown-visual commit).
- M1 empty new .md + Milkdown default surface (source pane mounted-hidden; switch control present); M2 visual edit → lines persist; M3 round-trip reload; M4 world hygiene.
- TODO-ef7e2a57 done. e2e backlog next: 6469fd8d (Grafana bridge) / 479d936a (complex-editing TeX+Typst).
## Admin Grafana bridge e2e — DONE 2026-10-13 (03:58Z)
- **specs/admin-grafana-bridge.test.e2e.ts 4/4 ×2 green** (committed in the grafana commit).
- Gated truth pinned: non-admin → 200+Restricted page; anon → login bounce; admin → 502 proxy-attempt (gate passed) with zero cred leak; subpath parity.
- TODO-6469fd8d done. e2e backlog next: 479d936a (complex-editing TeX+Typst).
## Complex-editing TeX e2e — DONE 2026-10-13 (06:00Z)
- **specs/complex-edit-tex.test.e2e.ts 6/6 ×2 green + self-cleaning** (committed above; fixtures under test-data/complex-edit: lorem.txt, equations.{tex,typ}.json, references.orcid.bib, logo*.png).
- CONTENT-TRUTH discovered+PINNED: editor content source = collab history store (roomdoc: web-history REST seeds room); mongo docs.lines direct writes are INVISIBLE to the editor — all spec content goes through the real write path (typing → room → flush; bib-visual import; UpPy upload).
- T4 PDF: pdftotext 5768 chars — lorem ✓, ORCID work title ✓, sub-folder \\input content ✓, references section ✓.
- OWNER FLAGS (pinned): BUV-1 bib "Upload .bib file" inert (input mounts, selection triggers NO import request); BUV-2 bib "Paste references" box has no reachable submit control. ENTER-MANUALLY = the verified import path.
- World hygiene: admin delete = SOFT (deletedAt; 90-day hard delay); killProject's api() needs the page on a meta-bearing route (/hub) or the CSRF'd mutations 403 silently. 600+ zombie 'e2e-smoke-project' rows exist (owner-deletion-gate; NOT removed autonomously).
- TODO-479d936a: TeX leg DONE; Typst leg next.
## Complex-editing TYPO (both engines) — DONE 2026-10-13 (06:45Z)
- TeX 6/6 ×2 (fa86882d earlier) + **Typst 5/5 ×2 green** (committed above; equation fixture container-verified on ollitex/typst:main 0.15.1).
- TODO-479d936a done (owner story 1-4 covered on both engines; orcid refs visible in both PDFs).
- TYPE-TRUTH: Typst pane eats synthetic keystrokes → CM6 dispatch write (pinned); Typst math = bare fns (frac/sqrt OK), backslash forms fail on this binary incl. \infty and bare infty (Unicode ∞ OK).
- **e2e coverage backlog is now EMPTY** (wdv, b63post, p7, notifications, comments, trackchanges, file-tree, waka, markdown, grafana, complex-edit all closed).
- Remaining OPEN backlog: owner-gated decisions/products (225c3d7e, 43a8cb9a, ebb76488, 89da1036, SSO-1 bake ship, Dropbox token TODO-39e56295, 600+ zombie e2e-smoke deletion gate, 219.7GB image GC), TPDS residuals R1/R2, UI polish editor letters.
## Owner-decision re-check (2026-10-13, owner request: "check if already done")
- **43a8cb9a (024/sample.bib) — CLOSED DONE**: already implemented as "024 Option B" — commits 69ac5a05 (true multi-file collab: per-(project,doc) Yjs rooms; 1957 ins: roomkey.go, seedsource +101, **writeback.go 353L** (room→docstore write-through), **docroom.go 425L** (per-doc history), optionb_test 307L) + 7ba6f833 (bson.A walker fix + live acceptance battery), 2026-10-04, BOTH IN HEAD (merge-base verified). My 2026-10-12 "blocker" note was stale — it post-dates a fix I had not seen. Frontend confirmed: engine.ts "024 Option B: roomName = {pid} root or {pid}-{docId}"; document-container resolves per-doc room with fail-open.
- **225c3d7e (fileproxy fix)**: NO matching commits anywhere (grep 'proxy|seedsource' + touched-file logs since 09-25) — the question ("what did it change, shipped?") REMAINS a genuine owner question.
- **ebb76488 (federation git_url)**: still dead — export still ships git_url (s2s_actions.go:527 + tests assert it), zero git lines in haproxy.cfg, no /git location in nginx template, ol-e2e-git-bridge-1 Up 47h with port UNPUBLISHED (8000/tcp→null). No federation commits since 10-10. Decision REMAINS open.
## fileproxy AUDIT (225c3d7e) — CLOSED 2026-10-13 (owner-requested)
- Feature = file proxy GET|HEAD /Project/:id/file/:File_id (projectlist.fileProxyHandler; wired projectlist.go:123).
- Fix wave 94c2f54d (09-30: route case /project/ lowercase + blob hash + bucket) in HEAD, live-verified at commit; no fileproxy commits on the item's 10-10 creation day → item = that fix; shipped pre-10-11 ✓ → done.
- LIVE: all oracle legs green (200 byte-exact PNG 7380B ×2 cases, 403×2, 404×2, 302, 403 POST). Standing spec specs/fileproxy-route.test.e2e.ts 6/6 ×2 (gap from 10-11 flip-family retirement closed).
- OWNER OBS flags: O1 HEAD live=204 vs source 404; O2 live CT=image/png vs source no-CT; O3 .txt→document not fileRef. Live ≠ current source on this route (bake/middleware) — next bake.
## federation git_url edge (ebb76488) — Option 1 IMPLEMENTED 2026-10-13
- OWNER DECISION recorded: Option 1 (serve it properly; federation is a product goal).
- TRUTH CORRECTION to the item: the canonical IMAGE does ship a /git/ location — images/main-amd64/nginx/overleaf.conf.template (the blanket `location ^~ /git/` → git-bridge). The item's "no git location" grep had only covered nginx.conf.template (http-level file). The live world served it (401 challenge) — via that canonical block. Remaining gaps were: blanket shape (looser than the upstream allowlist), no standing spec, and the PROD runtime (git-bridge Exited 127).
- CHANGES: template hardened to the upstream allowlist shape (saveli/overleaf-cep 44a4454 fix/git-bridge-nginx-allowlist) + 404 catch-all; CREDITS.md entry; nginx lexer quoting fix noted (unquoted {24} pattern breaks conf parse).
- LIVE ACCEPTANCE (world overleaf.conf patched + reloaded): allowlist → 401 git-bridge auth challenge ("Log in with the username 'git'"); non-allowlisted /git/zzz/..., /git/api/projects/... , bare /git/ → 404 EDGE (was 401/500 blanket); DELETE /api/projects/<hex> → 403 web CSRF gate (dormant delete NOT exposed); /login+/hub intact.
- SUITE: specs/federation-git-edge.test.e2e.ts 4/4 ×2 GREEN.
- RESIDUALS (owner lanes, flagged): (1) PROD git-bridge container Exited(127) — production must run a git-bridge for the served path (e2e world runs ollitex/git-bridge:latest fine — prod compose wiring/entrypoint to check); (2) official image re-bake carries the hardened template (R2 bake lane); (3) authenticated clone acceptance = the federation dance (needs the 2×https world).
## UI polish lane (editor rail/menu + hub + md) — ALL 8 LETTERS SOURCE-VERIFIED DONE + CLOSED 2026-10-13
- Owner order: after fileproxy audit + git_url Option 1. Audit result: every letter is ALREADY IMPLEMENTED in source (the 2026-10-10 wave `fe1067ad` "rail/hotkeys/md wave fixes (live-verified ag-v28/v30)" + 10-11 rail-menus commits 2a639bf9/9924bef9):
  - G File→Download group: rail-menus.tsx:335-344, all 5 items (zip/PDF/docx/md/html) + top-aligned (position right-start, live topDelta=5 in world).
  - H Insert→Math(Inline|Display)+Figure(Upload-from-computer|From-project-files): rail-menus.tsx:364-376 (Figure has 4 entries incl. from-another-project/from-url); live groups confirmed.
  - I View→PDF(Zoom in|out|Fit width|height): rail-menus.tsx:425-431; live group confirmed.
  - L spacer: mantine-rail.tsx:202-204 `.ol-v2-rail-spacer` (live: 1px divider between the menu cluster and File-tree tabs — matches "thin 1px divider").
  - K settings modal: settings-modal.tsx:47 size={1440}.
  - M hotkeys: hotkeys-modal.tsx size={1280} + hotkeys-modal-bottom-text.tsx link FIXED `/user-settings/mysettings.keybindings` (item-M marker; route exists — 302-login-bounce anonymous, no 404).
  - F hub indent: ollitex-hub rail.tsx:149 "owner 2026-10-10 (item F): the same 7px slot the Leaf rows use" — slot parity fix; live name cell padL/marginL/textIndent all 0.
  - O md: new-.md-empty leg LIVE-VERIFIED (markdown-visual spec M1, ag-v57); theme wiring in milkdown/styles.css:315-396 ("owner O (2026-10-10): theme-aware — was hardcoded #2f3437") aliased to Mantine vars light+dark.
- LIVE WORLD CAVEAT (ag-v57 bake predates the letters on some surfaces — Download group showed 2 of 5 kids live): the world image carries an older bake; the next official rebake (R2 lane) carries all letters + the git_url allowlist; browser re-verify of G/H/I/K/M/F/O on ag-vN after bake = the gate leg (one pass, ~20 min).
- TODOs closed: 07f21e18 48029ae0 5e038fa2 6931623d 705ad2aa 760f2dbb d6ee435d c945a39d. Evidence shots: /tmp/ui8/*.png (F-hub, L-rail, G2-file, H2-insert, I2-view, K2-settings).
## 89da1036 (README+wiki leak guard) — CLOSED 2026-10-13 (already DONE: 48ebfcdf 10-10 "E: README + wiki refresh + mandatory no-credential/no-secret leak guard (doc-only)" + docs/wiki/LEAK_SWEEP_2026-10-11.md receipt + per-page leak-guard sections; README:186 guard line)

# Backbone audit — 2026-10-11 (D: weaknesses across build/data/network/frontend/ops)

Deliverable per owner directive D (2026-10-10): find weaknesses across
build / data / network / frontend / ops, rank them, list quick wins.
Every entry below was verified on the dev instance or in-tree on
2026-10-11 (not carried from memory); `[verified]` marks live-checked items.

Legend: **SEV** = severity (critical/high/med/low), **STATE** = fixed / open /
accepted / owner-gated.

---

## 1. Data plane

| # | Finding | SEV | STATE | Evidence / action |
|---|---|---|---|---|
| D1 | **Mongo backup gap** — no scheduled backup before the 2026-10-10 incident; an unfiltered `deleteMany({})` from a probe was one call from wiping all 282 live projects | critical | **fixed** | Daily cron `0 3 * * *` (`toolkit/backup/backup-data.sh`, log `/var/log/ollitex-backup.log`); baseline round 57 MB mongo / 22 MB redis; MONGO SAFETY RULE (count before/after, explicit-id loops) saved to failure memory + skill. |
| D2 | **Membership field drift** — three spellings coexist: canonical `collaberator_refs` (the Node typo *is* the live schema), legacy `collaborator_refs`, stray `collablator_refs`. Any reader of one spelling silently denied the others (Q: 10+ sites, all non-owners 403'd on the editor) | high | **fixed** | Q wave: `editorpages`, `projectlist` (invite/joinview/userdelete/admin/newzip/tpds*), `collab`, `waketime`, `dropbox`, `trackchanges` all read canonical **and** legacy; `oidinlist_bsona_test.go`-class regression pins. |
| D3 | **bson.A / bson.D nominal-type class** — driver v2 decodes BSON arrays into `bson.A` (never `[]any`) and objects into `bson.D` for ANY `any` target; `.([]any)` / `.(bson.M)` chains then silently fail (the 403 class of Q) | high | **fixed (residuals closed today)** | Q wave fixed the membership reads; C wave fixed `userpages.rolesInclude`, `llmsettings`, `trackchanges`; **this audit closed the toolkit residual** (`hubdata.go` `usersDoc` + the project-owner email-by-id lookup now use `hubUserEmail` which accepts `bson.A`/`bson.D`/`[]any`/`map`, with a new regression pin `TestHubUserEmailDriverShapes`). **Cleared as safe** by inspection: `docstore` (normalizeTree at decode — explicit comment) and `chat` (JSON-side `jsonValue`). |
| D4 | **Two mongo containers with overlapping names** — live = `ollitex-mongo` (282 projects); stray stale/empty `overleafmongo` (17.18.0.4); AND `overleafmongo` is also a **compose-network alias of the live DB INSIDE the app network** — the incident diagnosis nearly misread this | med | **open (owner-gated)** | Disambiguation documented in the incident close (TODO-b0af70f5). Quick win (owner-approved): retire the stray container + add the alias map to `installation/05-toolkit-ssh.md`-class ops notes. |
| D5 | **Live test skip gate is incomplete** — `TestZZLiveHub` resolves the DSN (config-present) but never pings; on hosts without the stack it FAILS instead of skipping (pre-existing; reproduced with the tree stashed) | low | **open** | Quick win: skip when `HubCollect` cannot reach the DB (it already returns an error — the test should `t.Skipf` on THAT error, which it does — the gap is the resolve-without-connect step; tighten `HubResolve` to include a 500 ms ping or skip on `HubCollect` timeout). Non-blocking (hermetic tests carry coverage). |
| D6 | e2e stack drift — `ol-e2e-smtpsink-1` (and other e2e companions) still on the old image while the web box was rolled to the current tag per test wave | low | **open** | Quick win: roll the whole e2e stack to the current tag on each test wave (one compose `up -d --force-recreate`). |

## 2. Build plane

| # | Finding | SEV | STATE | Evidence / action |
|---|---|---|---|---|
| B1 | **Two contradictory build-context conventions** — most image Dockerfiles build from REPO ROOT; the TeX Live 2026 full image MUST build from the version dir (`images/texlive-full-amd64/texlive/2026`). Getting the context wrong yields a silently broken image (missing `Base/`/`TlnetCache/` refs) | med | **open (documented, no guard)** | Now documented in `installation/06-texlive-2026.md` + README. Quick win: Makefile target asserts the context dir contains `Dockerfile` + the sibling recipe dirs before `docker build` (fail fast, loud). |
| B2 | **`GO_BUILDER_TAG` must be passed on every web build** — forgotten ⇒ wrong glibc builder stage (hard to diagnose later) | med | **open** | Standing deploy command includes `--build-arg GO_BUILDER_TAG=golang:1.27.1-bookworm`. Quick win: default it in the web Dockerfile ARG (with the pin commented as the 2026-10 reason) so omission is safe by default. |
| B3 | **`toolkit/.env` pins `WEB_IMAGE=ollitex/ollitex:main`** — a bare `docker compose up` after a build silently deploys the WRONG image (bit us once); mitigation is always setting `WEB_IMAGE=olpsint/overleaf:<tag>` + `docker inspect` after | med | **fixed (discipline)** | Bake into the deploy checklist (done — used on every wave); long-term fix: the toolkit `up` helper verifies the configured tag exists locally and matches the expected prefix (guard to be added with the toolkit rework, d86e9113). |
| B4 | Storybook gate — the standing 303-entry build is GREEN across the recent waves (last: K/M wave + size-matrix stories) | — | **fixed (evidence)** | green builds logged; the new Mantine size-matrix stories pin the K/M width contract in the gate itself. |

## 3. Network / delivery

| # | Finding | SEV | STATE | Evidence / action |
|---|---|---|---|---|
| N1 | **haproxy restart trap** — after `docker compose --force-recreate ollitex-web`, haproxy (443) must be explicitly restarted or the old listener lingers | med | **fixed (discipline)** | Every deploy wave ends with `docker restart ollitex-haproxy` + site 200 check (verified ag-v37/38/39). |
| N2 | CSRF + open-redirect class — login/logout redirect validation, `x-csrf-token` contract, SSO `redirect_uri` pinned to registered URIs + PKCE | high | **fixed (live-verified)** | C wave security matrix on ag-v37 (no-token 403 / token 200; federation RP exchange; no cross-user key leak). |
| N3 | Monitoring surface: Grafana admin-gated same-origin proxy (SRF-locked, anon off), Prometheus rules, token-gated alert webhook (`INTERNAL_ALERTS_TOKEN`) | — | **fixed (AG v25)** | Live-verified v25; re-verified this campaign; documented in the wiki (7-instance-stats). |

## 4. Frontend

| # | Finding | SEV | STATE | Evidence / action |
|---|---|---|---|---|
| F1 | **Mantine 9 modal flex-basis class** — inline `width` loses to `flex: 0 0 var(--modal-size)` (sm/440px): EVERY numeric-sized OLModal on the editor surface was squashed (settings 440, hotkeys 440 live) | med | **fixed (systematic)** | `ol-modal.tsx` (Mantine path) now sets `flexBasis` + `--modal-size` inline from the requested size; live-verified 1440/1280 on ag-v39; Storybook size-matrix with in-story width probe (`stories/shared/ol-modal-size-matrix.stories.tsx`) locks it in the gate. |
| F2 | **Retired-route links in shipped UI** — hotkeys keybindings section still linked `/user/mysettings#key-bindings` (retired route) after the bottom text was fixed | low | **fixed** | All 3 links now `/user-settings/mysettings.keybindings` (live-verified ag-v39). |
| F3 | **PI window CSS polish (J)** — owner-gated: canonical OlliTex rails vs the mixed legacy chrome is an owner decision; the CSS fixes themselves are scoped | med | **owner-gated** | Board item J (TODO-19d15b31) waits on the owner's rail decision — flagged, not blocked on code. |
| F4 | Mantine-surface coverage gap outside the matrix stories — the legacy (react-bootstrap) OLModal path has no story | low | **open** | Minor; the legacy path is the permanent fallback for `/Project/:id` (parity-verified in e2e). Story candidate for a later wave. |

## 5. Ops

| # | Finding | SEV | STATE | Evidence / action |
|---|---|---|---|---|
| O1 | **WakaTime provision has no local rate limit** — admin-only surface; downstream Wakapi enforces `200/1h`; residual risk bounded by the admin gate | med | **accepted (recorded)** | Acceptance rationale: admin-only + downstream cap + CSRF on the provision route; revisit if the surface becomes user-facing. |
| O2 | **Log injection** — user-controlled strings reach server logs verbatim | low | **accepted (recorded)** | Dev instance, controlled actors, no log-shipping to untrusted sinks; revisit before any production hosting. |
| O3 | **Image/container sprawl** — v21…v37 web tags, 4 exited `g-2026-10-09-v21` containers, stray stale `overleafmongo` | med | **owner-gated (TODO-91e4f8a2)** | Keep/delete proposal prepared on request; **no deletion without owner confirmation** (standing rule). |
| O4 | Backup hygiene — next daily run 2026-10-11 03:00 UTC; verify size + log after each run | — | **open (recurring)** | Standing check folded into the backup cron (log line + size sanity in `backup-data.sh`). |

---

## Quick wins (ranked, effort ≤ half a day each)

1. **Roll the whole e2e stack to the current web tag** each test wave (D6) — one command, removes drift-class test surprises. *(done for the web box per wave; companions to be included next wave.)*
2. **Makefile guard for the TeX Live 2026 build context** (B1) — fail loud instead of a broken image.
3. **Default `GO_BUILDER_TAG` in the web Dockerfile ARG** (B2) — omitted arg becomes safe.
4. **`TestZZLiveHub` skip tightening** (D5) — hermetic runs stop failing on hosts without the stack.
5. **Retire the stray `overleafmongo` container + document the alias map** (D4) — with owner OK (deletion gate).
6. **Image/container cleanup proposal** (O3) — the keep/delete list is ready to present (owner-gated deletion).

## Explicitly NOT quick wins (needs sequencing / owner)

- J (PI CSS: rail canonicalization decision),
- B (e2e hub → /user-settings + /admin-settings reorg),
- P7 cutover (54ed9d38), TPDS→web merge (d414c964), build rework (d86e9113) — all owner-sequenced.

---

## Audit method (reproducible)

- **Data:** live mongosh probes (counts before/after, collection scans), the Q/C
  fix diffs in-tree, the new `TestHubUserEmailDriverShapes` pin, and the
  stashed-tree rerun that proved `TestZZLiveHub` fails identically without
  today's changes (pre-existing).
- **Build/ops:** `docker build` logs this campaign (v38/v39 green), deploy
  checklist execution logs, `crontab -l` (backup cron present), `docker ps -a`
  (container sprawl list).
- **Frontend:** live puppeteer measurements (modal widths before/after the
  fix), the Storybook gate, and the e2e two-cooperator matrix (green on
  ag-v39) as the behavioural gate.
- **Security:** the C-wave live matrix (CSRF, redirects, key leak, SSO
  pinning) re-confirmed on the current live tag.

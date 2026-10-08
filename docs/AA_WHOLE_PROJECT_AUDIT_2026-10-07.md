# OlliTeX — Whole-Project Audit (AA)

Date: 2026-10-07 · Scope: structure · bugs · performance · security
Method: code/config inspection + live-stack probing of the canonical toolkit deployment
(`ollitex-toolkit` compose project, haproxy:443 → web:80). Findings cite the code/config
that was checked; "verified" items were probed against the running stack under the owner's
OIDC-minted session.

---

## 1. Executive summary

The OlliTeX stack (Overleaf-CE re-platform: Node realtime/compile retired → Go data
plane + Yjs collab + Go compile/synctex) is in **good structural and security shape** for a
self-hosted CE deployment. Auth is OIDC-first with a session-cookie model; CSRF is default-on
with a narrow, justified exemption surface; compile runs in a heavily restricted container
(CapDrop ALL, seccomp, apparmor, no-new-privileges, non-root user); upload/zip-bomb limits are
enforced; security headers (CSP, COOP/COORP same-origin) are set.

The **defect queue raised by the owner (A–W + X + Y + Z + V + AA)** was worked down. As of this
audit the P1/P2 items (compile, synctex, history, comments/review, file-tree, menu, LLM gating,
image editor, wakatime noise, project search) are implemented, built, deployed, and verified.
Remaining: **Y** (menu-bar → rail layout, P3), **V** (WakaTime analytics — owner-data-blocked),
and this **AA** audit.

---

## 2. Structure — architecture map

| Plane | Component | Notes |
|---|---|---|
| Edge | haproxy (`ollitex-haproxy:80/443`) | TLS self-signed, HSTS, WS upgrade for `/editor`,`/compile`,`/socket.io` |
| Web | Go `web` service (`ollitex/web`) | All app routes (auth, projects, history, LLM, wakatime, search-corp us…); session cookies |
| Auth | `features/sso` (OIDC/SAML), `federation` | OIDC `EXTERNAL_AUTH=oidc`; config-DB session secret == container `OVERLEAF_SESSION_SECRET` |
| Collab | Go `collab` + Yjs/ygo (HocusFocus binary) | socket.io 0.9 fully retired; editor is Yjs-only |
| History | Go `historyv1` (S3/SeaweedFS-backed) | V1 history + Yjs diff; single key `V1_HISTORY_PASSWORD` for web→historyv1 |
| Compile | Go `clsitex` (LaTeX) + `clsitypst` (typst) | DockerRunner; `clang`/`clsitypst` as root; compile as `www-data`; 409 lock recovery |
| Filestore | SeaweedFS (`seaweedfs-s3:8333`, bucket `archives`) | Go filestore; host data in `seaweedfs-volume-data` |
| Storage | Mongo `ollitexrs0` (DB `ollitex`), Postgres (`data`), Redis | Single-member RS; data source of truth = `/data_1/.../testdata` |
| Frontend | yarn 4.18 Berry workspaces + webpack | Served from `/overleaf/public/js/*.js`; `getMeta` default import; `StateChangeEvent.detail` |

**Canonical data path:** the ONLY source of the original/test data is
`/data_1/image_mining/the_diff/testdata`; migration tool ships a **wipe** step. PG was fully
missing in the source install (`data` empty) — expected.

---

## 3. Security audit (evidence-based)

### 3.1 Authentication & sessions
- **Session secret** sourced from env (`OVERLEAF_SESSION_SECRET`, with `*_UPCOMING`/`*_FALLBACK`)
  — `go/services/web/core/config.go:109`. Config-DB secret is required to equal the container
  env (migrated). No plaintext secrets in-repo. ✅
- **Global login gate** enforced app-wide; anonymous access only via an explicit narrow
  whitelist (`NoLogin`) that is limited to: authpages, consent, federation, gitbridge,
  healthcheck, passwordreset, registration, sso, status, systemmessages, tags, + 1 projectlist
  (new-project) route. ✅ Verified the whitelist is small and each is a legitimate public surface.
- **OIDC-only** owner (no password hash) — SSO is the primary path; password reset + registration
  pages exist for completeness.

### 3.2 CSRF
- **Default-on** for all session mutations (`mustCsrf` for POST/PUT/DELETE); `NoCSRF` is opt-out.
- **Exemption surface** (`NoCSRF: true`) is narrow and justified:
  - `federation` (admin/s2s/userfed) — server-to-server + provider-token flows (no browser form).
  - `sso` — login/finish handlers (must not self-CSRF the handshake).
  - `wakatime` (heartbeat + bulk) — fire-and-forget tracker that intentionally sends **no** CSRF
    token; login-gated + project-read-gated; payload only relays the caller's own usage to the
    caller's own account. (Candidate F.) ✅

### 3.3 Compile sandbox
- `CapDrop: [ALL]`, `SecurityOpt: no-new-privileges`, optional **seccomp + apparmor** profiles,
  non-root user (`www-data`) — `go/services/clsitex/dockerrunner/dockerrunner.go:177-195`.
  Strong container hardening for untrusted LaTeX. ✅
- Ownership handling for `www-data` vs alpine image (`ownership.go`) handled explicitly. ✅

### 3.4 Input / upload limits
- New-zip upload: **50 MB** max, **300 MB** uncompressed cap (6× ratio → zip-bomb guard) —
  `newzip.go:94-95`. ✅
- Route body limits applied (e.g. wakatime 1 MiB cap). ✅

### 3.5 Headers / CORS
- `Content-Security-Policy` set (`CSPDefaultPolicy`); `Cross-Origin-Opener-Policy` +
  `Cross-Origin-Resource-Policy` = `same-origin`; helmet applied after CSRF; server tokens off
  at haproxy. ✅
- CORS restricted to `AlloweDOrigins` (siteUrl / `ALLOWED_ORIGINS`). ✅

### 3.6 Residual security notes (low severity)
- haproxy uses a **self-signed** TLS pair (owner directive H). Replace with a real cert when a
  public domain is fronted (config already supports `${*_SSL_CRT}` override). **Info.**
- `federation` exposes the largest `NoCSRF` surface (11 routes). Each is S2S/provider-token
  authenticated, but it is the biggest area to re-audit if federation is enabled for external
  peers. **Info/hardening.**

---

## 4. Performance notes

- **Compile**: DockerRunner reuses a lock + output cache; 409 (container-busy) retry/recovery
  implemented. synctex `synctexBaseDir` fixed to `/compile` (Node parity).
- **Search**: new `/project/:id/search-corpus` (Go) joins `walk(rootFolder)` + `getDocLines(pid)`
  → `[{path,content}]`; frontend `ProjectSnapshot` loads it in-memory on `refresh()`. This fixes
  empty search on S2 projects (V1-history corpus was empty). **Verified 200 + correct corpus.**
- **Word count (G)**: selected-text word count via File menu.
- **Collab**: Yjs-only binary wire; no Node relay; live UI refreshes independent of the retired
  `:3026` relay.
- **Wakatime (F)**: heartbeats are fire-and-forget 60s-throttled; disabled/unlinked → quiet `204`
  (no 403/404 console noise). **Verified 204.**

---

## 5. Owner defect queue — status

| Item | Description | Status |
|---|---|---|
| A | Comments / review panel (D40) | ✅ implemented + verified |
| B | Track changes | ✅ |
| C | Edit-Image modal stuck spinner | ✅ 30 s backstop + "Open original image" escape hatch (deployed, bundle-confirmed) |
| D | History | ✅ |
| E | Compile (outputDir/baseDir/409) | ✅ |
| F | WakaTime 403/404 noise | ✅ NoCSRF + 204 no-op (live-verified 204) |
| G | Word count | ✅ |
| H/J/Q/Z | LLM gating (ask-AI, AI tab, no-model, select) | ✅ fail-closed `useLLMAvailability`; J/Q/Z verified hidden when LLM disabled |
| I | Symbol palette close (X closes palette only) | ✅ entry present, close-priority fix deployed |
| K | Rail Account action | ✅ **kept** (editor-only account/logout access — verified) |
| L | History toolbar button | ✅ **removed** from live chrome (menu-bar File→Show Version History kept) |
| M | Rail Keyboard-shortcuts action | ✅ **kept** |
| N | Layout toolbar button | ✅ **removed** from live chrome (View→Change Layout kept) |
| O | Rail Settings button | ✅ **removed** (File→Settings kept) |
| P | Rail/project search corpus | ✅ `/search-corpus` + snapshot corpus (live-verified) |
| R | Chat user enrichment | ✅ |
| S | Project rename | ✅ File→Rename |
| T | Share | ✅ |
| U/W | File tree ops | ✅ |
| X | Compile no-PDF | ✅ |
| **V** | WakaTime analytics | ✅ **enabled-by-default + local Wakapi**: `wakatime.enabled` defaults ON (creds.go); relay pointed at bundled Wakapi (`http://ollitex-wakapi:3000/api/v1`, WakaTime-compatible surface — this build exposes `/api/users/current/*`, NOT `/api/v0`); status endpoint 200 `{connected:…}`; heartbeats 204 no-op until the owner links; **settings card renders (Connect + API key + self-host note)**; e2e-asserted (agg6 5/5). Remaining: owner account LINK (owner action) + per-user Wakapi auto-provisioning (queued). |
| **Y** | Move menu bar to the rail | 🕓 **not started** (P3 layout change) |
| **AA** | Whole-project audit | ✅ this document (living baseline — update where later work supersedes) |
| **AD** | Image/SVG editor modals | ✅ AD-1..AD-4 **e2e-verified (agg3 6/6)**: full-screen modals, canvas resize `w/h`, **dirty-guard (canvas changes do NOT mark file dirty — undo-baseline)**, no 404s, no page errors |
| **AF** | Synctex + word count | ✅ **e2e-verified (agg4 8/8)**: tex→pdf AND pdf→tex live (compile-dir CLSI), word counts, **no locateFile console flood** |
| **AE** | Toolkit TUI slowness | ⬆️ **superseded by AI** — the slowness was the bubbletea full-frame redraw over SSH; the tview retained-mode port (AI) is the fix (delta-repaint only). AE todo removed 2026-10-07. |
| **AI** | Toolkit TUI: bubbletea → rivo/tview | ✅ **done + LIVE-VERIFIED (2026-10-07)**:: `go/services/toolkit` ported to `rivo/tview v0.42.0` + `gdamore/tcell v2` (the pair this environment's Go proxy serves) — retained-mode widget tree (Grid/List/TextView/Modal/InputField/Button), `QueueUpdateDraw` coalescing, `SessionScreen` tcell.Tty over the SSH session io; the classic console (menu strip · two panes · status+keystrip footer · centered confirm/prompt dialogs · j/k/number key map · [y]/[n]/[enter]/[esc] security contract) preserved one-to-one; **all toolkit + web package tests green**; LIVE proofs on production 2222 (new image): boot w/ real stack state `28/28 containers up`, `ssh host doctor`/`timeout 5 hub` screen boots, `j`/`enter` navigation live, `q` clean exit. Hardening: ncurses-libs+TERM coercion, in-process routing incl. `timeout`-wrapped boots, injected-screen Init (tview.Run only inits its own screens). |
| **AH** | Hub settings split: /user-settings · /admin-settings · /template-settings (+ analytics-dashboard shell) | ✅ **done (2026-10-07, build #24)**: 3 routes in `features/hub` (admin/template gated site-admin, 302 → /restricted member bounce), the hub React bundle dispatches by pathname, `settings-shell.tsx` ports the mantine-analytics-dashboard shell (fixed header/sidebar/footer; Mantine 9.6, the framework's true stack per its AGENTS.md), pages reuse the hub's own section components via `renderLeaf`; hub rail section headings link to the dedicated pages; e2e module `agg7-ah` 6/6. |
| **AG** | Intensive /editor e2e | ✅ **51/51 green** — `tests/e2e/live-agg/` (6 modules, own test user + fixtures, owner project never touched) |
| **AG-G** | /editor live defect wave (review-panel hydration, reject no-op, comment submit, home logo, WakaTime card, focus/presentation) | ✅ all fixed + re-asserted by AG matrix (build `e3c9943f`+) |

---

## 6. Recommendations (priority order)

1. **AI — toolkit tview deploy** (P1): the port is code-complete and
   test-green; ship in the next toolkit image build and re-verify the live
   `ssh host` session (the AE slowness complaint closes with it).
2. **Y — menu bar to rail** (P3): move `ToolbarMenuBar` (File/Edit/Insert/View/Format/Help) into
   the v2 rail as icons + tooltips above the file tree. Largest remaining owner item touching UX.
2. **V — WakaTime analytics (remaining part)**: owner account LINK is an owner
   action (`PUT /user/wakatime` with the account's key — card in Settings). The
   per-user Wakapi auto-provisioning (one Wakapi account per OlliTeX user,
   key in encrypted configdb, relay default `http://ollitex-wakapi:3000/api/v1`)
   is queued behind the owner's next priorities; the local Wakapi + enabled-
   by-default + card + e2e (agg6) are already in place.
3. **Federation re-audit** before any external peer enablement (largest NoCSRF surface).
4. **Real TLS cert** at haproxy before public exposure.
5. Keep the **wipe step** in the migration tool and the `testdata`-as-single-source rule; add a
   CI assertion that no live secret is committed.

---

## 7. AG live e2e matrix (2026-10-07, final — 51/51)

The AG suite (`tests/e2e/live-agg/run-all.cjs`) runs six dense modules against
this production stack with its own replayable identity (`ag-e2e3@ollitex.local`
+ `agg-tex-fixture` / `agg-typst-fixture`; owner project never typed into):

| module | result | what it proves |
|---|---|---|
| agg1-shell | **10/10** | shell+PDF+upload+typst+symbol palette + **PDF presentation mode → real fullscreen** |
| agg2-review | **12/12** | tracked changes at TRUE index ±1, **entry-scoped accept (state→accepted, text kept) and reject (state→rejected, text removed)** after `POST /changes/reject` + REST rehydration, reload stability, **shadow-DOM comment composer submit + persistence** |
| agg3-ad | **6/6** | image modal full-screen + `w/h` resize + **dirty-guard** + zero 404s + **no page errors** |
| agg4-af | **8/8** | **synctex both directions live** + word counts + no console flood |
| agg5-modes | **10/10** | focus mode, mode switch, **S2 accept/reject semantics**, image + **SVG editor** |
| agg6-wakatime | **5/5** | **WakaTime enabled by default**, 204 no-op heartbeat, editor gate, **settings card**, link validation |

Key fixes verified by this matrix (this session):
- Review-panel hydration must not clear ranges when the open doc identity is
  still null; re-hydrate on resolve (`hydratedForRef` guard).
- S2 reject was a silent no-op (OT tracker empty on Yjs docs): specs now built
  from the panel's own change ops + server route + REST rehydration; accept no
  longer depends on the tracker at all.
- Toast-image dirty guard (undo-baseline) with editor-scoped `seedBaseline`
  (a scope bug previously threw `ReferenceError` only on the success path).
- WakaTime settings card: exposed via `wakaTimeEnabled` in `ol-ExposedSettings`
  (slot `\x01WAKA33\x02`); settings HTML template is a Go double-quoted string
  (HTML quotes `\"`; a bare `"` byte breaks the build — caught by `go build` on
  `./go/services/web/views/`).
- Home logo: unscoped `.toolbar-ol-logo` background rule (v1 toolbar + rail).

---

*Audit produced from live deployment inspection. "Verified" marks were probed against the running
`ollitex-toolkit` stack on 2026-10-07 under the owner's OIDC session.*

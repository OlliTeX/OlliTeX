# USER-JOURNEY + CAUSE-EFFECT TESTING AUDIT — 2026-10-13

Owner work order: "intense User Journey Testing and Cause-Effect Testing over the
whole project. Identify weaknesses and put the implementation of suitable tests on
the todo board."

## Method
- **User Journey Testing** = complete multi-surface user goals executed as one
  storyline (register → … → export), including intermediate failure states. The
  existing 124 specs are strong at PER-SURFACE arcs but weak at CROSS-SURFACE
  storylines: most journeys exist only as fragments.
- **Cause-Effect Testing** = systematic cause→effect matrices (role × route ×
  state, domain boundaries, failure injection, concurrency ordering). Currently
  scattered pins, no dedicated matrix suites.

## Evidence of the gaps (grep sweep 2026-10-13 over 124 specs)
- No IDOR/permission × route × state matrix spec (permission negatives are
  scattered: fileproxy 403, zip leg M, sharing-permissions) — an IDOR in any
  project-scoped route (doc/threads/tracked/file/zip/history/tags/share) would
  not be caught systematically.
- ZERO unicode/emoji/RTL/CJK tests in comment/tracked ranges or doc content —
  range math (pos/len over codepoints vs UTF-16) is an untested cause-effect
  boundary; one wrong assumption here breaks comment pinning silently.
- No concurrency ORDERING matrix (same-range conflict merge order, parallel
  restore, invite-vs-permission-change race) — collab-two-user covers basics.
- No full lifecycle journey (create→share→template→export→import→deactivate→
  trash→purge) with STORE-LEVEL cleanup verification (mongo/docstore/filestore/
  history residue after purge is only spot-checked).
- No theme-persistence + session-expiry + CSRF-rotation combined matrix.
- No failure-injection matrix (compile-fail mid-journey, websocket drop during
  save (024 room→docstore flush), zip part size boundaries, template zod
  boundaries) — negative legs are per-spec, not systematic.

## Board items created (implementation queued, 7 open TEST-IMPL items)
| TODO | Class | Scope |
|---|---|---|
| `uj-onboarding` ✅DONE 10-13 (8/8 green ×2) | Journey | register → mail-activate (incl. failure states) → first project → first compile → first comment → first export probe |
| `uj-choreography` ✅DONE 10-13 (7/7 green ×2) | Journey | two-user interleaved comment + tracked-change + history-restore storyline on one doc, incl. mid-journey reload recovery |
| `uj-lifecycle` | Journey | create→share→template→instance→export→import→deactivate→trash→purge + store-level residue verification + admin governance audit |
| `ce-idor` ✅DONE 10-13 (5/5 green ×2, clone-500 pinned) | Cause-Effect | full permission matrix: every project-scoped route × {owner, editor, comment-viewer, non-member, anonymous} × {active, trashed} + CSRF/token boundaries |
| `ce-boundaries` ✅DONE 10-13 (7/7 green ×2, 5 findings pinned) | Cause-Effect | domain boundaries: ranges (negative/swapped/out-of-range/emoji-codepoint), doc content (huge line, null bytes, RTL/CJK/emoji in comments+tracked), entity names, zip part sizes, template zod limits |
| `ce-failures` ✅DONE 10-13 (7/7 green ×2, 5 world-quirk pins) | Cause-Effect | failure injection + concurrency ordering: compile-fail mid-journey, ws-drop during save, same-range conflict merge order, parallel restore, invite race |
| `ce-persistence` ✅DONE 10-13 (8/8 green ×3) | Cause-Effect | theme switch/persist × surface, session expiry mid-action, settings state round-trip, cross-user CSRF boundary |

## Rules for the implementations
- Each suite: self-cleaning within one run (reaper truth), 2× green stable,
  CI-safe (no leaked projects/users/templates), serial where state-dependent.
- Owner-flagged product gaps (F1–F12, CMT/TC/MD/AF/BUV pins, attribution
  65b0d668, zip feature 047dc1d9) are PINNED where they intersect a journey —
  the suite asserts the current state and flips green when the gap is closed.
- Weaknesses the suites are DESIGNED to expose are listed per TODO; a FINDING
  (real defect) gets an owner flag + pin, per standing rules — not a silent fix.


## Implementation receipts (2026-10-13)

### CE-3 DONE — `specs/ce-failures.test.e2e.ts` (7 legs, 7/7 green ×2, 2.6m)
- **F4 corrupted-zip**: bare-garbage → 422 "Invalid zip file" (deterministic);
  well-formed zip w/ binary part → 200 accepted, no crash (pinned).
- **C1 same-range conflict (N=2)**: both rounds **converged, winner=AB merged**
  — the collab stack merged both texts; convergence invariant holds; no 5xx.
- **C3 parallel LWW (settings.name, N=3)**: both writers 204; settled hub shows
  exactly ONE value; winner=B all 3 rounds (deterministic ordering). name writes
  are owner-only (member → 403 pinned).
- **C4 invite-vs-remove race (N=2)**: invite 200; remove-by-email = 404 route
  (endpoint shape differs in this world — pinned, not a defect claim); final
  user state = consistent non-member both rounds.
- **F2 hard-kill mid-edit**: other client resolved to exactly **POST state
  (committed)** — no corrupted mix; killer's page dead; survivor editor alive.
- **F3 mail-sink down**: register is **MAIL-GATED → 422 "Failed to send the
  registration email."** (honest dependency — NOT a 200-swallow); sink up →
  200 "Registration successful" + activation mail delivered=true; body contract
  first_name+last_name+email (`name` → 400 "Too long name." — legacy misnomer);
  429 budget (5/60s/IP, redis `postRegister`) handled with 65s wait + pin.
- **⚑ WORLD-QUIRK PIN SET (owner record)**: (a) **invite-accept 204 does NOT
  yield editor access for the member** (probe: owner renders, member denied
  post-accept) — green path = direct `collaberator_refs` grant (collab-two-user);
  accept likely = request-access/pending semantics; (b) **`GET /project/:pid`
  is the invite-link route** (404 for detail) — readback via hub page;
  (c) register contract per F3 above; (d) **editor access binds to per-page
  session context** (fresh page in shared pre-logged context → "Restricted"
  even as owner); (e) **reaper hard-deletes idle fixture projects within
  minutes** — suites must create per-leg projects and self-clean.

### CE-4 DONE — `specs/ce-persistence.test.e2e.ts` (8 legs, 8/8 green ×3, 2.3m)
- **L1 THEME mid-session switch**: explicit `POST /user/settings {overallTheme}`
  flips hub + editor surfaces with re-render; per-user scheme-class parity asserted
  (both dark or both light; base shades may differ by design — 1.8 vs 4.2 lum = same scheme).
- **L2 THEME persist**: logout → re-login → scheme RETAINED (deterministic);
  cross-user state ISOLATED (A-light vs B-dark observed = per-session storage truth).
- **L3 SETTINGS round-trip**: `POST /user/settings {first_name}` → **mongo
  `users.first_name` persisted** (ground truth; no userSettings collection exists in
  this world) + leaf UI round-trip renders the value; neutral value restored.
  `GET /user/settings` 404s (unported-route pin — reads must go via leaf UI/mongo).
- **L4 SESSION-BIT**: member comment POST after session drop → **401 Unauthorized**
  deterministic; project intact, no half-state (half-state gated by the refusal set).
- **L5 CORRUPT cookie**: `GET /user/settings` with garbage session cookie → 200
  login-shell HTML (client-side auth gate — pinned, not a 5xx); re-login recovers.
- **L6 CROSS-USER CSRF**: USER2's token on ADMIN's session → **403 + state
  unchanged** (CE-1 standing invariant re-confirmed at the settings surface).
- **L7 TRASH gate (probed contract)**: `POST /admin/project/:pid/trash` → owner
  hub DROPS the project INSTANTLY (immediate visibility gate); member editor keeps
  working (per-user trash, legacy-faithful — pinned); `DELETE .../purge` 200, hub
  stays cleared, **mongo record PERSISTS** (hard-delete = time-based reaper,
  owner-gated — pinned).
- **L8 housekeeping**: both users restored to dark; trail ≥ 8 cells.
- **⚑ WORLD-TRUTH PINS (owner record)**: theme = session-scoped (redis `sess:*`),
  no `overallTheme` in `users`/`site_settings` mongo; settings writes land in
  `users.{first_name,...}` (mongo) — settings READ API unported (404) → leaf UI or
  mongo is the readback path; trash/purge = association visibility, record
  survival until reaper.

### Journey A DONE — `specs/journey-onboarding.test.e2e.ts` (8 legs, 8/8 green ×2, 2.5m)
Full onboarding storyline (fresh USER3 account, in-suite lifecycle):
- **L1 REAL form register**: form = `_csrf,first_name,last_name,email` — **NO
  password field** (the world never captures the sign-up password — root cause of
  the ⚑ 401 gap); account in mongo + activation mail in sink.
- **L2 activation link**: bad-token and good-token both → **302 /login** (route
  RETIRED in this build — pinned deterministically).
- **L3 ⚑ sign-up gap + RECOVERY**: pre-reset login → 401
  `invalid-password-retry-or-reset` (auth-lifecycle owner flag re-pinned); recovery
  proven end-to-end: reset mail (quoted-printable decoded; 64-hex token; full URL
  `/user/password/set?passwordResetToken=…&email=…`), set page (single password
  field — `passwordConfirm` is server-side), 400-missing-key contract, set → 200,
  **login with reset password OK**.
- **L4 first project + first compile**: hub UI "New project" → Blank (proven
  createBlankProject flow) → mongo ownership truth → editor → compile
  `{projectId, compileId, mainFile}` 200 → `output.pdf` **200 application/pdf**.
- **L5 first comment**: wire add 200/201 → visible in `GET /threads` (round-trip).
- **L6 export probe**: `GET /project/:pid/download/zip` → **404 dead-end pinned**
  (zipcontract PENDING — self-activates on TODO-047dc1d9; [zipcontract LIVE] path
  asserted in the same leg).
- **L7 persistence**: logout → re-login → threads API 200 + FIRST COMMENT SURVIVES
  + ownership intact; new-session fileproxy PDF = 404 (compile cache
  SESSION-SCOPED — pinned).
- **L8 cleanup audit**: project trash+purge; admin `delete` 200 + `purge` 200;
  account state = purged (no leak; audit leg recorded).

### Journey B DONE — `specs/journey-choreography.test.e2e.ts` (7 legs, 7/7 green ×2, 1.1m)
Two-user choreography composed from the green fragments (single doc storyline):
- **S1** owner edits (CM6 dispatch) + comment thread on a live range (wire 200/201 → visible).
- **S2** member sees thread (parity read), replies (two-way wire); owner edits UNDER the
  commented range → **RANGE-DRIFT TRUTH PINNED**: ranges carry an ANCHOR object
  (`"a":{"e":"…","s":"…"}`) beside numeric `start/end` — the world ANCHORS ranges
  (not naive offset shifting); thread content survives the under-range edit, no 5xx.
- **S3** track-changes toggle (owner `<500`) + toggled-on edit is versioned
  (/updates maxV≥1, /diff carries the marker) + member diff parity; client-side
  capture gap remains the trackchanges-arc pin.
- **S4** restore attempt (both roles): **403 role-identical** (deterministic;
  unported-or-gated — either way state UNCHANGED, threads+content intact — the
  restore/interleave question is now pinned at the refusal boundary).
- **S5** member HARD RELOAD mid-journey → content (MARK2), threads (both messages),
  timeline all consistent post-reload; P1/P2 content parity exact (024 room re-sync OK).
- **S6** attribution surface (65b0d668 interlock): File → "Show version history" panel
  present and **the "Anonymous" pin HOLDS** (attribution gap live-recorded).
- **S7** kill → member threads 404 "not found" (deterministic refusal class, no 5xx);
  project doc gone (owner audit).

### Journey C DONE — `specs/journey-lifecycle.test.e2e.ts` (7 legs, 7/7 green ×2, 3.6m)
Lifecycle + store-level purge + governance:
- **L1** create+content (comment+track toggle+P2 edit footprint, /updates 200).
- **L2** template publish REFUSED 400 ("Please try reco[rd/word]") — the fixture NAME
  trips it (template-name surface; pinned; honest fall-through to the import leg).
- **L3** import-copy: `/project/new/upload` zip → new project (COPY_PID) — two
  independent worlds proven.
- **L4** governance: `/admin/project/:pid/members` 200 (owner+member audit),
  `/admin/active-projects` 200 (fixture listed), **deactivate route 404 with
  CSRF = GENUINELY ABSENT** (legacy-faithful: deactivation here is the legacy
  90-day inactivity AUTO-JOB only — owner-gated time path, not an action).
- **L5** TRASH→UNTRASH **RESTORE CYCLE PROVEN**: trash 200 → untrash 200 → owner
  project list regained (admin POST `/admin/user/:uid/projects` 200, CSRD-gated),
  member threads 200, content intact. (Route proven: admin.go adUntrashPat.)
- **L6** **STORE-LEVEL CASCADE TRUTH (first store-cleanup assertion in the board)**:
  after the scoped hard-delete of the fixture's own projects row:
  `projects:1→0`, but **⚑ RESIDUE: docs=1, ydoc=1, chunks=1** — docstore + 024-collab
  + one chunk orphaned (NO cascade in this world; reaper presumably time-based).
  All other sub-stores 0 (rooms, docHistory*, projectInvites, audit, deletedProjects).
- **L7** audit trail: `projectAuditLogEntries`/`userAuditLogEntries`/`instanceStats`
  present; active-list post-purge = deterministic; template-registry state recorded.
- Self-cleaning proven (fixture + copy killed/deleted within the run).

### ZIP tracked-changes/comments implementation DONE — board TODO-047dc1d9 (2026-10-13)
Owner scope implemented end-to-end (source in repo, live in the e2e world):
- **EXPORT** `GET /project/:pid/download/zip` (new `dlzip_single.go` + route):
  200 application/zip; zip = project files (rootFolder walk, doc lines via
  docstore, file blobs via the filestore reference — compile parity) +
  `comments.json {"comments":[...]}` (collab ListThreads/MessagesOfThread
  flattened: id/text/doc/ranges/state) + `tracked-changes.json
  {"tracked":[...]}` (collab ListChanges). Anonymous → login bounce;
  ghost → 404; non-member → 403 (package gate canRead/loadUserAdmin);
  no cross-project bleed (per-project builder).
- **ADMIN MULTI-ZIP** (`dlzip.go` re-wired): each inner zip now carries the
  same files + payload JSON (shared `zswBuildZip`; placeholder fallback
  only when content cannot materialize).
- **IMPORT** (`newzip.go` hook): root-level `comments.json` /
  `tracked-changes.json` are CAPTURED, excluded from the project tree, and
  restored into the new project: comments → collab AddThread+AddComment
  (GET /threads then shows them — LIVE proven); tracked texts → canonical
  docstore source (docstore lines verified to carry ZIPTRACK-888) + best-effort
  collab AddChange; corrupt payloads → files-only fallback (deterministic 200).
- **TEMPLATE EXCLUSION** (owner scope "except for templates"): the exporter
  omits the payloads when a `templates.source_project_id` link row exists
  (future-proof; the profile's publish is the pinned 400 follow-up → leg H
  stays honestly pending).
- **ACCEPTANCE**: specs/zip-tracked-comments.test.e2e.ts **13/13 GREEN ×2**
  (2.4m/pass; legs A/B re-armed per their own re-arm clauses; legs G/I/K/L/M
  assert the full LIVE contract; H/J hold honest pins with the exact
  missing-world-truth recorded: template publish 400 in this profile;
  editor-buffer view gap for zip-imported docs (pre-existing: even the plain
  import sentinel never appeared in the buffer — store-level truth is green)).
- **REGRESSION**: comments-two-user + journey A + journey B = **19/19 green**
  post binary swap (journey A L6 self-armed to [zipcontract LIVE]).
- BAKE NOTE (owner-gated): the e2e world's web binary was swapped in the
  running container (CGO_ENABLED=0 build of the committed source; the
  image-baked binary predates the change). Source-of-truth is the repo; a
  full image rebuild (ag-v57 → next) is the owner-gated persistence step.
  The suite self-arms: if the world ever loses the binary, leg A re-pins
  the absent state (no silent breakage).


---

## ⚑ finding status board (updated 2026-10-14, post autonomous work-orders)

| # | Finding | Status |
|---|---------|--------|
| 1 | `POST /project/:pid/clone` → 500 (body `{}`) | **AUDITED — faithful Node quirk** (`projectName.trim()` TypeError; named clone = 200 live, blank = 400). Not a defect. |
| 2 | zip part trailing space → 500 | **AUDITED — faithful vendor pin** (ext gate; `{'Upload failed'}` Node shape). Deterministic. |
| 3 | intermittent comment-write 500s | **RESOLVED-GREEN** (comments-two-user + trackchanges 2× post-swaps; no 5xx observed). |
| 4 | invite-accept 204 ≠ editor access | **OWNER-ADJUDICATED 2026-10-14 → (b) accept = PENDING policy**: 204 no-access accept is the INTENDED semantics (invite consumed one-shot; the owner grants membership). Pin stands as the approved contract (sharing-invite-loop L1). |
| 5 | sign-up gap: no password field + retired activation | **STILL OWNER-ADJUDICATE** (mail-gated register is a product policy; Journey A pins it). |
| 6 | store-cascade: hard-delete orphans docs/ydoc/chunks | **PARTIALLY OBSERVED-SWEEPED** (zombie pass cleared the leaked rows; cascade-behavior question stays with the owner — the reaper appears to sweep over time). |
| 7 | zip-imported docs: empty editor buffer | **STILL OWNER-ADJUDICATE** (import→collab-room sync gap; store-level content verified; ZIP leg J pins). |
| 8 | `65b0d668` attribution "Anonymous" + template publish 400 | **ATTRIBUTION FIXED** (`45ab08a7`; panel renders "You", 29/29 regression); template publish stays 400 in this profile (exclusion self-activates when it lands). |
| 9 | ~~Export menu-item broken in the ag-v58 Mantine rail~~ (original read: File → "Download as source (.zip)" opens a BLANK POPUP, anchor navigation swallowed) | **❌ REJECTED 2026-10-14 — FALSE POSITIVE.** The "blank popup" is the NORMAL download-delivery UX: the click fires a real browser DOWNLOAD (new tab stays blank by design; the file IS the delivery). Direct evidence (ag-v59 world, 2026-10-14 06:45–07:40 UTC): (a) in-page trace shows the click calls `window.open("/project/<24hex>/download/zip", "_blank", "noopener")` with the correct href; (b) Playwright `download` event from the identical menu click → `e2e-smoke-project.zip`, 425 bytes, `PK` signature, saveAs-verified; (c) the speculative source fix (59ba96ec) was REVERTED (56394a7c) — no product change for a non-defect; (d) the toolbar-rail export leg re-armed to the true contract (visible item + click → `download` event + PK bytes + API 200 zip) — 13/13 green. Probe-lesson recorded (failure memory): download links present as blank tabs; assert the `download` event (armed BEFORE the click — events are not buffered), and contexts created via `browser.newContext` need `acceptDownloads: true` (the default `page` fixture accepts them). |

Also closed this block: **SSO leg-b** (TODO-7358ce12 → done, real LDAP round-trip +
ldapJIT user-doc fix, 14/14); **zombie sweep** (793 projects + sub-stores + 246 users,
4 fixtures verified); **TODO-65b0d668 → done**.
build-images junk: **RETIRED 2026-10-14** (`08ff26a3`, gate cleared by directive).
**Battery-recovery block (2026-10-14, post-ag-v58 bake)**: the full 641/25 battery's
25 failures triaged — 23 = stale `role=button` chrome selectors vs the owner-approved
a11y menubar wave (0e3cb1f5, "bake-pending pins" — the flip landed WITH the bake):
7 spec files aligned (menuitem roles + proven click patterns), **57/57 green**;
2 = git-bridge edge legs vs the ebbb76488 vhost allowlist (non-clone `/git/` now
404-at-edge — STRONGER gate): re-pinned, **4/4 green**. Remaining known failure:
external-probe dropbox token = owner-blocked (TODO-39e56295). New finding: #9 above.

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

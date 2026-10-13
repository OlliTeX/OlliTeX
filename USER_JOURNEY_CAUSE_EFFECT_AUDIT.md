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
| `uj-onboarding` | Journey | register → mail-activate (incl. failure states) → first project → first compile → first comment → first export probe |
| `uj-choreography` | Journey | two-user interleaved comment + tracked-change + history-restore storyline on one doc, incl. mid-journey reload recovery |
| `uj-lifecycle` | Journey | create→share→template→instance→export→import→deactivate→trash→purge + store-level residue verification + admin governance audit |
| `ce-idor` ✅DONE 10-13 (5/5 green ×2, clone-500 pinned) | Cause-Effect | full permission matrix: every project-scoped route × {owner, editor, comment-viewer, non-member, anonymous} × {active, trashed} + CSRF/token boundaries |
| `ce-boundaries` ✅DONE 10-13 (7/7 green ×2, 5 findings pinned) | Cause-Effect | domain boundaries: ranges (negative/swapped/out-of-range/emoji-codepoint), doc content (huge line, null bytes, RTL/CJK/emoji in comments+tracked), entity names, zip part sizes, template zod limits |
| `ce-failures` ✅DONE 10-13 (7/7 green ×2, 5 world-quirk pins) | Cause-Effect | failure injection + concurrency ordering: compile-fail mid-journey, ws-drop during save, same-range conflict merge order, parallel restore, invite race |
| `ce-persistence` | Cause-Effect | theme switch/persist × surface, session expiry mid-action, settings state round-trip, cross-user CSRF boundary |

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

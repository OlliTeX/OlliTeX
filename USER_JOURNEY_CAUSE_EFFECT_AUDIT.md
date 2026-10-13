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
| `ce-idor` | Cause-Effect | full permission matrix: every project-scoped route × {owner, editor, comment-viewer, non-member, anonymous} × {active, trashed} + CSRF/token boundaries |
| `ce-boundaries` | Cause-Effect | domain boundaries: ranges (negative/swapped/out-of-range/emoji-codepoint), doc content (huge line, null bytes, RTL/CJK/emoji in comments+tracked), entity names, zip part sizes, template zod limits |
| `ce-failures` | Cause-Effect | failure injection + concurrency ordering: compile-fail mid-journey, ws-drop during save, same-range conflict merge order, parallel restore, invite race |
| `ce-persistence` | Cause-Effect | theme switch/persist × surface, session expiry mid-action, settings state round-trip, cross-user CSRF boundary |

## Rules for the implementations
- Each suite: self-cleaning within one run (reaper truth), 2× green stable,
  CI-safe (no leaked projects/users/templates), serial where state-dependent.
- Owner-flagged product gaps (F1–F12, CMT/TC/MD/AF/BUV pins, attribution
  65b0d668, zip feature 047dc1d9) are PINNED where they intersect a journey —
  the suite asserts the current state and flips green when the gap is closed.
- Weaknesses the suites are DESIGNED to expose are listed per TODO; a FINDING
  (real defect) gets an owner flag + pin, per standing rules — not a silent fix.

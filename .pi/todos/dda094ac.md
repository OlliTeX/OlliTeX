{
  "id": "dda094ac",
  "title": "006: Git Provider sync with owner test accounts",
  "tags": [
    "live-audit",
    "psintern",
    "gitbridge",
    "owner-handoff"
  ],
  "status": "completed",
  "created_at": 1790815326476,
  "last_change": 1790974772787,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## SUPERSEDED (reconciled 2026-10-02)

Duplicate of TODO-1e567ac0 ("006: Git Provider sync (gittest26-itp + PAT from
test-accounts.md)"), which is **COMPLETED** (2026-10-01, commit 0661e6c254):
full E2E green on Gitea with the owner test account (link PAT → test server →
orgs/repos → export (byte-exact binary round-trip) → import (fresh project) →
state/merge/unlink), incl. 7 found-and-fixed bugs.

Evidence: TODO-1e567ac0 body (11-step chain + byte-level integrity + bug list).
This row is closed as the earlier, superseded duplicate of that same 006
owner-handoff; all remaining 006-adjacent work lives under TODO-d414c964
(TPDS→web merge: github leg CLOSED 2026-10-02 commit 3cb1142199 + 14/14
forgejo fixture green; linked-url-proxy + bake-verification legs remain).

{
  "id": "edac5d9d",
  "title": "Check upstream impact: suspended user's git-bridge PATs still clone/push/pull (web blocks them) — 0 \"suspended\" hits in our git-bridge → likely gap; fix + round-trip test",
  "tags": [
    "git-bridge",
    "pat",
    "suspended-user",
    "authentication",
    "security",
    "upstream-impact",
    "access-control"
  ],
  "status": "closed",
  "created_at": 1791687443307,
  "last_change": 1791730756585,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

## CLOSED 2026-10-11 — fixed + pinned green

- Gap confirmed: our git-bridge had 0 "suspended" checks (web blocked suspended users, but the bridge would still serve their PATs).
- Fix: `6096fa82` git-bridge: reject PATs of suspended users (auth-leg rejection, row stays intact).
- Pinned live: `tests/e2e/specs/git-bridge-suspended-token.test.e2e.ts` (wave spec #7) — 3/3 GREEN: healthy PAT accepted (token-info 200 + bridge non-401) → suspend → token-info 401 + bridge 401 auth-reject (row intact) → unsuspend → token-info 200 + bridge accepts again.
- Final integrated run: 16/16 (/tmp/wave-all.log, ag-v45)
- NOTE: repo materialisation for suspended users (bridge 403) is the SEPARATE stack-wide TODO-ff7d6b1a flush-chain gap, not this auth gap.


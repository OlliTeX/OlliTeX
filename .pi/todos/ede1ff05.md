{
  "id": "ede1ff05",
  "title": "LIVE-028 FIXED: clone project 404 (route case bug, clone 200 + content verified)",
  "tags": [
    "done",
    "live-audit",
    "psintern"
  ],
  "status": "done",
  "created_at": 1790773281432,
  "last_change": 1790779457861,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

**DONE + verified (2026-09-30).** Root cause: clone route `/Project/:id/clone` was case-pinned to Node's capital-P constant, but the modern frontend calls lowercase `/project/<id>/clone` (Express is case-insensitive) → Go 404. Fixed with the batch (?i) route patterns (LIVE-017 cluster, commit 94c2f54d5a). Verified live: POST /project/<pid>/clone {projectName} → **200**, clone project created with content copied (entities: main.tex doc present). Note: body WITHOUT projectName → 500 error page is the PINNED Node behavior (ProjectDuplicator TypeError) — a probe without body must not be read as a failure. Test projects cleaned from mongo.

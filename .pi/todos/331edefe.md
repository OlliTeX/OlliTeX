{
  "id": "331edefe",
  "title": "P7 next = formal U3: per-project LLM reads (llm/models, llm/compile-fix, llm/source-context) — oracle → Go → gate → commit; then U4-U9 as listed in body",
  "tags": [
    "web-go",
    "p7",
    "u3"
  ],
  "status": "done",
  "created_at": 1790084239746,
  "last_change": 1790665067681,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

GOAL-ORDER RECONCILED (2026-09-22): the formal objective list's U2 = project state + doc reads — that is ALREADY covered by U1 (POST /api/project), projectlist/docapi.go (doc family) + today's editor-entry slice (896e23dd3c). NEXT UNIT = formal U3: per-project LLM reads — router paths to confirm (grep `llm` in services/web/app/src/router.mjs): llm/models, llm/compile-fix, llm/source-context (+ any siblings). Flow as always: live Node oracle (dual-port, redirect:'manual') → Go feature + unit pins → parity gate → commit. After U3: U4 github-sync, U5 git integration, U6 track-changes/threads/sharing-updates, U7 webdav state, U8 user gaps, U9 hub shell (/hub,/admin,/admin-hub,/home) + template preview + notifications prefs + format-tex + token pages; THEN hard-cutover (runit→Go, image rebuild, full e2e, live stacks).

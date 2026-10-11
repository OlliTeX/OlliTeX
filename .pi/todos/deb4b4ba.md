{
  "id": "deb4b4ba",
  "title": "b63post-2 InactiveData — deactivateOldProjects/deactivateProject (Go conversion: admin route + async worker)",
  "tags": [
    "b63post-2",
    "inactive-data",
    "dormant",
    "owner-queue"
  ],
  "status": "pending",
  "created_at": 1790889804085,
  "last_change": 1790889804085,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

6.3.0_post gap (b63post family). Convert InactiveData (dormant/inactive projects) to Go (NOT settled, NOT in flip list).

## Node oracle (6.3.0_post)
- services/web/app/src/Features/InactiveData/
  - InactiveProjectController.deactivateOldProjects (admin/bulk: deactivate projects older than N days) — wired router.mjs:995
  - InactiveProjectController.deactivateProject (per-project) — router.mjs:1000
  - InactiveProjectManager (findInactiveProjects(limit, daysOld) etc.), 202 LOC
- async/maintenance surface (cron-driven), not a simple web route

## Go state: ABSENT (no dormant/inactive-data refs in go/services|cmd)

## Scope / decisions
- Pin the exact endpoints + semantics (who can call, what 'deactivate' does to the project/queue) from Node
- Decide delivery shape: admin route (flip) AND/OR the async deactivation worker (Go cron, like cronmail)
- Convert: Go feature + worker + config knob (daysOld/limit) + flip conf + go test + live smoke (create project, age it, deactivate, verify state)
## Acceptance: byte-parity (admin route) + deactivation worker green-slice

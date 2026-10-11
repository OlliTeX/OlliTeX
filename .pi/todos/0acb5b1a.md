{
  "id": "0acb5b1a",
  "title": "AJ-3: fix broken admin sections (Active projects 500, Instance statistics/Grafana, Overview & activity)",
  "tags": [
    "aj",
    "admin",
    "backend",
    "grafana",
    "bug"
  ],
  "status": "done",
  "created_at": 1791420882276,
  "last_change": 1791507168815,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

Owner feedback 2026-10 (AJ): three BROKEN sections on /admin-settings:
1) "Active projects: Failed to load active projects — Internal Server Error" — real 500, find the failing endpoint (server log in ollitex-web), fix handler (likely go/services/web/features/projectlist/admin.go or the hub section fetch), verify live.
2) "Instance statistics" — "This looks broken and not the grafana system that it should be." — the section must surface the Grafana dashboard (grafana container is in the stack; deep-link/embed) instead of a broken card. Check instancestats feature (go/services/web/features/instancestats/*) + the hub section it renders.
3) "Overview & activity" — "This is broken." — identify the failing data source, fix or replace with working content.

Each: reproduce live as admin, fix backend or frontend, live-verify, e2e covers the fixed state.
DONE-CRITERIA: no "Failed to load"/500 on those sections; Instance statistics = Grafana; e2e assertions.

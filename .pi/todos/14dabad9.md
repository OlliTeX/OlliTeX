{
  "id": "14dabad9",
  "title": "AJ-7: retire /hub \"My settings\" + /hub \"Site settings\" once the settings pages are ready",
  "tags": [
    "aj",
    "hub",
    "retire",
    "sequencing"
  ],
  "status": "done",
  "created_at": 1791420923671,
  "last_change": 1791506969718,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

Owner feedback 2026-10 (AJ): once the /user-settings and /admin-settings pages are READY:
1) RETIRE the "My settings" accordion surface in /hub (the mysettings.* tree incl. My LLM settings — the owner pasted the full button tree) — because /user-settings replaces it.
2) RETIRE the "Site settings" navigation surface in /hub (Overview & activity aria-current button + the whole Site settings tree: Full site settings, Hub health, General/Content/Projects/Users/Diagnostics, Integrations, Services, Storage, Compilation, LLM Settings) — because /admin-settings replaces it.

Sequencing: AFTER AJ-1 (individual pages) + AJ-3 (broken sections fixed) + AJ-4 (env-fixed fields removed) so nothing needed is lost; then remove from nav-tree.ts /rail.tsx + sections, update e2e (agg4/agg7 hub-nav expectations), docs.
DONE-CRITERIA: /hub no longer lists My settings or Site settings; every capability reachable at /user-settings or /admin-settings; e2e green.

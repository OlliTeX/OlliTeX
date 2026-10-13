# Probe plan — owner e2e TODOs (run AFTER battery #3 finishes; workers=1 shared stack)

All probes: login-first, one context per actor, `waitForTimeout` headroom, cleanup (kill projects, delete users) in afterAll.
Pattern: probe file `_tmp-*.test.e2e.ts` → read the CH2-style console output → delete probe → write spec with re-arm guards → run spec green → commit.

## TODO-d07e1c99 — bib visual editor under /editor (Zotero/ORCID/Mendeley)
**Source map (verified):**
- `frontend/modules/bib-editor/frontend/js/extensions/bib-editor-extension.ts` — activates when the focused doc is `.bib`; event protocol `bib-editor:*` (entries-updated / write / delete / import / write-failed / undo / redo / history-state); guarded writes + bounded history.
- `bib-editor-panel.tsx` — toolbar: **search + Add (dropdown) + bulk Delete**; entry list; preview (flush-on-leave, NO Save button); delete confirm; bulk-delete guard.
- **Add dropdown items** (live-verify order + exact labels): `Enter manually` (bib-manual-modal "Add reference" OLModal), `Import from Library` (bib-import-from-library), `Import from ORCID.org` (orcid-picker modal), `Import from Zotero` (zotero-picker-modal), `Upload .bib file` (hidden file input → BibImportModal).
- Components: bib-entry-form/preview/list/manual-modal/import-modal/import-from-library.

**Probe 1 (structure):** create blank project → file tree New file `refs.bib` → OPEN it (double-click per standing fact) → wait → dump: panel presence (`[class*=bib]`), toolbar buttons (Add/search/Delete), entry count for a seeded bib (seed `@article{key...}` via mongo doc update or the editor type), Add-dropdown items' exact text.
**Probe 2 (round-trips):** Add → Enter manually (fill key/title/etc, submit) → entry appears in list AND in `.cm-content` source; Edit in form → source updates; Delete (preview Actions) → source + list updated; undo/redo if surfaced; bulk delete.
**Probe 3 (cross-provider):** Add → Import from Zotero (picker modal opens; with live zotero credentials linked → can fetch refs; at MINIMUM pin the modal surface + cancel), Import from ORCID (modal surface), Import from Library (API-backed picker list). Mendeley: check whether the Add menu offers Mendeley in THIS build (grep showed no mendeley in bib-editor imports — likely NOT in v2 Add menu; pin + flag if absent).
**Spec legs (draft):** open+panel · add-manual round-trip · edit round-trip · single delete · bulk delete+confirm · preview surface · import-from-library list · zotero picker surface · orcid picker surface · upload-.bib import · write-failure/undo surface (if reachable) · Mendeley presence (flag if absent).

## TODO-7dbc2a1c — library under /editor (Zotero/ORCID/Mendeley)
**Source map (verified):**
- Go `library` feature (`go/services/web/features/library/controller.go`) — PAGE routes RETIRED 2026-10-05 (`/library`, `/library/trashed` → 404; hub absorbs the page); APIs LIVE: `GET|POST /library/references`, `POST /library/references/match`, `GET /library/references/count|download|citation-key-suggestions`, `POST /library/references/delete|restore`, `PATCH /library/references/:key`. Rate limits: 120/min api, 60/min writes (per client IP — SHARED with the battery; be gentle).
- Frontend `bib-editor/frontend/js/library/` (library-api/context/page/root/manual-modal) — the editor-internal library surface + `pages/library.tsx` (old #library-root page, retired route).

**Probe 1 (API contract):** as USER: create ref (`POST /library/references` payload — read `library-api.ts` for exact fields: key/type/fields{title,author,year,url...}) → `GET /library/references` lists it → `PATCH :key` update → `GET /citation-key-suggestions` → `POST /delete` (trashed) → `GET` count/trashed? → `POST /restore` → `POST /delete` final (cleanup). Record statuses + shapes.
**Probe 2 (editor surface):** in the bib editor Add → Import from Library → picker opens → seeded library items listed → select → entry imported into the .bib (round-trip to source). Also: does the HUB have a library section now (ollitex-hub `library-section.tsx` — found in source; probe the hub `/library` or a hub route if it exists — check `hub-root.tsx` for the route path).
**Spec legs (draft):** api-create→list→patch→suggestions·trash-restore · editor import-from-library round-trip · hub library surface (if present; pin + flag if absent) · zotero/orcid/mendeley cross-checks shared with TODO-d07e1c99 (avoid duplicate legs — split cleanly).

## TODO-8803eddf — tracked changes + comments (JSON) in project ZIP import/export (EXCEPT templates)
**Source map:**
- download: `GET /project/:pid/download/zip` (verified live = new-tab navigation); upload: `POST /project/new/upload` (newzip.go); revision zips via history feature.
- tracked changes/comments storage: per-doc mongo (`trackchanges` go feature + comments) — zip inclusion is the QUESTION.

**Probe 1 (export):** project + doc edits with TRACKED CHANGES ON (toggle in editor or via API) + one COMMENT created → `GET /project/:pid/download/zip` (raw request) → unzip in sandbox → list zip contents → look for tracked-changes/comments JSON (names? `changes.json`? per-doc? `comments/`?). Record EXACT layout.
**Probe 2 (import):** zip that zip (or a crafted one) → `POST /project/new/upload` (form fields per newzip.go: file + name?) → new project → verify tracked changes + comments restored (API: trackchanges endpoints + comments list) → also verify a TEMPLATE (published template bundle) does NOT carry them (templates exclusion).
**Spec legs (draft):** export-carries · import-restores-changes · import-restores-comments · templates-excluded · (flag if any leg fails = product gap).

## Standing e2e facts to reuse
- double-click opens a file; single select only
- `api()` harness: `status()` is a METHOD; follows redirects
- rate limits are real + shared per IP — cooldowns on 429 (loginRobust exists)
- Mantine menus: hidden legacy `<a dropdown-item>` clones exist — use the visible-item iteration (see toolbar-rail clickMenuItem)
- CSRF: page meta `ol-csrfToken` for raw `page.request` mutations
- credentials: `.auth/*.json` (0600) for zotero classic pair / dropbox sl.* / mendeley; git-hosts in `credentials/config.json`

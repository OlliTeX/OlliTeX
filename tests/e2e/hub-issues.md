# /hub issue ledger — 2026-09-07 (owner found 33 issues on the LIVE container)

Status: **34/34 filed (phase 0 done, owner list = source of truth)**
Severity: P0 core function missing/broken · P1 feature/UX gap · P2 cosmetic · P3 polish
Verified against: production `https://psintern.neuro.uni-bremen.de/hub` (owner cycles `compose_cep/cycle_overleafserver.sh`; my local `make all` feeds the shared image).

| # | sev | area (leaf) | Issue → Required state | Status |
|---|-----|-------------|------------------------|--------|
| 0 | P2 | brand | **Rename OlliTex → LibreLeaf** → REVERSED 2026-09-16 by owner: all user-visible occurrences are **OlliTeX** again (footers/titles/manifest; `web.sitemanifest.json` name = OlliTeX; Makefile + e2e comments updated). Stale compiled `app/views/*.js` build residue + generated `public/` bundles regenerate on build. | done |
| 1 | P2 | hub header | Replace icon+text block (OlliTex / "Workspace & administration") with **logo_full.svg** (app logo asset). | open |
| 2 | P1 | `#/overview` | "Instance management" shortcut buttons (Site settings/Manage users/Manage projects/Templates/LLM instance) do nothing → **make them navigate** to the hub sections (Q4: or remove). | open |
| 3 | P2 | `#/projects.*` | **Remove** in-page filter tab group (Projects/Your projects/Shared with you/Archived) — rail already has these. | open |
| 4 | P0 | `#/projects.*` | New project button must open the **full menu**: Blank project · Import (.zip, Word, Markdown, GitHub) · Templates (Example + More templates) — Mantine re-build, all modals included. | open |
| 5 | P2 | `#/projects.*` | **Remove** "open_in_new Full project list" header link. | open |
| 6 | P2 | `#/templates.*` | **Remove** in-page "Categories" side rail (duplicate of left rail). | open |
| 7 | P2 | rail | Folder heads (Templates/My settings/…) wrong pressed/active CSS: white bg + puzzle glyph → restyle like the leaf rows; invalid icon names render `.notdef` puzzle pieces — **audit ALL icon names** against the bundled material-symbols font. | open |
| 8 | P1 | rail | Rail can't be wheel-scrolled; give the rail **its own scrollbar**, decouple from page scrolling (fixed viewport layout: header 64px + rail/main each overflow-y:auto, body no scroll). | open |
| 9 | P2 | `#/templates.*` | "All templates" appears **twice** in rail; only the lower one works — remove the duplicate (keep the top/static position). | open |
| 10 | P2 | `#/library` | **Remove** "open_in_new Full library" link. | open |
| 11 | P0 | `#/library` | "Add reference" must expose the full menu: Paste references (BibTeX/DOI) · Upload .bib · Enter manually · Import from ORCID · Import from Zotero — **all modals rebuilt in Mantine**. | open |
| 12 | P2 | `#/mysettings.*` | **Remove** in-page tab strip (Account/Password/Appearance/Editor defaults) + the "Open the full settings page" alert. Rail covers it. | open |
| 13 | P0 | `#/mysettings.password` | Password content missing → focused section must show the password form. | open |
| 14 | P0 | `#/mysettings.appearance` | Appearance content missing (personal dark/light/system) → focused section. | open |
| 15 | P0 | `#/mysettings.editordefaults` | Editor-defaults content missing → focused section. | open |
| 16 | P0 | `#/mysettings.keybindings` · `sync` · `references` · `sessions` · `site.general.enclose`(Q3) · `site.general.messages` · `site.general.stats` | Content **fully missing** → build each: Keybindings, Project synchronisation (zotero/webdav/dropbox), Reference managers, Sessions, (enclose→?), System messages (admin), Instance statistics (admin). | open |
| 17 | P3 | `#/mysettings.email` | Minutes NumberInput: move "min" label to a suffix behind the input. | open |
| 18 | P1 | `#/site.general.projects.all|inactive` | Checkbox per row + select-all header → bulk toolbar: Download · Change owner · Trash. Parity with legacy admin page. | open |
| 19 | P1 | `#/site.general.projects.trashed` | Checkboxes + select-all → bulk: Download · Change owner · Restore · Delete. | open |
| 20 | P1 | `#/site.general.projects.deleted` | Checkboxes + select-all → bulk: Restore · Purge. | open |
| 21 | P1 | `#/site.general.users.all|admins|suspended|inactive` | Checkboxes + select-all → bulk: Suspend · Resume · Mail · Set admin · Unset admin · Delete. | open |
| 22 | P2 | `#/site.general.users.*` | **Remove** "Show inactive" switch (inactive is its own rail item). | open |
| 23 | P1 | `#/site.general.users.deleted` | Checkboxes + select-all → bulk: Restore · Purge. | open |
| 24 | P2 | rail | Color-code the rail: **admin items red font** (danger), **user settings blue font**. | open |
| 25 | P1 | `#/site.general.users.all|admins|suspended|inactive` | Row actions: Resend · Info · Update · Suspend · Delete (per row, parity). | open |
| 26 | P1 | `#/site.general.users.deleted` | Row actions: Restore · Purge. | open |
| 27 | P2 | `#/site.general.projects.all|inactive|trashed` | **Remove** "Include trash / deleted" checkbox (state is expressed by the rail item). | open |
| 28 | P1 | `#/site.general.projects.all|inactive` | Row actions: Download .zip · Change owner · Share · Trash. | open |
| 29 | P1 | `#/site.general.projects.trashed` | Row actions: Download · Change owner · Share · Restore · Delete. | open |
| 30 | P1 | `#/site.general.projects.deleted` | Row actions: Restore · Purge. | open |
| 31 | P2 | rail | "activityzone" icon (invalid glyph → puzzle piece) on one item — replace with valid symbol (part of #7 audit). | open |
| 32 | P1 | `#/site.integrations.sso-saml|oidc|ldap`, `site.services.email|services|branding|grammar`, `site.compilation.sandboxed|pandoc|git|github|linkedfiletypes|webdav|dropbox` | **Remake in Mantine** (currently legacy wraps) — zero parameter loss (field-level ledger exists from audit). LARGEST item. | open |
| 33 | P2 | rail | "Pandoc" active row: broken label/glyph (icon `convert` invalid → puzzle piece) — part of #7 audit. | open |

## Questions for owner (asked 2026-09-07, default if silent noted)
- **Q1 rename scope**: LibreLeaf in ALL user-visible strings app-wide (navbar/footer/titles/manifest) — default YES; code identifiers stay (optional later pass).
- **Q2 production**: may I self-cycle production once at the green gate? default = NO (owner cycles).
- **Q3**: `#/site.general.enclose` — which page? default: treat as `site.general.misc` (+ cover messages + stats explicitly).
- **Q4 (#2)**: overview shortcuts = navigate (recommended default) or remove?

## Backlog (owner: after fixes + e2e, "if there is time")
- [ ] Mendeley integration back on the task list.

## Waves (execution order)
- **WAVE A** (quick structural/cosmetic, low risk): 0,1,2,3,5,6,7(icons),8,9,10,12,17,22,24,27,31,33
- **WAVE B** (feature parity medium): 13,14,15,16,4,11
- **WAVE C** (heavy): 18,19,20,21,23,25,26,28,29,30 → then 32
- After each wave: build green + targeted probes; at end of C: full gate + E2E repurpose + old-page removal + morning report.

## 2026-09-07 — Owner 11-item batch (#1–#11) status

### Done (committed, e2e stack: http://localhost:7420/hub)
- **#1 admin LLM per-section leaves** — `site.llm.general|usage|grammar|compliance`,
  one card per leaf (commit df8b54a1).
- **#2 admin users/projects pagination** — server filter mapping + totalSize +
  Mantine Pagination + "Page X of Y — N projects/users"; client slicing 25/page
  only when the list exceeds a page (commit e9f779e0).
- **#3 templates.all** — shows every template incl. unpublished (commit df8b54a1).
- **#4a–4e library** — 5 import/create modals (manual, paste BibTeX/DOI, .bib
  upload, ORCID, Zotero), 25/page + load-more, multi-select + bulk
  download/trash, trash tab restore/purge, per-row edit (commit 405d5fbb).
- **#5a/5b keybindings** — Mantine radio group + options table + Customize
  modal (rebind/capture, clear, preset reset stays open, import/export,
  Apply; Ctrl-combo works) (commit 9ef28b62; keybindings e2e 3/3).
- **#6a/6b sync & references** — split leaves with Mantine cards around the
  real widgets (commit df8b54a1).
- **#7 user LLM per-section** — `mysettings.llm.general|usage|grammar|compliance`.
- **#8 mendeley admin** — `site.integrations.mendeley`: clientId + secret +
  enabled, DB-override resolution (proven with EMPTY env), status endpoint
  re-resolves per request (commit 00f01886).
- **#9 branding** — full-width footer/contact inputs (commit df8b54a1).
- **#10 projects** — list table 25/page (Page 1 of 9 on 206 projects),
  row menu (Open/Copy/Download/Archive/Trash-Restore/Leave/tag), bulk
  toolbar (Download/Archive/Trash/Restore/Add-to-tag), trashed leaf with
  Restore, New-project menu: Blank(name+template) / From template /
  Existing .zip / From GitHub(repo+branch) / .docx / .md (commit 9ef28b62).

### #11 audit: /hub modal surfaces
Every **modal** in the hub tree is now Mantine — admin ConfirmModal,
library ×5 create/import modals, projects ×7 (blank/template/github/zip/
docx/md/trash-confirm), keybindings customize modal. No `OLModal` usage
remains in any hub-rendered module (widgets embedded below use 0 OLModal).

**Remaining legacy surfaces (NOT modals — pages/inline chrome):**
1. `site.*` admin leaves still on the legacy r9 admin pages
   (Miscellaneous / Appearance(legacy) / Signup / Enclose …) — a native
   Mantine rebuild is the next big wave (several sections, each its own
   settings block; users/projects/LLM/mendeley templates already native).
2. Integration widgets (dropbox/github-sync/mendeley/webdav/zotero) keep
   their internal legacy OLButton chrome inside the Mantine card shells
   (they are the real widgets, shared with the editor file-tree flows —
   reworking them forks shared feature modules; deliberate trade-off).
3. `mysettings.grammar` (languagetool section) — legacy buttons inline.

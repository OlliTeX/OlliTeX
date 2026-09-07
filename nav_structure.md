# OlliTeX Hub — Navigation Structure (owner-approved design)

Status: **design finalized with owner (2026-09-07), not yet implemented.**
Reminder doc: point me at this file to drive the build.

## 1. Goal

ONE hub page: **`/hub`** — replaces the two hubs `/hub/admin` and
`/hub/workspace` (both URLs become redirects to `/hub`).

One left rail. All navigation is a **nested accordion** (Mantine
`Accordion`), max depth 3 (top-level → group → leaf/action). No
second in-page nav bar anywhere (the site-settings "inner rail" goes
away — its 19 sections first-class items in the main rail).

## 2. The tree (canonical)

```
OlliTeX
│
├─ Overview & activity                        (admin)
│
├─ Projects                                   ▸ accordion
│   ├─ All projects
│   ├─ My projects
│   ├─ Shared with you
│   ├─ Archived projects
│   └─ Organize Tags                          ▸ accordion   (live tag list)
│       ├─ <tag> …                            (one entry per existing tag)
│       └─ New tag
│
├─ Templates                                  ▸ accordion
│   ├─ All templates
│   ├─ Academic journals
│   ├─ Books
│   ├─ Presentations
│   ├─ Posters
│   ├─ CVs
│   ├─ Homework
│   ├─ Bibliographies
│   ├─ Calendars
│   ├─ Formal letters
│   ├─ Reports
│   ├─ Theses
│   └─ Newsletters
│
├─ Reference library
│
├─ My settings                                ▸ accordion
│   ├─ Update account info
│   ├─ Change password
│   ├─ Keybindings
│   ├─ Project synchronisation
│   ├─ Reference managers
│   ├─ Sessions
│   ├─ Appearance
│   ├─ Editor defaults
│   ├─ Email preferences
│   └─ My LLM settings                        ▸ accordion
│       ├─ General
│       ├─ Grammar Checking
│       ├─ Compliance Review
│       └─ Usage
│
└─ Site settings                              (admin) ▸ accordion
    ├─ GENERAL                                ▸ accordion
    │   ├─ Miscellaneous
    │   ├─ Appearance
    │   ├─ Sign-up
    │   ├─ Manage templates                   (gallery import/list/delete)
    │   ├─ Projects                           ▸ accordion   (admin, all users)
    │   │   ├─ All projects
    │   │   ├─ Inactive projects
    │   │   ├─ Trashed projects
    │   │   └─ Deleted projects
    │   ├─ Users & access                     ▸ accordion
    │   │   ├─ All users
    │   │   ├─ Administrators
    │   │   ├─ Suspended users
    │   │   ├─ Inactive users
    │   │   └─ Deleted users
    │   ├─ Active projects
    │   ├─ Open/Close Editor
    │   ├─ System messages
    │   └─ Instance statistics
    │
    ├─ INTEGRATIONS                           ▸ accordion
    │   ├─ Zotero
    │   ├─ External URLs
    │   ├─ SSO · SAML
    │   ├─ SSO · OIDC
    │   └─ SSO · LDAP
    │
    ├─ SERVICES                               ▸ accordion
    │   ├─ Email / SMTP
    │   ├─ Services
    │   ├─ Branding
    │   └─ Grammar (LT)                       (incl. LanguageTool availability parameters)
    │
    ├─ COMPILATION                            ▸ accordion
    │   ├─ Sandboxed compiles
    │   ├─ Pandoc
    │   ├─ Git integration
    │   ├─ GitHub sync
    │   ├─ Linked file types
    │   ├─ WebDAV
    │   └─ Dropbox
    │
    └─ LLM Settings                           ▸ accordion
        ├─ Features
        ├─ API Connection
        ├─ Model Selection
        ├─ System Prompt
        ├─ AI Prompts
        └─ Usage
```

## 3. Navigation rules (agreed)

1. Every `▸ accordion` folds independently at its own level
   (Site settings → GENERAL → Miscellaneous all fold; each sibling
   group stays put).
2. The active section's ancestor chain auto-expands on load and on
   selection.
3. Open/closed state of each folder is remembered (per browser, and
   restored on reload).
4. Every leaf is deep-linkable — hash routes, e.g.
   `/hub#/site/general/users/suspended`, `/hub#/projects/my`,
   `/hub#/mysettings/llm/compliance`. Unknown hash → harmless
   fallback (today's SectionFallback behaviour).
5. Built with Mantine `Accordion` (keyboard accessible, themeable).
6. Admin-only items (OVERVIEW, whole SITE SETTINGS, admin sub-views)
   hide entirely for non-admin members; members see a clean personal
   tree. Gating uses the same admin capability the `/hub/admin` route
   uses today.
7. `/hub/admin` and `/hub/workspace` redirect to `/hub` (bookmarks
   keep working). The two old bundles may stay on disk until the
   redirect is verified.
8. Landing section: **admin → Overview & activity; member →
   Projects → All projects.** (owner default: confirmed pattern
   "silence = suggestion stands")
9. The top-level group LABELS above the first items of each hub
   ("Workspace", "My settings", "Configuration"…) are NOT part of
   the new design; the tree above is the rail.

## 4. Item → source mapping (what exists / what is new)

### Existing (re-wrap into the new rail)
| Nav item | Source today |
|---|---|
| Overview & activity | admin-hub Overview section (instance-stats based) |
| Projects → My / Shared / Archived / All | workspace projects-section (`POST /api/project` filters: `ownedByUser`, `sharedWithUser`, `archived`; All = no filter) |
| Projects → Organize Tags | workspace projects-section "Organize Tags" (GET/POST/DELETE `/tag`) + `filters.tag`; classic: `frontend/js/features/project-list/components/sidebar/tags-list.tsx` |
| Templates → *category* | workspace templates-section (`GET /api/template/categories` + `GET /api/templates?category=<key>`); category list is data-driven — the 12 named categories are the current production set |
| Reference library | workspace library-section (bib-editor) |
| My settings → Update account info / Change password / Keybindings / Project synchronisation / Reference managers / Sessions | classic `/user/mysettings` split sections (page-shells mysettings; sections exist there) — split out individually |
| My settings → Email preferences | workspace notifications-settings-section |
| My settings → My LLM settings → General / Grammar Checking / Compliance Review / Usage | `modules/llm/.../llm-settings-page.tsx` (exact four sections, verified) — split into four sub-views |
| Site settings → GENERAL → Miscellaneous / Sign-up / (template gallery settings) | admin-site-section native sections (`PUT /admin/site-settings/:id`) |
| Site settings → GENERAL → Manage templates | admin-hub "Templates" section (bundle import/list/delete) |
| Site settings → GENERAL → Projects → 4 views | admin-tools all-projects list; views: All / Inactive / Trashed / Deleted |
| Site settings → GENERAL → Users & access → 5 views | admin-tools user list; views: All / Administrators / Suspended / Inactive / Deleted |
| Site settings → GENERAL → Active projects | admin "active projects" (live sessions) page |
| Site settings → GENERAL → Open/Close Editor | routes `/admin/openEditor`, `/admin/closeEditor`, `/admin/disconnectAllUsers` (wrap as an action page) |
| Site settings → GENERAL → System messages | `/admin/messages` (+ clear) |
| Site settings → GENERAL → Instance statistics | `/admin/instance-stats` (instance-stats module) |
| INTEGRATIONS (5), SERVICES (3 + Grammar incl. LT-availability params), COMPILATION (7) | admin-site-section items (legacy-wrapped); note: move missing LanguageTool **availability** settings INTO the Grammar (LT) item |
| LLM Settings → 6 items | `modules/llm/.../llm-admin-settings-page.tsx` (Features, API Connection, Model Selection, System Prompt, AI Prompts, Usage) — split into six sub-views |

### New sections to build
- **Site settings → GENERAL → Appearance** (NEW, owner 2026-09-07) — the
  instance theme editor for `/hub`. Admins can change ALL hub colors
  (primary, background, surface, text, dimmed, border, button, button
  text — separately for **light and dark mode**), the **font itself**
  and the **font size** (+ radius). Controls (Mantine):
  per-mode color pickers, font select, font-size + radius sliders.
  Buttons: **Apply** (save + live re-theme), **Reset** (back to
  OlliTeX defaults), **Export** (download the JSON), **Import**
  (file picker → parse/validate → preview → Apply). Stored as a JSON
  document in the instance site_settings (`hubTheme` key), served via
  a new admin API (`GET/PUT /admin/hub-theme`) and injected as page
  meta so every hub render uses it; provider rebuilds the Mantine
  theme live on apply (no reload). Applies to both light and dark.
  JSON shape (v1):
  ```json
  {
    "version": 1,
    "light": { "primary": "#098842", "background": "#ffffff", "surface": "#f7f8fa",
               "text": "#1b222c", "dimmed": "#5b6572", "border": "#e2e5ea",
               "button": "#098842", "buttonText": "#ffffff",
               "fontFamily": "Noto Sans", "fontSize": 16, "radius": 8 },
    "dark":  { "primary": "#098842", "background": "#2f3a4c", "surface": "#263041",
               "text": "#e8ebf0", "dimmed": "#9aa5b5", "border": "#3b475c",
               "button": "#098842", "buttonText": "#ffffff",
               "fontFamily": "Noto Sans", "fontSize": 16, "radius": 8 }
  }
  ```
- **My settings → Appearance** — theme/appearance controls (wire the
  existing `overall-theme.ts` store into a proper user-facing page;
  today it only exists as the header toggle).
- **My settings → Editor defaults** — new-project defaults (compile
  engine, language, sync on).

## 5. Implementation notes

- **One route, one bundle**: `GET /hub` (login required; same admin
  capability meta the admin hub uses) renders `HubRoot`; `section`
  from the hash drives content. Keep the error boundary per
  section (SectionFallback pattern) so one bad section never kills
  the hub.
- **Rail model**: one data file defines the whole tree (id, label,
  icon, `adminOnly`, `children`, `leaf: {route/render}`); the
  renderer is generic (the accordion depth is data-driven) —
  adding a section later = one line in the tree file.
- **Leaf content**: reuse the existing section components verbatim
  wherever they exist (projects templates users site-settings llm
  user-llm library notifications my-settings-splits); the splits
  (admin users/projects views, user LLM 4-way, LLM admin 6-way,
  mysettings 7-way) = parameterizing existing wrapped components
  with a `view` prop, not new APIs.
- **Theme**: `overall-theme.ts` (single source of truth) +
  `colorSchemeManager` bridge stay; Appearance page binds to the
  same store.
- **Icons**: `.material-symbols` class + bundled
  `material-symbols.css` (do NOT reintroduce `.material-symbols-rounded`).
- **Build order suggestion**: 1) single-route shell + accordion rail
  (all existing items), 2) splits/views (users, projects, user-LLM,
  LLM admin, mysettings), 3) new items (Appearance, Editor
  defaults, active projects, open/close editor, system messages,
  instance stats, manage templates), 4) redirects + cleanup.
- **Verify** (same bar as previous hub work): docker image compiles
  green (webpack) → Playwright probes (rail geometry, both themes,
  accordion fold/auto-expand/deep-link persistence, admin vs
  member visibility) → E2E suite 21 passed / 1 skipped → push
  `ext-6.3.0-port` → `sh cycle_overleafserver.sh` → prod smoke.

## 6. Constraints (standing)

- Mantine 9.6.0 is the framework for all hub UI.
- Do NOT touch the production editor or login/register.
- No parallel subagents (owner instruction).
- Node ≥ 24.18.1; production canonical =
  `https://psintern.neuro.uni-bremen.de` (TLS).
- Production env: compose `/data_1/docker/compose_cep/overleafserver/compose.yaml`;
  cycle: `cd /data_1/docker/compose_cep && sh cycle_overleafserver.sh`.
- Push: `GIT_SSH_COMMAND='ssh -i /root/.ssh/github -o IdentitiesOnly=yes' git push origin ext-6.3.0-port` — never blind `git add -A`; no secrets in commits.
- E2E target: dedicated test stack only (`tests/e2e`, port 7420),
  never the production box.

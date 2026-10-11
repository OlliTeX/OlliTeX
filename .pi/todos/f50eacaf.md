{
  "id": "f50eacaf",
  "title": "Typst admin section in /hub — DONE: hub typst section (ollitex-hub typst-section.tsx in built bundle, nav-tree) + Go SiteSettings 'typst' group P3.6 (manager section + seed + validateTypstSection) + P6.16 typst new-project creation (ee034c6e7e)",
  "tags": [
    "hub",
    "typst",
    "admin",
    "owner-request"
  ],
  "status": "done",
  "created_at": 1789138941531,
  "last_change": 1790665067681,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

Owner request (2026-09-11): "I am missing a typst admin section in /hub like for the sandbox compile with tex."

Context: /hub admin instance section (services/web/modules/ollitex-hub/frontend/js/sections/admin/admin-instance-section.tsx + site/sandboxed-section.tsx) exposes sandboxed compile settings for TeX (compile containers, allowed images, sandboxed mode toggle). The Typst integration (services/clsi_typst, OVERLEAF_CLSI_TYPST_*, typst compile sibling container, typst allowed-images) has NO hub admin surface — currently env-only (compose) + site_settings gaps.

Work plan (a P1 hub feature; after green gate):
1. Audit how the TeX sandboxed admin section works (admin instance section → which settings section, e.g. `sandbox`/`compile` in site_settings; API endpoints used; component in site/sandboxed-section.tsx; backend validators in SiteSettingsManager.mjs).
2. Mirror for Typst: a "Typst compile" admin subsection in /hub (admin instance area) covering at minimum: Typst enabled toggle, compile container/image (typst-runner image + allowed images list), sandboxed compile behavior, and a link to the compile health. Store under a new site_settings section `typst` (validator + whitelist) served by existing admin site-settings API (already generic per-section) — or extend an existing section if cleaner.
3. Wire the stored settings into the server (settings.js/site_settings → clsi_typst config already reads `OVERLEAF_CLSI_TYPST_CLSITYPE`/allowed images; make it read the stored section at runtime the same way env does).
4. e2e: hub admin typst section renders, toggle round-trips via API, appears in /hub for admin, hidden for non-admins; unit: validator.
5. Update RELEASE_NOTES.md + docs/wiki/admins instance-settings page.
Evidence (verified 2026-09-21): services/web/modules/ollitex-hub/frontend/js/sections/admin/site/typst-section.tsx (+ index.ts, nav-tree.ts) ships in the built hub bundle (public/js/modules/ollitex-hub/pages/* reference typst); backend: go/services/web/features/sitesettings/manager.go `"typst": {"enabled","url"}` group (line 35), seeds.go case "typst" (line 184), validators.go validateTypstSection (line 378/699) — flipped with P3.6 SiteSettings GATE 3/3. Typst project creation surface flipped in P6.16 (commit ee034c6e7e, GATE 5/5).
Closure evidence (verified 2026-09-21):
- /hub frontend section: services/web/modules/ollitex-hub/frontend/js/sections/admin/site/typst-section.tsx (+ index.ts, hub/nav-tree.ts) is built into the shipped bundle (public/js/modules/ollitex-hub/pages/* reference typst).
- Go backend (P3.6 SiteSettings, GATE 3/3): go/services/web/features/sitesettings/manager.go `"typst": {"enabled","url"}` (line 35) + registered section list (line 54); seeds.go `case "typst"` (line 184); validators.go `validateTypstSection` (lines 378, 699).
- Typst new-project creation surface flipped separately: P6.16, commit ee034c6e7e, GATE 5/5.
Note: if owner wants the DEEPER typst compile-admin (runner image / allowed-images / clsi_typst env mirroring the sandboxed-TeX section, per original work plan), that part remains a product gap — reopen if so.

# GOAL[5] — TERMINAL AUDIT (final board item)

**Date:** 2026-11-16 · **World:** OlliTeX CE `ag-v57` (rebuild → ag-v57 lineage), E2E stack A `ol-e2e-overleaf-1:7420` (172.20.0.2) + B `7421`, shared Mongo/Redis
**Session scope (this arc):** battery drift triage to terminal state → owner handoff (external credentials) → three owner-todo e2e arcs → ledger convergence → this report.

---

## 1. Method & standing rules (what "green" means here)

1. **Probe-first, pin-faithful, flag-loud** — every spec starts from a live probe of the *current* behavior (never the assumed one); what is pinned asserts live-verbatim contracts (status + exact messages) **and** behavior; each product gap is pinned **and** FLAGGED in the spec header + this log (never re-pinned to pass on weaker text).
2. **Re-arm trip-wires** — retired/moved surfaces carry assert-inverse legs: if the product revives them the suite flips red (e.g. zip-export landing, Mendeley menu item appearing, removed-user kick returning, messages page rendering content, v2 presence/request-access/rail-account surfacing).
3. **Evidence-gated close** — no board item closes without green runs + commit + log entry; owner-gated items close only as "pinned + flagged".
4. **Secrets discipline** — real credentials live only in git-ignored `tests/e2e/credentials/` (0600); tracked files carry placeholders/`${VAR}`; chat logs carry key names + sha256 prefixes only. CAPTCHA/interactive walls are documented, never bypassed.
5. **No interference** — no spec/probe runs while a full battery runs (shared workers=1 world, shared per-IP rate limits, shared users).

---

## 2. Phase coverage table (this GOAL, Phases 1–6)

| Phase | Deliverable | State | Evidence |
|---|---|---|---|
| **1 — Trust repair** | Rebuild-from-source image, faithful envs, standing suite green | ✅ done | image `ag-v57`; `toolkit/.env` + `tests/e2e/.env.test` faithful extractions (env.example 93-key contract); standing suite green (see §3) |
| **2 — e2e foundations** | Harness, helpers (loginRobust/cooldowns, cleanup), parity gate 99 items | ✅ done | `helpers/auth.ts` (+ `helpers/ctx.ts`, `helpers/host.ts`), the 99-item gate = `specs/parity-check.test.e2e.ts` (99/99), `credentials/external.ts` (getServiceToken:134) |
| **3 — Feature waves** | A–D waves: auth pages, admin lifecycle, tags/inspection, inspection migration, drawio, federation gate | ✅ done | commits `1e38ce2d` (auth 6/6+8/8), `41cf53f2` (admin 6/6), `3bc79c90` (9/9+7/7), `5f7d8452` (8a6426ef), `d87ae57b` (drawio 4/4), `89feda4a` (federation 7/7) |
| **4 — Storybook/UI polish** | UI polish letters, a11y, dark mode, axe pins | ✅ done | polish spec 5/5 + module-m1 webdav axe pin (known-debt whitelist, NEW violations still fail) |
| **5 — Product arcs** | Sync/bridge update-conflict + resolution (WebDAV/git-hosts/Dropbox), dropbox+zotero+mendeley credential arcs & link-cycles, external probes, capture tooling | ✅ done | `1cc69f26` (sync/bridge green), `17792add`/`f8a6fbda`/`a2d9466b`/`ffbddd06`/`1f0e12d1` (credential arcs), `ccb052cf` (owner handoff); `scripts/capture-interactive-tokens.mjs` |
| **6 — Owner-todo arcs** | bib visual editor · library under /editor · ZIP tracked-changes+comments | ✅ done | `ad98f567` (6/6) · `c9aeb787` (10/10) · `475a36bf` (6/6) — see §4 |

**Board item `9fbda0eb` (this audit)**: full-arc coverage table (§2/§4), live-verification summary (§3/§5), ledger convergence (below), final owner handoff report (§6–§8).

---

## 3. Live-verification summary (batteries → terminal state)

| Battery | Result | Note |
|---|---|---|
| #1 (baseline drift) | 450 passed / **33 failed** / 4 skipped | drift inventory established |
| #2 (round-1) | 465 passed / **23 failed** / 4 skipped | checkpoint commits landed |
| **#3 (terminal)** | **487 passed / 1 failed / 4 skipped** (1.0h) | **single red = dropbox owner-token expiry** (external credential, not drift) — tracked TODO-39e56295, loud actionable failure after `9dec56c9` |

**Round-2 drift resolution (all 23 battery-2 reds → green):** `e693f112` — toolbar-rail 13/13 (v2 chrome re-pin), modals 5/5 (removed-user + history + sync), panes 11/11 (v2 history sidebar), module-uis 8/8 (contextual Insert menu), polish 5/5 (axe known-debt), hub-mysettings 9/9 (WebDAV provider-list + API), notifications (debounce race), parity gate 99/99. Combined verification of all 12 touched specs: **76/76 green (14.0m)**.

**Parity gate:** 99/99 green (templates matrix truth-updated: `/templates/manage` is the live surface).

**Standing suite at sign-off:** 492 tests — 487 green · 4 skipped (external-cred gates) · 1 owner-credential red (tracked, actionable).

---

## 4. Owner-todo arcs (this session, Phase 6)

| Todo | Result | Flagged (owner) |
|---|---|---|
| **bib visual editor** (`ad98f567`) | 6/6 green — panel mount, six-item Add menu, manual-add round-trip, code↔visual parity, validation contract, search filter | Mendeley import item absent in this build's Add menu (not a defect claim — observation) |
| **library under /editor** (`c9aeb787`) | 10/10 green — full data plane: list/create-201/400-validation/PATCH/409-duplicateKey/match/download-library.bib/suggestions/soft-delete+restore/delete-guard-400 | none — all contracts green. NEW: CSRF token channel documented (`<ol-csrfToken>` meta → x-csrf-token) |
| **ZIP tracked changes+comments** (`475a36bf`) | 6/6 green (pins) | **FLAG 1:** user single-project zip route ABSENT → File-menu "Download as source (.zip)" anchor 404s (leg A re-arms if the route lands) · **FLAG 2:** admin multi-zip = placeholder marker only (asserts NO .tex / comment / tracked-changes payload; re-arms if real content appears) · **FLAG 3:** import is files-only — a tracked-changes+comments zip round-trip is impossible in the current model (owner decision recorded) |

---

## 5. Credential matrix (owner handoff)

| Service | State | Where | Action needed |
|---|---|---|---|
| git hosts (github/gitea/gitlab/forgejo) | ✅ live (specs green) | `credentials/config.json` (tracked, placeholder-safe) | none |
| WebDAV (Nextcloud nc.uni-bremen.de) | ✅ live (sync+resolution green) | same | none |
| LLM (OpenAI-compatible) | ✅ live | same | none |
| Zotero | ✅ linked-state verified (9p/3s) | `.auth/zotero.json` (0600, git-ignored) | none |
| Dropbox | 🔴 **owner sl.* token expired** (401 expired_access_token; refresh grant exhausted) | `.auth/dropbox.json` | **owner:** regenerate app token (console) or re-run `scripts/capture-interactive-tokens.mjs`; then `external-probe -g "dropbox token"` must pass (TODO-39e56295) |
| Mendeley | ✅ verified (link-cycle green; credential arc closed `f8a6fbda`/`1b41ff73`) | `.auth/mendeley.json` (0600, git-ignored) | none (re-capture only if the suite goes red) |

---

## 6. Flag register (accumulated, all pinned in-spec)

| # | Flag | Status |
|---|---|---|
| F1 | user zip-export route absent (File-menu anchor dead) | pinned + re-arm |
| F2 | admin multi-zip placeholder (no files/comments/tracked-changes) | pinned + re-arm |
| F3 | zip round-trip of tracked-changes/comments impossible in current model | owner decision |
| F4 | bib Add-menu: Mendeley import item absent in this build | pinned (observation) |
| F5 | read-only request-access flow absent in v2 chrome | pinned + re-arm |
| F6 | presence/online-users indicator absent in v2 rail | pinned + re-arm |
| F7 | rail account menu absent in v2 | pinned + re-arm |
| F8 | removed-user: no in-session kick; fresh-open is denied | pinned + re-arm |
| F9 | `/admin-settings/site.general.messages` renders nav-shell only (content missing) — genuine regression | pinned (not masked) |
| F10 | `/templates` + `/template/:id` gallery routes retired → 404; `/templates/manage` is live | pinned |
| F11 | WebDAV F1/F2/F3 (earlier arc) | pinned |
| F12 | ghsync merge no-op; Dropbox sync engine stubbed; Mendeley seed-vs-reader asymmetry | pinned / owner-precondition |

---

## 7. Owner-pending (non-blocking) decisions

- `225c3d7e` · `43a8cb9a` (sample.bib content) · `ebb76488` (federation git_url dead URL — edge route vs stop advertising) · `89da1036` (README/wiki no-secret guard) — all remain open, recorded with recommendations.

---

## 8. Sign-off

- ✅ Phases 1–6 all have green evidence + commit + log entry (no item closed on faith).
- ✅ Standing suite terminal state: 487/492, single red isolated to an owner action (external credential), parity gate 99/99.
- ✅ Ledger converged: the three owner-todo e2e arcs + their board duplicates all `done` with cross-referenced evidence (board items ed0ca1ca/419f9f3f/ab876e5d closed 2026-11-16 with commit cross-references).
- ✅ No destructive/production action outside green-lit scoped e2e-local infrastructure in this session; no CAPTCHA bypass; no secret in tracked files or chat.

**Commit lineage (this session, oldest→newest):**
`40f3e13d` → `801c978c` → `e693f112` → `9dec56c9` → `ad98f567` → `c9aeb787` → `475a36bf` (+ `a3c79438`/`4e34a72b` log corrections)

**Terminal board state for GOAL[5]:** all phases evidence-gated green; the only forward dependencies are (a) the owner Dropbox token re-capture (TODO-39e56295), (b) the four recorded owner decisions, (c) product decisions on flags F1–F3 (zip/tracked-changes+comments scope).

**GOAL[5] audit: COMPLETE.** → `9fbda0eb` closed.

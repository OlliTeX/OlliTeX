# AG — Intensive /editor live e2e suite (owner request, 2026-10-07)

Rigorous, module-focused e2e coverage of the whole `/editor` surface, run
**against the live production stack** (`psintern.neuro.uni-bremen.de`).

Reproducible by design (owner requirement): the suite logs in as its own
dedicated test account with its own fixture projects — the owner's live
project is never the test target.

| what | value |
|---|---|
| test account | `ag-e2e3@ollitex.local` (created via the site signup API) |
| fixtures | `agg-tex-fixture` (article main.tex + sample.bib + frog.jpg), `agg-typst-fixture` (typst template + hello.png) |
| owner project | never typed into, never mutated by the suite |

## Modules

- `agg1-shell.cjs` — **10/10** — editor shell, PDF render, **PDF zoom dropdown → Presentation mode → real fullscreen** (locator user-gesture click), file upload, typst PDF, symbol palette.
- `agg2-review.cjs` — **12/12** — tracked changes (insert + **±1 coord truth**, delete-range), **accept/reject keyed by the needle's panel entry** (reject `POST /project/:pid/doc/:doc/changes/reject` + rehydration), entry actions, reload stability, **comment composer (shadow-DOM CM6) submit + persist**.
- `agg3-ad.cjs` — **6/6** — image edit modal (toolbar+canvas), image resize `w/h`, **dirty-guard: canvas changes do NOT mark the file dirty** (undo-baseline), no 404s, **no page errors** (seedBaseline scope regression).
- `agg4-af.cjs` — **8/8** — synctex tex→pdf AND controlled pdf→tex (rendered lines, tolerance), AF-5/AF-6 word counts with real counts, **no console flood** (doc_id guard), typst PDF.
- `agg5-modes.cjs` — **10/10** — editing↔reviewing semantics, context menu, review rail, File menu (Rename/Share/Word count), **entry-scoped tracked-change accept/reject + S2 rehydration**, image modal, **SVG editor (svgedit iframe)**.
- `agg6-wakatime.cjs` — **5/5** — **WakaTime ON by default: status 200 + `{connected:false}`**, heartbeat 204 quiet no-op, editor `exposedSettings.wakaTimeEnabled=true`, **settings-page card (title + Connect + API key + self-host note)**, link validation 400.
- `agg7-ah.cjs` — **6/6** — **AH hub settings split**: `/user-settings` renders (14 sections: account/password/keybindings/sync/git/references/sessions/appearance/editor/email + LLM×4) with working sidebar navigation; `/admin-settings` renders the 46 site sections (users/projects/SSO/storage/compilation/LLM — not the restricted page); `/template-settings` renders manage+gallery (2 sections, no restricted page); hub rail section headings carry the dedicated-page links (`data-hub-settings-link` → the three routes); `/restricted?from=%2Fadmin-settings` member-bounce surface reachable (302 contract pinned in Go + anon probe 302 → /login).

**TOTAL: 57/57 green (2026-10-07 final, build #24).**

## Run

```sh
cd tests/e2e/live-agg
node run-all.cjs            # all modules, combined matrix
node agg2-review.cjs        # or a single module
```

(Playwright path is auto-resolved: pinned global install with a bare-module
fallback, so the suite works regardless of the repo's virtual store.)

Screenshots land in `/var/tmp/agg[1-6]-*.png`.

## Invariants asserted (owner defects fixed 2026-10-07)

- **AC (tracked changes)** — bursts coalesce to ONE record at the TRUE docstore
  index (static NoAnchors captures; anchors only for comment threads). Positions
  asserted ±1 (op `p` points at the insertion boundary). Wire shape
  `{ op: { i|d: text, p: pos } }` — `op.p` is op-level.
- **AF (synctex/wordcount)** — CE runs both directions in the compile dir; the
  word counter never re-counts for the same `doc_id` (flood fix).
- **AD (editors)** — Mantine 9 full-screen modals; toast-image dirty guard =
  undo-stack depth past the post-load baseline; `seedBaseline` must live at
  editor scope (a scope bug made it `ReferenceError` only on the success path).
- **AG-G (review panel)** — hydration must not clear on unknown `openDocName`;
  rehydrate when it resolves (`hydratedForRef`); accept/reject **must target
  the entry containing the test's needle** (first-match `.find(/reject/i)`
  clicks the wrong entry when earlier subtests left entries); S2 rehydrates
  from REST after actions — never rebuild from the empty OT tracker.
- **WakaTime (Wakepi step 1)** — enabled by default; local relay
  `http://ollitex-wakapi:3000/api/v1` (Go appends `/users/current`);
  not-linked heartbeats are 204 no-ops; settings card renders only when
  `ol-ExposedSettings` carries `wakaTimeEnabled` (slot `\x01WAKA33\x02` →
  `true`/`false` in `pageBase`).
- **Home logo** — unscoped `.toolbar-ol-logo` background rule for v1 toolbar
  AND rail (CSS var resolves the SVG).

## E2E environment traps learned (keep the future runs green)

- E2E modules are .cjs with the proven harness pattern (top-level `require`,
  `async function main()`, single browser context); repo-local
  `tests/e2e/node_modules/playwright(-core)` is a broken bootstrap — pin the
  global Playwright install.
- `page.evaluate` bodies are PLAIN JS (no TypeScript `as` casts).
- Fixture lookup uses Mongo `projects.name` (not `title`).
- `.entity-name` may carry an icon-ligature text prefix — match with `hasText`
  substring / `endsWith`, not exact text.
- File creation in e2e uses the Go upload contract: `POST /Project/:id/upload`
  with multipart `qqfile` + form `name` + query `folder_id` + `x-csrf-token`
  from `<meta name="ol-csrfToken">`.
- CM6 is virtualized — use keyboard navigation, not DOM queries, in tests.
- `requestFullscreen()` needs a REAL user gesture: Playwright `locator.click()`,
  not `page.evaluate(el.click())`.
- The comment composer (MentionsInput) hosts CodeMirror 6 in its shadow root —
  reach `.cm-content` through `host.shadowRoot`.
- The settings page template (`go/services/web/views/pages_data_p3c.go`) is a
  Go double-quoted string: HTML quotes are `\"` — a bare `"` byte terminates
  the constant (build error `unexpected <`); verify with
  `go build ./go/services/web/views/` before `make build-community`.
- Do NOT run two `make build-community` in parallel (docker mount/cache
  contention; mid-build edits are not picked up).
- `/tmp` gets cleaned between tool calls — persist probes in `/var/tmp`.
- `pgrep -f "make build-community"` is unreliable; check the build log for the
  `BUILDn_EXIT` marker instead.

# TYPST INTEGRATION PLAN (on `ext-6.3.0-port`)

Goal: integrate Typst **compile + editor** support — feature level approximating
LaTeX/tex files — from the work at `../typst_addon` (branch
`ext-6.3.0-typst`, 26 commits on top of our old base `f9ff6ce`), into this
branch (currently `36a3331ea2`, 66 commits past that same base).

Source material (in `../typst_addon`):
- `TYPST_INTEGRATION_PLAN.md` (1078 lines) — the "why" doc; executive decisions §0,
  architecture §2–§6, feature scope §7, DoD §12.
- `TYPST_PHASES.md` (168 lines) — the "do" ledger; P0–P4 **done & verified**, P5 (deploy)
  partially open in *their* env.
- `improve_typst.md` / `improve_typst2.md` — autocomplete improvement passes,
  **already implemented** in the branch (no extra work beyond porting the final code).
- Code: `services/clsi_typst/` (new compile service), `services/web/modules/typst/`
  (new module), plus ~20 hook points in web core.

---

## 1. Key findings (verify before implementing)

1. **This is a surgical port, NOT a branch merge.** The typst branch is a *mixed*
   development line: typst work **plus** their own parallel drift on branding/logos,
   keybinds UX, llm admin UI, admin-tools site settings, webdav/registration internals,
   e2e scaffolding. All of that we have already re-done (usually better and later).
   → Port **only the typst slice**; keep our version on every other conflict.
2. **The typst slice is essentially complete** (P0 spike, P1 service, P2 web dispatch,
   P3 parity features, P4 autocomplete depth — all ✅ in their ledger, with test
   evidence: 46 clsi_typst unit + 36 acceptance incl. real docker compiles → real PDFs;
   8/8 log-parser corpus; module frontend units green).
3. **clsi is untouched in their branch** — `clsi_typst` imports clsi's runtime files
   **in place** (`import DockerRunner from '../../clsi/app/js/DockerRunner.mjs'`).
   **Verified: our branch changed zero clsi runtime files since the base** (we added one
   test file only) → the in-place-import strategy works on our tree *as-is*. Keep it;
   do **not** re-extract `clsi-core` (their P1b, shelved — re-scope only if we ever run
   clsi and clsi_typst from different directories).
4. **Our deployment model makes their P5 simpler.** Their P5 (deploy) is written for a
   dev compose + a release image with separate services. **Our production overleaf
   container already runs clsi as a runit service inside the image**
   (`server-ce/runit/clsi-overleaf/run` → `node /overleaf/services/clsi/app.js`,
   docker.sock sibling model). → `clsi_typst` becomes the **next runit sibling**
   (`clsi_typst-overleaf`), and web reaches it at loopback
   (`CLSI_TYPEST_URL=http://127.0.0.1:3014`) — no new compose service, no nginx change,
   works identically in the e2e image stack.
5. **Runtime gate ships OFF by default** (`COMPILE_TYPEST_ENABLED`): menu hidden,
   clsi_typst answers 501. We control rollout; nothing breaks if the flag stays off.
6. **New browser deps** (both from their `services/web/package.json`):
   `codemirror-lang-typst@^0.4.0` (lezer typst grammar, wasm) and
   `@typstyle/typstyle-wasm-bundler@^0.13.18` (in-browser formatter, wasm-pack bundler
   target) + two webpack `webassembly/async` rules (they added them to
   `webpack.config.js` — we must re-add the same rules on top of our current webpack
   config).
7. **Compile is docker-only**, image `pandoc/typst:3-alpine` (typst 0.14.x, single
   static binary, no system fonts), sandbox contract copied from clsi (uid 33:33,
   `NetworkDisabled`, `CapDrop ALL`, seccomp profile from clsi — verified to load on
   alpine), `Entrypoint: []` quirk handled. Word count via wordometer-style
   compile-time injection + pdfjs-dist last-page text (their clsi_typst does this).
   **No SyncTeX / click-to-source, no LSP, no SVG output in v1** (their explicit
   decisions §0 — typst emits no synctex and native SVG carries no span data; the
   tinymist WASM renderer that provides those is deliberately out of scope).
8. **Editor parity delivered** (their P3/P4, salvaged from the old `../typst` fork +
   the `texlyre` reference and re-ported to this codebase): `.typ` syntax highlighting,
   lezer linter diagnostics in the lint gutter, `#heading` outline (core lezer
   enterNode + runtime extraction), bold `*…*`/italic `_…_` shortcuts, typstyle Format
   button in the toolbar (only for `.typ` docs), `#cite(§` from .bib /
   `#label(§` / `#include "…".typ` reference completion (improvement passes applied),
   "Typst project" new-project modal (empty + article template,
   `POST /project/new/typst`), compiler dropdown option, root-doc dropdown filtered
   to `*.typ`, ide-settings trims image/draft/etc. for typst, `typst` in
   `textExtensions`/`safeCompilers`/`validRootDocExtensions`, log parser mapping
   miette-style `error: [file] … ┌─ file:line:col` to click-to-line in the output log
   panel, wordcount modal reuse.
9. **Dual-route note (owner constraint):** every typst surface in the editor works on
   **both** `/editor` and `/Project` by construction (module components + core
   source-editor extensions are route-agnostic; our Mantine conversion does not touch
   these files). The new-project modal and compiler dropdown exist in the shared
   project-list/ide-settings code. Verify both routes in e2e.

## 2. What NOT to take (their non-typst drift — keep OUR version)

Their branch also carries parallel work that we have since re-done on top of the same
base — these "both changed" files are **ours wins** (do not port their hunks), with a
handful of exceptions noted in §3-B:

- branding: all logo/favicon/svg assets, `tools/logo/*`, `BRANDING.md`, `README.md`,
  `CREDITS.md`, footer views + `thin-footer.tsx`, `ciam_mixins.pug`,
  `unsupported-browser.pug`, `fat-footer-base.pug` (we own the branding state).
- e2e scaffolding: their `tests/e2e/*` predates ours (we have the evolved suite,
  fixtures, parity harness, PHASE gate) → keep ours wholesale.
- llm modules, admin-tools site-settings, keybinds/hotkeys UI, rail account menu,
  webdav/router internals, githubinterface, notifications, registration handler,
  `libraries/settings/Settings.js` (zero typst hunks in their diff — verified),
  `server-ce/Dockerfile|env.sh|settings.js|init_scripts` (their hunks are branding;
  we re-apply only what typst needs, §5), `locales/en.json` (add only the 3 typst
  keys listed below).
- `yarn.lock` / `.yarn/install-state.gz` — regenerate by running `yarn` after the dep
  merge, don't copy.

## 3. Port matrix

### A. Take verbatim (typst-only additions — no conflict on our side)

| Path | Notes |
|---|---|
| `services/clsi_typst/` (whole dir: app, config, entrypoint, seccomp ref, examples, vendor/wordometer.typ, tests, install_deps.sh, Dockerfile) | The compile service. In-place imports of our byte-identical clsi files — keep the relative layout (both live under `services/` in the image). |
| `services/web/modules/typst/` (whole module: index.mjs gate, TypstRouter, templates `basic`+`article`, frontend components, `languages/typst/*` (index/linter/shortcuts/completion/reference-completion/document-outline), tests) | New-project modal/menu, toolbar buttons, lezer language + linter + reference completion + outline. |
| `services/web/frontend/js/ide/log-parser/typst-log-parser.ts` | error→`file:line:col` parser (miette/typst 0.14 captured corpus). |
| `services/web/frontend/js/features/pdf-preview/util/output-files.ts` | `compiler==='typst'` log routing. |
| `services/web/frontend/js/features/project-list/components/new-project-button.tsx` + `new-project-button-modal.tsx` | blank_typst_project variant (flag-gated). |
| `services/web/frontend/js/features/ide-settings/components/compiler-settings/compiler-setting.tsx` + `root-document-setting.tsx` | Typst option (flag-gated) + `*.typ` root-doc filter. |
| `services/web/frontend/js/features/source-editor/languages/index.ts` | `LanguageDescription` for typst (async `typst()` factory). |
| `services/web/frontend/js/features/source-editor/extensions/doc-folder.ts`, `extensions/toolbar/commands.ts` (typstToggleBold/Italic), `extensions/toolbar/typst-format.ts` (typstyle wasm lazy), `utils/tree-operations/outline.ts` (Heading/HeadingMarker), `hooks/use-codemirror-scope.ts` | Editor wiring (all our-branch-untouched = clean). |
| `services/web/app/src/Features/Compile/ClsiManager.mjs`, `CompileController.mjs`, `CompileManager.mjs` | per-compiler dispatch (typst → `Settings.apis.clsi.typst`), main.typ root-doc pick, wordcount/status routing. (We never changed these since the base — take their version; then re-run **our** compile-related unit tests.) |
| `services/web/types/compile.ts`, `services/web/types/project-settings.ts` | `compiler?` field, `ProjectCompiler += 'typst'`. |
| `services/web/test/frontend/ide/log-parser/typst-log-parser.test.ts`, `test/frontend/features/source-editor/extensions/toolbar/typst-wrap-commands.test.ts`, `test/frontend/features/ide-settings/settings/compiler-setting.test.tsx`, `test/frontend/features/project-list/components/new-project-button.test.tsx`, `test/unit/src/Compile/ClsiManager.test.mjs` | their test additions (clsi diff in ours: none → take). |
| `services/clsi/test/unit/js/DockerRunner.regression.test.js` — **ours** (keep ours; theirs is the pre-existing sibling). | |

### B. Merge hunks (both sides touched; take ONLY typst hunks from theirs)

| Path | What to take from their diff |
|---|---|
| `services/web/config/settings.defaults.js` | `safeCompilers += 'typst'`; `apis.clsi.typst.url` (`CLSI_TYPEST_HOST`/`CLSI_TYPEST_URL`, default `127.0.0.1:3014`); `Settings.typst.enabled` (`COMPILE_TYPEST_ENABLED`, default **false**); `validRootDocExtensions += 'typ'`; `textExtensions += 'typ'`; `moduleImportSequence += 'typst'`; `overleafModuleImports.typstNewProjectMenu` + `overleafModuleImports.typstNewProjectModalWrapper` entries; append to `sourceEditorToolbarButtonGroups` (the `typst-toolbar-buttons` path). Merge into our file (ours has hub/llm/etc. additions — additive both ways). |
| `services/web/app/src/infrastructure/ExpressLocals.mjs` | `typstEnabled: !!(Settings.typst && Settings.typst.enabled)` (their *only* typst hunk — verified). |
| `services/web/frontend/js/features/ide-settings/context/settings-modal-context.tsx` | their `isTypst` branch (hide imageName/draft/stopOnFirstError/optimizeCompiles for typst projects). Rebase onto our P5/P7 version of the file. |
| `services/web/types/exposed-settings.ts` | `+ typstEnabled: boolean` (ours already has the hub keys). |
| `services/web/package.json` | `+ codemirror-lang-typst ^0.4.0`, `+ @typstyle/typstyle-wasm-bundler ^0.13.18` (dependencies). |
| `services/web/webpack.config.js` | the two `webassembly/async` rules for `typst_syntax_bg\.wasm$` and `typstyle_wasm_bg\.wasm$` (plus the matching `exclude` adjustments in their diff). Rebase onto our current webpack config. |
| `package.json` (root) | workspaces `+ "services/clsi_typst"`. |
| `services/web/locales/en.json` | `blank_typst_project`, `project_template`, `typst_project_template_article` (their 3 keys; diff-verified no reflow — re-verify after merge; keep `en.json` an object, never an array). |
| `services/web/translations-loader.js` | only if their diff adds typst-specific extraction config — check; likely none (i18n keys are ours' loader format). |

### C. Skip (their hunks, no typst value for us)

All of §2's list — including their `server-ce` branding hunks, their
`services/web/modules/*` llm/admin/webdav/registration hunks, their e2e scaffolding,
their `.github/workflows/e2e.yml`, `yarn.lock`.

## 4. Runtime architecture after port (on our tree)

```
Browser editor (CodeMirror, /editor AND /Project)
  typst language (wasm lezer) + linter + outline + completion + toolbar (typstyle)
  pdf-preview ← existing pdfjs + output-files.ts (compiler-aware log)
  new-project modal "Typst project" (flag-gated) → POST /project/new/typst (module)

web (services/web)
  ClsiManager: compiler === 'typst' → Settings.apis.clsi.typst.url (127.0.0.1:3014)
               otherwise → existing clsi (unchanged)
  module 'typst' mounted only when Settings.typst.enabled (off → 404 /project/new/typst)

clsi_typst (runit sibling in the overleaf image, port 3014)
  same HTTP contract as clsi (compile/stop/status/wordcount/clear-cache/output)
  TypstRunner: `typst compile` in pandoc/typst:3-alpine (uid 33, no net, cap-drop,
  seccomp, Entrypoint: [])
  wordcount: wordometer.typ injection + pdfjs last-page parse → texcount envelope
  feature gate: COMPILE_TYPEST_ENABLED=false → 501 on compile (hard fail, never
  "LaTeX-as-typst")
  imports clsi files in place (byte-identical on both branches — verified)
```

## 5. Deployment on OUR stack (simpler than their P5)

Our production/e2e overleaf container runs every backend as a **runit service inside
the image** (clsi already works this way). So:

1. **Image**: `server-ce/Dockerfile` — add `services/clsi_typst` to the services that
   get `yarn`-installed (root workspaces already includes it after merge) and copied to
   `/overleaf/services/clsi_typst` *alongside* `services/clsi` (layout matters for the
   in-place imports). clsi stays byte-identical.
2. **runit**: add `server-ce/runit/clsi_typst-overleaf/run` (mirror
   `clsi-overleaf/run`): docker.sock group wiring (same block), `source
   /etc/overleaf/env.sh`, `LISTEN_ADDRESS=127.0.0.1`,
   `exec /sbin/setuser www-data node /overleaf/services/clsi_typst/app.js >>
   /var/log/overleaf/clsi_typst.log 2>&1`. (Check how our Dockerfile enumerates
   runit services — mirror whatever registers `clsi-overleaf`.)
3. **Env (settings + env.sh)**: `COMPILE_TYPEST_ENABLED` (owner-controlled rollout,
   **default false**), `CLSI_TYPEST_URL=http://127.0.0.1:3014` (loopback — web and
   clsi_typst are in the same container), `TYPST_DOCKER_IMAGE=pandoc/typst:3-alpine`
   (pinnable), `TYPST_ENABLE_DOCKER=true`. Document in our runbook.
4. **Image pull**: `docker pull pandoc/typst:3-alpine` on the build host and the e2e
   host (stock image, no build).
5. **nginx**: no change (loopback; their §6.2 decision "no extra nginx needed for v1"
   fits our single-container model exactly).

## 6. Phased execution (each phase ends GREEN before the next starts)

**T1 — Pure additions, flag OFF (zero behaviour change to running system)**
- Port §3-A verbatim (clsi_typst, module, log parser, pdf-preview/new-project/
  ide-settings/source-editor files, types, their tests).
- Port §3-B merge hunks.
- GATES: `yarn install` (PnP resolves the 2 new deps + workspace); `yarn eslint` 0
  errors (typst files + touched files); clsi_typst unit suite green **against OUR
  clsi** (`yarn workspace @overleaf/clsi-typst test:unit`); web vitest suites green
  (ClisiManager, compiler-setting, new-project-button, typst-log-parser,
  typst-wrap-commands, typst-foundation); **clsi's own suite unchanged + green**
  (our DockerRunner regression test included); webpack build GREEN (wasm rules);
  `COMPILE_TYPEST_ENABLED` unset → `/project/new/typst` 404, menu hidden, and a
  hand-rolled `POST …/compile` with compiler `typst` to clsi_typst → **501**.
  No e2e delta required (nothing user-visible yet).

**T2 — clsi_typst live inside the image (docker compile → real PDF)**
- §5.1 + §5.2 image + runit wiring; `pandoc/typst:3-alpine` pulled on both hosts.
- GATES: docker exec overleaf(e2e) → clsi_typst process up; acceptance suite green
  (36 tests incl. real compiles): basic → readable PDF, stop/clear/allowed-image
  400, error-corpus 200+log, 501 gate, wordcount envelope; **latex regression**:
  existing e2e compile smoke (tex project → PDF) green on the new image.

**T3 — Flag ON, end-to-end in the e2e stack (feature level ≈ tex)**
- Set `COMPILE_TYPEST_ENABLED=true` in the e2e stack env only.
- New e2e spec `specs/typst.test.e2e.ts` (write our own — never `test.each`):
  1. flag on: new-project modal shows "Typst project" (empty + article radio);
  2. create (both templates) → editor opens `main.typ`;
  3. compile → **PDF preview visible** (same assertions the tex smoke uses);
  4. error doc (corpus case 04) → output log lists the error with
     `file:line:col`, click jumps to the line;
  5. wordcount modal returns numbers for the article doc;
  6. editor features: `.typ` highlighted (cm-editor + typst class present), outline
     lists `#heading` entries, `#cite(§` completion shows the .bib key, bold/italic/
     Format buttons visible for the `.typ` doc (and absent for a `.tex` doc);
  7. **both routes**: repeat steps 2–5 on `/editor/<pid>` and `/Project/<pid>`;
  8. flag OFF on a second context: menu hidden + compile 501;
  9. pageerror-less + axe (critical/serious = 0) on the new-project modal + editor.
- GATES: that spec 2/2+ green; full existing editor gate (PHASE ratchet) still green;
  LaTeX projects compile exactly as before (dual-route).

**T4 — Production rollout (owner flip)**
- Rebuild image (`server-ce && make all`), cycle overleafserver, verify: clsi_typst
  runit up, latex compile regression on a live project, flag OFF → no typst surfaces
  (menu hidden, 501) — **then** owner flips `COMPILE_TYPEST_ENABLED=true` in prod env
  and re-verifies with their own account (no fixture users in prod).
- GATES: image-id == git HEAD; bundle markers; runit status; owner smoke pass.

## 7. Risks & mitigations

| Risk | Mitigation |
|---|---|
| In-place clsi imports break if we ever reorganize clsi | clsi runtime is byte-identical now (verified); document the constraint in `clsi_typst/README.md` (their P1b note already explains it). Re-extract to a shared lib **only** as a separate task if the layout ever changes. |
| Wasm deps under Yarn 4 PnP + webpack (typst grammar, typstyle) | Their branch proved the exact versions + webpack `webassembly/async` rules work with this repo's webpack (our config is a superset); T1 gate catches any PnP/webpack mismatch before runtime. |
| Merging `settings.defaults.js` / `settings-modal-context.tsx` / webpack onto our newer versions | §3-B lists the exact hunks; all additive. Re-run the module/hub/llm unit suites + webpack build — our P8/M-wave code is the canary. |
| en.json corruption (past incident) | diff the 3 added keys by hand after merge; `i18n-lint` + object-shape check. |
| Feature bleed: typst menu appears for non-admin/flag-off | Runtime gate is `ExposedSettings.typstEnabled`; T1 gate asserts hidden menu + 501 with flag off. |
| clsi (LaTeX) regressions | clsi untouched in the port; clsi suite + live tex compile are gates at T1/T2/T3. |
| typst 0.14 quirks (`#cite` broken in the stock image's build — their F3.8 note) | templates ship citation-free; `.bib` works for F4 completion; document in the README. Bumping the typst image version is a 1-line env change (`TYPST_DOCKER_IMAGE`) when we want modern `#cite`. |
| Dual-route drift (`/editor` vs `/Project`) | typst surfaces live in shared core/module code; T3 gate repeats key steps on both routes. |

## 8. Owner decisions (before T4)

1. **Rollout**: keep `COMPILE_TYPEST_ENABLED` default **false** until you flip it (assumed yes — matches "flags off by default" in their P5 and owner safety preference).
2. **Typst image pin**: stock `pandoc/typst:3-alpine` (typst 0.14.x) for v1 — OK, or want a newer typst (newer `#cite` etc., but template/fixture re-verification needed)?
3. **v2 depth (out of scope now, listed for later)**: tinymist WASM in-browser renderer (click-to-source + live preview), LSP (typlst/tinymist), SVG output with span data, markdown→typst conversion. None planned.

## 9. Definition of done (on THIS branch)

- [ ] T1–T4 gates green, LaTeX path bit-for-bit unchanged (clsi zero diff, clsi suite
      identical baseline).
- [ ] With flag on, a fresh Typst project (empty AND article) created from the UI modal
      compiles to a PDF in the existing preview on **both** `/editor` and `/Project`;
      errors parse to `file:line:col` with click-to-line; wordcount returns
      wordometer-class numbers; linter/outline/`#cite`/`#label`/`#include`
      completion/Format/bold/italic work on `.typ`.
- [ ] Flag off: menu hidden, `/project/new/typst` 404, clsi_typst 501 (verified both).
- [ ] Sandbox contract: compile runs in `pandoc/typst` container, uid 33:33,
      `NetworkDisabled`, `CapDrop ALL`, seccomp on (re-verify on the pinned image).
- [ ] No synctex output for typst (assert in e2e — their DoD item).
- [ ] Prod rollout gated behind owner flip; rollback = env flag (code stays inert).
- [ ] Docs: `clsi_typst/README.md` + runbook section (envs, image, how to add another
      compiler later) + README "Typst support" note.

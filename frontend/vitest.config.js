/**
 * Frontend (frontend/) workspace test config — the /hub, editor-renovation
 * and Typst T1 suites.
 *
 * Canonical layout note (2026-09 reorg): the Node-app unit suite stays at
 * services/web (its canonical location + config; `make unit` runs both).
 * These projects run with this file's directory as vitest's root: relative
 * `..` import chains therefore never exit the root boundary (a vite
 * resolution break observed when the same files were rooted elsewhere).
 */
const { defineConfig } = require('vitest/config')

const COVERAGE_ENABLED = process.env.COVERAGE_UNIT_TESTS === 'true'

let reporterOptions = {}
if (process.env.CI && process.env.JUNIT_ROOT_SUITE_NAME) {
  reporterOptions = {
    maxWorkers: '50%',
    reporters: [
      'default',
      [
        'junit',
        {
          classnameTemplate: `${process.env.JUNIT_ROOT_SUITE_NAME}.{filename}`,
        },
      ],
    ],
    outputFile: 'data/reports/junit-vitest.xml',
  }
}

const path = require('node:path')

// This package directory (frontend/): hub + renovation tests live under it.
const rootId = __dirname
// Node-app env setup (shared UV_THREADPOOL / worker env) at its canonical home.
const SW_TEST_UNIT = path.join(__dirname, '..', 'services', 'web', 'test', 'unit')

// Babel macro stub for the vitest runner (esbuild does not run babel macros):
// legacy components call importOverleafModules('x') at module scope; resolve
// the macro module to an empty registration stub so the import graph loads.
const MACRO_STUB = path.join(
  rootId,
  'modules/ollitex-hub/test/frontend/stubs/import-overleaf-module.mjs'
)
const overleafMacroStub = {
  name: 'ollitex-hub-macro-stub',
  enforce: 'pre',
  resolveId(source) {
    if (/[\\/]macros\/import-overleaf-module\.macro(\.js)?$/.test(source)) {
      return MACRO_STUB
    }
    return null
  },
}

const REPO_ROOT = path.join(__dirname, '..')

module.exports = defineConfig({
  // Node-app and hub import graphs reach services/web (outside this package
  // root); allow the repo root in vite's fs server.
  server: { fs: { allow: [REPO_ROOT] } },
  plugins: [overleafMacroStub],
  resolve: {
    alias: [
      {
        // 'abort-controller'@3 ships `main: dist/abort-controller` (no
        // extension), which the PnP ESM loader cannot infer. Alias to the
        // concrete file (CJS-style resolution target) so vitest's SSR
        // chain resolves it.
        find: /^abort-controller$/,
        replacement: path.join(
          __dirname,
          '..', '.yarn/cache',
          'abort-controller-npm-3.0.0-2f3a9a2bcb-90ccc50f01.zip',
          'node_modules', 'abort-controller', 'dist', 'abort-controller.js'
        ),
      },
      { find: '@modules', replacement: path.join(rootId, 'modules') },
      { find: '@', replacement: path.join(rootId, 'js') }, // the app source is this package's js/
    ],
  },
  esbuild: {
    // Repo components rely on the automatic JSX runtime (babel preset-react
    // classic-free); esbuild's default 'transform' runtime requires a React
    // import in every file — align vitest with the webpack build.
    jsx: 'automatic',
  },
  test: {
    setupFiles: [path.join(SW_TEST_UNIT, 'unit-env.mjs'), path.join(SW_TEST_UNIT, 'bootstrap.mjs')],
    globals: false,
    isolate: false,
    passWithNoTests: true, // in case there are no tests from one project or other in a module
    testNamePattern: process.env.TEST_NAME_PATTERN || undefined,
    projects: [
      {
        extends: true,
        test: {
          name: 'ModuleFrontend',
          // Module-level frontend logic specs (bib-editor, diagram, languagetool,
          // latex-editor, python-runner, toast-image, llm): their SUTs live in
          // this package (modules/<m>/frontend), so the specs live with them.
          include: [
            path.join(rootId, 'modules/*/test/unit/**/*.test.mjs'),
            path.join(rootId, 'test/unit/src/**/*.test.mjs'),
          ],
          fileParallelism: true,
        },
      },
      {
        extends: true,
        test: {
          name: 'HubFrontend',
          // /hub React integration test suite (owner mandate 2026-09-07):
          // fast, browser-free component tests covering every hub surface
          // (rail, leaves, menus, settings, admin sections, theme logic).
          environment: 'jsdom',
          environmentOptions: {
            jsdom: {
              url: 'https://www.test-overleaf.com/',
              pretendToBeVisual: true,
            },
          },
          setupFiles: [path.join(rootId, 'modules/ollitex-hub/test/frontend/vitest.setup.ts')],
          include: [path.join(rootId, 'modules/ollitex-hub/test/frontend/**/*.test.{ts,tsx}')],
          exclude: [path.join(rootId, 'modules/ollitex-hub/test/frontend/helpers/**')],
          fileParallelism: true,
        },
      },
      {
        extends: true,
        test: {
          name: 'EditorRenovation',
          // editor renovation (EDITOR_RENOVATION_PLAN.md): baseline specs that
          // freeze the current editor surfaces' behavior before renovation,
          // and the renovated surfaces' conformance afterwards (P0e onward).
          environment: 'jsdom',
          environmentOptions: {
            jsdom: {
              url: 'https://www.test-overleaf.com/',
              pretendToBeVisual: true,
            },
          },
          setupFiles: [path.join(rootId, 'test/frontend/editor-renovation/vitest.setup.ts')],
          include: [path.join(rootId, 'test/frontend/editor-renovation/**/*.test.{ts,tsx}')],
          fileParallelism: true,
        },
      },
      {
        extends: true,
        test: {
          name: 'TypstT1',
          // Typst T1 (TYPST_INTEGRATION_PLAN.md §6): log parser (0.15.1
          // miette format), compiler-setting + new-project-modal flag gating,
          // toolbar wrap commands, and the module's typst language support --
          // ported from the `typst_addon` branch (ext-6.3.0-typst). The repo's
          // mocha test:frontend harness cannot load .tsx spec files under
          // Node 22 + yarn PnP, so these run here under the same vitest+jsdom
          // setup as EditorRenovation.
          //
          // globals: true -- these specs are written in the mocha idiom
          // (bare describe/it/beforeEach, chai expect), which vitest's global
          // suite API is source-compatible with.
          globals: true,
          server: {
            deps: {
              // Resolve via vite (where the abort-controller file alias above
              // applies) instead of the PnP ESM loader, which cannot resolve
              // this package's extensionless `main`.
              inline: [/abort-controller/],
            },
          },
          environment: 'jsdom',
          environmentOptions: {
            jsdom: {
              url: 'https://www.test-overleaf.com/',
              pretendToBeVisual: true,
            },
          },
          setupFiles: [
            path.join(rootId, 'test/frontend/editor-renovation/vitest.setup.ts'),
            path.join(rootId, 'test/frontend/typst/codemirror-shim.ts'),
          ],
          include: [
            path.join(rootId, 'test/frontend/ide/log-parser/typst-log-parser.test.ts'),
            path.join(rootId, 'test/frontend/features/ide-settings/settings/compiler-setting.test.tsx'),
            path.join(rootId, 'test/frontend/features/project-list/components/new-project-button.test.tsx'),
            path.join(rootId, 'test/frontend/features/source-editor/extensions/toolbar/typst-wrap-commands.test.ts'),
            path.join(rootId, 'modules/typst/test/frontend/languages/*.test.ts'),
          ],
          fileParallelism: true,
        },
      },
    ],
    ...reporterOptions,
    // 2026-09 (IMPROVEMENTS P0.3): TpdsProjectFlusher (fully mocked, no I/O)
    // intermittently blew the default 5s under parallel load on the shared box
    // (passes standalone x3) — and its beforeEach (dynamic import of the
    // module under test) hit the 10s hook budget under the full-parallel run.
    // 30s keeps real failures fast while removing the contention flake.
    hookTimeout: 30_000,
    testTimeout: process.env.CI ? 30_000 : 20_000,
    coverage: {
      enabled: COVERAGE_ENABLED,
      // Add 'sequential' / 'parallel' to the folder
      reportsDirectory: `data/coverage/esm-unit-${(process.env.JUNIT_ROOT_SUITE_NAME || 'all').split(' ').pop()}`,
      include: [
        path.join(rootId, 'modules/*/index.mjs'),
        path.join(rootId, 'modules/*/app/src/**/*.{js,mjs}'),
      ],
      provider: 'istanbul',
      reporters: ['console-details', 'clover'],
      all: true,
    },
  },
})

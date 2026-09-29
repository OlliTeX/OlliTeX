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

// Babel macro stub for the vitest runner (esbuild does not run babel macros):
// legacy components call importOverleafModules('x') at module scope; resolve
// the macro module to an empty registration stub so the import graph loads.
const MACRO_STUB = path.join(
  __dirname,
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

module.exports = defineConfig({
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
      { find: '@modules', replacement: path.join(__dirname, 'modules') },
      { find: '@', replacement: path.join(__dirname, '..', '..', 'frontend/js') },
    ],
  },
  esbuild: {
    // Repo components rely on the automatic JSX runtime (babel preset-react
    // classic-free); esbuild's default 'transform' runtime requires a React
    // import in every file — align vitest with the webpack build.
    jsx: 'automatic',
  },

  test: {
    setupFiles: ['./test/unit/unit-env.mjs', './test/unit/bootstrap.mjs'],
    globals: false,
    isolate: false,
    passWithNoTests: true, // in case there are no tests from one project or other in a module
    testNamePattern: process.env.TEST_NAME_PATTERN || undefined,
    projects: [
      {
        extends: true,
        test: {
          name: 'Parallel',
          include: [
            'modules/*/test/unit/**/*.test.mjs',
            'test/unit/src/**/*.test.mjs',
          ],
          sequence: {
            groupOrder: 2,
          },
          exclude: ['**/*.sequential.test.mjs'],
          fileParallelism: true,
        },
      },
      {
        extends: true,
        test: {
          name: 'Sequential',
          sequence: {
            groupOrder: 1,
          },
          include: [
            'modules/*/test/unit/**/*.sequential.test.mjs',
            'test/unit/src/**/*.sequential.test.mjs',
          ],
          fileParallelism: false,
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
        'app.mjs',
        'app/**/*.{js,mjs}',
        'modules/*/index.mjs',
        'modules/*/app/src/**/*.{js,mjs}',
      ],
      exclude: ['app/src/Features/Metadata/packageMapping.mjs'],
      provider: 'istanbul',
      reporters: ['console-details', 'clover'],
      all: true,
    },
  },
})

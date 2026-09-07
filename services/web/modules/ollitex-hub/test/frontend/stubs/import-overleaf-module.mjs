// Test stub for the webpack-era babel macro `import-overleaf-module.macro`.
//
// At build time, babel-plugin-macros evaluates importOverleafModules('x')
// at compile time and rewrites it into static imports of every module that
// self-registers under that macro name. In the vitest frontend runner
// esbuild does not run babel macros, so we resolve the macro module to this
// stub: the integration tests that embed legacy hub leaves do not depend on
// dynamically registered widgets (they assert the core UI flows), and an
// empty registration list keeps the import graph loadable.
export default function importOverleafModules() {
  return []
}

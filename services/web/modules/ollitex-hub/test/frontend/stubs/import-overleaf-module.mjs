// Test stub for the webpack-era babel macro `import-overleaf-module.macro`.
//
// At build time, babel-plugin-macros evaluates importOverleafModules('x')
// at compile time and rewrites it into static imports of every module that
// self-registers under that macro name. In the vitest frontend runner
// esbuild does not run babel macros, so we resolve the macro module to this
// stub: the integration tests that embed legacy hub leaves do not depend on
// dynamically registered widgets (they assert the core UI flows), and an
// empty registration list keeps the import graph loadable.
import TypstNewProjectMenu from '@modules/typst/frontend/js/components/typst-new-project-menu'
import TypstNewProjectModalWrapper from '@modules/typst/frontend/js/components/typst-new-project-modal-wrapper'
export default function importOverleafModules(name) {
  // The real build (babel-plugin-macros) registers every module that
  // self-declares under that macro name. In vitest we only need one of
  // those for the new-project dropdown's 'Blank Typst project' item to
  // render (modules/typst/frontend/js/components/typst-new-project-menu.tsx);
  // provide it with the same shape the macro's output has (entry.import is
  // the module namespace). Other names stay empty as before — no other
  // test renders their registration-dependent UI.
  if (name === 'typstNewProjectMenu') {
    return [{ name, import: { default: TypstNewProjectMenu } }]
  }
  if (name === 'typstNewProjectModalWrapper') {
    return [{ name, import: { default: TypstNewProjectModalWrapper } }]
  }
  return []
}

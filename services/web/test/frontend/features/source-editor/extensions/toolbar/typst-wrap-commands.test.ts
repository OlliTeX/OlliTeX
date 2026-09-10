/**
 * Typst bold/italic toolbar commands (F2.8).
 *
 * Core-side test (the commands live in core `commands.ts` alongside the
 * LaTeX wrap commands so the module's shortcuts / toolbar can reuse them).
 * Run via `yarn test:frontend`. Uses the repo's standard
 * `CodemirrorTestSession` (real CodeMirror `view` + `applyCommand`) and the
 * `viewHelpers` chai plugin (provides `expect(cm).line(n)`).
 *
 * Asserts the Typst-style markers are wrapped in by the command
 * (typst bold = *..., typst italic = _...), not the LaTeX control words.
 */
import { expect, use } from 'chai'
import {
  typstToggleBold,
  typstToggleItalic,
} from '../../../../../../frontend/js/features/source-editor/extensions/toolbar/commands'

import { CodemirrorTestSession, viewHelpers } from '../../helpers/codemirror'

use(viewHelpers)

describe('typstToggleBold (F2.8)', function () {
  it('wraps a selected range in *...* (typst bold) and inserts it', function () {
    const cm = new CodemirrorTestSession(['<hello> world'])
    cm.applyCommand(typstToggleBold)
    // wrapRanges('*', '*') inserts * + content + *. No LaTeX \textbf is
    // emitted (typst bold uses *...* markup).
    expect(cm).line(1).to.not.contain('\\textbf')
    expect(cm).line(1).to.contain('*')
    expect(cm).line(1).to.contain('hello')
  })

  it('does not dispatch when the view is readOnly', function () {
    const readOnlyView = {
      get state() {
        return { readOnly: true }
      },
      dispatch(_change: never) {
        // should not be called
        throw new Error('dispatch should not be called in readOnly view')
      },
    } as never /* EditorView stand-in: only state.readOnly consulted */
    const res = typstToggleBold(readOnlyView)
    expect(res).to.equal(false)
  })
})

describe('typstToggleItalic (F2.8)', function () {
  it('wraps a selected range in _..._ (typst italic) and inserts it', function () {
    const cm = new CodemirrorTestSession(['<hello> world'])
    cm.applyCommand(typstToggleItalic)
    expect(cm).line(1).to.not.contain('\\textit')
    expect(cm).line(1).to.contain('_')
    expect(cm).line(1).to.contain('hello')
  })
})

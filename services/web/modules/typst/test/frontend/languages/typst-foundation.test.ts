/**
 * Typst module frontend unit tests (F2.12).
 *
 * Run via the `yarn test:frontend` script (mocha + frontend bootstrap) —
 * it picks up this file under modules/typst/test/frontend.
 *
 * These are module-local, Node-side smoke tests (no webpack, so the lezer
 * wasm grammar is not loaded here). They assert:
 *   - the language module export shape (F2.6); `typst()` is an async
 *     factory (the wasm grammar is lazy-loaded inside it, so importing this
 *     module in Node is safe — the factory's `.then(support)` chain is what
 *     CodeMirror's `languageDataAt` awaits at real runtime).
 *   - the linter wiring + LintSource shape (F2.7).
 *
 * The bold/italic commands live in core (`frontend/js/.../toolbar/commands`)
 * and are covered by the core test
 * `test/frontend/features/source-editor/extensions/toolbar/typst-wrap-commands.test.ts`.
 * The full wasm grammar + typstyle build is covered by the webpack prod
 * build and the browser e2e pass.
 */
import { expect } from 'chai'
import type { Diagnostic } from '@codemirror/lint'
import { EditorState } from '@codemirror/state'

describe('modules/typst: language module (F2.6)', function () {
  it('exports async typst() factory, lezer props, and HighlightStyle', function () {
    return import(
      '../../../frontend/js/languages/typst'
    ).then(m => {
      // dynamic import resolves the module without running `typst()` (which
      // lazily imports the wasm grammar).
      expect(typeof m.typst, 'typst() export').to.equal('function')
      // `typstHighlight` (the lezer props used to build the wasm grammar)
      // is a plain object of styleTags — importable without wasm.
      expect(
        m.typstHighlight,
        'typstHighlight (lezer props) export',
      ).to.not.equal(undefined)
      expect(
        m.TypstHighlightStyle,
        'TypstHighlightStyle export',
      ).to.not.equal(undefined)
    })
  })
})

describe('modules/typst: linter (F2.7)', function () {
  it('typstLinter returns the [linter, transactionExtender] pair', function () {
    return import(
      '../../../frontend/js/languages/typst/linter'
    ).then(m => {
      const ext = m.typstLinter() as unknown as readonly unknown[]
      expect(ext).to.be.instanceOf(Array)
      expect(ext.length).to.equal(2)
    })
  })

  it('typstLintSource produces shape-valid diagnostics on a clean tree', function () {
    return import(
      '../../../frontend/js/languages/typst/linter'
    ).then(m => {
      const view = {
        state: EditorState.create({ doc: 'hello world' }),
      } as never /* EditorView stand-in: only state is consulted */
      // syntaxTree() with no lezer language is a dummy tree (no Error
      // nodes), so the LintSource reports no diagnostics — assert the
      // contract (array of Diagnostic-shaped objects).
      const diagnostics = m.typstLintSource(view) as readonly Diagnostic[]
      expect(Array.isArray(diagnostics)).to.equal(true)
      for (const d of diagnostics) {
        expect(typeof d.from).to.equal('number')
        expect(typeof d.to).to.equal('number')
        expect(typeof d.message).to.equal('string')
      }
    })
  })
})

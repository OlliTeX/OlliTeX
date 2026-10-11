/**
 * List-item continuation keymap (source editor):
 *   - Enter after an `\item` inserts a new `\item` (indent preserved;
 *     `\item[]` with the cursor inside the brackets for `description` items,
 *     mirroring the optional argument; trailing whitespace after the cursor
 *     is consumed, not carried over)
 *   - Shift-Enter always inserts a plain newline
 *   - No continuation for empty lists; none inside verbatim blocks
 *
 * Behavior spec ported from the upstream keymap feature (closes
 * overleaf#1384; origin <https://github.com/MarkQuach12/overleaf.git>
 * branch feature/source-editor-list-item-continuation, commits 448ff7d6 +
 * 90867398 — see CREDITS.md), run on this repo's vitest+jsdom harness
 * (the mocha/cypress test:frontend harness is retired in this tree; same
 * pattern as the TypstT1 source-editor specs).
 *
 * The tests drive REAL keydown events through a real EditorView built with
 * the FULL `latex()` extension set (wiring in languages/latex/index.ts
 * included), so a regression in the wiring fails here too.
 */
import { afterEach, describe, expect, it } from 'vitest'
import { EditorView } from '@codemirror/view'
import { keymaps } from '../../../../../../js/features/source-editor/extensions/keymaps'
import { latex } from '../../../../../../js/features/source-editor/languages/latex'

const views: EditorView[] = []

function openDoc(content: string): EditorView {
  const view = new EditorView({
    doc: content,
    // production editor keymap stack (defaultKeymap + history + lint +
    // folding + scroll) + full language support (incl. shortcuts() +
    // listItemContinuation() — the wiring under test)
    extensions: [keymaps, latex()],
  })
  views.push(view)
  return view
}

afterEach(() => {
  while (views.length) views.pop()?.destroy()
})

function cursorToLineEnd(view: EditorView, line: number) {
  const l = view.state.doc.line(line)
  view.dispatch({ selection: { anchor: l.to, head: l.to }, scrollIntoView: false })
}

function pressEnter(view: EditorView, shift = false) {
  // CM attaches keydown handlers to view.contentDOM (inputState.ensureHandlers),
  // not view.dom — dispatch to the contenteditable so the keymap plugin sees it.
  view.contentDOM.dispatchEvent(
    new KeyboardEvent('keydown', {
      key: 'Enter',
      shiftKey: shift,
      bubbles: true,
      cancelable: true,
    })
  )
}

function type(view: EditorView, text: string) {
  // state-native replacement (replaceSelection = insertText minus its extra
  // undo grouping); avoids the @codemirror/commands named-export interop
  // issue under yarn PnP in this harness.
  view.dispatch(view.state.replaceSelection(text))
}

function lines(view: EditorView): string[] {
  // doc.lines is a LineIterator (cm6 6.5.x) — just use the doc string
  return view.state.doc.toString().split('\n')
}

describe('list-item continuation (Enter → \\item)', () => {
  it('continues an itemize list with a new \\item on Enter', () => {
    const v = openDoc('\\begin{itemize}\n\\item first\n\\end{itemize}')
    cursorToLineEnd(v, 2)
    pressEnter(v)
    type(v, 'second')
    expect(lines(v)).toEqual([
      '\\begin{itemize}',
      '\\item first',
      '\\item second',
      '\\end{itemize}',
    ])
  })

  it('preserves indentation when continuing a list', () => {
    const v = openDoc('\\begin{itemize}\n  \\item first\n\\end{itemize}')
    cursorToLineEnd(v, 2)
    pressEnter(v)
    type(v, 'second')
    expect(lines(v)).toEqual([
      '\\begin{itemize}',
      '  \\item first',
      '  \\item second',
      '\\end{itemize}',
    ])
  })

  it('mirrors the \\item[] optional argument and parks the cursor inside the brackets', () => {
    const v = openDoc('\\begin{description}\n\\item[first] desc\n\\end{description}')
    cursorToLineEnd(v, 2)
    pressEnter(v)
    // cursor is inside the square brackets: typing fills them
    type(v, 'Label')
    expect(lines(v)).toEqual([
      '\\begin{description}',
      '\\item[first] desc',
      '\\item[Label] ',
      '\\end{description}',
    ])
  })

  it('does not carry trailing whitespace onto the new item', () => {
    // line 3 is "\item " (stray trailing space); cursor sits BEFORE the space
    const v = openDoc('\\begin{itemize}\n\\item \n\\end{itemize}')
    const l = v.state.doc.line(2)
    v.dispatch({ selection: { anchor: l.to - 1, head: l.to - 1 } })
    pressEnter(v)
    type(v, 'second')
    expect(lines(v)).toEqual([
      '\\begin{itemize}',
      '\\item',
      '\\item second',
      '\\end{itemize}',
    ])
  })

  it('inserts another item (does not exit) on Enter at an empty item', () => {
    const v = openDoc('\\begin{itemize}\n\\item first\n\\item\n\\end{itemize}')
    cursorToLineEnd(v, 3)
    pressEnter(v)
    type(v, 'second')
    expect(lines(v)).toEqual([
      '\\begin{itemize}',
      '\\item first',
      '\\item',
      '\\item second',
      '\\end{itemize}',
    ])
  })

  it('does not fabricate a first \\item in an empty list', () => {
    const v = openDoc('\\begin{itemize}\n\\end{itemize}')
    cursorToLineEnd(v, 1)
    pressEnter(v)
    // plain newline, no \item generated
    expect(lines(v)).toEqual(['\\begin{itemize}', '', '\\end{itemize}'])
  })

  it('does not insert an \\item inside a verbatim block nested in a list', () => {
    const v = openDoc(
      '\\begin{itemize}\n\\item first\n\\begin{verbatim}\ncode\n\\end{verbatim}\n\\end{itemize}'
    )
    cursorToLineEnd(v, 4) // the "code" line
    pressEnter(v)
    type(v, 'more')
    expect(lines(v)).toEqual([
      '\\begin{itemize}',
      '\\item first',
      '\\begin{verbatim}',
      'code',
      'more',
      '\\end{verbatim}',
      '\\end{itemize}',
    ])
  })

  it('Shift-Enter inserts a plain newline instead of a new item', () => {
    const v = openDoc('\\begin{itemize}\n\\item first\n\\end{itemize}')
    cursorToLineEnd(v, 2)
    pressEnter(v, true)
    type(v, 'plain')
    expect(lines(v)).toEqual([
      '\\begin{itemize}',
      '\\item first',
      'plain',
      '\\end{itemize}',
    ])
  })

  it('outside any list, Enter stays a plain newline', () => {
    const v = openDoc('paragraph\nmore here')
    cursorToLineEnd(v, 1)
    pressEnter(v)
    type(v, 'x')
    expect(lines(v)).toEqual(['paragraph', 'x', 'more here'])
  })
})

/**
 * 2026-09 (OlliTeX, owner batch: selected-text word count): bridge between
 * File → "Word count" (menu-bar) and the word-count modal.
 *
 * Reviewer guidance on the community "selected text word count" contribution:
 * keep the action OUT of the floating menu; File → Word count ALWAYS shows the
 * whole-document count and ADDS a "Selection" section when text is selected.
 *
 * The menu handler captures the active CodeMirror editor's selection through
 * the DOM-attached EditorView (no React context crossing needed), stores it
 * here, and the modal renders the selection section from this store.
 */
import { EditorView } from '@codemirror/view'

export type EditorSelectionInfo = {
  from: number
  to: number
  text: string
  /** true when the active editor is a Typst document */
  isTypst: boolean
}

let pendingSelection: EditorSelectionInfo | null = null

/**
 * Capture the main selection of the first rendered editor that has one.
 * Returns null (and clears the pending selection) when nothing is selected —
 * in that case File → Word count shows the whole-document count only.
 */
export function captureEditorSelection(): EditorSelectionInfo | null {
  pendingSelection = null
  try {
    const editorDom = document.querySelector<HTMLElement>('.cm-editor')
    if (!editorDom) {
      return pendingSelection
    }
    const view = EditorView.findFromDOM(editorDom)
    if (!view || view.state.selection.main.empty) {
      return pendingSelection
    }
    const { from, to } = view.state.selection.main
    if (from >= to) {
      return pendingSelection
    }
    const text = view.state.doc.sliceString(from, to)
    // CodeMirror marks the active language as a class on the editor root
    // (e.g. "lang-latex" / "lang-typst").
    const isTypst =
      editorDom.classList.contains('lang-typst') ||
      /lang-typst\b/.test(editorDom.className)
    pendingSelection = { from, to, text, isTypst }
    return pendingSelection
  } catch {
    pendingSelection = null
    return pendingSelection
  }
}

export function getPendingSelection(): EditorSelectionInfo | null {
  return pendingSelection
}

export function clearPendingSelection(): void {
  pendingSelection = null
}

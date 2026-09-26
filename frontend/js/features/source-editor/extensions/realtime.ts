// Yjs real-time bridge (S4 flip — D19/D24).
//
// Replaces the OT (ShareJS) bridge that lived here. The CM6 ↔ Y.Text sync
// is the D24 engine bridge (frontend/js/features/ide-react/collab):
//
//   * LOCAL  — every CM6 transaction's change spans are replayed as
//     granular Y.Text ops (one transaction per edit), via `syncExtension`.
//   * REMOTE — any Y.Text change from the room (server-relayed peer edits,
//     seed) is mirrored into the CM6 document through the container's sink
//     (a `userEvent:'input.remote'` dispatch, out of the undo stack).
//
// `EditorFacade`, `trackChangesAnnotation` and `ChangeDescription` are
// kept as compatibility exports for OT-era modules that still import them
// (they are inert under the Yjs engine — D25 disables the OT review
// surfaces).

import { Annotation, type ChangeSpec, type Transaction } from '@codemirror/state'
import { EditorView, ViewPlugin } from '@codemirror/view'
import { EventEmitter } from 'events'
import type RangesTracker from '@overleaf/ranges-tracker'
import { debugConsole } from '@/utils/debugging'
import { DocumentContainer } from '@/features/ide-react/editor/document-container'
import { syncExtension } from '@/features/ide-react/collab'

type Origin = 'remote' | 'undo' | 'reject' | undefined

export type ChangeDescription = {
  origin: Origin
  inserted: boolean
  removed: boolean
}

/**
 * A custom extension that connects the CodeMirror 6 editor to the
 * currently open Yjs document (the Go collab service, /collab/<projectId>).
 */
export const realtime = (
  { currentDoc }: { currentDoc: DocumentContainer },
  handleError: (error: Error) => void
) => {
  const bridge = ViewPlugin.define(view => {
    currentDoc.attachToCM6(view)

    return {
      destroy() {
        currentDoc.detachFromCM6()
      },
    }
  })

  // NOTE: not a view plugin, so shouldn't get removed.
  const ensureBridge = EditorView.updateListener.of(update => {
    if (!update.view.plugin(bridge)) {
      const message = 'The realtime extension has been destroyed!!'
      debugConsole.warn(message)
      currentDoc.trigger('error', new Error(message), {
        doc_id: currentDoc.doc_id,
      })
      handleError(new Error(message))
    }
  })

  return [
    bridge,
    // The D24 local direction: CM6 change spans → Y.Text.
    syncExtension(currentDoc.sync),
    ensureBridge,
  ]
}

// ------------------------------------------------------------------------
// Compatibility surface (OT-era imports still reference these; all inert
// under the Yjs engine — D25).
// ------------------------------------------------------------------------

export class EditorFacade extends EventEmitter {
  constructor(public view: EditorView) {
    super()
  }

  getValue(): string {
    return this.view.state.doc.toString()
  }

  cmChange(changes: ChangeSpec, _origin?: string): void {
    this.view.dispatch({ changes })
  }

  cmInsert(position: number, text: string): void {
    this.cmChange({ from: position, insert: text })
  }

  cmDelete(position: number, text: string): void {
    this.cmChange({ from: position, to: position + text.length })
  }

  attachShareJs(_shareDoc: unknown, _maxDocLength?: number): void {
    // Inert under the Yjs engine (D25).
  }

  detachShareJs(): void {
    // Inert under the Yjs engine (D25).
  }

  handleUpdateFromCM(
    _transactions: readonly Transaction[],
    _ranges?: RangesTracker
  ): void {
    // Inert: the D24 local bridge (syncExtension) handles CM6→Y.Text.
  }

  setTrackChangesUserId(userId: string | null): void {
    if (userId != null) {
      debugConsole.log(
        '[cm6] tracked changes requested (Yjs engine — DISABLED, D25)'
      )
    }
  }
}

export const trackChangesAnnotation = Annotation.define()

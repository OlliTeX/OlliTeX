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
// `trackChangesAnnotation` and `ChangeDescription` are kept as
// compatibility exports for OT-era modules that still import them (they
// are inert under the Yjs engine — D25 disables the OT review surfaces).

import {
  Annotation,
  type Extension,
  Transaction,
} from '@codemirror/state'
import { EditorView, ViewPlugin, type ViewUpdate } from '@codemirror/view'
import { debugConsole } from '@/utils/debugging'
import getMeta from '@/utils/meta'
import { DocumentContainer } from '@/features/ide-react/editor/document-container'
import {
  syncExtension,
  spanBodies,
  type LocalChange,
  type TrackedChangeBody,
} from '@/features/ide-react/collab'

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
    // D40 (d5 pending piece — editor-side tracked-change creation): while this
    // session's track-changes is ON, capture local edits as
    // server-authoritative change records (d2 propose/apply, d10 read path).
    trackedChangesCapture(currentDoc),
    ensureBridge,
  ]
}

// ------------------------------------------------------------------------
// D40 — editor-side tracked-change capture (source-editor extension).
//
// Gated on `currentDoc.track_changes_as` (set by
// editor-manager-context from the panel's 'toggle-track-changes' intent /
// the `track_changes` REST state). Every LOCAL edit span becomes one
// change record on the D40 create surface
// (POST /project/:pid/doc/:doc/changes, d5/d10): zero-width insert
// {content, start, end: start} or delete {start, end}. Remote mirrors
// (userEvent 'input.remote') never capture. Best-effort by design — a
// capture failure must NEVER block typing (Node parity: review writes are
// best-effort around the document flow).
// ------------------------------------------------------------------------

// (The pure span→body contract, `spanBodies`, lives in the collab engine
// package — ide-react/collab/capture.ts — and is re-exported here for
// host-side consumers.)
export { spanBodies, type TrackedChangeBody } from '@/features/ide-react/collab'

const isRemoteMirror = (update: ViewUpdate) =>
  update.transactions.some(
    tx => tx.annotation(Transaction.userEvent) === 'input.remote'
  )

export const trackedChangesCapture = (
  currentDoc: DocumentContainer
): Extension => {
  // AC (owner 2026-10-07): typing "bbbbbbbbbb" must NOT create ten change
  // records. Strategy mimics the ORIGINAL overleaf (6.3.0 editor-core,
  // libraries/overleaf-editor-core/lib/file_data/tracked_change_list.js +
  // tracked_change.js + tracking_props.js):
  //   * a tracked change = range + {type, userId, ts};
  //   * two changes MERGE iff same type (insert|delete) + same user +
  //     TOUCHING/overlapping ranges (TrackingProps.canMergeWith +
  //     Range.canMerge) — no time window, no gaps;
  //   * the list is MERGED ONCE, AT THE END ("merged only once at the end,
  //     for performance and to avoid intermediate ranges getting
  //     incorrectly merged"), SORTED BY START before folding;
  //   * merged ts = MIN(ts) (start of the burst).
  // Our REST record model is the direct analogue (kind = type, one local
  // user, touching = contiguous): buffered bodies are sorted by start and
  // contiguous same-kind runs fold into one record; deletes never merge
  // with inserts (canMergeWith); disjoint runs stay separate.
  const FLUSH_DELAY_MS = 400
  let pending: TrackedChangeBody[] = []
  let flushTimer: ReturnType<typeof setTimeout> | null = null

  type InsertBody = { content: string; start: number; end: number }
  const isInsert = (b: TrackedChangeBody): b is InsertBody =>
    typeof (b as { content?: unknown }).content === 'string' &&
    (b as { start?: number; end?: number }).start ===
      (b as { start?: number; end?: number }).end

  const coalesce = (bodies: TrackedChangeBody[]): TrackedChangeBody[] => {
    // Deletes: separate records, in original order (single-kind record
    // model; canMergeWith forbids delete+insert merges).
    const deletes = bodies.filter(b => !isInsert(b)).map(b => ({ ...b }))
    // Inserts: sort by start (original _mergeRanges), then fold
    // contiguous runs (Range.touches: end === other.start).
    const inserts = bodies.filter(isInsert).slice().sort((a, b) => a.start - b.start)
    const folded: InsertBody[] = []
    for (const cur of inserts) {
      const last = folded[folded.length - 1]
      if (last && cur.start === last.start + last.content.length) {
        last.content += cur.content
      } else {
        folded.push({ start: cur.start, end: cur.end, content: cur.content })
      }
    }
    return [...deletes, ...folded]
  }

  const flush = () => {
    if (flushTimer) {
      clearTimeout(flushTimer)
      flushTimer = null
    }
    const bodies = pending
    pending = []
    if (!bodies.length) return
    const pid = getMeta('ol-project_id')
    if (!pid) return
    for (const body of coalesce(bodies)) {
      void fetch(
        `/project/${pid}/doc/${encodeURIComponent(
          currentDoc.doc_id
        )}/changes`,
        {
          method: 'POST',
          credentials: 'same-origin',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': getMeta('ol-csrfToken') ?? '',
          },
          body: JSON.stringify(body),
        }
      ).catch(e =>
        debugConsole.warn(
          '[d40] tracked-change capture failed: ' + String(e)
        )
      )
    }
    // D40 live-echo: the server now holds fresh tracked-change records —
    // refresh the ranges hydration (throttled in the ranges provider) so
    // the new tracked edit renders without a reload.
    void currentDoc.trigger('ranges:hydrate')
  }

  const beforeUnload = () => {
    // never lose the typing burst if the page unloads mid-flush
    const bodies = pending
    pending = []
    if (!bodies.length) return
    const pid = getMeta('ol-project_id')
    if (!pid) return
    for (const body of coalesce(bodies)) {
      fetch(
        `/project/${pid}/doc/${encodeURIComponent(
          currentDoc.doc_id
        )}/changes`,
        {
          method: 'POST',
          credentials: 'same-origin',
          headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': getMeta('ol-csrfToken') ?? '',
          },
          body: JSON.stringify(body),
        }
      )
    }
  }
  window.addEventListener('beforeunload', beforeUnload)

  return EditorView.updateListener.of(update => {
    if (!update.docChanged || isRemoteMirror(update)) return
    if (currentDoc.track_changes_as == null) return
    const pid = getMeta('ol-project_id')
    if (!pid) return
    const spans: LocalChange[] = []
    update.changes.iterChanges((fromA, toA, _fromB, _toB, inserted) => {
      spans.push({ from: fromA, to: toA, insert: inserted.toString() })
    })
    let bodies: TrackedChangeBody[] = []
    for (const spanBody of spanBodies(spans)) {
      const body = spanBody as Record<string, unknown>
      const bStart = body.start as number
      const bEnd = body.end as number
      if (bEnd > bStart && (body.content as unknown) === undefined) {
        // d11b: carry the DELETED text (old-space slice from startState)
        // so the server stores it and the panel renders op.d. The d5
        // delete body {start,end} stays shape-compatible (the create
        // surface accepts content on deletes). Capped at 4 KiB (honest
        // pin for pathological deletions). **Explicit kind**: the create
        // surface infers kind from content-emptiness, so a delete WITH
        // content must pin kind: 'delete' or it would record as insert.
        bodies.push({
          ...body,
          kind: 'delete',
          content: update.startState.sliceDoc(bStart, bEnd).slice(0, 4096),
        })
      } else {
        bodies.push(spanBody)
      }
    }
    if (!bodies.length) return
    pending.push(...bodies)
    if (!flushTimer) {
      flushTimer = setTimeout(flush, FLUSH_DELAY_MS)
    }
  })
}

// (The former EditorFacade OT-compat surface was removed in the S2 sweep
// 2026-10-06 — no importer remained after the Yjs flip; the D24 bridge
// above is the live local-direction path.)

export const trackChangesAnnotation = Annotation.define()

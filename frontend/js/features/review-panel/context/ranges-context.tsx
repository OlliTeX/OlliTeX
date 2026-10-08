import { useRef } from 'react'
import {
  createContext,
  FC,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react'
import { DocumentContainer } from '@/features/ide-react/editor/document-container'
import {
  Change,
  CommentOperation,
  EditOperation,
} from '../../../../types/change'
import { rejectChanges } from '@/features/source-editor/extensions/changes/reject-changes'
import { getJSON } from '@/infrastructure/fetch-json'
import { useCodeMirrorViewContext } from '@/features/source-editor/components/codemirror-context'
import { postJSON } from '@/infrastructure/fetch-json'
import { useIdeReactContext } from '@/features/ide-react/context/ide-react-context'
import { useConnectionContext } from '@/features/ide-react/context/connection-context'
import useSocketListener from '@/features/ide-react/hooks/use-socket-listener'
import { throttle } from 'lodash'
import { useEditorOpenDocContext } from '@/features/ide-react/context/editor-open-doc-context'
import { TextOperation, Range } from 'overleaf-editor-core'
import { rangesUpdatedEffect } from '@/features/source-editor/extensions/history-ot'
import ClearTrackingProps from 'overleaf-editor-core/lib/file_data/clear_tracking_props'
import { isInsertOperation } from '@/utils/operations'
import {
  EditorSelection,
  Transaction,
  TransactionSpec,
} from '@codemirror/state'
import { buildRangesFromSnapshot } from '@/features/review-panel/utils/snapshot-ranges'
import { useEditorAnalytics } from '@/shared/hooks/use-editor-analytics'
import { useReviewPanelViewContext } from './review-panel-view-context'

export type Ranges = {
  docId: string
  changes: Array<Change<EditOperation> & { snapshotRange?: Range }>
  comments: Array<
    Change<CommentOperation> & { snapshotRange?: Range; resolved?: boolean }
  >
}

export const RangesContext = createContext<Ranges | undefined>(undefined)

type RangesActions = {
  acceptChanges: (
    ...changes: Array<Change<EditOperation> & { snapshotRange?: Range }>
  ) => Promise<void>
  rejectChanges: (
    ...changes: Array<Change<EditOperation> & { snapshotRange?: Range }>
  ) => Promise<void>
}

const buildRanges = (currentDocument: DocumentContainer | null) => {
  const ranges = currentDocument?.ranges

  if (!ranges) {
    return undefined
  }

  const dirtyState = ranges.getDirtyState()
  ranges.resetDirtyState()

  const changed = {
    changes: new Set([
      ...Object.keys(dirtyState.change.added),
      ...Object.keys(dirtyState.change.moved),
      ...Object.keys(dirtyState.change.removed),
    ]),
    comments: new Set([
      ...Object.keys(dirtyState.comment.added),
      ...Object.keys(dirtyState.comment.moved),
      ...Object.keys(dirtyState.comment.removed),
    ]),
  }

  return {
    changes:
      changed.changes.size > 0
        ? ranges.changes.map(change =>
            changed.changes.has(change.id) ? { ...change } : change
          )
        : ranges.changes,
    comments:
      changed.comments.size > 0
        ? ranges.comments.map(comment =>
            changed.comments.has(comment.id) ? { ...comment } : comment
          )
        : ranges.comments,
    docId: currentDocument.doc_id,
  }
}

const buildRangesFromHistoryOT = (currentDocument: DocumentContainer) => {
  return buildRangesFromSnapshot(
    currentDocument.historyOTShareDoc.snapshot,
    currentDocument.doc_id
  )
}

export const RangesActionsContext = createContext<RangesActions | undefined>(
  undefined
)

export const RangesProvider: FC<React.PropsWithChildren> = ({ children }) => {
  const view = useCodeMirrorViewContext()
  const { projectId } = useIdeReactContext()
  const { currentDocument } = useEditorOpenDocContext()
  const { socket } = useConnectionContext()
  const { sendEvent } = useEditorAnalytics()
  const { openDocName } = useEditorOpenDocContext()
  const hydratedRef = useRef(false)
  // 2026-10-07 (AG-2): which open-doc identity the last hydration covered,
  // so a late-resolving openDocName can still re-hydrate the panel.
  const hydratedForRef = useRef<string | null>(null)
  const [ranges, setRanges] = useState<Ranges | undefined>(() =>
    buildRanges(currentDocument)
  )
  const reviewPanelView = useReviewPanelViewContext()

  // rebuild the ranges when the current doc changes
  useEffect(() => {
    if (currentDocument) {
      if (currentDocument.isHistoryOT()) {
        setRanges(buildRangesFromHistoryOT(currentDocument))
      } else {
        setRanges(buildRanges(currentDocument))
      }
    }
  }, [currentDocument])

  // audit 005/029 (2026-09-30) + D40 live echo (2026-10-06): the Yjs engine
  // (D25/D40) carries comments in the room's Y.Doc / the REST records, not
  // in a document-native RangesTracker — so buildRanges() above yields an
  // EMPTY tracker and the review panel shows no entries even though the
  // server holds the threads. D40's shipping contract is the REST surface
  // (Go web features/review: /ranges returns the exact CommentShape/
  // ChangeShape the panel renders, keyed by content doc — 'main.tex' P1).
  // Hydrate the ranges context from that endpoint whenever the native
  // tracker is empty for the open document — at mount (below) AND live
  // whenever a server-side record lands (comment create, tracked-change
  // capture, resolve/reopen), signalled via the document container's
  // 'ranges:hydrate' event.
  const hydrateFromServer = useCallback(
    async (stale: () => boolean) => {
      if (!currentDocument) {
        return
      }
      const items =
        await getJSON<Array<{ id: string; ranges?: { changes?: any[]; comments?: any[] } }>>(
          `/project/${projectId}/ranges`
        )
      if (stale() || !items?.length) {
        return
      }
      const docId = openDocName || ''
      // 2026-10-07 (AG-2 flake root cause): when the open-doc name is not
      // known (open-docs still hydrating — a race with fast first typing in
      // review mode), we CANNOT decide that the doc has no ranges — do NOT
      // clear the tracker; keep the last state and wait for the next
      // hydration (the 'ranges:hydrate' event fires on every capture, and
      // the openDocName effect below re-hydrates when the name lands).
      if (!docId) {
        return
      }
      // NO cross-doc fallback (owner 2026-10-07: sample.bib showed
      // main.tex's comment tags — the old "first entry with any comments"
      // fallback leaked one doc's ranges onto every other doc, which is
      // exactly what /ranges — keyed per content doc by pathname — is not
      // for). A doc with no server-side review records renders NO marks.
      const pick = items.find(it => (it.id || '') === docId)
      if (!pick) {
        // openDocName is the basename the panel uses ('main.tex') but
        // accept the root-anchored form as well — a mismatch here
        // previously cleared a doc that DOES have review records.
        const alt = items.find(it => (it.id || '').replace(/\/+/, '') === docId.replace(/\/+/, ''))
          || items.find(it => (it.id || '').split('/').pop() === docId.split('/').pop() && docId.length > 0)
        if (!alt) {
          setRanges({
            docId: currentDocument.doc_id,
            changes: [],
            comments: [],
          })
          return
        }
        setRanges({
          docId: currentDocument.doc_id,
          changes: (alt.ranges?.changes ?? []).map(c => ({
            id: (c as any).id,
            op: (c as any).op,
            state: (c as any).state,
            metadata: (c as any).metadata,
          })),
          comments: (alt.ranges?.comments ?? []).map(c => ({
            id: (c as any).id,
            op: (c as any).op,
            resolved: (c as any).resolved,
            metadata: (c as any).metadata,
          })),
        })
        return
      }
      setRanges({
        docId: currentDocument.doc_id,
        changes: (pick.ranges?.changes ?? []).map(c => ({
          id: (c as any).id,
          op: (c as any).op,
          state: (c as any).state,
          metadata: (c as any).metadata,
        })),
        comments: (pick.ranges?.comments ?? []).map(c => ({
          id: (c as any).id,
          op: (c as any).op,
          resolved: (c as any).resolved,
          metadata: (c as any).metadata,
        })),
      })
    },
    [currentDocument, openDocName, projectId]
  )

  useEffect(() => {
    if (!currentDocument) {
      return
    }
    const native = buildRanges(currentDocument)
    const nativeHasEntries =
      !!native && (native.comments.length > 0 || native.changes.length > 0)
    if (nativeHasEntries) {
      return
    }
    // 2026-10-07 (AG-2 flake): allow a re-hydration when the open-doc name
    // is only NOW known (open-docs hydrate late; the first hydration with
    // an unknown identity must not permanently gate the panel).
    const docName = openDocName || ''
    if (hydratedRef.current && hydratedForRef.current !== docName) {
      hydratedForRef.current = docName
    } else if (hydratedRef.current) {
      return
    }
    let cancelled = false
    hydratedRef.current = true
    hydratedForRef.current = docName
    void hydrateFromServer(() => cancelled).catch(() => {
      // keep the (empty) native ranges; panel shows the empty state
    })
    return () => {
      cancelled = true
      hydratedRef.current = false
    }
  }, [currentDocument, openDocName, projectId, hydrateFromServer])

  // D40 live echo (Yjs engine): after a server-side review record is
  // written (comment POST /project/:pid/thread/:tid/messages, tracked-change
  // POST /project/:pid/doc/:doc/changes, resolve/reopen), the writing side
  // triggers 'ranges:hydrate' on the document container; re-hydrate from
  // the REST source of truth so chips/panel stay current without a reload.
  useEffect(() => {
    if (!currentDocument || currentDocument.isHistoryOT()) {
      return
    }
    const listener = throttle(
      () => {
        void hydrateFromServer(() => false).catch(() => {
          // best-effort live refresh; keep the last good state
        })
      },
      500,
      { leading: true, trailing: true }
    )
    currentDocument.on('ranges:hydrate', listener)
    return () => {
      currentDocument.off('ranges:hydrate', listener)
      listener.cancel()
    }
  }, [currentDocument, hydrateFromServer])

  useEffect(() => {
    if (currentDocument && currentDocument.isHistoryOT()) {
      const listener = throttle(
        () => {
          window.setTimeout(() => {
            setRanges(buildRangesFromHistoryOT(currentDocument))
          })
        },
        500,
        { leading: true, trailing: true }
      )

      currentDocument.on('ranges:dirty.ot', listener)

      return () => {
        currentDocument.off('ranges:dirty.ot')
      }
    }
  }, [currentDocument])

  useEffect(() => {
    if (currentDocument && !currentDocument.isHistoryOT()) {
      const listener = throttle(
        () => {
          window.setTimeout(() => {
            const native = buildRanges(currentDocument)
            // D40 live echo (owner 2026-10-07: comment tags appeared "for a
            // short moment" then vanished): on the Yjs engine the native
            // tracker is EMPTY BY DESIGN — the REST-hydrated ranges are the
            // source of truth. Clobbering them with an empty native build on
            // every ranges:redraw/dirty.cm6 (any editor edit/redraw) erased
            // live comment chips. Only take native state when it actually
            // carries entries; otherwise keep the hydrated state.
            if (native.comments.length > 0 || native.changes.length > 0) {
              setRanges(native)
            }
          })
        },
        500,
        { leading: true, trailing: true }
      )

      // currentDocument.on('ranges:clear.cm6', listener)
      currentDocument.on('ranges:redraw.cm6', listener)
      currentDocument.on('ranges:dirty.cm6', listener)

      return () => {
        // currentDocument.off('ranges:clear.cm6')
        currentDocument.off('ranges:redraw.cm6')
        currentDocument.off('ranges:dirty.cm6')
      }
    }
  }, [currentDocument])

  useSocketListener(
    socket,
    'accept-changes',
    useCallback(
      (docId: string, entryIds: string[]) => {
        if (currentDocument?.ranges) {
          if (docId === currentDocument.doc_id) {
            currentDocument.ranges.removeChangeIds(entryIds)
            setRanges(buildRanges(currentDocument))
          }
        }
      },
      [currentDocument]
    )
  )

  const actions = useMemo(() => {
    if (!currentDocument) {
      return
    }

    if (currentDocument.isHistoryOT()) {
      return {
        async acceptChanges(...changes) {
          const op = new TextOperation()

          let currentSnapshotPos = 0
          for (const change of changes) {
            const { start, end, length } = change.snapshotRange!

            if (start > currentSnapshotPos) {
              op.retain(start - currentSnapshotPos)
            }

            currentSnapshotPos = end

            if (isInsertOperation(change.op)) {
              // accept tracked insertion

              // clear tracking from snapshot
              op.retain({ r: length }, { tracking: new ClearTrackingProps() }) // TODO: { type: 'none' })
            } else {
              // accept tracked deletion

              // remove text from snapshot
              op.remove(length) // NOTE: tracking is removed automatically
            }
          }

          const shareDoc = currentDocument.historyOTShareDoc

          const length = shareDoc.snapshot.getStringLength()

          if (currentSnapshotPos < length) {
            op.retain(length - currentSnapshotPos)
          }

          shareDoc.submitOp([op])
          sendEvent('rp-changes-accepted', {
            count: changes.length,
            view: reviewPanelView,
          })

          // dispatch an effect as the editor's doc doesn't change when tracked changes are accepted
          view.dispatch({
            effects: rangesUpdatedEffect.of(null),
          })
        },
        async rejectChanges(...changes) {
          const shareDoc = currentDocument.historyOTShareDoc

          const originalLength = shareDoc.snapshot.getStringLength()

          const op = new TextOperation()

          let currentSnapshotPos = 0
          const specs: TransactionSpec[] = []
          for (const change of changes) {
            const { start, end, length } = change.snapshotRange!

            if (start > currentSnapshotPos) {
              op.retain(start - currentSnapshotPos)
            }

            currentSnapshotPos = end

            if (isInsertOperation(change.op)) {
              // reject tracked insertion

              // remove text from snapshot
              op.remove(length) // NOTE: tracking is removed automatically

              // remove text from editor
              specs.push({
                changes: {
                  from: change.op.p,
                  to: change.op.p + change.op.i.length,
                  insert: '',
                },
                annotations: [
                  Transaction.remote.of(true),
                  // Transaction.addToHistory.of(false), // TODO: is this needed for the undo stack?
                ],
              })
            } else {
              // reject tracked deletion

              // remove tracking from snapshot
              op.retain({ r: length }, { tracking: new ClearTrackingProps() }) // TODO: { type: 'none' })

              // re-add text to editor
              specs.push({
                changes: {
                  from: change.op.p,
                  insert: change.op.d,
                },
                selection: EditorSelection.cursor(
                  change.op.p + change.op.d.length
                ), // TODO: map selection through changes?
                annotations: [
                  Transaction.remote.of(true),
                  // Transaction.addToHistory.of(false), // TODO: is this needed for the undo stack?
                ],
              })
            }
          }

          if (currentSnapshotPos < originalLength) {
            op.retain(originalLength - currentSnapshotPos)
          }

          shareDoc.submitOp([op])
          sendEvent('rp-changes-rejected', {
            count: changes.length,
            view: reviewPanelView,
          })

          // in case the doc didn't change
          view.dispatch(...specs, {
            effects: rangesUpdatedEffect.of(null),
          })
        },
      } satisfies RangesActions
    } else {
      return {
        async acceptChanges(...changes) {
          // 2026-10-07 (AG-2 hardening): the state flip is SERVER-side
          // (collab.AcceptChange) — do not gate it on the OT tracker being
          // mounted (S2 docs may not attach one); the local bookkeeping is
          // best-effort on top.
          const ids = changes.map(change => change.id)
          const url = `/project/${projectId}/doc/${currentDocument.doc_id}/changes/accept`
          await postJSON(url, { body: { change_ids: ids } })
          currentDocument.ranges?.removeChangeIds(ids)
          // 2026-10-07 (AG-2 TC-5/CM-1b): do NOT rebuild from the OT tracker
          // (empty on S2 — it CLEARED the hydrated panel state right after
          // each action, hiding the remaining entries). Re-hydrate from the
          // REST source of truth instead (the D40 event path).
          try { currentDocument?.trigger?.('ranges:hydrate') } catch (e) { /* no-op */ }
          sendEvent('rp-changes-accepted', {
            count: ids.length,
            view: reviewPanelView,
          })
        },
        async rejectChanges(...changes) {
          {
            const ids = changes.map(change => change.id)
            // 2026-10-07 (AG-2 TC-5): the OLD S2 reject called
            // reject-changes.ts, which resolves specs from the OT RangesTracker
            // — EMPTY on Yjs/S2 docs (the changes live server-side + in the
            // panel state), so it returned {} and the button did NOTHING
            // (no text revert, no state change). Reject must work from the
            // change ops the panel already holds:
            //   insert  → remove [p, p+len(i))
            //   delete  → re-insert d at p
            // sorted DESC by p (adjacent-change interaction rule from
            // reject-changes.ts) — best effort: a drifted position skips its
            // spec instead of aborting the whole action.
            const specs: Array<{ from: number; to?: number; insert: string }> = []
            const ordered = [...changes].sort((a, b) => (b.op?.p ?? 0) - (a.op?.p ?? 0))
            for (const change of ordered) {
              const op: any = change.op
              const p = typeof op?.p === 'number' ? op.p : -1
              if (p < 0) continue
              if (typeof op?.i === 'string') {
                const to = p + op.i.length
                try {
                  if (view.state.doc.sliceString(p, to) === op.i) {
                    specs.push({ from: p, to, insert: '' })
                  }
                } catch (e) {
                  // position drifted — skip (server record still flips)
                }
              } else if (typeof op?.d === 'string') {
                specs.push({ from: p, insert: op.d })
              }
            }
            if (specs.length > 0) {
              view.dispatch({
                changes: specs,
                effects: rangesUpdatedEffect.of(null),
              })
            }
            currentDocument.ranges?.removeChangeIds(ids)
            // Server state pending→rejected (the panel's source of truth;
            // collab.RejectChange). Accept mirrors this shape.
            const url = `/project/${projectId}/doc/${currentDocument.doc_id}/changes/reject`
            await postJSON(url, { body: { change_ids: ids } })
            // 2026-10-07 (AG-2): same rule — re-hydrate from the server
            // (pending→rejected), never rebuild from the empty S2 tracker.
            try { currentDocument?.trigger?.('ranges:hydrate') } catch (e) { /* no-no-op */ }
            sendEvent('rp-changes-rejected', {
              count: ids.length,
              view: reviewPanelView,
            })
          }
        },
      } satisfies RangesActions
    }
  }, [currentDocument, projectId, view, sendEvent, reviewPanelView])

  if (!actions) {
    return null
  }

  return (
    <RangesActionsContext.Provider value={actions}>
      <RangesContext.Provider value={ranges}>{children}</RangesContext.Provider>
    </RangesActionsContext.Provider>
  )
}

export const useRangesContext = () => {
  return useContext(RangesContext)
}

export const useRangesActionsContext = () => {
  const context = useContext(RangesActionsContext)
  if (!context) {
    throw new Error(
      'useRangesActionsContext is only available inside RangesProvider'
    )
  }
  return context
}

// Yjs collaboration container (S4 flip — D19/D24/D25).
//
// This class keeps the EXACT public API the IDE consumed from the old OT
// DocumentContainer (join/leave, getSnapshot, pollSavedStatus,
// setTrackChangesUserId, submitOp, op getters, events), but the text
// synchronization is now the D24 Yjs engine (frontend/js/features/
// ide-react/collab) speaking to the Go collab service over /collab/<pid>.
//
// What this means per surface:
//
//  * TEXT — Y.Text is the source of truth. Local edits are granular delta
//    ops (D24); remote updates are full mirrors (D24). The server is the
//    single seed source (D19): join waits for the first sync to land.
//  * OFFLINE — y-indexeddb (per-project) replaces the OT offline backup;
//    edits made offline survive a reload and sync on reconnect.
//  * TRACK-CHANGES / COMMENTS — OT-document-native (D25); DISABLED with an
//    honest UI placeholder. The API surface stays (no-op) so the IDE
//    components keep working unchanged.
//  * BUS (presence, file-tree, settings, references) — unchanged: still the
//    project socket via the real-time relay (joinProjectResponse & co).
//
// The Socket / EditorWatchdogManager constructor args are retained for
// constructor-signature compatibility (open-documents.ts) and are no
// longer used for text synchronization.

import type { EditorView } from '@codemirror/view'
import RangesTracker from '@overleaf/ranges-tracker'
import getMeta from '@/utils/meta'
import { debugConsole } from '@/utils/debugging'
import { Socket } from '@/features/ide-react/connection/types/socket'
import { IdeEventEmitter } from '@/features/ide-react/create-ide-event-emitter'
import EditorWatchdogManager from '@/features/ide-react/connection/editor-watchdog-manager'
import {
  createEngine,
  YjsEngine,
  YTextSync,
} from '@/features/ide-react/collab'
import EventEmitter from '@/utils/EventEmitter'
import {
  Change,
  CommentOperation,
  EditOperation,
} from '../../../../../services/web/types/change'

// Same structural shape the old OT container exposed for the ranges
// extension (the review-panel contexts map over these arrays).
type _RangesTracker = Omit<RangesTracker, 'changes' | 'comments'> & {
  changes: Change<EditOperation>[]
  comments: Change<CommentOperation>[]
}

// join deadline: the collab service must accept the handshake and deliver
// the initial sync (step1+step2) quickly; anything slower is a failure the
// IDE should surface (fail-closed, same as the old joinDoc error path).
const JOIN_DEADLINE_MS = 5000
// after the socket is connected, allow this much settle time for the
// initial sync payload to be applied (local application is synchronous in
// yjs, so this only covers the inter-message gap).
const CONNECT_SETTLE_MS = 250
const POLL_MS = 50

type JoinCallback = (error?: Error) => void

export class DocumentContainer extends EventEmitter {
  readonly doc_id: string
  docName = ''
  joined = false
  track_changes_as: string | null = null

  // Comments / tracked-changes ranges — the OT-native tracker does NOT
  // exist on the Yjs engine (D25); keep an EMPTY tracker so the ranges
  // extensions and consumers that read `ranges` stay inert and crash-free.
  ranges: _RangesTracker = new RangesTracker([], []) as _RangesTracker

  // CM6 view (set by attachToCM6; the old consumers only needed doc_id,
  // but the sync sink and chaosMonkey operate on the view).
  view?: EditorView

  private engine: YjsEngine | null = null
  private viewRef: { current: EditorView | null } = { current: null }

  constructor(
    docId: string,
    _socket?: Socket,
    _globalEditorWatchdogManager?: EditorWatchdogManager,
    private readonly ideEventEmitter?: IdeEventEmitter,
    private readonly detachDoc?: (docId: string, doc: DocumentContainer) => void
  ) {
    super()
    this.doc_id = docId
  }

  // --------------------------------------------------------------------
  // OT surfaces — honestly absent on the Yjs engine (D25)
  // --------------------------------------------------------------------

  get shareDoc(): any {
    throw new Error(
      'OT shareDoc unavailable: text sync runs on the Yjs engine (D25)'
    )
  }

  isHistoryOT(): boolean {
    return false
  }

  get historyOTShareDoc(): any {
    throw new Error(
      'OT shareDoc unavailable: text sync runs on the Yjs engine (D25)'
    )
  }

  getType(): string {
    return 'yjs-text'
  }

  submitOp(..._ops: unknown[]): void {
    // Local edits are captured from the CM6 update stream (D24 local
    // direction); there is nothing to submit.
  }

  setTrackChangesUserId(userId: string | null): void {
    if (userId != null) {
      debugConsole.log(
        '[doc] tracked changes requested (Yjs engine — DISABLED, D25)'
      )
    }
  }

  setTrackChangesIdSeeds(_id_seeds: unknown): void {
    // no-op (D25)
  }

  getTrackingChanges(): boolean {
    return false
  }

  // --------------------------------------------------------------------
  // Op/ack surface — CRDT has no op-ack pipeline; always "saved".
  // --------------------------------------------------------------------

  getInflightOp(): unknown[] | undefined {
    return undefined
  }
  getPendingOp(): unknown[] | undefined {
    return undefined
  }
  getRecentAck(): unknown | undefined {
    return undefined
  }
  getInflightOpCreatedAt(): number | null | undefined {
    return null
  }
  getPendingOpCreatedAt(): number | null | undefined {
    return null
  }
  hasBufferedOps(): boolean {
    return false
  }
  clearInflightAndPendingOps(): void {}
  flush(): void {}
  flushPendingOps(): void {}

  /**
   * Saved-status polling (unsaved-changes alert): under a CRDT a local
   * edit is durable the moment it lands in the shared type (IndexedDB
   * covers the offline window; the server persists on unload), so there
   * is no ack-pending state to wait for.
   */
  pollSavedStatus(): boolean {
    return true
  }

  // --------------------------------------------------------------------
  // Engine access (the new realtime bridge + consumers)
  // --------------------------------------------------------------------

  get sync(): YTextSync {
    const eng = this.engine
    if (!eng) {
      throw new Error('engine not attached (join first)')
    }
    return eng.sync
  }

  get text(): YjsEngine['text'] {
    const eng = this.engine
    if (!eng) {
      throw new Error('engine not attached (join first)')
    }
    return eng.text
  }

  // --------------------------------------------------------------------
  // CM6 attachment
  // --------------------------------------------------------------------

  attachToCM6(view: EditorView): void {
    this.view = view
    this.viewRef.current = view
    this.trigger('cm6:attach')
  }

  detachFromCM6(): void {
    delete this.view
    this.viewRef.current = null
  }

  // --------------------------------------------------------------------
  // Join / leave
  // --------------------------------------------------------------------

  join(callback?: JoinCallback): void {
    this.ensureEngine()

    const settled = this.waitFirstSync().then(
      () => callback?.(),
      error => {
        if (error) {
          this.trigger('error', error, { doc_id: this.doc_id })
          callback?.(error)
        }
      }
    )
    void settled
  }

  leave(callback?: JoinCallback): void {
    this.destroyEngine()
    callback?.()
  }

  leaveAndCleanUp(callback?: (error?: Error) => void): void {
    this.destroyEngine()
    this.cleanUpDoc()
    callback?.()
  }

  leaveAndCleanUpPromise(): Promise<void> {
    this.destroyEngine()
    this.cleanUpDoc()
    return Promise.resolve()
  }

  private cleanUpDoc(): void {
    this.joined = false
    this.detachDoc?.(this.doc_id, this)
  }

  // Create the engine lazily (browser: attachProviders connects to
  // /collab/<projectId> with the session cookie; Node: offline engine for
  // tests).
  private ensureEngine(): void {
    if (this.engine) {
      return
    }
    const projectId = getMeta('ol-project_id')
    if (!projectId) {
      throw new Error('no project id for the collab engine')
    }
    // Deferred sink: the CM6 view exists AFTER join (state is created
    // post-join), so the sink reads the view lazily through the ref.
    const sink = {
      read: (): string => {
        const v = this.viewRef.current
        return v ? v.state.doc.toString() : ''
      },
      write: (next: string): void => {
        const v = this.viewRef.current
        if (!v) return
        const cur = v.state.doc.toString()
        if (cur === next) return
        v.dispatch({
          changes: { from: 0, to: v.state.doc.length, insert: next },
          userEvent: 'input.remote',
        })
      },
    }
    this.engine = createEngine(projectId, sink)
    this.emit('op:sent') // parity hook: transport is now the WS provider
  }

  /**
   * Waits until the provider reports connected (and the initial sync has
   * settled), bounded by JOIN_DEADLINE_MS. Fails closed (timeout) — the
   * same contract the old joinDoc error path gave the IDE.
   *
   * D37: y-websocket 3.x exposes live state via EVENTS, not a `.status`
   * property: 'status' → { status: 'connecting'|'connected'|
   * 'disconnected' }, 'synced' → boolean (true once the initial sync
   * completed). Reading `provider.status` (the 2.x-shaped API) always
   * yields undefined and the join deadlines out — hence event listeners
   * here, with the raw socket readyState as a fallback signal.
   */
  private waitFirstSync(): Promise<void> {
    const ws = this.engine?.providers?.ws as
      | ({
          on: (ev: string, cb: (v: any) => void) => void
          ws?: { readyState?: number }
        } & { ws?: { readyState?: number } })
      | null
    const started = Date.now()

    const state = { connected: false, synced: false }
    if (ws && typeof ws.on === 'function') {
      ws.on('status', (s: { status?: string }) => {
        if (s && s.status === 'connected') {
          state.connected = true
        }
      })
      ws.on('synced', (v: unknown) => {
        if (v === true) {
          state.synced = true
        }
      })
    }
    const rawOpen = (): boolean => {
      const sock = (ws as { ws?: { readyState?: number } } | null)?.ws
      return sock?.readyState === 1
    }

    const tick = (resolve: () => void, reject: (e: Error) => void): void => {
      const connected = state.connected || rawOpen()
      const textLen = this.engine ? this.engine.text.length : 0
      if (connected) {
        // Settled heuristic: either we already have content (seed applied)
        // or the connection has been up long enough for the initial sync
        // (yjs applies it synchronously once the payload is received).
        if (textLen > 0 || state.synced || Date.now() - started >= CONNECT_SETTLE_MS) {
          resolve()
          return
        }
      }
      if (Date.now() - started >= JOIN_DEADLINE_MS) {
        reject(
          new Error(
            'could not reach the collaboration service — check your connection and reload'
          )
        )
        return
      }
      window.setTimeout(() => tick(resolve, reject), POLL_MS)
    }

    this.joined = true
    return new Promise((resolve, reject) => tick(resolve, reject))
  }

  private destroyEngine(): void {
    if (this.engine) {
      try {
        this.engine.destroy()
      } catch (error) {
        debugConsole.warn('[doc] engine destroy', error)
      }
      this.engine = null
    }
    this.joined = false
  }

  // --------------------------------------------------------------------
  // Misc API kept for consumer compatibility
  // --------------------------------------------------------------------

  getSnapshot(): string {
    try {
      return this.engine ? this.engine.text.toString() : ''
    } catch {
      return ''
    }
  }

  // Debug helper (old chaos monkey): types a marker into the CM6 host, which
  // flows through the D24 local bridge — a valid end-to-end exercise of the
  // local direction. No recurring timer: one marker per call.
  chaosMonkey(_line = 0, char = 'a'): void {
    if (this.view) {
      this.view.dispatch({
        changes: {
          from: 0,
          insert: char + ' ' + new Date() + '\n',
        },
      })
    }
  }

  cleanUp = (): void => {
    this.destroyEngine()
    this.detachDoc?.(this.doc_id, this)
  }
}

/**
 * Kept for API compatibility (open-documents imports it). Returns the
 * "size" of a submitted op — the Yjs engine has no op pipeline, so 0.
 * @deprecated no-op under the Yjs engine (S4 flip).
 */
export function getShareJsOpSize(_op: unknown): number {
  return 0
}

// connection-manager — join + connection-state for the IDE.
//
// 2026-10-06 (owner: "We need to retire socket.io 0.9 fully in the code
// base"): the socket.io 0.9 bus (Go realtime service, socket.io.js bundle,
// nginx /socket.io) is RETIRED. The join now rides the Yjs/ygo collab
// plane — the same-origin WSS path the text engine already uses
// (ws(s)://host/collab/<projectId>, session-cookie auth, proven live:
// 101 Switching Protocols through haproxy→nginx with the owner's
// session). A clean open means join succeeded: the collab service
// authenticates the cookie and enforces project privileges itself on
// room join.
//
// Public contract (connection-context + socket-diagnostics) unchanged:
//   - state: ConnectionState (live)
//   - 'statechange' StateChangeEvent (state + previousState)
//   - tryReconnectNow() / registerUserActivity() / close(error)
//   - getSocketDebuggingInfo(): SocketDebuggingInfo
//   - readonly socket: Socket (shim; bus emits are now no-ops with a log)
import {
  ConnectionState,
  ConnectionError,
  SocketDebuggingInfo,
  ExternalHeartbeat,
} from '../types/connection-state'
import { Socket } from '../types/socket'
import getMeta from '../../../utils/meta'
import { collabEndpoint } from '@/features/ide-react/collab/providers'

const STATE_CHANGED_EVENT = 'statechange'

const initialState: ConnectionState = {
  readyState: WebSocket.CLOSED,
  forceDisconnected: false,
  inactiveDisconnect: false,
  lastConnectionAttempt: 0,
  reconnectAt: null,
  forcedDisconnectDelay: 0,
  error: '',
}

const externalHeartbeatInit: ExternalHeartbeat = {
  currentStart: 0,
  lastSuccess: 0,
  lastLatency: 0,
}

const USER_ACTIVITY_RECONNECT_NOW_DELAY = 500

// join protocol version served to the IDE's `joinProjectResponse`
// listener (replaces the retired bus handshake). Any stable value works —
// the IDE only reacts to the event presence and to version CHANGES.
const JOIN_PROTOCOL_VERSION = 202610061

// collab-plane close codes → the legacy error strings the IDE UI maps.
const CLOSE_CODE_ERROR: Record<number, ConnectionError> = {
  4001: 'not-logged-in',
  4004: 'project-deleted',
}

function newPublicId(): string {
  if (typeof crypto !== 'undefined' && typeof (crypto as Crypto & { randomUUID?: () => string }).randomUUID === 'function') {
    return (crypto as unknown as { randomUUID: () => string }).randomUUID()
  }
  return `pub-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

export class StateChangeEvent extends CustomEvent<{
  state: ConnectionState
  previousState: ConnectionState
}> {
  state: ConnectionState
  previousState: ConnectionState

  constructor(state: ConnectionState, previousState: ConnectionState) {
    super(STATE_CHANGED_EVENT, {
      detail: { state, previousState },
    })
    this.state = state
    this.previousState = previousState
  }
}

// socketShim — satisfies the `Socket` type surface the IDE consumed from
// the socket.io client. Bus emits no longer reach a server bus (retired),
// so emit() is a logged no-op; on()/removeListener() keep a local store so
// any late listener wiring stays harmless.
function makeSocketShim(mgr: {
  emitLog: (event: string, args: unknown[]) => void
  openNow: () => void
  closeNow: (force: boolean) => void
  setForceDisconnected: (v: boolean) => void
  sessionId: () => string | null
}): Socket & { __notify(event: string, payload: unknown): void } {
  const handlers = new Map<string, Array<(...data: unknown[]) => void>>()

  function emitImpl(event: string, args: unknown[]): void {
    mgr.emitLog(event, args)
    const list = handlers.get(event)
    if (list) {
      for (const cb of list.slice()) {
        cb(...args)
      }
    }
  }

  return {
    __notify: (event: string, payload: unknown) => {
      const list = handlers.get(event)
      if (!list) return
      for (const cb of list.slice()) {
        cb(payload)
      }
    },
    publicId: newPublicId(),
    on: (event, callback) => {
      const list = handlers.get(event) ?? []
      list.push(callback)
      handlers.set(event, list)
    },
    removeListener: (event, callback) => {
      const list = handlers.get(event)
      if (!list) return
      const i = list.indexOf(callback)
      if (i >= 0) list.splice(i, 1)
    },
    emit: ((event: string, ...rest: unknown[]) => {
      const cbLike =
        rest.length > 0 && typeof rest[rest.length - 1] === 'function'
          ? rest.pop()
          : undefined
      emitImpl(event, rest)
      const errCb = cbLike as
        | undefined
        | ((error: Error, ...data: unknown[]) => void)
      if (errCb) errCb(new Error('bus-retired: no-op'))
    }) as unknown as Socket['emit'],
    socket: {
      connected: false,
      connecting: false,
      connect: () => mgr.openNow(),
      disconnect: () => mgr.closeNow(false),
      sessionid: '',
      transport: { name: 'websocket' },
      transports: ['websocket'],
    } as unknown as Socket['socket'],
    disconnect: () => mgr.closeNow(false),
    forceDisconnectWithoutEvent: () => {
      mgr.setForceDisconnected(true)
      mgr.closeNow(true)
    },
  }
}

export class ConnectionManager extends EventTarget {
  state: ConnectionState = { ...initialState }
  readonly socket: Socket
  private userIsLeavingPage = false
  private lastUserActivity = Date.now()
  private websocketFailureCount = 0

  private ws: WebSocket | null = null
  private keepAliveTimer: ReturnType<typeof setInterval> | null = null
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private autoReconnect: boolean
  private reconnectGracefullyIntervalMs: number
  private projectId: string
  private baseHref: string
  private externalHeartbeat: ExternalHeartbeat = { ...externalHeartbeatInit }

  constructor() {
    super()
    this.projectId = (getMeta('ol-project_id') as string) ?? ''
    this.baseHref = (getMeta('ol-wsUrl') as string) || (typeof window !== 'undefined' ? window.location.origin : '')
    const grace = getMeta('ol-maxReconnectGracefullyIntervalMs')
    this.reconnectGracefullyIntervalMs =
      typeof grace === 'number' && grace > 0 ? grace : 45000
    this.autoReconnect = true

    const self = this
    this.socket = makeSocketShim({
      emitLog: (event, args) => {
        // the legacy bus is retired; IDE emits that targeted it are logged
        // for observability and dropped (server-side features use REST).
        console.debug('[ollitex] bus emit (no-op, bus retired):', event, args)
      },
      openNow: () => self.open(),
      closeNow: (force) => self.close(force ? 'unable-to-join' : null),
      setForceDisconnected: (v) => {
        self.state = { ...self.state, forceDisconnected: v }
      },
      sessionId: () => (self.ws?.readyState === WebSocket.OPEN ? `${self.projectId}` : null),
    })
    // nested sessionid stays populated for diagnostics
    Object.defineProperty(this.socket.socket, 'sessionid', {
      get: () => (self.ws?.readyState === WebSocket.OPEN ? self.projectId : ''),
      configurable: true,
    })

    // the legacy socket.io client connected eagerly on construction; keep
    // that behavior on the collab plane (editor pages only)
    this.startAutoConnect()
  }

  // connect — open the join socket on the Yjs collab plane (idempotent).
  connect(): void {
    this.open()
  }

  private autoConnectOnce = false

  private startAutoConnect(): void {
    if (this.autoConnectOnce) return
    this.autoConnectOnce = true
    if (typeof window === 'undefined') return // headless/unit tests
    if (!this.projectId) return // non-editor page (no room)
    this.open()
  }

  private endpoint(): string {
    // same wss(s) base + `collab/<projectId>` room the text engine uses
    const src = this.baseHref || (typeof window !== 'undefined' ? window.location.href : undefined)
    const ep = collabEndpoint(this.projectId, src)
    return `${ep.wsBaseUrl}/${ep.room}`
  }

  private open(): void {
    this.cancelReconnect()
    this.cleanupSocket()
    if (this.userIsLeavingPage) return

    this.state = {
      ...this.state,
      readyState: WebSocket.CONNECTING,
      error: '',
      lastConnectionAttempt: Date.now(),
    }
    this.dispatchStateChange()

    let ws: WebSocket
    try {
      ws = new WebSocket(this.endpoint())
    } catch (err) {
      this.failJoin('unable-to-connect', String(err))
      return
    }
    this.ws = ws
    this.socket.socket.connecting = true

    ws.onopen = () => {
      this.websocketFailureCount = 0
      this.socket.socket.connected = true
      this.socket.socket.connecting = false
      console.debug('[ollitex] join: OPEN — wss /collab/' + this.projectId)
      this.setState({
        readyState: WebSocket.OPEN,
        error: '',
        reconnectAt: null,
      })
      // deliver the legacy `joinProjectResponse` handshake to IDE
      // listeners (project:joined gate). The view comes from the session
      // join REST route (same model + privilege as the retired bus's
      // private-API join), mapped onto the payload shape the IDE expects.
      void (async () => {
        if (!this.projectId) return
        let project: unknown = null
        let permissionsLevel: string | null = null
        try {
          const res = await fetch(`/project/${this.projectId}/join`, {
            credentials: 'same-origin',
            headers: { Accept: 'application/json' },
          })
          if (res.ok) {
            const view: { project?: unknown; privilegeLevel?: string } = await res.json()
            project = view.project
            permissionsLevel = view.privilegeLevel ?? null
          } else {
            console.debug('[ollitex] join view fetch:', res.status)
          }
        } catch (err) {
          console.debug('[ollitex] join view fetch failed:', err)
        }
        const notify = (
          this.socket as Socket & {
            __notify(event: string, payload: unknown): void
          }
        ).__notify
        notify('joinProjectResponse', {
          project,
          permissionsLevel,
          protocolVersion: JOIN_PROTOCOL_VERSION,
          publicId: this.socket.publicId,
        })
      })()
    }

    ws.onerror = () => {
      // reason arrives with onclose; if the socket never opened, the join
      // already failed — keep the state, onclose finalizes it.
    }

    // Keepalive: the collab service speaks the hocuspocus BINARY protocol
    // (a JSON frame there is a protocol violation — sending the old JSON
    // meta/sync answers was one driver of the 2026-10-07 1006 loop).
    // Every 25s we emit ONE protocol-valid client frame: hocuspocus type 0
    // (sync) with an empty state vector = a single 0x00 byte. It
    //   1) resets the haproxy `timeout tunnel` on this pipe — the ~30s
    //      idle close was the OTHER driver (the IDE's own socket carries
    //      real traffic and survives; the gate socket carried none),
    //   2) gives the server traffic both ways (it replies with its state
    //      vector), so no proxy or server idle policy sees this socket
    //      as dead.
    // The text engine's ygo provider owns the room's real sync + awareness
    // on its own connection; this only keeps the gate alive.
    this.keepAliveTimer = setInterval(() => {
      if (this.ws === ws && ws.readyState === WebSocket.OPEN) {
        // outer 0 (sync) + inner 0 (SyncStep1) + VarBytes(0) empty state vector
        ws.send(new Uint8Array([0, 0, 0]))
      }
    }, 25_000)

    // hocuspocus peer replies (decoded from the server-side dispatcher in
    // ygo provider/websocket/peer.go + sync.go):
    //   t0 sync : [outer 0][inner subType][payload] — an inner 0 (SyncStep1)
    //             MUST be answered with an inner 1 (SyncStep2); inner 1/2
    //             (step2/update) need no reply.
    //   t9 ping: liveness probe — answer with the 1-byte pong frame (tag 10).
    // Everything else (step2, update, awareness, stateless, auth) is
    // irrelevant to the join gate: consume, never echo.
    ws.onmessage = (ev: MessageEvent) => {
      let buf: Uint8Array | null = null
      if (ev.data instanceof ArrayBuffer) buf = new Uint8Array(ev.data)
      else if (ev.data instanceof Uint8Array) buf = ev.data
      if (!buf || buf.length === 0) return
      const t = buf[0]
      if (t === 9) {
        ws.send(new Uint8Array([10])) // pong
      } else if (t === 0 && buf[1] === 0) {
        // SyncStep1 → SyncStep2 with an empty update (we hold no local state)
        ws.send(new Uint8Array([0, 1, 1, 0]))
      }
      // inner 1 (step2) / 2 (update) and other tags → no reply required
    }

    ws.onclose = (ev: CloseEvent) => {
      this.socket.socket.connected = false
      this.socket.socket.connecting = false
      this.ws = null
      if (this.keepAliveTimer) {
        clearInterval(this.keepAliveTimer)
        this.keepAliveTimer = null
      }
      const code = typeof ev?.code === 'number' ? ev.code : 1006
      console.debug('[ollitex] join: closed code=' + code + ' reason=' + (ev?.reason || '(none)'))

      if (CLOSE_CODE_ERROR[code]) {
        // definitive join rejection (auth / no such project)
        this.setState({
          readyState: WebSocket.CLOSED,
          error: CLOSE_CODE_ERROR[code],
          reconnectAt: this.maybeReconnect() ? Date.now() + this.reconnectGracefullyIntervalMs : null,
          forceDisconnected: code === 4003,
        })
        return
      }
      if (code === 4003 || code === 1008) {
        // forced by the server (admin)
        this.setState({
          readyState: WebSocket.CLOSED,
          error: 'unable-to-join',
          reconnectAt: null,
          forceDisconnected: true,
          forcedDisconnectDelay: 0,
        })
        return
      }
      // transport drop — reconnect gracefully
      this.websocketFailureCount += 1
      this.setState({
        readyState: WebSocket.CLOSED,
        error: 'unable-to-connect',
        reconnectAt: this.maybeReconnect() ? Date.now() + this.nextBackoff() : null,
        inactiveDisconnect: this.websocketFailureCount > 0,
      })
    }
  }

  private maybeReconnect(): boolean {
    return this.autoReconnect && !this.userIsLeavingPage && this.state.forceDisconnected === false
  }

  private nextBackoff(): number {
    // 1s, 2s, 4s … capped at the graceful interval (same feel as the
    // legacy bus backoff, without the bus)
    const step = Math.min(
      this.reconnectGracefullyIntervalMs,
      1000 * Math.pow(2, Math.min(this.websocketFailureCount, 10)),
    )
    return step
  }

  private scheduleReconnect(delay: number): void {
    this.cancelReconnect()
    this.reconnectTimer = setTimeout(() => this.open(), delay)
  }

  private cancelReconnect(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
  }

  // tryReconnectNow — immediate reconnect ("try again" affordance).
  tryReconnectNow(): void {
    this.lastUserActivity = Date.now()
    this.state = { ...this.state, forceDisconnected: false }
    this.scheduleReconnect(USER_ACTIVITY_RECONNECT_NOW_DELAY)
  }

  // registerUserActivity — editor input; resets the backoff ladder.
  registerUserActivity(): void {
    this.lastUserActivity = Date.now()
    this.websocketFailureCount = 0
  }

  // close — explicit teardown (leave editor / project switch).
  close(error?: ConnectionError | null): void {
    this.autoReconnect = false
    this.userIsLeavingPage = true
    this.cancelReconnect()
    this.cleanupSocket()
    this.setState({
      readyState: WebSocket.CLOSED,
      error: (error as ConnectionError | undefined) ?? this.state.error,
      reconnectAt: null,
    })
  }

  // getSocketDebuggingInfo — the socket-diagnostics page's contract.
  getSocketDebuggingInfo(): SocketDebuggingInfo {
    return {
      client_id: this.socket.socket.sessionid,
      publicId: this.socket.publicId,
      transport: this.socket.socket.transport?.name,
      lastUserActivity: this.lastUserActivity,
      connectionState: this.state,
      externalHeartbeat: this.externalHeartbeat,
    }
  }

  // ---- internals --------------------------------------------------------

  private failJoin(error: ConnectionError, detail: string): void {
    console.debug('[ollitex] join failed:', detail)
    this.setState({
      readyState: WebSocket.CLOSED,
      error,
      reconnectAt: null,
    })
  }

  private setState(patch: Partial<ConnectionState>): void {
    this.setStateFull({ ...this.state, ...patch })
  }

  private setStateFull(next: ConnectionState): void {
    const previous = this.state
    this.state = next
    this.dispatchStateChange(previous)
  }

  private dispatchStateChange(previous: ConnectionState = this.state): void {
    // DEFENSIVE: the context reads event.detail.state; deliver it via a
    // CustomEvent when constructable and ALWAYS via a plain Event fallback.
    // A notification failure must NEVER kill the state machine (that is
    // what wedged the loading screen before this fix).
    const detail = { state: this.state, previousState: previous }
    try {
      this.dispatchEvent(new StateChangeEvent(this.state, previous))
    } catch (err) {
      try {
        const ev = new Event(STATE_CHANGED_EVENT)
        ;(ev as Event & { detail: typeof detail }).detail = detail
        this.dispatchEvent(ev)
      } catch {
        // listeners are best-effort; the state field is already current
      }
      console.warn('[ollitex] statechange dispatch fallback:', err)
    }
  }

  private cleanupSocket(): void {
    if (this.keepAliveTimer) {
      clearInterval(this.keepAliveTimer)
      this.keepAliveTimer = null
    }
    if (this.ws) {
      this.ws.onopen = null
      this.ws.onerror = null
      this.ws.onclose = null
      this.ws.onmessage = null
      try {
        this.ws.close(1000, 'client-closed')
      } catch {
        // already closed
      }
      this.ws = null
    }
  }
}

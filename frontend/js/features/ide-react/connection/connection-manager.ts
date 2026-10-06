// connection-manager — join + connection-state for the IDE.
//
// 2026-10-06 (owner: "We need to retire socket.io 0.9 fully in the code
// base"): this class is the ONLY socket.io 0.9 consumer left in the editor.
// The socket.io bus (Go `realtime` service, `socket.io.js` bundle, nginx
// /socket.io) is RETIRED. The join now rides the Yjs/ygo collab plane —
// the SAME origin WSS path the text engine already uses (proven live:
// wss://host/collab/<projectId> → 101 Switching Protocols with the owner's
// session cookie; cookie auth is same-origin, no auth query params).
//
// Contract kept identical to the old (socket.io) connection manager, so the
// IDE context and the socket-diagnostics page are unchanged:
//   - EventTarget emitting `statechange` with `{ state }`
//   - state: { readyState, error, reconnectAt, forceDisconnected }
//   - getSocketDebuggingInfo() / socket.{sessionid,publicId,transport} shim
//   - registerUserActivity() / tryReconnectNow() / close()
//
// Join semantics: the collab service authenticates the session cookie and
// enforces project privileges itself on room join (401 not-logged-in,
// 403 not-authorized, 404 project-deleted as WS close codes). A clean
// open (`onopen`) means join succeeded — there is no separate bus join
// round-trip any more.
import { getMeta } from '@/shared/middleware/getMeta'
import { collabEndpoint } from '@/features/ide-react/collab/providers'

const STATE_CHANGED_EVENT = 'statechange'

// legacy error shape (connection-context's `close(err)`) — the old module
// exported this type; keep it so consumer imports keep compiling.
export type ConnectionError = { message?: string; [k: string]: unknown } | string

export type SocketState = {
  readyState: number // 0=CLOSED 1=CONNECTING 2=OPEN (native WS constants)
  error: string | null
  reconnectAt: number | null
  forceDisconnected: boolean
}

export type SocketInfo = {
  sessionId: string | null
  publicId: string
  transport: { name: string }
}

export class StateChangeEvent extends Event {
  constructor(public state: SocketState) {
    super(STATE_CHANGED_EVENT)
  }
}

// close codes the Go collab service uses for join rejections (ygo/h1ws
// application close codes): the editor maps them onto the error strings
// the IDE's error UI already understands.
const CLOSE_CODES: Record<number, string> = {
  4001: 'not-logged-in',
  4003: 'not-authorized',
  4004: 'project-deleted',
}

// local identity for the diagnostics page (the bus used to mint `publicId`
// client-side too — a UUID is the same contract).
function newPublicId(): string {
  if (typeof crypto !== 'undefined' && (crypto as unknown as { randomUUID?: () => string }).randomUUID) {
    return (crypto as unknown as { randomUUID: () => string }).randomUUID()
  }
  return `pub-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

export class ConnectionManager extends EventTarget {
  public socket: {
    sessionid: string | null
    publicId: string
    transport: { name: string }
  }

  private ws: WebSocket | null = null
  private error: string | null = null
  private reconnectAt: number | null = null
  private forceDisconnected = false
  private readyState = 0 // WebSocket.CLOSED
  private closeTimer: ReturnType<typeof setTimeout> | null = null
  private retriesSinceActivity = 0

  // public live state — connection-context reads `manager.state`
  public state: SocketState = {
    readyState: 0,
    error: null,
    reconnectAt: null,
    forceDisconnected: false,
  }

  // constructor — no-arg (connection-context: `new ConnectionManager()`).
  // project id + endpoint come from the page (ol-project_id meta / the
  // current URL), exactly like the pre-retirement manager did.
  constructor(
    private url?: string,
    private projectId?: string,
    private autoReconnect: boolean = true,
    private gracefulIntervalMs: number = Number(getMeta('ol-maxReconnectGracefullyIntervalMs') ?? 45000),
  ) {
    super()
    if (this.projectId == null || this.projectId === '') {
      this.projectId = (getMeta('ol-project_id') as string | null) ?? null
    }
    this.socket = {
      sessionid: null,
      publicId: newPublicId(),
      transport: { name: 'websocket' },
    }
  }

  // connect — open the join socket on the Yjs collab plane.
  connect(): void {
    this.open()
  }

  private endpoint(): string {
    // same-origin `wss?://host/collab/<projectId>` — identical to the text
    // engine's provider endpoint (providers.ts → collabEndpoint).
    const base = this.url || (typeof window !== 'undefined' ? window.location.href : undefined)
    const ep = collabEndpoint(this.projectId ?? '', base)
    const room = ep.room // collab/<projectId> (no leading slash)
    return `${ep.wsBaseUrl}/${room}`
  }

  private open(): void {
    this.cleanupSocket()
    this.clearReconnect()
    this.error = null
    this.setState(1) // CONNECTING

    let ws: WebSocket
    try {
      ws = new WebSocket(this.endpoint())
    } catch (err) {
      // endpoint malformed (page meta missing) — surface as join error
      this.handleError(String(err))
      return
    }
    this.ws = ws

    ws.onopen = () => {
      this.readyState = 2 // OPEN
      this.error = null
      this.reconnectAt = null
      this.socket.sessionid = `collab-${this.projectId}`
      this.socket.transport = { name: 'websocket' }
      this.setState(2)
    }

    ws.onerror = () => {
      // the browser does not expose the reason here; onclose carries the
      // code. If the socket never even opened, treat as a join failure.
      if (this.readyState !== 2) {
        this.handleError(this.error ?? 'unauthorized')
      }
    }

    ws.onclose = (ev: CloseEvent) => {
      this.readyState = 0
      const code = ev?.code
      if (code && CLOSE_CODES[code]) {
        // definitive join rejection (auth / privilege / no such project)
        this.handleError(CLOSE_CODES[code])
        return
      }
      // server-side forced close (admin) or transport drop
      if (code === 4003 || code === 1008) {
        this.forceDisconnected = true
        this.handleError('not-authorized')
        return
      }
      if (this.autoReconnect) {
        this.scheduleReconnect()
      } else {
        this.reconnectAt = null
        this.setState(0)
      }
    }

    // the collab plane speaks the y-websocket protocol on OTHER
    // connections (the text engine's own provider); messages on this
    // join socket are ignored by design — the open/close state IS the
    // join signal. (Presence cursors will ride y-Awareness in a later
    // stage, not a proprietary bus event stream.)
  }

  private scheduleReconnect(): void {
    if (!this.autoReconnect || this.forceDisconnected) return
    const backoff = Math.min(
      this.gracefulIntervalMs,
      1000 * Math.pow(2, this.retriesSinceActivity),
    )
    this.retriesSinceActivity += 1
    this.reconnectAt = Date.now() + backoff
    this.setState(this.readyState)
    this.closeTimer = setTimeout(() => this.open(), backoff)
  }

  // registerUserActivity — the IDE calls this on user input; reset the
  // backoff ladder (same behavior as the old bus version).
  registerUserActivity(): void {
    this.retriesSinceActivity = 0
    if (this.readyState !== 2 && this.autoReconnect) {
      this.reconnectAt = Date.now()
    }
  }

  // tryReconnectNow — immediate reconnect (user clicked "try again").
  tryReconnectNow(_err?: ConnectionError): void {
    this.retriesSinceActivity = 0
    this.forceDisconnected = false
    this.clearReconnect()
    this.open()
  }

  // close — explicit teardown (editor close / project leave).
  close(_err?: ConnectionError): void {
    this.autoReconnect = false
    this.clearReconnect()
    this.cleanupSocket()
    this.setState(0)
  }

  // getSocketDebuggingInfo — the socket-diagnostics page's contract.
  getSocketDebuggingInfo(): {
    socketInfo: SocketInfo
    error: string | null
    socket: { sessionid: string | null; publicId: string; transport: { name: string } }
  } {
    return {
      socketInfo: {
        sessionId: this.socket.sessionid,
        publicId: this.socket.publicId,
        transport: this.socket.transport,
      },
      error: this.error,
      socket: this.socket,
    }
  }

  // handleError — mirror the old bus error strings the IDE UI maps to
  // (not-logged-in / project-deleted / rate-limited / unable-to-...).
  private handleError(message: string): void {
    this.error = message
    this.ws = null
    this.readyState = 0
    this.reconnectAt =
      this.autoReconnect && !this.forceDisconnected ? Date.now() + this.gracefulIntervalMs : null
    this.setState(0)
  }

  private cleanupSocket(): void {
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

  private clearReconnect(): void {
    if (this.closeTimer) {
      clearTimeout(this.closeTimer)
      this.closeTimer = null
    }
  }

  private setState(readyState: number): void {
    this.readyState = readyState
    this.state = {
      readyState,
      error: this.error,
      reconnectAt: this.reconnectAt,
      forceDisconnected: this.forceDisconnected,
    }
    this.dispatchEvent(new StateChangeEvent(this.state))
  }
}

// re-exported for consumers that imported the old module surface
export { collabEndpoint as wsBaseUrlHelper }

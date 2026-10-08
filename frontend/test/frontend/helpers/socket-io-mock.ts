/**
 * SocketIOMock — structural re-creation of the retired socket.io 0.x test
 * harness (the old `js/ide-react/connection/SocketIoShim` module — the
 * socket.io 0.9 stack is fully gone; the live socket surface is the
 * ConnectionManager event bus over ygo).
 *
 * Server→client events are simulated with `emitToClient` (fires the
 * registered client listeners synchronously), matching the socket.io
 * mock semantics the CYPRESS/RTL specs were written against:
 *   `socket.on('event', cb)`  — app code registering a listener
 *   `socket.emitToClient('event', ...payload)` — the spec driving the
 *   server side
 * The Socket TYPE (js/features/ide-react/connection/types/socket.ts) is
 * the structural contract; this mock satisfies the members the specs +
 * the storybook scope decorator use.
 */
export class SocketIOMock {
  publicId = 'socket-mock'
  connected = false
  private listeners: Record<string, Array<(...args: any[]) => void>> = {}

  on(event: string, callback: (...args: any[]) => void): this {
    ;(this.listeners[event] ??= []).push(callback)
    return this
  }

  once(event: string, callback: (...args: any[]) => void): this {
    const wrapper = (...args: any[]) => {
      this.off(event, wrapper)
      callback(...args)
    }
    return this.on(event, wrapper)
  }

  off(event: string, callback?: (...args: any[]) => void): this {
    if (!callback) {
      delete this.listeners[event]
      return this
    }
    this.listeners[event] = (this.listeners[event] || []).filter(f => f !== callback)
    return this
  }

  // client→server direction: sink (no server in the test world).
  emit(..._args: any[]): this {
    return this
  }

  /** socket.io mock API: deliver a server→client event to the listeners. */
  emitToClient(event: string, ...args: any[]): void {
    for (const cb of this.listeners[event] || []) {
      cb(...args)
    }
  }

  countEventListeners(event: string): number {
    return (this.listeners[event] || []).length
  }

  connect(): this {
    this.connected = true
    return this
  }

  disconnect(): void {
    this.connected = false
  }
}

// legacy named shape (some importers used `SocketIOMock` alongside a
// default export of the shim namespace).
export default { SocketIOMock }

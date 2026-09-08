// overleaf-lab #14 (2026-09-08): runtime error collector for /hub.
//
// The "Hub health" leaf needs to SHOW uncaught errors that happened anywhere
// in the hub (a toast helper throwing, a broken section render, a socket
// reject). This is a tiny ring buffer (no dependencies, no side effects on
// the event) started once from HubRoot; the health leaf renders the tail.
//
// Deliberately NOT a logging library: it must survive any failure mode
// (including JSON stringify failures), so every entry is a pre-built string.

export type CollectedError = {
  at: number
  kind: 'error' | 'unhandledrejection'
  text: string
}

const MAX = 25

const buffer: CollectedError[] = []
let started = false

function push(kind: CollectedError['kind'], text: string): void {
  buffer.push({ at: Date.now(), kind, text })
  if (buffer.length > MAX) buffer.shift()
}

function toText(value: unknown): string {
  try {
    if (value == null) return 'null'
    if (typeof value === 'string') return value.slice(0, 400)
    const err = value as any
    if (err?.stack && typeof err.stack === 'string') return err.stack.slice(0, 400)
    return JSON.stringify(value).slice(0, 400)
  } catch {
    return String(value).slice(0, 400)
  }
}

/** Start the collectors once (idempotent). Safe in jsdom (listeners no-op there). */
export function startErrorCollector(): void {
  if (started || typeof window === 'undefined') return
  started = true
  window.addEventListener('error', (e: ErrorEvent) => {
    push('error', `${e && e.message ? e.message : 'window error'}${e && e.filename ? ` (${e.filename}:${e.lineno})` : ''}`)
  })
  window.addEventListener('unhandledrejection', (e: PromiseRejectionEvent) => {
    push('unhandledrejection', toText((e as any)?.reason ?? (e as any)?.detail ?? new Error('unhandled rejection')))
  })
}

export function getCollectedErrors(): CollectedError[] {
  return buffer.slice()
}

export function clearCollectedErrors(): void {
  buffer.length = 0
}

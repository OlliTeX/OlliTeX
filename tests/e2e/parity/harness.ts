import { execFileSync } from 'node:child_process'

/**
 * Parity harness — shared by the LEGACY baseline specs and the HUB parity specs.
 *
 * The owner's equivalence definition: the /hub feature does what the legacy
 * feature did — same endpoints + payloads, same resulting visible state, same
 * role/flag/state sensitivity. This helper captures the API contract while a
 * real browser flow runs, so specs assert on behaviour, not on brittle DOM.
 */
export type ApiCall = { method: string; path: string; body?: any; status?: number }

/**
 * Start capturing API calls (method + pathname + JSON body + response status).
 * Returns { calls, stop }. Ignores static assets and the socket.io websocket.
 */
export function captureApi(page: any, baseURL: string) {
  const calls: ApiCall[] = []
  const interesting = (p: string) =>
    !/\.(js|css|png|jpe?g|gif|svg|woff2?|ico|map)(\?|$)/i.test(p) &&
    !/socket\.io|__webpack|favicon/i.test(p)
  const reqMap = new WeakMap<any, { method: string; path: string; body?: any }>()

  page.on('request', (req: any) => {
    let p = ''
    try {
      const u = new URL(req.url(), baseURL)
      p = u.pathname
    } catch { return }
    if (!interesting(p)) return
    let body
    try {
      const raw = req.postData()
      if (raw) body = raw.startsWith('{') ? JSON.parse(raw) : raw
    } catch { body = req.postData() }
    reqMap.set(req, { method: req.method(), path: p, body })
  })

  page.on('response', async (res: any) => {
    const meta = reqMap.get(res.request())
    if (!meta) return
    calls.push({ ...meta, status: res.status() })
  })

  const find = (method: string, pathRe: RegExp) =>
    calls.find(c => c.method === method && pathRe.test(c.path))
  const stop = () => {
    page.removeAllListeners('request')
    page.removeAllListeners('response')
  }
  return { calls, find, stop }
}

/** Poll until a predicate over the captured calls is true (UI latency). */
export async function waitForCall(cap: { calls: ApiCall[] }, fn: (c: ApiCall[]) => boolean, label: string, timeoutMs = 8000): Promise<void> {
  const start = Date.now()
  for (;;) {
    if (fn(cap.calls)) return
    if (Date.now() - start > timeoutMs) {
      throw new Error(`timeout waiting for ${label}; saw:\n` + cap.calls.slice(0, 40).map(c => `  ${c.method} ${c.path} -> ${c.status}`).join('\n'))
    }
    await new Promise(r => setTimeout(r, 120))
  }
}

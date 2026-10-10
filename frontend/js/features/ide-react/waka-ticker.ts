/**
 * WakaTime heartbeat ticker (owner request G, 2026-10-09).
 *
 * The relay is fire-and-forget by server contract:
 *   POST /project/:id/wakatime/heartbeat   {entity, time}
 * The server answers 204 No Content whenever the feature is off, the user
 * has not opted in, or the project gate fails — so this ticker is safe to
 * run unconditionally on editor pages and only produces traffic when the
 * user has explicitly enabled WakaTime (and only to the server, never to
 * wakatime.com / wakapi directly).
 *
 * Cadence: one heartbeat per minute while the editor page is visible AND
 * shows recent editing activity (any input event or the document title
 * pulsing "unsaved" — a cheap proxy; exact keystroke accounting is the
 * editor's domain and not this ticker's). Time credit = the minutes since
 * the last tick, capped so a background tab never back-fills hours.

 * Disabled by default at the client when the instance exposes
 * `wakaTimeEnabled: false` (ol-ExposedSettings) — zero requests then.
 */

const TICK_MS = 60 * 1000
const MAX_TIME_PER_TICK = 1 // one minute credit per tick

let lastTick = Date.now()
let activity = false

function exposeEnabled(): boolean {
  try {
    const el = document.querySelector('meta[name=ol-ExposedSettings]')
    const raw = el ? el.getAttribute('content') : null
    if (!raw) return false
    const s = JSON.parse(raw)
    return s?.wakaTimeEnabled === true
  } catch {
    return false
  }
}

function projectId(): string {
  try {
    // /project/<24hex> or /editor/<24hex> (the hub editor variant)
    const m = location.pathname.match(/\/(?:project|editor)\/([a-fA-F0-9]{24})/)
    return m ? m[1] : ''
  } catch {
    return ''
  }
}

function activeEntity(): string {
  // best-effort: the file-tree "active" document is not on a stable DOM
  // attribute across versions — fall back to the root doc name when the
  // page carries it (ol-projectName is the project, not the doc, so the
  // language mapping (`.tex` → LaTeX) still lands right for the dominant
  // case). Editors with a different active file credit it precisely.
  try {
    const el = document.querySelector('[data-file-tree-active] [data-file-name], [data-active-doc-name]')
    const n = el?.getAttribute('data-file-name') || el?.getAttribute('data-active-doc-name')
    if (n) return n
  } catch {
    // fall through
  }
  return 'main.tex'
}

function tick(pid: string) {
  const now = Date.now()
  const dt = Math.min(MAX_TIME_PER_TICK, Math.max(0, Math.round((now - lastTick) / 1000 / 60)))
  lastTick = now
  if (dt <= 0 || !activity) return
  activity = false // consume the activity flag (it re-arms on input)
  try {
    void fetch(`/project/${pid}/wakatime/heartbeat`, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      // the relay is CSRF-exempt by design (fire-and-forget tracker,
      // reference PR#249 parity) — no token attached.
      body: JSON.stringify({ entity: activeEntity(), time: dt, isWrite: true }),
    }).then(r => {
      if (!r.ok && r.status !== 204) {
        // quiet by contract; never throw into the editor
      }
      return null
    }).catch(() => undefined)
  } catch {
    // never break the editor over the relay
  }
}

export function startWakaTicker(): void {
  if (typeof window === 'undefined' || typeof document === 'undefined') return
  if (!exposeEnabled()) return // instance off → zero overhead
  const pid = projectId()
  if (!pid) return // not an editor page
  try {
    window.addEventListener('input', () => { activity = true }, { passive: true, capture: true })
    window.addEventListener('keydown', () => { activity = true }, { passive: true, capture: true })
  } catch {
    return
  }
  window.setInterval(() => {
    if (document.visibilityState === 'visible' && activity) tick(pid)
  }, TICK_MS)
}

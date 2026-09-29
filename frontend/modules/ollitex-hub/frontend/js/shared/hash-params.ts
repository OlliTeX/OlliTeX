// overleaf-lab — #8 (2026-09-08): URL-persisted section state for /hub.
//
// Hub leaf ids and leaf *state* share one hash channel:  #/<leaf>?q=...&tag=...
// The leaf id is the stable deep-link key (hub-root reconciles against it);
// the query part after `?` is section-local state (search box, tag filter,
// page). Writing state uses history.replaceState — no extra history entries,
// no hashchange storms, and the rendered leaf is unaffected.
//
// Leaf ownership: a state param set is written together with the id of the
// leaf it was produced under (`leaf=<id>`). On mount a section ignores state
// whose owner is a different leaf, so navigating A→B (A's debounced write
// racing B's mount) can never pre-fill B with A's filters.
//
// Contract with hub-root.tsx (#1): parseHash()/selection ALWAYS strips the
// query part; only the id before `?` is a navigational key.

export type HashParams = Record<string, string>

const LEAF_KEY = 'leaf'

function safeSplit(): { id: string; params: HashParams } {
  try {
    return splitHash(window.location.hash || '')
  } catch {
    return { id: '', params: {} }
  }
}

/** Split a raw hash (`#/projects.all?q=a&page=2`) into id + params. */
export function splitHash(raw: string): { id: string; params: HashParams } {
  let h = raw
  if (h.startsWith('#')) h = h.slice(1)
  if (h.startsWith('/')) h = h.slice(1)
  const [id, query] = h.split('?', 2)
  const params: HashParams = {}
  if (query) {
    for (const pair of query.split('&')) {
      if (!pair) continue
      const [k, v] = pair.split('=', 2)
      try {
        params[decodeURIComponent(k)] = decodeURIComponent(v || '')
      } catch {
        params[k] = v || ''
      }
    }
  }
  return { id, params }
}

/**
 * Current leaf's state params (empty object when none, or when the stored
 * params were written under a different leaf — the ownership marker wins).
 */
export function readLeafHashParams(): HashParams {
  const { id, params } = safeSplit()
  if (params[LEAF_KEY] && params[LEAF_KEY] !== id) return {}
  const { [LEAF_KEY]: _leaf, ...rest } = params
  return rest
}

/**
 * Persist leaf-state params in `#/<current-leaf>?leaf=<id>&k=v&...`
 * (replaceState — no history spam, no re-navigation). The owning leaf id is
 * derived from the current hash at call time and stored as `leaf=`.
 * Empty/undefined values are dropped. Returns false in harnesses without a
 * usable location.
 */
export function writeLeafHashParams(params: HashParams): boolean {
  try {
    const { id } = safeSplit()
    if (!id) return false
    const kept: string[] = [`${LEAF_KEY}=${encodeURIComponent(id)}`]
    for (const [k, v] of Object.entries(params)) {
      if (v === undefined || v === null || v === '' || k === LEAF_KEY) continue
      kept.push(`${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
    }
    const next = `#/${id}` + (kept.length ? `?${kept.join('&')}` : '')
    if (next === window.location.hash) return true
    window.history.replaceState(null, '', next)
    return true
  } catch {
    return false
  }
}

/**
 * Legacy single-param writer without the ownership marker; kept for callers
 * that persist state on a known-stable leaf (template categories).
 * @deprecated prefer writeLeafHashParams
 */
export function writeHashParams(params: HashParams): boolean {
  return writeLeafHashParams(params)
}

/** Parse a positive integer param (page), min 1. */
export function intParam(params: HashParams, key: string, fallback = 1): number {
  const raw = params[key]
  if (raw === undefined) return fallback
  const n = parseInt(raw, 10)
  return Number.isFinite(n) && n > 0 ? n : fallback
}

/**
 * Accordion open-state for the hub rail (nav_structure.md §3.1-3.3):
 * - each folder folds independently (state = set of open folder ids)
 * - the active leaf's ancestors auto-open on selection
 * - the set persists per browser (localStorage)
 *
 * External store for useSyncExternalStore: the snapshot is REPLACED on
 * every change (Object.is comparison requires a new reference).
 */
import { useSyncExternalStore } from 'react'

type Listener = () => void

const KEY = 'ol-hub-open-folders'

function readInitial(): Set<string> {
  try {
    const raw = window.localStorage.getItem(KEY)
    if (!raw) return new Set()
    const arr = JSON.parse(raw)
    return new Set(Array.isArray(arr) ? arr.filter(x => typeof x === 'string') : [])
  } catch {
    return new Set()
  }
}

let snapshot: Set<string> =
  typeof window !== 'undefined' ? readInitial() : new Set<string>()
const listeners = new Set<Listener>()

function persist() {
  try {
    window.localStorage.setItem(KEY, JSON.stringify([...snapshot]))
  } catch {
    // storage may be unavailable (private mode) — state still works in-memory
  }
}

function emit() {
  listeners.forEach(l => {
    try {
      l()
    } catch {
      // never let a broken listener kill the rail
    }
  })
}

function commit(next: Set<string>) {
  snapshot = next
  persist()
  emit()
}

export const accordionState = {
  get: () => snapshot,
  has: (id: string) => snapshot.has(id),
  toggle: (id: string) => {
    const next = new Set(snapshot)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    commit(next)
  },
  open: (id: string) => {
    if (snapshot.has(id)) return
    commit(new Set(snapshot).add(id))
  },
  close: (id: string) => {
    if (!snapshot.has(id)) return
    const next = new Set(snapshot)
    next.delete(id)
    commit(next)
  },
  /** open every folder in the chain (auto-expand on selection) */
  openChain: (ids: string[]) => {
    const next = new Set(snapshot)
    let changed = false
    ids.forEach(id => {
      if (id && !next.has(id)) {
        next.add(id)
        changed = true
      }
    })
    if (changed) commit(next)
  },
  subscribe: (l: Listener) => {
    listeners.add(l)
    return () => {
      listeners.delete(l)
    }
  },
}

export function useOpenSet(): Set<string> {
  return useSyncExternalStore(
    cb => accordionState.subscribe(cb),
    () => accordionState.get(),
    () => new Set<string>()
  )
}

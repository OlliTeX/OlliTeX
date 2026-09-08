// overleaf-lab — #3a + #6 + #9 (2026-09-08): one data-fetching hook for /hub
// sections.
//
// Replaces the per-section `useEffect + fetch + 3 useState` boilerplate with
// an explicit, shared contract:
//   const { data, loading, error, refetch, lastUpdated } = useSectionData(
//     () => getJSON('/admin/active-projects'),
//     { label: 'GET /admin/active-projects', live: true }
//   )
//
// * `label`  — the endpoint identity (endpoint-contract manifest, item #3b,
//              cross-checks these labels against a server-side manifest).
// * `live`   — #6: refetch on tab activation AND on a 30s interval while the
//              tab is visible (the "real-time who-is-editing" and system
//              message panes; the legacy page refetched on tab switch, the
//              hub now actually updates while you are looking at it).
// * `cacheMs`— #9: in-memory dedupe by `label`; a repeat call inside the TTL
//              (or after a mutation that did not invalidate) resolves from the
//              cache instead of re-fetching — this kills the navigation
//              refetch storm for the shared /admin/site-settings payload.
// * `invalidate(label)` — call after a successful PUT/POST so the next read
//              is fresh (save-then-reload must never serve the TTL copy).

import { useCallback, useEffect, useRef, useState } from 'react'

export interface SectionData<T> {
  data: T | null
  loading: boolean
  error: string | null
  lastUpdated: number | null
  refetch: (force?: boolean) => Promise<void>
}

interface Options {
  /** endpoint identity; doubles as the cache key */
  label?: string
  /** live-updating pane: activation refetch + 30s interval while visible */
  live?: boolean
  /** in-memory TTL for identical reads (ms) */
  cacheMs?: number
}

const DEFAULT_TTL = 30_000

type Entry = { at: number; promise: Promise<any> }
const cache = new Map<string, Entry>()

export function invalidate(sectionLabel: string): void {
  cache.delete(sectionLabel)
}

/** Test-only: drop every cached read (prevents cross-test TTL pollution). */
export function clearSectionDataCache(): void {
  cache.clear()
}

async function cachedCall<T>(label: string | undefined, fn: () => Promise<T>, cacheMs?: number): Promise<T> {
  const ttl = cacheMs ?? DEFAULT_TTL
  const hit = label ? cache.get(label) : undefined
  if (hit && Date.now() - hit.at < ttl) return hit.promise as Promise<T>
  const promise = fn().then(
    v => {
      if (label) cache.set(label, { at: Date.now(), promise: Promise.resolve(v) })
      return v
    },
    err => {
      // do not cache failures
      if (label) cache.delete(label)
      throw err
    }
  )
  if (label) cache.set(label, { at: Date.now(), promise })
  return promise
}

export function useSectionData<T>(fetcher: () => Promise<T>, opts: Options = {}): SectionData<T> {
  const { label, live, cacheMs } = opts
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [lastUpdated, setLastUpdated] = useState<number | null>(null)
  const alive = useRef(true)

  const refetch = useCallback(
    async (force = false) => {
      // force=true = bypass the TTL cache (after a successful mutation);
      // force=false = serve a fresh (<TTL) cached read — storm protection.
      if (force) invalidate(label || '')
      setLoading(true)
      setError(null)
      try {
        const value = await cachedCall(label, () => fetcher(), cacheMs)
        if (!alive.current) return
        setData(value)
        setLastUpdated(Date.now())
      } catch (err: any) {
        if (!alive.current) return
        setError((err?.data?.message as string) || err?.message || String(err))
      } finally {
        if (alive.current) setLoading(false)
      }
    },
    // fetcher is expected to be stable (module-level or useCallback); the
    // label/clock are the contract.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [label, cacheMs]
  )

  useEffect(() => {
    alive.current = true
    void refetch(false)
    return () => {
      alive.current = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [label])

  // #6 — live panes: refetch on tab activation + 30s heartbeat while visible.
  // Forced reads: a live pane must never sit on the shared TTL copy.
  useEffect(() => {
    if (!live) return
    const visible = () => document.visibilityState === 'visible'
    const onVisibility = () => {
      if (visible()) void refetch(true)
    }
    document.addEventListener('visibilitychange', onVisibility)
    const timer = window.setInterval(() => {
      if (visible()) void refetch(true)
    }, 30_000)
    return () => {
      document.removeEventListener('visibilitychange', onVisibility)
      window.clearInterval(timer)
    }
  }, [live, refetch])

  return { data, loading, error, lastUpdated, refetch }
}

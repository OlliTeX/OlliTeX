// overleaf-lab #9 (2026-09-08): shared in-memory cache for the full
// /admin/site-settings payload.
//
// Every native site-settings section (signup, sso-*, email, sandboxed, …)
// does GET /admin/site-settings (the WHOLE payload) and then slices its
// section out — navigating between the ~20 sections used to trigger one full
// fetch each time (the "refetch storm" measured on slow networks). The server
// payload contains no per-user state, so a short shared TTL + promise dedupe
// is safe: reads inside the TTL share one request; a successful save invalidates
// before the post-save read so the editor always sees its own write.

import { getJSON } from '@/infrastructure/fetch-json'

const PATH = '/admin/site-settings'
const TTL_MS = 30_000

type Entry = { at: number; promise: Promise<any> | null; value: any }
let entry: Entry | null = null

export function invalidateSiteSettingsCache(): void {
  entry = null
}

export function getSiteSettings(force = false): Promise<any> {
  const now = Date.now()
  if (!force && entry && now - entry.at < TTL_MS) {
    if (entry.value !== null) return Promise.resolve(entry.value)
    if (entry.promise) return entry.promise
  }
  const promise: Promise<any> = getJSON(PATH).then(
    v => {
      entry = { at: Date.now(), promise: null, value: v }
      return v
    },
    err => {
      entry = null // never cache failures
      throw err
    },
  )
  entry = { at: now, promise, value: null }
  return promise
}

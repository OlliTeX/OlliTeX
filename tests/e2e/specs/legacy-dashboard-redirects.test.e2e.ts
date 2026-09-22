/**
 * Legacy project-dashboard redirects (P7 cutover contract).
 *
 * Node oracle (services/web/app/src/router.mjs projectDashboardRedirects,
 * owner queue 7 2026-09-10): the legacy project-list pages are removed —
 * every dashboard state renders in the hub (/hub#/projects.*). The routes
 * 301 so bookmarks and SSO deep links land in the hub; Project APIs
 * (POST /project/new*, /user/projects, /project/:id/entities) are untouched.
 *
 * Wire contract (captured 2026-09-22 on the e2e stack, Node v22.21.1):
 *   anonymous: 302 /login, text/plain "Found. Redirecting to /login"
 *   authed:    301, Location=<hub>, text/plain
 *              "Moved Permanently. Redirecting to <hub>"
 *
 * Framework-agnostic: the SAME contract must hold on the Node backend AND
 * the Go backend (1:1 drop-in). This spec is the durable gate — it passes
 * on either backend, so it survives the P7 Node de-commissioning.
 *
 * TRANSPORT NOTE (2026-09-22, Playwright 1.62.1 / Chromium 151): the
 * browser-routed `context.request` API follows these redirects despite
 * `redirect: 'manual'` (observed: the 200 login page arrives as the
 * response), which corrupts the status/body contract this spec pins. The
 * requests therefore go through node's fetch (undici), where
 * `redirect: 'manual'` is honored and the raw 3xx wire is visible — the
 * same contract, transport-faithfully observed.
 */
import { test, expect } from '@playwright/test'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }

const TARGETS: Array<[path: string, hub: string]> = [
  ['/project', '/hub#/projects.all'],
  ['/project/owned', '/hub#/projects.owned'],
  ['/project/shared', '/hub#/projects.shared'],
  ['/project/archived', '/hub#/projects.archived'],
  ['/project/trashed', '/hub#/projects.trashed'],
  ['/project/untagged', '/hub#/projects.all'],
  // Node's own literal target (its static string) — pinned 1:1, not "fixed".
  ['/project/tags/mytag', '/hub#/projects.tags.tags'],
]

test.describe.configure({ mode: 'serial' })

function manual(url: string, cookies: string[] = []): Promise<{ status: number; headers: Record<string, string>; body: string }> {
  return global
    .fetch(url, { redirect: 'manual', headers: cookies.length ? { cookie: cookies.join('; ') } : {} })
    .then(async (r) => {
      const body = await r.text()
      const headers: Record<string, string> = {}
      r.headers.forEach((v: string, k: string) => {
        headers[k] = v
      })
      return { status: r.status, headers, body }
    })
}

async function loginCookies(): Promise<string[]> {
  const h = { 'user-agent': 'legacy-dash-gate' }
  const r0 = await global.fetch(`${BASE}/login`, { headers: { ...h, accept: 'text/html' } })
  const html = await r0.text()
  const csrf0 = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
  if (!csrf0) throw new Error('no csrf on /login')
  const r1 = await global.fetch(`${BASE}/login`, {
    method: 'POST',
    headers: {
      ...h,
      'content-type': 'application/json',
      accept: 'application/json',
      'x-csrf-token': csrf0,
      cookie: ck0,
    },
    body: JSON.stringify(ADMIN),
  })
  if (r1.status !== 200) throw new Error(`login -> ${r1.status} ${((await r1.text()) + '').slice(0, 160)}`)
  const ck = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
  if (!ck) throw new Error('no session cookie after login')
  return [ck]
}

test('anonymous: each dashboard route bounces 302 → /login', async () => {
  for (const [path] of TARGETS) {
    const r = await manual(`${BASE}${path}`)
    expect(r.status, path).toBe(302)
    expect(r.headers.location, path).toBe('/login')
    expect(r.body, path).toBe('Found. Redirecting to /login')
  }
})

test('authed: each dashboard route 301s to its hub target (exact wire)', async () => {
  const cookies = await loginCookies()
  for (const [path, hub] of TARGETS) {
    const r = await manual(`${BASE}${path}`, cookies)
    expect(r.status, path).toBe(301)
    expect(r.headers.location, path).toBe(hub)
    expect(r.headers['content-type'], path).toBe('text/plain; charset=utf-8')
    expect(r.body, path).toBe(`Moved Permanently. Redirecting to ${hub}`)
  }
})

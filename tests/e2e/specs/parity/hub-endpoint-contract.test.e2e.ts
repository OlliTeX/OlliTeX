import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { ADMIN, USER } from '../../fixtures/credentials'

/**
 * ENDPOINT CONTRACT MANIFEST (#3b, 2026-09-08 — "reduce API contract drift").
 *
 * The /hub sections consume a fixed set of endpoints (the same manifest the
 * "Hub health" leaf probes at runtime). This spec pins the contract in CI:
 *   * every ADMIN endpoint the hub consumes answers with a DETERMINISTIC
 *     status + JSON content type (a renamed/broken/HTML-erroring endpoint
 *     fails here immediately at the wire level, before any UI symptom),
 *   * every one of them DENIES a non-site-admin with 403 (no leak),
 *   * the two new endpoints (#4 editor state, #14 hub health) report the
 *     exact shapes the hub sections render.
 *
 * Add a row here whenever a hub section gets a new endpoint.
 */
const BASE = 'http://127.0.0.1:7420'
let p: any = null

interface Row {
  label: string
  method: string
  path: string
  /** deterministic accepted statuses (admin role) */
  status: number[]
  /** site-admin-only route → the denial test expects 302/401/403 for users */
  adminOnly?: boolean
  bodyHas?: (body: any) => boolean
  bodyNote?: string
}

const MANIFEST: Row[] = [
  { label: 'site settings (JSON map)', method: 'GET', path: '/admin/site-settings', status: [200], adminOnly: true,
    bodyHas: b => b && typeof b === 'object' && Object.keys(b).length > 0 },
  { label: 'template admins', method: 'GET', path: '/admin/site/template-admins', status: [200], adminOnly: true },
  { label: 'LLM admin settings', method: 'GET', path: '/admin/llm/settings/json', status: [200], adminOnly: true },
  { label: 'LLM usage', method: 'GET', path: '/admin/llm/usage', status: [200], adminOnly: true },
  { label: 'instance stats series (day window — #7)', method: 'GET',
    path: '/admin/instance-stats/api/series?metric=user_count&window=day', status: [200], adminOnly: true,
    bodyHas: b => b && Array.isArray(b.points), bodyNote: 'points[] array' },
  { label: 'instance stats series (week window — #7)', method: 'GET',
    path: '/admin/instance-stats/api/series?metric=active_projects&window=week', status: [200], adminOnly: true,
    bodyHas: b => b && Array.isArray(b.points) },
  { label: 'instance stats alert config (#7)', method: 'GET', path: '/admin/instance-stats/api/alert-config', status: [200], adminOnly: true,
    bodyHas: b => b && typeof b.diskWarningPercent === 'number' && typeof b.ramWarningPercent === 'number' && Array.isArray(b.alertEmails) },
  { label: 'active projects (#6 live pane)', method: 'GET', path: '/admin/active-projects', status: [200], adminOnly: true,
    bodyHas: b => Array.isArray(b) },
  { label: 'system messages (public read, admin writes)', method: 'GET', path: '/system/messages', status: [200],
    bodyHas: b => Array.isArray(b) },
  { label: 'template categories (public read)', method: 'GET', path: '/api/template/categories', status: [200] },
  { label: 'editor gate state (#4)', method: 'GET', path: '/admin/editor-state', status: [200], adminOnly: true,
    bodyHas: b => b && typeof b.editorIsOpen === 'boolean', bodyNote: 'editorIsOpen boolean' },
  { label: 'hub health core (#14)', method: 'GET', path: '/api/hub/health', status: [200], adminOnly: true,
    bodyHas: b => b && b.ok === true && typeof b.uptimeSec === 'number' && b.platform?.node, bodyNote: 'ok/uptimeSec/platform.node' },
]

const PAGE_ROWS = [
  { label: 'hub page shell (HTML)', path: '/hub' },
  { label: 'user settings page (HTML)', path: '/user/settings' },
]

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
  // a real HTML page must be up before api() (CSRF meta etc.)
  await p.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' })
})
test.afterAll(async () => { if (p) await p.context().close().catch(() => {}) })

test('contract: every endpoint the /hub consumes answers with the pinned status + JSON', async () => {
  for (const row of MANIFEST) {
    const res = await api(p, row.method, row.path)
    expect(res.status(), `${row.label}: GET ${row.path} → ${res.status()} (expected one of ${row.status.join('/')})`).toBeGreaterThanOrEqual(100)
    expect(row.status.includes(res.status()), `${row.label}: status ${res.status()} not in contract ${row.status}`).toBeTruthy()
    const ct = (res.headers()['content-type'] || '').toLowerCase()
    expect(ct.includes('json'), `${row.label}: content-type "${ct}" is not JSON`).toBeTruthy()
    if (row.bodyHas) {
      let body: any = null
      try { body = await res.json() } catch { body = null }
      expect(row.bodyHas(body), `${row.label}: body shape contract failed${row.bodyNote ? ` (${row.bodyNote})` : ''}`).toBeTruthy()
    }
  }
  // the hub + user pages themselves serve HTML (not JSON 404s)
  for (const page of PAGE_ROWS) {
    const res = await api(p, 'GET', page.path)
    expect(res.status(), `${page.label}: ${res.status()}`).toBe(200)
    const ptype = (res.headers()['content-type'] || '').toLowerCase()
    expect(ptype.includes('html'), `${page.label}: content-type "${ptype}"`).toBeTruthy()
  }
})

test('contract: the whole /admin-only manifest denies non-site-admins (no leak)', async ({ browser }) => {
  const ctx = await browser.newContext()
  const q = await ctx.newPage()
  await loginRobust(q, USER.email, USER.password)
  await q.goto(BASE + '/projects', { waitUntil: 'domcontentloaded' }).catch(() => {})
  for (const row of MANIFEST.filter(r => r.adminOnly)) {
    // this server denies with a 302 redirect to the restricted page (the
    // followed target is a normal page, so we must not follow it here)
    const res = await q.request.get(BASE + row.path, { maxRedirects: 0 })
    expect([302, 401, 403].includes(res.status()),
      `${row.label}: non-admin denial must be 302/401/403, got ${res.status()}`).toBeTruthy()
  }
  await ctx.close()
})

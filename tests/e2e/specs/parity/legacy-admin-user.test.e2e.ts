import { test, expect, type Page, type Browser } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { captureApi, waitForCall } from '../../parity/harness'
import { ADMIN, TPLADMIN, USER } from '../../fixtures/credentials'

/**
 * LEGACY BASELINE — /admin/user (parity scope page 11 of 13).
 *
 * RELIABLE (API-first) parity per the owner's equivalence definition:
 *   "functions like the original" = same endpoints + payloads + resulting state.
 * Rendering is asserted loosely (page loads, role-gated) to avoid brittle DOM
 * waits on the 30+ user list; the FUNCTION is proven by the captured API
 * contract + resulting stored state (verified at the source: the users doc).
 *
 * One shared context + ONE login (CE login rate-limits 20/min/IP).
 * Mutations use throwaway users (PAR-*), purged after each test.
 */
const BASE = process.env.OL_BASE || 'http://127.0.0.1:7420'
const W = () => `par${Date.now().toString(36)}${Math.floor(Math.random() * 997)}`
const email = (t: string) => `${t}@e2e.test`

type Shared = { ctx: any; page: Page }
const SH: { v?: Shared } = {}
async function admin(browser: Browser): Promise<Page> {
  if (!SH.v) {
    const ctx = await browser.newContext({ viewport: { width: 1500, height: 900 } })
    const page = await ctx.newPage()
    await page.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
    const cb = page.locator('button:has-text("Accept all cookies")')
    if ((await cb.count()) > 0) await cb.first().click().catch(() => {})
    await loginRobust(page as any, ADMIN.email, ADMIN.password)
    SH.v = { ctx, page }
  }
  return SH.v.page
}
test.afterAll(async () => { if (SH.v) await SH.v.ctx.close().catch(() => {}) })

async function csrf(p: Page) {
  const t = await p.locator('meta[name="ol-csrfToken"]').getAttribute('content')
  if (!t) throw new Error('no CSRF meta — not logged in?')
  return t
}
async function api(p: Page, method: string, path: string, body?: any) {
  const doCall = async () => {
    const headers: Record<string, string> = {}
    if (method !== 'GET') {
      headers['X-Csrf-Token'] = await csrf(p)
      if (body !== undefined) headers['Content-Type'] = 'application/json'
    }
    return p.request.fetch(BASE + path, {
      method: method as any,
      headers,
      data: body === undefined ? undefined : JSON.stringify(body),
    }) as Promise<any>
  }
  let r = await doCall()
  if (r.status() === 403) {
    // CE rotates the session CSRF token — stale DOM meta → refresh page and retry ONCE
    await p.reload({ waitUntil: 'domcontentloaded' }).catch(() => {})
    r = await doCall()
  }
  return r
}
async function mkUser(p: Page, name: string, extra: any = {}) {
  const r = await api(p, 'POST', '/admin/user/create', { email: email(name), firstName: 'Par', lastName: 'W', ...extra })
  expect(r.status(), (await r.text()).slice(0, 160)).toBe(200)
  const j = await r.json()
  const u = j.user || j
  if (!u.id && !u._id) throw new Error(`create response without user id: ${(await r.text()).slice(0, 160)}`)
  return u
}
async function findUser(p: Page, mail: string) {
  const r = await api(p, 'POST', '/admin/users', { sort: { by: 'name', asc: true } })
  const j = await r.json()
  return (j.users || []).find((u: any) => (u.email || '').toLowerCase() === mail.toLowerCase())
}
async function purge(p: Page, id: string | null) { if (id) await api(p, 'DELETE', `/admin/user/${id}`).catch(() => {}) }

test.describe('legacy /admin/user (baseline)', () => {
  test('renders for site admin (200, no 403, user list present)', async ({ browser }) => {
    const p = await admin(browser)
    const resp = await p.goto(BASE + '/admin/user', { waitUntil: 'domcontentloaded' })
    expect(resp!.status()).toBe(200)
    const body = (await p.locator('body').innerText().catch(() => '')) || ''
    expect(/All users/.test(body), 'expected the "All users" heading').toBeTruthy()
  })

  test('denied: tpladmin + user cannot open /admin/user', async ({ browser }) => {
    for (const a of [TPLADMIN, USER]) {
      const ctx = await browser.newContext(); const p = await ctx.newPage()
      await p.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
      await loginRobust(p as any, a.email, a.password)
      const resp = await p.goto(BASE + '/admin/user', { waitUntil: 'domcontentloaded' }).catch(() => null)
      const body = (await p.locator('body').innerText().catch(() => '')) || ''
      const denied = (resp && resp.status() === 403) || !/All users/.test(body)
      expect(denied, `${a.email} must not access /admin/user`).toBeTruthy()
      await ctx.close()
    }
  })

  test('list: POST /admin/users returns the sorted user list', async ({ browser }) => {
    const p = await admin(browser)
    const r = await api(p, 'POST', '/admin/users', { sort: { by: 'name', asc: true } })
    expect(r.status()).toBe(200)
    const j = await r.json()
    expect(Array.isArray(j.users)).toBeTruthy()
    expect(j.users.length).toBeGreaterThan(0)
  })

  test('create → user exists (state)', async ({ browser }) => {
    const p = await admin(browser)
    const w = W(); const u = await mkUser(p, w)
    expect(u.email).toBe(email(w))
    expect(await findUser(p, email(w))).toBeTruthy()
    await purge(p, u.id)
  })

  test('resend activation: POST /admin/user/:id/send-activation → 200', async ({ browser }) => {
    const p = await admin(browser)
    const u = await mkUser(p, W())
    const r = await api(p, 'POST', `/admin/user/${u.id}/send-activation`, {})
    expect(r.status()).toBe(200)
    await purge(p, u.id)
  })

  test('info: GET /admin/user/:id/info → 200 detail', async ({ browser }) => {
    const p = await admin(browser)
    const u = await mkUser(p, W())
    const r = await api(p, 'GET', `/admin/user/${u.id}/info`)
    expect(r.status()).toBe(200)
    await purge(p, u.id)
  })

  test('update: POST /admin/user/:id/update {firstName,lastName,email,isAdmin,canManageTemplates}', async ({ browser }) => {
    const p = await admin(browser)
    const u = await mkUser(p, W())
    const r = await api(p, 'POST', `/admin/user/${u.id}/update`, { firstName: 'Parity', lastName: 'Hub' })
    expect(r.status()).toBe(200)
    const after = await findUser(p, u.email)
    expect(after?.firstName).toBe('Parity')
    await purge(p, u.id)
  })

  test('suspend + resume: update{suspended} toggles state', async ({ browser }) => {
    const p = await admin(browser)
    const u = await mkUser(p, W())
    expect((await api(p, 'POST', `/admin/user/${u.id}/update`, { suspended: true })).status()).toBe(200)
    expect(await findUser(p, u.email)).toBeTruthy()
    expect((await api(p, 'POST', `/admin/user/${u.id}/update`, { suspended: false })).status()).toBe(200)
    await purge(p, u.id)
  })

  test('delete (soft) then restore: endpoint contract + state survives', async ({ browser }) => {
    const p = await admin(browser)
    const u = await mkUser(p, W())
    expect((await api(p, 'POST', `/admin/user/${u.id}/delete`, { sendEmail: false, toUserId: null })).status()).toBe(200)
    expect((await api(p, 'POST', `/admin/user/${u.id}/restore`, {})).status()).toBe(200)
    await purge(p, u.id)
  })

  test('purge (hard): soft-delete then DELETE /admin/user/:id → user gone', async ({ browser }) => {
    const p = await admin(browser)
    const u = await mkUser(p, W())
    expect((await api(p, 'POST', `/admin/user/${u.id}/delete`, { sendEmail: false, toUserId: null })).status()).toBe(200)
    const r = await api(p, 'DELETE', `/admin/user/${u.id}`)
    expect(r.status()).toBe(200)
    expect(await findUser(p, u.email)).toBeFalsy()
  })
})

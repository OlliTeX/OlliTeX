import { test, expect, type Page, type Browser } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { captureApi, waitForCall } from '../../parity/harness'
import { ADMIN, TPLADMIN, USER } from '../../fixtures/credentials'

/**
 * HUB PARITY — admin users leaf (parity/legacy/admin-user.yaml).
 * Proves the /hub admin-users section performs the SAME contract as legacy
 * /admin/user: same endpoints + payloads + resulting state, driven through the
 * NEW Mantine UI (menu, modals, toggles) — the owner's equivalence definition.
 *
 * Robustness: one shared context + ONE login (CE rate-limits logins); 127.0.0.1
 * base (matches Playwright baseURL so the cookie jar is not split); throwaway
 * users purged after each test.
 */
const BASE = process.env.OL_BASE || 'http://127.0.0.1:7420'
const HUB_USERS = BASE + '/hub#/site.general.users.all'
const HUB_DELETED = BASE + '/hub#/site.general.users.deleted'
const W = () => `ph${Date.now().toString(36)}${Math.floor(Math.random() * 997)}`
const email = (t: string) => `${t}@e2e.test`

type Shared = { ctx: any; page: Page }
const SH: { v?: Shared } = {}
async function admin(browser: Browser): Promise<Page> {
  if (!SH.v) {
    const ctx = await browser.newContext({ viewport: { width: 1500, height: 900 } })
    const page = await ctx.newPage()
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
    return p.request.fetch(BASE + path, { method: method as any, headers, data: body === undefined ? undefined : JSON.stringify(body) }) as Promise<any>
  }
  let r = await doCall()
  if (r.status() === 403) { await p.reload({ waitUntil: 'domcontentloaded' }).catch(() => {}); r = await doCall() }
  return r
}
async function mkUser(p: Page, name: string) {
  const r = await api(p, 'POST', '/admin/user/create', { email: email(name), firstName: 'Par', lastName: 'W' })
  expect(r.status(), (await r.text()).slice(0, 160)).toBe(200)
  const j = await r.json(); const u = j.user || j
  if (!u.id && !u._id) throw new Error(`create had no id: ${(await r.text()).slice(0, 120)}`)
  return u
}
async function purge(p: Page, id: string | null) { if (id) await api(p, 'DELETE', `/admin/user/${id}`).catch(() => {}) }

async function gotoHubUsers(p: Page, url = HUB_USERS) {
  await p.goto(url, { waitUntil: 'domcontentloaded' })
  await p.waitForSelector('button:has-text("New user")', { timeout: 25000 })
  await p.waitForSelector('table, [class*="mantine-Table"]', { timeout: 15000 }).catch(() => {})
}
const row = (p: Page, mail: string) => p.locator('tr', { has: p.locator('text=' + mail) }).first()
async function ensureRow(p: Page, mail: string) {
  const base = p.locator('input[placeholder*="Search" i]').first()
  if (await base.isVisible().catch(() => false)) {
    await base.fill(mail)
    await p.waitForTimeout(700)
  }
  await expect(row(p, mail).locator('text=' + mail).first()).toBeVisible({ timeout: 15000 })
}
async function openRowMenu(p: Page, mail: string) {
  const r = row(p, mail)
  await r.locator('button[aria-label="Actions"]').first().click()
  await p.locator('[role="menu"]').last().waitFor({ state: 'visible', timeout: 8000 })
}

test.describe('hub admin users (parity vs /admin/user)', () => {
  test('renders for site admin: nav leaf + New user + table', async ({ browser }) => {
    const p = await admin(browser)
    await gotoHubUsers(p)
    await expect(p.locator('button:has-text("New user")').first()).toBeVisible()
  })

  test('deny: tpladmin + user cannot reach the hub admin users leaf', async ({ browser }) => {
    for (const a of [TPLADMIN, USER]) {
      const ctx = await browser.newContext(); const q = await ctx.newPage()
      await q.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
      await loginRobust(q as any, a.email, a.password)
      await q.goto(HUB_USERS, { waitUntil: 'domcontentloaded' }).catch(() => {})
      await q.waitForTimeout(1200)
      // non-admin hub: the admin users table ("New user") must NOT be present
      const sees = await q.locator('button:has-text("New user")').first().isVisible().catch(() => false)
      expect(sees, `${a.email} must not see hub admin users`).toBe(false)
      await ctx.close()
    }
  })

  test('create: "New user" modal → POST /admin/user/create {email}', async ({ browser }) => {
    const p = await admin(browser); const w = W()
    await gotoHubUsers(p)
    const cap = captureApi(p as any, BASE)
    await p.locator('button:has-text("New user")').first().click()
    const dlg = p.locator('[role="dialog"]').last()
    await dlg.locator('input').first().fill(email(w))
    const more = await dlg.locator('input').count()
    if (more >= 3) { await dlg.locator('input').nth(1).fill('ParHub'); await dlg.locator('input').nth(2).fill('W') }
    await dlg.locator('button', { hasText: /create|add|invite|make|register/i }).last().click()
    await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === '/admin/user/create'), 'POST create')
    const call = cap.calls.find(x => x.path === '/admin/user/create' && x.method === 'POST') as any
    expect(call.status).toBe(200)
    expect(call.body?.email).toBe(email(w))
    await purge(p, (await (await api(p, 'POST', '/admin/users', { sort: { by: 'name', asc: true } })).json()).users?.find((u: any) => u.email === email(w))?.id || null)
  })

  test('resend: menu "Send activation email" → POST /admin/user/:id/send-activation', async ({ browser }) => {
    const p = await admin(browser)
    const u = await mkUser(p, W())
    await gotoHubUsers(p)
    await ensureRow(p, u.email)
    const cap = captureApi(p as any, BASE)
    await openRowMenu(p, u.email)
    await p.getByRole('menuitem', { name: /send activation/i }).click()
    await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === `/admin/user/${u.id}/send-activation`), 'send-activation')
    expect((cap.calls.find(x => x.path === `/admin/user/${u.id}/send-activation`) as any)?.status).toBe(200)
    await purge(p, u.id)
  })

  test('info: menu "User info" → GET /admin/user/:id/info modal', async ({ browser }) => {
    const p = await admin(browser)
    const u = await mkUser(p, W())
    await gotoHubUsers(p)
    await ensureRow(p, u.email)
    await openRowMenu(p, u.email)
    await p.getByRole('menuitem', { name: /user info/i }).click()
    const dlg = p.locator('[role="dialog"]').filter({ hasText: /user info/i }).last()
    await expect(dlg).toBeVisible({ timeout: 5000 })
    await expect(dlg.locator('text=' + u.email)).toBeVisible({ timeout: 5000 })
    await p.keyboard.press('Escape')
    await purge(p, u.id)
  })

  test('update: menu "Update…" modal → POST /admin/user/:id/update {firstName,lastName}', async ({ browser }) => {
    const p = await admin(browser)
    const u = await mkUser(p, W())
    await gotoHubUsers(p)
    await ensureRow(p, u.email)
    const cap = captureApi(p as any, BASE)
    await openRowMenu(p, u.email)
    // 2026-09-13 (owner #24): the menu item is “Edit…” (legacy parity); the
    // modal is titled “Edit user” and saves with the same /admin/user/:id/update.
    await p.getByRole('menuitem', { name: /edit/i }).click()
    const dlg = p.locator('[role="dialog"]').filter({ hasText: /edit user/i }).last()
    await dlg.locator('input').first().fill('HubParity')
    await dlg.locator('button', { hasText: /save|update|apply/i }).last().click()
    await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === `/admin/user/${u.id}/update`), 'POST update')
    const call = cap.calls.find(x => x.method === 'POST' && x.path === `/admin/user/${u.id}/update`) as any
    expect(call.status).toBe(200)
    expect(call.body?.firstName).toBe('HubParity')
    await purge(p, u.id)
  })

  test('suspend + resume: menu toggles → POST /admin/user/:id/update {suspended}', async ({ browser }) => {
    const p = await admin(browser)
    const u = await mkUser(p, W())
    await gotoHubUsers(p)
    await ensureRow(p, u.email)
    const cap = captureApi(p as any, BASE)
    await openRowMenu(p, u.email)
    await p.getByRole('menuitem', { name: /suspend user/i }).click()
    await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === `/admin/user/${u.id}/update` && x.body?.suspended === true), 'suspend')
    await purge(p, u.id)
  })

  test('delete + purge: menu "Delete user" then "Purge permanently"', async ({ browser }) => {
    const p = await admin(browser)
    const u = await mkUser(p, W())
    await gotoHubUsers(p)
    await ensureRow(p, u.email)
    const cap = captureApi(p as any, BASE)
    await openRowMenu(p, u.email)
    await p.getByRole('menuitem', { name: /delete user/i }).click()
    // overleaf-lab #4: single-user delete is guarded by a confirmation dialog
    // (destructive-action guard) — click through it.
    const delDlg = p.locator('[role="dialog"]').filter({ hasText: /delete/i }).last()
    await expect(delDlg).toBeVisible({ timeout: 5000 })
    await delDlg.locator('button', { hasText: /^delete user$/i }).last().click()
    await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === `/admin/user/${u.id}/delete`), 'POST delete')
    expect((cap.calls.find(x => x.path === `/admin/user/${u.id}/delete`) as any)?.status).toBe(200)
    const cap2 = captureApi(p as any, BASE)
    await gotoHubUsers(p, HUB_DELETED)
    await ensureRow(p, u.email)
    await openRowMenu(p, u.email)
    await p.getByRole('menuitem', { name: /purge permanently/i }).click()
    const dlg = p.locator('[role="dialog"]').filter({ hasText: /purge/i }).last()
    await expect(dlg).toBeVisible({ timeout: 5000 })
    await dlg.locator('button', { hasText: /^purge$/i }).click()
    await waitForCall(cap2, c => c.some(x => x.method === 'DELETE' && x.path === `/admin/user/${u.id}`), 'DELETE purge')
  })
})

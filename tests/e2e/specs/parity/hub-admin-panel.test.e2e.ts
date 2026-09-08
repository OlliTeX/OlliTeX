import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { ADMIN, USER, TPLADMIN } from '../../fixtures/credentials'

// Parity HUB side (hub leaves) for the CE admin panel surface:
// SystemMessages (site.general.messages), EditorControls (site.general.editor —
// PG-PN-1 added), ActiveProjects (site.general.activeprojects) — same endpoints
// as legacy /admin/panel.
const BASE = 'http://127.0.0.1:7420'
let p: any = null
const unique = (prefix: string) => `${prefix}-${Date.now()}${Math.floor(Math.random() * 90 + 10)}`

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
  await p.goto(BASE + '/hub#/site.general.messages', { waitUntil: 'domcontentloaded' })
})
test.afterAll(async () => { if (p) await p.context().close().catch(() => {}) })
const a = (method: string, path: string, body?: unknown) => api(p, method, path, body)

const gotoLeaf = async (id: string) => {
  const href = BASE + '/hub#/site.general.' + id
  await p.goto(href, { waitUntil: 'domcontentloaded' })
  // hash-only changes are same-document navigations and are flaky when the shell
  // is on a distant leaf — force a full load so the target hash is rendered for sure
  if (((p.url().split('#')[1] || '').replace(/^\//, '')) !== ('site.general.' + id)) {
    await p.reload({ waitUntil: 'domcontentloaded' })
  }
  await p.waitForTimeout(700)
}

test('renders: the hub leaves for the panel panes all render', async () => {
  await gotoLeaf('messages')
  await expect(p.getByRole('heading', { name: /system messages|messages/i }).first()).toBeVisible({ timeout: 15000 })
  await gotoLeaf('editor')
  await expect(p.getByRole('heading', { name: /editor controls/i }).first()).toBeVisible({ timeout: 15000 })
  await gotoLeaf('activeprojects')
  await expect(p.getByRole('heading', { name: /projects/i }).first()).toBeVisible({ timeout: 15000 })
})

test('deny: non-site-admins cannot use the hub panel leaves', async ({ browser }) => {
  for (const who of [TPLADMIN, USER]) {
    const ctx = await browser.newContext(); const q = await ctx.newPage()
    await loginRobust(q, who.email, who.password)
    await q.goto(BASE + '/hub#/site.general.editor', { waitUntil: 'domcontentloaded' })
    await q.waitForTimeout(1200)
    const body = (await q.locator('body').innerText().catch(() => '')) || ''
    expect(/close editor|disconnect all/i.test(body), who.email + ' must not see editor controls').toBeFalsy()
    await ctx.close()
  }
})

test('system-messages: same post/clear contract from the hub messages leaf', async ({ browser }) => {
  const msg = unique('parity-hub-msg')
  try {
    const r = await a('POST', '/admin/messages', { content: msg })
    expect(r.status(), 'post status ' + r.status).toBeTruthy()
    expect(r.status()).toBeLessThan(400)
    // shared read contract — the exact data the hub section renders
    const list = await (await a('GET', '/system/messages')).json()
    expect(list.some((m: any) => m.content === msg), 'message in the shared /system/messages feed').toBeTruthy()
    // hub UI on a clean first-load (fresh context → deterministic leaf render; the
    // in-flight shell state of a busy session can race hash navigations)
    const ctx = await browser.newContext(); const q = await ctx.newPage()
    await loginRobust(q, ADMIN.email, ADMIN.password)
    await q.goto(BASE + '/hub#/site.general.messages', { waitUntil: 'domcontentloaded' })
    await expect(q.locator('body', { hasText: msg }).first()).toBeVisible({ timeout: 15000 })
    await ctx.close()
  } finally {
    await a('POST', '/admin/messages/clear', {}).catch(() => {})
    const list = await (await a('GET', '/system/messages')).json()
    expect(list.some((m: any) => m.content === msg), 'message cleared from the feed').toBeFalsy()
  }
})

test('active-projects: the hub leaf renders the same real-time active-projects contract', async () => {
  // fresh first-load (deterministic leaf render)
  const ctx = (await p.context()).browser()!
  const c2 = await ctx.newContext(); const q = await c2.newPage()
  await loginRobust(q, ADMIN.email, ADMIN.password)
  await q.goto(BASE + '/hub#/site.general.activeprojects', { waitUntil: 'domcontentloaded' })
  const heading = q.getByRole('heading', { name: /active projects/i }).first()
  await expect(heading).toBeVisible({ timeout: 15000 })
  const body = (await q.locator('body').innerText()) || ''
  const list = await (await a('GET', '/admin/active-projects')).json()
  if (Array.isArray(list) && list.length === 0) {
    expect(/no projects (are )?currently being (actively )?edited/i.test(body), 'empty-state parity').toBeTruthy()
  } else {
    expect(Array.isArray(list), 'shared feed is a list').toBeTruthy()
  }
  expect(/real-time|editor service/i.test(body), 'pane description parity').toBeTruthy()
  await c2.close()
})

test('editor: the hub Editor controls leaf drives the same close/open endpoints', async () => {
  await gotoLeaf('editor')
  const csrf = (await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => '')) || ''
  const closeRes = await p.request.post('/admin/closeEditor', {
    data: { isOpen: false },
    headers: { 'Content-Type': 'application/json', 'X-Csrf-Token': csrf },
  })
  expect(closeRes.status(), 'closeEditor status').toBeLessThan(400)
  const openRes = await p.request.post('/admin/openEditor', {
    headers: csrf ? { 'X-Csrf-Token': csrf } : {},
  })
  expect(openRes.status(), 'openEditor status').toBeLessThan(400)
  // the hub UI buttons are present for admin (parity of the pane controls)
  await expect(p.getByRole('button', { name: /close editor/i })).toBeVisible({ timeout: 10000 })
})

test('disconnect: the same disconnectAllUsers endpoint answers the hub', async () => {
  const csrf = (await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => '')) || ''
  const res = await p.request.post('/admin/disconnectAllUsers', {
    headers: csrf ? { 'X-Csrf-Token': csrf } : {},
  })
  expect(res.status(), 'disconnect status').toBeLessThan(400)
})

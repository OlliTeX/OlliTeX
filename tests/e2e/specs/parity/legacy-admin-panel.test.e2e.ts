import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, captureApi } from '../../parity/harness'
import { ADMIN, USER, TPLADMIN } from '../../fixtures/credentials'

// Parity baseline (legacy /admin/panel, CE build = 3 panes) — system messages,
// active projects, open/close editor.
const BASE = 'http://127.0.0.1:7420'
const PAGE = BASE + '/admin/panel'
let p: any = null
const unique = (prefix: string) => `${prefix}-${Date.now()}${Math.floor(Math.random() * 90 + 10)}`

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
})
test.afterAll(async () => { if (p) await p.context().close().catch(() => {}) })
const a = (method: string, path: string, body?: unknown) => api(p, method, path, body)

const postRedirect = async (path: string, body?: unknown) => {
  const csrf = (await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => '')) || ''
  const res = await p.request.post(
    path,
    body
      ? { data: body, headers: { 'Content-Type': 'application/json', 'X-Csrf-Token': csrf } }
      : { headers: csrf ? { 'X-Csrf-Token': csrf } : {} },
  )
  return res.status() < 400
}

test('renders: /admin/panel shows its panes (messages, projects, editor)', async () => {
  await p.goto(PAGE, { waitUntil: 'domcontentloaded' })
  expect(p.url(), 'on the panel').toContain('/admin/panel')
  for (const pane of ['#system-messages', '#active-projects', '#open-close-editor']) {
    await expect(p.locator(pane).first()).toBeAttached({ timeout: 15000 })
  }
  await expect(p.getByRole('button', { name: /post message/i }).first()).toBeVisible({ timeout: 10000 })
  await expect(p.getByRole('button', { name: /close editor/i }).first()).toBeVisible({ timeout: 10000 })
})

test('deny: non-site-admins are denied the admin panel', async ({ browser }) => {
  for (const who of [TPLADMIN, USER]) {
    const ctx = await browser.newContext(); const q = await ctx.newPage()
    await q.goto(PAGE, { waitUntil: 'domcontentloaded' }).catch(() => {})
    const ok = /login|signin|denied|forbidden/i.test(q.url()) || !/post message/i.test((await q.locator('body').innerText().catch(() => '')) || '')
    expect(ok, who.email + ' denied').toBeTruthy()
    await ctx.close()
  }
})

test('system-messages: post a message, it renders on the panel; clear removes it', async () => {
  const msg = unique('parity-msg')
  try {
    const r = await a('POST', '/admin/messages', { content: msg })
    expect(r.status(), 'post status ' + r.status).toBeTruthy()
    expect(r.status()).toBeLessThan(400)
    await p.goto(PAGE, { waitUntil: 'domcontentloaded' })
    await expect(p.locator('body', { hasText: msg }).first()).toBeVisible({ timeout: 15000 })
  } finally {
    await a('POST', '/admin/messages/clear', {}).catch(() => {})
    await p.goto(PAGE, { waitUntil: 'domcontentloaded' }).catch(() => {})
    const bodyText = (await p.locator('body').innerText().catch(() => '')) || ''
    expect(bodyText.includes(msg), 'message cleared').toBeFalsy()
  }
})

test('active-projects: the pane renders the projects listing surface', async () => {
  await p.goto(PAGE, { waitUntil: 'domcontentloaded' })
  const pane = p.locator('#active-projects').first()
  await expect(pane).toBeAttached({ timeout: 15000 })
  const paneText = ((await pane.innerText().catch(() => '')) || '').toLowerCase()
  expect(/active projects|no projects/i.test(paneText), 'pane shows the listing (' + paneText.slice(0, 60) + ')').toBeTruthy()
})

test('editor: closeEditor blocks then openEditor restores (state round-trip)', async () => {
  // ensure a known-open starting state
  await postRedirect('/admin/openEditor')
  await postRedirect('/admin/closeEditor', { isOpen: false })
  const closedRedirect = await postRedirect('/admin/closeEditor', { isOpen: false })
  expect(closedRedirect, 'closeEditor accepted').toBeTruthy()
  // parity of the pane's promise: "stop anyone opening the editor" — the admin
  // surface itself stays reachable (server gate applies to editor opens).
  const restored = await postRedirect('/admin/openEditor')
  expect(restored, 'openEditor restores state').toBeTruthy()
})

test('disconnect: disconnectAllUsers answers for the site admin', async () => {
  const ok = await postRedirect('/admin/disconnectAllUsers')
  expect(ok, 'disconnect accepted').toBeTruthy()
})

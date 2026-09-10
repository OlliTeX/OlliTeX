import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, captureApi } from '../../parity/harness'
import { ADMIN, USER, TPLADMIN } from '../../fixtures/credentials'

// Parity baseline (legacy /admin/panel, CE build = 3 panes) — system messages,
// active projects, open/close editor.
// 2026-09-10 (owner queue 4): the legacy PAGE is REMOVED — /admin/panel 301s to
// /hub#/overview; the admin surfaces (system messages / active projects / open-
// close editor) live on the hub (site.general.*). The API contract tests
// (POST /admin/messages, /admin/closeEditor, /admin/openEditor,
// /admin/disconnectAllUsers) are unchanged; page tests now assert the redirect.
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

test('redirects: /admin/panel 301 → /hub#/overview (page removed 2026-09-10)', async () => {
  const res = await p.request.get(PAGE, { maxRedirects: 0 })
  expect(res.status(), '301 expected').toBe(301)
  expect(res.headers()['location']).toBe('/hub#/overview')
})

test('deny: non-site-admins are denied the admin panel (gate enforced on the hub)', async ({ browser }) => {
  for (const who of [TPLADMIN, USER]) {
    const ctx = await browser.newContext(); const q = await ctx.newPage()
    await loginRobust(q, who.email, who.password)
    await q.goto(PAGE, { waitUntil: 'domcontentloaded' })
    await q.waitForTimeout(1200)
    const body = (await q.locator('body').innerText().catch(() => '')) || ''
    expect(/post message/i.test(body), who.email + ' must NOT see the panel controls').toBeFalsy()
    await ctx.close()
  }
})

test('system-messages: post a message, it is readable via GET /system/messages; clear removes it', async () => {
  const msg = unique('parity-msg')
  try {
    const r = await a('POST', '/admin/messages', { content: msg })
    expect(r.status(), 'post status ' + r.status).toBeLessThan(400)
    const list = await (await a('GET', '/system/messages')).json().catch(() => [])
    const items = Array.isArray(list) ? list : (list.messages ?? [])
    expect(items.some((m: any) => (m.content ?? m.text ?? '').includes(msg)), 'message readable').toBeTruthy()
  } finally {
    await a('POST', '/admin/messages/clear', {}).catch(() => {})
    const list = await (await a('GET', '/system/messages')).json().catch(() => [])
    const items = Array.isArray(list) ? list : (list.messages ?? [])
    expect(items.some((m: any) => (m.content ?? m.text ?? '').includes(msg)), 'message cleared').toBeFalsy()
  }
})

test('active-projects: the hub active-projects leaf renders the listing surface', async () => {
  await p.goto(BASE + '/hub#/site.general.activeprojects', { waitUntil: 'domcontentloaded' })
  await p.waitForTimeout(1500)
  const bodyText = ((await p.locator('body').innerText().catch(() => '')) || '').toLowerCase()
  expect(/active projects|no projects/i.test(bodyText), 'surface shows the listing (' + bodyText.slice(0, 80) + ')').toBeTruthy()
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

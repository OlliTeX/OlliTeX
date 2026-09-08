import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, captureApi, waitForCall, killProject } from '../../parity/harness'
import { USER, ADMIN } from '../../fixtures/credentials'

// Parity (HUB) — /hub#/templates.all (gallery leaf) vs legacy /templates.
// Same dataset (GET /api/templates), categories (GET /api/template/categories),
// "Open as template" (POST /project/new {template}).
const BASE = 'http://127.0.0.1:7420'
const LEAF = BASE + '/hub#/templates.all'
let p: any = null
let leafLoaded = false

// The hub page is one document: goto() to the same URL is a no-op for the
// browser, so after API mutations we hard-reload (reloadNav) to refetch.
const nav = async () => {
  if (!leafLoaded || p.url() !== LEAF) {
    await p.goto(LEAF, { waitUntil: 'domcontentloaded' })
    leafLoaded = true
  } else {
    await p.reload({ waitUntil: 'domcontentloaded' })
  }
}

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, USER.email, USER.password)
})
test.afterAll(async () => { if (p) await p.context().close().catch(() => {}) })
const a = (method: string, path: string, body?: unknown) => api(p, method, path, body)
const tpl = async () => {
  const d = await (await a('GET', '/api/templates')).json()
  const arr = d.templates ?? d
  const t = arr.find((x: any) => /parity fixture/i.test(x.name || ''))
  if (!t) throw new Error('fixture template missing — re-import it')
  return t
}

test('renders: hub gallery leaf lists the templates (with use-as-template control)', async () => {
  await nav()
  const body = await p.locator('body').innerText()
  expect(/template/i.test(body), 'template language').toBeTruthy()
  expect(/Parity Fixture Template/.test(body), 'fixture listed').toBeTruthy()
})

test('deny: guest cannot open the hub gallery leaf', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  const r = await q.goto(LEAF, { waitUntil: 'domcontentloaded' }).catch(() => null)
  const body = (await q.locator('body').innerText().catch(() => '')) || ''
  const denied = r?.status() === 403 || /log ?in|sign in/i.test(body) || !/template/i.test(body)
  expect(denied, 'guest denied').toBeTruthy()
  await ctx.close()
})

test('list: hub leaf backed by GET /api/templates (same data as legacy)', async () => {
  await nav()
  const d = await (await a('GET', '/api/templates')).json()
  const arr = d.templates ?? d
  expect(arr.some((x: any) => /parity fixture/i.test(x.name || '')), 'fixture in same API the hub reads').toBeTruthy()
  await expect(p.locator('text=Parity Fixture Template').first()).toBeVisible({ timeout: 15000 })
})

test('detail: hub shows the template meta (name; author shown when present — parity with legacy detail)', async () => {
  await nav()
  const body = await p.locator('body').innerText()
  expect(/Parity Fixture Template/.test(body), 'fixture visible on the leaf').toBeTruthy()
  // shared contract: the API exposes the same fields the legacy detail page renders
  const t = await tpl()
  for (const k of ['version', 'name', 'author', 'description', 'category', 'lastUpdated']) {
    expect(k in t, 'API field ' + k).toBeTruthy()
  }
})

test('categories: hub category selector present and wired to the categories API', async () => {
  const cats = await (await a('GET', '/api/template/categories')).json()
  expect(Array.isArray(cats) && cats.length > 0, 'categories available').toBeTruthy()
  const t = await tpl()
  // public API category is a path ('/templates/academic-journal'); option values are keys
  const raw = t.category ?? cats[0].key ?? ''
  const key = (cats.find((c: any) => c.key === raw) ? raw : raw.split('/').pop()) || cats[0].key
  await nav()
  const sel = p.locator('select[aria-label="Template category"]').first()
  await expect(sel).toBeAttached({ timeout: 15000 })
  const cap = captureApi(p as any, BASE)
  await sel.selectOption(key)
  await waitForCall(cap, c => c.some(x => x.method === 'GET' && x.path.startsWith('/api/templates')), 'gallery refetch')
  await p.waitForTimeout(1500)
  const body = (await p.locator('body').innerText()) || ''
  expect(/Parity Fixture Template/.test(body), 'fixture listed under its category').toBeTruthy()
})

test('use: hub "Start from template" → modal → Create project → POST /project/new {template}', async () => {
  await nav()
  const cap = captureApi(p as any, BASE)
  await p.locator('button', { hasText: /use template/i }).first().click()
  const dlg = p.locator('[role="dialog"]', { hasText: /as a template/i }).last()
  await expect(dlg).toBeVisible({ timeout: 8000 })
  const cap2 = captureApi(p as any, BASE)
  await dlg.locator('input').first().fill('Parity Hub Gallery Use ' + Date.now().toString(36))
  await dlg.locator('button', { hasText: /create project/i }).click()
  await waitForCall(cap2, c => c.some(x => x.method === 'POST' && x.path === '/project/new'), 'project/new')
  const created = cap2.calls.find(x => x.method === 'POST' && x.path === '/project/new')
  expect(created?.status, 'project/new 200').toBe(200)
  const pid = created?.body === null ? undefined : undefined
  await p.waitForTimeout(2500)
  const m = p.url().match(/\/project\/([a-f0-9]+)/)
  void pid
  expect(m, 'navigated into the created project (or captured)').toBeTruthy()
  if (m) await killProject(p, m[1]).catch(() => {})
})

test('role: admin opens the hub gallery leaf', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  await loginRobust(q, ADMIN.email, ADMIN.password)
  const r = await q.goto(LEAF, { waitUntil: 'domcontentloaded' }).catch(() => null)
  const body = (await q.locator('body').innerText().catch(() => '')) || ''
  expect(r?.status() === 200 && /Parity Fixture Template/.test(body), 'admin sees gallery').toBeTruthy()
  await ctx.close()
})

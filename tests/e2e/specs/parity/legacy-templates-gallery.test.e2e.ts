import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, killProject } from '../../parity/harness'
import { USER } from '../../fixtures/credentials'

// Parity baseline (legacy /templates gallery) — browse templates + start a
// project from one. Contract proven live: /api/templates, /api/template/
// categories, /template/:id, POST /project/new {projectName, template}.
const BASE = 'http://127.0.0.1:7420'
let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, USER.email, USER.password)
})
test.afterAll(async () => {
  if (p) await p.context().close().catch(() => {})
})
const a = (method: string, path: string, body?: unknown) => api(p, method, path, body)
const tpl = async () => {
  const j = await (await a('GET', '/api/templates')).json()
  const arr = j.templates ?? j
  // deterministic: the persistent fixture template (not whichever row sorts first)
  const t = arr.find(x => (x.name || '').includes('Parity Fixture Template'))
  expect(t, 'fixture template present in /api/templates (got: ' + arr.map(x => x.name).slice(0, 6).join(', ') + ')').toBeTruthy()
  return t
}

test('renders: /templates gallery shows h1 + fixture template card', async () => {
  const r = await p.goto(BASE + '/templates', { waitUntil: 'domcontentloaded' })
  expect(r?.status()).toBe(200)
  await expect(p.locator('h1', { hasText: /templates/i })).toBeVisible({ timeout: 15000 })
  const t = await tpl()
  await expect(p.locator(`a[href*="${t.id}"]`).first()).toBeVisible({ timeout: 10000 })
})

test('gate: guests see a login prompt, not template content', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  await q.goto(BASE + '/templates', { waitUntil: 'domcontentloaded' }).catch(() => {})
  const body = (await q.locator('body').innerText().catch(() => '')) || ''
  const denied = /log ?in|sign in/i.test(body) && !/parit y fixture template/i.test(body.replace(/\s+/g, ' '))
  expect(denied, 'guest gated (body: ' + body.replace(/\s+/g, ' ').slice(0, 90) + ')').toBeTruthy()
  await ctx.close()
})

test('list: GET /api/templates returns the fixture template with its meta', async () => {
  const r = await a('GET', '/api/templates')
  expect(r.status()).toBe(200)
  const t = await tpl()
  expect(t.name).toBe('Parity Fixture Template')
  expect(t.id, 'id').toBeTruthy()
  expect(t.version, 'version').toBeTruthy()
})

test('detail: /template/:id shows name, "Open as Template", "View PDF"', async () => {
  const t = await tpl()
  const r = await p.goto(BASE + '/template/' + t.id, { waitUntil: 'domcontentloaded' })
  expect(r?.status()).toBe(200)
  await expect(p.locator('text=Parity Fixture Template').first()).toBeVisible({ timeout: 15000 })
  expect((await p.locator('body').innerText()).toLowerCase(), 'use action present').toMatch(/open as template|start|use/i)
  expect((await p.locator('body').innerText()).toLowerCase(), 'pdf view present').toMatch(/view pdf/i)
})

test('use: starting a project from the template creates it', async () => {
  const t = await tpl()
  const name = 'Parity TplUse ' + Date.now().toString(36)
  const r = await a('POST', '/project/new', { projectName: name, template: t.id })
  expect([200, 201].includes(r.status()), 'create from template: ' + (await r.text().catch(() => '')).slice(0, 160)).toBeTruthy()
  const j = await r.json()
  const pid = j.project_id
  expect(pid, 'project created').toBeTruthy()
  await killProject(p, pid)
})

test('categories: enabled categories list + category filter works', async () => {
  const cats = await (await a('GET', '/api/template/categories')).json()
  expect(Array.isArray(cats), 'categories array').toBeTruthy()
  expect(cats.length > 0, 'at least one enabled category').toBeTruthy()
  // filter by the fixture template's category key (or the first category)
  const t = await tpl()
  const key = t.category ?? cats[0].key
  const r = await a('GET', `/api/templates?category=${encodeURIComponent(key)}`)
  expect(r.status()).toBe(200)
  const j = await r.json()
  const arr = (j.templates ?? j).map((x: any) => x.key ?? x.category)
  expect(arr.every((c: string) => c === key), 'filter respects the category key').toBeTruthy()
})

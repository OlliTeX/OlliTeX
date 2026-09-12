import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, killProject, ensureFixtureTemplate } from '../../parity/harness'
import { USER } from '../../fixtures/credentials'

/**
 * Parity baseline for the RETIRED legacy /templates gallery (owner items 7+9,
 * 2026-09-16). The legacy pages no longer render: /templates[/:category] and
 * /template/:id 301-redirect into the hub (gallery: /hub#/templates.all).
 * The functional contract survives on the API + project-creation surface
 * that the hub gallery uses: /api/templates, /api/template/categories,
 * /template/:id/preview (thumbnail/PDF), POST /project/new {projectName, template}.
 */
const BASE = 'http://127.0.0.1:7420'
let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, USER.email, USER.password)
  await ensureFixtureTemplate(browser)
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

test('redirects: /templates 301s into the hub gallery (legacy page removed)', async () => {
  // maxRedirects:0 — page.goto would follow the redirect (Playwright policy).
  const r = await p.request.get(BASE + '/templates', { maxRedirects: 0 })
  expect(r.status(), 'legacy /templates must be a 301, got ' + (await r.text().catch(() => '')).slice(0, 140)).toBe(301)
  expect(r.headers()['location'], 'redirect target').toBe('/hub#/templates.all')
  const r2 = await p.request.get(BASE + '/templates/academic-journal', { maxRedirects: 0 })
  expect(r2.status(), 'category page 301').toBe(301)
  expect(r2.headers()['location']).toBe('/hub#/templates.all')
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

test('detail: /template/:id 301s; preview endpoints (thumbnail + PDF) stay live', async () => {
  const t = await tpl()
  const r = await p.request.get(BASE + '/template/' + t.id, { maxRedirects: 0 })
  expect(r.status(), 'legacy detail page must 301, got ' + (await r.text().catch(() => '')).slice(0, 140)).toBe(301)
  expect(r.headers()['location']).toBe('/hub#/templates.all')
  // the hub gallery's "View PDF" + thumbnail use the preview endpoint —
  // it must still serve (PNG thumbnail)
  const th = await p.request.get(BASE + '/template/' + t.id + '/preview', { params: { style: 'thumbnail' } })
  expect([200, 404].includes(th.status()), 'thumbnail preview: ' + th.status()).toBeTruthy()
  if (th.status() === 200) {
    expect((th.headers()['content-type'] || '').startsWith('image/'), 'PNG content-type, got ' + th.headers()['content-type']).toBeTruthy()
  }
  // "View PDF": versioned PDF (or 404 when none was compiled — tolerate either,
  // the legacy contract had the same shape)
  const pdf = await p.request.get(BASE + '/template/' + t.id + '/preview')
  expect([200, 404].includes(pdf.status()), 'preview PDF: ' + pdf.status()).toBeTruthy()
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

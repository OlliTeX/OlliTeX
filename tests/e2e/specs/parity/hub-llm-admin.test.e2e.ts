import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { ADMIN, USER, TPLADMIN } from '../../fixtures/credentials'

// Parity HUB side (hub admin LLM leaves, /hub#/site.llm.*) — the same
// site-admin LLM surface as legacy /admin/llm/settings: settings JSON,
// save, check, models, usage — all served by the shared admin endpoints.
const BASE = 'http://127.0.0.1:7420'
const PAGE = BASE + '/hub#/site.llm.features'
let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
  await p.goto(PAGE, { waitUntil: 'domcontentloaded' })
})
test.afterAll(async () => { if (p) await p.context().close().catch(() => {}) })
const a = (method: string, path: string, body?: unknown) => api(p, method, path, body)
const settings = async () => (await a('GET', '/admin/llm/settings/json')).json().catch(() => null)

test('renders: hub LLM features leaf renders the admin LLM settings', async () => {
  await expect(p.getByRole('heading', { name: /features|llm/i }).first()).toBeVisible({ timeout: 15000 })
  const body = (await p.locator('body').innerText()) || ''
  expect(/api|model|prompt|enabled/i.test(body), 'llm settings controls present').toBeTruthy()
})

test('deny: non-site-admins cannot open the hub LLM admin leaves', async ({ browser }) => {
  for (const who of [TPLADMIN, USER]) {
    const ctx = await browser.newContext(); const q = await ctx.newPage()
    await loginRobust(q, who.email, who.password)
    await q.goto(PAGE, { waitUntil: 'domcontentloaded' })
    await q.waitForTimeout(1200)
    const body = (await q.locator('body').innerText().catch(() => '')) || ''
    expect(/system prompt|allowed models/i.test(body), who.email + ' must NOT see the admin LLM form').toBeFalsy()
    await ctx.close()
  }
})

test('settings-json: the hub leaf is backed by the same /admin/llm/settings/json data', async () => {
  const s = await settings()
  expect(s, 'settings object (shared API)').toBeTruthy()
  expect('hasLlmApiKey' in s, 'hasLlmApiKey flag').toBeTruthy()
  expect(Array.isArray(s.allowedModels), 'allowedModels array').toBeTruthy()
})

test('save: POST /admin/llm/settings round-trips from the hub admin surface', async () => {
  const before = await settings()
  expect(before, 'settings object').toBeTruthy()
  const prompt = 'parity-hub ' + Date.now()
  try {
    const r = await a('POST', '/admin/llm/settings', { ...before, systemPrompt: prompt })
    expect(r.status(), 'save status ' + r.status).toBeTruthy()
    expect(r.status()).toBeLessThan(500)
    const after = await settings()
    expect(after?.systemPrompt, 'systemPrompt persisted (hub save path)').toBe(prompt)
  } finally {
    await a('POST', '/admin/llm/settings', { ...before }).catch(() => {})
    const back = await settings()
    expect(back?.systemPrompt, 'restored').toBe(before?.systemPrompt)
  }
})

test('check: the same check endpoint answers the hub admin UI', async () => {
  const r = await a('POST', '/admin/llm/settings/check', { url: 'http://127.0.0.1:9/x' })
  expect(r.status(), 'check status').toBeLessThan(500)
})

test('models: the same models endpoint serves the hub admin UI', async () => {
  const r = await a('POST', '/admin/llm/models', {})
  expect(r.status(), 'models status').toBeLessThan(500)
})

test('usage: the same usage endpoint backs the hub usage meter', async () => {
  const u = await (await a('GET', '/admin/llm/usage')).json().catch(() => null)
  expect(u, 'usage object').toBeTruthy()
  expect(u.ok === true, 'ok flag').toBeTruthy()
  expect(typeof u.calls === 'number', 'calls number').toBeTruthy()
  expect(Array.isArray(u.byDay), 'byDay array').toBeTruthy()
})

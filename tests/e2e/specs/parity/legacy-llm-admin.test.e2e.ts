import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { ADMIN, USER, TPLADMIN } from '../../fixtures/credentials'

// Parity baseline (legacy /admin/llm/settings) — site-admin LLM settings:
// settings JSON, save (round-trip), connection check, model scan, usage.
const BASE = 'http://127.0.0.1:7420'
const PAGE = BASE + '/admin/llm/settings'
let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
})
test.afterAll(async () => { if (p) await p.context().close().catch(() => {}) })
const a = (method: string, path: string, body?: unknown) => api(p, method, path, body)
const settings = async () => (await a('GET', '/admin/llm/settings/json')).json().catch(() => null)

test('renders: /admin/llm/settings loads for the site admin', async () => {
  await p.goto(PAGE, { waitUntil: 'domcontentloaded' })
  expect(p.url(), 'on the page').toContain('/admin/llm/settings')
  const body = (await p.locator('body').innerText()) || ''
  await expect(p.locator('input:visible, select:visible, button:visible').first()).toBeVisible({ timeout: 15000 })
  expect(/llm|system prompt|api|model/i.test(body), 'llm settings surface').toBeTruthy()
})

test('deny: non-site-admins are denied the admin LLM settings', async ({ browser }) => {
  for (const who of [TPLADMIN, USER]) {
    const ctx = await browser.newContext(); const q = await ctx.newPage()
    await q.goto(PAGE, { waitUntil: 'domcontentloaded' }).catch(() => {})
    const ok = /login|signin|denied|forbidden/i.test(q.url()) || !/system prompt/i.test((await q.locator('body').innerText().catch(() => '')) || '')
    expect(ok, who.email + ' denied').toBeTruthy()
    await ctx.close()
  }
})

test('settings-json: GET /admin/llm/settings/json returns the settings object', async () => {
  const s = await settings()
  expect(s, 'settings object').toBeTruthy()
  expect(typeof s.llmApiType === 'string' || s.llmApiType === undefined, 'llmApiType field').toBeTruthy()
  expect('hasLlmApiKey' in s, 'hasLlmApiKey flag').toBeTruthy()
  expect(Array.isArray(s.allowedModels), 'allowedModels array').toBeTruthy()
})

test('save: POST /admin/llm/settings persists and reads back (round-trip)', async () => {
  const before = await settings()
  expect(before, 'settings object').toBeTruthy()
  const prompt = 'parity ' + Date.now()
  try {
    const r = await a('POST', '/admin/llm/settings', { ...before, systemPrompt: prompt })
    expect(r.status(), 'save status ' + r.status()).toBeLessThan(500)
    const after = await settings()
    expect(after?.systemPrompt, 'systemPrompt persisted').toBe(prompt)
  } finally {
    await a('POST', '/admin/llm/settings', { ...before }).catch(() => {})
    const back = await settings()
    expect(back?.systemPrompt, 'restored').toBe(before?.systemPrompt)
  }
})

test('check: POST /admin/llm/settings/check responds gracefully (no endpoint configured)', async () => {
  const r = await a('POST', '/admin/llm/settings/check', { url: 'http://127.0.0.1:9/x' })
  expect(r.status(), 'check status ' + r.status).toBeTruthy()
  expect(r.status()).toBeLessThan(500)
})

test('models: POST /admin/llm/models responds (scan is graceful without a key)', async () => {
  const r = await a('POST', '/admin/llm/models', {})
  expect(r.status(), 'models status ' + r.status()).toBeLessThan(500)
})

test('usage: GET /admin/llm/usage returns {ok, calls, tokens, byDay[]}', async () => {
  const u = await (await a('GET', '/admin/llm/usage')).json().catch(() => null)
  expect(u, 'usage object').toBeTruthy()
  expect(u.ok === true, 'ok flag').toBeTruthy()
  expect(typeof u.calls === 'number', 'calls number').toBeTruthy()
  expect(Array.isArray(u.byDay), 'byDay array').toBeTruthy()
})

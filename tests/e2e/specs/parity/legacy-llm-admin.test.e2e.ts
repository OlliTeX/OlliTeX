import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { ADMIN, USER, TPLADMIN } from '../../fixtures/credentials'

// Parity baseline (legacy /admin/llm/settings) — site-admin LLM settings:
// settings JSON, save (round-trip), connection check, model scan, usage.
// 2026-09-10 (owner queue 5): the legacy PAGE is REMOVED — /admin/llm/settings
// now 301s to the hub surface (Site → LLM → Instance), which manages the same
// endpoints. The API contract tests below are unchanged; the page tests now
// assert the redirect instead of the removed UI.
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

test('redirects: /admin/llm/settings 301 → /hub#/site.llm.instance (page removed 2026-09-10)', async () => {
  const res = await p.request.get(PAGE, { maxRedirects: 0 })
  expect(res.status(), '301 expected').toBe(301)
  expect(res.headers()['location']).toBe('/hub#/site.llm.instance')
})

test('deny: non-site-admins are denied the admin LLM settings (gate enforced on the hub)', async ({ browser }) => {
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
  expect(r.status(), 'models status ' + r.status).toBeLessThan(500)
})

test('usage: GET /admin/llm/usage returns {ok, calls, tokens, byDay[]}', async () => {
  const u = await (await a('GET', '/admin/llm/usage')).json().catch(() => null)
  expect(u, 'usage object').toBeTruthy()
  expect(u.ok === true, 'ok flag').toBeTruthy()
  expect(typeof u.calls === 'number', 'calls number').toBeTruthy()
  expect(Array.isArray(u.byDay), 'byDay array').toBeTruthy()
})

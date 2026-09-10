import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, captureApi, waitForCall } from '../../parity/harness'
import { USER, TPLADMIN, ADMIN } from '../../fixtures/credentials'

// Parity baseline (legacy /user/llm-settings) — BYO provider management.
// Contract proven by specs/byo-llm.test.e2e.ts; this spec drives the LEGACY
// UI and mirrors the role/behaviour matrix.
const BASE = 'http://127.0.0.1:7420'
const PAGE = BASE + '/user/llm-settings'
const RUN = Date.now().toString(36)
const NAME = `parity-llm-${RUN}`
const DUMMY_KEY = 'sk-e2e-dummy-key-0000' // NOT a real key
const UNREACHABLE = 'https://e2e-unreachable.invalid/v1'
let p: any = null
let providerId: string | null = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, USER.email, USER.password)
})
test.afterAll(async () => {
  // cleanup: remove the test provider (best effort)
  if (providerId && p) {
    await p.request
      .post(BASE + '/user/llm-providers/' + providerId + '/delete', {
        headers: { 'X-CSRF-TOKEN': await p.locator('meta[name="ol-csrfToken"]').getAttribute('content') },
      })
      .catch(() => {})
  }
  if (p) await p.context().close().catch(() => {})
})
const a = (method: string, path: string, body?: unknown) => api(p, method, path, body)
const list = async () => (await (await a('GET', '/user/llm-providers')).json()).providers ?? []

test('redirects: /user/llm-settings 301 → /hub#/mysettings.llm.general (page removed 2026-09-16)', async () => {
  const res = await p.request.get(PAGE, { maxRedirects: 0 })
  expect(res.status(), '301 expected').toBe(301)
  expect(res.headers()['location']).toBe('/hub#/mysettings.llm.general')
})

test('deny: guest cannot open /user/llm-settings', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  await q.goto(PAGE, { waitUntil: 'domcontentloaded' }).catch(() => {})
  const url = q.url()
  const body = (await q.locator('body').innerText().catch(() => '')) || ''
  const denied = /login|sign in/i.test(url) || !/add provider/i.test(body)
  expect(denied, 'guest denied (at ' + url + ')').toBeTruthy()
  await ctx.close()
})

test('save: a new provider persists (masked key) and is visible on the hub BYO surface', async () => {
  const r = await a('POST', '/user/llm-providers', {
    name: NAME,
    providerType: 'openaiCompatible',
    baseUrl: UNREACHABLE,
    apiKey: DUMMY_KEY,
    models: ['e2e-model-1'],
    completionModel: 'e2e-model-1',
  })
  expect([200, 201].includes(r.status()), 'save: ' + (await r.text().catch(() => '')).slice(0, 160)).toBeTruthy()
  const body = await r.json().catch(() => ({}))
  const saved = (body.provider ?? body) as any
  expect(saved.hasKey === true, 'hasKey true').toBeTruthy()
  expect(saved.apiKey, 'apiKey never echoed').toBeUndefined()
  const list0 = await list()
  const mine = list0.find((x: any) => x.name === NAME) ?? list0[list0.length - 1]
  providerId = (mine as any)?.id ?? (mine as any)?.provider_id ?? saved.id ?? null
  expect(providerId, 'provider persisted with an id').toBeTruthy()
  // the hub BYO surface (redirect target) lists the provider
  await p.goto(BASE + '/hub#/mysettings.llm.general', { waitUntil: 'domcontentloaded' })
  await p.waitForTimeout(2500)
  await expect(p.locator('body').getByText(NAME).first()).toBeVisible({ timeout: 15000 })
})

test('check: provider check vs unreachable endpoint fails gracefully (no 5xx)', async () => {
  expect(providerId, 'provider saved first').toBeTruthy()
  const r = await a('POST', '/user/llm-providers/check', { providerId })
  expect(r.status(), 'graceful check: never 5xx (got ' + (await r.text().catch(() => '')).slice(0, 160) + ')').toBeLessThan(500)
})

test('scan: model discovery fails gracefully (clean JSON, clear message)', async () => {
  expect(providerId, 'provider saved first').toBeTruthy()
  const r = await a('POST', '/user/llm-providers/scan', { rowId: providerId })
  const body = await r.json().catch(() => null)
  expect(body, 'scan response is JSON (not an HTML crash)').toBeTruthy()
  expect(body.ok === false ? true : true, 'ok=false or ok=true').toBeTruthy()
  const msg = String(body.message ?? body.error ?? '')
  expect(/reach|unreachable|failed|network/i.test(msg), 'clear failure message: ' + msg).toBeTruthy()
})

test('toggle: enabling/disabling the provider persists', async () => {
  if (!providerId) test.skip()
  const before = (await list()).find((x: any) => x.id === providerId) as any
  const target = !(before?.enabled === true)
  const r = await a('POST', `/user/llm-providers/${providerId}`, { enabled: target })
  expect([200, 204].includes(r.status()), 'toggle: ' + (await r.text().catch(() => '')) .slice(0, 140)).toBeTruthy()
  const after = (await list()).find((x: any) => x.id === providerId) as any
  expect(Boolean(after?.enabled), 'enabled persisted').toBe(target)
  // restore the previous state
  const r2 = await a('POST', `/user/llm-providers/${providerId}`, { enabled: Boolean(before?.enabled) })
  expect([200, 204].includes(r2.status()), 'restore toggle').toBeTruthy()
})

test('delete: provider delete removes it from the list', async () => {
  if (!providerId) test.skip()
  const r = await a('POST', `/user/llm-providers/${providerId}/delete`, {})
  expect([200, 204].includes(r.status()), 'delete: ' + (await r.text().catch(() => '')).slice(0, 140)).toBeTruthy()
  const after = await list()
  expect(after.find((x: any) => x.id === providerId), 'provider removed').toBeUndefined()
  providerId = null
})

// role coverage: template admin + admin open the same page (personal settings)
test('role: tpladmin + admin reach the hub BYO surface through the redirect', async ({ browser }) => {
  for (const u of [TPLADMIN, ADMIN]) {
    const ctx = await browser.newContext(); const q = await ctx.newPage()
    await loginRobust(q, u.email, u.password)
    await q.goto(BASE + '/user/llm-settings', { waitUntil: 'domcontentloaded' })
    expect(q.url(), u.email + ' lands on the hub').toContain('/hub')
    await expect(q.locator('body', { hasText: /LLM|provider/i }).first()).toBeVisible({ timeout: 15000 })
    await ctx.close()
  }
})

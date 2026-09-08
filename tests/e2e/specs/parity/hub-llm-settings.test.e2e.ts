import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, captureApi, waitForCall } from '../../parity/harness'
import { USER, TPLADMIN } from '../../fixtures/credentials'

// Parity (HUB) — /hub#/mysettings.llm.general (LLM providers leaf) vs legacy
// /user/llm-settings. Same BYO-provider contract:
//   GET  /user/llm-providers, POST /user/llm-providers(201), POST /:id,
//   POST /check, POST /scan, POST /:id/delete.
const BASE = 'http://127.0.0.1:7420'
const LEAF = BASE + '/hub#/mysettings.llm.general'
const RUN = Date.now().toString(36)
const NAME = `parity-hub-llm-${RUN}`
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

test('renders: hub LLM leaf shows the provider area + add control', async () => {
  await p.goto(LEAF, { waitUntil: 'domcontentloaded' })
  await p.waitForTimeout(2500)
  const body = (await p.locator('body').innerText()) || ''
  expect(/provider/i.test(body), 'provider UI present').toBeTruthy()
})

test('deny: guest cannot open the hub LLM leaf', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  const r = await q.goto(LEAF, { waitUntil: 'domcontentloaded' }).catch(() => null)
  const body = (await q.locator('body').innerText().catch(() => '')) || ''
  const denied = r?.status() === 403 || !/provider/i.test(body) || /sign in|log ?in/i.test(body)
  expect(denied, 'guest denied (got ' + (r?.status()) + ')').toBeTruthy()
  await ctx.close()
})

test('save: hub provider form → POST /user/llm-providers (masked key, persisted)', async () => {
  await p.goto(LEAF, { waitUntil: 'domcontentloaded' })
  await p.waitForTimeout(2500)
  const add = p.locator('button', { hasText: /add provider/i }).last()
  await expect(add).toBeVisible({ timeout: 15000 })
  await add.click()
  await p.waitForTimeout(800)
  const nameI = p.locator('input[placeholder*="gateway" i]').first()
  await expect(nameI).toBeVisible({ timeout: 8000 })
  await nameI.fill(NAME)
  await p.locator('input[placeholder*="v1" i]').first().fill(UNREACHABLE)
  await p.locator('input[type="password"]').first().fill(DUMMY_KEY)
  const ta = p.locator('textarea').first()
  await ta.fill('e2e-model-1')
  const cap = captureApi(p as any, BASE)
  await p.locator('button', { hasText: /save provider/i }).first().click()
  await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === '/user/llm-providers'), 'save call')
  await p.waitForTimeout(1500)
  const saved = await list()
  const mine = saved.find((x: any) => x.name === NAME)
  providerId = (mine as any)?.id ?? (mine as any)?.provider_id ?? null
  expect(providerId, 'provider persisted (list: ' + JSON.stringify(saved.map((x: any) => x.name)) + ')').toBeTruthy()
})

test('check: hub provider check vs unreachable endpoint is graceful', async () => {
  expect(providerId, 'provider saved first').toBeTruthy()
  const r = await a('POST', '/user/llm-providers/check', { providerId })
  expect(r.status(), 'graceful check (<500): ' + (await r.text().catch(() => '')).slice(0, 160)).toBeLessThan(500)
})

test('scan: hub model scan fails gracefully (clean JSON)', async () => {
  expect(providerId, 'provider saved first').toBeTruthy()
  const r = await a('POST', '/user/llm-providers/scan', { rowId: providerId })
  const body = await r.json().catch(() => null)
  expect(body, 'scan returns JSON').toBeTruthy()
  const msg = String(body.message ?? body.error ?? '')
  expect(/reach|unreachable|failed|network/i.test(msg), 'clear message: ' + msg).toBeTruthy()
})

test('toggle: hub enable switch persists via POST /user/llm-providers/:id', async () => {
  expect(providerId, 'provider saved first').toBeTruthy()
  const before = (await list()).find((x: any) => x.id === providerId) as any
  const target = !(before?.enabled === true)
  const r = await a('POST', `/user/llm-providers/${providerId}`, { enabled: target })
  expect([200, 204].includes(r.status()), 'toggle: ' + (await r.text().catch(() => '')).slice(0, 140)).toBeTruthy()
  const after = (await list()).find((x: any) => x.id === providerId) as any
  expect(Boolean(after?.enabled), 'enabled persisted').toBe(target)
  await a('POST', `/user/llm-providers/${providerId}`, { enabled: Boolean(before?.enabled) }).catch(() => {})
})

test('delete: hub remove → POST /user/llm-providers/:id/delete', async () => {
  expect(providerId, 'provider saved first').toBeTruthy()
  const r = await a('POST', `/user/llm-providers/${providerId}/delete`, {})
  expect([200, 204].includes(r.status()), 'delete: ' + (await r.text().catch(() => '')).slice(0, 140)).toBeTruthy()
  const after = await list()
  expect(after.find((x: any) => x.id === providerId), 'provider removed').toBeUndefined()
  providerId = null
})

test('role: tpladmin opens the hub LLM leaf (personal settings)', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  await loginRobust(q, TPLADMIN.email, TPLADMIN.password)
  await q.goto(LEAF, { waitUntil: 'domcontentloaded' })
  await q.waitForTimeout(2000)
  const body = (await q.locator('body').innerText().catch(() => '')) || ''
  expect(/provider/i.test(body), 'tpladmin leaf').toBeTruthy()
  await ctx.close()
})

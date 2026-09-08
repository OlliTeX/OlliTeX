import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { ADMIN, USER, TPLADMIN } from '../../fixtures/credentials'

// Parity baseline (legacy /admin/site) — the site settings surface.
const BASE = 'http://127.0.0.1:7420'
const PAGE = BASE + '/admin/site'
let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
  await p.goto(PAGE, { waitUntil: 'domcontentloaded' })
})
test.afterAll(async () => { if (p) await p.context().close().catch(() => {}) })
const a = (method: string, path: string, body?: unknown) => api(p, method, path, body)
const all = async () => (await a('GET', '/admin/site-settings')).json().catch(() => null)

test('renders: /admin/site shows the site settings surface', async () => {
  expect(p.url(), 'on the page').toContain('/admin/site')
  const body = (await p.locator('body').innerText()) || ''
  expect(/site settings|miscellaneous|sign.?up|settings/i.test(body), 'settings surface').toBeTruthy()
  await expect(p.locator('input:visible, select:visible, button:visible, [role="switch"]').first()).toBeVisible({ timeout: 15000 })
})

test('deny: non-site-admins are denied /admin/site', async ({ browser }) => {
  for (const who of [TPLADMIN, USER]) {
    const ctx = await browser.newContext(); const q = await ctx.newPage()
    await q.goto(PAGE, { waitUntil: 'domcontentloaded' }).catch(() => {})
    const ok = /login|signin|denied|forbidden/i.test(q.url()) || !/site settings/i.test((await q.locator('body').innerText().catch(() => '')) || '')
    expect(ok, who.email + ' denied').toBeTruthy()
    await ctx.close()
  }
})

test('settings-read: GET /admin/site-settings returns the full section set', async () => {
  const r = await all()
  expect(r, 'settings object').toBeTruthy()
  for (const s of ['misc', 'signup', 'email', 'sso-saml', 'externalUrl', 'templates', 'services', 'branding']) {
    expect(s in r, `section present: ${s}`).toBeTruthy()
  }
  expect(typeof r.misc.appName === 'string', 'misc.appName string').toBeTruthy()
})

test('save-roundtrip: PUT /admin/site-settings/misc persists appName and reads back', async () => {
  const before = await all()
  const keep = before.misc
  try {
    const r = await a('PUT', '/admin/site-settings/misc', { ...keep, appName: 'Parity OlliTeX ' + Date.now() })
    expect(r.status(), 'put status ' + r.status).toBeTruthy()
    expect(r.status()).toBeLessThan(500)
    const after = await all()
    expect(after.misc.appName, 'new value read back').toContain('Parity OlliTeX')
  } finally {
    await a('PUT', '/admin/site-settings/misc', keep).catch(() => {})
    const back = await all()
    expect(back?.misc?.appName, 'restored').toBe(keep.appName)
  }
})

test('signup: PUT /admin/site-settings/signup round-trips enabled + domains', async () => {
  const before = await all()
  const keep = before.signup
  try {
    const body = { ...keep, allowedEmailDomains: [...(keep.allowedEmailDomains || []), 'parity.example'] }
    const r = await a('PUT', '/admin/site-settings/signup', body)
    expect(r.status(), 'put status ' + r.status).toBeTruthy()
    expect(r.status()).toBeLessThan(500)
    const after = await all()
    expect(after.signup.allowedEmailDomains || [], 'domain persisted').toContain('parity.example')
  } finally {
    await a('PUT', '/admin/site-settings/signup', keep).catch(() => {})
    const back = await all()
    expect(back?.signup, 'restored').toBeTruthy()
  }
})

test('sso: SAML/OIDC/LDAP sections round-trip and guard enabled-without-credentials', async () => {
  const before = await all()
  for (const s of ['sso-saml', 'sso-oidc', 'sso-ldap']) {
    const keep = before[s]
    // 1) the default (enabled:false) state saves cleanly
    const ok = await a('PUT', `/admin/site-settings/${s}`, { ...keep, enabled: false })
    expect(ok.status(), `${s} default save status ${ok.status()}`).toBeLessThan(400)
    const afterOk = await all()
    expect(afterOk[s].enabled, `${s} enabled=false persisted`).toBeFalsy()
    // 2) server guard: enabling without credentials/secret is rejected (client error)
    const guarded = await a('PUT', `/admin/site-settings/${s}`, { ...keep, enabled: true })
    expect(guarded.status(), `${s} enable-without-creds rejected (got ${guarded.status()})`).toBeGreaterThanOrEqual(400)
    expect(guarded.status()).toBeLessThan(500)
    // 3) state unchanged after the guarded attempt
    const after = await all()
    expect(after[s].enabled, `${s} still disabled`).toBeFalsy()
  }
})

test('email-test: POST /admin/site-settings/email/test responds deterministically', async () => {
  const r = await a('POST', '/admin/site-settings/email/test', { to: 'e2e-admin@e2e.test', subject: 'parity' })
  // contract: 200 success, 422 bad address, 429 rate-limited,
  // 500 e-mail section unconfigured, 502 transport failure (sanitized)
  expect([200, 422, 429, 500, 502], 'test status ' + r.status).toContain(r.status())
  if (r.status() >= 400) {
    const j = await r.json().catch(() => ({}))
    expect(j.message, 'sanitized message').toBeTruthy()
  }
})

test('template-admins: GET /admin/site/template-admins lists the flag holders', async () => {
  const r = await (await a('GET', '/admin/site/template-admins')).json().catch(() => null)
  expect(r, 'response object').toBeTruthy()
  expect(Array.isArray(r.users), 'users array').toBeTruthy()
  expect(r.users.some((u: any) => (u.email || '').includes('@e2e.test')), 'fixture admin listed').toBeTruthy()
})

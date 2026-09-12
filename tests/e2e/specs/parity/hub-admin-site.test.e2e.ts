import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { ADMIN, USER, TPLADMIN } from '../../fixtures/credentials'

// Parity HUB side (hub site-settings leaves) — the same site settings surface
// as legacy /admin/site: same GET/PUT section endpoints driven from /hub leaves.
const BASE = 'http://127.0.0.1:7420'
let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
  await p.goto(BASE + '/hub#/site.general.enclose', { waitUntil: 'domcontentloaded' })
})
test.afterAll(async () => { if (p) await p.context().close().catch(() => {}) })
const a = (method: string, path: string, body?: unknown) => api(p, method, path, body)
const all = async () => (await a('GET', '/admin/site-settings')).json().catch(() => null)

test('renders: the hub "Full site settings" index maps every section', async () => {
  await expect(p.getByRole('heading', { name: /site settings/i }).first()).toBeVisible({ timeout: 15000 })
  const body = (await p.locator('body').innerText()) || ''
  for (const label of ['Miscellaneous', 'Sign-up', 'SSO', 'Email', 'Sandboxed', 'Branding']) {
    expect(new RegExp(label, 'i').test(body), `index lists ${label}`).toBeTruthy()
  }
})

test('deny: non-site-admins cannot open the hub site-settings leaves', async ({ browser }) => {
  for (const who of [TPLADMIN, USER]) {
    const ctx = await browser.newContext(); const q = await ctx.newPage()
    await loginRobust(q, who.email, who.password)
    await q.goto(BASE + '/hub#/site.general.misc', { waitUntil: 'domcontentloaded' })
    await q.waitForTimeout(1200)
    const body = (await q.locator('body').innerText().catch(() => '')) || ''
    expect(/app name|sign-?up|miscellaneous/i.test(body), who.email + ' must not get the site form').toBeFalsy()
    await ctx.close()
  }
})

test('settings-read: the hub sections read the same /admin/site-settings data', async () => {
  const r = await all()
  expect(r, 'settings object (shared API)').toBeTruthy()
  for (const s of ['misc', 'signup', 'email', 'sso-saml', 'externalUrl']) {
    expect(s in r, `section present: ${s}`).toBeTruthy()
  }
})

test('save-roundtrip: the hub misc section PUTs the same endpoint (appName)', async () => {
  const before = await all()
  const keep = before.misc
  try {
    const r = await a('PUT', '/admin/site-settings/misc', { ...keep, appName: 'Hub Parity ' + Date.now() })
    expect(r.status(), 'put status ' + r.status).toBeTruthy()
    expect(r.status()).toBeLessThan(500)
    const after = await all()
    expect(after.misc.appName, 'new value read back').toContain('Hub Parity')
  } finally {
    await a('PUT', '/admin/site-settings/misc', keep).catch(() => {})
    const back = await all()
    expect(back?.misc?.appName, 'restored').toBe(keep.appName)
  }
})

test('signup: the hub sign-up section PUT round-trips', async () => {
  const before = await all()
  const keep = before.signup
  try {
    const r = await a('PUT', '/admin/site-settings/signup', { ...keep, allowedEmailDomains: [...(keep.allowedEmailDomains || []), 'hub.example'] })
    expect(r.status(), 'put status ' + r.status).toBeTruthy()
    expect(r.status()).toBeLessThan(500)
    const after = await all()
    expect(after.signup.allowedEmailDomains || [], 'domain persisted').toContain('hub.example')
  } finally {
    await a('PUT', '/admin/site-settings/signup', keep).catch(() => {})
  }
})

test('sso: the hub SSO leaves PUT the same sso-* endpoints (with the server guard)', async () => {
  const before = await all()
  for (const s of ['sso-saml', 'sso-oidc', 'sso-ldap']) {
    const keep = before[s] || {}
    try {
      // disabling is always allowed
      const ok = await a('PUT', `/admin/site-settings/${s}`, { ...keep, enabled: false })
      expect(ok.status(), `${s} disable status ${ok.status()}`).toBeLessThan(400)
      const afterOk = await all()
      expect(afterOk[s].enabled, `${s} enabled=false persisted`).toBeFalsy()

      // the server guard (state-independent): enabling WITHOUT credentials
      // must be rejected, whatever the previously stored config was
      const guarded = await a('PUT', `/admin/site-settings/${s}`, {
        enabled: true,
        identityServiceName: 'Guard Probe',
        issuer: '',
        entryPoint: '',
        authorizationURL: '',
        tokenURL: '',
        url: '',
        clientID: '',
        idpCert: '',
        privateKey: '',
      })
      expect(guarded.status(), `${s} enable-without-creds rejected (got ${guarded.status()})`).toBeGreaterThanOrEqual(400)
      expect(guarded.status()).toBeLessThan(500)
      const after = await all()
      expect(after[s].enabled, `${s} still disabled`).toBeFalsy()
    } finally {
      // restore the section exactly as found (the e2e seed leaves SAML
      // configured; the other two disabled)
      await a('PUT', `/admin/site-settings/${s}`, keep).catch(() => {})
    }
  }
})

test('email-test: the same email-test endpoint answers the hub', async () => {
  const r = await a('POST', '/admin/site-settings/email/test', { to: 'e2e-admin@e2e.test', subject: 'parity-hub' })
  expect([200, 422, 429, 500, 502], 'test status ' + r.status).toContain(r.status())
  if (r.status() >= 400) {
    const j = await r.json().catch(() => ({}))
    expect(j.message, 'sanitized message').toBeTruthy()
  }
})

test('template-admins: the same template-admins endpoint serves the hub', async () => {
  const r = await (await a('GET', '/admin/site/template-admins')).json().catch(() => null)
  expect(r, 'response object').toBeTruthy()
  expect(Array.isArray(r.users), 'users array').toBeTruthy()
})

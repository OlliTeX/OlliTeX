/**
 * zotero link cycle — CONNECT (3-legged OAuth) + LINKED STATE + DISCONNECT,
 * live against the owner's zotero.org account (credentials in the LOCAL,
 * git-ignored tests/e2e/credentials/config.json §zotero; app client key/
 * secret on the overleaf container via .env.test ZOTERO_CLIENT_*).
 *
 * Product wire (go/services/web/features/zotero, Node parity):
 *   connect    = GET /user/zotero/oauth → 302 zotero.org authorize
 *                (server request-token step, app creds) → authorize →
 *                GET /user/zotero/oauth/callback → server exchange →
 *                users.refProviders.zotero.apiKeyEncrypted {apiKey,zoteroUserId}
 *   status     = GET /user/zotero/status → true/false (true ⇒ live
 *                GET api.zotero.org/keys/<apiKey> check)
 *   groups     = GET /user/zotero/groups   (not linked → 200 null)
 *   disconnect = DELETE /user/zotero       ($unset refProviders.zotero +
 *                best-effort key revocation) → 200 OK; no-op when not linked
 *
 *   FLAG (owner, 2026-10-12): the zotero.org LOGIN leg is CLOUDFLARE
 *   TURNSTILE-gated — the submit button stays inert headlessly (no POST is
 *   fired; raw form POST → 400) on datacenter egress. Per policy we do NOT
 *   attempt a CAPTCHA bypass. Two supported modes:
 *     a) operator-assisted (recommended): run once with a headed browser on
 *        an owner-trusted machine —
 *        ZOTERO_MANUAL_LOGIN=1 npx playwright test specs/zotero-link-cycle.test.e2e.ts --headed
 *        and complete the Turnstile/login in the window; the spec then
 *        finishes authorize → callback → linked state → DISCONNECT
 *        (the disconnect is product-side and fully automated);
 *     b) automated partial (this world): Z1/Z2 run headless-green, the
 *        login leg detects the wall and skips cleanly with evidence, and
 *        the unlink no-op pins (Z5/Z6) still cover the disconnect surface.
 *   Everything AFTER the link (status=true, groups, disconnect, idempotent
 *   unlink, key revocation) is product-side and needs no zotero.org access.
 */
import { test, expect } from '@playwright/test'
import { USER } from '../fixtures/credentials'
import { loginRobust } from '../helpers/auth'
import { api, mongoEval } from '../parity/harness'
import { getExternal, externalEnabled } from '../credentials/external'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const MANUAL = process.env.ZOTERO_MANUAL_LOGIN === '1'
function localUserId(): string {
  const raw = String(
    mongoEval(`'X' + db.users.findOne({ email: '${USER.email.replace(/'/g, '')}' })._id.toHexString()`),
  ).replace(/^X/, '').trim()
  if (!/^[0-9a-f]{24}$/.test(raw)) throw new Error('local user id unresolvable: ' + raw.slice(0, 20))
  return raw
}

function zoteroCfg(): any {
  const z = getExternal('zotero')
  test.skip(!z, 'zotero not present in credentials/config.json')
  test.skip(externalEnabled('zotero') !== true, 'zotero disabled in credentials/config.json')
  return z
}

/** True when the submit produced a real POST (no Turnstile wall). */
async function loginAttempt(page: import('playwright').Page, cfg: any) {
  await page.goto('https://www.zotero.org/login', { waitUntil: 'domcontentloaded' })
  await page.fill('input[name="username"]', cfg.username)
  await page.fill('input[name="password"]', cfg.password)
  let posted = false
  const h = (r: import('playwright').Request) => {
    if (r.method() === 'POST' && /zotero\.org\/login/.test(r.url())) posted = true
  }
  page.on('request', h)
  await page
    .locator('button:has-text("Log In"), button:has-text("Log in to Zotero")')
    .first()
    .click({ timeout: 20_000 })
    .catch(() => {})
  await page.waitForTimeout(4_000)
  page.off('request', h)
  if (posted) return true // real login attempt happened (proceed to authz)
  // MANUAL mode: the operator finishes the (Turnstile) login in the window
  if (MANUAL) {
    console.log('Z3: waiting up to 5 minutes for the operator to complete the zotero.org login…')
    await page.waitForURL(
      (u) => !String(u).includes('/login'),
      { timeout: 300_000 },
    ).catch(() => {})
    return !String(page.url()).includes('/login')
  }
  return false
}

test.describe.configure({ mode: 'serial' })
test.setTimeout(420_000)

test('Z1: pre-state not linked + enabled-surface answers', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const s = await api(page, 'GET', '/user/zotero/status')
  expect(s.status()).toBe(200)
  expect((await s.text()).trim()).toBe('false')
  const g = await api(page, 'GET', '/user/zotero/groups')
  expect(g.status()).toBe(200)
  expect((await g.text()).trim()).toBe('null') // enabled + not linked (pinned)
  expect(String(mongoEval(`'X' + JSON.stringify(db.users.findOne({ _id: ObjectId("${localUserId()}") }).refProviders)`))).not.toContain('zotero')
})

let zoteroAuthzUrl = ''

test('Z2: connect START — /oauth 302s to zotero.org authorize (app creds live)', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  // maxRedirects:0 — the raw 302 + Location IS the contract (the harness
  // `api` helper follows the redirect to zotero.org by default)
  const r = await page.context().request.get('/user/zotero/oauth', { maxRedirects: 0 })
  expect(r.status(), 'oauth start (got ' + r.status() + ')').toBe(302)
  const location = r.headers()['location'] ?? ''
  expect(location, 'Location is the zotero.org authorize URL (got ' + location + ')')
    .toMatch(/^https:\/\/www\.zotero\.org\/oauth\/authorize\?/)
  expect(location).toContain('oauth_token=')
  zoteroAuthzUrl = location
})

test('Z3: zotero.org login + authorize + callback (operator-assisted when TURNSTILE-gated)', async ({ page }) => {
  const cfg = zoteroCfg()
  test.skip(
    !zoteroAuthzUrl,
    'Z2 did not produce an authorize URL (run Z2 first — serial describe)',
  )
  await loginRobust(page, USER.email, USER.password)
  const loggedIn = await loginAttempt(page, cfg)
  test.skip(
    !loggedIn,
    'zotero.org login is TURNSTILE-gated headlessly (no POST fired; raw POST → 400). ' +
      'Operator-assist once: ZOTERO_MANUAL_LOGIN=1 npx playwright test specs/zotero-link-cycle.test.e2e.ts --headed ' +
      '— everything downstream of the link (incl. disconnect) is then fully automated.',
  )

  // AUTHORIZE (pin what the live page offers; tolerant matcher)
  await page.goto(zoteroAuthzUrl, { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(2_000)
  await page
    .locator('button:has-text("Allow"), button:has-text("Permitieren"), button:has-text("Authorize")')
    .first()
    .click({ timeout: MANUAL ? 300_000 : 20_000 })
    .catch(() => {})
  // the redirect must land on OUR callback
  await page.waitForURL(
    (u) => String(u).includes('/user/zotero/oauth/callback'),
    { timeout: MANUAL ? 320_000 : 30_000 },
  )
  const cb = page.url()
  expect(cb).toContain('oauth_token=')
  const html = (await page.locator('body').innerText().catch(() => '')) +
    (await page.content().catch(() => ''))
  expect(html, 'callback HTML carries the zotero-linked hand-off').toMatch(/zotero-linked|zotero/)
})

test('Z4: LINKED state — status true (live /keys check) + groups 200', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const s = await api(page, 'GET', '/user/zotero/status')
  const linked = (await s.text()).trim() === 'true'
  test.skip(!linked, 'not linked (Z3 skipped — Turnstile wall; operator-assist once)')
  expect(s.status()).toBe(200)
  const g = await api(page, 'GET', '/user/zotero/groups')
  expect(g.status()).toBe(200)
  const gt = (await g.text()).trim()
  expect(gt === 'null' || gt === '[]' || /^\[/.test(gt), 'groups is a JSON list (got ' + gt.slice(0, 80) + ')').toBeTruthy()
  expect(String(mongoEval(`'X' + JSON.stringify(db.users.findOne({ _id: ObjectId("${localUserId()}") }).refProviders ?? {})`))).toContain('zotero')
})

test('Z5: disconnect — DELETE /user/zotero unlinks (+ no-op when already unlinked)', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const del = await api(page, 'DELETE', '/user/zotero')
  expect(del.status(), 'unlink (got ' + del.status() + ')').toBe(200)
  // the product-side result holds in BOTH states: status back/false at false
  const s = await api(page, 'GET', '/user/zotero/status')
  expect((await s.text()).trim(), 'status false after unlink').toBe('false')
})

test('Z6: idempotent unlink — DELETE again → 200 (pinned no-op contract)', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const del2 = await api(page, 'DELETE', '/user/zotero')
  expect(del2.status(), 'idempotent unlink (got ' + del2.status() + ')').toBe(200)
})

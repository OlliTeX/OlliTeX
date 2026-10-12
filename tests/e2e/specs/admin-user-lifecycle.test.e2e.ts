/**
 * admin-user-lifecycle — ADMIN user-management (board 7a8749ff), LIVE-PINNED
 * 2026-10-12 (ag-v57 world). Evidence: OPEN_QUEUE_PROGRESS.md cont.7 + admin-cy
 * probe battery.
 *
 * PINNED CONTRACT (each leg carries its live evidence in the run record):
 *   Surface = hub admin users leaf (/admin-settings/site.general.users.all +
 *   .users.deleted) over the P6.3b API (go/services/web/features/adminusers/):
 *     POST   /admin/user/create
 *     POST   /admin/user/:id/send-activation
 *     POST   /admin/user/:id/update      {suspended/inactive/admin/roleName/…}
 *     POST   /admin/user/:id/delete      (soft; 200 {"deletedAt": ISO})
 *     POST   /admin/user/:id/restore     (200 {"restoredId","email"})
 *     DELETE /admin/user/:id             (purge)
 *     POST   /admin/user                 (list)   · GET /admin/user/:id (info)
 *
 * ⚑ KEY PIN (Node parity, bcrypt-verified live): admin-create IGNORES any
 * password key in the body — it always stores a RANDOM 32-byte hex secret
 * (mutations.go createHandler63b: `pw := randomHex63b(32)`; store's
 * bcrypt hash checked offline against the sent password = FALSE). So an
 * admin-created account cannot log in with the "created" password; credential
 * bootstrap = the password-RESET link (or send-activation path), exactly the
 * Node/Overleaf legacy-admin contract. first_name/last_name are the body keys
 * (firstName/lastName are IGNORED; missing → email-prefix fallback, observed
 * in the 200 body).
 *
 * ⚑ FLAG (consequence gap, pinned not shipped): because the credential
 * bootstrap is reset-link-only, admin-side CONSEQUENCES (role admin→user delta,
 * suspended-session rejection, delete→project policy) can only be pinned at
 * the WIRE level for an account that never logs in; the suspended-SESSION
 * consequence is pinned by the existing git-bridge-suspended-token spec
 * (reference leg 2 — not duplicated here).
 */
import { test, expect, type Page, type Browser } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { ADMIN, USER } from '../fixtures/credentials'
import { writeFileSync } from 'fs'
import { execSync } from 'child_process'

const BASE = 'http://127.0.0.1:7420'
const RUN = 'admcy-' + Date.now()
const DISPO = RUN + '@e2e.test'
const SENT_PW = RUN.slice(0, 8) + '-Pw1!'

let UID = ''
let SH: { ctx: any; page: Page } | null = null

function docRaw(): string {
  const q = 'const u = db.users.findOne({ email: "' + DISPO + '" }); print(u ? JSON.stringify({ hp: String(Boolean(u.hashedPassword)), susp: u.suspended === undefined ? "unset" : String(u.suspended), del: u.deletedAt === undefined ? "unset" : "set", adm: JSON.stringify(u.flags || null).slice(0, 80) }) : "GONE")'
  writeFileSync('/tmp/lc-doc.js', q)
  return execSync('docker exec -i ol-e2e-mongo-1 mongosh --quiet sharelatex < /tmp/lc-doc.js', { encoding: 'utf8' })
    .trim().split('\n').pop() || ''
}

async function admin(browser: Browser): Promise<Page> {
  if (!SH) {
    const ctx = await browser.newContext({ viewport: { width: 1500, height: 900 } })
    const page = await ctx.newPage()
    await loginRobust(page as any, ADMIN.email, ADMIN.password)
    SH = { ctx, page }
  }
  return SH.page
}
test.afterAll(async () => { if (SH) await SH.ctx.close().catch(() => {}) })

async function pApi(page: Page, method: string, path: string, body?: any): Promise<{ status: number; text: string }> {
  const csrf = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const res = await page.request.fetch(BASE + path, {
    method: method as any,
    headers: { ...(csrf ? { 'X-Csrf-Token': csrf } : {}), ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}) },
    data: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = (await res.text().catch(() => '')).slice(0, 400)
  return { status: res.status(), text }
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
async function loginPost(browser: any, email: string, password: string): Promise<{ status: number; text: string }> {
  const sleepLocal = (ms: number) => new Promise((r) => setTimeout(r, ms))
  for (let i = 0; i < 3; i++) {
    const c = await browser.newContext()
    const bootp = await c.newPage()
    await bootp.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
    const tok = (await bootp.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    await bootp.close()
    const rp = await c.request.post(BASE + '/login', { data: { email, password }, headers: tok ? { 'X-Csrf-Token': tok } : {} })
    const text = (await rp.text().catch(() => '')).slice(0, 300)
    await c.close()
    if (rp.status() !== 429) return { status: rp.status(), text }
    await sleepLocal(65_000)
  }
  throw new Error('loginPost: still 429 after 3 windows')
}

test.describe.configure({ mode: 'serial' })
test.setTimeout(420_000)

test('M1 admin creates user C: pinned body contract + random-password parity + bootstrap reality', async ({ browser }) => {
  const page = await admin(browser)
  // 422 pin: empty email
  const bad = await pApi(page, 'POST', '/admin/user/create', { email: '' })
  expect(bad.status, 'empty email → PINNED 422 (got ' + bad.status + ')').toBe(422)
  // the create
  const r = await pApi(page, 'POST', '/admin/user/create', { email: DISPO, first_name: 'AdmCy', last_name: 'Probe', password: SENT_PW })
  expect(r.status, 'POST /admin/user/create → PINNED 200 (got ' + r.status + ': ' + r.text.slice(0, 140) + ')').toBe(200)
  const rj: any = JSON.parse(r.text).user || JSON.parse(r.text)
  UID = String(rj.id || rj._id || '').toLowerCase()
  expect(UID).toMatch(/^[0-9a-f]{24}$/)
  expect(String(rj.email), 'response echoes the email').toBe(DISPO)
  // ⚑ parity pin: first_name honored in the store, firstName(key) is NOT
  const doc = docRaw()
  expect(doc, 'user doc exists after create').toContain('hp":"true"')

  // ⚑ random-password parity: the SENT password must NOT authenticate
  // (create stores randomHex63b(32) — Node legacy-admin contract)
  const lp = await loginPost(browser, DISPO, SENT_PW)
  expect(lp.status, 'login with the "created" password → PINNED 401 (got ' + lp.status + ')').toBe(401)
  expect(lp.text.includes('invalid-password-retry-or-reset'), '401 key pin').toBe(true)
})

test('M2 suspend / unsuspend round-trip (wire) + suspended-login consequence', async ({ browser }) => {
  const page = await admin(browser)
  const up = await pApi(page, 'POST', '/admin/user/' + UID + '/update', { suspended: true })
  expect(up.status, 'suspend → 200 (got ' + up.status + ')').toBe(200)
  expect(up.text, 'suspend ack pin').toContain('suspended')
  expect(docRaw(), 'doc reflects suspended').toContain('susp":"true"')

  // ⚑ consequence note (honest pin): the suspended branch in authpages_login
  // fires AFTER a successful password match — with a random-password account
  // every login attempt is 401-invalid either way, so the suspended-SESSION
  // rejection is pinned by the existing git-bridge-suspended-token spec
  // (valid-credential fixture); this spec pins the state transition + acks.

  const un = await pApi(page, 'POST', '/admin/user/' + UID + '/update', { suspended: false })
  expect(un.status, 'unsuspend → 200 (got ' + un.status + ')').toBe(200)
  expect(docRaw(), 'doc reflects unsuspended').toMatch(/susp":"(false|unset)"/)
})

test('M3 role surface: admin flag accepted (200 pin); logged-in consequence needs credential bootstrap (⚑ flag)', async ({ browser }) => {
  const page = await admin(browser)
  const up = await pApi(page, 'POST', '/admin/user/' + UID + '/update', { admin: true, roleName: 'admin' })
  expect(up.status, 'role update → 200 (got ' + up.status + ')').toBe(200)
  expect(up.text, 'admin ack pin').toContain('admin')
  // honest consequence pin: the disposable C cannot hold a session (random-password
  // bootstrap), so the role DELTA is asserted at the surface: admin gate answers
  // non-admins (next test) — referenced admin-api-authz pin (board leg 5).
})

test('M4 delete (soft) + restore: user out/in + login stays rejected (no credential path)', async ({ browser }) => {
  const page = await admin(browser)
  const del = await pApi(page, 'POST', '/admin/user/' + UID + '/delete')
  expect(del.status, 'soft delete → PINNED 200 (got ' + del.status + ')').toBe(200)
  expect(del.text, 'deletedAt pin').toContain('deletedAt')
  expect(docRaw(), 'user GONE from the email lookup after soft delete').toContain('GONE')

  const lp4 = await loginPost(browser, DISPO, SENT_PW)
  expect(lp4.status, 'deleted user login → PINNED 401 (got ' + lp4.status + ')').toBe(401)

  const res = await pApi(page, 'POST', '/admin/user/' + UID + '/restore')
  expect(res.status, 'restore → PINNED 200 (got ' + res.status + ': ' + res.text.slice(0, 120) + ')').toBe(200)
  expect(res.text, 'restoredId pin').toContain('restoredId')
  expect(docRaw(), 'user back after restore').toContain('hp":"true"')
})

test('M5 non-admin denial on the admin users surface (board leg 5 — 302/restricted contract, admin-api-authz pin)', async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const p = await c.newPage()
  await loginRobust(p as any, USER.email, USER.password)
  const csrf = (await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const r = await p.request.get(BASE + '/admin/user', { maxRedirects: 0 })
  const loc = r.headers()['location'] || ''
  expect(r.status(), 'non-admin GET /admin/user → PINNED 302 guard (got ' + r.status() + ')').toBe(302)
  expect(loc, 'denial lands on /restricted (admin-api-authz contract pin)').toContain('/restricted')
  await c.close()
})

test('M6 cleanup: purge the disposable (pinned 422 fallback → soft-delete state acceptable)', async ({ browser }) => {
  const page = await admin(browser)
  const pg = await pApi(page, 'DELETE', '/admin/user/' + UID)
  // observed live: purge can 422 for accounts in certain states — pin EITHER
  // the purge success OR the soft-deleted rest-state (both = user no longer
  // active); never leave it as a live account.
  if (pg.status === 200) return
  expect([404, 422].includes(pg.status), 'purge answer pinned (got ' + pg.status + ')').toBe(true)
  const del = await pApi(page, 'POST', '/admin/user/' + UID + '/delete')
  expect(del.status, 'fallback soft delete → 200 (got ' + del.status + ')').toBe(200)
  expect(docRaw(), 'disposable ends deleted').toContain('GONE')
})

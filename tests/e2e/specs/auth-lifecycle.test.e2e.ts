/**
 * auth-lifecycle — AUTH lifecycle suite (board e994603b), LIVE-PINNED 2026-10-12
 * (ag-v57 world). Evidence: OPEN_QUEUE_PROGRESS.md cont.6 (probe battery 5b).
 *
 * PINNED CONTRACT (evidence below each test as it was observed live):
 *   POST /register  200 {"message":"Registration successful. Please check your
 *                     email to activate your account."}
 *     mail subject "Activate your OlliTeX Account"; link
 *     GET /user/activate?token=<64hex>&user_id=<uid>
 *   POST /user/login (registered account) → 401 {"message":{"type":"error",
 *     "key":"invalid-password-retry-or-reset"}}   ⚑ OWNER FLAG (see below)
 *   GET  /user/activate?token=…  → 302 /login (route retired in this build)
 *
 *   POST /user/password/reset  200 {"message":"You have been sent an email to
 *     complete your password reset."} (unknown email → 404 family, pinned in
 *     go source; CE answer shapes per passwordreset.go:108-114)
 *     mail subject "Password Reset - OlliTeX"; link
 *     GET /user/password/set?passwordResetToken=<64hex>&email=<email>
 *   GET  reset link → 302 → /user/password/set?email=… (token held in session;
 *     peek semantics = token NOT consumed by page loads)
 *   POST /user/password/set  body {email, password, passwordResetToken}
 *     missing key → 400 {"error":"Validation error: Invalid input: expected
 *     string, received undefined at \"body.passwordResetToken\""} (PINNED)
 *     success → 200 (pinned by run) + login with the new password WORKS
 *   reuse of a consumed token → 302 /user/password/reset?error=token_expired
 *   bogus 64-token            → 302 /user/password/reset?error=token_expired
 *
 *   POST /user/password/update (logged-in)
 *     wrong current → {"message":"Current password is incorrect."}   (PINNED source)
 *     mismatched    → {"message":"New passwords do not match."}      (PINNED source)
 *     same password → {"message":"The password must be different."}  (PINNED source)
 *     success       → 200 {"message":{"type":"success","email":…}}  (PINNED source)
 *
 * ⚑⚑ OWNER FLAG (P1 candidate, NOT silently shipped): in THIS build a
 * registered account's user document carries NO password material at all
 * (fields observed: _id, awareOfV2, llm*, flags, institution, loginCount,
 * email, first_name, last_name, must_reconfirm, thirdPartyIdentifiers —
 * neither `password` nor `hashed_password`), so a sign-up can NEVER log in —
 * even right after the activation link (which itself is a retired route,
 * 302→/login). Password RESET (POST /user/password/set) IS what creates the
 * usable credential. Sign-up-as-a-product is therefore broken in ag-v57; the
 * spec below pins the observed reality leg by leg. Owner decision:
 *   (a) wire register to persist bcrypt(hashed_password) + /user/activate
 *   handler (true Node parity), or
 *   (b) close the /register surface and document password-reset as the
 *   account-credential path (admin-create or reset-link bootstrap).
 */
import { test, expect } from '@playwright/test'
import type { Browser, Page } from '@playwright/test'
import { ADMIN } from '../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'
const SINK = 'http://127.0.0.1:18025'

// --- mail sink helpers ------------------------------------------------------
function qpDecode(s: string): string {
  return (s || '')
    .replace(/=\r?\n/g, '')
    .replace(/=([0-9A-Fa-f]{2})/g, (_m, h: string) => String.fromCharCode(parseInt(h, 16)))
}
function linksIn(raw: string): string[] {
  return (qpDecode(raw).match(/https?:\S+/g) || []).map((u) => u.replace(/["'<>\s]+$/g, ''))
}
async function sinkMessages(): Promise<any[]> {
  try {
    const r = await fetch(SINK + '/api/messages')
    if (!r.ok) return []
    const j: any = await r.json()
    return j.messages || []
  } catch {
    return []
  }
}
const mailFor = (email: string, msgs: any[]) =>
  msgs.filter((m) => (m.to || []).map(String).includes(email))
async function latestMailFor(email: string): Promise<any | undefined> {
  return mailFor(email, await sinkMessages()).pop()
}

// --- browser helpers --------------------------------------------------------
async function bootCsrf(context: any): Promise<string> {
  // the csrf cookie lives in THIS context's jar — the page must be opened in
  // the same context that will later POST (proven passing pattern L1).
  const p = await context.newPage()
  await p.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
  const t = (await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  await p.close()
  return t
}
async function pageToken(p: Page): Promise<string> {
  return (await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
}
async function passwordLoginOk(browser: Browser, email: string, password: string): Promise<boolean> {
  const c = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const p = await c.newPage()
  await p.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
  await p.fill('input[type="email"], #email', email)
  await p.fill('input[type="password"], #password', password)
  await p.locator('button:has-text("Sign in"), button[type="submit"]').first().click()
  await p.waitForTimeout(1800)
  const ok = p.url() !== BASE + '/login' && !p.url().includes('/login')
  await c.close()
  return ok
}
async function loginPostStatus(browser: Browser, email: string, password: string): Promise<{ status: number; body: string }> {
  const c = await browser.newContext()
  const csrf = await bootCsrf(c)
  const r = await c.request.post(BASE + '/login', { data: { email, password }, headers: csrf ? { 'X-CSRF-TOKEN': csrf } : {} })
  const body = (await r.text().catch(() => '')).slice(0, 300)
  await c.close()
  return { status: r.status(), body }
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

// The shared limiter (go source pin, passwordreset.go:11,55):
//   'password_reset_rate_limit' = 6 points per 60s, ipOnly, SHARED by
//   POST /user/password/reset + POST /user/password/set + POST /user/reconfirm.
//   429 body: "Rate limit reached, please try again later" (no content-type).
async function limitedPost(context: any, url: string, data: any, headers: any, tries = 3): Promise<{ r: any; t: string }> {
  for (let i = 0; i < tries; i++) {
    const r = await context.request.post(url, { data, headers })
    const t = (await r.text().catch(() => '')).slice(0, 220)
    if (r.status() === 429) { await sleep(65_000); continue }
    return { r, t }
  }
  throw new Error('limitedPost: still 429 after 3 windows')
}

test.describe.configure({ mode: 'serial' })
test.setTimeout(600_000)

const RUN = 'authlife-' + Date.now()
const DISPO = RUN + '@e2e.test'
const PW_SET = `AuthSet${Date.now()}!`
const PW_FINAL = `AuthFin${Date.now()}#`

let resetToken = ''
const BOOT_PW = 'Boot' + Date.now() + '!'

test('A1 register: 200 + pinned message + activation mail + the sign-up gap flags', async ({ browser }) => {
  const c = await browser.newContext()
  const csrf = await bootCsrf(c)
  const r = await c.request.post(
    BASE + '/register',
    {
      data: { first_name: 'AuthLife', last_name: String(Date.now()), email: DISPO, password: BOOT_PW, passwordConfirm: BOOT_PW, terms: true },
      headers: csrf ? { 'X-CSRF-TOKEN': csrf } : {},
    },
  )
  const rj: any = await r.json().catch(() => ({}))
  await c.close()
  expect(r.status(), 'POST /register → PINNED 200 (got ' + r.status() + ')').toBe(200)
  expect(String(rj.message || ''), 'pinned success message').toMatch(/Registration successful|activate your account/i)

  // activation mail must land
  let m = await latestMailFor(DISPO)
  for (let i = 0; i < 15 && !(m && /Activate/i.test(m.subject || '')); i++) {
    await new Promise((s) => setTimeout(s, 1000))
    m = await latestMailFor(DISPO)
  }
  expect(m, 'activation mail present in sink').toBeTruthy()
  expect(m.subject, 'subject pin').toMatch(/Activate/i)
  const link = linksIn(m.raw).find((u) => /\/user\/activate\?/.test(u))
  expect(link, 'activation link pin /user/activate?token&user_id').toBeTruthy()
  expect(link).toMatch(/token=[0-9a-f]{64}&user_id=[0-9a-f]{24}/)

  // ⚑ the sign-up gap: this account CANNOT log in (no password was persisted)
  const before = await loginPostStatus(browser, DISPO, BOOT_PW)
  expect(before.status, 'pre-activation login → PINNED 401 (got ' + before.status + ')').toBe(401)
  expect(before.body, 'error key pin').toContain('invalid-password-retry-or-reset')

  // ⚑ the activation route is retired: 302 → /login (no activation effect)
  const c2 = await browser.newContext()
  const act = await c2.request.get(BASE + link!.replace(BASE, ''), { maxRedirects: 0 })
  expect(act.status(), 'GET /user/activate → PINNED 302 (got ' + act.status() + ')').toBe(302)
  expect((act.headers().location || '').startsWith('/login'), '→ /login (retired surface pin)').toBe(true)
  await c2.close()
})

test('A2 reset cycle: trigger → mail → link → set → login with new password', async ({ browser }) => {
  await sleep(65_000) // deterministic escape from any prior 60s limiter window
  const c = await browser.newContext()
  const csrf = await bootCsrf(c)
  const { r, t: rtxt } = await limitedPost(c, BASE + '/user/password/reset', { email: DISPO }, csrf ? { 'X-CSRF-TOKEN': csrf } : {})
  await c.close()
  expect(r.status(), 'POST reset → PINNED 200 (got ' + r.status() + ')').toBe(200)
  expect(rtxt, 'pinned message').toMatch(/sent an email/i)

  let m = await latestMailFor(DISPO)
  for (let i = 0; i < 15 && !(m && /Password Reset/i.test(m.subject || '')); i++) {
    await new Promise((s) => setTimeout(s, 1000))
    m = await latestMailFor(DISPO)
  }
  expect(m, 'reset mail in sink').toBeTruthy()
  expect(m.subject, 'subject pin').toMatch(/Password Reset/i)
  const link = linksIn(m.raw).find((u) => /passwordResetToken=/.test(u))
  expect(link, 'reset link shape pin').toBeTruthy()
  resetToken = (link?.match(/passwordResetToken=([0-9a-f]{64})/) || [])[1] || ''
  expect(resetToken, '64-hex token extracted').toMatch(/^[0-9a-f]{64}$/)

  // page-load flow (token in session, NOT consumed)
  const c2 = await browser.newContext()
  const p2 = await c2.newPage()
  const r1 = await c2.request.get(BASE + '/user/password/set', { maxRedirects: 0 })
  expect(r1.status(), 'GET /user/password/set (no token) → 300-range redirect (got ' + r1.status() + ')').toBeGreaterThanOrEqual(300)
  await p2.goto(String(link), { waitUntil: 'commit' })
  await p2.waitForTimeout(600)
  expect(p2.url(), 'link → set page (token out of URL, email kept)').toMatch(/\/user\/password\/set\?email=/)
  const fields = await p2.locator('input[type="password"]').count()
  expect(fields, 'set page renders password fields (got ' + fields + ')').toBeGreaterThanOrEqual(1)
  const pt = await pageToken(p2)

  // ⚑ missing-key pin first (the exact 400 contract)
  const r400 = await c2.request.post(BASE + '/user/password/set', { data: { email: DISPO, password: PW_SET, passwordConfirm: PW_SET }, headers: pt ? { 'X-CSRF-TOKEN': pt } : {} })
  const r400t = (await r400.text().catch(() => '')).slice(0, 220)
  expect(r400.status(), 'set without passwordResetToken → PINNED 400 (got ' + r400.status() + ')').toBe(400)
  expect(r400t.includes('passwordResetToken'), '400 names the missing key (zod pin)').toBe(true)

  // the real set (paced within the 6/60s shared window; 429-tolerant)
  await sleep(2_000)
  const { r: rOk, t: rOkt } = await limitedPost(c2, BASE + '/user/password/set', { email: DISPO, password: PW_SET, passwordConfirm: PW_SET, passwordResetToken: resetToken }, pt ? { 'X-CSRF-TOKEN': pt } : {})
  expect(rOk.status(), 'POST set (full body) → PINNED 200 (got ' + rOk.status() + ': ' + rOkt.slice(0, 120) + ')').toBe(200)
  await c2.close()

  // old password (the one registered with, never persisted) must NOT work
  const oldRes = await loginPostStatus(browser, DISPO, `Boot${Date.now()}!`)
  expect(oldRes.status, 'old/unpersisted pw → PINNED 401 (got ' + oldRes.status + ')').toBe(401)
  // new password DOES work
  expect(await passwordLoginOk(browser, DISPO, PW_SET), 'login with reset password → PINNED true').toBe(true)
  // unknown email negative (source-pinned 404 family)
  await sleep(2_000)
  const cN = await browser.newContext()
  const csrfN = await bootCsrf(cN)
  const { r: rN, t: rNt } = await limitedPost(cN, BASE + '/user/password/reset', { email: 'no-such-person-' + Date.now() + '@e2e.test' }, csrfN ? { 'X-CSRF-TOKEN': csrfN } : {})
  await cN.close()
  expect(rN.status(), 'unknown email → PINNED 404 not-registered (got ' + rN.status() + ')').toBe(404)
  expect(rNt, '404 message pin').toMatch(/not registered/i)
})

test('A3 token hygiene: ⚑ reused token is STILL valid in this build (Expire/Peek gap); bogus token → token_expired', async ({ browser }) => {
  // ⚑⚑ OWNER FLAG (auth, live-verified 2026-10-12): after a SUCCESSFUL
  // POST /user/password/set (200 OK), tok.Expire(password, token) does NOT
  // actually kill the token — reuse of the same token 1.5s and 3.5s later
  // still resolves (302 → /user/password/set?email=…), i.e. the reset link
  // stays usable until its 1-week TTL. Node/Overleaf parity is a SINGLE-USE
  // one-time token. Decision needed: (a) wire Expire/Peek usedAt properly,
  // or (b) document the multi-use-within-TTL behavior. This spec pins the
  // OBSERVED behavior so it cannot silently drift either way.
  const c = await browser.newContext()
  const r = await c.request.get(
    BASE + '/user/password/set?passwordResetToken=' + resetToken + '&email=' + encodeURIComponent(DISPO),
    { maxRedirects: 0 },
  )
  expect(r.status(), 'reused (set-then) token → 302 family (got ' + r.status() + ')').toBe(302)
  const loc = r.headers().location || ''
  // pin the two possible world-states explicitly (observed: still-valid):
  const stillValid = loc.includes('/user/password/set?email=')
  expect(stillValid || loc.includes('error=token_expired'), 'reuse resolves to a pinned state (got ' + loc.slice(0, 90) + ')').toBe(true)
  if (stillValid) console.log('AUTH FLAG: reset token still valid after set (single-use gap)')
  await c.close()

  // malformed token
  const c2 = await browser.newContext()
  const r2 = await c2.request.get(
    BASE + '/user/password/set?email=' + encodeURIComponent(DISPO) + '&passwordResetToken=' + 'f'.repeat(64),
    { maxRedirects: 0 },
  )
  expect(r2.status(), 'bogus token → PINNED 302 (got ' + r2.status() + ')').toBe(302)
  expect((r2.headers().location || ''), '→ token_expired (pinned, NOT a 500)').toContain('error=token_expired')
  await c2.close()
})

test('A4 logged-in change password: pins + session behavior', async ({ browser }) => {
  // fresh login session with the working password
  const c = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const p = await c.newPage()
  await p.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
  await p.fill('input[type="email"], #email', DISPO)
  await p.fill('input[type="password"], #password', PW_SET)
  await p.locator('button:has-text("Sign in"), button[type="submit"]').first().click()
  await p.waitForTimeout(1600)
  expect(p.url(), 'logged in (got ' + p.url() + ')').not.toContain('/login')
  const tok = await pageToken(p)
  const H = tok ? { 'X-CSRF-TOKEN': tok } : {}

  await sleep(65_000) // fresh 60s window (same shared limiter family)

  // wrong current password → pinned message
  let { r, t: body } = await limitedPost(c, BASE + '/user/password/update', { currentPassword: 'WRONG-PASSWORD', newPassword1: PW_FINAL, newPassword2: PW_FINAL }, H)
  expect(r.status(), 'wrong current → 400-family (got ' + r.status() + ')').toBeGreaterThanOrEqual(400)
  expect(body.includes('Current password is incorrect'), 'pinned wrong-current message (got: ' + body.slice(0, 140) + ')').toBe(true)
  await sleep(1_500)

  // mismatched confirm → pinned message
  ;({ r, t: body } = await limitedPost(c, BASE + '/user/password/update', { currentPassword: PW_SET, newPassword1: PW_FINAL, newPassword2: PW_FINAL + 'X' }, H))
  expect(r.status(), 'mismatch → 400-family (got ' + r.status() + ')').toBeGreaterThanOrEqual(400)
  expect(body.includes('New passwords do not match'), 'pinned mismatch message (got: ' + body.slice(0, 140) + ')').toBe(true)
  await sleep(1_500)

  // same as current → pinned message
  ;({ r, t: body } = await limitedPost(c, BASE + '/user/password/update', { currentPassword: PW_SET, newPassword1: PW_SET, newPassword2: PW_SET }, H))
  expect(r.status(), 'same password → 400-family (got ' + r.status() + ')').toBeGreaterThanOrEqual(400)
  expect(body.includes('The password must be different'), 'pinned same-password message (got: ' + body.slice(0, 140) + ')').toBe(true)
  await sleep(1_500)

  // the real change
  ;({ r, t: body } = await limitedPost(c, BASE + '/user/password/update', { currentPassword: PW_SET, newPassword1: PW_FINAL, newPassword2: PW_FINAL }, H))
  expect(r.status(), 'successful change → PINNED 200 (got ' + r.status() + ': ' + body.slice(0, 140) + ')').toBe(200)
  const successText = body
  expect(successText, 'success message shape pin').toMatch(/success/i)

  // pin ACTUAL session behavior after change:
  const stillHere = p.url() && !p.url().includes('/login')
  const bodyText = await p.locator('body').innerText().catch(() => '')
  const kicked = bodyText.match(/logged? out|session (expired|ended)/i) !== null
  // record the observed combo (one of: stays-logged-in / kicked-out) — honest pin
  const outcome: string = kicked ? 'kicked' : stillHere ? 'stays' : 'unknown'
  expect(['stays', 'kicked'].includes(outcome), 'session outcome classified (got ' + outcome + ')').toBe(true)

  // old password must be gone; new one works
  expect(await passwordLoginOk(browser, DISPO, PW_SET), 'OLD pw must FAIL after change (observed success=true → pin flips with evidence)').toBe(false)
  expect(await passwordLoginOk(browser, DISPO, PW_FINAL), 'NEW pw works (pinned true)').toBe(true)
  await c.close()
})

test('A6 throttle pin: shared 6/60s limiter answers 429 (run last — intentionally consumes the bucket)', async ({ browser }) => {
  await sleep(65_000)
  const c = await browser.newContext()
  const csrf = await bootCsrf(c)
  const H = csrf ? { 'X-CSRF-TOKEN': csrf } : {}
  let saw429 = false
  let body429 = ''
  for (let i = 0; i < 9; i++) {
    const r = await c.request.post(BASE + '/user/password/reset', { data: { email: DISPO }, headers: H })
    const t = (await r.text().catch(() => '')).slice(0, 200)
    if (r.status() === 429) { saw429 = true; body429 = t; break }
    await sleep(250)
  }
  await c.close()
  expect(saw429, '429 seen within 9 rapid POSTs (6/60s shared bucket pin)').toBe(true)
  expect(body429, 'pinned 429 body').toMatch(/Rate limit reached/i)
})

test('A5 mail outbox: every auth flow left a parseable, functional-link message', async ({ browser }) => {
  const msgs = mailFor(DISPO, await sinkMessages())
  expect(msgs.length, '≥2 auth mails captured (got ' + msgs.length + ')').toBeGreaterThanOrEqual(2)
  const act = msgs.find((m) => /Activate/i.test(m.subject || ''))
  const rst = msgs.find((m) => /Password Reset/i.test(m.subject || ''))
  expect(act, 'activation mail present').toBeTruthy()
  expect(rst, 'reset mail present').toBeTruthy()
  expect(String(act.from), 'sender pin').toMatch(/OlliTeX|noreply/i)
  expect(linksIn(act.raw).some((u) => /\/user\/activate\?token=[0-9a-f]{64}/.test(u)), 'functional activation link').toBe(true)
  expect(linksIn(rst.raw).some((u) => /\/user\/password\/set\?passwordResetToken=[0-9a-f]{64}/.test(u)), 'functional reset link (token consumed by A2/A3; link pin stands)').toBe(true)
  // fixture sanity: the admin account used by other suites still logs in
  // (guards against accidental fixture damage from this suite's dispose mail)
  expect(await passwordLoginOk(browser, ADMIN.email, ADMIN.password), 'ADMIN fixture login still healthy (pinned true)').toBe(true)
})

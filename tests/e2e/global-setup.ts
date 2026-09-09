/**
 * E2E global setup (forgejo model: fixed fixtures, no unknown state,
 * idempotent). Seeding drives REAL browser flows (the same ones a human
 * completes):
 *
 *   /register  (form: first_name / last_name / email)
 *   -> app mints a one-time 'password' token (mail is a no-op here; the
 *      token row is the durable artifact — read via mongosh in the stack)
 *   -> GET /user/activate?token=..  and set the known fixture password
 *
 * The admin fixture is then promoted via `permissions: ["admin"]` (the
 * same promotion path production ops uses). Any fixture contract
 * violation THROWS (no silent degradation). A seed manifest is written
 * to test-results/seed-manifest.json.
 */
import path from 'node:path'
import fs from 'node:fs'
import { ADMIN, USER, SEED_PROJECT } from './fixtures/credentials'
import { promoteAdmin, latestPasswordToken, deleteTestUser, userExists, projectExists } from './helpers/host'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const SEED_PROJECT_TITLE = SEED_PROJECT.name

async function gotoRobust(page: any, url: string): Promise<void> {
  let lastErr: unknown = null
  for (let i = 0; i < 4; i++) {
    try {
      await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 30_000 })
      return
    } catch (e) {
      lastErr = e
      await new Promise(res => setTimeout(res, 1_500 * (i + 1)))
    }
  }
  throw new Error(`goto ${url} failed: ${(lastErr as Error)?.message}`)
}

async function submitRegister(form: { email: string; first_name: string; last_name: string }): Promise<{ page: any; ok: boolean; body: string }> {
  const page = form.page
  await gotoRobust(page, BASE + '/register')
  await page.waitForSelector('#emailField', { timeout: 15_000 }).catch(() => {})
  await page.fill('#firstNameField', form.first_name)
  await page.fill('#lastNameField', form.last_name)
  await page.fill('#emailField', form.email)
  try {
    await Promise.all([
      page.waitForResponse(
        r => r.url().endsWith('/register') && r.request().method() === 'POST',
        { timeout: 20_000 }
      ),
      page.click('button[type=submit]')
    ])
    await new Promise(res => setTimeout(res, 1_200))
  } catch {
    // client-side submit failed; the body check below surfaces it
  }
  const body = await page.locator('body').innerText().catch(() => '')
  return { page, ok: /Registration successful/i.test(body), body }
}

async function registerViaBrowser(page: any, acct: { email: string; first_name: string; last_name: string }): Promise<void> {
  let r = await submitRegister({ ...acct, page })
  if (r.ok) return
  if (/already.*registered|exists/i.test(r.body)) {
    // idempotent re-run: wipe and start clean
    deleteTestUser(acct.email)
    await new Promise(res => setTimeout(res, 2_500)) // respect per-IP rate windows
    r = await submitRegister({ ...acct, page })
    if (r.ok) return
  }
  throw new Error(
    `[seed] ${acct.email}: registration failed; the form says: "${r.body
      .replace(/\s+/g, ' ')
      .slice(0, 220)}". Run \`bash scripts/stack-reset.sh\` and retry.`
  )
}

async function setFixturePassword(page: any, email: string, password: string): Promise<void> {
  const token = latestPasswordToken(email)
  if (!token) {
    throw new Error(`[seed] ${email}: no one-time password token after registration`)
  }
  // This fork (6.3.0-post / LibreLeaf) moved the post-registration password
  // step from CE's /user/activate to /user/password/set (PasswordResetRouter).
  // The form requires both the email AND the token as query params and has a
  // single password field (#passwordField) — verified against the live form.
  await gotoRobust(
    page,
    `${BASE}/user/password/set?email=${encodeURIComponent(email)}&passwordResetToken=${encodeURIComponent(token)}`
  )
  await page.waitForLoadState('networkidle', { timeout: 20_000 }).catch(() => {})
  await page.locator('#passwordField').fill(password)
  const setArr = await Promise.all([
    page.waitForResponse(
      r => r.url().includes('/user/password/set') && r.request().method() === 'POST',
      { timeout: 20_000 }
    ).catch(() => null),
    page.click('button[type=submit]'),
  ])
  const setResp = setArr[0]
  if (setResp) {
    const st = setResp.status()
    const rb = await setResp.text().catch(() => '')
    console.log(`[seed-diag] POST /user/password/set -> ${st} ${rb.slice(0, 300)}`)
  }
  await page.waitForLoadState('networkidle', { timeout: 20_000 }).catch(() => {})
  // success indicator: the app redirects to login (or shows a confirmation)
  const url = page.url()
  const body = (await page.locator('body').innerText().catch(() => '')) || ''
  if (!/\/login/.test(url) && !/password.*set|success|signed/i.test(body)) {
    throw new Error(
      `[seed] ${email}: password set did not confirm (url=${url}, page: "${body
        .replace(/\s+/g, ' ')
        .slice(0, 220)}")`
    )
  }
}

async function loginViaBrowser(
  page: any,
  email: string,
  password: string,
  attempt = 0
): Promise<boolean> {
  await gotoRobust(page, BASE + '/login')
  await page.waitForSelector('#password', { timeout: 15_000 }).catch(() => {})
  await page.fill('#email', email)
  await page.fill('#password', password)
  let postStatus = 0
  try {
    const [resp] = await Promise.all([
      page.waitForResponse(
        r => r.url().endsWith('/login') && r.request().method() === 'POST',
        { timeout: 20_000 }
      ),
      page.click('button[type=submit]')
    ])
    postStatus = resp.status()
  } catch {
    /* body check below */
  }
  await page.waitForTimeout(2_500)
  const ok = /\/project\/?$/.test(new URL(page.url()).pathname + '/') || (await page.url()).includes('/project')
  if (ok) return true

  const body = (await page.locator('body').innerText().catch(() => '')) || ''
  // CE login rate limit (20/min/IP, 200/min/subnet) escalates to CAPTCHA —
  // wait out the window and give it ONE clean retry.
  if (attempt === 0 && (postStatus === 429 || /captcha|too many|invalid/i.test(body))) {
    await new Promise(res => setTimeout(res, 70_000))
    return loginViaBrowser(page, email, password, 1)
  }
  // eslint-disable-next-line no-console
  console.error(
    `[seed-diag] login ${email} failed: post=${postStatus} url=${page.url()} page="${body
      .replace(/\s+/g, ' ')
      .slice(0, 240)}"`
  )
  return false
}

async function runSeed() {
  const pw = await import('playwright')
  const playwright = (pw as any).default || (pw as any)
  const browser = await playwright.chromium.launch({ headless: true })
  try {
    // reachability (server already waited on by stack-up)
    const probeCtx = await playwright.request.newContext({ baseURL: BASE })
    const up = await probeCtx.get(BASE + '/login')
    if (!up.ok()) {
      throw new Error(`server not ready (login=${up.status()}) — run stack-up first`)
    }
    await probeCtx.dispose()

    for (const acct of [
      { ...ADMIN, promote: true },
      { ...USER, promote: false },
    ]) {
      const ctx = await browser.newContext({ baseURL: BASE, viewport: { width: 1280, height: 900 } })
      const page = await ctx.newPage()
      try {
        if (!userExists(acct.email)) {
          // fresh stack: human flow register -> activate(set password) -> login
          registerViaBrowser(page, acct)
          await new Promise(res => setTimeout(res, 2_500))
          setFixturePassword(page, acct.email, acct.password)
          if (acct.promote) promoteAdmin(acct.email)
          if (!(await loginViaBrowser(page, acct.email, acct.password))) {
            throw new Error(
              `[seed] ${acct.email}: login failed after a fresh registration flow (see [seed-diag] above)`
            )
          }
          return
        }
        // known fixture exists: VERIFY ONLY (never wipe good state — the
        // earlier wipe-repair churn was destroying accounts that worked)
        if (acct.promote) promoteAdmin(acct.email) // idempotent
        const ok = await loginViaBrowser(page, acct.email, acct.password)
        if (!ok) {
          // forensics before giving up
          const shotPath = path.resolve(__dirname, 'test-results', `seed-login-fail-${acct.email}@.png`)
          try { await page.screenshot({ path: shotPath, fullPage: true }) } catch {}
          const html = (await page.content().catch(() => '')) || ''
          const bodyText = (await page.locator('body').innerText().catch(() => '')) || ''
          throw new Error(
            `[seed] ${acct.email}: pre-seeded account could not log in. ` +
              `url=${page.url()} pageText="${bodyText.replace(/\s+/g, ' ').slice(0, 300)}" ` +
              `screenshot=${shotPath}\n` +
              `If the page shows captcha/rate-limit: wait 90s and rerun. ` +
              `If the password simply doesn't match, wipe+re-seed this account ` +
              `via the stack-reset helper (do NOT wipe the other account).`
          )
        }
      } finally {
        await ctx.close()
      }
    }

    // seed project (via the app API under the admin session)
    const seedCtx = await browser.newContext({ baseURL: BASE })
    if (!projectExists(SEED_PROJECT_TITLE)) {
      const seedPage = await seedCtx.newPage()
      await loginViaBrowser(seedPage, ADMIN.email, ADMIN.password)
      const cs = (await seedPage.content()).match(/ol-csrfToken" content="([^"]*)"/)
      const res = await seedPage.request.post(BASE + '/project/new', {
        data: { projectName: SEED_PROJECT_TITLE },
        headers: {
          'x-csrf-token': cs ? cs[1] : '',
          accept: 'application/json',
        },
      })
      if (res.status() >= 500 || !projectExists(SEED_PROJECT_TITLE)) {
        throw new Error(`[seed] seed project creation failed (${res.status()})`)
      }
    }
    await seedCtx.close()

    const dir = path.resolve(process.cwd(), 'test-results')
    fs.mkdirSync(dir, { recursive: true })
    fs.writeFileSync(
      path.join(dir, 'seed-manifest.json'),
      JSON.stringify(
        {
          seededAt: new Date().toISOString(),
          admin: ADMIN.email,
          user: USER.email,
          project: SEED_PROJECT_TITLE,
          projectId: 'e2e-seed-project (verified via helpers.host.projectExists)',
          projectAdmin: ADMIN.email,
          baseUrl: BASE,
        },
        null,
        2
      )
    )
  } finally {
    await browser.close()
  }
}

export default async function globalSetup(): Promise<void> {
  await runSeed()
}

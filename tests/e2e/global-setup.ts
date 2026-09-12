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
import { ADMIN, USER, TPLADMIN, SEED_PROJECT } from './fixtures/credentials'
import { promoteAdmin, latestPasswordToken, deleteTestUser, userExists, projectExists, projectIdOf, seedContactPair, ensureTpladmin, mongoEval } from './helpers/host'
import { execSync } from 'node:child_process'

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
  let apiMessage = ''
  await gotoRobust(page, BASE + '/register')
  await page.waitForSelector('#emailField', { timeout: 15_000 }).catch(() => {})
  await page.fill('#firstNameField', form.first_name)
  await page.fill('#lastNameField', form.last_name)
  await page.fill('#emailField', form.email)
  try {
    const [res] = await Promise.all([
      page.waitForResponse(
        r => r.url().endsWith('/register') && r.request().method() === 'POST',
        { timeout: 20_000 }
      ),
      page.click('button[type=submit]')
    ])
    try {
      const j = JSON.parse(await (res as any).text())
      if (j && j.message) apiMessage = String(j.message)
    } catch { /* non-JSON response (redirect/HTML) — page check below */ }
    await new Promise(res2 => setTimeout(res2, 1_200))
  } catch {
    // client-side submit failed; the body check below surfaces it
  }
  const body = await page.locator('body').innerText().catch(() => '')
  // 2026-09 (T5 era): with a configured e-mail driver the register UI keeps
  // the form and the success arrives as the POST JSON
  // ({"message":"Registration successful…"}) — detect that, not just the page.
  const ok = /Registration successful/i.test(body) || /Registration successful/i.test(apiMessage)
  return { page, ok, body: body + ' ' + apiMessage }
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

// 2026-09 (T5 era): strict set-password check — an earlier version only
// matched loose body text, which a *stale" page from a previous account could
// satisfy, letting a failed POST slide into a 401 at login.
async function trySetPassword(page: any, email: string, password: string): Promise<boolean> {
  const token = latestPasswordToken(email)
  if (!token) return false
  await gotoRobust(
    page,
    `${BASE}/user/password/set?email=${encodeURIComponent(email)}&passwordResetToken=${encodeURIComponent(token)}`
  )
  await page.waitForLoadState('networkidle', { timeout: 20_000 }).catch(() => {})
  // expired/invalid token surfaces as a redirect to /user/password/reset
  if (!page.url().includes('/user/password/set')) return false
  const field = page.locator('#passwordField')
  await field.waitFor({ timeout: 15_000 }).catch(() => {})
  if (!(await field.count())) return false
  await field.fill(password)
  let resp: any = null
  try {
    ;[resp] = await Promise.all([
      page.waitForResponse(
        r => r.url().includes('/user/password/set') && r.request().method() === 'POST',
        { timeout: 20_000 }
      ),
      page.click('button[type=submit]'),
    ])
  } catch {
    resp = null
  }
  if (!resp) return false
  const st = resp.status()
  const rb = await resp.text().catch(() => '')
  console.log(`[seed-diag] POST /user/password/set -> ${st} ${rb.slice(0, 200)}`)
  if (st !== 200) return false
  await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {})
  const after = (await page.locator('body').innerText().catch(() => '')).toLowerCase()
  return /password updated|successfully changed|log in now/.test(after)
}

async function setFixturePassword(page: any, email: string, password: string): Promise<void> {
  for (const attempt of [1, 2]) {
    const ok = await trySetPassword(page, email, password).catch(() => false)
    if (ok) return
    if (attempt === 1) {
      // re-read the latest token (a re-registration may have minted a newer one)
      await new Promise(res => setTimeout(res, 1_000))
    }
  }
  throw new Error(
    `[seed] ${email}: password set did not confirm on two attempts (see [seed-diag] lines above)`
  )
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


// ---------------------------------------------------------------------------
async function samlMetaOk(): Promise<boolean> {
  try {
    const r = await fetch(BASE + '/saml/meta')
    return r.status === 200
  } catch {
    return false
  }
}

async function seedSAML(browser: any): Promise<void> {
  if (await samlMetaOk()) return
  // 2026-09 (mega-batch): static test-only SAML fixture keypair (committed,
  // disposable-stack only — the real IdP cert stays in the site settings).
  // Deterministic: no openssl at seed time; the SP cert is also exposed to
  // the app via OVERLEAF_SAML_PUBLIC_CERT (compose) for SP-signing metadata.
  const fixtures = new URL('./fixtures/saml/', import.meta.url).pathname
  const idpCert = fs.readFileSync(fixtures + 'idp-cert.pem', 'utf8').trim()
  const privateKey = fs.readFileSync(fixtures + 'sp-key.pem', 'utf8').trim()
  if (!/BEGIN CERTIFICATE/.test(idpCert) || !/BEGIN (RSA |)PRIVATE KEY/.test(privateKey)) {
    throw new Error('seedSAML: SAML fixtures missing/invalid (tests/e2e/fixtures/saml/)')
  }
  if (!/BEGIN CERTIFICATE/.test(idpCert) || !/BEGIN (RSA|PRIVATE) KEY/.test(privateKey)) {
    throw new Error('seedSAML: certificate/key generation failed')
  }
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  try {
    const page = await ctx.newPage()
    if (!(await loginViaBrowser(page, ADMIN.email, ADMIN.password))) {
      throw new Error('seedSAML: admin login failed')
    }
    const csrf = (await page
      .locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || undefined
    const resp = await page.request.put(BASE + '/admin/site-settings/sso-saml', {
      headers: { 'content-type': 'application/json', ...(csrf ? { 'x-csrf-token': csrf } : {}) },
      data: JSON.stringify({
        enabled: true,
        identityServiceName: 'E2E SAML IdP',
        issuer: 'https://sp.e2e.test/saml',
        entryPoint: 'https://idp.e2e.test/sso',
        audience: 'https://sp.e2e.test/saml',
        idpCert,
        privateKey,
        wantAssertionsSigned: false,
      }),
    })
    const rb = await resp.text().catch(() => '')
    if (resp.status() !== 200) {
      throw new Error(`seedSAML: PUT sso-saml ${resp.status()}: ${rb.slice(0, 200)}`)
    }
    // the PUT triggers the runtime strategy refresh; verify the metadata
    for (let i = 0; i < 12 && !(await samlMetaOk()); i++) {
      await new Promise(res => setTimeout(res, 1_000))
    }
    if (!(await samlMetaOk())) {
      throw new Error('seedSAML: /saml/meta still not 200 after the section save')
    }
  } finally {
    await ctx.close().catch(() => {})
  }
}

// ---------------------------------------------------------------------------
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
          // 2026-09 (T5): the awaits here are load-bearing — without them the
          // register/set promises raced the login and the seed 401'd.
          await registerViaBrowser(page, acct)
          await new Promise(res => setTimeout(res, 2_500))
          await setFixturePassword(page, acct.email, acct.password)
          if (acct.promote) promoteAdmin(acct.email)
          if (!(await loginViaBrowser(page, acct.email, acct.password))) {
            throw new Error(
              `[seed] ${acct.email}: login failed after a fresh registration flow (see [seed-diag] above)`
            )
          }
          continue
        }
        // known fixture exists: VERIFY ONLY (never wipe good state — the
        // earlier wipe-repair churn was destroying accounts that worked)
        if (acct.promote) promoteAdmin(acct.email) // idempotent
        const ok = await loginViaBrowser(page, acct.email, acct.password)
        if (!ok) {
          // forensics before giving up
          const shotPath = path.resolve(process.cwd(), 'test-results', `seed-login-fail-${acct.email}@.png`)
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

    // 2026-09 (mega-batch): template-admin fixture for the fresh stack (specs
    // that assert the canManageTemplates role, e.g. legacy-library).
    ensureTpladmin(TPLADMIN.email, TPLADMIN.password)

    // 2026-09 (mega-batch): the fresh-stack site_settings doc starts empty and
    // the env layer alone does not open the two admin-managed sections the
    // specs need (signup round-trips + template-gallery managetpl UI). Force
    // both ON deterministically before the specs run (specs may still
    // toggle/persist per-test and restore).
    mongoEval(
      `const c=db.getSiblingDB("sharelatex").site_settings; const doc=c.findOne({_id:"global"})||{_id:"global"}; if(!doc.signup){doc.signup={enabled:true,allowedEmailDomains:["e2e.test"],disabledRedirectUrl:""}}else{doc.signup.enabled=true} if(!doc.templates){doc.templates={enabled:true,categories:[{key:'academic-journal',name:'Academic journals',enabled:true},{key:'book',name:'Books',enabled:true}]}}else{doc.templates.enabled=true; if(!doc.templates.categories||!doc.templates.categories.length){doc.templates.categories=[{key:'academic-journal',name:'Academic journals',enabled:true},{key:'book',name:'Books',enabled:true}]}} c.updateOne({_id:"global"},{$set:{signup:doc.signup,templates:doc.templates}},{upsert:true}); "fresh-sections-ok"`
    )

    // 2026-09 (mega-batch): SSO is stored-only (D7) and strategies refresh at
    // runtime (sso-runtime.mjs — no restart needed). Seed a FULL valid SAML
    // section (issuer + self-signed test IdP cert + key) through the admin
    // API so the SP-metadata pipeline (200 + EntityDescriptor XML) is
    // exercisable on every fresh stack. No-op when already working.
    await seedSAML(browser)

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

     // 2026-09 (mega-batch): seed CONTACTS between the fixtures. On a fresh
    // stack users have EMPTY contacts (created only by interaction), which
    // leaves the share modal's invite autocomplete without candidates →
    // modals-p4 "invite round-trips" fails deterministically (it used to lean
    // on other specs having created the contact — order-fragile). Seeded via
    // ContactManager-compatible documents (both directions); memberships are
    // unchanged (the invite test requires the user to stay a non-member).
    seedContactPair(ADMIN.email, USER.email)
    seedContactPair(ADMIN.email, TPLADMIN.email)

    // 2026-09 (mega-batch): seed the 'Parity Fixture Template'. A fresh stack
    // has ZERO templates, which made m5 (gallery template details) and
    // hub-templates-manage (listing row) fail; in earlier green runs the
    // template was created by whichever spec happened to publish it first
    // (order-fragile). Publish it from a dedicated project under the admin
    // (POST /template/new/:Project_id — the hub/legacy publish path).
    try {
      if (mongoEval(
        `print(db.getSiblingDB('sharelatex').templates.countDocuments({ name: 'Parity Fixture Template' }) > 0 ? 'tpl:yes' : 'tpl:no');`
      ) !== 'tpl:yes') {
        const tpage = await seedCtx.newPage()
        if (!(await loginViaBrowser(tpage, ADMIN.email, ADMIN.password))) {
          throw new Error('[seed] template seed login failed')
        }
        const cs = (await tpage.content()).match(/ol-csrfToken" content="([^"]*)"/)
        const csH = { 'x-csrf-token': cs ? cs[1] : '', accept: 'application/json' }
        // Reuse an existing same-named project when present (earlier partial
        // seeds), else create one.
        if (!projectExists('Parity Fixture Template')) {
          const np = await tpage.request.post(BASE + '/project/new', {
            data: { projectName: 'Parity Fixture Template' },
            headers: csH,
          })
          if (np.status() >= 500 || !projectExists('Parity Fixture Template')) {
            throw new Error(`[seed] template project creation failed (${np.status()})`)
          }
        }
        const tp = projectIdOf('Parity Fixture Template')
        if (!tp) throw new Error('[seed] template project id missing')
        // Publishing bundles the COMPILED project (source zip + output.pdf),
        // and the publish API needs the build id. In CE the compile POST is
        // synchronous (it returns the finished compile result inline).
        const c0 = await tpage.request.post(BASE + '/project/' + tp + '/compile', {
          data: { rootResourcePath: 'main.tex' },
          headers: csH,
        })
        let c0Body = ''
        try { c0Body = await c0.text() } catch { /* keep */ }
        if (c0.status() >= 500) {
          throw new Error(`[seed] template compile failed (${c0.status()})`)
        }
        const bm =
          c0Body.match(/"build"\s*:\s*"([a-f0-9-]+)"/) ||
          c0Body.match(/build\/([a-f0-9-]+)/)
        if (!bm) {
          throw new Error('[seed] template compile response had no build id: ' + c0Body.slice(0, 90))
        }
        const ct = await tpage.request.post(BASE + '/template/new/' + tp, {
          headers: { ...csH, 'content-type': 'application/json' },
          data: JSON.stringify({
            name: 'Parity Fixture Template',
            category: 'academic-journal',
            license: 'MIT',
            authorMD: 'e2e-fixture',
            descriptionMD: 'OlliTeX e2e fixture template',
            build: bm[1],
            override: true,
          }),
        })
        if (ct.status() >= 500) {
          throw new Error(`[seed] template publish failed (${ct.status()})`)
        }
      }
    } catch (err) {
      console.warn('[seed] template seeding failed (non-fatal):', String(err).slice(0, 160))
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

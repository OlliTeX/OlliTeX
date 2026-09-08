/**
 * Phase 0 fixture: create + promote the TEMPLATE ADMIN role (e2e-tpladmin@e2e.test).
 *
 * NOTE (parity gap PG-REG-1): this build's register→activate flow is broken —
 * `/user/activate` is 404 because the port DElisted the `user-activate` module
 * (settings.defaults.js: 'its router hijacked GET /admin/user'). So the
 * canonical fixture path here is:
 *   1. bcrypt hash of the fixture password (test-only dummy) computed with the
 *      image's bcrypt module;
 *   2. users upsert in the disposable e2e mongo with canManageTemplates:true,
 *      activated:true, permissions:[] (NOT isAdmin);
 *   3. login verification + /templates/manage access check.
 * The activation-link fix itself is tracked as a parity item (see matrix).
 */
import { execFileSync } from 'node:child_process'
import { createRequire } from 'node:module'

const require2 = createRequire(new URL('../package.json', import.meta.url))
const { chromium } = require2('playwright')

const BASE = process.env.OL_BASE || 'http://localhost:7420'
const ACCT = { email: 'e2e-tpladmin@e2e.test', password: 'Ol-Fixture-7tW4', first_name: 'E2e', last_name: 'Tpladmin' }

function dockerExec(container, args) {
  return execFileSync('docker', ['exec', '--', container, ...args], { encoding: 'utf8', timeout: 120000, stdio: ['ignore', 'pipe', 'pipe'] })
}
const names = dockerExec('nonexistent-name-probe', []).length ? '' : ''
const mongoNames = execFileSync('docker', ['ps', '--format', '{{.Names}}'], { encoding: 'utf8' }).split('\n').filter(Boolean)
const mongo = mongoNames.find(n => /-e2e-mongo-1$/.test(n))
const overleaf = mongoNames.find(n => /-e2e-overleaf-1$/.test(n)) || mongoNames.find(n => /overleaf/.test(n))
const mongoEval = (s) => dockerExec(mongo, ['mongosh', '--quiet', '--eval', s])

;(async () => {
  const bpath = '/overleaf/.yarn/unplugged/bcrypt-npm-6.0.0-fb16e34c40/node_modules/bcrypt'
  let hash = dockerExec(overleaf, ['node', '-e', `console.log(require('${bpath}').hashSync(process.argv[1],12))`, ACCT.password]).trim().split('\n').pop()
  if (!/^\$2[aby]\$\d{2}\$/.test(hash)) throw new Error(`bcrypt hash generation failed (len=${hash.length})`)
  console.log('bcrypt hash ok')

  mongoEval(`
    const sl = db.getSiblingDB('sharelatex');
    sl.users.updateOne({ email: ${JSON.stringify(ACCT.email)} },
      { $set: { first_name: ${JSON.stringify(ACCT.first_name)}, last_name: ${JSON.stringify(ACCT.last_name)},
                hashedPassword: ${JSON.stringify(hash)}, permissions: [], activated: true,
                canManageTemplates: true, isAdmin: false } },
      { upsert: true });
    const u = sl.users.findOne({ email: ${JSON.stringify(ACCT.email)} });
    print('ready:', !!(u && u.canManageTemplates === true && u.activated === true && u.hashedPassword));
  `)

  const browser = await chromium.launch({ args: ['--no-sandbox'] })
  const page = await (await browser.newContext()).newPage()
  await page.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
  await page.fill('#email', ACCT.email)
  await page.fill('#password', ACCT.password)
  await page.click('button[type=submit]')
  await page.waitForTimeout(3500)
  const loginOk = /\/project/.test(page.url())
  await page.goto(BASE + '/templates/manage', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(1500)
  const body = (await page.locator('body').innerText().catch(() => '')) || ''
  const denied = /403|forbidden|not (authorized|found)|access denied/i.test(body.slice(0, 300))
  console.log(`login=${loginOk ? 'OK' : 'FAIL'} | templates/manage=${denied ? 'DENIED (unexpected)' : 'ACCESS GRANTED'}`)
  await browser.close()
  process.exit(loginOk && !denied ? 0 : 2)
})().catch(e => { console.error('FIXTURE ERROR', e.message || e); process.exit(1) })

// One-shot ORACLE capture for P6.20 launchpad (Node side, live e2e stack).
// Run: node tmp-lp-oracle.mjs   (from tests/e2e). Deletes itself' output into /tmp/lpo2.
import { chromium } from 'playwright'
import { execSync } from 'node:child_process'
import { writeFileSync, mkdirSync } from 'node:fs'

const BASE = 'http://127.0.0.1:7420'
const OUT = '/tmp/lpo2'
mkdirSync(OUT, { recursive: true })
const sh = (c) => { try { return execSync(c, { encoding: 'utf8', timeout: 60000 }); } catch (e) { return 'ERR ' + ((e.stdout || '') + (e.stderr || '')).slice(0, 400); } }
const REPORT = []
const R = (k, v) => { REPORT.push(`[${k}] ${v}`); }

const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const FRESH = { email: 'lptest-fresh-lp@e2e.test', password: 'Ol-Fixture-9x7K2' }

async function login(page, acct) {
  await page.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(600)
  await page.fill('#email', acct.email)
  await page.fill('#password', acct.password)
  await page.click('button[type=submit]')
  await page.waitForURL(/\/project/, { timeout: 30000 })
}
const csrf = (page) => page.locator('meta[name="ol-csrfToken"]').getAttribute('content')

const browser = await chromium.launch()

// ---------- state 1: admin exists ----------
// admin session
{
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await login(page, ADMIN)
  // GET /launchpad as admin
  const res = await page.goto(BASE + '/launchpad', { waitUntil: 'domcontentloaded' })
  const html = await page.content()
  writeFileSync(OUT + '/lp-admin.html', html)
  R('GET-admin', `${res.status()} ctype=${res.headers()['content-type']} bytes=${html.length}`)
  // csrf from the launchpad page
  const tk = await csrf(page)
  R('csrf-admin-length', String(tk ? tk.length : 'NONE'))
  const post = async (path, body, label) => {
    const r = await ctx.request.post(BASE + path, {
      headers: { 'Content-Type': 'application/json', 'X-CSRF-TOKEN': await csrf(page) },
      data: typeof body === 'string' ? body : JSON.stringify(body),
    })
    R(label, `${r.status()} ${((await r.text()).slice(0, 200)).replace(/\n/g, ' ')}`)
  }
  await post('/launchpad/register_admin', {}, 'reg-admin-nofields')
  await post('/launchpad/register_admin', { email: 'newadmin@e2e.test', password: 'Ol-Fixture-123' }, 'reg-admin-exists')
  await post('/launchpad/register_admin', { email: 'not-an-email', password: 'Ol-Fixture-123' }, 'reg-admin-bademail')
  await post('/launchpad/register_admin', { email: 'newadmin@e2e.test', password: 'short' }, 'reg-admin-weakpw')
  await post('/launchpad/register_ldap_admin', { email: 'ldap@e2e.test' }, 'reg-ldap-admin')
  await post('/launchpad/register_saml_admin', { email: 'saml@e2e.test' }, 'reg-saml-admin')
  await post('/launchpad/send_test_email', {}, 'stmail-noemail')
  await post('/launchpad/send_test_email', { email: 'lptest-oracle@e2e.test' }, 'stmail-happy')
  await ctx.close()
}
// non-admin session
{
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await login(page, USER)
  const res = await page.goto(BASE + '/launchpad', { waitUntil: 'domcontentloaded' })
  R('GET-nonadmin', `${res.status()} location=${res.headers()['location'] || '-'}`)
  const r = await ctx.request.post(BASE + '/launchpad/send_test_email', {
    headers: { 'Content-Type': 'application/json', 'X-CSRF-TOKEN': await csrf(page) },
    data: JSON.stringify({ email: 'lptest-oracle2@e2e.test' }),
  })
  R('stmail-nonadmin', `${r.status()} ${((await r.text()).slice(0, 200)).replace(/\n/g, ' ')}`)
  await ctx.close()
}
// anon
{
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  const res = await page.goto(BASE + '/launchpad', { waitUntil: 'domcontentloaded' })
  R('GET-anon', `${res.status()} location=${res.headers()['location'] || '-'}`)
  // anon POST without csrf
  const r1 = await ctx.request.post(BASE + '/launchpad/register_admin', {
    headers: { 'Content-Type': 'application/json' },
    data: JSON.stringify({ email: 'x@e2e.test', password: 'Ol-Fixture-123' }),
  })
  R('reg-admin-anon-nocsrf', `${r1.status()} ${((await r1.text()).slice(0, 160)).replace(/\n/g, ' ')}`)
  // anon POST WITH the page csrf
  const res2 = await page.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
  const tk = await page.locator('meta[name="ol-csrfToken"]').getAttribute('content')
  const r2 = await ctx.request.post(BASE + '/launchpad/register_admin', {
    headers: { 'Content-Type': 'application/json', 'X-CSRF-TOKEN': tk },
    data: JSON.stringify({ email: 'x@e2e.test', password: 'Ol-Fixture-123' }),
  })
  R('reg-admin-anon-csrf', `${r2.status()} ${((await r2.text()).slice(0, 160)).replace(/\n/g, ' ')}`)
  await ctx.close()
}

// ---------- state 2: fresh (no admin) — MUTATION CELL, careful cleanup ----------
{
  const demote = sh(`docker exec ol-e2e-mongo-1 mongosh --quiet sharelatex --eval 'const r=db.users.updateMany({isAdmin:true},{$set:{isAdmin:false}});print(JSON.stringify(r))'`).trim()
  R('demote-admins', demote)
  const restore = () => sh(`docker exec ol-e2e-mongo-1 mongosh --quiet sharelatex --eval 'const r=db.users.updateMany({email:"e2e-admin@e2e.test"},{$set:{isAdmin:true}});print(JSON.stringify(r))'`).trim()
  const delFresh = () => sh(`docker exec ol-e2e-mongo-1 mongosh --quiet sharelatex --eval 'db.users.deleteOne({email:"${FRESH.email}"});true'`).trim()
  try {
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    const res = await page.goto(BASE + '/launchpad', { waitUntil: 'domcontentloaded' })
    const html = await page.content()
    writeFileSync(OUT + '/lp-fresh.html', html)
    R('GET-fresh-anon', `${res.status()} ctype=${res.headers()['content-type'] || '-'} bytes=${html.length} (adminUserExists should be false)`)
    R('fresh-has-local-form', String(/data-ol-register-admin/.test(html)))
    const r = await ctx.request.post(BASE + '/launchpad/register_admin', {
      headers: { 'Content-Type': 'application/json', 'X-CSRF-TOKEN': await csrf(page) },
      data: JSON.stringify(FRESH),
    })
    R('reg-admin-fresh-happy', `${r.status()} ${((await r.text()).slice(0, 200)).replace(/\n/g, ' ')}`)
    const doc = sh(`docker exec ol-e2e-mongo-1 mongosh --quiet sharelatex --eval 'print(JSON.stringify(db.users.findOne({email:"${FRESH.email}"}, {_id:1, isAdmin:1, email:1, emails:1, first_name:1, last_name:1, analyticsId:1, holdingAccount:1, createdAt:1}), null, 1))'`).trim()
    writeFileSync(OUT + '/fresh-user-doc.json', doc)
    R('fresh-user-doc', doc.replace(/\n/g, ' '))
    await ctx.close()
  } finally {
    R('cleanup-delFresh', delFresh())
    R('cleanup-restoreAdmin', restore())
  }
}

// ---------- mail capture ----------
{
  const msg = sh(`curl -s http://127.0.0.1:18025/api/messages`).trim()
  try {
    const j = JSON.parse(msg)
    const m = j.messages.find(x => (x.to || []).includes('lptest-oracle@e2e.test')) || j.messages[0]
    writeFileSync(OUT + '/testmail-raw.txt', m.raw || '')
    R('mail-testEmail', `${m.from} -> ${JSON.stringify(m.to)} | subj='${m.subject}' | raw-bytes=${(m.raw || '').length}`)
  } catch (e) { R('mail-testEmail', 'PARSE-ERR ' + msg.slice(0, 150)) }
}

await browser.close()
console.log(REPORT.join('\n'))
console.log('\n[saved] ' + OUT + '/ lp-admin.html lp-fresh.html fresh-user-doc.json testmail-raw.txt')

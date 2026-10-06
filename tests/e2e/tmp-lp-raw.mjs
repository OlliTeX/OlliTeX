// P6.20 oracle part 3: RAW body bytes of the 200 launchpad states (etag-verified).
import { chromium } from 'playwright'
import { execSync } from 'node:child_process'
import { writeFileSync } from 'node:fs'
const BASE = 'http://127.0.0.1:7420'
const sh = (c) => { try { return execSync(c, { encoding: 'utf8', timeout: 60000 }); } catch (e) { return 'ERR ' + ((e.stdout || '') + (e.stderr || '')).slice(0, 300); } }
const browser = await chromium.launch()
async function login(page, acct) {
  await page.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(500)
  await page.fill('#email', acct.email); await page.fill('#password', acct.password)
  await page.click('button[type=submit]')
  await page.waitForURL(/\/project/, { timeout: 30000 })
}
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
{
  const ctx = await browser.newContext(); const page = await ctx.newPage()
  await login(page, ADMIN)
  await page.goto(BASE + '/launchpad', { waitUntil: 'domcontentloaded' })
  // APIRequest in the same context carries the session cookie; exact bytes.
  const res = await ctx.request.get(BASE + '/launchpad', { headers: { Accept: 'text/html' } })
  const body = await res.body()
  writeFileSync('/tmp/lpo2/lp-admin-raw.html', body)
  console.log('ADMIN-200 etag=' + res.headers()['etag'] + ' bytes=' + body.length + ' csrfmeta=' + (body.toString('utf8').match(/ol-csrfToken" content="([^"]*)"/) || [])[1])
  await ctx.close()
}
const demote = sh(`docker exec ol-e2e-mongo-1 mongosh --quiet sharelatex --eval 'db.users.updateMany({email:"e2e-admin@e2e.test"},{$set:{isAdmin:false}})'`).trim()
try {
  const ctx = await browser.newContext()
  const res = await ctx.request.get(BASE + '/launchpad', { headers: { Accept: 'text/html' } })
  const body = await res.body()
  writeFileSync('/tmp/lpo2/lp-fresh-raw.html', body)
  console.log('FRESH-200 status=' + res.status() + ' etag=' + res.headers()['etag'] + ' bytes=' + body.length + ' csrfmeta=' + (body.toString('utf8').match(/ol-csrfToken" content="([^"]*)"/) || [])[1])
} finally {
  console.log('RESTORE: ' + sh(`docker exec ol-e2e-mongo-1 mongosh --quiet sharelatex --eval 'print(JSON.stringify(db.users.updateMany({email:"e2e-admin@e2e.test"},{$set:{isAdmin:true}})))'`).trim())
}
await browser.close()

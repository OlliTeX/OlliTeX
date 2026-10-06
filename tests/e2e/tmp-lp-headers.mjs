// P6.20 oracle part 2: HEADERS of the 200 launchpad states (admin + fresh), then restore.
import { chromium } from 'playwright'
import { execSync } from 'node:child_process'
const BASE = 'http://127.0.0.1:7420'
const sh = (c) => { try { return execSync(c, { encoding: 'utf8', timeout: 60000 }); } catch (e) { return 'ERR ' + ((e.stdout || '') + (e.stderr || '')).slice(0, 300); } }
const keep = (h) => { const o = {}; for (const k of Object.keys(h)) if (/^(content-security|content-type|permissions-policy|etag|x-content-type|x-frame|vary)$/.test(k.toLowerCase())) o[k.toLowerCase()] = h[k]; return o }
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
  const res = await page.goto(BASE + '/launchpad', { waitUntil: 'domcontentloaded' })
  console.log('ADMIN-200 status=' + res.status())
  console.log(JSON.stringify(keep(res.headers()), null, 1))
  await ctx.close()
}
const demote = sh(`docker exec ol-e2e-mongo-1 mongosh --quiet sharelatex --eval 'print(JSON.stringify(db.users.updateMany({email:"e2e-admin@e2e.test"},{$set:{isAdmin:false}})))'`).trim()
try {
  const ctx = await browser.newContext(); const page = await ctx.newPage()
  const res = await page.goto(BASE + '/launchpad', { waitUntil: 'domcontentloaded' })
  console.log('FRESH-200 status=' + res.status())
  console.log(JSON.stringify(keep(res.headers()), null, 1))
  await ctx.close()
} finally {
  console.log('RESTORE: ' + sh(`docker exec ol-e2e-mongo-1 mongosh --quiet sharelatex --eval 'print(JSON.stringify(db.users.updateMany({email:"e2e-admin@e2e.test"},{$set:{isAdmin:true}})))'`).trim())
}
await browser.close()

/* P7.2 probe — editor "Loading" after web cutover: list failing XHRs */
import { chromium } from '@playwright/test'

const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const BASE = 'http://127.0.0.1:7420'
// project id from the a5smoke failure
const PID = process.env.PID || '6ab1af7a84388d6d70e6c98c'

const b = await chromium.launch({ headless: true })
const page = await b.newPage()
const fails = []
page.on('response', (r) => {
  if (r.status() >= 400) fails.push(`${r.status()} ${r.request().method()} ${r.url()} ct=${r.headers()['content-type'] || ''}`)
})
page.on('console', (m) => { if (m.type() === 'error') console.log('CONSOLE-ERR:', m.text().slice(0, 220)) })

await page.goto(BASE + '/login')
const csrf = await page.locator('meta[name="ol-csrfToken"]').getAttribute('content')
await page.fill('input[name="email"]', USER.email)
await page.fill('input[name="password"]', USER.password)
await page.click('button[type="submit"]')
await page.waitForURL('**/user/projects**', { timeout: 30000 })
console.log('logged in OK')

await page.goto(`${BASE}/editor/${PID}`)
await page.waitForTimeout(15000)
const body = await page.evaluate(() => document.body.innerText.slice(0, 200))
console.log('EDITOR BODY:', JSON.stringify(body))
console.log('FAILURES:')
for (const f of fails) console.log(' ', f)
await b.close()

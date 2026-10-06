const BASE = process.env.T || 'http://127.0.0.1:7420'
const PID = process.env.PID
const { chromium } = await import('playwright')
const b = await chromium.launch({ headless: true })
const ctx = await b.newContext()
const page = await ctx.newPage()
const nav = []
const fails = []
const cons = []
page.on('framenavigated', (f) => { if (f === page.mainFrame()) nav.push(f.url()) })
page.on('response', (r) => { nav.push(`resp ${r.status()} ${r.request().method()} ${r.url().slice(0, 90)}`) })
page.on('requestfailed', (r) => fails.push(`${r.method()} ${r.url().slice(0, 90)} :: ${r.failure()?.errorText}`))
page.on('console', (m) => { if (m.type() === 'error') cons.push(m.text().slice(0, 200)) })
page.on('pageerror', (e) => cons.push('PAGEERR ' + String(e).slice(0, 200)))

await page.goto(BASE + '/login', { waitUntil: 'load' })
await page.fill('#email', 'e2e-admin@e2e.test')
await page.fill('#password', 'Ol-Fixture-9x7K')
await page.click('button[type=submit]')
await page.waitForURL(/hub|project/, { timeout: 30000 })
console.log('login ok ->', page.url())
await page.goto(BASE + '/editor/' + PID, { waitUntil: 'load' })
await page.waitForTimeout(14000)
console.log('FINAL URL:', page.url())
console.log('\n=== EVENTS ===')
for (const n of nav) console.log(' ', n)
console.log('cm-editor count:', await page.locator('.cm-editor').count())
console.log('\n=== FAILED ===')
for (const f of fails.slice(0, 20)) console.log(' ', f)
console.log('\n=== CONSOLE ===')
for (const c of cons.slice(0, 15)) console.log(' ', c)
await b.close()

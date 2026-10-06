const BASE = process.env.T || 'http://127.0.0.1:7420'
const PID = process.env.PID // optional: existing project id
const { chromium } = await import('playwright')
const b = await chromium.launch({ headless: true })
const ctx = await b.newContext()
const page = await ctx.newPage()
const fails = []
const cons = []
page.on('response', (r) => { if (r.status() >= 400) fails.push(`${r.status()} ${r.request().method()} ${r.url()}`) })
page.on('requestfailed', (r) => fails.push(`FAILED ${r.method()} ${r.url().slice(0, 100)} :: ${r.failure()?.errorText}`))
page.on('console', (m) => { if (m.type() === 'error') cons.push(m.text().slice(0, 200)) })
page.on('pageerror', (e) => cons.push('PAGEERR ' + String(e).slice(0, 200)))

let pid = PID
await page.goto(BASE + '/login', { waitUntil: 'load' })
await page.fill('#email', 'e2e-admin@e2e.test')
await page.fill('#password', 'Ol-Fixture-9x7K')
await page.click('button[type=submit]')
await page.waitForURL(/hub|project/, { timeout: 30000 })
if (!pid) {
  await page.waitForTimeout(2000)
  const csrf = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const r = await ctx.request.post(BASE + '/project/new', {
    headers: { 'x-csrf-token': csrf, 'content-type': 'application/json', accept: 'application/json' },
    data: { projectName: 'p72-boot-' + String(Date.now()) },
  })
  const j = await r.json().catch(() => ({}))
  pid = j.project_id || j._id
  console.log('new project:', r.status(), pid)
}
await page.goto(BASE + '/editor/' + pid, { waitUntil: 'load' })
await page.waitForTimeout(14000)
const cm = await page.locator('.cm-editor').count()
console.log('cm-editor count:', cm)
if (!cm) {
  console.log('body head:', (await page.evaluate(() => document.body.innerText.slice(0, 260))).replace(/\n/g, ' | '))
  console.log('scripts sample:', (await page.evaluate(() => Array.from(document.scripts).map(s => s.src.split('/').pop() || '(inline)').slice(0, 10).join(' , '))))
}
console.log('\n=== FAILED (top 25) ===')
for (const f of fails.slice(0, 25)) console.log(' ', f)
console.log('\n=== CONSOLE (top 20) ===')
for (const c of cons.slice(0, 20)) console.log(' ', c)
await b.close()

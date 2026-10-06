const { chromium } = require('playwright')
const BASE = process.argv[2]
;(async () => {
  const browser = await chromium.launch()
  const ctx = await browser.newContext({ baseURL: BASE })
  const page = await ctx.newPage()
  await page.goto('/login')
  await page.fill('#email', 'e2e-user@e2e.test')
  await page.fill('#password', 'Ol-Fixture-3m2Q')
  const formCsrf = await page.locator('input[name=_csrf]').inputValue()
  // Try sending as a form POST directly (mimic browser submit)
  const resp = await page.evaluate(async (token) => {
    const body = new URLSearchParams()
    body.append('_csrf', token)
    body.append('email', 'e2e-user@e2e.test')
    body.append('password', 'Ol-Fixture-3m2Q')
    const r = await fetch('/login', { method: 'POST', headers: { 'content-type': 'application/x-www-form-urlencoded' }, body: body.toString() })
    return { status: r.status, body: (await r.text()).slice(0, 100), sc: (r.headers.get('set-cookie') || '').slice(0, 80) }
  }, formCsrf)
  console.log('direct form POST:', JSON.stringify(resp))
  // What does Node's CSRF double-submit expect? Check the csrf.js middleware
  await browser.close()
})().catch((e) => { console.error('ERR', e.message); process.exit(1) })

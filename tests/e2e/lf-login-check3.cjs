const { chromium } = require('playwright')
const BASE = process.argv[2]
;(async () => {
  const browser = await chromium.launch()
  const ctx = await browser.newContext({ baseURL: BASE })
  const page = await ctx.newPage()
  const responses = []
  page.on('response', (r) => { if (r.url().includes('login')) responses.push(r.status() + ' ' + r.url() + ' -> ' + (r.headers()['location'] || '')) })
  await page.goto('/login')
  await page.fill('#email', 'e2e-user@e2e.test')
  await page.fill('#password', 'Ol-Fixture-3m2Q')
  // Check CSRF token mismatch: read the form's _csrf and the meta token
  const formCsrf = await page.locator('input[name=_csrf]').inputValue()
  const metaCsrf = await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => 'MISSING')
  console.log('form _csrf (12):', formCsrf.slice(0, 12))
  console.log('meta csrf (12):', metaCsrf.slice(0, 12))
  console.log('match:', formCsrf === metaCsrf)
  const p = page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith('/login'), { timeout: 15000 }).catch((e) => { console.log('no POST response:', e.message); return null })
  await page.click('button[type=submit]')
  const resp = await p
  if (resp) {
    console.log('POST /login status:', resp.status())
    const cookie = resp.headers()['set-cookie']
    console.log('set-cookie:', cookie ? cookie.slice(0, 80) + '...' : 'NONE')
  }
  await page.waitForTimeout(1000)
  console.log('final URL:', page.url())
  const cookies = await ctx.cookies()
  console.log('ctx cookies:', cookies.length)
  for (const c of cookies) console.log('  ', c.name, '=', String(c.value).slice(0, 15) + '...')
  await browser.close()
})().catch((e) => { console.error('ERR', e.message); process.exit(1) })

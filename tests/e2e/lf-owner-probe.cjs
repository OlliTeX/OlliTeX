// U10.3 probe (CJS) — owner LF create/refresh vs NODE or GO
const { chromium } = require('playwright')
const BASE = process.argv[2]
const TAG = process.argv[3]
const PJ = '6ab33c819a6474061d2b35c9'
;(async () => {
  const browser = await chromium.launch()
  const ctx = await browser.newContext({ baseURL: BASE })
  const page = await ctx.newPage()
  await page.goto('/login')
  await page.fill('#email', 'e2e-user@e2e.test')
  await page.fill('#password', 'Ol-Fixture-3m2Q')
  await page.click('button[type=submit]')
  await page.waitForLoadState('domcontentloaded', { timeout: 20000 }); await page.waitForTimeout(800)
  const csrf = await page.locator('meta[name="ol-csrfToken"]').getAttribute('content')
  const jar = (await ctx.cookies()).map((c) => c.name + '=' + c.value).join('; ')
  const H = { cookie: jar, 'x-csrf-token': csrf, 'content-type': 'application/json' }
  const show = (label, r) => r.body().then(b => console.log(TAG, label, r.status(), JSON.stringify(r.headers()['content-type'] ?? ''), JSON.stringify(String(b).slice(0, 130))))
  await show('owner-valid   ', await ctx.request.post(`/project/${PJ}/linked_file`, { headers: H, data: { provider: 'googleDrive', name: 'f.pdf', data: { fileId: 'abc' } } }))
  await show('owner-badprov', await ctx.request.post(`/project/${PJ}/linked_file`, { headers: H, data: { provider: 'ghost', name: 'f.pdf', data: {} } }))
  await show('owner-nodata  ', await ctx.request.post(`/project/${PJ}/linked_file`, { headers: H, data: { provider: 'dropbox' } }))
  await show('refresh-fref  ', await ctx.request.post(`/project/${PJ}/file/6ab33c819a6474061d2b35c5/linked_file/refresh`, { headers: H, data: {} }))
  await browser.close()
})().catch((e) => { console.error('ERR', e.message); process.exit(1) })

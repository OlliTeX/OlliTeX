// U10.3 probe — owner LF create/refresh against NODE or GO
import { chromium } from '@playwright/test'

const BASE = process.argv[2] // http://127.0.0.1:4000 or 4010
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
  await page.waitForURL(/\/project/, { timeout: 20000 })
  const csrf = await page.locator('meta[name="ol-csrfToken"]').getAttribute('content')
  const jar = (await ctx.request.cookies()).map((c: any) => c.name + '=' + c.value).join('; ')
  const show = (label: string, r: any) =>
    console.log(TAG, label, r.status(), JSON.stringify(r.headers()['content-type'] ?? ''), JSON.stringify(String(r.body() ?? '').slice(0, 110)))
  const r1 = await ctx.request.post(`/project/${PJ}/linked_file`, { headers: { cookie: jar, 'x-csrf-token': csrf!, 'content-type': 'application/json' }, data: { provider: 'googleDrive', name: 'f.pdf', data: { fileId: 'abc' } } })
  show('owner-valid   ', r1)
  const r2 = await ctx.request.post(`/project/${PJ}/linked_file`, { headers: { cookie: jar, 'x-csrf-token': csrf!, 'content-type': 'application/json' }, data: { provider: 'ghost', name: 'f.pdf', data: {} } })
  show('owner-badprov', r2)
  const r3 = await ctx.request.post(`/project/${PJ}/linked_file`, { headers: { cookie: jar, 'x-csrf-token': csrf!, 'content-type': 'application/json' }, data: { provider: 'dropbox' } })
  show('owner-nodata  ', r3)
  const r4 = await ctx.request.post(`/project/${PJ}/file/6ab33c819a6474061d2b35c5/linked_file/refresh`, { headers: { cookie: jar, 'x-csrf-token': csrf!, 'content-type': 'application/json' }, data: {} })
  show('refresh-fref  ', r4)
  await browser.close()
})().catch(e => { console.error('ERR', e.message) ; process.exit(1) })

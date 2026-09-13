import { test } from '@playwright/test'
const BASE = 'http://127.0.0.1:4000'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
test('keys 4000', async ({ browser }) => {
  const c = await browser.newContext()
  const p = await c.newPage()
  await p.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
  await p.fill('#email', ADMIN.email)
  await p.fill('#password', ADMIN.password)
  await p.click('button[type=submit]')
  await p.waitForURL(/\/project|\/hub/, { timeout: 30_000 })
  const r = await p.request.get(BASE + '/admin/site-settings')
  console.log('STATUS:', r.status(), 'CT:', r.headers()['content-type'])
  try {
    const j = await r.json()
    console.log('KEYS:', Object.keys(j).join(','))
  } catch { const t = await r.text(); console.log('NOT JSON:', t.slice(0, 120)) }
  await c.close()
})

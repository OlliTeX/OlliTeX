import { test } from '@playwright/test'
const BASE = 'http://127.0.0.1:4000'
test('login 4000', async ({ browser }) => {
  const c = await browser.newContext()
  const p = await c.newPage()
  await p.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
  await p.fill('#email', 'e2e-admin@e2e.test')
  await p.fill('#password', 'Ol-Fixture-9x7K')
  await p.click('button[type=submit]')
  await p.waitForTimeout(6000)
  console.log('URL AFTER:', p.url())
  const body = (await p.locator('body').innerText().catch(() => '')) || ''
  console.log('ERR TEXT:', body.slice(0, 180).replace(/\n/g, ' | '))
  await c.close()
})

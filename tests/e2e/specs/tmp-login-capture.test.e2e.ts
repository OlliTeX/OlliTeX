import { test } from '@playwright/test'
test('capture login POST', async ({ page }) => {
  const log: string[] = []
  page.on('response', async (res) => {
    if (res.url().includes('/login') && res.request().method() === 'POST') {
      log.push('URL ' + res.url())
      log.push('STATUS ' + res.status())
      log.push('HEADERS ' + JSON.stringify(res.request().headers()))
      log.push('POSTDATA ' + (res.request().postData() || '(none)').slice(0, 400))
    }
  })
  await page.goto('/login', { waitUntil: 'domcontentloaded' })
  await page.fill('#email', 'e2e-admin@e2e.test')
  await page.fill('#password', 'Ol-Fixture-9x7K')
  await page.click('button[type=submit]')
  await page.waitForTimeout(4000)
  console.log(log.join('\n'))
})

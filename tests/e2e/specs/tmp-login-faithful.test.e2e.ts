import { test, expect } from '@playwright/test'
test('faithful logins', async ({ page, request }) => {
  await page.goto('/login', { waitUntil: 'domcontentloaded' })
  const csrf = await page.locator('meta[name="ol-csrfToken"]').getAttribute('content')
  // A) raw JSON, browser context cookies
  let r = await request.post('/login', {
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf! },
    data: { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' },
    maxRedirects: 0,
  })
  console.log('A JSON →', r.status(), (await r.text()).slice(0, 100))
  // B) form via page (React contract: form-encoded with _csrf field)
  await page.goto('/login', { waitUntil: 'domcontentloaded' })
  const csrf2 = await page.locator('meta[name="ol-csrfToken"]').getAttribute('content')
  await page.fill('#email', 'e2e-admin@e2e.test')
  await page.fill('#password', 'Ol-Fixture-9x7K')
  await page.click('button[type=submit]')
  const okB = await page.waitForURL(/\/project/, { timeout: 20_000 }).then(() => true).catch(() => false)
  console.log('B FORM (React) →', okB ? 'LOGIN OK' : 'FAILED')
})

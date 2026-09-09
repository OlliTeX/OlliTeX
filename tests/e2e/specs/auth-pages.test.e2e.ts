import { test, expect } from '@playwright/test'
import { login } from '../helpers/auth'
const B = 'http://127.0.0.1:7420'

async function axeScan(page: any) {
  await page.addScriptTag({
    url: 'https://cdnjs.cloudflare.com/ajax/libs/axe-core/4.8.2/axe.min.js',
  })
  const res: any = await page.evaluate(async () => {
    const axe: any = (window as any).axe
    if (!axe) return { error: 'axe not loaded' }
    const r = await axe.run(document, {
      resultTypes: ['violations'],
      rules: {
        'region': { enabled: false },
        'landmark-one-main': { enabled: false },
        'color-contrast': { enabled: true },
      },
    })
    return (r.violations || []).map((v: any) => ({
      rule: v.id,
      impact: v.impact,
      nodes: v.nodes.length,
    }))
  })
  if (res.error) throw new Error(res.error)
  const blocking = res.filter((v: any) => v.impact === 'critical' || v.impact === 'serious')
  expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([])
}

test('login page on Mantine: surface + working login + a11y', { timeout: 900000 }, async ({ page }) => {
  const pageErrors: string[] = []
  const consoleErrors: string[] = []
  page.on('pageerror', e => pageErrors.push(String(e)))
  page.on('console', msg => {
    if (msg.type() === 'error') consoleErrors.push(msg.text())
  })

  await page.goto(B + '/login', { waitUntil: 'load' })

  // Mantine surface is live
  await expect(page.getByRole('button', { name: /log ?in/i }).first()).toBeVisible({ timeout: 15_000 })
  const mantineInputs = await page.locator('.mantine-Input-wrapper').count()
  expect(mantineInputs, 'two Mantine inputs').toBe(2)
  await expect(page.locator('#email')).toBeVisible()
  await expect(page.locator('#password')).toBeVisible()
  await expect(page.locator('a[href="/register"]')).toBeVisible()
  await expect(page.locator('a[href="/user/password/reset"]')).toBeVisible()

  // a11y on the signed-out page
  await axeScan(page)

  // REAL LOGIN through the Mantine form (proves hydrate-form + CSRF +
  // redirect survive the conversion); legacy contract selectors.
  await test.step('login through the Mantine form', async () => {
    await login(page, {
      email: 'e2e-user@e2e.test',
      password: 'Ol-Fixture-3m2Q',
      first_name: 'E2E',
      last_name: 'User',
    })
  })

  expect(pageErrors, JSON.stringify(pageErrors, null, 2)).toEqual([])
  expect(
    consoleErrors.filter(t => !/401|403|Failed to load resource/i.test(t)),
    JSON.stringify(consoleErrors, null, 2)
  ).toEqual([])
})

test('register page on Mantine: surface + fields + a11y', { timeout: 900000 }, async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', e => pageErrors.push(String(e)))

  await page.goto(B + '/register', { waitUntil: 'load' })

  const submit = page.getByRole('button', { name: /create ?account/i })
  await expect(submit.first()).toBeVisible({ timeout: 15_000 })
  await expect(page.locator('#firstNameField')).toBeVisible()
  await expect(page.locator('#lastNameField')).toBeVisible()
  await expect(page.locator('#emailField')).toBeVisible()
  await expect(page.getByRole('link', { name: /log ?in here/i }).first()).toBeVisible()

  await axeScan(page)

  // form is a real native form (async-form engine attachable) with CSRF
  const form = page.locator('form[data-ol-async-form]')
  await expect(form).toBeVisible()
  expect(await form.locator('input[name="_csrf"][type="hidden"]').count()).toBe(1)

  expect(pageErrors, JSON.stringify(pageErrors, null, 2)).toEqual([])
})

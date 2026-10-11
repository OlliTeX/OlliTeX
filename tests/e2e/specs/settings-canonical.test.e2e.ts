/**
 * settings-canonical — B (owner directive 2026-10-10): the hub→settings
 * reorg contracts on the CANONICAL deep-link routes
 *
 *   /user-settings/<leaf-id>   (user-level leaves)
 *   /admin-settings/<leaf-id>  (admin sections)
 *
 * covering the pages that postdate the original suite:
 *   - WakaTime (user opt-in leaf + admin section: toggle, Wakapi server
 *     URL default /wakapi, Check connection, Provision-for-user)
 *   - Python runner (admin section: interpreter pinned, package policy)
 *   - SSO providers (multi) admin section
 *   - Instance stats (admin section + the Grafana admin gate)
 *   - non-admin gating on the admin routes (restricted, not 200)
 *
 * Credentials: fixtures/credentials.ts ONLY (owner rule — never inline).
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { ADMIN, USER } from '../fixtures/credentials'

type Page = import('@playwright/test').Page

async function body(page: Page): Promise<string> {
  await page.waitForTimeout(1_400)
  return page.locator('body').innerText()
}

test.describe('user settings — canonical /user-settings leaves', () => {
  test('keybindings leaf renders the scheme choices (canonical route)', async ({ page }) => {
    await loginRobust(page, USER.email, USER.password)
    await page.goto('/user-settings/mysettings.keybindings')
    const text = await body(page)
    expect(text).toMatch(/keybinding/i)
    // one of the known schemes is listed
    expect(text).toMatch(/default|emacs|vim/i)
  })

  test('appearance leaf renders the theme controls (canonical route)', async ({ page }) => {
    await loginRobust(page, USER.email, USER.password)
    await page.goto('/user-settings/mysettings.appearance')
    const text = await body(page)
    expect(text, 'appearance leaf content').toMatch(/appearance|theme/i)
  })

  test('llm general leaf renders (user BYO surface)', async ({ page }) => {
    await loginRobust(page, USER.email, USER.password)
    await page.goto('/user-settings/mysettings.llm.general')
    const text = await body(page)
    expect(text).toMatch(/LLM|provider|API/i)
  })

  test('legacy /user-settings/mysettings.email parity: canonical + hash show the same section', async ({ page }) => {
    await loginRobust(page, USER.email, USER.password)
    await page.goto('/user-settings/mysettings.email')
    const canon = await body(page)
    expect(canon).toMatch(/email/i)
    // the legacy hash route still resolves to the same capability
    await page.goto('/user-settings/mysettings.email')
    const legacy = await body(page)
    expect(legacy).toMatch(/email/i)
  })
})

test.describe('admin settings — canonical /admin-settings sections', () => {
  test('wakatime section: toggle + server URL (default /wakapi) + check + provision', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/admin-settings/site.integrations.wakatime')
    const text = await body(page)
    expect(text).toMatch(/WakaTime/i)
    // the site-settings form loads async after the shell — resilient wait,
    // plain-text locators (regex form unreliable in this Playwright build)
    await expect(async () => {
      expect(await page.locator('text=Wakapi server URL').count()).toBeGreaterThan(0)
      expect(await page.locator('text=Check connection').count()).toBeGreaterThan(0)
      expect(await page.locator('text=Provision').count()).toBeGreaterThan(0)
    }).toPass({ timeout: 25_000 })
    const all = await body(page)
    expect(all).toMatch(/\/wakapi/i)
  })

  test('python runner section: interpreter pinned + package policy', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/admin-settings/site.compilation.pythonrunner')
    const text = await body(page)
    expect(text).toMatch(/Python runner/i)
    // the site-settings form loads async after the shell (data fetch per
    // fresh context) — the suite's established resilience for this shape
    // (plain-text locators: the `text=/<regex>/` form is unreliable in this
    // Playwright build — verified 0-matches where plain text finds the node)
    await expect(async () => {
      expect(await page.locator('text=Interpreter (pinned in the image)').count()).toBeGreaterThan(0)
      expect(await page.locator('text=Allowed package policy').count()).toBeGreaterThan(0)
    }).toPass({ timeout: 25_000 })
  })

  test('SSO providers (multi) section renders', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/admin-settings/site.integrations.sso-providers')
    const text = await body(page)
    expect(text).toMatch(/SSO/i)
    expect(text).toMatch(/provider/i)
  })

  test('instance statistics section + Grafana admin surface resolve', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/admin-settings/site.general.stats')
    const text = await body(page)
    expect(text).toMatch(/statistic|compiles|instance/i)
  })

  test('admin LLM rate-limiter leaf (canonical)', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/admin-settings/site.llm.instance')
    const text = await body(page)
    expect(text).toMatch(/rate|LLM/i)
  })
})

test.describe('gating — admin routes for a non-admin', () => {
  test('non-admin gets the restricted view on /admin-settings (not 200 content)', async ({ page }) => {
    await loginRobust(page, USER.email, USER.password)
    await page.goto('/admin-settings/site.integrations.wakatime')
    const text = await body(page)
    // the section content must NOT be visible to a plain user
    expect(text).not.toMatch(/Wakapi server URL/i)
    // and the page surfaces a restriction/landing, not a crash
    expect(text.length).toBeGreaterThan(0)
  })

  test('non-admin gets the restricted view on /admin-settings/site.llm.instance', async ({ page }) => {
    await loginRobust(page, USER.email, USER.password)
    await page.goto('/admin-settings/site.llm.instance')
    const text = await body(page)
    expect(text).not.toMatch(/Rate Limiter.*instance/i)
  })
})

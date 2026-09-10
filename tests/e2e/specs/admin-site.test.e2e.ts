/**
 * admin-site → /hub — the owner's admin console contracts, driven through the
 * unified /hub page (2026-09-07 owner: "adapt old e2e tests to /hub"):
 *  (1) the golden Site-settings sections render as NATIVE Mantine leaves on
 *      /hub (sandboxed/git/github/webdav/dropbox/templates/misc) — the
 *      legacy bootstrap tab rail no longer exists on /hub
 *  (2) pandoc toggle persists (site_settings round-trip) via the native
 *      /hub pandoc leaf — restored after (stack hygiene)
 *  (3) admin sees the LLM instance card on /user/llm-settings (R11-6; the
 *      user page surface stays a contract)
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { mongoEval } from '../helpers/host'
import { ADMIN } from '../fixtures/credentials'

async function go(page: import('playwright').Page) {
  await loginRobust(page, ADMIN.email, ADMIN.password)
}

test('/hub site-settings: golden sections render as native leaves (no legacy rail)', async ({ page }) => {
  await go(page)
  // native Mantine leaves (owner #32 remakes) — these must not carry the
  // legacy bootstrap tab rail at all
  const native: Array<[string, string[]]> = [
    ['site.compilation.sandboxed', ['Sandboxed compiles', 'Compile host dir', 'Add image']],
    ['site.compilation.git', ['Git integration', 'Host', 'Port']],
    ['site.compilation.github', ['GitHub sync', 'Client ID']],
    ['site.compilation.webdav', ['WebDAV', 'Root path']],
    ['site.compilation.dropbox', ['Dropbox', 'App key']],
  ]
  for (const [hash, labels] of native) {
    await page.goto(`/hub/#${hash}`)
    await page.waitForTimeout(1_600)
    const body = await page.locator('body').innerText()
    for (const label of labels) {
      expect(body, `${hash} should render "${label}"`).toContain(label)
    }
    // zero legacy bootstrap markup on these native leaves
    expect(await page.locator('#manage-site-root, .btn-primary, .form-control').count(),
      `${hash} must not render the legacy tab rail`).toBe(0)
  }
  // legacy-embed leaves (outside the #32 remake scope) still render their
  // sections — Templates + Misc
  for (const [hash, label] of [['site.general.managetpl', 'Templates'], ['site.general.misc', 'Misc']] as Array<[string, string]>) {
    await page.goto(`/hub/#${hash}`)
    await page.waitForTimeout(1_600)
    expect(await page.locator('body').innerText()).toContain(label)
  }
  // the LLM console branch lives OUTSIDE Site settings (site.llm.*); the
  // site leaves must not render it (R9-9 intent)
  const body = await page.locator('body').innerText()
  expect(body).not.toMatch(/Instance LLM Settings/i)
})

test('pandoc toggles + persists via /hub native leaf (site_settings round-trip)', async ({ page }) => {
  // Same design contract as before (round 9): pandoc.enabled persists to
  // site_settings and gates ENABLE_PANDOC_CONVERSIONS at restart. This spec
  // now pins it through the native /hub pandoc leaf: switch + Save flips the
  // stored flag, previous state restored afterwards.
  await go(page)
  await page.goto('/hub/#site.compilation.pandoc')
  await expect(page.getByRole('button', { name: 'Save' }).first()).toBeVisible({ timeout: 20_000 })
  // the enabled state is the FIRST role=switch on the native section
  const toggle = page.locator('input[role="switch"]').first()
  await expect(toggle).toBeVisible({ timeout: 15_000 })

  const stored = () =>
    String(
      mongoEval(
        'const d = db.getSiblingDB("sharelatex").site_settings.findOne({_id: "global"}); print(d && d.pandoc ? d.pandoc.enabled : "missing");'
      )
    ).trim()

  const prior = await toggle.isChecked()
  await toggle.click()
  await page.getByRole('button', { name: 'Save' }).first().click()
  await expect(async () => {
    expect(stored(), 'pandoc.enabled must flip in site_settings').toBe(String(!prior))
  }).toPass({ timeout: 20_000 })

  // stack hygiene: restore prior state
  await page.reload()
  await page.waitForTimeout(1_200)
  const t2 = page.locator('input[role="switch"]').first()
  if ((await t2.isChecked()) !== prior) {
    await t2.click()
    await page.getByRole('button', { name: 'Save' }).first().click()
    await expect(async () => {
      expect(stored(), 'prior state restored').toBe(String(prior))
    }).toPass({ timeout: 20_000 })
  }
})

test('admin sees the LLM instance (Rate Limiter) card on the hub (R11-6, moved + relabeled 2026-09-16)', async ({ page }) => {
  await go(page)
  await page.goto('/hub#/site.llm.instance', { waitUntil: 'domcontentloaded' })
  await expect(page.locator('text=/Rate Limiter/i').first()).toBeVisible({ timeout: 20_000 })
})

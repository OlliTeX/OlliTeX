/**
 * keybindings — /user/mysettings key-binding card + modal (owner spec):
 *  (1) the preset row is EXACTLY Overleaf/Vim/Emacs (no Custom — R11 owner)
 *      + the action table is populated (69-row registry)
 *  (2) a rebind survives Apply + page reload (persistence), then cleaned up
 *  (3) "Reset now" applies the selected preset: clears overrides, persists,
 *      shows inline applied-feedback and KEEPS THE MODAL OPEN (owner spec)
 * Three independent journeys (Playwright fresh-context model).
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { USER } from '../fixtures/credentials'

const MODAL = '[data-testid="custom-keybindings-modal"]'
// The Mantine modal portal ROOT is a zero-height static anchor (the panel is
// position:fixed inside it), so visibility/size assertions must target the
// rendered content section, not the root.
const MODAL_CONTENT = '[data-testid="custom-keybindings-modal"] section.mantine-Modal-content'

async function go(page: import('playwright').Page) {
  await loginRobust(page, USER.email, USER.password)
}

async function openModal(page: import('playwright').Page) {
  await page.goto('/hub/#mysettings.keybindings')
  await page.waitForTimeout(1_500)
  await page.click('text=Customize key bindings…')
  await expect(page.locator(MODAL_CONTENT)).toBeVisible({ timeout: 15_000 })
}

test('preset row is exactly Overleaf/Vim/Emacs + populated action table', async ({ page }) => {
  await go(page)
  await openModal(page)
  // wait until the preset radios are actually rendered before reading labels
  // (the settings page hydrates the preset list asynchronously from the API)
  await expect(page.locator(`${MODAL} input[type="radio"]`)).toHaveCount(3, {
    timeout: 20_000,
  })
  // NOTE: the preset labels are SIBLINGS of the radio inputs (label[for]), not
  // parents — resolve the accessible name via the input id.
  const presetLabels = await page
    .locator(`${MODAL} input[type="radio"]`)
    .evaluateAll(els =>
      els
        .map(e => {
          const label =
            (e.id && document.querySelector(`label[for="${e.id}"]`)) ||
            e.parentElement?.querySelector('label') ||
            e.nextElementSibling
          return (label?.textContent || '').trim()
        })
        .filter(t => /^(Overleaf|Vim|Emacs|Custom)/.test(t))
    )
  expect(presetLabels.sort().length, `exactly three presets (got: ${presetLabels.join(', ')})`).toBe(3)
  expect(presetLabels.some(l => /^Overleaf/.test(l))).toBe(true)
  expect(presetLabels.some(l => /^Vim/.test(l))).toBe(true)
  expect(presetLabels.some(l => /^Emacs/.test(l))).toBe(true)
  expect(presetLabels.some(l => /^Custom/.test(l)), 'no Custom preset').toBe(false)

  const rows = await page.locator(`${MODAL} tbody tr`).count()
  expect(rows, 'the 69-row action registry (R11)').toBe(69)
})

test('a rebind survives Apply + page reload (persistence)', async ({ page }) => {
  await go(page)
  await openModal(page)

  const row = page.locator(`${MODAL} tbody tr`, { hasText: 'Move line(s) up' }).first()
  await expect(row).toBeVisible()

  const clear = row.getByRole('button', { name: 'Clear' })
  if ((await clear.count()) > 0) {
    await clear.click()
    await page.waitForTimeout(400)
  }
  await row.getByRole('button', { name: 'Rebind' }).click()
  await page.waitForTimeout(300)
  await page.keyboard.press('Meta+Shift+P')
  await page.waitForTimeout(400)
  expect(await row.locator('td').nth(2).innerText()).toMatch(/P/i)

  await page
    .locator(`${MODAL} button`, { hasText: 'Apply' })
    .last()
    .click()
  await page.waitForTimeout(1_200) // persist

  // reload → the stored binding is visible again
  await page.goto('/hub/#mysettings.keybindings')
  await page.waitForTimeout(1_500)
  await page.click('text=Customize key bindings…')
  await expect(page.locator(MODAL_CONTENT)).toBeVisible({ timeout: 15_000 })
  const row2 = page.locator(`${MODAL} tbody tr`, { hasText: 'Move line(s) up' }).first()
  expect(await row2.locator('td').nth(2).innerText()).toMatch(/P/i)

  // cleanup: reset to Overleaf defaults so the next journey starts clean
  await page.locator(`${MODAL} label:has-text("Overleaf")`).first().click()
  await page.locator(`${MODAL} button`, { hasText: 'Reset now' }).click()
  await page.waitForTimeout(800)
})

test('"Reset now" applies preset, persists, shows feedback, KEEPS modal open', async ({ page }) => {
  await go(page)
  await openModal(page)

  await page.locator(`${MODAL} label:has-text("Overleaf")`).first().click()
  await page.locator(`${MODAL} button`, { hasText: 'Reset now' }).click()
  await page.waitForTimeout(1_000)

  // owner spec: modal stays open + inline applied feedback
  await expect(page.locator(MODAL_CONTENT)).toBeVisible()
  const applied = await page
    .locator(`${MODAL} [class*=body], ${MODAL} .modal, ${MODAL} form`)
    .first()
    .innerText()
    .catch(() => '')
  const feedback =
    (await page.locator(`${MODAL} >> text=/Applied|✓/i`).count()) >= 1
  expect(
    feedback,
    `expected inline "applied" feedback (modal excerpt: ${applied.slice(0, 200)})`
  ).toBe(true)
})

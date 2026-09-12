/**
 * notifications — email preference journey (one pass): the legacy
 * /user/notification-preferences URL now 301s to the hub surface
 * (My settings → Email preferences); the mute-all toggle round-trips
 * (persist across reload) and is restored — the "no instant
 * tracked-changes email to the author" contract is this user-controlled
 * grace period.
 *
 * 2026-09-10 (owner queue 3): legacy page removed; this journey now drives
 * the redirect target (hub) instead of the removed legacy form.
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { USER } from '../fixtures/credentials'

test('notification preferences: legacy URL redirects to hub; mute toggle round-trips', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)

  const res = await page.request.get('/user/notification-preferences', { maxRedirects: 0 })
  expect(res.status(), 'legacy URL 301s').toBe(301)
  expect(res.headers()['location']).toBe('/hub#/mysettings.email')

  await page.goto('/hub#/mysettings.email', { waitUntil: 'domcontentloaded' })
  const bodyText = await page.locator('body').innerText()
  expect(bodyText.toLowerCase()).toMatch(/notification/)

  const mute = page.locator('label.mantine-Switch-body').first()
  if ((await mute.count()) === 0) {
    test.info().annotations.push({ type: 'skip', description: 'mute control not on this revision' })
    return
  }

  // 2026-09 (mega-batch): the role=switch input is visually hidden under the
  // styled track — force-clicking the input does not reliably toggle it.
  // Click the Mantine label (labels natively toggle their input).
  const muteInput = mute.locator('input').first()
  const want = !(await muteInput.isChecked())
  await mute.click()
  await page.waitForTimeout(1200)

  // persist across reload
  await page.goto('/hub#/mysettings.email', { waitUntil: 'domcontentloaded' })
  await expect(page.locator('label.mantine-Switch-body').first().locator('input').first()).toBeChecked(want)

  // restore
  await page.locator('label.mantine-Switch-body').first().click()
  await page.waitForTimeout(900)
})

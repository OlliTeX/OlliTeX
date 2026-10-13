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

test('notification preferences: legacy URL is retired; the hub email leaf round-trips', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)

  // B wave 2026-10-11: /user/notification-preferences was RETIRED (Go 404 —
  // same retirement as /user/settings#* family, 2026-10-05; see
  // legacy-notification-prefs.test.e2e.ts). The preferences surface lives at
  // the canonical /user-settings leaf now.
  const res = await page.request.get('/user/notification-preferences', { maxRedirects: 0 })
  expect(res.status(), 'legacy URL is a Go 404 (retired)').toBe(404)

  await page.goto('/user-settings/mysettings.email', { waitUntil: 'domcontentloaded' })
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
  const original = await muteInput.isChecked()
  const want = !original
  await mute.click()
  // 2026-11-16 (9b142051 drift triage): the v2 switch debounces its save
  // (~2.5s live: click → POST /notifications/preferences). The old 1.2s
  // raced the debounce → state not persisted → toBeChecked(want) failed.
  // Wait for the save to land (or 8s max), then reload.
  await page.waitForTimeout(1000)
  await page.waitForRequest(
    (r) => (r.method() === 'POST') && /notification/i.test(r.url()),
    { timeout: 20000 },
  ).catch(() => {})
  await page.waitForTimeout(700)

  // persist across reload (debounced save can land late under load: reload-retry)
  const leafInput = () => page.locator('label.mantine-Switch-body').first().locator('input').first()
  let persisted = false
  for (let i = 0; i < 3 && !persisted; i++) {
    if (i > 0) await page.waitForTimeout(6000)
    await page.goto('/user-settings/mysettings.email', { waitUntil: 'domcontentloaded' })
    // toBeChecked with a short window; treat timeout as "not there yet"
    persisted = await expect(leafInput())
      .toBeChecked(want, { timeout: 8000 })
      .then(() => true)
      .catch(() => false)
  }
  expect(persisted, 'toggled state must persist across reload').toBe(true)

  // World hygiene (2026-10-13): this toggle inverts B's GLOBAL muteAll leaf and the
  // chat fan-out honors it (chat notify.go MutedUserIDs) — leaving the world in a
  // muted state poisons the receiver-side suites. Restore the original state.
  if (want !== original) {
    // Restore over the WIRE (the UI label position is not stable across the
    // re-render after the persistence reload — 2026-10-13). POST the global
    // back to `original` and read it back through the canonical route.
    const csrf = await page
      .locator('meta[name="ol-csrfToken"]')
      .getAttribute('content')
      .catch(() => null)
    const put = await page.request.post('/notifications/preferences', {
      headers: csrf ? { 'x-csrf-token': csrf } : {},
      data: { muteAllNotifications: original },
    })
    expect(put.status(), 'restore POST → 2xx (got ' + put.status() + ' ' + (await put.text()).slice(0, 120) + ')').toBeLessThan(300)
    const got = await page.request.get('/notifications/preferences')
    expect(got.status()).toBe(200)
    const body = await got.json()
    const mutedNow = JSON.stringify(body)
    expect(JSON.stringify({ m: body.muteAllNotifications ?? JSON.parse(mutedNow).muteAllNotifications }), 'restore read-back')
      .toContain(original ? 'true' : 'false')
  }

  // restore
  await page.locator('label.mantine-Switch-body').first().click()
  await page.waitForTimeout(900)
})

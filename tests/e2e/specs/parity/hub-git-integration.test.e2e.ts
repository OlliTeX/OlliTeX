/**
 * 2026-09-11 (owner batch 2 item 6): Git integration (git-bridge tokens)
 * restored to the hub mysettings — #/mysettings.gitsync.
 *
 * Covers: leaf renders, add token → one-time raw reveal (olp_…), token
 * listed, revoke. API round-trips stay in legacy-admin-site/endpoint specs;
 * this is the UI surface the owner asked for.
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { ADMIN } from '../../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'
let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
  await p.goto(BASE + '/hub#/mysettings.gitsync', { waitUntil: 'domcontentloaded' })
})
test.afterAll(async () => { if (p) await p.context().close().catch(() => {}) })

test('renders: git-bridge token surface with create + limits copy', async () => {
  const body = (await p.locator('body').innerText()) || ''
  expect(/Git authentication tokens/i.test(body), 'intro present').toBeTruthy()
  expect(/up to 10 tokens/i.test(body), 'limit copy').toBeTruthy()
  await expect(p.locator('main button:has-text("Add another token")').first()).toBeVisible({ timeout: 15000 })
})

test('flow: add → one-time reveal → listed → revoke', async () => {
  const before = await p.locator('main table tbody tr').count()

  await p.locator('main button:has-text("Add another token")').first().click()
  const dlg = p.locator('[role="dialog"]')
  await expect(dlg).toBeVisible({ timeout: 15000 })

  // the raw token (olp_…) must be revealed exactly once
  const revealed = await dlg.locator('pre, code').first().innerText().catch(() => '')
  expect(revealed, 'raw token revealed').toMatch(/^olp_/)

  await dlg.locator('button:has-text("Done")').click()
  await expect(async () => {
    expect(await p.locator('main table tbody tr').count()).toBe(before + 1)
  }).toPass({ timeout: 15000 })

  // revoke the fresh row
  await p.locator('main table tbody tr').last().locator('[aria-label="Remove token"]').first().click()
  const confirm = p.locator('[role="dialog"]')
  await expect(confirm).toBeVisible({ timeout: 10000 })
  await confirm.locator('button:has-text("Remove")').first().click()
  await expect(async () => {
    expect(await p.locator('main table tbody tr').count()).toBe(before)
  }).toPass({ timeout: 15000 })
})

/**
 * grammar — per-user grammar-check preference (one journey):
 *  GET is readable, POST round-trips a mode (stored preference, not just
 *  the derived effective one), and the mysettings editor-settings page
 *  renders the grammar section. (POSTs carry the csrf token via authFetch.)
 */
import { test, expect } from '@playwright/test'
import { USER } from '../fixtures/credentials'
import { authFetch, loginRobust } from '../helpers/auth'

const MODES = ['default', 'lt', 'llm', 'lt+llm']

test('grammar settings read + save round-trip + mysettings section renders', async ({ page, context }) => {
  await loginRobust(page, USER.email, USER.password)
  await page.waitForTimeout(500)

  const getRes = await authFetch(context, page, 'GET', '/user/llm-settings/grammar')
  expect(getRes.status(), await getRes.text().catch(() => '')).toBe(200)
  const before = await getRes.json()
  const stored = MODES.includes(before.storedMode ?? before.mode)
    ? (before.storedMode ?? before.mode)
    : 'default'

  const save = await authFetch(context, page, 'POST', '/user/llm-settings/grammar', {
    mode: stored,
  })
  expect(save.status(), JSON.stringify(await save.json().catch(() => ({})))).toBe(200)

  const afterRes = await authFetch(context, page, 'GET', '/user/llm-settings/grammar')
  const after = await afterRes.json().catch(() => ({}))
  const afterStored = after.storedMode ?? after.mode
  expect(afterStored).toBe(stored)

  // 2026-09-16: the legacy /user/llm-settings page is removed — the grammar
  // picker renders on the hub leaf (My settings → LLM → Grammar Checking).
  await page.goto('/hub#/mysettings.llm.grammar', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(2_000)
  const body = await page.locator('body').innerText()
  expect(body).toMatch(/Grammar/i)
})

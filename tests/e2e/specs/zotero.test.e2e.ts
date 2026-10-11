/**
 * zotero — graceful-degradation contract + /library render (one journey):
 *  zotero surfaces must answer GRACEFUL JSON (2xx or 4xx — the "disabled
 *  on this site" 403 counts) with no valid credential — NEVER 5xx (the
 *  regression class we fixed for webdav, R11). 5xx is retried a couple of
 *  times (cold-stack service warmup window).
 */
import { test, expect } from '@playwright/test'
import { USER } from '../fixtures/credentials'
import { authFetch, loginRobust } from '../helpers/auth'

async function expectNot5xx(res: import('playwright').APIResponse): Promise<number> {
  let status = res.status()
  for (let i = 0; i < 2 && status >= 500; i++) {
    await new Promise((r) => setTimeout(r, 4_000)) // cold-stack warmup
    const r2 = await (res.request()).context.get(res.url())
    status = r2.status()
  }
  return status
}

test('zotero is graceful without credentials + /library renders', async ({ page, context }) => {
  await loginRobust(page, USER.email, USER.password)
  await page.waitForTimeout(500)

  const groups = await authFetch(context, page, 'GET', '/user/zotero/groups')
  const gbody = (await groups.text().catch(() => '')).slice(0, 200)
  expect(
    await expectNot5xx(groups),
    `zotero groups must not 5xx: ${gbody}`
  ).toBeLessThan(500)

  const connect = await authFetch(context, page, 'POST', '/user/zotero/connect', {
    token: 'e2e-bad-token',
    scopes: 'libraries:read',
  })
  const cbody = (await connect.text().catch(() => '')).slice(0, 200)
  expect(await expectNot5xx(connect), `zotero connect bad-token graceful: ${cbody}`).toBeLessThan(500)

  // B wave 2026-10-11: the top-level /library page was retired (Go 404 — see
  // parity/legacy-library.test.e2e.ts); the library surface is the hub leaf.
  const res = await page.goto('/hub#/library')
  expect(res?.status() ?? 200).toBe(200)
  await page.waitForTimeout(1_500)
  const body = await page.locator('body').innerText()
  expect(body.toLowerCase()).toMatch(/bib|library|bibtex/)
})

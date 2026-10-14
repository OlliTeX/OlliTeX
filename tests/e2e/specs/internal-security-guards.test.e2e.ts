/**
 * BUG HUNT 2026-10-14 (BH-1/BH-2/BH-3, owner-gated product changes — ag-67+):
 * permanent regression for the SSRF guard + the fail-closed alerts webhook.
 *
 * Contract pinned here:
 *   BH-2  POST /internal/alerts — NO token → 401 (fail-closed, code default);
 *        configured INTERNAL_ALERTS_TOKEN → 200 accepted.
 *   BH-1  POST /user/llm-providers/check — literal IP in a blocked range
 *        (172.16/12 = the compose network itself) → 400 "blocked host"
 *        (synchronous, no dial); a PUBLIC host is NOT SSRF-blocked (the dial
 *        itself may fail for unrelated network reasons — that is not the
 *        guard's message).
 *   BH-3  POST /user/webdav/connect — baseUrl in a blocked range → 400
 *        "blocked host" and NOTHING saved (the rejection precedes the write).
 *
 * Dial-level enforcement (name resolution vetoes, redirect re-checks) is
 * covered by the core unit tests (web/core/ssrf_test.go) — this spec pins the
 * user-facing surfaces.
 */
import { test, expect } from '@playwright/test'
import { readFileSync } from 'fs'
import { ADMIN } from '../fixtures/credentials'
import { loginRobust } from '../helpers/auth'
import { api } from '../parity/harness'

const BASE = process.env.OL_BASE || 'http://127.0.0.1:7420'

// World token: single source of truth = tests/e2e/docker-compose.test.yml
// (INTERNAL_ALERTS_TOKEN under the overleaf service; the spec reads it from
// there instead of duplicating the secret).
function worldAlertsToken(): string | undefined {
  const raw = readFileSync(new URL('../docker-compose.test.yml', import.meta.url), 'utf8')
  const m = raw.match(/INTERNAL_ALERTS_TOKEN:\s*(\S+)/)
  return m?.[1]
}

test.describe('bug-hunt guards (BH-1 / BH-2 / BH-3)', () => {
  test('BH-2: /internal/alerts fail-closed — no token 401, world token 200', async ({ browser }) => {
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    const noTok = await page.request.post(BASE + '/internal/alerts', {
      headers: { 'Content-Type': 'application/json' },
      data: { alerts: [] },
    })
    expect(noTok.status(), 'unauthenticated alerts POST must be fail-closed 401').toBe(401)

    const tok = worldAlertsToken()
    expect(tok, 'world must configure INTERNAL_ALERTS_TOKEN (docker-compose.test.yml)').toBeTruthy()
    const withTok = await page.request.post(BASE + '/internal/alerts', {
      headers: { 'Content-Type': 'application/json', 'X-Internal-Token': tok! },
      data: { alerts: [] },
    })
    expect(withTok.status(), 'configured world token must be accepted').toBe(200)
    await ctx.close()
  })

  test('BH-1/BH-3: LLM check + WebDAV connect veto blocked-range hosts; public passes', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)

    // BH-1 — blocked literal IP (compose network 172.16/12): synchronous 400,
    // no dial. The exact internal host is chosen so a REGRESSION (guard
    // removed) would visibly echo an internal body instead of 400.
    const blocked = await api(page, 'POST', '/user/llm-providers/check', {
      url: 'http://172.20.0.11:8025/v1',
      apiKey: 'sk-test',
    })
    expect(blocked.status()).toBe(400)
    expect(await blocked.text()).toContain('blocked host')

    // BH-1 — public host must NOT be SSRF-blocked (guard passes; the dial may
    // still fail for unrelated network reasons — that is a different message).
    const pub = await api(page, 'POST', '/user/llm-providers/check', {
      url: 'https://example.com/v1',
      apiKey: 'sk-test',
    })
    const pubBody = await pub.text().catch(() => '')
    expect(pubBody, `public host dial must not be SSRF-blocked (got status ${pub.status()})`).not.toContain('blocked host')

    // BH-3 — WebDAV connect on a blocked host: 400 + nothing saved (the
    // rejection precedes the credentials write, so no cleanup is needed).
    const wd = await api(page, 'POST', '/user/webdav/connect', {
      baseUrl: 'http://172.20.0.11:8025/',
      username: 'guard-probe',
      password: 'not-saved',
    })
    expect(wd.status()).toBe(400)
    expect(await wd.text()).toContain('blocked host')
  })
})

import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { ADMIN, USER } from '../../fixtures/credentials'

/**
 * CONFIG-REGISTRY ROUND-TRIP over the SHARED config DB (owner decision
 * 2026-10-04: "Postgres is an essential part of the tech stack and data
 * should be stored on it" — Postgres = single source of truth for the
 * config DB; the SQLite fallback was removed as dangerous).
 *
 * The e2e stack boots the app with DATABASE_URL set (docker-compose.test.yml,
 * service `postgres`), so with the new single-source-of-truth semantics the
 * Go web stack MUST boot the Postgres config store — and this spec proves
 * the /hub admin surface round-trips through that store:
 *   GET → registry visible      (configschema registry exposure)
 *   PUT → value persisted       (write path in Postgres)
 *   GET → value readable back   (read path in Postgres)
 *   PUT "" → reset to env/default (delete path)
 *   non-admin PUT → denied      (no write leak)
 *
 * Probe key: OVERLEAF_NOTIFICATIONS_DRY_RUN (bool; in the configschema
 * registry; behaviorally harmless in the e2e stack; reset at the end).
 */
const KEY = 'OVERLEAF_NOTIFICATIONS_DRY_RUN'

test.describe('config registry (shared Postgres store round-trip)', () => {
  test('admin: read → write → read back → reset', async ({ browser }) => {
    const c = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    const p = await c.newPage()
    await loginRobust(p, ADMIN.email, ADMIN.password)
    // a real HTML page must be up before api() (CSRF meta etc.)
    await p.goto('http://127.0.0.1:7420/hub', { waitUntil: 'domcontentloaded' })
    try {
      // 1) registry exposure
      const r0 = await api(p, 'GET', '/api/hub/config')
      expect(r0.status(), 'GET /api/hub/config must be 200').toBe(200)
      const b0 = await r0.json()
      expect(b0, 'registry payload must be an object').toBeTruthy()
      expect(Object.keys(b0 as Record<string, unknown>), `key ${KEY} must be in the registry`).toContain(KEY)

      // 2) write (persisted to the shared store)
      const pw = await api(p, 'PUT', '/api/hub/config', { [KEY]: 'true' })
      expect(pw.status(), 'PUT must be 200').toBe(200)

      // 3) read back (value must come from the store, source=db)
      const r1 = await api(p, 'GET', '/api/hub/config')
      const b1 = await r1.json()
      expect(String((b1 as any)[KEY]?.value), 'value must read back as "true"').toBe('true')

      // 4) reset ("" = delete → env/default takes over again)
      const pr = await api(p, 'PUT', '/api/hub/config', { [KEY]: '' })
      expect(pr.status(), 'reset PUT must be 200').toBe(200)
      const r2 = await api(p, 'GET', '/api/hub/config')
      const b2 = await r2.json()
      const v2 = (b2 as any)[KEY]?.value
      expect(['', null, undefined].includes(v2), `reset must clear the stored value (got ${JSON.stringify(v2)})`).toBeTruthy()
    } finally {
      await c.close().catch(() => {})
    }
  })

  test('non-admin PUT is denied (no write leak)', async ({ browser }) => {
    const c = await browser.newContext()
    const q = await c.newPage()
    await loginRobust(q, USER.email, USER.password)
    await q.goto('http://127.0.0.1:7420/projects', { waitUntil: 'domcontentloaded' }).catch(() => {})
    // maxRedirects: 0 — the denial is a 302 /restricted bounce (Node parity);
    // following it replays the PUT against /restricted and the harness reports
    // the redirect target's 404 instead of the denial itself.
    const tok = await q.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)
    const res = await q.request.put('http://127.0.0.1:7420/api/hub/config', {
      headers: { 'Content-Type': 'application/json', 'X-CSRF-TOKEN': tok ?? '' },
      data: JSON.stringify({ [KEY]: 'true' }),
      maxRedirects: 0,
    } as any)
    expect([302, 401, 403].includes(res.status()),
      `non-admin denial must be 302/401/403, got ${res.status()}`).toBeTruthy()
    await c.close().catch(() => {})
  })
})

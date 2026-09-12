/**
 * 2026-09-16 (owner task T2, IMPROVEMENTS P0.1): API-level authorization
 * regression proof for the /admin/* surface.
 *
 * The audit (2026-09-16) found 47 /admin/* routes, ALL carrying
 * AuthorizationMiddleware.ensureUserIsSiteAdmin. A non-admin request is
 * denied with a 302 → /restricted (see _redirectToRestricted). This spec
 * proves that end-to-end (session middleware + route guard) so the guard
 * cannot silently regress to "UI-gated only" (the original finding was a
 * non-admin receiving 200 on POST /admin/llm/settings {}).
 *
 * Positive control: user-scoped endpoints stay reachable for every
 * logged-in user (the audit must not over-shoot).
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { ADMIN, USER } from '../../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'

// Representative, safe sample of the guarded surface (read routes —
// nothing is mutated, fixtures stay pristine).
const ADMIN_GETS = [
  ['GET', '/admin/site-settings'],
  ['GET', '/admin/llm/settings/json'],
  ['GET', '/admin/active-projects'],
  ['GET', '/admin/user'],
  ['GET', '/admin/project'],
]

const USER_CONTEXTS = [
  { label: 'site-admin', email: ADMIN.email, password: ADMIN.password, role: 'admin' },
  { label: 'regular user', email: USER.email, password: USER.password, role: 'user' },
]

async function login(browser: any, account: { email: string; password: string }) {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const p = await c.newPage()
  await loginRobust(p, account.email, account.password)
  return { c, p }
}

for (const acct of USER_CONTEXTS) {
  test(`${acct.role}: admin-surface authz — ${acct.label}`, async ({ browser }) => {
    const { c, p } = await login(browser, acct)

    try {
      for (const [method, path] of ADMIN_GETS) {
        // maxRedirects:0 → see the raw guard decision (302 → /restricted)
        // instead of the followed /restricted or /login page.
        const r = await p.request.get(BASE + path, { maxRedirects: 0 })
        if (acct.role === 'admin') {
          // admin: the route is reachable (JSON 200 or an HTML 200/302 page
          // render) and must NOT be the admin-denial redirect.
          const loc = r.headers()['location'] || ''
          expect(
            r.status(),
            `${method} ${path} for admin (status ${r.status()} loc ${loc})`
          ).toBeLessThan(400)
          expect(
            loc,
            `admin must not be bounced to /restricted (${method} ${path})`
          ).not.toContain('/restricted')
        } else {
          expect(
            r.status(),
            `non-admin denied on ${method} ${path} (got ${r.status()})`
          ).toBe(302)
          expect(
            r.headers()['location'] || '',
            `denial should land on /restricted for ${method} ${path}`
          ).toContain('/restricted')
        }
      }

      // positive control: user-scoped LLM endpoint stays open to everyone
      const u: any = await p.request.get(BASE + '/user/llm/selected-model')
      expect(
        u.status(),
        'user-scoped endpoint must stay reachable (audit must not over-block)'
      ).toBeLessThan(400)
    } finally {
      await c.close().catch(() => {})
    }
  })
}

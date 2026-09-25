/**
 * consent — the GDPR cookie-consent surface (P7-post item 2).
 *
 * The Go web owns the server side of the consent flow:
 *   - POST /cookie-consent  (CSRF-protected; allowlisted value)
 *   - GET  /cookie-consent  (parse + report the current choice)
 *   - GET  /legal           (the banner's privacy/cookie-policy link target)
 * and the frontend cookie banner's buttons point at the same values
 * (data-ol-cookie-banner-set-consent="essential"|"all").
 *
 * Anonymous by design: consent is per-browser and the banner also shows on
 * unauthenticated pages, so every assertion here runs WITHOUT a login.
 *
 * Server contract (go/services/web/features/consent):
 *   200 {"ok":true,"consent":"all"|"essential"}
 *       + Set-Cookie oa=1|0; Path=/; Max-Age=31536000; Secure; SameSite=Lax
 *   400 {"ok":false,"error":"invalid_consent"}   (any other value)
 *   GET  {"consent":"all"|"essential"|null}
 *
 * Test-mechanics notes (both pinned empirically, 2026-09, this config):
 *  - Playwright's API `request` context does NOT sync the browser context's
 *    cookie JAR (probe: jar cookie → 403; same cookie via explicit header →
 *    200). So the session for the CSRF-protected POSTs is acquired through
 *    `request` itself (GET /login populates the request context's own jar),
 *    and seeded-choice GETs send the `cookie` header explicitly.
 *  - Browsers discard Secure cookies set over the plain-http test origin, so
 *    the JAR is never inspected after the POST; the Set-Cookie HEADER is the
 *    server contract under test and is asserted verbatim.
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { ADMIN } from '../fixtures/credentials'

// acquire the anonymous session + csrf token through the request context's
// own jar (see file doc: no page↔request jar sync).
async function sessionWithCsrf(request) {
  const res = await request.get('/login')
  const html = await res.text().catch(() => '')
  const csrf = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  return { csrf, loginStatus: res.status() }
}

test.describe('consent — server endpoints', () => {
  test('GET /cookie-consent reports null when no choice exists', async ({ request }) => {
    const res = await request.get('/cookie-consent')
    expect(res.status()).toBe(200)
    expect(await res.json()).toEqual({ consent: null })
  })

  test('POST without CSRF token is rejected (403)', async ({ request }) => {
    const res = await request.post('/cookie-consent', {
      data: { consent: 'all' },
      headers: { accept: 'application/json' },
    })
    expect(res.status()).toBe(403)
  })

  test('POST with an invalid value gets the 400 contract', async ({ request }) => {
    const { csrf, loginStatus } = await sessionWithCsrf(request)
    expect(loginStatus).toBe(200)
    expect(csrf, 'login page must carry the csrf meta').toBeTruthy()

    for (const bad of [
      { consent: 'tracker' },
      { consent: '' },
      { consent: 1 },
      {},
    ]) {
      const res = await request.post('/cookie-consent', {
        data: bad,
        headers: { 'x-csrf-token': csrf!, accept: 'application/json' },
      })
      expect(res.status(), JSON.stringify(bad)).toBe(400)
      expect(await res.json()).toEqual({ ok: false, error: 'invalid_consent' })
    }
  })

  test('POST "all" → 200 contract + canonical Set-Cookie', async ({ request }) => {
    const { csrf } = await sessionWithCsrf(request)

    const res = await request.post('/cookie-consent', {
      data: { consent: 'all' },
      headers: { 'x-csrf-token': csrf!, accept: 'application/json' },
    })
    expect(res.status()).toBe(200)
    expect(await res.json()).toEqual({ ok: true, consent: 'all' })

    const setCookie = res.headers()['set-cookie'] ?? ''
    // the response refreshes the session cookie too (HttpOnly — expected);
    // assert the CONTRACT line for the consent cookie specifically
    const oaLine = setCookie.split(/[,\n]/).map((part) => part.trim()).find((part) => part.startsWith('oa=')) ?? ''
    expect(oaLine, 'Set-Cookie must carry the oa consent cookie').toContain('oa=1')
    expect(oaLine).toContain('Path=/')
    expect(oaLine).toContain('Max-Age=31536000')
    expect(oaLine).toContain('Secure')
    expect(oaLine).toContain('SameSite=Lax')
    // deliberately NOT HttpOnly: the first-party consent gate reads it
    expect(oaLine).not.toContain('HttpOnly')
  })

  test('POST "essential" → 200 contract + oa=0 Set-Cookie', async ({ request }) => {
    const { csrf } = await sessionWithCsrf(request)

    const res = await request.post('/cookie-consent', {
      data: { consent: 'essential' },
      headers: { 'x-csrf-token': csrf!, accept: 'application/json' },
    })
    expect(res.status()).toBe(200)
    expect(await res.json()).toEqual({ ok: true, consent: 'essential' })
    const setCookie = res.headers()['set-cookie'] ?? ''
    const oaLine = setCookie.split(/[,\n]/).map((part) => part.trim()).find((part) => part.startsWith('oa=')) ?? ''
    expect(oaLine).toContain('oa=0')
  })

  test('GET echoes the seeded choice (all / essential / garbage)', async ({ request }) => {
    // oa=1 → all
    let res = await request.get('/cookie-consent', {
      headers: { cookie: 'oa=1' },
    })
    expect(res.status()).toBe(200)
    expect(await res.json()).toEqual({ consent: 'all' })

    // oa=0 → essential
    res = await request.get('/cookie-consent', {
      headers: { cookie: 'oa=0' },
    })
    expect(await res.json()).toEqual({ consent: 'essential' })

    // unparseable → null
    res = await request.get('/cookie-consent', {
      headers: { cookie: 'oa=banana' },
    })
    expect(await res.json()).toEqual({ consent: null })
  })
})

test.describe('consent — legal page + banner wiring', () => {
  test('GET /legal serves the cookie policy (variants, anchors, CSP-safe)', async ({ request }) => {
    for (const variant of ['/legal', '/legal/', '/Legal', '/LEGAL']) {
      const res = await request.get(variant)
      expect(res.status(), variant).toBe(200)
    }
    const res = await request.get('/legal')
    expect(res.headers()['content-type'] ?? '').toContain('text/html')
    const body = await res.text()
    expect(body).toContain('<section id="cookies"')
    expect(body).toContain('<section id="privacy"')
    expect(body).toContain('oa=1')
    expect(body).toContain('oa=0')
    // CSP safety under the app default policy (default-src 'none'):
    expect(body).not.toContain('<script')
    expect(body).not.toContain('<style')
    expect(body).not.toContain('https://')
  })

  test('the cookie banner buttons + their /legal#Cookies link all resolve', async ({ page }) => {
    // The cookie banner (shipped in the page views; the Mantine auth surface
    // has none) is verified on a stable logged-in page that renders it.
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const res = await page.goto('/user/settings')
    expect(res.status()).toBe(200)
    const html = await page.content()
    // the banner offers both consent values…
    expect(html).toContain('data-ol-cookie-banner-set-consent="essential"')
    expect(html).toContain('data-ol-cookie-banner-set-consent="all"')
    // …and every banner link target the server now actually serves
    const legalLinks = [...html.matchAll(/href="([^"]*\/legal[^"]*)"/g)].map((m) => m[1])
    expect(legalLinks.length, 'banner must link the cookie policy').toBeGreaterThan(0)
    for (const href of legalLinks) {
      const path = href.split('#')[0]
      const r = await page.request.get(path)
      expect(r.status(), href).toBe(200)
    }
  })
})

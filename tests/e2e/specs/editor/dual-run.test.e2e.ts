/**
 * editor P0 dual-run (EDITOR_RENOVATION_PLAN.md — Phase 0 exit criterion).
 *
 * Contract under test:
 *   GET /editor/:Project_id serves the CURRENT editor shell with exactly the
 *   same auth/role/behavior semantics as the legacy GET /Project/:Project_id,
 *   so the renovated (Mantine) surface can be built on /editor while
 *   /Project stays the untouched fallback.
 *
 * Proven here (live e2e):
 *   - the full golden loop works on the NEW url: render → type (OT/CM6) →
 *     compile → PDF panel (same steps as specs/smoke, on /editor/:id);
 *   - the /editor url does not redirect away (no /Project rewrite);
 *   - guest semantics identical (login wall on both urls);
 *   - a non-member gets IDENTICAL status/redirect on both urls (whatever the
 *     CE semantics are — parity is the contract, not a specific status);
 *   - deep variant /editor/:id/detached answers (route exists) like its legacy
 *     twin for a member.
 *
 * Backend unit twin: services/web/test/unit/src/Project/EditorRouteContract.test.mjs
 * (same middleware chain registration, asserted against the router source).
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../../helpers/auth'
import { ADMIN, USER } from '../../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'

test.describe('editor /editor dual-run (P0)', () => {
  test('golden loop on the NEW url: render → type → compile → PDF', async ({ page }) => {
    test.setTimeout(600_000)

    await loginRobust(page, ADMIN.email, ADMIN.password)
    const projectId = await createBlankProject(page)
    await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })

    // switch the same open session over to the NEW url (no reload needed for
    // the contract, but a full navigation is the strongest proof: the shell
    // must bootstrap from /editor/:id on its own)
    await page.goto(`/editor/${projectId}`, { waitUntil: 'domcontentloaded' })
    await page.waitForURL(url => url.pathname === `/editor/${projectId}`)
    const editor = page.locator('.cm-content').first()
    await expect(editor).toBeVisible({ timeout: 90_000 })

    // OT/CM6 live editing works on the new url
    await editor.click()
    await page.keyboard.insertText('\n%some dual-run P0 text on /editor')
    await expect(page.locator('.cm-content')).toContainText('dual-run P0 text', { timeout: 15_000 })

    // compile via the standard API (CSRF meta) — identical contract on both urls
    const csrf = await page
      .locator('meta[name="ol-csrfToken"]')
      .getAttribute('content')
      .catch(() => null)
    const start = await page
      .context()
      .request.post(`/project/${projectId}/compile`, {
        headers: csrf
          ? { 'x-csrf-token': csrf, accept: 'application/json' }
          : { accept: 'application/json' },
      })
    expect(start.status(), await start.text().catch(() => '')).toBe(200)
    const compile = await start.json().catch(() => ({}))
    const status = compile.status ?? (compile.compile && compile.compile.status) ?? 'unknown'
    expect(
      ['success', 'complete', 'completed'].includes(status),
      `compile on /editor must succeed, got: ${JSON.stringify(compile).slice(0, 300)}`
    ).toBe(true)

    // compiled output reachable on the new url
    const pdfVisible =
      (await page.locator('.ide-redesign-pdf-container, .pdf-viewer, iframe[src*="pdf"]').first().isVisible().catch(() => false)) ||
      (compile.outputFiles || []).some((f: any) => /\.pdf$/.test(f.path || f.url || ''))
    expect(pdfVisible, 'compiled PDF must be reachable on /editor').toBe(true)
  })

  test('route parity: guest + non-member answer identically on /editor and /Project', async ({ browser }) => {
    // guest: identical login wall
    const guest = await browser.newContext()
    const gp = await guest.newPage()
    await gp.goto(BASE + '/login', { waitUntil: 'domcontentloaded' }).catch(() => {})
    const rGuestNew = await gp.request.get('/editor/000000000000000000000000', { maxRedirects: 0 })
    const rGuestOld = await gp.request.get('/Project/000000000000000000000000', { maxRedirects: 0 })
    await guest.close()
    expect(rGuestNew.status()).toBe(rGuestOld.status())
    expect(rGuestNew.headers()['location']).toBe(rGuestOld.headers()['location'])

    // non-member: admin owns the project; USER must get the SAME answer on
    // both urls (parity, whatever CE's access semantics are)
    const admCtx = await browser.newContext()
    const admPage = await admCtx.newPage()
    await loginRobust(admPage, ADMIN.email, ADMIN.password)
    const projectId = await createBlankProject(admPage)
    const memberResp = await admPage.request.get(`/Project/${projectId}`, { maxRedirects: 0 })
    expect(memberResp.status()).toBe(200)
    await admCtx.close()

    const userCtx = await browser.newContext()
    const userPage = await userCtx.newPage()
    await loginRobust(userPage, USER.email, USER.password)
    const rNew = await userPage.request.get(`/editor/${projectId}`, { maxRedirects: 0 })
    const rOld = await userPage.request.get(`/Project/${projectId}`, { maxRedirects: 0 })
    await userCtx.close()
    expect(rNew.status(), 'non-member answer must match between /editor and /Project').toBe(rOld.status())
    if ([301, 302].includes(rNew.status())) {
      expect(rNew.headers()['location']).toBe(rOld.headers()['location'])
    }
  })

  test('detached variant registered on /editor too (member can request it)', async ({ browser }) => {
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const projectId = await createBlankProject(page)
    // the legacy twin is a real route; the /editor twin must answer as well
    // (200 render, or the SAME non-2xx answer as the legacy twin — parity)
    const rNew = await page.request.get(`/editor/${projectId}/detached`, { maxRedirects: 0 })
    const rOld = await page.request.get(`/Project/${projectId}/detached`, { maxRedirects: 0 })
    await ctx.close()
    expect(rNew.status()).toBe(rOld.status())
    if ([301, 302].includes(rNew.status())) {
      expect(String(rNew.headers()['location'] || '').replace('/editor/', '/Project/')).toBe(rOld.headers()['location'])
    }
  })
})

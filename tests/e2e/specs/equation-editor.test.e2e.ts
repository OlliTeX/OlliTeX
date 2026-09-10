/**
 * Equation Editor (MathLive) — services/web/modules/latex-editor
 *
 * Regression spec for the owner's "my latex editor is gone (including the
 * button and context menu)" report:
 *
 * Root cause (fixed 2026-09-10, latex-editor-toolbar-button.tsx): the
 * `.tex`-only gate read `openDoc.name` — but in this codebase `openDoc`
 * is the OPEN ACTION (a function from editor-manager-context); a
 * function's `.name` is always the string "openDoc", so
 * `activeFileIsTex` was false on EVERY file → the button rendered null
 * → its 'latex-editor:open' listener never mounted → the math-preview
 * tooltip action was dead as well. Gate now uses the exposed
 * open-document name (useEditorOpenDocContext().openDocName).
 *
 * Coverage:
 *   1. /editor  — toolbar shows the "Equation Editor" button (ƒ icon)
 *   2. /Project — same on the legacy route (dual-route invariant)
 *   3. round-trip — click → floating window opens → MathLive math-field
 *      (wasm + bundled fonts, no CDN) mounts → type `x+y=2` →
 *      "Export equation to cursor in document" → the document gains
 *      `$x+y=2$` (wrapped inline math).
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'

const B = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

test.describe('equation editor (MathLive)', () => {
  let c: any
  let page: any
  let pid: string
  const pageErrors: string[] = []

  test.beforeAll(async ({ browser }) => {
    c = await browser.newContext({ viewport: { width: 1600, height: 1000 } })
    page = await c.newPage()
    page.on('pageerror', (e: Error) => pageErrors.push(String(e).slice(0, 200)))
    await loginRobust(page, 'e2e-admin@e2e.test', 'Ol-Fixture-9x7K')
    pid = await createBlankProject(page)
  })

  test.afterAll(async () => {
    try {
      await page.request.delete('/project/' + pid).catch(() => {})
    } catch {}
    await c.close().catch(() => {})
  })

  test('/editor: Equation Editor button visible in source toolbar', async () => {
    await page.goto(B + '/editor/' + pid, { waitUntil: 'load' })
    await page.waitForSelector('.cm-content', { timeout: 25000 })
    await page.waitForTimeout(2500)
    const btn = page.locator('[aria-label="Equation Editor"]').first()
    await expect(btn).toBeVisible()
    // meta feature gate must be on (pug boolean: content attr present)
    const hasAttr = await page
      .locator('meta[name="ol-latexEditorAvailable"]')
      .evaluate((el) => el.hasAttribute('content'))
    expect(hasAttr, 'ol-latexEditorAvailable must be true').toBe(true)
    expect(pageErrors, 'no pageerrors: ' + pageErrors.join(' | ')).toEqual([])
  })

  test('/Project (legacy route): button visible too', async () => {
    await page.goto(B + '/Project/' + pid, { waitUntil: 'load' })
    await page.waitForSelector('.cm-content', { timeout: 25000 })
    await page.waitForTimeout(2500)
    await expect(page.locator('[aria-label="Equation Editor"]').first()).toBeVisible()
  })

  test('round-trip: open window, MathLive input, export inserts $...$', async () => {
    await page.goto(B + '/editor/' + pid, { waitUntil: 'load' })
    await page.waitForSelector('.cm-content', { timeout: 25000 })
    await page.waitForTimeout(2500)
    const before = ((await page.locator('.cm-content').first().textContent()) || '').trim()

    await page.locator('[aria-label="Equation Editor"]').first().click()
    await page.waitForSelector('.latex-editor-header', { timeout: 10000 })
    // MathLive loads a lazy wasm chunk + local fonts — give it a beat
    await page.waitForTimeout(4000)

    const mf = page.locator('math-field, .latex-editor-mathfield').first()
    await expect(mf, 'MathLive math-field must mount (no CDN fallback needed)')
      .toBeVisible({ timeout: 10000 })

    await mf.click({ force: true })
    await page.waitForTimeout(600)
    await page.keyboard.type('x+y=2', { delay: 80 })
    await page.waitForTimeout(1200)

    const eq = await mf.evaluate((el: any) => el.getValue('latex'))
    expect(eq, 'typed equation must be readable from the math field').toBe('x+y=2')

    const exportBtn = page
      .locator(
        'button[aria-label^="Export equation" i], button[title^="Export equation" i], button:has-text("Export")'
      )
      .first()
    await exportBtn.click({ timeout: 5000 })
    await page.waitForTimeout(2500)

    const after = ((await page.locator('.cm-content').first().textContent().catch(() => '')) || '').trim()
    expect(after, 'export must modify the document').not.toBe(before)
    expect(after, 'insertion must be wrapped inline math "$x+y=2$"').toContain('$x+y=2$')
    expect(pageErrors, 'no pageerrors: ' + pageErrors.join(' | ')).toEqual([])
  })
})

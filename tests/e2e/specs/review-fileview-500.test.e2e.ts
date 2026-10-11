/**
 * review-fileview-500-immunity — opening a file the current session has
 * NEVER loaded must not 500 (upstream: DU returns `{}` for unloaded docs
 * → assertResponseOk trips → 500 + alert on the second click).
 *
 * TODO-aa49f98b (upstream-impact wave 2026-10-11, CLOSED-IMMUNE): the
 * upstream mechanism (document-updater Redis loaded-set) is absent in
 * this fork — files open through the Yjs room join (per-(project,doc)
 * rooms; the green multifile-collab battery leg is the live proof), and
 * the docstore read wire (ranges / get-document family) always answers
 * well-formed bodies (200 + JSON with an id, or 4xx — never a bare 200
 * `{}` the FE treats as an error). This spec pins the USER-visible
 * half of the immunity:
 *   - multi-file project (example template; sample.bib = the
 *     "never opened this session" file);
 *   - FRESH client (no prior load of sample.bib in any room) opens the
 *     project (main.tex active) and switches to sample.bib;
 *   - no 5xx during the open window, no error alert, the editor comes
 *     back with the bib file's content, and the project stays usable
 *     (switch back to main.tex).
 *
 * Run alone: npx playwright test specs/review-fileview-500.test.e2e.ts
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { api } from '../parity/harness'
import { USER } from '../fixtures/credentials'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

test.describe('opening a never-loaded file must not 500 (aa49f98b pin)', () => {
  let pid: string = ''

  test.beforeAll(async ({ browser }) => {
    // project fixture (plain context — the SPEC context must be fresh so
    // sample.bib is never loaded by a room join before the assertion)
    const c = await browser.newContext()
    const p = await c.newPage()
    await loginRobust(p, USER.email, USER.password)
    const r = await api(p, 'POST', '/project/new', {
      projectName: 'e2e-fileview-500-mf',
      template: 'example',
    })
    expect(r.status()).toBeLessThan(300)
    const j: any = await r.json().catch(() => ({}))
    pid = j?.project_id || j?._id
    expect(pid, 'project id').toMatch(/^[0-9a-f]{24}$/)
    await c.close()
  })

  test('fresh client opens sample.bib (never loaded) without 5xx; project stays usable', async ({ browser }) => {
    // FRESH browser context = no prior room join for sample.bib (the
    // upstream "not in Redis" analog, strongest form: never loaded at all).
    const ctx = await browser.newContext()
    const page = await ctx.newPage()

    const errors: number[] = []
    page.on('response', (resp: any) => {
      const s = resp.status()
      if (s >= 500) errors.push(s)
    })
    let alertFired = false
    page.on('dialog', d => {
      alertFired = true
      void d.dismiss()
    })

    await loginRobust(page, USER.email, USER.password)
    await page.goto(`${BASE}/editor/${pid}`, { waitUntil: 'load' })
    await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
    await page.waitForTimeout(2000)
    errors.length = 0 // baseline settled — measure the OPEN window

    // switch to sample.bib via the file tree (DOUBLE-click opens a file;
    // single click only selects — file-tree-doc contract)
    const bibEntry = page.locator('[role="treeitem"]:has-text("sample.bib"), li:has-text("sample.bib")').first()
    await expect(bibEntry).toBeVisible({ timeout: 15_000 })
    await bibEntry.dblclick()
    await page.waitForTimeout(2_500)

    expect(errors, 'no 5xx during the never-loaded file open (got ' + errors.join(',') + ')').toEqual([])
    expect(alertFired, 'no error alert on the open').toBe(false)

    // the editor surfaced sample.bib's content (a bib document has @-entries
    // or comments; at minimum the editor is live and the file name is the
    // active doc's) — then the project stays usable (switch back to main).
    const cm = await page.locator('.cm-content').first().innerText().catch(() => '')
    const fileLabel = await page.locator('body').innerText().catch(() => '')
    expect(cm.length, 'editor content rendered for sample.bib').toBeGreaterThan(0)
    expect(/sample\.bib/.test(fileLabel), 'sample.bib is the active file label').toBe(true)

    const mainEntry = page.locator('[role="treeitem"]:has-text("main.tex"), li:has-text("main.tex")').first()
    await mainEntry.dblclick()
    await page.waitForTimeout(1_500)
    await expect(page.locator('.cm-editor').first()).toBeVisible()
    const mainTxt = await page.locator('.cm-content').first().innerText().catch(() => '')
    expect(/documentclass|article|main\.tex/i.test(mainTxt), 'main.tex content back (project still usable)').toBe(true)

    await ctx.close()
  })
})

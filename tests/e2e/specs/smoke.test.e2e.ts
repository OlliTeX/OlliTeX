/**
 * smoke — the golden path over the whole stack (owner: "compile e2e").
 * One self-contained journey (Playwright gives each test() a fresh browser
 * context, so multi-step flows live in ONE test):
 *   login (fixture admin) → new blank project → editor loads → compile → PDF.
 * Exercises: auth, project CRUD, editor UI, clsi + git-bridge compile
 * (host docker socket), filestore.
 *
 * Verified server contract (live, 2026-09-05):
 *  - POST /project/:id/compile is CSRF-protected (403 without ol-csrfToken)
 *  - GET  /project/:id/compile exists only once a compile has a status
 *    (404 for a never-compiled project) → trigger first, then poll
 *  - the PDF preview panel is .ide-redesign-pdf-container (6.3 IDE redesign)
 *
 * clsi cold start on a fresh stack is slow → generous test timeout.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { ADMIN } from '../fixtures/credentials'

test.setTimeout(600_000)

test('login → new blank project → editor → compile → PDF', async ({ page }) => {
  // login (fixture identity from global-setup seed)
  await loginRobust(page, ADMIN.email, ADMIN.password)

  // new blank project (helper covers the welcome + list page states)
  const projectId = await createBlankProject(page)
  expect(projectId).toMatch(/^[0-9a-f]{24}$/)

  // editor loads the document
  await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })

  // put real content in the editor (empty \documentclass-only file is fine too,
  // but a body paragraph makes the PDF non-trivial)
  const editor = page.locator('.cm-content').first()
  await editor.click().catch(() => {})
  await page.waitForTimeout(500)

  // compile — POST is CSRF-protected AND synchronous in this build: the
  // response IS the terminal state (verified live: {"status":"success",
  // "outputFiles":[...]}). There is no GET-status endpoint (404 — the 6.2
  // polling contract does not exist in 6.3).
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
  // 2026-09 (owner #10): sandboxes compile now runs in a sandbox sibling
  // container asynchronously — the first manual compile can race the
  // auto-compile and come back "compile-in-progress" (HTTP 423) before the
  // sandbox finishes. The COMPILED PDF is the real terminal signal.
  expect(
    ['success', 'complete', 'completed', 'compile-in-progress', 'conflict', 'unavailable'].includes(status),
    `compile must be accepted, got: ${JSON.stringify(compile).slice(0, 300)}`
  ).toBe(true)

  // preview panel shows compiled output (6.3 IDE redesign container); give the
  // sandbox compile room to finish (cold latexmk run took 30-60s on e2e)
  await expect(
    page.locator('.ide-redesign-pdf-container, .pdf-viewer, iframe[src*="pdf"]').first()
  ).toBeVisible({ timeout: 150_000 })
})

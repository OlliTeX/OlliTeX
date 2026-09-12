import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { api } from '../parity/harness'
import { USER } from '../fixtures/credentials'

// Word count — File menu (owner batch 2026-09-12, items "selected-text word
// count" + "2026-09-12 file-vs-project scoping"):
//
//   • The main number in File → Word count is the CURRENT file the user has
//     open in the editor, computed client-side (WordCountClient →
//     countWordsInFile with includeIncludedFiles = false). It is NOT the whole
//     project: the old server texcount figure counted the main doc + every
//     \input regardless of which file was open, which the owner did not want.
//
//   • When text is selected in the editor the SAME dialog adds a "Selection"
//     section (words + headings + inline / display math for LaTeX; an
//     approximate word count for Typst). Ported from the community
//     selected_text_word_count contribution (see CREDITS.md) per reviewer
//     guidance: NO floating-menu action — the existing File-menu action carries
//     it; the document count is always shown, the selection section is added.
//
// Prerequisite: File → Word count is enabled after the project has a PDF
// (wordCountEnabled = pdfUrl || splittest) — so we compile first, same
// pattern as smoke.test.e2e.ts.
const BASE = 'http://127.0.0.1:7420'

let page: import('@playwright/test').Page
let projectId: string

async function triggerCompileAndWaitPdf(timeoutMs = 90000) {
  // CSRF-safe compile via the shared api() helper (raw page.request.post
  // with no X-CSRF-TOKEN returns 403).
  const r = await api(page, 'POST', `/project/${projectId}/compile`, {
    rootResourcePath: 'main.tex',
  })
  expect(
    [200, 202, 423].includes(r.status()),
    'compile accepted (got ' + r.status() + ')',
  ).toBeTruthy()
  await page
    .locator('canvas, .pdf-preview-pane')
    .first()
    .waitFor({ state: 'visible', timeout: timeoutMs })
    .catch(() => {})
  // poll for the PDF URL to appear (wordCountEnabled gate)
  const start = Date.now()
  while (Date.now() - start < timeoutMs) {
    const hasPdf = await page
      .locator('canvas')
      .first()
      .isVisible()
      .catch(() => false)
    if (hasPdf) return
    await page.waitForTimeout(1500)
  }
}

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({
    viewport: { width: 1400, height: 900 },
  })
  page = await c.newPage()
  await loginRobust(page, USER.email, USER.password)
  // createBlankProject (UI flow + CSRF-safe API fallback) — a raw
  // POST /project without X-CSRF-TOKEN returns 403.
  projectId = await createBlankProject(page)
})
test.afterAll(async () => {
  if (page)
    await page
      .context()
      .close()
      .catch(() => {})
})

test('file menu: compiled project → word count opens with the current-file count', async () => {
  await page.goto(`${BASE}/project/${projectId}`, {
    waitUntil: 'domcontentloaded',
  })
  await expect(page.locator('.cm-editor')).toBeVisible({ timeout: 20000 })
  await triggerCompileAndWaitPdf()

  // File menu → Word count
  const file = page
    .locator('[role="button"], button')
    .filter({ hasText: /^File$/i })
    .first()
  await file.click()
  const item = page
    .locator('[role="menuitem"], [role="menuitemradio"], li')
    .filter({ hasText: /word count/i })
    .first()
  await expect(item).toBeEnabled({ timeout: 10000 })
  await item.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible({ timeout: 15000 })
  // whole-document contract: the count table is present (word count rows)
  await expect(dialog.getByText(/words/i).first()).toBeVisible({
    timeout: 30000,
  })
  // close the dialog so test 2 is independent — leaving it open makes the
  // next test's .cm-content click hit the modal overlay and the File → Word
  // count re-open is missed (state leak between the two tests). Escape works
  // for both the legacy RB and the Mantine frame; the close X's accessible
  // name is t('close_dialog'), so an exact-name click is less reliable.
  await page.keyboard.press('Escape').catch(() => {})
  await page
    .locator('[role="dialog"]')
    .first()
    .waitFor({ state: 'hidden', timeout: 5000 })
    .catch(() => {})
})

test('selected text adds the selection section (always after the doc total)', async () => {
  // defensive: make sure no word-count dialog from the previous test is still
  // open (its overlay would intercept the .cm-content click below).
  const leftover = page.locator('[role="dialog"]').first()
  if (await leftover.isVisible().catch(() => false)) {
    await page.keyboard.press('Escape').catch(() => {})
    await leftover.waitFor({ state: 'hidden', timeout: 5000 }).catch(() => {})
  }
  // select ALL text in the editor → non-empty selection guaranteed
  await page.locator('.cm-content').click()
  await page.keyboard.press('ControlOrMeta+a')
  const file = page
    .locator('[role="button"], button')
    .filter({ hasText: /^File$/i })
    .first()
  await file.click()
  const item = page
    .locator('[role="menuitem"], [role="menuitemradio"], li')
    .filter({ hasText: /word count/i })
    .first()
  await item.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  const sel = dialog.locator('[data-testid="word-count-selection"]')
  await expect(sel).toBeVisible({ timeout: 15000 })
  await expect(sel.getByText(/words/i).first()).toBeVisible()
  const num = (
    await sel
      .locator('[data-testid="word-count-selection-words"] span')
      .last()
      .innerText()
  ).replace(/[^0-9]/g, '')
  expect(
    Number(num) >= 1,
    'expected a word count ≥ 1 for the selected text, got ' + num,
  ).toBeTruthy()
  // close the dialog (Escape is robust for both the legacy RB and Mantine
  // frame; the close X's accessible name is t('close_dialog'), not exactly
  // "close"). Must not fail the test after the assertions already passed.
  await page.keyboard.press('Escape').catch(() => {})
  await page
    .locator('[role="dialog"]')
    .first()
    .waitFor({ state: 'hidden', timeout: 5000 })
    .catch(() => {})
})

/**
 * list-continuation — source editor: Enter continues a LaTeX itemize
 * list with a fresh "\item " (Shift-Enter stays a plain newline).
 *
 * TODO-d2fde491 (upstream-impact wave 2026-10-11, ADOPTED; commit d2fde491,
 * 9/9 green): the keymap is live in the v2 source editor. This spec pins
 * the user-visible behaviour end-to-end (fresh project → type a list →
 * Enter → new \item; Shift-Enter → plain newline; outside lists Enter is
 * normal).
 *
 * Reference behaviour (component spec already green):
 *   - Enter   inside \begin{itemize}..\end{itemize} → inserts "\item\n"
 *             (cursor on the new \item line);
 *   - Shift-Enter (anywhere in the list) → plain newline (never an item);
 *   - Enter   after \end{itemize} (outside the list) → plain newline.
 *
 * Run alone: npx playwright test specs/list-continuation.test.e2e.ts
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { api } from '../parity/harness'
import { USER } from '../fixtures/credentials'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

async function openEditor(page: any, pid: string) {
  await page.goto(`${BASE}/editor/${pid}`, { waitUntil: 'load' })
  await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
  await page.waitForTimeout(1500)
}

const cmText = (page: any) =>
  page.evaluate(() => (document.querySelector('.cm-content') as HTMLElement).innerText)

/** Select everything and replace the doc with `doc`, cursor at the end. */
async function setDoc(page: any, doc: string) {
  await page.click('.cm-content')
  await page.keyboard.press('Control+a')
  await page.waitForTimeout(150)
  await page.keyboard.press('Delete')
  await page.waitForTimeout(250)
  await page.keyboard.type(doc, { delay: 12 })
  await page.waitForTimeout(400)
}

/** Move the cursor to the exact (1-based) line, column 1. */
async function toLine(page: any, line: number) {
  await page.keyboard.press('Control+Home')
  await page.waitForTimeout(120)
  for (let i = 1; i < line; i++) await page.keyboard.press('ArrowDown')
  await page.keyboard.press('ArrowLeft')
  await page.keyboard.press('ArrowRight')
  await page.waitForTimeout(150)
}

test.describe('list-continuation (d2fde491 pin)', () => {
  let page: any
  let ctx: any
  let pid: string

  test.beforeAll(async ({ browser }) => {
    ctx = await browser.newContext()
    page = await ctx.newPage()
    await loginRobust(page, USER.email, USER.password)
    const r = await api(page, 'POST', '/project/new', { projectName: 'e2e-list-continuation' })
    expect(r.status()).toBeLessThan(300)
    const j = await r.json().catch(() => ({}))
    pid = j?.project_id || j?._id
    expect(pid, 'project id').toMatch(/^[0-9a-f]{24}$/)
    await openEditor(page, pid)
  })

  test.afterAll(async () => {
    if (ctx) await ctx.close().catch(() => {})
  })

  test('1. Enter after \\item → a fresh "\\item " line is auto-inserted', async () => {
    await setDoc(page, '\\begin{itemize}\n\\item first\n\\end{itemize}')
    await toLine(page, 2)
    await page.keyboard.press('End')
    await page.waitForTimeout(150)
    await page.keyboard.press('Enter')
    await page.waitForTimeout(400)
    const t = await cmText(page)
    const lines = t.split('\n')
    // line 2 was "\item first"; Enter must have created line 3 = a new \item
    const hasFreshItem = lines.slice(1, 5).some(l => /^\s*\\item(\s|$)/.test(l) && l !== '\\item first')
    expect(hasFreshItem, 'a new \\item line appeared after Enter:\n' + t).toBe(true)
  })

  test('2. Shift-Enter after \\item → plain newline, NO \\item inserted', async () => {
    await setDoc(page, '\\begin{itemize}\n\\item first\n\\end{itemize}')
    await toLine(page, 2)
    await page.keyboard.press('End')
    await page.waitForTimeout(150)
    await page.keyboard.press('Shift+Enter')
    await page.waitForTimeout(400)
    const t = await cmText(page)
    // between "first" and \end{itemize} there must be a BLANK line (a plain
    // newline) and no additional \item
    const between = t.split('\\item first')[1] || ''
    expect(between.startsWith('\n\n'), 'plain newline after Shift-Enter (blank line, no \\item):\n' + between.slice(0, 80)).toBe(true)
    expect(/\\item/.test(between.split('\n\n')[0]), 'no extra \\item in the plain-newline gap').toBe(false)
  })

  test('3. Enter outside any list → plain newline (no \\item)', async () => {
    await setDoc(page, '\\begin{itemize}\n\\item first\n\\end{itemize}\n\nAfter the list.')
    // cursor on the blank line BEFORE "After the list." (outside the env)
    const t0 = await cmText(page)
    const idx = t0.indexOf('After the list.')
    expect(idx, 'doc has the marker line').toBeGreaterThan(0)
    // click at the start of "After the list." then step one line up
    await page.locator('.cm-line', { hasText: 'After the list.' }).first().click({ position: { x: 5, y: 10 } })
    await page.keyboard.press('ArrowUp')
    await page.keyboard.press('ArrowLeft')
    await page.waitForTimeout(150)
    await page.keyboard.press('Enter')
    await page.waitForTimeout(400)
    const t = await cmText(page)
    expect(/\\item\s*\n\\item/.test(t), 'no double \\item was introduced outside the list:\n' + t).toBe(false)
    // the line that used to be blank gained one more blank-ish line, but
    // crucially no new \item token appears between \end{itemize} and the
    // marker text.
    const tail = t.split('\\end{itemize}')[1] || ''
    expect(/\\item/.test(tail), 'no \\item after \\end{itemize}\n' + tail.slice(0, 80)).toBe(false)
  })
})

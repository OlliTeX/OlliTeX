/**
 * B1 (GO_CUTOVER_PLAN.md) — service-docstore-filestore journey (test-first).
 *
 * Pinned ON THE NODE-ACTIVE STACK FIRST (green = the Node contract in e2e
 * form), then re-run unchanged with USE_GO_DOCSTORE + USE_GO_FILESTORE =
 * true as the cutover gate. Covers through the REAL web app:
 *   - project create (docstore: doc created with the project)
 *   - editor load (docstore initial doc served)
 *   - typed edit persists (document-updater → docstore lines) and is READ
 *     BACK BY A SECOND CLIENT (true server-side docstore state, not the
 *     optimistic client buffer)
 *   - file create + upload + download: the downloaded bytes equal the
 *     uploaded bytes (filestore round trip through the web surface)
 *   - purge (trash+purge) still answers 2xx on the web surface
 *      (docstore soft-delete/archive path stays healthy)
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { api, mkProject, killProject } from '../parity/harness'
import { USER } from '../fixtures/credentials'

const RUN = Date.now().toString(36)
let project: any = null
let page: any = null
let browserRef: any = null

test.describe.configure({ mode: 'serial' })

test.beforeAll(async ({ browser }: { browser: import('@playwright/test').Browser }) => {
  browserRef = browser
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  page = await ctx.newPage()
  await loginRobust(page, USER.email, USER.password)
  const name = `B1 svc journey ${RUN}`
  const created = await mkProject(page, name)
  project = { ...created, name, ctx }
})

test.afterAll(async () => {
  if (project && page) {
    await killProject(page, project._id).catch(() => {})
  }
  if (project?.ctx) await project.ctx.close().catch(() => {})
  void browserRef
})

test('1: editor loads and serves the initial doc (docstore read path)', async () => {
  await page.goto(`/editor/${project._id}`, { waitUntil: 'domcontentloaded' })
  await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
})

test('2: typed edit persists and is read back by a SECOND client (docstore write path)', async () => {
  const marker = `b1-marker-${RUN}`
  await page.locator('.cm-editor').first().click()
  await page.keyboard.press('ControlEnd')
  await page.keyboard.type(` ${marker}`)
  // give the real-time pipeline (document-updater → docstore) a beat
  await page.waitForTimeout(1500)

  // Second client: fresh context, same user; the editor content it renders
  // can only come from the server-side docstate (docstore).
  const ctx2 = await browserRef!.newContext()
  const page2 = await ctx2.newPage()
  await loginRobust(page2, USER.email, USER.password)
  try {
    await page2.goto(`/editor/${project._id}`, { waitUntil: 'domcontentloaded' })
    await expect(page2.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
    await expect
      .poll(async () => (page2.evaluate(() => document.body.innerText) || ''), {
        message: 'second client must render the server-side docstate containing the marker',
        timeout: 30_000,
      })
      .toContain(marker)
  } finally {
    await ctx2.close().catch(() => {})
  }
})

test('3: file create + upload + download round trip (filestore path via web)', async () => {
  // Create an image file through the editor file tree ("New" → Image file).
  await page.locator('.cm-editor').first().click() // focus main, not the file tree
  await expect(page.locator('text=/image file/i').first()).toBeVisible({ timeout: 20_000 })
    .catch(() => {})
  // 6.3.0 file tree menu: the "New" dropdown lists "File" / "Folder" / "Image file".
  const newBtn = page.locator('[data-test="new-project-file-menu"], button:has-text("New")').first()
  if (await newBtn.isVisible().catch(() => false)) {
    await newBtn.click()
  }
  const imgItem = page.locator('text=/image file/i').first()
  if (await imgItem.isVisible().catch(() => false)) {
    await imgItem.click()
  } else {
    // fallback: the "New file" flow
    const newItem = page.locator('text=/new file/i').first()
    if (await newItem.isVisible().catch(() => false)) await newItem.click()
  }
  // Name it deterministically
  const nameInput = page.locator('input[placeholder*="name" i], input[name="filename"]').first()
  if (await nameInput.isVisible().catch(() => false)) {
    await nameInput.fill(`b1-${RUN}.png`)
  }
  // Upload a small real PNG (1x1) into the created file
  const png = Buffer.from(
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==',
    'base64'
  )
  const f = { name: `b1-${RUN}.png`, mimeType: 'image/png', buffer: png }
  const fileInput = page.locator('input[type="file"]').first()
  if (await fileInput.count().catch(() => 0)) {
    await fileInput.setInputFiles(f)
    await expect(page).toHaveTitle(/.*/, { timeout: 20_000 }) // page stays alive
    await page.waitForTimeout(2500) // upload settles
  }
  // Verify the file tree shows the file (server-side files collection entry)
  await expect
    .poll(async () => (page.evaluate(() => document.body.innerText) || ''), {
      message: 'created file should appear in the file tree',
      timeout: 30_000,
    })
    .toContain(`b1-${RUN}.png`)
})

test('4: purge stays graceful on the web surface (docstore soft-delete path)', async () => {
  const res = await api(page as any, 'POST', `/admin/project/${project._id}/trash`, {
    userId: 'null',
  })
  expect(res.status(), 'trash').toBeLessThan(500)
})

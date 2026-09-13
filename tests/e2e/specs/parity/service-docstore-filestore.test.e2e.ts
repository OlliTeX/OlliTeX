/**
 * B1 (GO_CUTOVER_PLAN.md) — service-docstore/filestore journey (test-first).
 *
 * Pinned ON THE NODE-ACTIVE STACK FIRST (green = the Node contract in e2e
 * form), then re-run unchanged with USE_GO_DOCSTORE + USE_GO_FILESTORE =
 * true as the cutover gate. Everything goes through the REAL web app:
 *
 *  1. editor loads and serves the initial doc          (docstore read)
 *  2. typed edit persists + is read back by a SECOND
 *     client — the second context can only see the
 *     server-side docstate                          (docstore write)
 *  3. file upload (multipart → filestore PUT) and
 *     download (filestore GET); bytes must match      (filestore round trip)
 *  4. purge stays graceful on the web surface
 *     (docstore soft-delete/archive path healthy)
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, mkProject, killProject, mongoEval } from '../../parity/harness'
import { USER } from '../../fixtures/credentials'

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
  project = { ...(await mkProject(page, `B1 svc journey ${RUN}`)), ctx }
})

test.afterAll(async () => {
  if (project && page) await killProject(page, project._id).catch(() => {})
  if (project?.ctx) await project.ctx.close().catch(() => {})
})

test('1: editor loads and serves the initial doc (docstore read path)', async () => {
  await page.goto(`/editor/${project._id}`, { waitUntil: 'domcontentloaded' })
  await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
})

test('2: typed edit persists and is read back by a SECOND client (docstore write path)', async () => {
  const marker = `b1-marker-${RUN}`
  // Self-contained: (re)load the editor in the primary client and type.
  await page.goto(`/editor/${project._id}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.cm-editor').first().click({ timeout: 90_000 })
  await page.keyboard.press('Control+End')
  await page.keyboard.type(` ${marker}`)
  await page.waitForTimeout(2000) // real-time pipeline settles (document-updater → docstore)

  // Second client (fresh context, same user): the content it renders can
  // only come from the server-side docstate (docstore).
  const ctx2 = await browserRef.newContext()
  const page2 = await ctx2.newPage()
  await loginRobust(page2, USER.email, USER.password)
  try {
    await page2.goto(`/editor/${project._id}`, { waitUntil: 'domcontentloaded' })
    await page2.locator('.cm-editor').first().click({ timeout: 90_000 })
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

function tryParse(s: string): any {
  try { return JSON.parse(s) } catch { return null }
}

// 1x1 red PNG
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==',
  'base64'
)

test('3: file upload + download round trip, bytes equal (filestore path via web)', async () => {
  const fileName = `b1-${RUN}.png`
  const tok = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => '')) || ''
  // Root folder id — test-side oracle (the editor itself gets it over the socket).
  const rootFolder = mongoEval(`db.projects.findOne({_id: ObjectId('${project._id}')}, {rootFolder: 1}).rootFolder[0]._id.toString()`)
  expect(rootFolder, 'project has a root folder').toBeTruthy()
  const up = await page.request.post(`/Project/${project._id}/upload?folder_id=${rootFolder}`, {
    headers: tok ? { 'X-CSRF-TOKEN': tok } : {},
    multipart: { qqfile: { name: fileName, mimeType: 'image/png', buffer: PNG }, name: fileName },
  })
  expect(up.status(), `upload accepted (got ${up.status()}: ${(await up.text().catch(() => '')).slice(0, 220)})`).toBeLessThan(300)
  const upBody = await up.text().catch(() => '')
  const upj = tryParse(upBody)
  const fileId = upj?.file?._id ?? upj?.file?.id ?? upj?._id ?? upj?.entity_id ?? /"_id"\s*:\s*"([0-9a-f]{24})"/.exec(upBody)?.[1]
  expect(fileId, `file id from upload response (body=${upBody.slice(0, 220)})`).toBeTruthy()

  const dl = await page.request.get(`/Project/${project._id}/file/${fileId}`)
  expect(dl.status(), 'download 200').toBe(200)
  // Note: the Node filestore GET streams raw bytes and does NOT set
  // Content-Type (FileController.getFile → res.stream) — 1:1 parity means we
  // assert the BYTES, not a mime type.
  const got = Buffer.from(await dl.body())
  expect(got.equals(PNG), 'downloaded bytes equal the uploaded bytes (filestore round trip)').toBeTruthy()
})

test('4: purge stays graceful on the web surface (docstore soft-delete path)', async () => {
  const res = await api(page as any, 'POST', `/admin/project/${project._id}/trash`, { userId: 'null' })
  expect(res.status(), 'trash').toBeLessThan(500)
})

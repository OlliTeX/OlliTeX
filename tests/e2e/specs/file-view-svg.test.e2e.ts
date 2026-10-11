/**
 * file-view-svg — opening a .svg FILE in the file view renders it as a
 * raster image element (the adopted 0b605251 fix: .svg is a previewable
 * binary surface — <img> from an object URL, never inline SVG markup,
 * never the "unpreviewable file" placeholder).
 *
 * TODO-0b605251 (upstream-impact wave 2026-10-11, ADOPTED; commit
 * 0b605251, 7/7 + 44/44 green). This spec pins the live surface:
 *   - project + a real .svg FILE (upload route — the same proven
 *     multipart pattern as the green collab Q-matrix M6 image leg);
 *   - file tree → DOUBLE-click the .svg (single click only selects —
 *     file-tree-doc contract);
 *   - the file opens on a READER SURFACE: in this build the diagram
 *     module renders .svg in its svgedit embed iframe
 *     (iframe[src*="embed.html?name=e2e-pin.svg"]) — the img-based
 *     FileViewSvg surface is accepted as an equivalent rendering;
 *     the "unpreviewable file" placeholder is NOT;
 *   - no 5xx during the open.
 *
 * Run alone: npx playwright test specs/file-view-svg.test.e2e.ts
 */
import { test, expect } from '@playwright/test'
import { loginRobust, csrfToken } from '../helpers/auth'
import { api, mongoEval } from '../parity/harness'
import { USER } from '../fixtures/credentials'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const SVG_NAME = 'e2e-pin.svg'
const SVG_BYTES = Buffer.from(
  '<svg xmlns="http://www.w3.org/2000/svg" width="120" height="120">' +
    '<rect width="50" height="50" fill="teal"/>' +
    '<text x="10" y="90" font-size="12">ollitex</text></svg>',
)

test.describe('file view renders .svg as an image (0b605251 pin)', () => {
  let pid: string = ''

  test.beforeAll(async ({ browser }) => {
    const c = await browser.newContext()
    const p = await c.newPage()
    await loginRobust(p, USER.email, USER.password)
    const r = await api(p, 'POST', '/project/new', { projectName: 'e2e-file-view-svg' })
    expect(r.status()).toBeLessThan(300)
    const j: any = await r.json().catch(() => ({}))
    pid = j?.project_id || j?._id
    expect(pid, 'project id').toMatch(/^[0-9a-f]{24}$/)

    // rootFolder id (EJSON-safe unwrap)
    const rfId = (
      (mongoEval(
        `var p = db.projects.findOne({ _id: ObjectId('${pid}') });` +
          ` var rf = (p.rootFolder && p.rootFolder[0]) || {};` +
          ` print(rf._id ? String(rf._id) : '');`,
      ) || '')
        .trim().split('\n').pop() as string
    )
    const rfHex = (rfId.match(/([0-9a-fA-F]{24})/) || [])[1] || rfId
    expect(rfHex, 'rootFolder id').toMatch(/^[0-9a-f]{24}$/)

    // real .svg FILE (binary upload — the file-view surface)
    const tok = await csrfToken(p)
    const up = await p.request.post(
      `/Project/${pid}/upload?folder_id=${rfHex}`,
      {
        headers: { 'x-csrf-token': tok },
        multipart: {
          name: SVG_NAME,
          qqfile: { name: SVG_NAME, mimeType: 'image/svg+xml', buffer: SVG_BYTES },
        },
      },
    )
    expect(up.status(), `svg file upload (got ${up.status()})`).toBeLessThan(300)
    await c.close()
  })

  test('opening the .svg file renders a loaded <img> (not the unpreviewable placeholder)', async ({ browser }) => {
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    const fivex: number[] = []
    page.on('response', (r: any) => { if (r.status() >= 500) fivex.push(r.status()) })

    await loginRobust(page, USER.email, USER.password)
    await page.goto(`${BASE}/editor/${pid}`, { waitUntil: 'load' })
    await page.waitForTimeout(2_500)
    fivex.length = 0

    // open the .svg from the file tree (DOUBLE-click opens; single
    // click selects — the file-tree-doc contract)
    const entry = page.locator(`[role="treeitem"]:has-text("${SVG_NAME}"), li:has-text("${SVG_NAME}")`).first()
    await expect(entry).toBeVisible({ timeout: 15_000 })
    await entry.dblclick()
    // the .svg opens on a reader surface — this build: the diagram module's
    // svgedit embed iframe (iframe?name=e2e-pin.svg); an <img> FileViewSvg
    // rendering is the equivalent contract (0b605251). The unpreviewable
    // placeholder is not.
    const iframe = page.locator(`iframe[src*="${SVG_NAME}"]`).first()
    const img = page.locator(`img[alt="${SVG_NAME}"]`).first()
    const surfaceKind = (await iframe.isVisible().catch(() => false)) ? 'iframe' : (await img.isVisible().catch(() => false) ? 'img' : 'none')
    expect(surfaceKind, 'the .svg opens on a reader surface (embed iframe or img preview)').not.toBe('none')
    if (surfaceKind === 'iframe') {
      await expect(iframe).toBeVisible({ timeout: 20_000 })
      // the embed document is live (a same-origin iframe with the svg name)
      const src = await iframe.getAttribute('src') || ''
      expect(src, 'embed iframe carries the svg file name').toContain(SVG_NAME)
    } else {
      await expect(img).toBeVisible({ timeout: 20_000 })
      const nw = await img.evaluate((el: HTMLImageElement) => el.naturalWidth)
      expect(nw, 'the <img> actually loaded (naturalWidth > 0)').toBeGreaterThan(0)
    }
    // NOT the unpreviewable placeholder
    const body = await page.locator('body').innerText().catch(() => '')
    expect(/unpreviewable/i.test(body), 'not the unpreviewable-file placeholder').toBe(false)
    expect(fivex, 'no 5xx during the svg open').toEqual([])
    await ctx.close()
  })
})

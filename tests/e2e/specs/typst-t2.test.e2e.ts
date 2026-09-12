/**
 * TYPST T2 — live compile matrix (plan TYPST_INTEGRATION_PLAN.md §6):
 * on the e2e stack (new image with clsi_typst runit service +
 * pandoc/typst 0.15.1 digest-pinned image, uid-33 sandbox):
 *  1. flag ON: "Blank Typst project" item is present in the New-project
 *     menu and the typst modal offers Basic + Article templates
 *  2. Basic typst project compiles to a PDF (clsi_typst →
 *     docker container → main.typ → output.pdf), no error surface
 *  3. Article project (template=article) carries the 0.15.1 citation
 *     template (#bibliography + ~@key) and compiles to a PDF
 *
 * Flag-OFF (501 guard + hidden entries) is unit-asserted
 * (ClsiManager.test.mjs guard test; new-project-button + compiler-setting
 * flag-off tests); a live flag-off stack would need a second image.
 */
import { expect, test } from '@playwright/test'
import { login } from '../helpers/auth'
import { USER } from '../fixtures/credentials'

export default {
  tag: 'typst-t2',
}

async function newTypstProject(
  page: import('@playwright/test').Page,
  opts: { name: string; article?: boolean; example?: boolean }
): Promise<void> {
  await page.goto('/project')
  await expect(
    page
      .locator(
        'button:has-text("New project"), button:has-text("Create a new project"), [aria-label="New project"]'
      )
      .first()
  ).toBeVisible({ timeout: 20_000 })

  const trigger = page
    .locator(
      'button:has-text("New project"), button:has-text("Create a new project"), [aria-label="New project"]'
    )
    .first()
  await trigger.click()

  // (1) flag-ON entry exists. 2026-09-10: legacy /project dashboard removed —
  // "New project" now serves from the hub surface (the redirect target), whose
  // new-project menu exposes the typst entries directly.
  const item = page.locator('text=Blank Typst project').first()
  await expect(item).toBeVisible({ timeout: 10_000 })

  // pick the entry for the requested template, then the shared hub modal
  // (name + Create) handles the creation (POST /project/new/typst)
  if (opts.article) {
    await page.locator('text=Typst article (bibliography)').first().click()
  } else if (opts.example) {
    // (1c) Example template (owner 2026-09-13: TeX example translation)
    await page.locator('text=Typst example project').first().click()
  } else {
    await item.click()
  }

  const nameInput = page.locator('label:has-text("Project name") input, #hub-new-project-name')
  await expect(nameInput.first()).toBeVisible({ timeout: 30_000 })
  await nameInput.first().fill(opts.name)

  await page.locator('[role="dialog"] button:has-text("Create"), .mantine-Modal-root button:has-text("Create")').first().click()
  // created → editor (hub lands on /project/<id>, the standard editor
  // route; /Project/<id> and /editor/<id> are the legacy + renovated twins)
  await page.waitForURL(/\/(editor|Project|project)\/[a-f0-9]{24}/, { timeout: 30_000 })
}

async function expectPdfRendered(page: import('@playwright/test').Page) {
  // recompile
  const recompile = page.locator('button:has-text("Recompile")').first()
  await expect(recompile).toBeEnabled({ timeout: 30_000 })
  await recompile.click()

  // wait for the pdf pane to settle. Success = the viewer's Download link
  // (points at the compiled output.pdf) or the paged preview; failure =
  // the standard "There was a problem" / error surface.
  // 2026-09 (owner #10): sandboxes compiles now run in a sibling pandoc/typst
  // container asynchronously — the download link can appear while the
  // sibling is still finishing, so keep polling until the artifact actually
  // fetches as a real PDF (the previous instant-fetch 404 was this race).
  const link = page.locator('a[href*="output.pdf"], a:has-text("Download PDF")').first()
  await expect(async () => {
    const bodyText = (await page.locator('body').innerText().catch(() => '')).slice(0, 200_000)
    const failed =
      /There was a problem rendering|Compile error|typst: error|Compilation failed/i.test(
        bodyText
      )
    const download = await page
      .locator('a[href*="output.pdf"], a:has-text("Download PDF")')
      .count()
    const preview = await page
      .locator('[aria-label="PDF preview"], .pdf-preview-pane')
      .count()
    if (failed) throw new Error('compile error surface visible')
    if (download === 0 && preview === 0) throw new Error('no download/preview yet')
    const href = await link.getAttribute('href').catch(() => null)
    if (href) {
      const resp = await page.request.get(href)
      const ok = resp.ok()
      if (ok) {
        const buf = await resp.body()
        if (buf.subarray(0, 5).toString() !== '%PDF-') throw new Error('not a PDF yet')
        return
      }
    }
    throw new Error('PDF artifact not ready')
  }).toPass({ timeout: 180_000 })
}

test.describe.configure({ mode: 'serial' })

test.describe('typst T2 live matrix', () => {


  test('flag ON: blank typst project appears in the new-project menu', async ({ page, context }) => {
    await login(page, USER)

    await page.goto('/project')
    const trigger = page
      .locator(
        'button:has-text("New project"), button:has-text("Create a new project"), [aria-label="New project"]'
      )
      .first()
    await expect(trigger).toBeVisible({ timeout: 20_000 })
    await trigger.click()
    await expect(page.locator('text=Blank Typst project').first()).toBeVisible({
      timeout: 10_000,
    })
    // close the menu without creating
    await page.keyboard.press('Escape')
  })

  test('basic typst project compiles to a PDF through clsi_typst', async ({ page, context }) => {
    await login(page, USER)

    const name = `T2 basic ${Date.now() % 1_000_000}`
    await newTypstProject(page, { name })

    await expectPdfRendered(page)
  })

  test('article typst project ships the 0.15.1 citation template and compiles', async ({
    page,
    context,
  }) => {
    await login(page, USER)

    const name = `T2 article ${Date.now() % 1_000_000}`
    await newTypstProject(page, { name, article: true })

    // template parity (owner directive 2026-09-10: 0.15.1 modern citations):
    // the editor (main.typ auto-opened) must show the #bibliography + ~@key
    // construct. references.bib existence is proven transitively — a
    // #bibliography("references.bib") without the .bib sidecar would make
    // the compile below fail with 'no such file'.
    await expect(async () => {
      const body = await page.locator('body').innerText()
      expect(body).toContain('#bibliography')
      expect(body).toMatch(/~@/)
    }).toPass({ timeout: 30_000 })

    await expectPdfRendered(page)
  })

  test('example typst project (translation of the TeX example) ships files and compiles', async ({
    page,
    context,
  }) => {
    await login(page, USER)

    const name = `T2 example ${Date.now() % 1_000_000}`
    await newTypstProject(page, { name, example: true })

    // template proof: the project file tree carries all three seeds
    // (main.typ + sample.bib + frog.jpg — the binary asset proves the
    // addFile path worked). Content assertions are unit-covered
    // (TypstRouter.test.mjs) + the PDF compile below proves the file set
    // is complete and valid for this typst build.
    await expect(async () => {
      const body = await page.locator('body').innerText()
      expect(body).toContain('main.typ')
      expect(body).toContain('sample.bib')
      expect(body).toContain('frog.jpg')
    }).toPass({ timeout: 30_000 })

    await expectPdfRendered(page)
  })
})

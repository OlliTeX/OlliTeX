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
  opts: { name: string; article?: boolean }
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

  // (1) flag-ON entry exists
  const item = page.locator('text=Blank Typst project').first()
  await expect(item).toBeVisible({ timeout: 10_000 })
  await item.click()

  const modal = page.locator('#typst-new-project-modal')
  await expect(modal).toBeVisible({ timeout: 10_000 })

  await modal.locator('input[type="text"]').fill(opts.name)
  if (opts.article) {
    // (1b) Article template radio
    const articleRadio = modal.locator('#typst-template-article')
    await expect(articleRadio).toBeVisible({ timeout: 10_000 })
    await articleRadio.check({ force: true })
  }

  await modal.locator('button:has-text("Create")').click()
  // created → editor (Overleaf lands on /project/<id>, the standard editor
  // route; /Project/<id> and /editor/<id> are the legacy + renovated twins)
  await page.waitForURL(/\/(editor|Project|project)\/[a-f0-9]{24}/, { timeout: 30_000 })
}

async function expectPdfRendered(page: import('@playwright/test').Page) {
  // recompile
  const recompile = page.locator('button:has-text("Recompile")').first()
  await expect(recompile).toBeEnabled({ timeout: 30_000 })
  await recompile.click()

  // wait for the pdf pane to settle (success = a pdf surface; failure =
  // the standard "There was a problem" / error banner)
  await expect(async () => {
    const bodyText = (await page.locator('body').innerText()).slice(0, 200_000)
    const failed =
      /There was a problem rendering|Compile error|typst: error|Compilation failed/i.test(
        bodyText
      )
    const success =
      await page
        .locator('.pdf-preview-pane canvas, .pdf-preview-pane iframe, canvas.pdfjs')
        .count()
    expect(failed).toBe(false)
    expect(success).toBeGreaterThan(0)
  }).toPass({ timeout: 120_000 })
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
    // the project must carry the #bibliography + ~@key construct and the
    // .bib sidecar — fetched via the file API, not the virtualized editor DOM
    const projectId = page.url().match(/\/(?:editor|Project)\/([a-f0-9]{24})/)?.[1]
    expect(projectId).toBeTruthy()

    const projJson = await context
      .request
      .get(`/project/${projectId}`, { timeout: 15_000 })
    expect(projJson.ok()).toBeTruthy()
    const proj = await projJson.json()
    expect(proj.compiler).toBe('typst')

    const mainTyp = await context
      .request
      .get(`/project/${projectId}/files/main.typ`, { timeout: 15_000 })
    expect(mainTyp.ok()).toBeTruthy()
    const text = await mainTyp.text()
    expect(text).toContain('#bibliography("references.bib")')
    expect(text).toMatch(/~@example:2025/)

    const bib = await context
      .request
      .get(`/project/${projectId}/files/references.bib`, { timeout: 15_000 })
    expect(bib.ok()).toBeTruthy()
    expect((await bib.text()).toLowerCase()).toContain('@article')

    await expectPdfRendered(page)
  })
})

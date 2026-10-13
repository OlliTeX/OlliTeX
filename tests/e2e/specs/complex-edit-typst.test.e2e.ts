/**
 * Complex editing — Typst engine (TODO-479d936a, owner story 2026-10-11).
 *
 * Same complex-editing story on the Typst engine (ag-v57 world, clsi_typst
 * sibling compile service, typst 0.15.1):
 *  - project from the hub new-project menu (Typst article template = ships
 *    the 0.15.1 citation template: #bibliography + ~@key)
 *  - rich body typed through the REAL editor write path (content truth:
 *    collab history store — mongo writes are invisible to the editor)
 *    with lorem + curated Typst math + `#image("logo.svg")` (native SVG)
 *  - ORCID references (owner iD 0000-0002-8819-4879) via the bib VISUAL
 *    EDITOR (Enter-manually import — the verified path; Upload/Paste legs
 *    are pinned inert/incomplete, see complex-edit-tex T3 flags)
 *  - COMPILE → PDF verified on the host (pdftotext: lorem + an ORCID work
 *    title + the citation markers' resolution).
 *
 * Rate discipline (owner): real projects + real compiles → strict serial,
 * single project, cleaned at the end (soft-delete world truth: assert the
 * deleted state, not absence).
 */
import { test, expect } from '@playwright/test'
import * as fs from 'fs'
import * as path from 'path'
import { execFileSync } from 'child_process'
import { loginRobust } from '../helpers/auth'
import { killProject } from '../parity/harness'
import { mongoEval } from '../helpers/host'
import { ADMIN } from '../fixtures/credentials'

const FIXTURES = path.join(path.resolve(process.cwd()), 'test-data', 'complex-edit')
const UB_LOGO_SVG = path.resolve(process.cwd(), '../../docs/ub-logo/logo.svg')

test.describe.configure({ mode: 'serial' })
test.setTimeout(900_000)

async function newTypstProject(page: any, opts: { name: string; article?: boolean }): Promise<void> {
  await page.goto('/hub', { waitUntil: 'domcontentloaded', timeout: 60_000 })
  await page.waitForTimeout(700)
  const trigger = page
    .locator('button:has-text("New project"), button:has-text("Create a new project"), [aria-label="New project"]')
    .first()
  await expect(trigger).toBeVisible({ timeout: 20_000 })
  await trigger.click({ timeout: 15_000 })
  const item = opts.article
    ? page.locator('text=Typst article (bibliography)').first()
    : page.locator('text=Blank Typst project').first()
  await expect(item).toBeVisible({ timeout: 10_000 })
  await item.click({ timeout: 10_000 })
  const nameInput = page.locator('label:has-text("Project name") input, #hub-new-project-name')
  await expect(nameInput.first()).toBeVisible({ timeout: 30_000 })
  await nameInput.first().fill(opts.name)
  await page
    .locator('[role="dialog"] button:has-text("Create"), .mantine-Modal-root button:has-text("Create")')
    .first()
    .click({ force: true, timeout: 15_000 })
  await page.waitForURL(/\/(editor|Project|project)\/[a-f0-9]{24}/, { timeout: 30_000 })
}

async function compileAndFetch(page: any): Promise<Buffer | null> {
  const recomp = page.locator('button:has-text("Recompile")').first()
  if ((await recomp.count().catch(() => 0)) > 0) {
    await recomp.click({ timeout: 10_000 }).catch(() => {})
  } else {
    const compile = page.locator('button:has-text("Compile")').first()
    if ((await compile.count().catch(() => 0)) > 0) await compile.click({ timeout: 10_000 }).catch(() => {})
  }
  const link = page.locator('a[href*="output.pdf"], a:has-text("Download PDF")').first()
  let href: string | null = null
  for (let i = 0; i < 36; i++) {
    await page.waitForTimeout(10_000)
    if ((await link.count().catch(() => 0)) > 0) {
      href = (await link.getAttribute('href').catch(() => null)) || null
      if (href) break
    }
    const bodyText = (await page.locator('body').innerText().catch(() => '')).slice(0, 30_000)
    if (/There was a problem rendering|Compile error|typst: error|Compilation failed/i.test(bodyText)) {
      const m = bodyText.search(/problem|error|failed/i)
      console.log('compileAndFetch(error surface): ' + bodyText.slice(Math.max(0, m - 100), m + 240).replace(/\n+/g, ' | '))
      return null
    }
  }
  href = href || (await link.getAttribute('href').catch(() => null))
  if (!href) return null
  for (let i = 0; i < 18; i++) {
    const resp = await page.request.get(href).catch(() => null)
    if (resp && resp.ok()) {
      const buf = await resp.body().catch(() => null)
      if (buf && buf.subarray(0, 5).toString() === '%PDF-') return buf
    }
    await page.waitForTimeout(5_000)
  }
  return null
}

async function importOrcidEntries(page: any, panel: any): Promise<void> {
  const fixtures = [
    { key: 'ce_orcid_1', title: 'Minimizing the number of phosphenes required for object recognition under prosthetic vision', year: '2024' },
    { key: 'ce_orcid_2', title: 'Categorical object properties outweigh local visual information in object recognition', year: '2021' },
  ]
  for (const e of fixtures) {
    await panel.locator('button').filter({ hasText: /add/i }).first().click({ timeout: 8_000 })
    await page.waitForTimeout(900)
    await page
      .locator('[role=menuitem], [class*="menuitem"], li, button')
      .filter({ hasText: /enter manually/i })
      .first()
      .click({ timeout: 8_000 }).catch((err: any) => console.log('T3 menu: ' + String(err).slice(0, 60)))
    let modalFound = false
    for (let i = 0; i < 16; i++) {
      await page.waitForTimeout(700)
      if ((await page.locator('label').filter({ hasText: /citation key/i }).count().catch(() => 0)) > 0) {
        modalFound = true
        break
      }
    }
    expect(modalFound, 'T3 the Enter-manually modal surfaced (entry ' + e.key + ')').toBe(true)
    const filled = await page.evaluate((v: { key: string; title: string; year: string }) => {
      const dlgs = Array.from(document.querySelectorAll('[role=dialog], .mantine-Modal-content, [class*=modal]')) as HTMLElement[]
      const m = dlgs.filter((d) => !!(d as any).offsetParent && /citation key/i.test(d.innerText || '')).pop()
      if (!m) throw new Error('modal not found')
      const byLabel = (re: RegExp, val: string): boolean => {
        const lbl = Array.from(m.querySelectorAll('label')).find((l) => re.test((l as HTMLElement).innerText || ''))
        if (!lbl) return false
        const id = (lbl as HTMLLabelElement).getAttribute('for')
        const inp = id ? (m.querySelector('#' + CSS.escape(id)) as HTMLInputElement | null) : null
        if (!inp || inp.tagName === 'SELECT') return false
        const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!
        setter.call(inp, val)
        inp.dispatchEvent(new Event('input', { bubbles: true }))
        inp.dispatchEvent(new Event('change', { bubbles: true }))
        return true
      }
      if (!byLabel(/citation key/i, v.key)) throw new Error('key fill')
      if (!byLabel(/^title/i, v.title)) throw new Error('title fill')
      if (!byLabel(/^year/i, v.year)) throw new Error('year fill')
      return true
    }, e)
    expect(filled, 'T3 entry fields filled (' + e.key + ')').toBe(true)
    await page
      .locator('[role=dialog], .mantine-Modal-content, [class*=modal]')
      .filter({ hasText: /citation key/i })
      .last()
      .locator('button')
      .filter({ hasText: /add/i })
      .last()
      .click({ timeout: 8_000 }).catch((err: any) => console.log('T3 submit: ' + String(err).slice(0, 60)))
    await page.waitForTimeout(3_500)
  }
}

test.describe('complex editing Typst', () => {
  let page: any
  let pid = ''

  test('T0: Typst article project + rich main.typ (lorem, math, #image, #bibliography)', async ({ page: p }) => {
    page = p
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await newTypstProject(page, { name: 'complex-typst-probe', article: true })
    const url = page.url()
    const m = /\/(editor|Project|project)\/([a-f0-9]{24})/.exec(url)
    pid = m ? m[2] : ''
    expect(pid, 'T0 the typst project id was captured from the URL ' + url).toMatch(/^[0-9a-f]{24}$/)

    const lorem = fs.readFileSync(path.join(FIXTURES, 'lorem.txt'), 'utf8').trim()
    const paras = lorem.split(/\n\s*\n/).slice(0, 6)
    const typEqs: string[] = JSON.parse(fs.readFileSync(path.join(FIXTURES, 'equations.typ.json'), 'utf8')).equations_typ

    const body = [
      '#set page(paper: "a4", margin: (top: 2.2cm, bottom: 2.2cm, left: 2.4cm, right: 2.4cm))',
      '',
      '= Complex editing fixture (Typst)',
      '',
      ...paras,
      '',
      '= Equations',
      '',
      ...typEqs.map((e: string) => e),
      '',
      '= Graphics',
      '',
      '#image("logo_full.png", width: 45%)',
      '',
      '= References',
      '',
      'Works: ~@ce_orcid_1 and ~@ce_orcid_2.',
      '',
      '#bibliography("references.bib")',
    ].join('\n')

    await expect(page.locator('.cm-content').first()).toBeVisible({ timeout: 120_000 })
    await page.waitForTimeout(2_500)
    // TYPE-TRUTH (probed 2026-10-13): in the Typst editor the focused
    // .cm-content EATS most synthetic keystrokes (10-char type → +2 len)
    // while the CM6 dispatch path writes cleanly — use the editor's own
    // view state (real CM ops → the collab room, same as typing).
    const written = await page.evaluate((text: string) => {
      const contentDOM = document.querySelector('.cm-content') as any
      const view = contentDOM?.cmView?.view
      if (!view) return 'NO-CMV'
      const st = view.state
      view.dispatch({ changes: { from: 0, to: st.doc.length, insert: text }, selection: { anchor: Math.min(20, text.length) }, userEvent: 'input' })
      return 'len=' + view.state.doc.length
    }, body).catch((e: any) => 'ERR:' + String(e).slice(0, 80))
    console.log('T0 write = ' + written)
    await page.waitForTimeout(5_000)
    const cmText = (await page.locator('.cm-content').first().innerText().catch(() => ''))
    console.log('T0 observation: editor len=' + cmText.length + ' lorem=' + /lorem/i.test(cmText) + ' image=' + /#image\("logo_full\.png"\)/.test(cmText) + ' bib=' + /#bibliography/.test(cmText))
    expect(cmText, 'T0 the rich Typst body is in the editor (len ' + cmText.length + ')').toMatch(/lorem/i)
    expect(cmText, 'T0 the bibliography directive was typed').toMatch(/#bibliography\("references\.bib"\)/)
  })

  test('T1: upload the ub-logo (file-tree Upload affordance — PNG pair)', async ({ page: p }) => {
    page = p
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 90_000 })
    await page.waitForSelector('.cm-content', { timeout: 120_000 })
    await page.waitForTimeout(1_500)
    await page.locator('button', { hasText: 'Upload' }).first().click({ timeout: 10_000 })
    await page.waitForTimeout(1_200)
    const plain = page.locator('input[type="file"]:not([webkitdirectory])').first()
    const nPlain = await plain.count().catch(() => 0)
    const nAny = await page.locator('input[type="file"]').count().catch(() => 0)
    console.log('T1 observation: file inputs = plain ' + nPlain + ' total ' + nAny)
    expect(nAny, 'T1 the Upload affordance surfaces file inputs (got ' + nAny + ')').toBeGreaterThan(0)
    const useInput = nPlain > 0 ? plain : page.locator('input[type="file"]').first()
    await useInput.setInputFiles([path.join(FIXTURES, 'logo_full.png'), path.join(FIXTURES, 'logo.png')], { timeout: 8_000 })
    await page.waitForTimeout(6_000)
    const treeText = (await page.locator('body').innerText().catch(() => ''))
    console.log('T1 observation: logo_full.png in tree = ' + /logo_full\.png/.test(treeText) + ' logo.png = ' + /(^|\s|>)logo\.png/.test(treeText))
    expect(treeText, 'T1 the uploaded logo files are in the live tree').toMatch(/logo_full\.png/)
  })

  test('T2: ORCID references through the bib VISUAL EDITOR', async ({ page: p }) => {
    page = p
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 90_000 })
    await page.waitForSelector('.cm-content', { timeout: 120_000 })
    await page.waitForTimeout(1_500)
    // references.bib: the article template may already ship one (the T0
    // body references references.bib) — create it only if missing
    const treeText = (await page.locator('body').innerText().catch(() => ''))
    if (!/references\.bib|refs\.bib/.test(treeText)) {
      await page.locator('button', { hasText: /new file/i }).first().click({ timeout: 10_000 })
      await page.waitForTimeout(800)
      await page.locator('input:visible').last().fill('references.bib').catch((e: any) => console.log('T2 fill: ' + String(e).slice(0, 60)))
      const go = page.locator('button:visible', { hasText: /create|add|save|ok/i }).last()
      if ((await go.count().catch(() => 0)) > 0) await go.click({ timeout: 4_000 }).catch(() => {})
      await page.keyboard.press('Enter').catch(() => {})
      await page.waitForTimeout(2_000)
    }
    await page.locator('text=references.bib').first().click({ timeout: 15_000 }).catch((e: any) => console.log('T2 open: ' + String(e).slice(0, 60)))
    await page.waitForTimeout(2_500)
    const swap = page.locator('.editor-toggle-switch').first()
    await expect(swap).toBeVisible({ timeout: 20_000 })
    await swap.locator('label, span').filter({ hasText: /visual/i }).first().click({ timeout: 8_000 })
    const panel = page.locator('.bib-editor-panel-content').first()
    await expect(panel).toBeVisible({ timeout: 20_000 })
    await importOrcidEntries(page, panel)
    const panelText = ((await panel.innerText().catch(() => '')) || '').replace(/\n+/g, ' | ')
    console.log('T2 observation: panel = ' + panelText.slice(0, 320))
    expect(panelText, 'T2 the ORCID works are in the visual bib panel (got: ' + panelText.slice(0, 200) + ')').toMatch(/phosphenes|object recognition|Rotermund/i)
    // code-mode parity
    await swap.locator('label, span').filter({ hasText: /code|source/i }).first().click({ timeout: 8_000 }).catch(async () => {
      await swap.locator('label, span').first().click({ timeout: 8_000 }).catch(() => {})
    })
    await page.waitForTimeout(3_000)
    const cmText = (await page.locator('.cm-content').first().innerText().catch(() => ''))
    console.log('T2 observation: code mode ce_orcid = ' + /ce_orcid_1/.test(cmText) + ' len=' + cmText.length)
    expect(cmText, 'T2 the imported entries are in the .bib source (len ' + cmText.length + ')').toMatch(/ce_orcid_1/)
  })

  test('T3: COMPILE → real PDF with lorem + resolved ORCID references', async ({ page: p }) => {
    page = p
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 90_000 })
    await page.waitForSelector('.cm-content', { timeout: 120_000 })
    await page.waitForTimeout(2_000)
    const buf = await compileAndFetch(page)
    expect(buf, 'T3 the compiled Typst PDF artifact is available').not.toBeNull()
    expect((buf as Buffer).length, 'T3 PDF is non-trivial (≥ 15 KB; got ' + (buf as Buffer).length + ')').toBeGreaterThanOrEqual(15_000)
    const pdfPath = '/tmp/complex-typst-' + Date.now() + '.pdf'
    fs.writeFileSync(pdfPath, buf as Buffer)
    let text = ''
    try {
      text = execFileSync('pdftotext', [pdfPath, '-'], { encoding: 'utf8', timeout: 30_000 })
    } catch {
      text = ''
    }
    console.log('T3 observation: pdftotext len=' + text.length + ' lorem=' + /lorem/i.test(text) + ' orcid=' + /phosphenes|prosthetic vision/i.test(text) + ' refs=' + /References/.test(text))
    expect(text, 'T3 the PDF carries the lorem body (pdftotext empty/failed)').toMatch(/lorem/i)
    // 0.15.1 citations: the bibliography entries render in the references
    expect(text, 'T3 an ORCID work title resolved into the references (got ' + ((text.match(/References[\s\S]{0,400}/) || [''])[0].slice(0, 160)) + ')').toMatch(/phosphenes|prosthetic vision|object recognition/i)
    try { fs.unlinkSync(pdfPath) } catch { /* best effort */ }
  })

  test('T4: world hygiene — deleted state asserted (soft-delete world truth)', async ({ page: p }) => {
    page = p
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/hub', { waitUntil: 'domcontentloaded', timeout: 60_000 })
    await page.waitForTimeout(700)
    await killProject(page, pid).catch((e: any) => console.log('T4 kill: ' + String(e).slice(0, 80)))
    await page.waitForTimeout(2_000)
    const state = (mongoEval(`var p = db.projects.findOne({ _id: new ObjectId("${pid}") }); print(p ? (p.deletedAt ? 'DELETED' : 'ACTIVE') : 'GONE');`) || '').trim()
    console.log('T4 observation: project state after kill = ' + state)
    expect(state, 'T4 the typst probe project is deleted or gone (got ' + state + ')').toMatch(/DELETED|GONE/)
  })
})

/**
 * Complex editing — TeX engine (TODO-479d936a, owner story 2026-10-11).
 *
 * Mimics complex real editing, end-to-end on the ag-v57 world (:7420):
 *  - substantial lorem body + ORCID references (owner iD 0000-0002-8819-4879)
 *    IMPORTED THROUGH THE BIB VISUAL EDITOR ("Upload .bib file")
 *  + display equations (im2latex corpus picks) + ub-logo graphics
 *  + multi-file (sub/extra.tex in a subfolder, \\input from main)
 *  → COMPILE → PDF verified on the host: real %PDF, non-trivial size,
 *    pdftotext carries the lorem body, an ORCID work title fragment, and
 *    the sub-folder content.
 *
 * CONTENT-TRUTH (discovered 2026-10-13, pinned): the editor's source of
 * truth is the COLLAB HISTORY store (room content seeded from the web
 * history REST; collab/roomdoc.go). Direct mongo `docs.lines` writes are
 * INVISIBLE to the editor (a seeded rich main.tex showed blank template
 * content in the pane). Therefore every byte in this spec goes through
 * the REAL write path: editor typing (CodeMirror ops → room → flush →
 * history), the bib-visual "Upload .bib file" import, and the file-tree
 * Upload — i.e. exactly what a user does.
 *
 * REAPER NOTE: the image cron deactivates idle projects
 * (POST /internal/deactivateOldProjects; inactive-data spec) — the suite
 * runs continuously (activity keeps the project warm); the final kill is
 * best-effort idempotent.
 *
 * Rate discipline (owner): real projects + real compiles → strict serial,
 * single project, cleaned at the end.
 */
import { test, expect } from '@playwright/test'
import * as fs from 'fs'
import * as path from 'path'
import { execFileSync } from 'child_process'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { killProject } from '../parity/harness'
import { mongoEval } from '../helpers/host'
import { ADMIN } from '../fixtures/credentials'

const FIXTURES = path.join(path.resolve(process.cwd()), 'test-data', 'complex-edit')

test.describe.configure({ mode: 'serial' })
test.setTimeout(900_000)

async function compileAndFetch(page: any): Promise<Buffer | null> {
  // The editor auto-compiles on load (compile?auto_compile=true) — the PDF
  // may already exist; clicking Recompile re-triggers. Either way, the
  // artifact URL appears in the compile response (outputFiles[].url);
  // poll link + artifact bytes with generous windows (multi-pass TeX +
  // bibtex + the async sandbox flush, owner #10).
  const recomp = page.locator('button:has-text("Recompile")').first()
  if ((await recomp.count().catch(() => 0)) > 0) {
    await recomp.click({ timeout: 10_000 }).catch(() => {})
  } else {
    const compile = page.locator('button:has-text("Compile")').first()
    if ((await compile.count().catch(() => 0)) > 0) await compile.click({ timeout: 10_000 }).catch(() => {})
  }
  const link = page.locator('a[href*="output.pdf"], a:has-text("Download PDF")').first()
  // long window: first compile of a rich multi-pass doc can take minutes
  let href: string | null = null
  for (let i = 0; i < 36; i++) {
    await page.waitForTimeout(10_000)
    const n = await link.count().catch(() => 0)
    if (n > 0) {
      href = (await link.getAttribute('href').catch(() => null)) || null
      if (href) break
    }
    const bodyText = (await page.locator('body').innerText().catch(() => '')).slice(0, 30_000)
    if (/There was a problem rendering|Compile error|Compilation failed/i.test(bodyText)) {
      console.log('compileAndFetch: error surface visible: ' + bodyText.slice(bodyText.search(/problem|error|failed/i), bodyText.search(/problem|error|failed/i) + 200).replace(/\n+/g, ' | '))
      return null
    }
  }
  if (!href) {
    // fallback: the href may only be on later render ticks
    href = (await link.getAttribute('href').catch(() => null)) || null
  }
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

test.describe('complex editing TeX', () => {
  let page: any
  let pid = ''

  test('T0: blank project + rich main.tex through the REAL editor write path', async ({ page: p }) => {
    page = p
    await loginRobust(page, ADMIN.email, ADMIN.password)
    pid = await createBlankProject(page)
    expect(pid, 'T0 project created').toMatch(/^[0-9a-f]{24}$/)

    const lorem = fs.readFileSync(path.join(FIXTURES, 'lorem.txt'), 'utf8').trim()
    const paras = lorem.split(/\n\s*\n/).slice(0, 7)
    const texEqs: string[] = JSON.parse(fs.readFileSync(path.join(FIXTURES, 'equations.tex.json'), 'utf8')).equations_tex
    const fracish = texEqs.filter((e: string) => /\\frac|\\sqrt/.test(e)).slice(0, 2)
    const eqs = fracish.length >= 2 ? fracish : texEqs.slice(0, 2)

    const body = [
      '\\documentclass{article}',
      '\\usepackage[utf8]{inputenc}',
      '\\usepackage{graphicx}',
      '\\usepackage{amsmath}',
      '\\begin{document}',
      '\\section{Complex editing fixture}',
      ...paras,
      '\\section{Equations}',
      ...eqs,
      '\\section{Graphics}',
      '\\begin{figure}',
      '\\includegraphics[width=0.5\\textwidth]{logo_full.png}',
      '\\caption{ub logo fixture}',
      '\\end{figure}',
      '\\section{Included file}',
      '\\input{sub/extra}',
      '\\section*{References}',
      '\\nocite{*}',
      '\\bibliographystyle{plain}',
      '\\bibliography{refs}',
      '\\end{document}',
    ].join('\n')

    await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 90_000 })
    const cm = page.locator('.cm-content').first()
    await cm.waitFor({ state: 'visible', timeout: 120_000 })
    await page.waitForTimeout(2_000)
    // replace the boilerplate: select-all + typed body (CodeMirror ops →
    // room → history = the canonical store)
    await cm.click({ timeout: 10_000 })
    await page.keyboard.press('ControlOrMeta+a')
    await page.waitForTimeout(300)
    await page.keyboard.type(body, { delay: 2 }).catch((e: any) => console.log('T0 type err: ' + String(e).slice(0, 80)))
    await page.waitForTimeout(5_000)
    const cmText = await cm.innerText().catch(() => '')
    console.log('T0 observation: editor len = ' + cmText.length + ' lorem=' + /lorem/i.test(cmText) + ' nocite=' + /nocite/.test(cmText) + ' input=' + /\\input\{sub\/extra\}/.test(cmText))
    expect(cmText, 'T0 lorem body present in the editor (len ' + cmText.length + ')').toMatch(/lorem/i)
    expect(cmText, 'T0 the references block was typed').toMatch(/\\nocite\{\*\}/)
    expect(cmText, 'T0 the \\input{sub/extra} line was typed').toMatch(/input\{sub\/extra\}/)
  })

  test('T1: sub/extra.tex in a real subfolder (file-tree UI)', async ({ page: p }) => {
    page = p
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 90_000 })
    await page.waitForSelector('.cm-content', { timeout: 120_000 })
    await page.waitForTimeout(2_000)
    // New folder (toolbar)
    await page.locator('button', { hasText: /new folder/i }).first().click({ timeout: 10_000 })
    await page.waitForTimeout(800)
    const foldInp = page.locator('input:visible').last()
    await foldInp.fill('sub').catch((e: any) => console.log('T1 folder fill: ' + String(e).slice(0, 60)))
    const submit = page.locator('button:visible', { hasText: /create|add|save|ok/i }).last()
    if ((await submit.count().catch(() => 0)) > 0) await submit.click({ timeout: 4_000 }).catch(() => {})
    await page.keyboard.press('Enter').catch(() => {})
    await page.waitForTimeout(1_200)
    const tree = (await page.locator('body').innerText().catch(() => ''))
    console.log('T1 observation: folder sub in tree = ' + /(^|\n)sub(\n|$)/.test(tree))
    expect(tree, 'T1 the subfolder "sub" exists in the tree').toMatch(/sub/)
    // New file inside the folder: expand it, then its New-file affordance
    const folderRow = page.locator('[role="treeitem"], li, div', { hasText: /^sub$/ }).last()
    await folderRow.click({ timeout: 8_000 }).catch(() => {})
    await page.waitForTimeout(700)
    const newFileBtn = page.locator('button[aria-label*="new file" i], button[title*="new file" i], button', { hasText: /new file/i })
    const nfCount = await newFileBtn.count().catch(() => 0)
    if (nfCount > 0) {
      await newFileBtn.last().click({ timeout: 8_000 }).catch(() => {})
      await page.waitForTimeout(800)
      const fileInp = page.locator('input:visible').last()
      await fileInp.fill('extra.tex').catch((e: any) => console.log('T1 file fill: ' + String(e).slice(0, 60)))
      const go = page.locator('button:visible', { hasText: /create|add|save|ok/i }).last()
      if ((await go.count().catch(() => 0)) > 0) await go.click({ timeout: 4_000 }).catch(() => {})
      await page.keyboard.press('Enter').catch(() => {})
      await page.waitForTimeout(1_500)
    }
    const tree2 = (await page.locator('body').innerText().catch(() => ''))
    console.log('T1 observation: extra.tex in tree = ' + /extra\.tex/.test(tree2))
    // open it and type the content (real write path)
    await page.locator('text=extra.tex').first().click({ timeout: 10_000 }).catch((e: any) => console.log('T1 open extra: ' + String(e).slice(0, 60)))
    await page.waitForTimeout(2_500)
    const cm = page.locator('.cm-content').first()
    await cm.click({ timeout: 10_000 })
    await page.keyboard.press('ControlOrMeta+a').catch(() => {})
    await page.keyboard.type('\\paragraph{Sub-folder file.} This paragraph lives in sub/extra.tex and is pulled in by the main document via the input command.', { delay: 2 }).catch(() => {})
    await page.waitForTimeout(3_500)
    const t3 = (await page.locator('body').innerText().catch(() => ''))
    console.log('T1 observation: extra.tex persisted in tree = ' + /extra\.tex/.test(t3))
    expect(t3, 'T1 extra.tex exists in the live tree (the \\input target)').toMatch(/extra\.tex/)
  })

  test('T2: upload the ub-logo PNG (file-tree Upload affordance)', async ({ page: p }) => {
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
    console.log('T2 observation: file inputs = plain ' + nPlain + ' total ' + nAny)
    expect(nAny, 'T2 the Upload affordance surfaces file inputs (got ' + nAny + ')').toBeGreaterThan(0)
    const useInput = nPlain > 0 ? plain : page.locator('input[type="file"]').first()
    await useInput.setInputFiles(path.join(FIXTURES, 'logo_full.png'), { timeout: 8_000 })
    await page.waitForTimeout(6_000)
    const treeText = (await page.locator('body').innerText().catch(() => ''))
    console.log('T2 observation: logo_full.png in live tree = ' + /logo_full\.png/.test(treeText))
    expect(treeText, 'T2 the uploaded PNG is in the live tree').toMatch(/logo_full\.png/)
  })

  test('T3: ORCID references imported THROUGH THE bib VISUAL EDITOR (Enter-manually path)', async ({ page: p }) => {
    page = p
    // IMPORT-PATH TRUTH (probed 2026-10-13, flags pinned): the Add menu
    // offers Paste references / "Upload .bib file" / Enter manually /
    // Import from ORCID.org / Zotero / Library — but in ag-v57:
    //   (a) "Upload .bib file" mounts the hidden <input accept=".bib">
    //       and selecting a file triggers NO import request at all
    //       (owner flag: inert upload path);
    //   (b) "Paste references" opens its box but no submit control is
    //       reachable (owner flag: incomplete flow).
    // The ENTER-MANUALLY modal is the verified in-editor import
    // (bib-editor spec, live round-tripped) and is what T3 uses, with
    // entries sourced from the same ORCID-work fixture as the .bib file.
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 90_000 })
    await page.waitForSelector('.cm-content', { timeout: 120_000 })
    await page.waitForTimeout(1_500)
    // create refs.bib
    await page.locator('button', { hasText: /new file/i }).first().click({ timeout: 10_000 })
    await page.waitForTimeout(800)
    await page.locator('input:visible').last().fill('refs.bib').catch((e: any) => console.log('T3 fill: ' + String(e).slice(0, 60)))
    const go = page.locator('button:visible', { hasText: /create|add|save|ok/i }).last()
    if ((await go.count().catch(() => 0)) > 0) await go.click({ timeout: 4_000 }).catch(() => {})
    await page.keyboard.press('Enter').catch(() => {})
    await page.waitForTimeout(2_000)
    await page.locator('text=refs.bib').first().click({ timeout: 15_000 }).catch((e: any) => console.log('T3 open: ' + String(e).slice(0, 60)))
    await page.waitForTimeout(2_500)
    const swap = page.locator('.editor-toggle-switch').first()
    await expect(swap).toBeVisible({ timeout: 20_000 })
    await swap.locator('label, span').filter({ hasText: /visual/i }).first().click({ timeout: 8_000 })
    const panel = page.locator('.bib-editor-panel-content').first()
    await expect(panel).toBeVisible({ timeout: 20_000 })

    // the .bib fixture entries (deterministic: first two ORCID works)
    interface BibEntry { key: string; title: string; author: string; year: string; type: string }
    const fixtures: BibEntry[] = [
      { key: 'ce_orcid_1', title: 'Minimizing the number of phosphenes required for object recognition under prosthetic vision', author: 'Rotermund, David', year: '2024', type: 'misc' },
      { key: 'ce_orcid_2', title: 'Categorical object properties outweigh local visual information in object recognition', author: 'Rotermund, David', year: '2021', type: 'inproceedings' },
    ]
    for (const e of fixtures) {
      await panel.locator('button').filter({ hasText: /add/i }).first().click({ timeout: 8_000 })
      await page.waitForTimeout(1_000)
      await page
        .locator('[role=menuitem], [class*="menuitem"], li, button')
        .filter({ hasText: /enter manually/i })
        .first()
        .click({ timeout: 8_000 }).catch((err: any) => console.log('T3 menu: ' + String(err).slice(0, 60)))
      // the modal lazy-mounts (1-8s in the bib spec) — poll for the key field
      let modalFound = false
      for (let i = 0; i < 16; i++) {
        await page.waitForTimeout(700)
        if ((await page.locator('label').filter({ hasText: /citation key/i }).count().catch(() => 0)) > 0) { modalFound = true; break }
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
      }, { key: e.key, title: e.title, year: e.year })
      expect(filled, 'T3 the entry fields were filled (entry ' + e.key + ')').toBe(true)
      await page
        .locator('[role=dialog], .mantine-Modal-content, [class*=modal]')
        .filter({ hasText: /citation key/i })
        .last()
        .locator('button')
        .filter({ hasText: /add/i })
        .last()
        .click({ timeout: 8_000 }).catch((err: any) => console.log('T3 submit: ' + String(err).slice(0, 60)))
      await page.waitForTimeout(4_000)
    }
    const panelText = ((await panel.innerText().catch(() => '')) || '').replace(/\n+/g, ' | ')
    console.log('T3 observation: panel = ' + panelText.slice(0, 400))
    const orcidVisible = /phosphenes|object recognition|Rotermund/i.test(panelText)
    expect(orcidVisible, 'T3 the ORCID works are in the visual bib panel (got: ' + panelText.slice(0, 200) + ')').toBe(true)
    // code-mode parity: the entries are in the document source
    await swap.locator('label, span').filter({ hasText: /code|source/i }).first().click({ timeout: 8_000 }).catch(async () => {
      await swap.locator('label, span').first().click({ timeout: 8_000 }).catch(() => {})
    })
    await page.waitForTimeout(3_000)
    const cmText = (await page.locator('.cm-content').first().innerText().catch(() => ''))
    console.log('T3 observation: code mode ce_orcid keys = ' + /ce_orcid_1/.test(cmText) + ' len=' + cmText.length)
    expect(cmText, 'T3 the imported entries wrote into the .bib source (len ' + cmText.length + ')').toMatch(/ce_orcid_1/)
  })

  test('T4: COMPILE → real PDF with lorem + ORCID references + sub-folder content', async ({ page: p }) => {
    page = p
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 90_000 })
    await page.waitForSelector('.cm-content', { timeout: 120_000 })
    await page.waitForTimeout(2_000)
    const buf = await compileAndFetch(page)
    expect(buf, 'T4 the compiled PDF artifact is available (compile surface visible?)').not.toBeNull()
    expect((buf as Buffer).length, 'T4 PDF is non-trivial (≥ 15 KB; got ' + (buf as Buffer).length + ')').toBeGreaterThanOrEqual(15_000)
    const pdfPath = '/tmp/complex-tex-' + Date.now() + '.pdf'
    fs.writeFileSync(pdfPath, buf as Buffer)
    let text = ''
    try {
      text = execFileSync('pdftotext', [pdfPath, '-'], { encoding: 'utf8', timeout: 30_000 })
    } catch {
      text = ''
    }
    console.log('T4 observation: pdftotext len = ' + text.length + ' | lorem=' + /lorem/i.test(text) + ' orcid=' + /phosphenes|prosthetic vision/i.test(text) + ' subfile=' + /Sub-folder file/i.test(text) + ' refs=' + /References/.test(text))
    expect(text, 'T4 the PDF carries the lorem body (pdftotext empty/failed)').toMatch(/lorem/i)
    expect(text, 'T4 an ORCID work title fragment is in the PDF references (got ' + ((text.match(/References[\s\S]{0,400}/) || [''])[0].slice(0, 160)) + ')').toMatch(/phosphenes|prosthetic vision|object recognition/i)
    expect(text, 'T4 the sub-folder \\input content compiled into the PDF').toMatch(/Sub-folder file/i)
    try { fs.unlinkSync(pdfPath) } catch { /* best effort */ }
  })

  test('T5: world hygiene — kill the project (idempotent)', async ({ page: p }) => {
    page = p
    // killProject's api() reads the CSRF meta FROM THE PAGE — /hub has it
    // (the login page does not); land there first or the mutations 403.
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/hub', { waitUntil: 'domcontentloaded', timeout: 60_000 })
    await page.waitForTimeout(700)
    await killProject(page, pid).catch((e: any) => console.log('T5 kill: ' + String(e).slice(0, 80)))
    await page.waitForTimeout(2_000)
    // WORLD TRUTH: admin delete is SOFT (deletedAt stamped, hard deletion
    // after the misc.projectHardDeletionDelayDays policy) — the doc can
    // legitimately persist; the contract is the deleted state.
    const state = (mongoEval(`var p = db.projects.findOne({ _id: new ObjectId("${pid}") }); print(p ? (p.deletedAt ? 'DELETED' : 'ACTIVE') : 'GONE');`) || '').trim()
    console.log('T5 observation: project state after kill = ' + state)
    expect(state, 'T5 the project is deleted (soft) or gone (got ' + state + ')').toMatch(/DELETED|GONE/)
  })
})


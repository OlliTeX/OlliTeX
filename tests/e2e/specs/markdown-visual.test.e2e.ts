/**
 * Markdown (.md) visual editor — O-1 regression pin (TODO-ef7e2a57).
 *
 * O-1 (owner wave, live-verified 2026-10-10): a NEW `.md` file has an EMPTY
 * body (no LaTeX boilerplate) and the editor shows the Milkdown VISUAL editor
 * (not the LaTeX source view) by default; source view is one "Code and visual
 * editor switch" away.
 *
 * ag-v57 contract (probed 2026-10-13):
 *   - create: POST /project/:pid/doc {name} 200 → mongo doc =
 *     {_id, project_id, rev, lines: [], ranges: {}, version} — `lines: []`
 *     (EMPTY — no boilerplate; .tex files seed LaTeX boilerplate).
 *   - default surface for .md = Milkdown: div.ProseMirror.ol-milkdown
 *     (visible, contenteditable) inside .milkdown; the markdown SOURCE
 *     CodeMirror (.cm-content[data-language="markdown"]) exists but is HIDDEN
 *     (0×0) until the "Code and visual editor switch" toggles it.
 *   - editing in the visual editor persists to the same `lines` array
 *     (the .md file type is the diff-lines store, not a separate body field).
 *   - round-trip: reload the editor, reopen the .md → the visual editor
 *     shows the previously typed text (probed: "roundtrip probe 77" survives).
 *
 * SURFACE FLAGS (owner, ag-v57): MD-1 the Markdown editor toolbar
 * (aria-label "Markdown editor toolbar") renders its controls as
 * glyphs (link/pencil/trash) — no accessible names (a11y); MD-2 both editor
 * panes are mounted at once (the source CodeMirror stays in the DOM 0×0) —
 * the switch flips visibility, not mount state.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { mongoEval } from '../helpers/host'
import { ADMIN } from '../fixtures/credentials'
import { killProject } from '../parity/harness'

test.describe.configure({ mode: 'serial' })
test.setTimeout(300_000)

async function openMd(page: any): Promise<void> {
  // open the .md file: click the tree row (up to two attempts — late tree
  // mounts have delayed the first click). The editor then swaps to Milkdown.
  for (let i = 0; i < 2; i++) {
    const row = page.locator('text=notes.md').first()
    try {
      await row.waitFor({ state: 'visible', timeout: 20_000 })
      await row.click({ timeout: 8_000 })
    } catch {
      if (i === 1) throw new Error('openMd: the notes.md tree row stayed unreachable')
      await page.waitForTimeout(2_000)
      continue
    }
    try {
      await page.locator('div.ProseMirror.ol-milkdown').first().waitFor({ state: 'visible', timeout: 20_000 })
      return
    } catch {
      if (i === 1) throw new Error('openMd: the Milkdown surface never appeared after the row click')
    }
    await page.waitForTimeout(2_000)
  }
}

function docLines(mid: string): any {
  const raw = (mongoEval(`var d = db.docs.findOne({ _id: new ObjectId("${mid}") }); d ? print(JSON.stringify(d.lines)) : print("GONE");`) || '').trim()
  try { return JSON.parse(raw) } catch { return raw }
}

test.describe('markdown md-visual editor (O-1 pin)', () => {
  let page: any
  let pid = ''
  let mid = ''

  test('M1: new .md = EMPTY lines (no LaTeX boilerplate) and Milkdown surface renders', async ({ page: p }) => {
    page = p
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/hub', { waitUntil: 'domcontentloaded', timeout: 60_000 })
    await page.waitForTimeout(900)
    pid = await createBlankProject(page)
    await page.waitForTimeout(700)
    const created = await page.evaluate(async ({ pid }: any) => {
      const csrf = document.querySelector('meta[name="ol-csrfToken"]')?.content || ''
      const r = await fetch('/project/' + pid + '/doc', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
        body: JSON.stringify({ name: 'notes.md' }),
      })
      let j: any = []
      try { j = await r.json() } catch { j = [] }
      const f = Array.isArray(j) ? j[0] : (j && (j.file || j.doc || j))
      return { status: r.status, id: (f && (f.id || f._id)) || '' }
    }, { pid })
    expect(created.status).toBeLessThan(300)
    mid = created.id
    expect(mid, 'M1 the .md doc was created (id)').toMatch(/[0-9a-f]{20,}/)
    // O-1 CORE: empty body, no boilerplate
    const lines = docLines(mid)
    console.log('M1 observation: new .md lines = ' + JSON.stringify(lines))
    expect(Array.isArray(lines), 'M1 lines is an array (got ' + JSON.stringify(lines) + ')').toBe(true)
    expect((lines as any[]).join(''), 'M1 no boilerplate content in a fresh .md').toBe('')
    // surface: the Milkdown visual editor is the VISIBLE .md pane
    await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 90_000 })
    await page.waitForSelector('.cm-content', { timeout: 120_000 })
    await openMd(page)
    const pm = page.locator('div.ProseMirror.ol-milkdown').first()
    await pm.waitFor({ state: 'visible', timeout: 60_000 })
    const vis = await page.evaluate(() => {
      const probe = (sel: string) => {
        const el = document.querySelector(sel) as HTMLElement | null
        if (!el) return null
        const r = el.getBoundingClientRect()
        return r.width > 0 && r.height > 0
      }
      const pmEl = document.querySelector('div.ProseMirror.ol-milkdown') as HTMLElement | null
      return {
        milkdownVisible: probe('.milkdown'),
        pmVisible: probe('div.ProseMirror.ol-milkdown'),
        pmEditable: !!pmEl && pmEl.isContentEditable,
        sourceHidden: (document.querySelector('.cm-content[data-language="markdown"]') as HTMLElement | null)?.getBoundingClientRect().width === 0,
        switchExists: !!Array.from(document.querySelectorAll('[aria-label]')).find((b: any) => /code and visual editor switch/i.test(b.getAttribute('aria-label') || ''))
      }
    })
    console.log('M1 surface = ' + JSON.stringify(vis))
    expect(vis.milkdownVisible, 'M1 the Milkdown wrapper is visible').toBe(true)
    expect(vis.pmVisible, 'M1 the ProseMirror surface is visible').toBe(true)
    expect(vis.pmEditable, 'M1 the visual editor is contenteditable').toBe(true)
    expect(vis.sourceHidden, 'M1 the markdown SOURCE pane is mounted-hidden by default').toBe(true)
    expect(vis.switchExists, 'M1 the code/visual switch control exists').toBe(true)
  })

  test('M2: edit in the visual editor → persists to the lines store', async ({ page: p }) => {
    page = p
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 90_000 })
    await page.waitForSelector('.cm-content', { timeout: 120_000 })
    await openMd(page)
    const pm = page.locator('div.ProseMirror.ol-milkdown').first()
    await pm.waitFor({ state: 'visible', timeout: 60_000 })
    await pm.click({ force: true, timeout: 8_000 })
    await page.keyboard.type('alpha md line one', { delay: 12 })
    await page.waitForTimeout(6_000)
    const lines = docLines(mid)
    console.log('M2 observation: lines = ' + JSON.stringify(lines).slice(0, 200))
    expect(JSON.stringify(lines), 'M2 the typed markdown is in the persisted lines (got ' + JSON.stringify(lines).slice(0, 120) + ')').toContain('alpha md line one')
  })

  test('M3: round-trip — reload editor, the visual editor shows the prior text', async ({ page: p }) => {
    page = p
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 90_000 })
    await page.waitForSelector('.cm-content', { timeout: 120_000 })
    await openMd(page)
    const pm = page.locator('div.ProseMirror.ol-milkdown').first()
    await pm.waitFor({ state: 'visible', timeout: 60_000 })
    await page.waitForTimeout(3_000)
    const text = await pm.evaluate((el: any) => (el.textContent || '').slice(0, 120))
    console.log('M3 observation: roundtrip text = ' + JSON.stringify(text).slice(0, 140))
    expect(text, 'M3 the reload shows the previously typed markdown (got ' + JSON.stringify(text).slice(0, 100) + ')').toContain('alpha md line one')
  })

  test('M4: world hygiene — kill the probe project', async ({ page: p }) => {
    page = p
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await killProject(page, pid)
    const gone = (mongoEval(`print(db.projects.countDocuments({ _id: new ObjectId("${pid}") }));`) || '').trim()
    console.log('M4 observation: project count after kill = ' + gone)
    expect(gone, 'M4 the probe project is gone').toBe('0')
  })
})

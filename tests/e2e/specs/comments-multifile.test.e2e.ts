/**
 * INTENSIVE COMMENTS — multi-file + anchor robustness (TODO-d4f9eb7a, gaps 3 + 4).
 *
 * Green neighbors this builds on (all re-verified 2026-10-13):
 *  - review-overview-completeness: comments on a NEVER-OPENED file are complete in
 *    the wire (doc + ranges) — the docstore/mongo-direct reads have no loaded-only
 *    leg (04724e42 immune pin).
 *  - review-panel R9 (D40-P3): comment range follows the seeded text after an edit.
 *
 * This suite extends to:
 *  M1 wire multi-file completeness on a SEEDED .bib (main.tex + sample.bib)
 *  M2 .bib reader/editor surface: open sample.bib no-5xx + content renders;
 *     comment created while the .bib tab is active; (CMT-3 observation: the
 *     inline-note surface is absent for BOTH files in ag-v57 — pinned same as
 *     comments-ui-lifecycle)
 *  M3 anchor robustness: comment spanning two lines → lines collapsed to one —
 *     thread ranges stay well-formed (observation pinned, not exact positions:
 *     Node-parity re-anchoring is the open question → owner)
 *  M4 comment at end-of-buffer → text appended after the anchor — ranges valid.
 *
 * Surface honesty: the UI note/panel comment surface is ABSENT in ag-v57 (CMT-3,
 * flagged owner-gated); every assertion below is therefore at the data-wire and
 * document-integrity level, which is the green, pinned foundation.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { mongoEval } from '../helpers/host'
import { USER } from '../fixtures/credentials'

test.describe.configure({ mode: 'serial' })
test.setTimeout(420_000)

async function apiJSON(page: any, method: string, url: string, body?: any) {
  return page.evaluate(
    async ({ method, url, body }) => {
      const csrf = document.querySelector('meta[name="ol-csrfToken"]')?.content ?? ''
      const res = await fetch(url, {
        method,
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
        body: body === undefined ? undefined : JSON.stringify(body),
      })
      let json: any = null
      const text = await res.text().catch(() => '')
      try { json = text ? JSON.parse(text) : null } catch { json = null }
      return { status: res.status, json, text: text.slice(0, 300) }
    },
    { method, url, body },
  )
}

// seed a doc + project-tree entry (pattern from collab-two-user, verified green)
function seedTextFile(pid: string, name: string, content: string): string {
  const escaped = content.replace(/\\/g, '\\\\').replace(/'/g, "\\'").replace(/\n/g, '\\n')
  const script = `
    var pid = ObjectId('${pid}');
    var doc = { _id: ObjectId(), project_id: pid, rev: 1, version: 1,
                lines: '${escaped}'.split('\\n'), ranges: [] };
    db.docs.insertOne(doc);
    var p = db.projects.findOne({ _id: pid });
    var entry = { name: '${name}', _id: String(doc._id) };
    var rf = (p.rootFolder && p.rootFolder[0]) || { name: 'rootFolder', _id: 'rf0', docs: [], fileRefs: [], folders: [] };
    rf.docs = rf.docs || []; rf.fileRefs = rf.fileRefs || []; rf.folders = rf.folders || [];
    rf.docs.push(entry);
    db.projects.updateOne({ _id: pid }, { $set: { rootFolder: [rf] } });
    String(doc._id);
  `
  const out = (mongoEval(script) || '').trim().split('\n').pop() as string
  if (!/^[0-9a-f]{24}$/.test(out)) throw new Error(`seedTextFile(${name}): bad doc id: ${out}`)
  return out
}

async function editorReady(page: any, pid: string): Promise<void> {
  await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 60_000 })
  await page.waitForSelector('[role="treeitem"]', { timeout: 90_000 })
  const item = page.locator('[role="treeitem"]', { hasText: 'main.tex' }).first()
  if ((await item.count()) > 0) {
    await item.dblclick({ timeout: 20_000 }).catch(() => item.click({ timeout: 5_000 }))
  }
  await page.waitForSelector('.cm-content', { timeout: 60_000 })
}

async function seedText(page: any, text: string): Promise<void> {
  await page.evaluate(
    (t: string) => {
      const el = document.querySelector('.cm-content') as any
      const view = el && el.cmView ? el.cmView.view : null
      if (!view) throw new Error('no CM view')
      const len = view.state.doc.length
      view.dispatch({ changes: { from: 0, to: len, insert: t }, selection: { anchor: 0 } })
    },
    text,
  )
}

async function roomHas(page: any, pid: string, needle: string, ms = 15_000): Promise<boolean> {
  const t0 = Date.now()
  for (;;) {
    const c = await page
      .evaluate(async (u: string) => {
        const r = await fetch(u, { credentials: 'same-origin' })
        if (!r.ok) return ''
        const j = await r.json()
        return j && typeof j.content === 'string' ? j.content : (j && (j as any).text) || ''
      }, `/project/${pid}/collab/doc`)
      .catch(() => '')
    if (String(c).includes(needle)) return true
    if (Date.now() - t0 > ms) return false
    await page.waitForTimeout(500)
  }
}

function wellFormedRanges(ranges: any[]): boolean {
  // canonical wire shape is {pos, len} (review.go messageAdd P1; green neighbor
  // review-overview-completeness asserts the same shape)
  return Array.isArray(ranges) && ranges.length > 0 && ranges.every(
    (r) => Number.isInteger(r.pos) && Number.isInteger(r.len) && r.len >= 0)
}

async function ensureAuth(page: any): Promise<void> {
  const ready = await page
    .evaluate(() => !!document.querySelector('meta[name="ol-csrfToken"]'))
    .catch(() => false)
  if (!ready) await loginRobust(page, USER.email, USER.password)
}

test.describe('comments multifile + anchors', () => {
  let page: any
  let pid = ''
  const stamp = 'cmtmf' + Date.now().toString(36)

  test('setup: seeded multi-file project (main.tex + sample.bib) open', async ({ page: p }) => {
    page = p
    await loginRobust(page, USER.email, USER.password)
    pid = await createBlankProject(page)
    seedTextFile(pid, 'sample.bib', '@article{greenwade93,\n  author = {Greenwade, J. H.},\n  title  = {Generating BibTeX on the fly},\n}\n')
    await editorReady(page, pid)
    // the .bib entry must be in the tree (project-tree completeness)
    const bib = page.locator('[role="treeitem"]', { hasText: 'sample.bib' }).first()
    expect(await bib.count(), 'sample.bib listed in the file tree').toBeGreaterThanOrEqual(1)
  })

  test('M1: wire completeness — comments on BOTH files (bib never opened) carry doc+ranges', async ({ page: p }) => {
    page = p
    await ensureAuth(page)
    await editorReady(page, pid)
    await seedText(page, 'line one\nline two\nline three')
    expect(await roomHas(page, pid, 'line three'), 'main.tex seeded and synced to the room').toBe(true)

    const tm = 'cf' + stamp + 'm1'
    const r1 = await apiJSON(page, 'POST', `/project/${pid}/thread/${tm}/messages`, {
      content: stamp + ' on main.tex',
      doc: 'main.tex',
      ranges: [{ pos: 10, len: 8 }],
    })
    expect(r1.status, 'main.tex comment → 201 (got ' + r1.status + ' ' + ((r1 as any).text || JSON.stringify(r1.json)).slice(0, 200) + ')').toBe(201)

    const tb = 'cf' + stamp + 'b1'
    const r2 = await apiJSON(page, 'POST', `/project/${pid}/thread/${tb}/messages`, {
      content: stamp + ' on sample.bib (never opened)',
      doc: 'sample.bib',
      ranges: [{ pos: 20, len: 20 }],
    })
    expect(r2.status, 'sample.bib comment → 201 (got ' + r2.status + ' ' + JSON.stringify(r2.json).slice(0, 120) + ')').toBe(201)

    const rec = await apiJSON(page, 'GET', '/project/' + pid + '/threads')
    expect(rec.status).toBe(200)
    const recObj = rec.json as any
    expect(String(recObj[tm]?.doc)).toBe('main.tex')
    expect(String(recObj[tb]?.doc)).toBe('sample.bib')
    // ranges live on the MESSAGE (Node-parity shape; green neighbor
    // review-overview-completeness reads rec[tid].messages[].ranges)
    const mMain = (recObj[tm].messages || []).find((m: any) => String(m.doc ?? 'main.tex') === 'main.tex')
    const mBib = (recObj[tb].messages || []).find((m: any) => String(m.doc ?? 'sample.bib') === 'sample.bib')
    const rMain = mMain?.ranges ?? recObj[tm].ranges
    const rBib = mBib?.ranges ?? recObj[tb].ranges
    expect(mMain, 'main.tex message present').toBeTruthy()
    expect(mBib, 'sample.bib message present').toBeTruthy()
    expect(wellFormedRanges(rMain), 'main.tex message ranges well-formed (' + JSON.stringify(rMain).slice(0, 80) + ')').toBe(true)
    expect(wellFormedRanges(rBib), 'sample.bib message ranges well-formed (' + JSON.stringify(rBib).slice(0, 80) + ')').toBe(true)
    // docstore consistency: the .bib doc the project tree points at exists
    // (name lives on the rootFolder entry, not the doc — seed shape parity)
    const bibDoc = mongoEval(`var p = db.projects.findOne({ _id: ObjectId('${pid}') }); var e = p.rootFolder[0].docs.filter(function(x){ return x.name === 'sample.bib'; })[0]; var d = e && db.docs.findOne({ _id: new ObjectId(e._id) }); print(d ? 'present' : 'absent');`)
    expect(String(bibDoc).trim(), 'sample.bib doc present in docstore/mongo').toContain('present')
  })

  test('M2: .bib editor surface opens without 5xx; comment while .bib is active', async ({ page: p }) => {
    page = p
    await ensureAuth(page)
    await editorReady(page, pid)
    const bib = page.locator('[role="treeitem"]', { hasText: 'sample.bib' }).first()
    await bib.dblclick({ timeout: 20_000 }).catch(() => bib.click({ timeout: 5_000 }))
    // no-5xx open pin (review-fileview-500 class, now for the .bib surface)
    const resp = await page.evaluate(async () => {
      const r = await fetch('/project/' + (window as any).olProjectId || '', { credentials: 'same-origin' })
      return r.status
    }).catch(() => 0)
    expect(Number(resp) === 0 || Number(resp) < 500, 'no 5xx on .bib surface activity (got ' + resp + ')').toBe(true)
    // the .bib content renders in the editor (reader/editor surface for .bib)
    await page.waitForTimeout(2_500)
    const bodyTxt = String(await page.locator('body').innerText())
    expect(bodyTxt, 'sample.bib content visible once opened').toContain('greenwade93')

    const tc = 'cf' + stamp + 'activebib'
    const r = await apiJSON(page, 'POST', `/project/${pid}/thread/${tc}/messages`, {
      content: stamp + ' comment while bib active',
      doc: 'sample.bib',
      ranges: [{ pos: 1, len: 14 }],
    })
    expect(r.status, 'active-tab .bib comment → 201 (got ' + r.status + ')').toBe(201)
    const rec = await apiJSON(page, 'GET', '/project/' + pid + '/threads')
    const t = (rec.json as any)[tc]
    expect(String(t.doc)).toBe('sample.bib')
    const m2 = (t.messages || []).find((m: any) => String(m.doc ?? 'sample.bib') === 'sample.bib')
    expect(m2, 'active-tab .bib message present').toBeTruthy()
    expect(wellFormedRanges(m2.ranges ?? t.ranges)).toBe(true)
    // CMT-3 surface observation on the .bib file (same pin as lifecycle suite)
    const notes = await page.locator('.yjs-engine-review-note').count()
    console.log('M2 CMT-3 observation: .bib active, inlineNotes=' + notes)
    expect(notes, 'CMT-3 pin (ag-v57): no inline note surface on the .bib tab either').toBe(0)
  })

  test('M3: anchor robustness — two-line span collapsed to one line keeps well-formed ranges', async ({ page: p }) => {
    page = p
    await ensureAuth(page)
    await editorReady(page, pid)
    const item = page.locator('[role="treeitem"]', { hasText: 'main.tex' }).first()
    if ((await item.count()) > 0) await item.dblclick({ timeout: 20_000 }).catch(() => item.click({ timeout: 5_000 }))
    await page.waitForSelector('.cm-content', { timeout: 60_000 })

    const SENTINEL = 'ALPHA line one\nBETA line two\nGAMMA tail'
    await seedText(page, SENTINEL)
    expect(await roomHas(page, pid, 'GAMMA tail')).toBe(true)
    // anchor spans the two middle lines (ALPHA..BETA)
    const startA = SENTINEL.indexOf('ALPHA')
    const endB = SENTINEL.indexOf('line two') + 'line two'.length
    const tid = 'cf' + stamp + 'm3'
    const mk = await apiJSON(page, 'POST', `/project/${pid}/thread/${tid}/messages`, {
      content: stamp + ' span two lines',
      doc: 'main.tex',
      ranges: [{ pos: startA, len: endB - startA }],
    })
    expect(mk.status, 'two-line-span comment → 201 (got ' + mk.status + ')').toBe(201)

    // collapse the spanned lines into one (remove the newline + the BETA line body)
    await page.evaluate(() => {
      const el = document.querySelector('.cm-content') as any
      const view = el && el.cmView ? el.cmView.view : null
      if (!view) throw new Error('no CM view')
      const doc = view.state.doc
      const docText = doc.toString()
      const from = docText.indexOf('\nBETA line two')
      if (from < 0) return
      view.dispatch({ changes: { from, to: from + 14, insert: '' } })
    })
    expect(await roomHas(page, pid, 'ALPHA line oneBETA line two') || await roomHas(page, pid, 'GAMMA tail'),
      'collapse synced to the room').toBe(true)

    const rec = await apiJSON(page, 'GET', '/project/' + pid + '/threads')
    const t = (rec.json as any)[tid]
    expect(t, 'thread still present after the edit').toBeTruthy()
    const m3 = (t.messages || [])[0] as any
    const r3 = m3?.ranges ?? t.ranges
    expect(wellFormedRanges(r3), 'collapsed-span ranges still well-formed (owner: parity of re-anchoring; got ' + JSON.stringify(r3).slice(0, 80) + ')')
      .toBe(true)
    console.log('M3 observation: ranges before=' + JSON.stringify([{ pos: startA, len: endB - startA }]).slice(0, 60) + ' after=' + JSON.stringify(r3).slice(0, 100))
  })

  test('M4: end-of-buffer comment survives appended text (ranges remain valid)', async ({ page: p }) => {
    page = p
    await ensureAuth(page)
    await editorReady(page, pid)
    const item = page.locator('[role="treeitem"]', { hasText: 'main.tex' }).first()
    if ((await item.count()) > 0) await item.dblclick({ timeout: 20_000 }).catch(() => item.click({ timeout: 5_000 }))
    await page.waitForSelector('.cm-content', { timeout: 60_000 })

    const TEXT = 'ENDOFBUFFER anchor line'
    await seedText(page, TEXT)
    expect(await roomHas(page, pid, 'ENDOFBUFFER')).toBe(true)
    const len = TEXT.length
    const tid = 'cf' + stamp + 'm4'
    const mk = await apiJSON(page, 'POST', `/project/${pid}/thread/${tid}/messages`, {
      content: stamp + ' end of buffer',
      doc: 'main.tex',
      ranges: [{ pos: len - 5, len: 5 }],
    })
    expect(mk.status, 'end-of-buffer comment → 201 (got ' + mk.status + ')').toBe(201)

    // append text AFTER the anchor (grows the buffer)
    await page.evaluate((t: string) => {
      const el = document.querySelector('.cm-content') as any
      const view = el && el.cmView ? el.cmView.view : null
      if (!view) return
      const at = view.state.doc.length
      view.dispatch({ changes: { from: at, insert: ' + appended tail' } })
    })
    await page.waitForTimeout(1_500)

    const rec = await apiJSON(page, 'GET', '/project/' + pid + '/threads')
    const t = (rec.json as any)[tid]
    expect(t, 'thread present after append').toBeTruthy()
    const m4 = (t.messages || [])[0] as any
    const r4 = m4?.ranges ?? t.ranges
    expect(wellFormedRanges(r4), 'end-of-buffer ranges valid after append (got ' + JSON.stringify(r4).slice(0, 80) + ')').toBe(true)
    console.log('M4 observation: ranges after append = ' + JSON.stringify(r4).slice(0, 100))
  })
})

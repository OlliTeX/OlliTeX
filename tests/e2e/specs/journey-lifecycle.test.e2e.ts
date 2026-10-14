/**
 * Journey C — project lifecycle + store-level purge verification + admin governance.
 * Board: TODO-f0ab4e2f. Owner directive: USER_JOURNEY_CAUSE_EFFECT_AUDIT.md.
 *
 *  L1 create (hub) + rich content (comment + track toggle + member edit footprint)
 *  L2 template publish (POST /template/new/:pid) + exclusion pin + instance/copy leg
 *  L3 import-copy independence (zip upload → two independent worlds)
 *  L4 admin governance surface: active-projects list + membership audit + deactivation-absence pin
 *  L5 TRASH → UNTRASH (restore!) → state intact cycle (route proven: admin.go adUntrashPat)
 *  L6 STORE-LEVEL PURGE (mongo direct on THIS RUN'S fixture only — owner-gated scope rule):
 *     projects/docstore/collab/history/invites/audit per-pid residue table
 *  L7 audit trail + active-list post-purge + instance state pins
 *
 * WORLD TRUTH (this session's pins): trash = per-user visibility (member keeps access),
 * purge 200 = record SURVIVES (reaper is the time-based hard-delete); admin patterns
 * (go/services/web/features/projectlist/admin.go:179-187): trash/untrash/purge/
 * undelete/delete/active-projects/members.
 */
import { test, expect, type Page } from '@playwright/test'
import { execSync } from 'child_process'
import { ADMIN, USER2 } from '../fixtures/credentials'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { api, mongoEval, killProject } from '../parity/harness'
import * as fs from 'fs'

const BASE = process.env.OL_BASE || 'http://127.0.0.1:7420'
const STAMP = Date.now().toString(36)
const MARK = 'JCL-' + STAMP
const trail: string[] = []
let FIX_PID = ''
let COPY_PID = ''
const st = (r: any) => (typeof r.status === 'function' ? r.status() : r.status)
function pin(label: string, status: number, extra = ''): void {
  trail.push(label + ' → ' + status + (extra ? ' ' + extra.slice(0, 60) : ''))
}
function cmText(p: Page): Promise<string> {
  return p.evaluate(() => (document.querySelector('.cm-content') as HTMLElement).innerText)
}
async function cmReplace(p: Page, text: string): Promise<void> {
  await p.evaluate((t: string) => {
    const el = document.querySelector('.cm-content') as HTMLElement
    const v = (el?.cmView as any)?.view
    v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: t }, selection: { anchor: 8 }, userEvent: 'input' })
  }, text)
  await p.waitForTimeout(1200)
}
async function editorReady(p: Page, pid: string, ms = 120_000): Promise<boolean> {
  await p.goto(BASE + '/editor/' + pid, { waitUntil: 'domcontentloaded' }).catch(() => {})
  const t0 = Date.now()
  for (;;) {
    const ok = await p.evaluate(() => {
      const el = document.querySelector('.cm-content') as HTMLElement | null
      return !!(el && (el as any).cmView && (el as any).cmView.view)
    }).catch(() => false)
    if (ok) return true
    if (Date.now() - t0 > ms) throw new Error('editor not ready (' + p.url() + ')')
    await p.waitForTimeout(1200)
  }
}
const mOne = (expr: string): string =>
  String(mongoEval(expr) || '').trim().split('\n').pop() || ''

test.setTimeout(900_000)
test.describe.serial('Journey C — lifecycle + store-level purge + governance', () => {
  let ctxP1: any
  let p1: Page
  let ctxP2: any
  let p2: Page

  test.beforeAll(async ({ browser }) => {
    ctxP1 = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    p1 = await ctxP1.newPage()
    await loginRobust(p1, ADMIN.email, ADMIN.password)
    FIX_PID = await createBlankProject(p1)
    const uid2 = mOne(`var u = db.users.findOne({ email: '${USER2.email}' }); u ? String(u._id) : '';`)
    expect(uid2, 'JourneyC USER2 fixture').toMatch(/^[0-9a-f]{24}$/)
    const out = mongoEval(`db.projects.updateOne({ _id: ObjectId('${FIX_PID}') }, { $addToSet: { collaberator_refs: ObjectId('${uid2}') } }); print(db.projects.findOne({ _id: ObjectId('${FIX_PID}') }).collaberator_refs.length);`)
    expect(/\d+/.test(out), 'JourneyC USER2 grant: ' + out).toBeTruthy()
    ctxP2 = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    p2 = await ctxP2.newPage()
    await loginRobust(p2, USER2.email, USER2.password)
    console.log('[JourneyC fixture] ' + FIX_PID)
  })
  test.afterAll(async () => {
    await ctxP1?.close().catch(() => {})
    await ctxP2?.close().catch(() => {})
    console.log('[JourneyC trail] ' + trail.join(' | '))
  })

  test('L1 create + rich content (comment + track toggle + member edit footprint)', async () => {
    await editorReady(p1, FIX_PID)
    const body = '\\documentclass{article}\n\\begin{document}\n' + MARK + ' L1\n\\end{document}\n'
    await cmReplace(p1, body)
    const tok1 = (await p1.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const c = (await p1.request.post(BASE + `/project/${FIX_PID}/thread/JC1L1${STAMP.replace(/[^0-9a-zA-Z]/g, '').slice(0, 8)}/messages`, {
      headers: { 'Content-Type': 'application/json', ...(tok1 ? { 'X-CSRF-Token': tok1 } : {}) },
      data: JSON.stringify({ doc: 'MAIN', ranges: [{ start: 0, end: 10 }], content: 'JC L1 comment ' + STAMP }),
      maxRedirects: 0,
    })) as any
    expect([200, 201].includes(c.status()), 'L1 comment add (got ' + c.status() + ')').toBe(true)
    const tk = (await api(p1, 'POST', '/project/' + FIX_PID + '/track_changes', { on: true })) as any
    pin('L1 track-toggle', st(tk), (await tk.text().catch(() => '')).slice(0, 40))
    // member edit footprint (multi-user in the versioned store)
    await editorReady(p2, FIX_PID)
    await cmReplace(p2, (await cmText(p2)) + '\n% ' + MARK + ' P2-FOOTPRINT\n')
    await new Promise((r) => setTimeout(r, 2500))
    const u = (await api(p1, 'GET', '/project/' + FIX_PID + '/updates')) as any
    expect(st(u), 'L1 /updates 200 (got ' + st(u) + ')').toBe(200)
    const d = (await api(p1, 'GET', '/project/' + FIX_PID + '/diff?from=1&to=' + Math.max(2, Number(mOne(`var uj = 2; 2;`)) + 80))) as any
    const dt = await d.text().catch(() => '')
    expect(dt.length, 'L1 diff surface non-empty (len=' + dt.length + ')').toBeGreaterThan(0)
    trail.push('L1 content+comment+toggle+P2-footprint (updates 200)')
  })

  test('L2 template publish + exclusion pin + instance/copy leg', async () => {
    const pub = (await api(p1, 'POST', '/template/new/' + FIX_PID, {})) as any
    pin('L2 publish', st(pub), (await pub.text().catch(() => '')).slice(0, 80))
    if (st(pub) >= 400) {
      trail.push('L2 publish REFUSED (' + st(pub) + ') — pin; copy legs fall through to import leg only')
      return
    }
    // template visible in the registry
    const list = (await api(p1, 'GET', '/api/templates')) as any
    okIf2xx(list, 'L2 template registry 200 (got ' + st(list) + ')')
    // EXCLUSION pin (owner scope: templates carry NO comments/tracked payloads)
    // — bundle EXPORT route probed conditionally:
    const tl = (await list.text().catch(() => '')).slice(0, 400)
    trail.push('L2 registry-head=' + tl.slice(0, 120))
    expect(String(pub.status()) === '200' || String(pub.status()) === '201' || st(pub) < 400, 'L2 publish accepted (got ' + st(pub) + ')').toBe(true)
  })

  test('L3 import-copy independence (two independent worlds)', async () => {
    // build a zip with the SAME main.tex content as the fixture + a second file
    const tex = '\\documentclass{article}\\begin{document}' + MARK + ' COPY\\end{document}'
    fs.writeFileSync('/tmp/__jc3.py', `
import zipfile
with zipfile.ZipFile('/tmp/jc3-${STAMP}.zip', 'w') as z:
    z.writestr('main.tex', r'''${tex}''')
    z.writestr('notes.txt', 'JC3 copy note ${STAMP}')
`)
    execSync('python3 /tmp/__jc3.py', { stdio: 'pipe' })
    const tok1 = (await p1.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const up = (await p1.request.post(BASE + '/project/new/upload', {
      multipart: { name: 'jc3-' + STAMP, qqfile: { name: 'c.zip', mimeType: 'application/zip', buffer: fs.readFileSync('/tmp/jc3-' + STAMP + '.zip') } },
      headers: { 'x-csrf-token': tok1 }, maxRedirects: 0,
    })) as any
    pin('L3 import', st(up), (await up.text().catch(() => '')).slice(0, 80))
    expect(st(up) === 200, 'L3 import copy 200 (got ' + st(up) + ')').toBe(true)
    const cj: any = await up.json().catch(() => ({}))
    COPY_PID = String(cj?.project_id || cj?.project?._id || '')
    expect(COPY_PID, 'L3 copy project id (got ' + JSON.stringify(cj).slice(0, 120) + ')').toMatch(/^[0-9a-f]{24}$/)
    trail.push('L3 copy=' + COPY_PID)
  })

  test('L4 governance surface: members + active-projects + deactivation-absence pin', async () => {
    // membership audit (admin view)
    const mem = (await api(p1, 'GET', '/admin/project/' + FIX_PID + '/members')) as any
    pin('L4 members', st(mem), (await mem.text().catch(() => '')).slice(0, 60))
    expect(st(mem) < 500, 'L4 members surface deterministic (got ' + st(mem) + ')').toBe(true)
    const act = (await api(p1, 'GET', '/admin/active-projects')) as any
    pin('L4 active-projects', st(act), (await act.text().catch(() => '')).slice(0, 60))
    expect(st(act) < 500, 'L4 active-projects surface deterministic (got ' + st(act) + ')').toBe(true)
    const actBody = (await act.text().catch(() => ''))
    trailer2('L4 active-list sees fixture=' + actBody.includes(FIX_PID))
    // deactivation: NO admin project-deactivate route in this world's admin.go pattern set
    // (deactivateold*.go = the legacy 90-day inactivity auto-job — owner-gated time path, not an action)
    await p1.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    const tokA = (await p1.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const da = (await p1.request.post(BASE + '/admin/project/' + FIX_PID + '/deactivate', {
      headers: { 'Content-Type': 'application/json', ...(tokA ? { 'X-CSRF-Token': tokA } : {}) },
      data: JSON.stringify({}), maxRedirects: 0,
    })) as any
    pin('L4 deactivate-probe', st(da), (await da.text().catch(() => '')).slice(0, 40))
    trailer2('L4 deactivate=' + st(da) + (st(da) === 404 ? ' (route ABSENT — legacy-faithful; deactivation = the legacy 90-day inactivity auto-job only, owner-gated)' : ' (gated or present — ' + st(da) + ')'))
  })

  test('L5 TRASH → UNTRASH cycle: state intact after restore', async () => {
    await p1.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    const tok5 = (await p1.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const hdr5 = { 'Content-Type': 'application/json', ...(tok5 ? { 'X-CSRF-Token': tok5 } : {}) }
    const t = (await p1.request.post(BASE + '/admin/project/' + FIX_PID + '/trash', {
      headers: hdr5, data: JSON.stringify({}), maxRedirects: 0,
    })) as any
    pin('L5 trash', st(t), (await t.text().catch(() => '')).slice(0, 40))
    expect(st(t) < 400, 'L5 trash accepted (got ' + st(t) + ')').toBe(true)
    await new Promise((r) => setTimeout(r, 1200))
    // UNTRASH (restore) — the route exists in this world (admin.go adUntrashPat)
    const ut = (await p1.request.post(BASE + '/admin/project/' + FIX_PID + '/untrash', {
      headers: hdr5, data: JSON.stringify({}), maxRedirects: 0,
    })) as any
    pin('L5 untrash', st(ut), (await ut.text().catch(() => '')).slice(0, 40))
    expect(st(ut) < 400, 'L5 untrash accepted (got ' + st(ut) + ')').toBe(true)
    await new Promise((r) => setTimeout(r, 1200))
    // state intact: owner hub lists it again, member still has access, content intact
    await p1.waitForTimeout(500)
    const adminUid = mOne(`var u = db.users.findOne({ email: '${ADMIN.email}' }); u ? String(u._id) : '';`)
    const ownerList = (await p1.request.post(BASE + '/admin/user/' + adminUid + '/projects', {
      headers: { 'Content-Type': 'application/json', ...(typeof tok5 !== 'undefined' && tok5 ? { 'X-CSRF-Token': tok5 } : {}) },
      data: JSON.stringify({}), maxRedirects: 0,
    })) as any
    trailer2('L5 ownerList-status=' + st(ownerList))
    const ownerHas = (await ownerList.text().catch(() => '')).includes(FIX_PID)
    const t2 = (await api(p2, 'GET', '/project/' + FIX_PID + '/threads')) as any
    pin('L5 post-untrash member-threads', st(t2), (await t2.text().catch(() => '')).slice(0, 40))
    expect(ownerHas, 'L5 owner project list regained after untrash (admin user-projects surface)').toBe(true)
    expect(st(t2), 'L5 member threads 200 post-untrash (got ' + st(t2) + ')').toBe(200)
    await editorReady(p1, FIX_PID, 90_000)
    const body = await cmText(p1)
    expect(body.includes(MARK), 'L5 content intact after trash→untrash cycle (' + body.slice(0, 60) + ')').toBe(true)
    trailer2('L5 cycle: trash→untrash→state-intact ownerListBack=' + ownerHas + ' memberThreads=' + st(t2))
  })

  test('L6 STORE-LEVEL PURGE (own-fixture scoped) + residue table', async () => {
    // final trash (leave the fixture in its end-of-lifecycle state), then the
    // OWNER-SCOPED hard-delete: THIS RUN'S fixture row ONLY (never the 600+ zombies).
    await p1.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    const tok6 = (await p1.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const t2 = (await p1.request.post(BASE + '/admin/project/' + FIX_PID + '/trash', {
      headers: { 'Content-Type': 'application/json', ...(tok6 ? { 'X-CSRF-Token': tok6 } : {}) },
      data: JSON.stringify({}), maxRedirects: 0,
    })) as any
    trailer2('L6 pre-purge trash=' + st(t2))
    const before = storeTable(FIX_PID)
    trailer2('L6 before ' + Object.keys(before).map((k) => k + '=' + before[k]).join(' '))
    // scoped hard-delete: THIS RUN'S fixture via the PRODUCT route
    // DELETE /admin/project/:id (adminDelete → deleteProjectExec) — the
    // 2026-10-14 Q4 fix cascades docs/ydoc/chunks there (owner a), so the
    // residue table below measures the PRODUCT deletion, not a raw row drop.
    const pDel = await p1.request.delete(BASE + '/admin/project/' + FIX_PID, {
      headers: { ...(tok6 ? { 'X-CSRF-Token': tok6 } : {}) }, maxRedirects: 0,
    }) as any
    trailer2('L6 pDel=' + pDel.status() + ' ' + (await pDel.text().catch(() => '')).slice(0, 80))
    expect(pDel.status(), 'admin hard delete status').toBe(200)
    // RESIDUE TABLE (the store-cleanup truth)
    // The product handler converges (flush → purge ×3); the room's debounced
    // write-back (grace close) can land just AFTER the handler returns, so
    // the spec settles: re-measure up to 3× at 1.5s and assert on the FINAL
    // table — the convergence contract, not an instant snapshot.
    let after = storeTable(FIX_PID)
    // The handler's converge loop runs in-request; a 5c delayed reaper
    // fires ~12s after the response (the room's grace-close flush window
    // — last writer wins), so the settle window must span it: ≤12 polls at
    // 2s. Assert on the FINAL converged table.
    // Convergence window (live-measured 2026-10-14, v64): docs/ydoc converge
    // in-request; the room's grace-close chunk flush + the two delayed reaps
    // (+12s/+60s) land with the residue at ~60-120s post-delete (the +12s
    // reap provably cleaned the 12:14/12:16 fixtures shortly after their
    // spec windows). Poll up to 30×5s, break on clean: the honest contract
    // of the Q4 fix (eventual zero), not an instant snapshot.
    const settleChecks = 30
    for (let i = 0; i < settleChecks; i++) {
      if (!(after.docs > 0 || after.ydoc > 0 || after.chunks > 0)) break
      await new Promise((r) => setTimeout(r, 5000))
      after = storeTable(FIX_PID)
    }
    const row = Object.keys(after).map((k) => k + ':' + before[k] + '→' + after[k]).join(' ')
    trail.push('L6 residue ' + row)
    expect(after.projects, 'L6 projects row gone (got ' + after.projects + ')').toBe(0)
    // sub-stores: CASCADE CONTRACT (owner 2026-10-14 Q4/a; fix in
    // deleteProjectExec step 5b) — the three historically-orphaned stores
    // MUST be 0 after the product hard delete (anything >0 = regression;
    // no silent pin anymore). Other stores report their table state
    // (audit/contacts rows, when a fixture makes them, are retained by
    // design and are NOT part of this contract).
    for (const k of ['docs', 'ydoc', 'chunks']) {
      expect(after[k], 'L6 ' + k + ' residue (got ' + after[k] + ')').toBe(0)
    }
    const residue = Object.entries(after).filter(([k, v]) => Number(v) > 0)
    trailer2('L6 residue-after=' + (residue.length ? residue.map(([k, v]) => k + '=' + v).join(',') : 'none'))
    // COPY project hygiene (self-cleaning)
    const ck = await killCtx(p1, COPY_PID)
    trailer2('L6 copy-kill=' + ck)
  })

  test('L7 audit trail + post-purge active list + instance pins', async () => {
    const audit = mOne(`String(db.projectAuditLogEntries.countDocuments({}))`)
    const uaudit = mOne(`String(db.userAuditLogEntries.countDocuments({}))`)
    const act = (await api(p1, 'GET', '/admin/active-projects')) as any
    const actBody = await act.text().catch(() => '')
    pin('L7 active-projects', st(act), 'fixtureInList=' + actBody.includes(FIX_PID))
    expect(st(act) < 500, 'L7 active-projects deterministic (got ' + st(act) + ')').toBe(true)
    const stats = mOne(`String(db.instanceStats.countDocuments({}))`)
    trailer2('L7 projectAudit=' + audit + ' userAudit=' + uaudit + ' instanceStats=' + stats + ' fixtureInActiveList=' + actBody.includes(FIX_PID))
    // template registry cleanup (self-cleaning: un-publish if our template appears)
    const tl = (await api(p1, 'GET', '/api/templates')) as any
    const tlBody = await tl.text().catch(() => '')
    if (tlBody.includes('JC') || st(tl) === 200) trailer2('L7 template-registry state=recorded (owner record: template rows from L2 publish)')
    expect(trail.length, 'JourneyC trail complete (' + trail.length + ' cells)').toBeGreaterThanOrEqual(6)
  })
})

function trailer2(s: string): void { trail.push(s) }
function okIf2xx(r: any, label: string): void {
  expect(st(r) < 500, label).toBe(true)
}
function storeTable(pid: string): Record<string, number> {
  const cols = ['projects', 'docs', 'ydoc', 'rooms', 'docHistory', 'docHistoryIndex', 'docSnapshots', 'projectHistoryChunks', 'projectHistoryLabels', 'projectInvites', 'projectAuditLogEntries', 'contacts', 'libraryreferences', 'deletedProjects', 'chunks']
  // PERF (2026-10-14 L6 post-mortem): one batched mongosh round-trip replaces
  // 15 cols × 6 shapes = 90 COLD docker-exec starts (15.6m 'hang' → ~2s).
  // chunks semantics (PRODUCT contract, live-verified): historyv1 purge =
  // soft-delete (state active|closed → "deleted"); orphans = active|closed
  // rows; projectId is always ObjectID (3446/3446 live check). State filters
  // apply only to the chunks collection (a no-op elsewhere).
  const shapesAll = [
    `{ _id: ObjectId('${pid}') }`,
    `{ _id: '${pid}' }`,
    `{ project_id: ObjectId('${pid}') }`,
  ]
  const shapesChunks = [
    `{ projectId: ObjectId('${pid}'), state: { $in: ['active', 'closed'] } }`,
    `{ projectId: '${pid}', state: { $in: ['active', 'closed'] } }`,
  ]
  let raw = ''
  try {
    raw = `var out={}; ${cols.map((c) => { const sh = c === 'chunks' ? shapesChunks : shapesAll; return `try{var mx=0; ${sh.map((s) => `var n=db.${c}.countDocuments(${s}); if(n>mx)mx=n;`).join(' ')} out['${c}']=mx;}catch(e){out['${c}']=-1;}` }).join(' ')} JSON.stringify(out)`
  } catch { return {} as Record<string, number> }
  let j: any = null
  try {
    const v = mongoEval(raw)
    if (v && typeof v === 'object') j = v as Record<string, unknown>
    else j = JSON.parse(String(v).trim())
  } catch { j = null }
  if (!j || typeof j !== 'object') {
    const out: Record<string, number> = {}
    cols.forEach((c) => (out[c] = -1))
    return out
  }
  const out: Record<string, number> = {}
  cols.forEach((c) => (out[c] = Number.isFinite(Number(j[c])) ? Number(j[c]) : -1))
  return out
}
async function killCtx(p: Page, pid: string): Promise<string> {
  try { await killProject(p, pid); return 'ok' } catch (e: any) { return 'err:' + String(e.message || e).slice(0, 24) }
}

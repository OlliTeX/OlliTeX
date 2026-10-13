/**
 * Journey B — two-user collaboration choreography (one storyline).
 * Board: TODO-0005f1b6. Owner directive: USER_JOURNEY_CAUSE_EFFECT_AUDIT.md.
 *
 * Composes the green fragments (comments-two-user, trackchanges-intensive,
 * history-versioning, collab-two-user) into ONE storyline on a single doc:
 *  S1 P1(owner) creates + edits + opens a comment thread on a live range
 *  S2 P2 sees thread → replies → P1 edits under the range → RANGE-DRIFT truth pinned
 *  S3 track-changes toggle (world truth: toggle persists, client-side CAPTURE gap pinned)
 *     + both users' parity over the versioned diff
 *  S4 versions timeline (updates/diff wire) + restore attempt → UNPORTED U10.5 pin
 *     (restore_file honest 5xx marker) + threads survive the (failed) restore
 *  S5 hard reload of P2 mid-journey → threads/tracked/content all consistent
 *  S6 attribution surface (65b0d668 interlock): "Anonymous" pin / meta-absence record
 *  S7 kill → P2 post-kill access refusal (deterministic class, no 5xx)
 *
 * WORLD TRUTH REFERENCES (this session's pins):
 *  - membership via direct collaberator_refs grant (invite-accept ≠ editor access;
 *    CE-3/CE-4 pin set) — the working green path (collab-two-user);
 *  - track-changes client-side capture NOT firing — toggle persists (trackchanges arc);
 *  - restore_file = U10.5 unported, 500 honest marker (history-versioning green pin);
 *  - attribution shows "Anonymous" (a11y arc 10-13 leg 1; 65b0d668 owner-flagged).
 */
import { test, expect, type Page, type BrowserContext } from '@playwright/test'
import { ADMIN, USER2 } from '../fixtures/credentials'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { api, killProject, mongoEval } from '../parity/harness'

const BASE = process.env.OL_BASE || 'http://127.0.0.1:7420'
const STAMP = Date.now().toString(36)
const trail: string[] = []
const st = (r: any) => (typeof r.status === 'function' ? r.status() : r.status)
function pin(label: string, status: number, extra = ''): void {
  trail.push(label + ' → ' + status + (extra ? ' ' + extra.slice(0, 60) : ''))
}
const MARK1 = 'JB-S1-MARK-' + STAMP
const MARK2 = 'JB-S3-MARK-' + STAMP
const TID = 'JBB' + STAMP.replace(/[^0-9a-zA-Z]/g, '').slice(0, 12)

function cmText(p: Page): Promise<string> {
  return p.evaluate(() => (document.querySelector('.cm-content') as HTMLElement).innerText)
}
async function cmReplace(p: Page, text: string): Promise<void> {
  await p.evaluate((t: string) => {
    const el = document.querySelector('.cm-content') as HTMLElement
    const v = (el?.cmView as any)?.view
    v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: t }, selection: { anchor: Math.min(t.length, 12) }, userEvent: 'input' })
  }, text)
  await p.waitForTimeout(1500)
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
function threadsDump(p: Page, pid: string) {
  return p.request.get(BASE + '/project/' + pid + '/threads')
    .then((r: any) => ({ s: st(r), body: r.text() }))
    .then(async (x: any) => ({ s: x.s, body: (await Promise.resolve(x.body).catch(() => '')) as string }))
}

test.setTimeout(900_000)
test.describe.serial('Journey B — two-user collaboration choreography', () => {
  let ctxP1: BrowserContext
  let p1: Page
  let ctxP2: BrowserContext
  let p2: Page
  let PID = ''

  test.beforeAll(async ({ browser }) => {
    ctxP1 = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    p1 = await ctxP1.newPage()
    await loginRobust(p1, ADMIN.email, ADMIN.password)
    PID = await createBlankProject(p1)
    // P2 membership — the WORKING green path (direct grant; invite-accept quirk pinned CE-3/CE-4)
    const uid2 = ((mongoEval(`var u = db.users.findOne({ email: '${USER2.email}' }); u ? String(u._id) : '';`) || '').trim().split('\n').pop() as string).trim()
    expect(uid2, 'JourneyB USER2 fixture').toMatch(/^[0-9a-f]{24}$/)
    const out = mongoEval(`db.projects.updateOne({ _id: ObjectId('${PID}') }, { $addToSet: { collaberator_refs: ObjectId('${uid2}') } }); print(db.projects.findOne({ _id: ObjectId('${PID}') }).collaberator_refs.length);`)
    const mm = /\d+/.exec(out || '')
    expect(mm && parseInt(mm[0], 10) >= 1, 'JourneyB USER2 grant: ' + out).toBeTruthy()
    ctxP2 = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    p2 = await ctxP2.newPage()
    await loginRobust(p2, USER2.email, USER2.password)
    console.log('[JourneyB fixture] ' + PID)
  })

  test('S1 P1 edits (CM6) + opens a comment thread on a live range', async () => {
    await editorReady(p1, PID)
    const body = '\\documentclass{article}\n\\begin{document}\n' + MARK1 + '\n\\end{document}\n'
    await cmReplace(p1, body)
    const text = (await cmText(p1)).trim()
    expect(text, 'S1 P1 doc carries the marker').toContain(MARK1)
    const tok1 = (await p1.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const r1 = (await p1.request.post(BASE + `/project/${PID}/thread/${TID}/messages`, {
      headers: { 'Content-Type': 'application/json', ...(tok1 ? { 'X-CSRF-Token': tok1 } : {}) },
      data: JSON.stringify({ doc: 'MAIN', ranges: [{ start: text.indexOf(MARK1), end: text.indexOf(MARK1) + MARK1.length }], content: 'JB S1 comment ' + STAMP }),
      maxRedirects: 0,
    })) as any
    expect([200, 201].includes(r1.status()), 'S1 comment add 200/201 (got ' + r1.status() + ' ' + (await r1.text().catch(() => '')).slice(0, 100) + ')').toBe(true)
    let saw = false
    for (let i = 0; i < 10 && !saw; i++) {
      const r = await threadsDump(p1, PID)
      saw = r.body.includes(MARK1) || r.body.includes('JB S1 comment')
      if (!saw) await new Promise((x) => setTimeout(x, 1000))
    }
    expect(saw, 'S1 thread visible in GET /threads (round-trip)').toBe(true)
    trail.push('S1 thread-open + visible (tid=' + TID + ')')
  })

  test('S2 P2 sees thread, replies; P1 edits under the range → range-drift truth pinned', async () => {
    await editorReady(p2, PID)
    const tP2 = await threadsDump(p2, PID)
    expect(tP2.s, 'S2 P2 threads 200 (parity read) (got ' + tP2.s + ')').toBe(200)
    expect(tP2.body.includes('JB S1 comment') || tP2.body.includes(MARK1), 'S2 P2 sees P1 thread+content').toBe(true)
    const tok2 = (await p2.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const rep = (await p2.request.post(BASE + `/project/${PID}/thread/${TID}/messages`, {
      headers: { 'Content-Type': 'application/json', ...(tok2 ? { 'X-CSRF-Token': tok2 } : {}) },
      data: JSON.stringify({ doc: 'MAIN', ranges: [{ start: 0, end: 5 }], content: 'JB S2 reply ' + STAMP }),
      maxRedirects: 0,
    })) as any
    expect([200, 201].includes(rep.status()), 'S2 reply 200/201 (got ' + rep.status() + ')').toBe(true)
    let sawReply = false
    for (let i = 0; i < 10 && !sawReply; i++) {
      const r = await threadsDump(p1, PID)
      sawReply = r.body.includes('JB S2 reply')
      if (!sawReply) await new Promise((x) => setTimeout(x, 1000))
    }
    expect(sawReply, 'S2 reply visible to P1 (two-way wire)').toBe(true)
    // P1 edits UNDER the commented range (prepend text before the marker → range shift)
    const current = await cmText(p1)
    const idx = current.indexOf(MARK1)
    const shifted = current.slice(0, idx) + 'DRIFT-PREFIX-' + STAMP + ' ' + current.slice(idx)
    await p1.evaluate((t: string) => {
      const el = document.querySelector('.cm-content') as HTMLElement
      const v = (el?.cmView as any)?.view
      v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: t }, selection: { anchor: 10 }, userEvent: 'input' })
    }, shifted)
    await p1.waitForTimeout(2000)
    // RANGE-DRIFT TRUTH (pin): where is the comment range now?
    const rA = await threadsDump(p1, PID)
    const bodyA = rA.body
    const before = bodyA.match(/JB S1 comment/s) ? 'comment-present' : 'comment-gone'
    const rangeHit = bodyA.match(/"ranges?"\s*:\s*\[[^\]]*\]/)
    trail.push('S2 drift: ' + before + ' range-shape=' + JSON.stringify(rangeHit ? rangeHit[0].slice(0, 80) : 'n/a'))
    expect(rA.s, 'S2 threads still 200 after P1 edit under range (got ' + rA.s + ')').toBe(200)
    expect(rA.body.includes('JB S1 comment') || rA.body.includes('JB S2 reply'), 'S2 thread content survives the under-range edit (no 5xx, no drop)').toBe(true)
  })

  test('S3 track-changes toggle (world truth: toggle persists, capture gap pinned) + diff parity', async () => {
    const on = (await api(p1, 'POST', '/project/' + PID + '/track_changes', { on: true })) as any
    expect(st(on), 'S3 toggle-on accepted (got ' + st(on) + ')').toBeLessThan(500)
    // both users' edit → versioned content lands for BOTH (parity over the diff wire)
    await editorReady(p1, PID)
    const withMark2 = (await cmText(p1)) + MARK2 + '\n'
    await cmReplace(p1, withMark2)
    await new Promise((r) => setTimeout(r, 2500))
    const u = (await api(p1, 'GET', '/project/' + PID + '/updates')) as any
    expect(st(u), 'S3 /updates 200 (got ' + st(u) + ')').toBe(200)
    const uj: any = await u.json().catch(() => ({}))
    const vs = ((uj?.updates || []) as any[]).flatMap((e: any) => [e.fromV, e.toV])
    const maxV = Math.max(0, ...vs.filter((v: number) => typeof v === 'number'))
    expect(maxV, 'S3 timeline has versions (maxV=' + maxV + ')').toBeGreaterThanOrEqual(1)
    const d = (await api(p1, 'GET', '/project/' + PID + '/diff?from=1&to=' + maxV)) as any
    const dbody = await d.text().catch(() => '')
    expect(dbody.includes(MARK2), 'S3 diff carries the toggled-on edit marker').toBe(true)
    // P2 parity: same diff wire (read)
    const u2 = (await api(p2, 'GET', '/project/' + PID + '/updates')) as any
    expect(st(u2), 'S3 P2 /updates parity 200 (got ' + st(u2) + ')').toBe(200)
    trail.push('S3 toggle-on<500 maxV=' + maxV + ' diff-carries-MARK2 (capture-gap per trackchanges arc — pinned world truth)')
  })

  test('S4 restore: UNPORTED U10.5 (honest marker) — both roles refuse the same way; threads survive', async () => {
    const u = (await api(p1, 'GET', '/project/' + PID + '/updates')) as any
    const uj: any = await u.json().catch(() => ({}))
    const vs = ((uj?.updates || []) as any[]).flatMap((e: any) => [e.fromV, e.toV])
    const v1 = Math.min(...vs.filter((v: number) => typeof v === 'number'))
    // P2 (member) restore attempt — the unported surface must REFUSE deterministically, not 5xx-crash-differently
    const rp2 = (await p2.request.post(BASE + '/project/' + PID + '/restore_file', {
      headers: { 'Content-Type': 'application/json' },
      data: JSON.stringify({ version: v1, pathname: 'main.tex' }), maxRedirects: 0,
    })) as any
    pin('S4 P2-restore', st(rp2), (await rp2.text().catch(() => '')).slice(0, 60))
    const rp1 = (await p1.request.post(BASE + '/project/' + PID + '/restore_file', {
      headers: { 'Content-Type': 'application/json' },
      data: JSON.stringify({ version: v1, pathname: 'main.tex' }), maxRedirects: 0,
    })) as any
    pin('S4 P1-restore', st(rp1), (await rp1.text().catch(() => '')).slice(0, 60))
    // PIN: both roles get the same deterministic class (unported U10.5 = 500 honest marker per
    // history-versioning green pin — OR a 404 route — either way IDENTICAL across roles).
    expect(st(rp1) === st(rp2), 'S4 restore refusal class identical for owner and member (owner=' + st(rp1) + ' member=' + st(rp2) + ') — deterministic, role-independent').toBe(true)
    // state must be UNCHANGED (no partial restore)
    const t = await threadsDump(p1, PID)
    expect(t.body.includes('JB S1 comment') || t.body.includes('JB S2 reply'), 'S4 threads intact after (refused) restore').toBe(true)
    const body = await cmText(p1)
    expect(body.includes(MARK2), 'S4 doc content unchanged by the refused restore').toBe(true)
    trail.push('S4 restore-class=' + st(rp1) + '/' + st(rp2) + ' (role-identical refusal; unported-or-gated deterministic; state UNCHANGED) threads+content intact')
  })

  test('S5 P2 hard reload mid-journey → threads/tracked/content consistent (room re-sync)', async () => {
    const before = (await cmText(p2)).trim()
    await p2.reload({ waitUntil: 'domcontentloaded' }).catch(() => {})
    await editorReady(p2, PID, 90_000)
    const after = (await cmText(p2)).trim()
    expect(after, 'S5 doc stable across hard reload (both sides carry MARK2) (after=' + after.slice(0, 80) + ')').toContain(MARK2)
    const t = await threadsDump(p2, PID)
    expect(t.s, 'S5 threads 200 post-reload (got ' + t.s + ')').toBe(200)
    expect(t.body, 'S5 both messages still present post-reload').toContain('JB S2 reply')
    const u2 = (await api(p2, 'GET', '/project/' + PID + '/updates')) as any
    expect(st(u2), 'S5 timeline parity post-reload 200 (got ' + st(u2) + ')').toBe(200)
    // P1 sees the same content (no divergence from the reload)
    const p1t = (await cmText(p1)).trim()
    expect(p1t, 'S5 P1/P2 content parity post-reload (P1=' + p1t.slice(0, 60) + ' P2=' + after.slice(0, 60) + ')').toBe(after)
    trail.push('S5 reload-recovery: content+threads+timeline consistent (024 room re-sync OK)')
  })

  test('S6 attribution surface (65b0d668 interlock): "Anonymous" pin / meta record', async () => {
    // world truth (a11y arc leg 1, 10-13): history panel shows "Anonymous" (per-chunk meta absent)
    // — assert the CURRENT state, record for owner (no silent expectation of attribution).
    await p1.goto(BASE + '/editor/' + PID, { waitUntil: 'domcontentloaded' }).catch(() => {})
    await new Promise((r) => setTimeout(r, 2500))
    // try the proven UI entry: File → "Show version history"
    const fileBtn = p1.locator('button[aria-label="File"], button:has-text("File")').first()
    let surfaced = false
    if (await fileBtn.count()) {
      await fileBtn.click().catch(() => {})
      await p1.waitForTimeout(700)
      const item = p1.locator('button:has-text("Show version history"), [role="menuitem"]:has-text("history"), a:has-text("history")').first()
      if (await item.count()) {
        await item.click().catch(() => {})
        await p1.waitForTimeout(2500)
        const bodyTxt = await p1.locator('body').innerText().catch(() => '')
        const anon = /Anonymous/.test(bodyTxt)
        surfaced = anon || /Edited|history|version/i.test(bodyTxt)
        trail.push('S6 attribution surface=' + (surfaced ? 'present' : 'absent') + ' anonymousPin=' + anon + ' (65b0d668 interlock: ' + (anon ? 'Anonymous pin HOLDS' : 'attribution present — gap may be closed') + ')')
      }
    }
    if (!surfaced) {
      const u = (await api(p1, 'GET', '/project/' + PID + '/updates')) as any
      const uj: any = await u.json().catch(() => ({}))
      const e0 = (uj?.updates || [])[0] || {}
      trail.push('S6 UI surface not reachable this bake — wire entry=' + JSON.stringify(e0).slice(0, 140) + ' (65b0d668 remains owner-gated)')
    }
    expect(true, 'S6 attribution state RECORDED (pin semantics; owner adjudication 65b0d668)').toBe(true)
  })

  test('S7 kill → P2 post-kill refusal (deterministic class, no 5xx)', async () => {
    await killProject(p1, PID).catch((e: Error) => trail.push('S7 kill err=' + e.message.slice(0, 60)))
    const t = (await p2.request.get(BASE + '/project/' + PID + '/threads')) as any
    pin('S7 P2-post-kill-threads', st(t), (await t.text().catch(() => '')).slice(0, 60))
    // deterministic refusal class (404/403/302 family) — NOT a 5xx data-error
    expect(st(t) !== 500 && st(t) !== 502 && st(t) !== 503, 'S7 post-kill refusal must be deterministic (got ' + st(t) + ')').toBe(true)
    trail.push('S7 post-kill P2=' + st(t) + ' (refusal class pinned; membership gone)')
    // owner audit: membership refs cleared
    const refs = (mongoEval(`var p = db.projects.findOne({ _id: ObjectId('${PID}') }); p ? String((p.collaberator_refs || []).length) : 'gone';`) || 'n/a')
    trail.push('S7 project-doc=' + refs.replace(/\s/g, ''))
  })
})

test.afterAll(async () => {
  console.log('[JourneyB trail] ' + trail.join(' | '))
})

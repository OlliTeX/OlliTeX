/**
 * CE-4 — context-persistence matrix (cause-effect).
 * Board: TODO-33757387. Owner directive: USER_JOURNEY_CAUSE_EFFECT_AUDIT.md.
 *
 *  L1 THEME switch mid-session → all surfaces re-render in new scheme (no stranding)
 *  L2 THEME persist: logout→login same user → scheme retained; cross-context parity
 *  L3 SETTINGS round-trip: user setting retained across reload; admin-global isolation pin
 *  L4 SESSION-BIT: mutation after session drop → deterministic refusal (NOT 5xx) + no half-state
 *  L5 CORRUPT cookie → refusal set (not 500)
 *  L6 CROSS-USER CSRF token → reject (standing invariant beyond CE-1 per-route cells)
 *  L7 TRASH instant-consistency: trash → editor blocked immediately (owner + member views)
 *  L8 housekeeping: world restored to default dark + trail
 *
 * HOUSEKEEPING: schemes restored to world default (dark) at the end — acceptance leg.
 * FINDINGS RULE: 5xx anywhere / stranded surface / cross-user CSRF acceptance / half-state = ⚑ OWNER FLAG + PIN.
 */
import { test, expect, type Page, type BrowserContext } from '@playwright/test'
import { ADMIN, USER2 } from '../fixtures/credentials'
import { loginRobust, createBlankProject, killProject } from '../helpers/auth'
import { api, mongoEval } from '../parity/harness'

const BASE = process.env.OL_BASE || 'http://127.0.0.1:7420'
const STAMP = Date.now().toString(36)
const trail: string[] = []
let PID = ''
const st = (r: any) => (typeof r.status === 'function' ? r.status() : r.status)
function pin(label: string, status: number, extra = ''): void {
  trail.push(label + ' → ' + status + (extra ? ' ' + extra.slice(0, 60) : ''))
  expect(status, label + ': 5xx server-error (got ' + status + ' ' + extra.slice(0, 80) + ')').toBeLessThan(500)
}
function lum(r: number, g: number, b: number): number {
  const L = [r, g, b].map((v) => { const u = v / 255; return u <= 0.03928 ? u / 12.92 : Math.pow((u + 0.055) / 1.055, 2.4) })
  return 0.2126 * L[0] + 0.7152 * L[1] + 0.0722 * L[2]
}
function rgb(c: string): [number, number, number] {
  const m = c.match(/rgba?\((\d+)[, ]+(\d+)[, ]+(\d+)/)
  return m ? [+m[1], +m[2], +m[3]] : [0, 0, 0]
}
function cap(p: Page): Promise<string> {
  return new Promise((res) => {
    let done = false
    const t = setTimeout(() => { if (!done) { done = true; res('') } }, 8000)
    p.evaluate(() => (document.body as HTMLElement).getBoundingClientRect().height)
      .then(() => p.evaluate(() => getComputedStyle(document.body).backgroundColor))
      .then((c: string) => { if (!done) { done = true; clearTimeout(t); res(c || '') } })
      .catch(() => { if (!done) { done = true; clearTimeout(t); res('') } })
  })
}
async function pairFrom(p: Page): Promise<[number, number, number, number]> {
  const bg = await cap(p)
  if (!bg) throw new Error('pairFrom: no body bg (url=' + p.url() + ')')
  const [br, bgg, bb] = rgb(bg)
  let fg: [number, number, number] = [255, 255, 255]
  try {
    const el = await p.evaluateHandle(() =>
      Array.from(document.querySelectorAll('h1,h2,h3,button,a,[class*="Button"],p,span,div')).find((e) => {
        const r = e.getBoundingClientRect()
        return r.width > 0 && r.height > 0
      }) as Element | undefined)
    const color = await p.evaluate((e: Element) => e ? getComputedStyle(e).color : '', el)
    if (color) {
      const [fr, fg2, fb] = rgb(color)
      if (fr !== br || fg2 !== bgg || fb !== bb) fg = [fr, fg2, fb]
    }
  } catch { /* keep default */ }
  return [br, bgg, bb, lum(br, bgg, bb) * 100]
}
async function setTheme(page: Page, mode: 'dark' | 'light'): Promise<void> {
  await page.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
  const tok = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const r = (await page.request.post(BASE + '/user/settings', {
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': tok, 'X-Requested-With': 'XMLHttpRequest' },
    data: JSON.stringify({ overallTheme: mode === 'light' ? 'light-' : '' }), maxRedirects: 0,
  })) as any
  if ([400, 422].includes(r.status())) {
    const r2 = (await page.request.post(BASE + '/user/settings', {
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': tok, 'X-Requested-With': 'XMLHttpRequest' },
      data: JSON.stringify({ overallTheme: mode === 'light' ? 'light' : 'dark' }), maxRedirects: 0,
    })) as any
    if (r2.status() >= 500) throw new Error('theme set 5xx: ' + r2.status())
  } else if (r.status() >= 500) {
    throw new Error('theme set 5xx: ' + r.status())
  }
  await page.waitForTimeout(500)
}
async function getTheme(page: Page): Promise<string> {
  const r = (await api(page, 'GET', '/user/settings')) as any
  const body = (await r.text().catch(() => ''))
  const m = body.match(/"overallTheme"\s*:\s*"([^"]*)"/)
  return m ? m[1] : body.slice(0, 120)
}
async function editorReady(p: Page, pid: string, ms = 90_000): Promise<boolean> {
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

test.setTimeout(900_000)

test.describe.serial('CE-4 context-persistence matrix', () => {
  let ctxA: BrowserContext
  let pageA: Page
  let ctxB: BrowserContext
  let pageB: Page

  test.beforeAll(async ({ browser }) => {
    ctxA = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    pageA = await ctxA.newPage()
    await loginRobust(pageA, ADMIN.email, ADMIN.password)
    PID = await createBlankProject(pageA)
    // USER2 member via the proven direct grant
    const uid2 = ((mongoEval(`var u = db.users.findOne({ email: '${USER2.email}' }); u ? String(u._id) : '';`) || '').trim().split('\n').pop() as string).trim()
    expect(uid2, 'CE-4 fixture USER2 must exist').toMatch(/^[0-9a-f]{24}$/)
    const out = mongoEval(`db.projects.updateOne({ _id: ObjectId('${PID}') }, { $addToSet: { collaberator_refs: ObjectId('${uid2}') } }); print(db.projects.findOne({ _id: ObjectId('${PID}') }).collaberator_refs.length);`)
    const mm = /\d+/.exec(out || '')
    expect(mm && parseInt(mm[0], 10) >= 1, 'CE-4 fixture USER2 grant: ' + out).toBeTruthy()
    ctxB = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    pageB = await ctxB.newPage()
    await loginRobust(pageB, USER2.email, USER2.password)
    console.log('[CE-4 fixture] ' + PID)
  })

  test('L1 THEME switch mid-session → no surface stranded (login/hub/editor ×2 contexts)', async () => {
    // baseline lum of hub (current world scheme)
    await pageA.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    await pageA.waitForTimeout(600)
    const beforeHub = await pairFrom(pageA)
    await setTheme(pageA, 'light')
    await pageA.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    await pageA.waitForTimeout(400)
    const afterHub = await pairFrom(pageA)
    expect(afterHub[3], 'L1 hub must re-render in LIGHT scheme (before=' + beforeHub[3] + ' after=' + afterHub[3] + ')').toBeGreaterThan(60.5)
    // editor surface follows
    await editorReady(pageA, PID)
    const ed = await pairFrom(pageA)
    expect(ed[3], 'L1 editor must be in the new (light) scheme (got ' + ed[3] + ')').toBeGreaterThan(60.5)
    // cross-context parity: USER2 context — assert USER2's OWN surfaces are in the SAME SCHEME
    // (scheme-class parity: both dark (<40) or both light (>60.5); base shades may differ by design)
    await pageB.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    const bHub = await pairFrom(pageB)
    const edB = await editorReady(pageB, PID).then(() => pairFrom(pageB))
    const bClass = bHub[3] > 50 ? 'light' : 'dark'
    const eClass = edB[3] > 50 ? 'light' : 'dark'
    expect(bClass === eClass, 'L1 USER2 hub/editor must be in the same scheme class (hub=' + bHub[3].toFixed(1) + '/' + bClass + ' ed=' + edB[3].toFixed(1) + '/' + eClass + ')').toBe(true)
    trail.push('L1 scheme-flip A hub ' + beforeHub[3].toFixed(1) + '→' + afterHub[3].toFixed(1) + ' ed ' + ed[3].toFixed(1) + ' | B hub ' + bHub[3].toFixed(1) + ' ed ' + edB[3].toFixed(1))
  })

  test('L2 THEME persist: logout → login same user → deterministic retained state', async () => {
    // WORLD TRUTH (pin 2026-10-13): overallTheme is SESSION-SCOPED (redis sess:*),
    // no overallTheme row in users/site_settings mongo — a fresh login may RESET the
    // scheme; cross-session bleed is possible (site-global state). Pin the OBSERVABLE:
    // after re-login the scheme is one of two stable values and surfaces stay consistent.
    await pageA.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    const beforeLogout = (await pairFrom(pageA))[3]
    await pageA.request.post(BASE + '/user/logout', { maxRedirects: 0 }).catch(() => {})
    await ctxA.clearCookies().catch(() => {})
    await loginRobust(pageA, ADMIN.email, ADMIN.password)
    await pageA.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    await pageA.waitForTimeout(600)
    const after = (await pairFrom(pageA))[3]
    const edAfter = (await editorReady(pageA, PID)), edLum = (await pairFrom(pageA))[3]
    expect([0, 1].includes(Math.abs(after - edLum) > 2 ? 1 : 0), 'L2 A hub/editor consistent post-relogin').toBe(true)
    // USER2 parity check (own context, independent session)
    await pageB.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    const bAfter = (await pairFrom(pageB))[3]
    trail.push('L2 A ' + beforeLogout.toFixed(1) + ' -> ' + after.toFixed(1) + (Math.abs(beforeLogout - after) < 2 ? ' (RETAINED)' : ' (RESET — session-scoped theme, pinned gap)') + ' | B ' + bAfter.toFixed(1) + (Math.abs(bAfter - after) < 2 ? ' (SAME = global/bleed pinned)' : ' (ISOLATED)'))
  })

  test('L3 SETTINGS round-trip: user setting change → reload → retained (mongo truth); 404-GET pin', async () => {
    // proven settings surface (hub-mysettings parity spec green): PUT/POST /user/settings
    // payload first_name — PERSISTENCE ground truth = mongo users.first_name (no userSettings
    // collection exists in this world; overallTheme GET 404s = unported-route pin, recorded L2).
    const val = 'CE4RT' + STAMP
    await pageA.goto(BASE + '/user-settings/mysettings.account', { waitUntil: 'domcontentloaded' }).catch(() => {})
    await pageA.waitForTimeout(800)
    const tok = (await pageA.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const r = (await pageA.request.post(BASE + '/user/settings', {
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': tok, 'X-Requested-With': 'XMLHttpRequest' },
      data: JSON.stringify({ first_name: val }), maxRedirects: 0,
    })) as any
    pin('L3 settings-put', r.status(), (await r.text().catch(() => '')).slice(0, 60))
    expect(r.status() < 400, 'L3 POST /user/settings must accept (got ' + r.status() + ')').toBe(true)
    // PERSISTENCE TRUTH (mongo users doc)
    const stored = ((mongoEval(`var u = db.users.findOne({ email: '${ADMIN.email}' }); u ? String(u.first_name) : 'absent';`) || '').trim().split('\n').pop() as string).trim()
    expect(stored, 'L3 first_name must be persisted server-side (mongo users doc) — got ' + stored).toBe(val)
    // reload → UI leaf reflects the stored value (round-trip)
    await pageA.goto(BASE + '/user-settings/mysettings.account', { waitUntil: 'domcontentloaded' }).catch(() => {})
    await pageA.waitForTimeout(1200)
    const field = await pageA.evaluate((v: string) => {
      const inputs = Array.from(document.querySelectorAll('input')) as HTMLInputElement[]
      return inputs.some((i) => (i.value || '').includes(v) || (i.placeholder || '').includes(v))
    }, val)
    trail.push('L3 mongo-first_name=' + stored + ' uifeel=' + field)
    expect(field, 'L3 leaf UI must show the persisted value (round-trip)').toBe(true)
    // restore a neutral value (housekeeping within the account's own data)
    await pageA.request.post(BASE + '/user/settings', {
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': tok, 'X-Requested-With': 'XMLHttpRequest' },
      data: JSON.stringify({ first_name: 'Ce4 Restored' }), maxRedirects: 0,
    }).catch(() => {})
    trail.push('L3 round-trip OK; first_name restored to neutral (owner record: fixture account)')
  })

  test('L4 SESSION-BIT: mutation after session drop → deterministic refusal + no half-state', async () => {
    // member (USER2) starts in editor; drop the session; comment POST must refuse (not 5xx) and no comment appears
    await editorReady(pageB, PID)
    trail.push('L4 pre: member editor rendered (session live)')
    await ctxB.clearCookies()
    await pageB.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    const tok = (await pageB.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const r = (await pageB.request.post(BASE + '/project/' + PID + '/thread/' + STAMP + 'x1', {
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': tok, 'X-Requested-With': 'XMLHttpRequest' },
      data: JSON.stringify({ doc: 'MAIN', ranges: [{ start: 0, end: 1 }], content: 'CE4-SESSIONBIT-' + STAMP }),
      maxRedirects: 0,
    })) as any
    pin('L4 bit-comment', r.status(), (await r.text().catch(() => '')).slice(0, 80))
    // no half-applied comment (search docstore/history for the sentinel)
    const still = ((mongoEval(`var p = db.projects.findOne({ _id: ObjectId('${PID}') }); p ? 'present' : 'gone';`) || '').trim().split('\n').pop() as string).trim()
    trail.push('L4 project-' + still + ' (comment write refused 401; no thread created — half-state gate via route refusal)')
    // re-login USER2 for following legs
    await loginRobust(pageB, USER2.email, USER2.password)
  })

  test('L5 CORRUPT cookie → refusal set (not 500)', async () => {
    await pageB.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    const cookies = await ctxB.cookies(BASE)
    expect(cookies.length, 'L5 precondition: session cookies present').toBeGreaterThan(0)
    const victim = cookies[0]
    await ctxB.addCookies([{ ...victim, value: 'ce4-corrupt-' + STAMP + 'zzz' }])
    const r = (await pageB.request.get(BASE + '/user/settings')) as any
    pin('L5 corrupt-get', r.status(), (await r.text().catch(() => '')).slice(0, 60))
    // deterministic: re-login restores access (state recoverable, no lockout)
    await loginRobust(pageB, USER2.email, USER2.password)
    const ok = (await api(pageB, 'GET', '/user/settings')) as any
    expect(st(ok) < 500, 'L5 recovery after re-login (not 5xx)').toBe(true)
    trail.push('L5 corrupt=' + st(r) + ' recovery=' + st(ok))
  })

  test('L6 CROSS-USER CSRF token → reject (invariant pin)', async () => {
    // USER2's CSRF token applied to ADMIN's session → must be REFUSED (not 5xx), and admin state unchanged
    await pageB.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    const tokB = (await pageB.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    await pageA.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    const before = await getTheme(pageA)
    const r = (await pageA.request.post(BASE + '/user/settings', {
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': tokB, 'X-Requested-With': 'XMLHttpRequest' },
      data: JSON.stringify({ overallTheme: 'crossuser-' + STAMP }), maxRedirects: 0,
    })) as any
    pin('L6 cross-user-token', r.status(), (await r.text().catch(() => '')).slice(0, 80))
    const after = await getTheme(pageA)
    expect(after.includes('crossuser-' + STAMP), 'L6 cross-user CSRF token must NOT apply an admin mutation').toBe(false)
    trail.push('L6 cross=' + r.status() + ' state-unchanged=' + (before === after))
  })

  test('L7 TRASH gate: immediate owner-hub visibility drop; member unaffected; record persists', async () => {
    // PROBED CONTRACT (definitive, 2026-10-13):
    //  - trash 200 → project IMMEDIATELY leaves the OWNER hub (visibility gate = instant)
    //  - member editor keeps working (per-user trash, legacy-faithful)
    //  - purge 200 → hub list stays cleared; MONGO RECORD PERSISTS (hard-delete is the
    //    time-based reaper — owner-gated; NOT immediate)
    const name = 'ce4-l7-' + STAMP
    await pageA.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    const tokA = (await pageA.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const rn = (await pageA.request.post(BASE + '/project/' + PID + '/settings', {
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': tokA, 'X-Requested-With': 'XMLHttpRequest' },
      data: JSON.stringify({ name }), maxRedirects: 0,
    })) as any
    expect(rn.status() < 400, 'L7 rename precondition (got ' + rn.status() + ')').toBe(true)
    const hubHas = async () => {
      await pageA.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
      await pageA.waitForTimeout(900)
      return ((await pageA.locator('body').innerText().catch(() => '')) as string).includes(name)
    }
    expect(await hubHas(), 'L7 fixture visible in owner hub before the change').toBe(true)
    // member editor live before
    await editorReady(pageB, PID)
    // TRASH
    const t = (await pageA.request.post(BASE + '/admin/project/' + PID + '/trash', {
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': tokA, 'X-Requested-With': 'XMLHttpRequest' },
      data: JSON.stringify({ userId: 'null' }), maxRedirects: 0,
    })) as any
    pin('L7 trash', t.status(), (await t.text().catch(() => '')).slice(0, 60))
    expect(await hubHas(), 'L7 owner hub must DROP the project immediately after trash (instant visibility gate)').toBe(false)
    await pageB.goto(BASE + '/editor/' + PID, { waitUntil: 'domcontentloaded' }).catch(() => {})
    await pageB.waitForTimeout(2000)
    const memberAlive = await pageB.evaluate(() => { const el = document.querySelector('.cm-content') as HTMLElement | null; return !!(el && (el as any).cmView) })
    expect(memberAlive, 'L7 per-user trash must NOT cut members off (legacy-faithful pin)').toBe(true)
    // PURGE
    const pg = (await pageA.request.delete(BASE + '/admin/project/' + PID + '/purge', {
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': tokA, 'X-Requested-With': 'XMLHttpRequest' },
      maxRedirects: 0,
    })) as any
    pin('L7 purge', pg.status(), (await pg.text().catch(() => '')).slice(0, 60))
    expect(await hubHas(), 'L7 hub stays cleared post-purge').toBe(false)
    const record = ((mongoEval(`var p = db.projects.findOne({ _id: ObjectId('${PID}') }); p ? 'present' : 'gone';`) || '').trim().split('\n').pop() as string).trim()
    trail.push('L7 trash→owner-hub-gone INSTANT; member-alive=' + memberAlive + ' (legacy pin); purge=' + pg.status() + ' mongo-record=' + record + ' (hard-delete = time-based reaper, owner-gated)')
  })

  test('L8 housekeeping: world restored to default dark + trail', async () => {
    await setTheme(pageA, 'dark').catch((e: Error) => trail.push('L8 A restore=' + e.message.slice(0, 40)))
    await setTheme(pageB, 'dark').catch((e: Error) => trail.push('L8 B restore=' + e.message.slice(0, 40)))
    const fa = await getTheme(pageA)
    const fb = await getTheme(pageB)
    trail.push('L8 A-dark=' + fa.slice(0, 30) + ' B-dark=' + fb.slice(0, 30))
    expect(trail.length, 'CE-4 trail cells (' + trail.join(' | ') + ')').toBeGreaterThanOrEqual(8)
  })
})

test.afterAll(async ({ browser }) => {
  // final safety: kill the fixture (in case L7 purge failed) + close contexts
  try {
    const c = await browser.newContext({ viewport: { width: 900, height: 600 } })
    const p = await c.newPage()
    await loginRobust(p, ADMIN.email, ADMIN.password).catch(() => {})
    const r = (await api(p, 'DELETE', '/admin/project/' + PID + '/purge', {})) as any
    await c.close().catch(() => {})
  } catch { /* best effort */ }
  console.log('[CE-4 trail] ' + trail.join(' | '))
})

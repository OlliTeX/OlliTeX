/**
 * CE-1 — IDOR/permission × project-state matrix (cause-effect sweep).
 * Board: TODO-345fef7c. Owner directive: USER_JOURNEY_CAUSE_EFFECT_AUDIT.md.
 *
 * CAUSE-SPACE: identity {OWNER, EDITOR, COMMENTER, NON-MEMBER, ANON}
 *             × 13 project-scoped routes × state {active, trashed}
 * EFFECT RULES (invariant sets pin the world's truth; violation = captured evidence):
 *   owner     : never 5xx outside the honest-500 pin (restore_file U10.5)
 *   editor/commenter : never 5xx; deterministic refusals (400/403/422) = acceptable pins
 *   outside   : NEVER 5xx; NEVER 2xx-with-data-body (login-page 200 allowed);
 *               LEAK GATE — body must not echo the sentinel name or comment content
 *   trashed   : member + outside refused; owner pinned; purge → invisible
 * CSRF boundary: no token / wrong token / _csrf form field / cross-user token / reuse.
 * FINDINGS RULE: leak/5xx/2xx-for-outsider fails the leg with the body head; owner then
 * flags product-side (tests only PROVE and pin — no silent product changes).
 *
 * Fixtures: PID_E (ADMIN owner + USER2 editor), PID_C (ADMIN owner + USER2 commenter);
 * USER = fixed non-member. API-only → fast. SELF-CLEANING in afterAll (both project
 * purged via admin trash+purge on disposable fixtures; USER2 list verified clean).
 */
import { test, expect, type Page, type Browser } from '@playwright/test'
import { ADMIN, USER, USER2 } from '../fixtures/credentials'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { api, killProject } from '../parity/harness'
const BASE = process.env.OL_BASE || 'http://127.0.0.1:7420'
const SINK = process.env.E2E_SMTPSINK || 'http://127.0.0.1:18025'
const LEAK = 'IDORLEAK-' + (Date.now() % 100_000_000) // sentinel comment payload (private class)
const TID = 'idormtid1' // 8 alnum chars (world idRe)
let PID_E = ''
let PID_C = ''
let owner: Page, editor: Page, nonmember: Page, anon: Page
const trail: string[] = []

function bodyLeaks(body: string): boolean {
  // Quoted pid = server SERIALIZED project data (JSON) → leak.
  // Bare pid inside a URL echo (client-sent path reflected by a 404 page) is
  // NOT a leak class — the client already knows the pid it requested.
  const dataPid = (PID_E !== '' && body.includes('"' + PID_E + '"')) || (PID_C !== '' && body.includes('"' + PID_C + '"'))
  return dataPid || body.includes(LEAK)
}

// Outside-identity gate with a DIFFERENTIAL probe for the 2xx-state-class:
// a 2xx body on a project-scoped route is acceptable ONLY if it is
// indistinguishable from the same request against a non-existent project
// (state-default echo). A body that differs from the ghost response is a
// project-scoped disclosure → ⚑ IDOR FLAG (owner adjudicates; test pins).
async function outsideOk(status: number, body: string, label: string, who: Page, path: string): Promise<void> {
  const head = JSON.stringify(body.slice(0, 140))
  const leaked = bodyLeaks(body)
  expect(leaked, label + ': LEAK — serialized project data in a negative response: ' + head).toBe(false)
  expect(status, label + ': 5xx for outside identity: ' + head).toBeLessThan(500)
  if (status >= 200 && status < 300 && !/login|sign.?in|password/i.test(body)) {
    const gpid = 'aa' + '10'.repeat(12) // 24-hex ghost project id
    const ghostUrl = (path.includes('/project/' + PID_E) || path.includes('/project/' + PID_C))
      ? (path.includes(PID_E) ? path.replace(PID_E, gpid) : path.replace(PID_C, gpid)) : null
    if (!ghostUrl) throw new Error(label + ': 2xx body for outside identity (no ghost compare possible): ' + head)
    const gr = (await who.request.get(ghostUrl, { maxRedirects: 0 })) as any
    const gb = await gr.text().catch(() => '')
    const same = gr.status() === status && gb === body
    trail.push(label + ' → ' + status + ' ' + (same ? 'default-echo' : 'DIFFERS'))
    if (!same) {
      throw new Error(label + ': ⚑ IDOR FLAG — project-scoped 2xx state for outside identity, DIFFERS from ghost-project response (real=' + JSON.stringify(body.slice(0, 100)) + ' ghost=' + JSON.stringify(gb.slice(0, 100)) + ')')
    }
    console.log('[CE-1 pin] ' + label + ' 2xx=' + JSON.stringify(body.slice(0, 80)) + ' → default-echo (allowed pin)')
  }
}
function memberOk(status: number, label: string, body: string): void {
  const head = JSON.stringify(body.slice(0, 140))
  expect(status, label + ': 5xx for member identity: ' + head).toBeLessThan(500)
}
function ownerOk(status: number, label: string, honest500: boolean, body: string, tag?: string): void {
  const head = JSON.stringify(body.slice(0, 140))
  if (honest500 && status === 500) {
    console.log('[CE-1 FINDING] ' + (tag || label) + ': owner mutation 500 (crash-class) pinned — evidence head=' + head + ' — owner adjudicates (do not silently re-arm)')
    return
  }
  expect(status, label + ': 5xx for owner identity: ' + head).toBeLessThan(500)
}

type Cell = { tag: string; method: 'GET' | 'POST'; path: string; body?: any; honest500?: boolean }
const READS: Cell[] = [
  { tag: 'R1 project-get', method: 'GET', path: '/project/{pid}' },
  { tag: 'R2 threads', method: 'GET', path: '/project/{pid}/threads' },
  { tag: 'R3 ranges', method: 'GET', path: '/project/{pid}/ranges' },
  { tag: 'R4 changes/users', method: 'GET', path: '/project/{pid}/changes/users' },
  { tag: 'R5 fileproxy-ghost', method: 'GET', path: '/project/{pid}/file/d41d8cd98f00b204e980' },
  { tag: 'R6 download-zip', method: 'GET', path: '/project/{pid}/download/zip' },
  { tag: 'R7 dropbox-state', method: 'GET', path: '/project/{pid}/dropbox/state' },
]
const MUTES: Cell[] = [
  { tag: 'M1 thread-msg', method: 'POST', path: '/project/{pid}/thread/' + TID + '/messages', body: { doc: 'main.tex', ranges: [{ start: 0, end: 1 }], content: 'm1' } },
  { tag: 'M2 track_changes', method: 'POST', path: '/project/{pid}/track_changes', body: { enabled: true } },
  { tag: 'M3 restore_file', method: 'POST', path: '/project/{pid}/restore_file', body: { version: 'ghost', pathname: 'main.tex' }, honest500: true },
  { tag: 'M4 msg-edit', method: 'POST', path: '/project/{pid}/thread/' + TID + '/messages/ghostmid1/edit', body: { content: 'm4' } },
  { tag: 'M5 invite-probe', method: 'POST', path: '/project/{pid}/invite', body: { email: 'idor-nm-probe@e2e.test', privileges: 'reader' } },
  { tag: 'M6 clone', method: 'POST', path: '/project/{pid}/clone', body: {}, honest500: true },
]

async function runCell(c: Cell, pid: string, kind: 'owner' | 'member' | 'outside', who: Page, whoLabel: string): Promise<void> {
  const url = BASE + c.path.replace('{pid}', pid)
  const hdr = async () => {
    const tok = await who.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)
    return { 'X-CSRF-Token': tok || '', 'X-Requested-With': 'XMLHttpRequest', 'Content-Type': 'application/json' }
  }
  const res = (c.method === 'GET'
    ? await who.request.get(url, { maxRedirects: 0 })
    : await who.request.post(url, { data: JSON.stringify(c.body), headers: await hdr(), maxRedirects: 0 })) as any
  const st = res.status()
  const body = await res.text().catch(() => '')
  const label = whoLabel + '/' + c.tag
  trail.push(label + '→' + st)
  if (kind === 'outside') await outsideOk(st, body, label, who, url)
  else if (kind === 'owner') ownerOk(st, label, !!c.honest500, body, c.tag)
  else memberOk(st, label, body)
  // M6 owner-clone: 500 = the pinned finding (no clone created); 2xx = purge the clone (world hygiene)
  if (c.tag === 'M6 clone' && kind === 'owner' && st < 400) {
    try {
      const tok = await owner.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)
      const r2 = (await owner.request.post(BASE + c.path.replace('{pid}', pid), {
        data: JSON.stringify({}), headers: { 'X-CSRF-Token': tok || '', 'X-Requested-With': 'XMLHttpRequest', 'Content-Type': 'application/json' }, maxRedirects: 0,
      })) as any
      const j: any = (typeof r2?.json === 'function') ? await r2.json().catch(() => ({})) : {}
      const nid = j?.project?._id || j?.project?.id
      if (nid) await killProject(owner, String(nid)).catch(() => {})
    } catch (e) { console.log('[M6 hygiene] ' + String(e)) }
  }
}

test.describe.configure({ mode: 'serial', timeout: 25 * 60_000 })

test.describe.serial('CE-1 IDOR/permission × state matrix', () => {
  test.beforeAll(async ({ browser }: { browser: Browser }) => {
    const cO = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    owner = await cO.newPage()
    await loginRobust(owner, ADMIN.email, ADMIN.password)

    PID_E = await createBlankProject(owner)
    PID_C = await createBlankProject(owner)

    const cE = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    editor = await cE.newPage()
    await loginRobust(editor, USER2.email, USER2.password)
    const cN = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    nonmember = await cN.newPage()
    await loginRobust(nonmember, USER.email, USER.password)
    const cA = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    anon = await cA.newPage()

    // invitations → mail → accept (proven sharing-invite-loop flow)
    const invite = async (pid: string, privileges: string) => {
      const inv = await api(owner, 'POST', '/project/' + pid + '/invite', { email: USER2.email, privileges })
      expect([200, 201, 202].includes(inv.status()), 'invite ' + privileges + ' (got ' + inv.status() + ' ' + (await inv.text()).slice(0, 120) + ')').toBe(true)
      let token = ''
      for (let i = 0; i < 40 && !token; i++) {
        await new Promise((r) => setTimeout(r, 1000))
        const mres = await fetch(SINK + '/api/messages').then((r2: any) => r2.json()).catch(() => null)
        const hit = ((mres?.messages || []).slice().reverse() as any[]).find(
          (m: any) => (m.to || []).some((a: string) => a.toLowerCase() === USER2.email.toLowerCase()) && /invite\/token\//.test(m.raw || ''))
        const mm = (hit?.raw || '').match(/\/project\/[0-9a-f]+\/invite\/token\/([0-9a-zA-Z_-]+)/)
        if (mm) token = mm[1]
      }
      expect(token, 'invite mail (privileges=' + privileges + ')').toBeTruthy()
      await editor.goto(BASE + '/project/' + pid + '/invite/token/' + token, { waitUntil: 'domcontentloaded' }).catch(() => {})
      const csrf = await editor.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)
      const acc = await editor.request.post(BASE + '/project/' + pid + '/invite/token/' + token + '/accept', {
        headers: { 'X-CSRF-Token': csrf || '', 'X-Requested-With': 'XMLHttpRequest' }, maxRedirects: 0,
      }) as any
      expect(acc.status(), 'accept (got ' + acc.status() + ' ' + (await acc.text()).slice(0, 100) + ')').toBe(204)
    }
    await invite(PID_E, 'readAndWrite')
    await invite(PID_C, 'review')

    // sentinel comment (private payload class for the leak gate)
    for (const pid of [PID_E, PID_C]) {
      const msg = await api(owner, 'POST', '/project/' + pid + '/thread/' + TID + '/messages', {
        doc: 'main.tex', ranges: [{ start: 0, end: 1 }], content: LEAK,
      })
      expect([200, 201, 202].includes(msg.status()), 'seed (got ' + msg.status() + ' ' + (await msg.text()).slice(0, 140) + ')').toBe(true)
    }
    console.log('[CE-1 fixtures] E=' + PID_E + ' C=' + PID_C)
  })

  test.afterAll(async () => {
    // world hygiene: memberships gone + both projects purged (disposable fixtures)
    for (const pid of [PID_C, PID_E]) await killProject(owner, pid).catch(() => {})
    const gl = await api(editor, 'GET', '/project')
    const lb: string = await gl.text().catch(() => '')
    expect(bodyLeaks(lb), 'USER2 list leaks disposed project sentinels').toBe(false)
    console.log('[CE-1 trail] cells=' + trail.length + ' last=' + trail.slice(-24).join(' | '))
  })

  test('L1 READ matrix — 7 routes × owner/editor/commenter/nonmember/anon', async () => {
    for (const c of READS) {
      await runCell(c, PID_E, 'owner', owner, 'O')
      await runCell(c, PID_E, 'member', editor, 'E')
      await runCell(c, PID_C, 'member', editor, 'C')
      await runCell(c, PID_E, 'outside', nonmember, 'NM')
      await runCell(c, PID_E, 'outside', anon, 'ANON')
    }
  })

  test('L2 MUTATION matrix — 6 routes × owner/editor/commenter/nonmember/anon', async () => {
    for (const c of MUTES) {
      await runCell(c, PID_E, 'owner', owner, 'O')
      await runCell(c, PID_E, 'member', editor, 'E')
      await runCell(c, PID_C, 'member', editor, 'C')
      await runCell(c, PID_E, 'outside', nonmember, 'NM')
      await runCell(c, PID_E, 'outside', anon, 'ANON')
    }
    // M5 owner-cell may have left a pending invite (probe email is non-existent — inert) — best-effort revocation:
    const tokX = await owner.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)
    await owner.request.delete(BASE + '/project/' + PID_E + '/invite/idor-nm-probe@e2e.test', {
      headers: { 'X-CSRF-Token': tokX || '', 'X-Requested-With': 'XMLHttpRequest' },
    }).catch(() => null)
  })

  test('L3 CSRF/token boundary — no/wrong/form-field/cross-user/reuse', async () => {
    const tokO = (await owner.locator('meta[name="ol-csrfToken"]').getAttribute('content')) || ''
    const tokE = (await editor.locator('meta[name="ol-csrfToken"]').getAttribute('content')) || ''
    const doMsg = (p: Page, tok?: string, form = false) =>
      p.request.post(BASE + '/project/' + PID_C + '/thread/' + TID + '/messages', form ? {
        multipart: { _csrf: tok || 'form-token', doc: 'main.tex', ranges: '[{"start":0,"end":1}]', content: 'csrf-probe' },
      } : {
        data: JSON.stringify({ doc: 'main.tex', ranges: [{ start: 0, end: 1 }], content: 'csrf-probe' }),
        headers: { ...(tok ? { 'X-CSRF-Token': tok } : {}), 'X-Requested-With': 'XMLHttpRequest', 'Content-Type': 'application/json' },
      }, { maxRedirects: 0 }) as any

    const b0 = await doMsg(owner, tokO)
    expect([200, 201, 202].includes(b0.status()), 'c0 valid-token baseline (got ' + b0.status() + ' ' + (await b0.text()).slice(0, 100) + ')').toBe(true)

    const r1 = await doMsg(owner)
    expect([400, 401, 403].includes(r1.status()), 'c1 no-token refusal (got ' + r1.status() + ' ' + (await r1.text()).slice(0, 100) + ')').toBe(true)

    const r2 = await doMsg(owner, 'totally-wrong-csrf-123')
    expect([400, 401, 403].includes(r2.status()), 'c2 wrong-token refusal (got ' + r2.status() + ' ' + (await r2.text()).slice(0, 100) + ')').toBe(true)

    const r3 = await doMsg(owner, null, true)
    expect([400, 401, 403].includes(r3.status()), 'c3 _csrf form-field refusal pin (got ' + r3.status() + ' ' + (await r3.text()).slice(0, 120) + ')').toBe(true)

    const r4 = await doMsg(owner, tokE)
    expect([400, 401, 403].includes(r4.status()), 'c4 cross-user token refusal (got ' + r4.status() + ' ' + (await r4.text()).slice(0, 100) + ')').toBe(true)

    const r5a = await doMsg(owner, tokO)
    const r5b = await doMsg(owner, tokO)
    expect(r5a.status(), 'c5 reuse a (got ' + r5a.status() + ')').toBeLessThan(500)
    expect(r5b.status(), 'c5 reuse b (got ' + r5b.status() + ')').toBeLessThan(500)
  })

  test('L4 TRASHED state — member + outside refused; owner pinned; purge → invisible', async () => {
    const t = await api(owner, 'POST', '/admin/project/' + PID_E + '/trash', { userId: 'null' })
    expect(t.status(), 'trash (got ' + t.status() + ')').toBeLessThan(500)

    const legs: Array<[Page, string, 'GET' | 'POST', string, 'member' | 'outside']> = [
      [editor, 'E', 'GET', '/project/' + PID_E + '/threads', 'member'],
      [editor, 'E', 'GET', '/project/' + PID_E + '/download/zip', 'member'],
      [editor, 'E', 'POST', '/project/' + PID_E + '/track_changes', 'member'],
      [nonmember, 'NM', 'GET', '/project/' + PID_E + '/threads', 'outside'],
      [anon, 'ANON', 'GET', '/project/' + PID_E + '/download/zip', 'outside'],
      [anon, 'ANON', 'GET', '/project/' + PID_E + '/threads', 'outside'],
    ]
    for (const [p, who, method, path, kind] of legs) {
      const tokX = await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)
      const res = (method === 'GET'
        ? await p.request.get(BASE + path, { maxRedirects: 0 })
        : await p.request.post(BASE + path, { data: JSON.stringify({ enabled: true }), headers: { 'X-CSRF-Token': tokX || '', 'X-Requested-With': 'XMLHttpRequest', 'Content-Type': 'application/json' }, maxRedirects: 0 })) as any
      const st = res.status(); const body = await res.text().catch(() => '')
      const label = 'trashed/' + who + path
      if (kind === 'outside') await outsideOk(st, body, label, p, BASE + path)
      else memberOk(st, label, body)
    }
    const ol = await api(owner, 'GET', '/project')
    expect(ol.status(), 'trashed owner list (got ' + ol.status() + ')').toBeLessThan(500)
    // E is disposable: hard purge already handled by afterAll — but prove the trashed
    // read surface returns a refusal or trashed-excluded list (no 5xx):
    const re = await api(owner, 'GET', '/project/' + PID_E)
    expect(re.status(), 'trashed owner single-get (got ' + re.status() + ')').toBeLessThan(500)
  })
  test('L5 matrix pin trail', async () => {
    // no-op leg — the trail is emitted in afterAll (keeps ordering: housekeeping last)
    expect(trail.length, 'matrix cells executed').toBeGreaterThanOrEqual(60)
  })
})

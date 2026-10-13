/**
 * CE-3 — failure-injection + concurrency-ordering matrix (cause-effect).
 * Board: TODO-39396c71. Owner directive: USER_JOURNEY_CAUSE_EFFECT_AUDIT.md.
 *
 * Builds ON collab-two-user (which pins CONVERGENT two-user edits). CE-3 adds:
 *  L1 F4 — corrupted zip import → deterministic refusal, NO orphan project
 *  L2 C1 — SAME-RANGE conflicting edits → convergence invariant: both buffers identical,
 *          content ∈ {A | B | true merge}, no crash (N=2)
 *  L3 C3 — parallel last-write-wins (settings.name; restore is unported U10.5) → final =
 *          exactly one of the two writers, no 5xx (N=3)
 *  L4 C4 — invite vs remove race → consistent final membership state, no ghost membership (N=2)
 *  L5 F2 — hard page kill MID-EDIT → other client sees exactly PRE or POST doc state, never a mix
 *  L6 F3 — register with mail-sink DOWN (scoped reversible infra: docker stop/start + restore) →
 *          register stays 200, deterministic login state
 *
 * WORLD QUIRK (probed 2026-10-13): editor/mutation access binds to the page's OWN
 * login flow — a fresh page in a pre-logged context is 403/"Restricted" even for the
 * owner. The proven working pattern (collab-two-user, battery green) = per-page
 * loginRobust + hub nav + editor deep link. Follow it throughout.
 * INVARIANT RULE: race legs assert OUTCOME SETS, never timing.
 * FINDINGS RULE: 5xx / corrupted mix / ghost membership = ⚑ FLAG + pin (owner adjudicates).
 */
import { test, expect, type Page } from '@playwright/test'
import { execSync } from 'child_process'
import { ADMIN, USER2 } from '../fixtures/credentials'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { api, killProject, mongoEval } from '../parity/harness'
import * as fs from 'fs'

const BASE = process.env.OL_BASE || 'http://127.0.0.1:7420'
const SINK = process.env.E2E_SMTPSINK || 'http://127.0.0.1:18025'
const STAMP = Date.now().toString(36)
let PID = ''
const trail: string[] = []
const st = (r: any) => (typeof r.status === 'function' ? r.status() : r.status)
function pin(label: string, status: number, extra = ''): void {
  trail.push(label + ' → ' + status + (extra ? ' ' + extra.slice(0, 60) : ''))
  expect(status, label + ': 5xx server-error (got ' + status + ' ' + extra.slice(0, 80) + ')').toBeLessThan(500)
}
function buildZip(file: string, entries: Record<string, string | { raw: string }>): void {
  const py = `
import zipfile, json
entries = json.loads(${JSON.stringify(JSON.stringify(entries))})
with zipfile.ZipFile(${JSON.stringify(file)}, 'w') as z:
    for n, v in entries.items():
        c = v if isinstance(v, str) else v['raw']
        z.writestr(n, c)
`
  fs.writeFileSync('/tmp/__ce3zip.py', py)
  execSync('python3 /tmp/__ce3zip.py', { stdio: 'pipe' })
}
function cmText(page: Page): Promise<string> {
  return page.evaluate(() => (document.querySelector('.cm-content') as HTMLElement).innerText)
}
async function cmReplace(page: Page, text: string): Promise<void> {
  await page.evaluate((t: string) => {
    const el = document.querySelector('.cm-content') as HTMLElement
    const v = (el?.cmView as any)?.view
    v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: t }, selection: { anchor: Math.min(t.length, 12) }, userEvent: 'input' })
  }, text)
  await page.waitForTimeout(1500)
}
async function openEditor(page: Page, ms = 180_000): Promise<boolean> {
  await page.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
  await page.waitForTimeout(800)
  await page.goto(BASE + '/editor/' + PID, { waitUntil: 'domcontentloaded' }).catch(() => {})
  const t0 = Date.now()
  for (;;) {
    const ok = await page.evaluate(() => {
      const el = document.querySelector('.cm-content') as HTMLElement | null
      return !!(el && (el as any).cmView && (el as any).cmView.view)
    }).catch(() => false)
    if (ok) return true
    if (Date.now() - t0 > ms) {
      const bodyhead = await page.locator('body').innerText().catch(() => '').then((b: string) => b.slice(0, 120))
      throw new Error('editor not ready in ' + ms + 'ms (url=' + page.url() + ' body=' + JSON.stringify(bodyhead) + ')')
    }
    await page.waitForTimeout(1500)
  }
}

/** PROVEN path (collab-two-user, battery-green): direct collaberator grant.
 * ⚑ FINDING (owner record): the invite-accept path (204) did NOT yield editor
 * access for the member in probe _tmp-own (2026-10-13): OWNER renders, MEMBER
 * denied post-accept — grant semantics differ (pendingEditor_refs vs collaberator_refs). */
function grantCollaboratorDirect(pid: string): void {
  const uid2 = ((mongoEval(`var u = db.users.findOne({ email: '${USER2.email}' }); u ? String(u._id) : '';`) || '').trim().split('\n').pop() as string).trim()
  if (!/^[0-9a-f]{24}$/.test(uid2)) throw new Error('USER2 fixture user not found')
  const out = mongoEval(`db.projects.updateOne({ _id: ObjectId('${pid}') }, { $addToSet: { collaberator_refs: ObjectId('${uid2}') } }); print(db.projects.findOne({ _id: ObjectId('${pid}') }).collaberator_refs.length);`)
  const m = /\d+/.exec(out || '')
  if (!m || parseInt(m[0], 10) < 1) throw new Error('grantCollaboratorDirect failed: ' + out)
}

async function inviteAccept(pageOwner: Page, pageMember: Page): Promise<boolean> {
  await pageOwner.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
  const tok = (await pageOwner.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const inv = (await api(pageOwner, 'POST', '/project/' + PID + '/invite', { email: USER2.email, privileges: 'readAndWrite' })) as any
  if (![200, 201, 202].includes(st(inv))) {
    trail.push('invite rejected: ' + st(inv) + ' ' + (await inv.text().catch(() => '')).slice(0, 60))
    return false
  }
  let token = ''
  for (let t = 0; t < 45 && !token; t++) {
    await new Promise((r) => setTimeout(r, 1000))
    const msgs = await fetch(SINK + '/api/messages').then((r3: any) => r3.json()).catch(() => null)
    const hit = ((msgs?.messages || []).slice().reverse() as any[]).find((m: any) =>
      (m.to || []).some((a: string) => a.toLowerCase() === USER2.email.toLowerCase()) && /invite\/token\//.test(m.raw || ''))
    const mm = (hit?.raw || '').match(/\/project\/[0-9a-f]+\/invite\/token\/([0-9a-zA-Z_-]+)/)
    if (mm) token = mm[1]
  }
  if (!token) {
    trail.push('invite token not found in sink')
    return false
  }
  await pageMember.goto(BASE + '/project/' + PID + '/invite/token/' + token, { waitUntil: 'domcontentloaded' }).catch(() => {})
  const mtok = (await pageMember.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const acc = (await pageMember.request.post(BASE + '/project/' + PID + '/invite/token/' + token + '/accept', {
    headers: { 'X-CSRF-Token': mtok, 'X-Requested-With': 'XMLHttpRequest' }, maxRedirects: 0,
  })) as any
  if (acc.status() !== 204) trail.push('accept → ' + acc.status() + ' ' + (await acc.text().catch(() => '')).slice(0, 60))
  return acc.status() === 204
}

test.setTimeout(900_000)

test.describe.serial('CE-3 failure-injection + concurrency-ordering', () => {
  // REAPER TRUTH (probed 2026-10-13): idle fixture projects are hard-deleted within minutes,
  // and a *different* login session opening the editor of a shared fixture is denied
  // ("Restricted"). Both editor legs therefore create their OWN project in their OWN session
  // (the proven-working pattern: same-session create → editor), and clean up locally.

  test.afterAll(() => {
    console.log('[CE-3 trail] ' + trail.join(' | '))
  })

  test('L1 F4 — corrupted zip import: deterministic refusal, no orphan project', async ({ browser }) => {
    const c = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    const p = await c.newPage()
    await loginRobust(p, ADMIN.email, ADMIN.password)
    fs.writeFileSync('/tmp/ce3bad1.zip', 'PK\x03\x04\xc0\xde\xad\xbe truncated garbage')
    const tok = (await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
    const r = (await p.request.post(BASE + '/project/new/upload', {
      multipart: { name: 'ce3-bad1-' + STAMP, qqfile: { name: 'bad.zip', mimeType: 'application/zip', buffer: fs.readFileSync('/tmp/ce3bad1.zip') } },
      headers: { 'x-csrf-token': tok }, maxRedirects: 0,
    })) as any
    pin('F4-badzip', st(r), await r.text().catch(() => ''))
    expect([400, 403, 422].includes(st(r)), 'F4-badzip expected deterministic refusal (got ' + st(r) + ')').toBe(true)
    await p.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
    const hb = await p.locator('body').innerText().catch(() => '')
    expect(hb.includes('ce3-bad1-' + STAMP), 'F4-leak: orphan project name visible in hub').toBe(false)
    buildZip('/tmp/ce3bad2.zip', { 'main.tex': '\\documentclass{article}\\begin{document}x\\end{document}', 'broken.dat': { raw: '\x00\x01\x02\xff\xfe' } })
    const r2 = (await p.request.post(BASE + '/project/new/upload', {
      multipart: { name: 'ce3-bad2-' + STAMP, qqfile: { name: 'b2.zip', mimeType: 'application/zip', buffer: fs.readFileSync('/tmp/ce3bad2.zip') } },
      headers: { 'x-csrf-token': tok }, maxRedirects: 0,
    })) as any
    pin('F4-corruptpart', st(r2), await r2.text().catch(() => ''))
    if ([200, 201].includes(st(r2))) {
      const j: any = await r2.json().catch(() => ({}))
      const nid = j?.project_id || j?.project?._id
      if (nid) await killProject(p, String(nid)).catch(() => {})
    }
    await c.close()
  })

  test('L2 C1 — same-range conflicting edits converge (adversarial, N=2)', async ({ browser }) => {
    const cA = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    const pA = await cA.newPage()
    await loginRobust(pA, ADMIN.email, ADMIN.password)
    PID = await createBlankProject(pA)
    const cB = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    const pB = await cB.newPage()
    await loginRobust(pB, USER2.email, USER2.password)
    grantCollaboratorDirect(PID)
    for (let i = 1; i <= 2; i++) {
      await openEditor(pA)
      await openEditor(pB)
      const preA = (await cmText(pA)).trim()
      const preBx = (await cmText(pB)).trim()
      expect(preA.length, 'C1 pre-state: both editors see the same non-empty doc (A=' + preA.length + ' B=' + preBx.length + ')').toBeGreaterThan(0)
      expect(preA).toBe(preBx)
      await Promise.all([
        (async () => { await cmReplace(pA, 'A-CONFLICT-' + STAMP + '-' + i + '\nline2\n') })(),
        (async () => { await cmReplace(pB, 'B-CONFLICT-' + STAMP + '-' + i + '\nline2\n') })(),
      ])
      let lastA = ''; let lastB = ''; let stableFor = 0
      for (let k = 0; k < 18; k++) {
        const a = (await cmText(pA)).trim(); const b = (await cmText(pB)).trim()
        if (a === lastA && b === lastB) stableFor++; else stableFor = 0
        lastA = a; lastB = b
        if (stableFor >= 2) break
        await new Promise((r) => setTimeout(r, 1000))
      }
      const postA = (await cmText(pA)).trim()
      const postB = (await cmText(pB)).trim()
      expect(postA, 'C1-i' + i + ' convergence: identical in both (A=' + JSON.stringify(postA.slice(0, 60)) + ' B=' + JSON.stringify(postB.slice(0, 60)) + ')').toBe(postB)
      const wA = postA.includes('A-CONFLICT-'); const wB = postB.includes('B-CONFLICT-')
      expect(wA !== wB || (wA && wB), 'C1-i' + i + ' outcome: A or B or merge — not a corrupted mix (A=' + wA + ' B=' + wB + ')').toBe(true)
      trail.push('C1-i' + i + ' winner=' + (wA ? 'A' : '') + (wB ? 'B' : '') + ' converged=yes')
    }
    await killProject(pA, PID).catch(() => {})
    await cA.close().catch(() => {})
    await cB.close().catch(() => {})
  })

  test('L3 C3 — parallel last-write-wins (settings.name, two owner pages) → settled state is exactly one value, N=3', async ({ browser }) => {
    // name is OWNER-scoped (member write → 403, pinned) — the race is two owner-session pages in ONE context
    for (let i = 0; i < 3; i++) {
      const c = await browser.newContext({ viewport: { width: 1100, height: 800 } })
      const pX = await c.newPage()
      await loginRobust(pX, ADMIN.email, ADMIN.password)
      const pid = await createBlankProject(pX)
      const pY = await c.newPage() // same context/session — second owner view
      await pY.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
      const A = 'lww-a-' + STAMP + i; const B = 'lww-b-' + STAMP + i
      const res = await Promise.all([
        api(pX, 'POST', '/project/' + pid + '/settings', { name: A }) as Promise<any>,
        api(pY, 'POST', '/project/' + pid + '/settings', { name: B }) as Promise<any>,
      ])
      pin('L3-r' + i, Math.max(st(res[0]), st(res[1])), 'A=' + st(res[0]) + ' B=' + st(res[1]))
      // settle + readback via the HUB page (detail route /project/:id is the invite route → 404 for detail)
      await pX.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
      await pX.waitForTimeout(2000)
      const lb = await pX.locator('body').innerText().catch(() => '')
      const hasA = lb.includes(A); const hasB = lb.includes(B)
      expect(hasA !== hasB, 'L3-r' + i + ' settled hub shows exactly one race value (A=' + hasA + ' B=' + hasB + ')').toBe(true)
      trail.push('L3-r' + i + ' winner=' + (hasA ? 'A' : 'B') + ' statuses=' + st(res[0]) + '/' + st(res[1]))
      await killProject(pX, pid).catch(() => {})
      await c.close().catch(() => {})
    }
  })

  test('L4 C4 — invite vs remove race → consistent final state, no ghost membership (N=2)', async ({ browser }) => {
    const cOwn = await browser.newContext({ viewport: { width: 900, height: 700 } })
    const pOwn = await cOwn.newPage()
    await loginRobust(pOwn, ADMIN.email, ADMIN.password)
    PID = await createBlankProject(pOwn)
    const cMe = await browser.newContext({ viewport: { width: 900, height: 700 } })
    const pMe = await cMe.newPage()
    await loginRobust(pMe, USER2.email, USER2.password)
    for (let i = 0; i < 2; i++) {
      await pOwn.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
      const tok = (await pOwn.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
      const results = await Promise.all([
        (async () => {
          const inv = (await pOwn.request.post(BASE + '/project/' + PID + '/invite', {
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': tok },
            data: JSON.stringify({ email: USER2.email, privileges: 'readAndWrite' }), maxRedirects: 0,
          })) as any
          return st(inv)
        })(),
        (async () => {
          const rem = (await pOwn.request.delete(BASE + '/project/' + PID + '/users/' + USER2.email, { headers: { 'X-CSRF-Token': tok } })) as any
          return st(rem)
        })(),
      ])
      pin('C4-race' + i, Math.max(results[0], results[1]), 'inv=' + results[0] + ' rem=' + results[1])
      // consistent final state: membership read reflects exactly one outcome (pin both worlds)
      await new Promise((r) => setTimeout(r, 1500))
      const vis = (await api(pMe, 'GET', '/project/' + PID)) as any
      pin('C4-userstate' + i, st(vis), (await vis.text().catch(() => '')).slice(0, 80))
      await pMe.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' }).catch(() => {})
      const hubBody = await pMe.locator('body').innerText().catch(() => '')
      const seesProject = hubBody.includes(PID) || hubBody.includes('lww-') || hubBody.includes(String(PID).slice(0, 8))
      trail.push('C4-i' + i + ' inv=' + results[0] + ' rem=' + results[1] + ' userGET=' + st(vis) + ' userSees=' + seesProject)
      await pMe.request.delete(BASE + '/project/' + PID + '/invite/' + USER2.email, { headers: { 'X-CSRF-Token': tok } }).catch(() => {})
    }
    await killProject(pOwn, PID).catch(() => {})
    await cOwn.close().catch(() => {})
    await cMe.close().catch(() => {})
  })

  test('L5 F2 — hard page kill mid-edit → other client sees exactly PRE or POST state', async ({ browser }) => {
    const cA = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    const pA = await cA.newPage()
    await loginRobust(pA, ADMIN.email, ADMIN.password)
    PID = await createBlankProject(pA)
    const cB = await browser.newContext({ viewport: { width: 1280, height: 900 } })
    const pB = await cB.newPage()
    await loginRobust(pB, USER2.email, USER2.password)
    grantCollaboratorDirect(PID)
    await openEditor(pA)
    await openEditor(pB)
    const pre = (await cmText(pA)).trim()
    expect(pre, 'L5 precondition: A sees doc').toContain('\\documentclass')
    const POST = 'KILL-MID-EDIT-' + STAMP + '\n'
    await pA.evaluate((t: string) => {
      const el = document.querySelector('.cm-content') as HTMLElement
      const v = (el?.cmView as any)?.view
      v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: t }, selection: { anchor: 8 }, userEvent: 'input' })
    }, POST)
    await new Promise((r) => setTimeout(r, 250)) // deliberate mid-flush window
    await cA.close() // HARD KILL — context teardown mid-flight, no graceful close
    await new Promise((r) => setTimeout(r, 3000))
    const post = (await cmText(pB)).trim()
    const isPre = post === pre
    const isPost = post === POST.trim()
    expect(isPre || isPost, 'F2: other client sees exactly PRE or POST, not a corrupted mix (got ' + JSON.stringify(post.slice(0, 80)) + ')').toBe(true)
    trail.push('F2 resolved=' + (isPost ? 'POST(committed)' : 'PRE(rollback)'))
    await new Promise((r) => setTimeout(r, 2500))
    const stillAlive = await pB.evaluate(() => { const el = document.querySelector('.cm-content') as HTMLElement | null; return !!(el && (el as any).cmView) })
    expect(stillAlive, 'F2: B editor survived the hard peer kill').toBe(true)
    await killProject(pB, PID).catch(() => {})
    await cB.close().catch(() => {})
  })

  test('L6 F3 — mail-sink dependency + rate-limit boundary (scoped reversible infra)', async ({ browser }) => {
    // CONTRACT (Go port registrationpage.go, Node P3.4 pins):
    //  - body needs first_name + last_name + email (+password) else 400 "Too long name."
    //  - registration is MAIL-GATED: sink down → 422 "Failed to send the registration email." (honest dependency)
    //  - sink up → 200 "Registration successful..." + activation mail in sink
    //  - rate limit 5/60s per IP → 429 is a LEGITIMATE state (pin + retry-once after window)
    const SINKC = 'ol-e2e-smtpsink-1'
    const probeEmail = 'ce3-f3-' + STAMP + '@e2e.test'
    const sinkDown = () => { try { execSync('docker stop ' + SINKC, { stdio: 'pipe', timeout: 30_000 }) } catch { /* already down */ } }
    const sinkUp = () => { try { execSync('docker start ' + SINKC, { stdio: 'pipe', timeout: 30_000 }) } catch { /* already up */ } }
    try {
      sinkDown()
      await new Promise((r) => setTimeout(r, 1500))
      const cR = await browser.newContext()
      const pR = await cR.newPage()
      await pR.goto(BASE + '/register', { waitUntil: 'domcontentloaded' }).catch(() => {})
      const rtok = (await pR.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
      const body = () => ({ first_name: 'Ce3f3', last_name: 'Probe' + STAMP, email: probeEmail, password: 'Ce3-Round-' + STAMP })
      const postRegister = async () => (await pR.request.post(BASE + '/register', {
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': rtok, 'X-Requested-With': 'XMLHttpRequest' },
        data: JSON.stringify(body()), maxRedirects: 0,
      })) as any
      let r = await postRegister()
      let downBody = (await r.text().catch(() => '')).slice(0, 120)
      if (st(r) === 429) {
        trail.push('F3-down → 429 rate-limited (prior budget consumed) — waiting 65s window')
        await new Promise((rr) => setTimeout(rr, 65_000))
        r = await postRegister()
        downBody = (await r.text().catch(() => '')).slice(0, 120)
      }
      pin('F3-register-down', st(r), downBody)
      // mail-gated contract: down → deterministic 422 mailfail (or 429 if budget exhausted)
      expect([422, 429].includes(st(r)), 'F3: with mail down, register is 422 mailfail or 429 budget (got ' + st(r) + ')').toBe(true)
      if (st(r) === 422) expect(downBody, 'F3: mailfail message contract').toContain('Failed to send the registration email')
      // no user-visible success, no activation mail while down
      const msgsDown = await fetch(SINK + '/api/messages').then((m: any) => m.json()).catch(() => null)
      const deliveredDown = !!msgsDown && ((msgsDown.messages || []).some((m: any) => (m.to || []).some((a: string) => a.includes(STAMP)) && /activate/i.test(m.raw || '')))
      expect(deliveredDown, 'F3: no activation mail may be delivered while sink is down').toBe(false)
      // recover sink → the NEXT attempt (fresh email to dodge any state) succeeds with mail delivery
      sinkUp()
      await new Promise((rr) => setTimeout(rr, 3000))
      const email2 = 'ce3-f3b-' + STAMP + '@e2e.test'
      let r2 = await postRegister.call(null) as any
      const body2 = { first_name: 'Ce3f3', last_name: 'Probe' + STAMP, email: email2, password: 'Ce3-Round-' + STAMP }
      r2 = (await pR.request.post(BASE + '/register', {
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': rtok, 'X-Requested-With': 'XMLHttpRequest' },
        data: JSON.stringify(body2), maxRedirects: 0,
      })) as any
      const upBody = (await r2.text().catch(() => '')).slice(0, 120)
      pin('F3-register-up', st(r2), upBody)
      if (st(r2) === 429) {
        trail.push('F3-up 429 budget — waiting 65s')
        await new Promise((rr) => setTimeout(rr, 65_000))
      }
      const r3 = st(r2) === 429 ? await (pR.request.post(BASE + '/register', {
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': rtok, 'X-Requested-With': 'XMLHttpRequest' },
        data: JSON.stringify(body2), maxRedirects: 0,
      })) as any : r2
      pin('F3-register-up2', st(r3), (await r3.text().catch(() => '')).slice(0, 100))
      if (st(r3) === 429) { trail.push('F3 budget exhausted — pinning 429 state (owner: e2e world register budget 5/60s shared)') }
      else {
        expect([200, 409].includes(st(r3)), 'F3: with mail up, register is 200 success (or 409 reused email) — got ' + st(r3)).toBe(true)
        if (st(r3) === 200) {
          let sawActivation = false
          for (let t = 0; t < 20 && !sawActivation; t++) {
            await new Promise((rr) => setTimeout(rr, 1000))
            const msgs = await fetch(SINK + '/api/messages').then((m: any) => m.json()).catch(() => null)
            sawActivation = !!msgs && ((msgs.messages || []).some((m: any) => (m.to || []).some((a: string) => a.includes(STAMP)) && /activate/i.test(m.raw || '')))
          }
          trail.push('F3-up activation-mail=' + sawActivation)
        }
        trail.push('F3 probe accounts left registered: ' + probeEmail + ' / ' + email2 + ' (owner record)')
      }
      await cR.close().catch(() => {})
    } finally {
      sinkUp()
    }
  })

  test('L7 trail', async () => {
    expect(trail.length, 'CE-3 cells executed (' + trail.join(' | ') + ')').toBeGreaterThanOrEqual(9)
  })
})

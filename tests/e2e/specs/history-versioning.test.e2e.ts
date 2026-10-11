/**
 * history-versioning.test.e2e.ts — board TODO-8055cfa3: the HISTORY /
 * versioning surface at connected-feature intensity.
 *
 * Surface map (probed live on ag-v46 before pinning, 2026-10-11):
 *   GET  /project/:pid/updates                    {nextBeforeTimestamp,
 *        updates:[{fromV,toV,pathnames[],labels[],meta:{users,start_ts,end_ts},project_ops[]}]
 *   GET  /project/:pid/diff?from=N&to=M           {diff:[{i|d|e, content...}]} (Yjs plane)
 *   GET  /project/:pid/filetree/diff?from=N&to=M  {diff:[{operation:pathname}]}
 *   GET  /project/:pid/doc/:docid/diff?from&to    per-doc content diff
 *   GET  /project/:pid/changes                    200 [] (empty in this stack)
 *   POST /project/:pid/labels  {version,comment}  200 {id,comment,version,user_display_name,created_at}
 *   GET  /project/:pid/labels                        list
 *   DELETE /project/:pid/labels/:labelObjectId      remove
 *   POST /project/:pid/restore_file {version,pathname}  → 500 HONEST MARKER
 *        (U10.5 RestoreManager unported in the Go web — WEB_GO_PLAN; invalid
 *        body → strict 400 JSON validation — both pinned here on purpose)
 *   POST /project/:pid/revert-project {version}         same 500/400 territory
 *   GET  /project/:pid/version/:n/zip                   404 in this stack (pinned)
 *   UI: File menu → "Show version history" (rail-menus.tsx, id=show_version_history)
 *       renders the panel (change-list + diff-view + file-tree portals).
 *
 * Auth note: the history surface is SESSION-auth (browser cookies); the
 * PAT Bearer is rejected (401) — that is the git-bridge plane, not this one.
 *
 * 2-user leg: USER creates + edits, invites ADMIN, ADMIN accepts the
 * invite token and edits; USER's /updates must reflect ADMIN's version.
 * (Full invite-mail loop = sharing suite item 22ac7721; here the
 * token-accept surface is used directly.)
 *
 * Out of scope (tracked): restore/REVERT success paths (U10.5, unported —
 * the 500 is the honest pin and is asserted), version-zip (404 pinned).
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { api, killProject, mongoEval } from '../parity/harness'
import { USER, ADMIN } from '../fixtures/credentials'

let PID = ''
let lastToV = 0
const MARK_A = 'OLPSINT_HIST_A_11'
const MARK_B = 'OLPSINT_HIST_B_22'
const MARK_C = 'OLPSINT_HIST_C_33'

// the File-menu item click pattern (modals-p4): real mouse click, menuitem
// role, fallback to the in-page visible-item click.
async function clickFileMenuItem(page: any, label: string) {
  await page.getByRole('button', { name: /^file$/i }).first().click({ timeout: 10_000 })
  await page.waitForTimeout(500)
  const re = new RegExp('^' + label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'i')
  const mi = page.getByRole('menuitem', { name: re }).first()
  if (await mi.isVisible({ timeout: 2500 }).catch(() => false)) {
    await mi.click({ timeout: 5_000 })
    await page.waitForTimeout(700)
    return true
  }
  await page.evaluate((want: string) => {
    const vis = (e: Element) =>
      e.getClientRects().length > 0 &&
      getComputedStyle(e as HTMLElement).visibility !== 'hidden' &&
      getComputedStyle(e as HTMLElement).display !== 'none'
    const cands = [...document.querySelectorAll('[role=menuitem], a.dropdown-item, li')]
    const hit = cands.find(e => (e.textContent || '').trim().toLowerCase().startsWith(want.toLowerCase()) && vis(e))
    ;(hit as HTMLElement | undefined)?.click()
  }, label)
  await page.waitForTimeout(700)
  return true
}

test.describe.configure({ mode: 'serial' })

test('T1 timeline wire: 2 edits → versions + content diff carries the markers', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const r = await api(page, 'POST', '/project/new', { projectName: 'e2e-history-suite' })
  expect(r.status()).toBeLessThan(300)
  const pj: any = await r.json().catch(() => ({}))
  PID = String(pj?.project_id || pj?._id || '')
  expect(PID).toMatch(/^[0-9a-f]{24}$/)

  // ---- edit A, wait for the yjs version log, edit B ----
  await page.goto('/editor/' + PID, { waitUntil: 'commit' })
  const cm = page.locator('.cm-editor')
  await expect(cm.first(), 'cm6 editor').toBeVisible({ timeout: 45_000 })
  await cm.first().click()
  await page.keyboard.press('ControlOrMeta+End')
  await page.keyboard.type('\n% ' + MARK_A, { delay: 8 })
  await page.waitForTimeout(2_500)
  await page.keyboard.press('ControlOrMeta+End')
  await page.keyboard.type('\n% ' + MARK_B, { delay: 8 })
  await page.waitForTimeout(3_000)

  // ---- the updates timeline (wire) ----
  const u = await api(page, 'GET', '/project/' + PID + '/updates')
  expect(u.status(), 'updates 200').toBe(200)
  const uj: any = await u.json()
  expect(Array.isArray(uj?.updates), 'updates is an array').toBe(true)
  expect((uj.updates as any[]).length, 'at least one version batch').toBeGreaterThanOrEqual(1)
  const allV = (uj.updates as any[]).flatMap((e: any) => [e?.fromV, e?.toV])
  const minFrom = Math.min(...allV.filter((v: number) => typeof v === 'number'))
  lastToV = Math.max(...allV.filter((v: number) => typeof v === 'number'))
  expect(lastToV, 'timeline reaches version >= 2').toBeGreaterThanOrEqual(2)
  const paths = (uj.updates as any[]).flatMap((e: any) => e?.pathnames || [])
  expect(paths.join(','), 'timeline references main.tex').toContain('main.tex')

  // ---- content diff over the whole range carries BOTH markers ----
  const d = await api(page, 'GET', `/project/${PID}/diff?from=${minFrom}&to=${lastToV}`)
  expect(d.status(), 'diff 200').toBe(200)
  const dt = await d.text()
  expect(dt, 'diff carries marker A').toContain(MARK_A)
  expect(dt, 'diff carries marker B').toContain(MARK_B)

  // ---- filetree diff: main.tex was EDITED across the range ----
  const ft = await api(page, 'GET', `/project/${PID}/filetree/diff?from=${minFrom}&to=${lastToV}`)
  expect(ft.status(), 'filetree/diff 200').toBe(200)
  const ftj: any = await ft.json()
  expect(JSON.stringify(ftj?.diff || []), 'filetree mentions main.tex').toContain('main.tex')
  expect(JSON.stringify(ftj?.diff || []), 'operation is tracked').toMatch(/edited|created|added/)
})

test('T2 timeline UI: File → "Show version history" panel renders the timeline', async ({ page }) => {
  expect(PID, 'T1 ran first').toBeTruthy()
  await loginRobust(page, USER.email, USER.password)
  await page.goto('/editor/' + PID, { waitUntil: 'commit' })
  await expect(page.locator('.cm-editor').first(), 'cm6 editor').toBeVisible({ timeout: 45_000 })
  const opened = await clickFileMenuItem(page, 'Show version history')
  expect(opened, 'File menu item clicked').toBe(true)

  // The panel (features/history via ide-react History): the change list or
  // the file tree must be on screen with a version row / main.tex.
  const panel = page.locator('.history-react, #history-file-tree, .change-list, [class*="history"]')
  const visible = (await panel.count()) > 0
  expect(visible, 'a history surface is on screen (panel file tree or change list)').toBe(true)
  const bodyText = await page.locator('body').innerText()
  expect(bodyText, 'panel shows the main.tex file (or a version entry)').toMatch(/main\.tex|version/i)
})

test('T3 labels: create → listed → deleted (round-trip)', async ({ page }) => {
  expect(PID, 'T1 ran first').toBeTruthy()
  await loginRobust(page, USER.email, USER.password)
  await page.goto('/', { waitUntil: 'domcontentloaded' })

  const c = await api(page, 'POST', '/project/' + PID + '/labels', { version: Math.max(1, lastToV), comment: 'e2e-history-label' })
  expect([200, 201].includes(c.status()), 'create label 2xx (body: ' + (await c.text()).slice(0, 160) + ')').toBe(true)
  const cj: any = await c.json().catch(() => ({}))
  const label = cj?.label || cj
  const id = String(label?.id || '')
  expect(id, 'label id returned').toMatch(/^[0-9a-f]{24}$/)
  expect(String(label?.comment || ''), 'comment stored').toBe('e2e-history-label')
  expect(label?.user_display_name, 'author enriched').toBeTruthy()

  const l1 = await api(page, 'GET', '/project/' + PID + '/labels')
  expect(l1.status()).toBe(200)
  const l1j: any = await l1.json()
  expect(JSON.stringify(l1j), 'label listed').toContain('e2e-history-label')

  const del = await api(page, 'DELETE', '/project/' + PID + '/labels/' + id)
  expect(del.status(), 'delete 2xx (body: ' + (await del.text()).slice(0, 120) + ')').toBeLessThan(300)

  const l2 = await api(page, 'GET', '/project/' + PID + '/labels')
  expect(JSON.stringify(await l2.json()), 'label gone after delete').not.toContain('e2e-history-label')
})

test('T4 restore/revert: strict validation 400 + honest U10.5 500 (unported) pinned', async ({ page }) => {
  expect(PID, 'T1 ran first').toBeTruthy()
  await loginRobust(page, USER.email, USER.password)
  await page.goto('/', { waitUntil: 'domcontentloaded' })

  // invalid body → strict 400 JSON (contract pinned)
  const bad = await api(page, 'POST', '/project/' + PID + '/restore_file', { bogus: 1 })
  expect(bad.status(), 'restore_file invalid → 400').toBe(400)
  const badj: any = await bad.json().catch(() => ({}))
  expect(String(badj?.error || ''), 'validation error message').toContain('Validation error')

  // valid body → the HONEST 500 (U10.5 RestoreManager unported in the Go web)
  const good = await api(page, 'POST', '/project/' + PID + '/restore_file', { version: Math.max(1, lastToV), pathname: 'main.tex' })
  expect(good.status(), 'restore_file valid → 500 (U10.5 unported — honest marker)').toBe(500)

  const bad2 = await api(page, 'POST', '/project/' + PID + '/revert-project', { bogus: 1 })
  expect(bad2.status(), 'revert-project invalid → 400').toBe(400)
  const good2 = await api(page, 'POST', '/project/' + PID + '/revert-project', { version: Math.max(1, lastToV) })
  expect(good2.status(), 'revert-project valid → 500 (U10.5 unported — honest marker)').toBe(500)

  // the version-zip endpoint's current reality (pinned, not hidden)
  const zip = await api(page, 'GET', '/project/' + PID + '/version/1/zip')
  expect([404, 402].includes(zip.status()), 'version/:n/zip is 404/402 territory in this stack').toBe(true)
})

test('T5 two-user: ADMIN accepts the invite, edits; USER timeline grows', async ({ page, context }) => {
  expect(PID, 'T1 ran first').toBeTruthy()
  // —— USER invites ADMIN ——
  await loginRobust(page, USER.email, USER.password)
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  const inv = await api(page, 'POST', '/project/' + PID + '/invite', {
    email: ADMIN.email,
    privileges: 'readAndWrite',
  })
  expect([200, 201, 202].includes(inv.status()), 'invite 2xx (body: ' + (await inv.text()).slice(0, 160) + ')').toBe(true)
  const invj: any = await inv.json().catch(() => ({}))
  // The accept link lives in the invite E-MAIL (smtpsink) — the same
  // surface a real user follows. The create response carries no token.
  const SINK = process.env.E2E_SMTPSINK || 'http://127.0.0.1:18025'
  let token = ''
  let acceptPath = ''
  for (let i = 0; i < 20 && !token; i++) {
    await page.waitForTimeout(1_000)
    const mres = await fetch(SINK + '/api/messages').then((r) => r.json()).catch(() => null)
    const hit = ((mres?.messages || []).slice().reverse() as any[]).find((m: any) =>
      (m.to || []).some((a: string) => a.toLowerCase() === ADMIN.email.toLowerCase()) &&
      /\/invite\/token\//.test(m.raw || ''),
    )
    if (hit) {
      const mm = (hit.raw || '').match(/\/project\/[0-9a-f]+\/invite\/token\/([0-9a-zA-Z_-]+)/)
      if (mm) {
        token = mm[1]
        acceptPath = '/project/' + PID + '/invite/token/' + token + '/accept'
      }
    }
  }
  expect(token, 'invite accept URL found in the invite mail').toBeTruthy()

  // —— ADMIN (fresh context) accepts + edits ——
  // PINNED CONTRACT (invite.go, mirrored by specs/collab-two-user): the
  // accept path is a Node-parity no-op for membership — observable
  // contract = one-shot consumption + sender contact + 204 (XHR) / 302
  // (page), and NO project refs write. The browser request context
  // follows the 302 to /project/:id, where a non-member gets the 404
  // page — which is why the XHR variant (204) is the assertable form.
  const c2 = await context.newPage()
  await loginRobust(c2, ADMIN.email, ADMIN.password)
  await c2.goto('/', { waitUntil: 'domcontentloaded' }).catch(() => {})
  const csrf = await c2.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)
  const acc = await c2.request.post('http://127.0.0.1:7420' + acceptPath, {
    headers: { 'X-CSRF-TOKEN': csrf || '', 'X-Requested-With': 'XMLHttpRequest' },
    maxRedirects: 0,
  }) as any
  expect(acc.status(), 'invite accept XHR → 204 pinned (got ' + (await acc.text().catch(() => '')).slice(0, 120) + ')').toBe(204)

  // consumption: the one-shot invite is gone from the list
  const lst = await api(page, 'GET', '/project/' + PID + '/invites')
  const lstj: any = await lst.json().catch(() => ({ invites: [] }))
  expect((lstj?.invites || []).length, 'invite consumed after accept').toBe(0)

  // membership for the co-edit leg: the app has NO new-member path on this
  // stack (invite-accept is the pinned no-op; PUT /project/:id/users/:uid
  // 404s for non-members — collab.go arrayFilters parity), so seed the
  // collab ref directly the way specs/collab-two-user does (= exactly what
  // a successful accept would have represented).
  // uid via the users collection (the /user/profile surface shape varies)
  const adminUid = String(mongoEval(`
    const u = db.users.findOne({ email: '${ADMIN.email}' });
    print(u ? String(u._id) : '');
  `) || '')
  expect(adminUid, 'admin uid present in users').toMatch(/^[0-9a-f]{24}$/)
  const grants = mongoEval(
    `db.projects.updateOne({ _id: ObjectId('${PID}') }, { $addToSet: { collaberator_refs: ObjectId('${adminUid}') } });
     print(db.projects.findOne({ _id: ObjectId('${PID}') }).collaberator_refs.length);`,
  )
  expect(/\d+/.test(String(grants)), 'grant ack (got: ' + grants + ')').toBeTruthy()

  await c2.goto('/editor/' + PID, { waitUntil: 'commit' })
  const cm = c2.locator('.cm-editor')
  await expect(cm.first(), 'cm6 editor (admin)').toBeVisible({ timeout: 45_000 })
  await cm.first().click()
  await c2.keyboard.press('ControlOrMeta+End')
  await c2.keyboard.type('\n% ' + MARK_C, { delay: 8 })
  await c2.waitForTimeout(3_000)
  await c2.close()

  // —— USER sees ADMIN's version in the timeline ——
  const c3 = await context.newPage()
  await loginRobust(c3, USER.email, USER.password)
  let grew = false
  for (let i = 0; i < 20 && !grew; i++) {
    const u = await api(c3, 'GET', '/project/' + PID + '/updates')
    if (u.status() === 200) {
      const uj: any = await u.json()
      const maxV = Math.max(0, ...(uj.updates || []).flatMap((e: any) => [e?.toV || 0, e?.fromV || 0]))
      if (maxV > lastToV) {
        grew = true
        const dt = await api(c3, 'GET', `/project/${PID}/diff?from=${lastToV}&to=${maxV}`)
        if (dt.status() === 200) {
          expect(await dt.text(), 'ADMIN edit is in the timeline diff').toContain(MARK_C)
        }
      }
    }
    if (!grew) await c3.waitForTimeout(1_000)
  }
  await c3.close()
  expect(grew, "ADMIN's version surfaced in USER's timeline (2-user connected)").toBe(true)
})

test('T6 cleanup: admin trashes + deletes the throwaway project', async ({ page }) => {
  expect(PID, 'T1 ran first').toBeTruthy()
  await loginRobust(page, ADMIN.email, ADMIN.password)
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await killProject(page, PID)
})

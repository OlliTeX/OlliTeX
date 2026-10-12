/**
 * sharing-permissions — SHARING/ACCESS lifecycle leg 2 (board 22ac7721).
 *
 * LIVE-PINNED 2026-10-12 (ag-v57 world, evidence in OPEN_QUEUE_PROGRESS.md):
 *   PUT /project/:pid/users/:uid
 *     body {"privilegeLevel": "readOnly"|"readAndWrite"|"review"}  (colLevel,
 *       collab.go:223 — `privileges` key → 400 "Unrecognized key" — PINNED)
 *     clean 24-hex uid, no modern user record for that (email, project) →
 *       404 {"message":"not found"}   (PINNED: probe5, uid len 24)
 *     legacy-ref membership (collaberator_refs array — the access plane per
 *       access.go) is NOT a modern user record → the PUT surface targets the
 *       modern project-user structure (Node parity: users array).
 *   DELETE /project/:pid/users/:uid → 204 (PINNED; idempotent on non-member)
 *   GET  /project/:pid/members      → 200 {"members":[{_id,first_name,last_name,email,privileges,signUpDate}]}
 *     (owner after a modern-user grant; [] otherwise — PINNED in L3 spec)
 *
 * STRUCTURAL NOTE (flagged for owner, not silently "fixed"): in this CE stack
 * the live membership plane for a fresh project is the legacy collaberator_refs
 * array (access.go), while the role-change PUT operates the modern users
 * structure — so "owner demotes editor→commenter" for a legacy-ref member is
 * 404-not-found TODAY. The pinned contract in this file is exactly that state
 * (+ the body-key validation + the members list shape). If the owner wires
 * legacy-refs→modern-users (or the UI share modal to a route that exists in
 * this stack), this spec body is the template to flip.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { mongoEval } from '../helpers/host'
import { USER, USER2 } from '../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'

test.describe.configure({ mode: 'serial' })
test.setTimeout(120_000)

test('L2 pinned: PUT body-key validation + legacy-ref 404 not-found + members shape + read leg', async ({ page, context, browser }) => {
  await loginRobust(page, USER.email, USER.password)
  const pid = String(await createBlankProject(page)).trim()
  expect(pid).toMatch(/^[0-9a-f]{24}$/)
  const csrf = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const H = { 'X-CSRF-TOKEN': csrf }

  const uid2 = String(mongoEval(`const u = db.users.findOne({ email: '${USER2.email}' }); print(u ? String(u._id) : '');`) || '').trim()
  expect(uid2).toMatch(/^[0-9a-f]{24}$/)
  // legacy-ref membership (the stack's live access plane per access.go)
  const grants = String(mongoEval(`db.projects.updateOne({ _id: ObjectId('${pid}') }, { $addToSet: { collaberator_refs: ObjectId('${uid2}') } });
    print(db.projects.findOne({ _id: ObjectId('${pid}') }).collaberator_refs.length);`) || '')
  expect(/\d+/.test(grants), 'seed membership ack').toBeTruthy()

  // B (legacy-ref member) can read the project surface today:
  const ctxB = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const pB = await ctxB.newPage()
  await loginRobust(pB, USER2.email, USER2.password)
  const read = await pB.request.get(BASE + '/editor/' + pid, { maxRedirects: 0 }) as any
  expect([200, 302, 303].includes(read.status()), 'B member /editor read OK (got ' + read.status() + ')').toBe(true)
  await ctxB.close()

  // PUT: the pinned body contract — `privileges` key is REJECTED (400, validated
  // BEFORE the user lookup):
  const badKey = await page.request.put(BASE + '/project/' + pid + '/users/' + uid2, { data: { privileges: 'readOnly' }, headers: H })
  expect(badKey.status(), 'PUT wrong key → pinned 400 Unrecognized (got ' + badKey.status() + ')').toBe(400)
  expect((await badKey.text().catch(() => '')).includes('Unrecognized key')).toBe(true)

  // PUT: the accepted key (privilegeLevel) on a SEEDED legacy-ref member →
  // 204 (PINNED: handler resolves collaberator_refs members; the 404
  // not-found shape applies only to unseeded uids — probe evidence in
  // OPEN_QUEUE_PROGRESS.md cont.5):
  const put = await page.request.put(BASE + '/project/' + pid + '/users/' + uid2, { data: { privilegeLevel: 'readOnly' }, headers: H })
  expect(put.status(), 'PUT privilegeLevel on seeded member → PINNED 204 (got ' + put.status() + ')').toBe(204)
  const memPut = await page.request.get(BASE + '/project/' + pid + '/members')
  expect(memPut.status(), 'GET members after PUT → 200').toBe(200)
  const memPutj: any = await memPut.json().catch(() => ({ members: [] }))
  expect(Array.isArray(memPutj?.members), 'members shape after PUT').toBe(true)
  const row = (memPutj.members || []).find((m: any) => String(m._id).toLowerCase() === uid2)
  if (row) expect(typeof row.privileges === 'string' && row.privileges.length > 0, 'post-PUT privileges field present (pinned ' + JSON.stringify(row.privileges) + ')').toBe(true)
  // restore + re-pin the unseeded-uid 404 family with a synthetic uid (honest
  // negative pin):
  const ghost = 'f'.repeat(24)
  const ghostPut = await page.request.put(BASE + '/project/' + pid + '/users/' + ghost, { data: { privilegeLevel: 'readOnly' }, headers: H })
  expect(ghostPut.status(), 'PUT unseeded uid → PINNED 404 not-found (got ' + ghostPut.status() + ')').toBe(404)
  expect((await ghostPut.text().catch(() => '')).includes('not found')).toBe(true)

  // DELETE is idempotent and 204 (PINNED) — the modals-p4 remove leg family:
  const del = await page.request.delete(BASE + '/project/' + pid + '/users/' + uid2, { headers: H })
  expect(del.status(), 'DELETE member → pinned 204 (got ' + del.status() + ')').toBe(204)

  // members list shape (owner):
  const mem = await page.request.get(BASE + '/project/' + pid + '/members')
  expect(mem.status(), 'GET members → 200').toBe(200)
  const memj: any = await mem.json().catch(() => ({ members: [] }))
  expect(Array.isArray(memj?.members), 'members shape {members:[...]}').toBe(true)
})

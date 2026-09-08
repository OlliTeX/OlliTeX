import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, mkProject, mongoEval } from '../../parity/harness'
import { ADMIN, USER, TPLADMIN } from '../../fixtures/credentials'

// Parity baseline (legacy /admin/project) — contract: endpoint + payload + state.
// Throwaway projects: created per test via POST /project/new, cleaned up.
const BASE = 'http://127.0.0.1:7420'
let ctxPage: any = null
const W = () => 'ph' + Date.now().toString(36) + Math.floor(Math.random() * 1000)

function admin() {
  expect(ctxPage, 'admin page missing').toBeTruthy()
  return ctxPage
}

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
  ctxPage = p
})
test.afterAll(async () => {
  if (ctxPage) await ctxPage.context().close().catch(() => {})
  ctxPage = null
})

const adminId = () => mongoEval('db.users.findOne({ email: "e2e-admin@e2e.test" })._id.toString()')
const projTrashed = (pid: string) =>
  Number(mongoEval(`(db.projects.findOne({ _id: ObjectId("${pid}") }, { trashed: 1 }).trashed || []).length`)) > 0
const collabCount = (pid: string) =>
  Number(mongoEval(`(db.projects.findOne({ _id: ObjectId("${pid}") }).collaberator_refs || []).length`))

test('renders: GET /admin/project shows the admin project list for site admin', async () => {
  const p = admin()
  await p.goto(BASE + '/admin/project', { waitUntil: 'domcontentloaded' })
  await p.waitForTimeout(1500)
  await expect(p).toHaveTitle(/Projects/i)
  await expect(p.locator('h1').first()).toContainText(/projects/i)
})

test('denied: tpladmin + user cannot open /admin/project', async ({ browser }) => {
  for (const a of [TPLADMIN, USER]) {
    const ctx = await browser.newContext(); const p2 = await ctx.newPage()
    await loginRobust(p2, a.email, a.password)
    const resp = await p2.goto(BASE + '/admin/project', { waitUntil: 'domcontentloaded' }).catch(() => null)
    const body = (await p2.locator('body').innerText().catch(() => '')) || ''
    const denied = (resp?.status() === 403) || !/Change owner|Trash project|All projects/i.test(body)
    expect(denied, `${a.email} must not manage projects`).toBeTruthy()
    await ctx.close()
  }
})

test('list: POST /admin/user/:id/projects returns the project list', async () => {
  const p = admin()
  const r = await api(p, 'POST', `/admin/user/${adminId()}/projects`, { sort: { by: 'lastUpdated', order: 'desc' } })
  expect(r.status()).toBe(200)
  const body = await r.json()
  expect(Array.isArray(body.projects) && body.projects.length > 0, 'expected at least one managed project').toBeTruthy()
})

test('download: GET /project/download/zip?project_ids= returns archive bytes', async () => {
  const p = admin()
  const proj = await mkProject(p, W())
  let r = await p.request.get(BASE + '/project/download/zip', { params: { project_ids: proj._id } })
  if (r.status() === 403) { await p.reload({ waitUntil: 'domcontentloaded' }); r = await p.request.get(BASE + '/project/download/zip', { params: { project_ids: proj._id } }) }
  expect(r.status()).toBe(200)
  const ct = r.headers()['content-type'] || ''
  expect(/zip|octet-stream/i.test(ct), ct).toBeTruthy()
})

test('trash + untrash: POST /admin/project/:id/trash {userId} then /untrash', async () => {
  const p = admin()
  const proj = await mkProject(p, W())
  const pid = proj._id
  const uid = adminId()

  let r = await api(p, 'POST', `/admin/project/${pid}/trash`, { userId: uid })
  expect(r.status(), 'trash: ' + (await r.text().catch(() => '')).slice(0, 120)).toBe(200)
  expect(projTrashed(pid), 'project trashed in DB').toBeTruthy()

  r = await api(p, 'POST', `/admin/project/${pid}/untrash`, { userId: uid })
  expect(r.status(), 'untrash').toBe(200)
  expect(projTrashed(pid), 'project untrashed in DB').toBeFalsy()
})

test('delete + undelete: DELETE /admin/project/:id (soft) then /undelete', async () => {
  const p = admin()
  const proj = await mkProject(p, W())
  const pid = proj._id
  const uid = adminId()
  await api(p, 'POST', `/admin/project/${pid}/trash`, { userId: uid }).catch(() => {})

  let r = await api(p, 'DELETE', `/admin/project/${pid}`)
  if (r.status() === 403) { await p.reload({ waitUntil: 'domcontentloaded' }); r = await api(p, 'DELETE', `/admin/project/${pid}`) }
  expect(r.status(), 'soft delete').toBe(200)

  r = await api(p, 'POST', `/admin/project/${pid}/undelete`, { userId: uid })
  expect(r.status(), 'undelete').toBe(200)
})

test('purge: DELETE /admin/project/:id/purge removes permanently', async () => {
  const p = admin()
  const proj = await mkProject(p, W())
  const pid = proj._id
  const uid = adminId()
  await api(p, 'POST', `/admin/project/${pid}/trash`, { userId: uid }).catch(() => {})
  await api(p, 'DELETE', `/admin/project/${pid}`).catch(() => {})
  let r = await api(p, 'DELETE', `/admin/project/${pid}/purge`)
  if (r.status() === 403) { await p.reload({ waitUntil: 'domcontentloaded' }); r = await api(p, 'DELETE', `/admin/project/${pid}/purge`) }
  expect(r.status(), 'purge').toBe(200)
  expect(Number(mongoEval(`db.projects.countDocuments({ _id: ObjectId("${pid}") })`)), 'purged from DB').toBe(0)
})

test('transfer: POST /project/:id/transfer-ownership {user_id, skipEmails}', async () => {
  const p = admin()
  const proj = await mkProject(p, W())
  const pid = proj._id
  const uid = mongoEval('db.users.findOne({ email: "e2e-user@e2e.test" })._id.toString()')
  expect(uid, 'e2e-user id').toBeTruthy()
  let r = await api(p, 'POST', `/project/${pid}/transfer-ownership`, { user_id: uid, skipEmails: true })
  if (r.status() === 403) { await p.reload({ waitUntil: 'domcontentloaded' }); r = await api(p, 'POST', `/project/${pid}/transfer-ownership`, { user_id: uid, skipEmails: true }) }
  expect([200, 204], 'transfer: ' + (await r.text().catch(() => '')).slice(0, 120)).toContain(r.status())
})

test('invite: POST /admin/project/:id/invite {email, privileges}', async () => {
  const p = admin()
  const proj = await mkProject(p, W())
  const pid = proj._id
  let r = await api(p, 'POST', `/admin/project/${pid}/invite`, { email: 'e2e-user@e2e.test', privileges: 'readAndWrite' })
  if (r.status() === 403) { await p.reload({ waitUntil: 'domcontentloaded' }); r = await api(p, 'POST', `/admin/project/${pid}/invite`, { email: 'e2e-user@e2e.test', privileges: 'readAndWrite' }) }
  expect(r.status(), 'invite: ' + (await r.text().catch(() => '')).slice(0, 120)).toBe(200)
  const lr = await api(p, 'GET', `/admin/project/${pid}/invites`)
  expect(lr.status(), 'invites list').toBe(200)
  const lj = await lr.json()
  const it = (lj.invites ?? []).find((x: any) => x?.email === 'e2e-user@e2e.test')
  expect(it, 'pending invite present: ' + JSON.stringify(lj).slice(0, 160)).toBeTruthy()
  expect(it.privileges, 'invite privilege recorded').toBe('readAndWrite')
})

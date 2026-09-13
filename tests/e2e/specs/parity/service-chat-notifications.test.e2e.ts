/**
 * B3 (GO_CUTOVER_PLAN.md) — chat + notifications service journey (test-first).
 *
 * Pinned ON THE NODE-ACTIVE services (in-container 127.0.0.1:3010 chat,
 * :3042 notifications) before the Go flip; re-run unchanged with
 * USE_GO_CHAT + USE_GO_NOTIFICATIONS=true as the cutover gate.
 *
 * Pinned contracts (Node 6.3.0, chat/app.js + MessageHttpController):
 *  - POST /project/:pid/messages {user_id, content} → 201 + formatted JSON
 *  - GET  /project/:pid/messages → array containing the sent message
 *  - DELETE /project/:pid/messages/:mid → removes it
 *  - POST /project/:pid/messages bad body → 400 {error, statusCode:400}
 *    (validation-tools shape; body-rooted, not 404)
 *  - unknown route → 404 JSON {message:"Not found"} (chat Express fallback)
 *  - notifications: POST /user/:uid + GET list + GET /key/:k/count + /status
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { mkProject, killProject, mongoEval } from '../../parity/harness'
import { login } from '../../helpers/auth'
import { USER } from '../../fixtures/credentials'

const RUN = Date.now().toString(36)

// in-container probes (services bind 127.0.0.1 inside overleafserver)
function probe(method: string, url: string, body?: unknown): { status: number; body: string } {
  const args = ['exec', 'overleafserver', 'curl', '-s', '-o', '/tmp/b3_body', '-w', '%{http_code}', '-X', method, '--max-time', '10', url]
  if (body !== undefined) {
    args.splice(3, 0, '-H', 'Content-Type: application/json', '-d', JSON.stringify(body))
  }
  let status = 0
  try {
    status = Number(execFileSync('docker', args, { encoding: 'utf8', timeout: 20_000 }).trim())
  } catch (e: any) {
    status = Number((e.stdout || '').trim()) || 0
  }
  let out = ''
  try {
    out = execFileSync('docker', ['exec', 'overleafserver', 'cat', '/tmp/b3_body'], { encoding: 'utf8', timeout: 10_000 })
  } catch { /* no body */ }
  return { status, body: out }
}

let page: any = null
let ctx: any = null
let projectId: string | null = null
let uid: string = ''

test.describe.configure({ mode: 'serial' })

test.beforeAll(async ({ browser }) => {
  ctx = await browser.newContext()
  page = await ctx.newPage()
  await login(page, USER)
  const created = await mkProject(page, `B3 chat ${RUN}`)
  projectId = created._id
  expect(projectId).toBeTruthy()
  uid = mongoEval(`db.users.findOne({email: ${JSON.stringify(USER.email)}}, {_id: 1})._id.toString()`)
  expect(uid, 'a real user _id').toMatch(/^[0-9a-f]{24}$/)
})

test.afterAll(async () => {
  if (projectId && page) await killProject(page, projectId).catch(() => {})
  if (ctx) await ctx.close().catch(() => {})
})

test('1: chat /status is up', async () => {
  const r = probe('GET', 'http://127.0.0.1:3010/status')
  expect(r.status, 'chat /status → 2xx').toBeLessThan(300)
})

let createdMessageId: string | null = null

test('2: POST a global message → 201 + formatted JSON', async () => {
  const text = `b3-msg-${RUN}`
  const r = probe('POST', `http://127.0.0.1:3010/project/${projectId}/messages`, {
    user_id: uid,
    content: text,
  })
  expect(r.status, `POST message → 201 (got ${r.status}: ${r.body.slice(0, 200)})`).toBe(201)
  const j = JSON.parse(r.body)
  createdMessageId = j.id ?? j.message_id ?? null
  expect(createdMessageId, 'message id in response (id, not _id)').toMatch(/^[0-9a-f]{24}$/)
  expect(j.content, 'content echo').toBe(text)
  expect(j.user_id, 'user_id preserved').toBeTruthy()
  expect(j.room_id, 'room_id = projectId (Node format)').toBe(projectId)
})

test('3: GET messages list contains the sent message', async () => {
  const r = probe('GET', `http://127.0.0.1:3010/project/${projectId}/messages?limit=50`)
  expect(r.status, 'GET messages → 200').toBe(200)
  expect(r.body, 'list contains the marker').toContain(`b3-msg-${RUN}`)
})

test('4: DELETE the message → subsequent GET lacks it', async () => {
  if (!createdMessageId) throw new Error('no message id from test 2')
  const del = probe('DELETE', `http://127.0.0.1:3010/project/${projectId}/messages/${createdMessageId}`)
  expect(del.status, 'DELETE → 2xx').toBeLessThan(300)
  const r = probe('GET', `http://127.0.0.1:3010/project/${projectId}/messages?limit=50`)
  expect(r.status).toBe(200)
  expect(r.body, 'marker gone after delete').not.toContain(`b3-msg-${RUN}`)
})

test('5: bad body → 400 with the validation-tools shape (not 404/5xx)', async () => {
  const r = probe('POST', `http://127.0.0.1:3010/project/${projectId}/messages`, { content: 'no user id' })
  expect(r.status, 'missing user_id → 400').toBe(400)
  let j: any = {}
  try { j = JSON.parse(r.body) } catch { /* assert text shape below */ }
  expect(j.statusCode !== undefined || /statusCode/.test(r.body), 'body carries statusCode field').toBeTruthy()
})

test('6: unknown chat route → 404 JSON {message:"Not found"}', async () => {
  const r = probe('GET', 'http://127.0.0.1:3010/no-such-route')
  expect(r.status, 'unknown route → 404').toBe(404)
  let j: any = {}
  try { j = JSON.parse(r.body) } catch {}
  expect(j.message, 'Express fallback body').toBe('Not found')
})

test('7: notifications service: add + list + count + status', async () => {
  const key = `b3key-${RUN}`
  const uidN = uid
  const add = probe('POST', `http://127.0.0.1:3042/user/${uidN}`, {
    key,
    templateKey: 'test',
    messageOpts: { text: `b3-notif-${RUN}` },
    forceCreate: true,
  })
  expect(add.status, `notif add 2xx (got ${add.status}: ${add.body.slice(0, 160)})`).toBeLessThan(300)
  const list = probe('GET', `http://127.0.0.1:3042/user/${uidN}`)
  expect(list.status, 'notif list 200').toBe(200)
  expect(list.body, 'list contains key').toContain(key)
  const count = probe('GET', `http://127.0.0.1:3042/key/${key}/count`)
  expect(count.status, 'count 200').toBe(200)
  const countVal = Number(/[\d]+/.exec(count.body)?.[0] ?? JSON.parse(count.body).count ?? 'NaN')
  expect(countVal, 'count ≥ 1').toBeGreaterThanOrEqual(1)
  const st = probe('GET', 'http://127.0.0.1:3042/status')
  expect(st.status).toBeLessThan(300)
  const hc = probe('GET', 'http://127.0.0.1:3042/health_check')
  expect(hc.status, 'health_check healthy').toBeLessThan(300)
})

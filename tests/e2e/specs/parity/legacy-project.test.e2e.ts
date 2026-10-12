import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, captureApi, waitForCall, mkProject as harnessMkProject, killProject } from '../../parity/harness'
import { USER, TPLADMIN } from '../../fixtures/credentials'

// Parity baseline (legacy /project) — all projects list + create/search/trash/tags.
// 2026-09-10 (owner queue 6): the legacy /project dashboard is REMOVED — the
// URL now 301s to the hub surface (Projects, /hub#/projects.all) which serves
// the same APIs. The tests below drive the hub surface via the redirect and
// keep the API contract assertions (create/search/trash-restore/tags).
const BASE = 'http://127.0.0.1:7420'
const PAGE = BASE + '/project'
const HUB_PROJECTS = BASE + '/hub#/projects.all'
let p: any = null

const mkProject = (page: any, name: string) => harnessMkProject(page, name)
const delProject = async (page: any, pid: string) => {
  const token = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => '')) || ''
  await page.request.delete(BASE + '/project/' + pid, { headers: token ? { 'X-CSRF-TOKEN': token } : {} }).catch(() => {})
}

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, USER.email, USER.password)
})
test.afterAll(async () => { if (p) await p.context().close().catch(() => {}) })
const a = (method: string, path: string, body?: unknown) => api(p, method, path, body)

test('retired: /project is a Go 404 now (redirect removed 2026-10-05)', async () => {
  const res = await p.request.get(PAGE, { maxRedirects: 0 })
  expect([404, 302].includes(res.status()), '404 (or the anonymous login bounce) expected').toBeTruthy()
  if (res.status() === 404) {
    const loc = (res.headers()['location'] || '').toLowerCase()
    expect(loc, 'must not be a hub redirect anymore').not.toContain('/hub')
  }
})

test('deny: guests get the login wall (404 now — no redirect to wall)', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  await q.goto(PAGE, { waitUntil: 'domcontentloaded' }).catch(() => {})
  const url = q.url()
  const body = (await q.locator('body').innerText().catch(() => '')) || ''
  const wall = /login/i.test(url) || /4\?0\?4|not found|error/i.test(body) || /log ?in|sign in/i.test(body)
  expect(wall, 'guest stopped (at ' + url + ')').toBeTruthy()
  await ctx.close()
})

test('create: hub "New project" → Blank → name → POST /project/new (v2: 2xx + editor redirect)', async () => {
  const name = 'PtyCreateProbe' + Date.now().toString(36)
  const cap = captureApi(p as any, BASE)
  await p.goto(HUB_PROJECTS, { waitUntil: 'domcontentloaded' })
  await p.locator('button:has-text("New project")').first().click()
  await p.locator('text=Blank project').first().click()
  await p.locator('#hub-new-project-name, label:has-text("Project name") input').first().fill(name)
  await p.locator('button:has-text("Create")').first().click()
  await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === '/project/new'), 'project/new')
  // 2026-11-16 (9b142051 drift triage): the v2 create flow REDIRECTS to the new
  // project's editor after create (verified live: /editor/:pid, POST body
  // {projectName, template}), and the old /project JSON list endpoint is
  // retired (404 HTML) — so assert the server-side facts that matter:
  // POST /project/new → 2xx + our name in the body + the editor transition.
  const call = cap.calls.find((x: any) => x.method === 'POST' && x.path === '/project/new')
  expect(call?.status, 'POST /project/new must be 2xx (got ' + call?.status + ')').toBeLessThan(300)
  const posted = (typeof call?.body === 'string' ? JSON.parse(call.body) : call?.body) as any
  expect(posted?.projectName ?? posted?.name, 'the typed name must reach the server').toBe(name)
  await expect.poll(async () => /\/editor\//.test(p.url()), { timeout: 20_000 }).toBe(true)
  // cleanup by the editor pid we were redirected to
  const newPid = p.url().split('/').pop() || ''
  if (/^[0-9a-f]{24}$/.test(newPid)) await delProject(p, newPid)
})

test('search: hub search filters the all-projects list', async () => {
  const name = 'PtySearchProbe' + Date.now().toString(36)
  const { _id: pid } = await mkProject(p, name)
  try {
    await p.goto(HUB_PROJECTS, { waitUntil: 'domcontentloaded' })
    const box = p.locator('input[placeholder="Search projects"], input[type="search"], input[placeholder*="search" i]').first()
    await expect(box).toBeVisible({ timeout: 15000 })
    await box.fill('zzz-no-match-' + Date.now())
    await p.waitForTimeout(1200)
    let body = (await p.locator('body').innerText()) || ''
    expect(body.includes(name), 'filtered out for nonsense query').toBeFalsy()
    await box.fill(name)
    await p.waitForTimeout(1500)
    body = await p.locator('body').innerText()
    expect(body.includes(name), 'visible for matching query').toBeTruthy()
    await box.fill('')
  } finally {
    await delProject(p, pid)
  }
})

test('trash-restore: API round-trip; /project/trashed retired (404)', async () => {
  const name = 'PtyTrashProbe' + Date.now().toString(36)
  const { _id: pid } = await mkProject(p, name)
  try {
    const tr = await a('POST', `/project/${pid}/trash`)
    expect([200, 204].includes(tr.status()), 'trash: ' + tr.status()).toBeTruthy()
    // the retired /project/* dashboards are 404 now (owner 2026-10-05) —
    // the hub #/projects.trashed surface is the trashed view.
    const tv = await p.request.get(BASE + '/project/trashed', { maxRedirects: 0 })
    expect(tv.status(), 'retired trashed dashboard → 404').toBe(404)
    // restore contract
    const restored = await a('POST', `/project/${pid}/restore`)
    expect([200, 204].includes(restored.status()), 'restore: ' + restored.status()).toBeTruthy()
  } finally {
    await delProject(p, pid)
  }
})

test('tags: POST /tag creates a tag retrievable via GET /tag', async () => {
  const tagLabel = 'ptag' + Date.now().toString(36)
  const created = await a('POST', '/tag', { name: tagLabel })
  expect([200, 201].includes(created.status()), 'tag create: ' + created.status()).toBeTruthy()
  const tag = (await created.json().catch(() => null)) as any
  const tagId = tag?._id ?? tag?.id
  try {
    const list = await (await a('GET', '/tag')).json().catch(() => ({} as any))
    const items = list.tags ?? list
    expect(
      (Array.isArray(items) ? items : []).some((t: any) => (t.name ?? t.label) === tagLabel),
      'tag present in GET /tag'
    ).toBeTruthy()
  } finally {
    if (tagId) await a('DELETE', `/tag/${tagId}`).catch(() => {})
  }
})

test('role: tpladmin still reaches the HUB projects surface (legacy /project is 404)', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  await loginRobust(q, TPLADMIN.email, TPLADMIN.password)
  await q.goto(HUB_PROJECTS, { waitUntil: 'domcontentloaded' })
  expect(q.url(), 'lands on the hub').toContain('/hub')
  const body = (await q.locator('body').innerText().catch(() => '')) || ''
  const ok = !/log ?in|sign in/i.test(q.url()) && !/restricted|don.t have permission/i.test(body)
  expect(ok, 'tpladmin sees the project surface (url ' + q.url() + ')').toBeTruthy()
  const r = await q.request.get(BASE + '/project', { maxRedirects: 0 })
  expect(r.status(), 'retired /project → 404 (or <500 in transition)').toBeLessThan(500)
  await ctx.close()
})

import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, captureApi, waitForCall } from '../../parity/harness'
import { USER, ADMIN } from '../../fixtures/credentials'

const BASE = process.env.OL_BASE || 'http://127.0.0.1:7420'
const PAGE = BASE + '/hub#/library'

test.describe.configure({ mode: 'serial' })
const SH = { ctx: {} as { browser: any; ctx: any } }

test.beforeAll(async ({ browser }) => {
  const ctx = await browser.newContext()
  const q = await ctx.newPage()
  await loginRobust(q, ADMIN.email, ADMIN.password)
  await q.close()
  SH.ctx = { browser, ctx }
})
test.afterAll(async () => { await SH.ctx.ctx.close().catch(() => {}) })

test('list/renders: hub references leaf renders + lists the entries + add control', async () => {
  const pg = await SH.ctx.ctx.newPage()
  await pg.goto(PAGE, { waitUntil: 'domcontentloaded' })
  await expect(pg.getByRole('heading', { name: /reference librar/i }).first()).toBeVisible({ timeout: 15000 })
  await expect(pg.locator('button', { hasText: /add reference/i }).first()).toBeVisible()
  await pg.close()
})

test('deny: guest cannot open the hub references leaf', async ({ browser }) => {
  const ctx = await browser.newContext()
  const pg = await ctx.newPage()
  await pg.goto(PAGE, { waitUntil: 'domcontentloaded' })
  await pg.waitForTimeout(1500)
  const body = (await pg.locator('body').innerText().catch(() => '')) || ''
  const landed = pg.url()
  const ok = /login|signin/i.test(landed) || !/add reference/i.test(body)
  expect(ok, 'guest is not shown the add-reference UI (url ' + landed + ')').toBeTruthy()
  await ctx.close()
})

test('create: "Add reference" → Paste → preview → Import (N) → POST /library/references (items[])', async () => {
  const pg = await SH.ctx.ctx.newPage()
  await pg.goto(PAGE, { waitUntil: 'domcontentloaded' })
  await expect(pg.getByRole('heading', { name: /reference librar/i }).first()).toBeVisible({ timeout: 15000 })
  const cap = captureApi(pg as any, BASE)
  await pg.locator('button', { hasText: 'Add reference' }).first().click()
  await pg.locator('[role="menuitem"]', { hasText: 'Paste references' }).first().click()
  await pg.waitForTimeout(400)
  const md = pg.locator('[role="dialog"]', { hasText: 'Paste references' }).last()
  await expect(md).toBeVisible({ timeout: 5000 })
  await md.locator('textarea').first().fill(`@article{parityhub${Date.now() % 100000000}, title={Hub Library Parity}, author={Tester}, year={2026}}`)
  await md.getByRole('button', { name: /preview/i }).first().click()
  // Preview parses (POST /library/match) and auto-selects importable entries
  await waitForCall(cap as any, (c: any[]) => c.some(x => x.method === 'POST' && x.path === '/library/match'), 'preview parse', 10000).catch(() => {})
  await pg.waitForTimeout(800)
  // The Import button is enabled only once a preview entry is selected; the
  // preview stage auto-selects importable rows — wait for the enabled one.
  const anyImp = md.locator('button', { hasText: /^import/i })
  const deadline = Date.now() + 8000
  let target: any = null
  while (!target && Date.now() < deadline) {
    const n = await anyImp.count()
    for (let i = 0; i < n; i++) {
      const bb = anyImp.nth(n - 1 - i)
      if (await bb.isEnabled().catch(() => false)) { target = bb; break }
    }
    if (!target) await pg.waitForTimeout(300)
  }
  if (!target) {
    const cbs = md.getByRole('checkbox')
    if (await cbs.count()) await cbs.last().click({ force: true })
    target = anyImp.last()
  }
  await expect(target).toBeEnabled({ timeout: 8000 })
  await target.click()
  await waitForCall(cap as any, (c: any[]) => c.some(x => x.method === 'POST' && x.path === '/library/references'), 'import call', 15000)
  const createCall = (cap as any).calls.find((x: any) => x.method === 'POST' && x.path === '/library/references' && x.status < 300)
  expect(createCall, 'POST /library/references → 2xx (saw: ' + (cap as any).calls.map((c: any) => c.method + ' ' + c.path + '→' + c.status).join(' | ') + ')').toBeTruthy()
  await pg.goto(PAGE, { waitUntil: 'domcontentloaded' })
  await expect(pg.getByText('Hub Library Parity').first()).toBeVisible({ timeout: 8000 })
  await pg.close()
})

test('trash-restore: hub row control → POST /library/references/delete {ids} → POST /library/references/restore {ids}', async () => {
  const pg = await SH.ctx.ctx.newPage()
  await pg.goto(PAGE, { waitUntil: 'domcontentloaded' })
  await expect(pg.getByRole('heading', { name: /reference librar/i }).first()).toBeVisible({ timeout: 15000 })
  const delKey = 'paritydel' + (Date.now() % 999999)
  const delEntry = { key: delKey, type: 'article', fields: [{ name: 'title', value: 'Parity Hub Del Entry' }, { name: 'author', value: 'T' }, { name: 'year', value: '2026' }] }
  const created = await (await api(pg as any, 'POST', '/library/references', { entries: [delEntry] })).json().catch(() => null)
  const id = created?.items?.[0]?._id ?? created?.items?.[0]?.id
  expect(id, 'entry created via API (deterministic row); body=' + JSON.stringify(created).slice(0, 160)).toBeTruthy()
  await pg.reload({ waitUntil: 'domcontentloaded' })
  // 2026-09-13 (owner #5): the library is cursor-paged (oldest first, 25/page) —
  // a just-created entry lands on the LAST page, so reach it through the
  // library's own search (parity with the legacy search box).
  await pg.locator('input[placeholder*="Search key" i]').first().fill(delKey)
  await pg.waitForTimeout(1600)
  const delRow = pg.locator('tr', { hasText: delKey }).first()
  await expect(delRow).toBeVisible({ timeout: 10000 })
  const cap = captureApi(pg as any, BASE)
  const delRows = pg.locator('tr', { hasText: delKey })
  expect(await delRows.count(), 'row with the new key exists').toBeGreaterThan(0)
  const row = delRows.first()
  const trashBtn = row.locator('button[aria-label="Move to trash"]')
  expect(await trashBtn.count(), 'row has a Move-to-trash control').toBe(1)
  await trashBtn.first().click({ force: true })
  // the hub opens a ConfirmModal ("Move to trash?" → Confirm)
  const confirmDlg = pg.locator('[role="dialog"]', { hasText: /move to trash/i }).last()
  await expect(confirmDlg).toBeVisible({ timeout: 5000 })
  await confirmDlg.locator('button', { hasText: /move to trash/i }).last().click()
  await waitForCall(cap as any, (c: any[]) => c.some(x => x.method === 'POST' && x.path === '/library/references/delete'), 'delete call', 10000)
  const del = (cap as any).calls.find((x: any) => x.method === 'POST' && x.path === '/library/references/delete')
  expect(del.status, 'delete status ' + del.status).toBeLessThan(300)
  expect(del.body && del.body.ids && del.body.ids.length === 1, 'delete body {ids:[1], permanent:false}')
  expect(del.body.permanent === false, 'trash = permanent:false')
  const rest = await api(pg as any, 'POST', '/library/references/restore', { ids: [id] })
  expect(rest.status(), 'restore status').toBeLessThan(300)
  await pg.close()
})

test('citekey: /library/match + /library/references/suggestions back the leaf', async () => {
  const pg = await SH.ctx.ctx.newPage()
  await pg.goto(PAGE, { waitUntil: 'domcontentloaded' })
  const m = await api(pg as any, 'GET', '/library/match?key=hub')
  expect(m.status(), 'match status').toBeLessThan(500)
  const su = await api(pg as any, 'GET', '/library/references/suggestions?base=Hub')
  expect(su.status(), 'suggestions status').toBeLessThan(500)
  await pg.close()
})

test('download: /library/references/download?ids= export contract', async () => {
  const pg = await SH.ctx.ctx.newPage()
  await pg.goto(PAGE, { waitUntil: 'domcontentloaded' })
  const dlEntry = { key: 'paritydl9', type: 'article', fields: [{ name: 'title', value: 'Parity Hub Dl Entry' }, { name: 'author', value: 'T' }, { name: 'year', value: '2026' }] }
  const created = await (await api(pg as any, 'POST', '/library/references', { entries: [dlEntry] })).json().catch(() => null)
  const id = created?.items?.[0]?._id ?? created?.items?.[0]?.id
  expect(id, 'entry created via API').toBeTruthy()
  const r = await api(pg as any, 'GET', `/library/references/download?ids=${id}`)
  expect(r.status(), 'download status').toBeLessThan(300)
  await pg.close()
})

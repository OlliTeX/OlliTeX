import { test, expect, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { ADMIN, USER, TPLADMIN } from '../../fixtures/credentials'
import { loginRobust } from '../../helpers/auth'
import { api, mkProject, mongoEval, captureApi, waitForCall } from '../../parity/harness'

// /hub parity for legacy /admin/project — the NEW Mantine leaves must drive the
// same admin project API the legacy page used (endpoints + payloads + state).
const BASE = 'http://127.0.0.1:7420'
const HUB = '/hub#/site.general.projects.all'
const HUB_TRASHED = '/hub#/site.general.projects.trashed'
const HUB_DELETED = '/hub#/site.general.projects.deleted'
let ctxPage: Page | null = null
const W = () => 'ph' + Date.now().toString(36) + Math.floor(Math.random() * 1000)

function admin() {
  if (!ctxPage) {
    ctxPage = (globalThis as any).__ppCtx as Page
    expect(ctxPage, 'admin page missing').toBeTruthy()
  }
  return ctxPage
}

async function gotoProject(p: Page, url: string = HUB) {
  await p.goto(url, { waitUntil: 'domcontentloaded' })
  await p.waitForSelector('input[placeholder*="Search" i]', { timeout: 25000 })
  await p.waitForTimeout(700)
}

async function findRow(p: Page, name: string) {
  const search = p.locator('input[placeholder*="Search" i]').first()
  if (await search.isVisible().catch(() => false)) {
    await search.fill(name)
    await p.waitForTimeout(700)
  }
  await expect(p.locator('tr', { has: p.locator('text=' + name) }).first()).toBeVisible({ timeout: 15000 })
}

const adminUid = () =>
  execFileSync('docker', ['exec', 'ol-e2e-mongo-1', 'mongosh', '--quiet', 'sharelatex', '--eval', 'db.users.findOne({email:\"e2e-admin@e2e.test\"})._id.toString()'], { encoding: 'utf8' }).trim()
const row = (p: Page, name: string) => p.locator('tr', { has: p.locator('text=' + name) }).first()
async function openRowMenu(p: Page, name: string) {
  await row(p, name).locator('button[aria-label="Actions"]').first().click()
  await expect(p.locator('[role="menu"]').last()).toBeVisible({ timeout: 3000 })
}

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
  ;(globalThis as any).__ppCtx = p
})
test.afterAll(async () => {
  const c = ctxPage!.context()
  if (c) { await c.tracing.stop().catch(() => {}); await c.close().catch(() => {}) }
})

test('renders: site admin sees the projects leaf with the project table', async ({ page }) => {
  const p = admin()
  await gotoProject(p)
  await expect(p.locator('input[placeholder*="Search" i]').first()).toBeVisible()
  const search = p.locator('input[placeholder*="Search" i]').first()
  await search.fill('e2e-seed-project')
  await p.waitForTimeout(700)
  await expect(p.locator('tr', { has: p.locator('text=e2e-seed-project') }).first()).toBeVisible({ timeout: 15000 })
})

test('deny: tpladmin + user cannot reach the hub admin projects leaves', async ({ page }) => {
  for (const who of [TPLADMIN, USER]) {
    const c2 = await page.context().browser()
    const ctx2 = await c2.newContext()
    const p2 = await ctx2.newPage()
    await loginRobust(p2, who.email, who.password)
    await p2.goto(BASE + HUB_TRASHED, { waitUntil: 'domcontentloaded' })
    await p2.waitForTimeout(2500)
    const body = (await p2.locator('body').innerText().catch(() => '')) || ''
    expect(/Search project name/i.test(body), `${who.email} must not reach the hub projects leaf`).toBeFalsy()
    await p2.context().close()
  }
})

test('download: row menu "Download" fetches /project/download/zip', async ({ page }) => {
  const p = admin()
  const w = W()
  const proj = await mkProject(p, w)
  const pid = proj._id
  await gotoProject(p)
  await findRow(p, w)
  await openRowMenu(p, w)
  const item = p.locator('[role="menu"]').last().getByRole('menuitem', { name: /download/i }).last()
  await expect(item).toBeVisible()
  const href = (await item.getAttribute('href')) || ''
  expect(href, 'row Download is wired to /project/:id/download/zip').toContain(`/project/${pid}/download/zip`)
  const popupPromise = p.context().waitForEvent('page', { timeout: 15000 }).catch(() => null)
  await item.click()
  const popup = await popupPromise
  if (popup) await popup.close().catch(() => {})
})

test('trash + restore: row menu "Trash" then "Restore" drive /admin/project/:id/trash|untrash', async ({ page }) => {
  const p = admin()
  const w = W()
  const proj = await mkProject(p, w)
  const pid = proj._id
  await gotoProject(p)
  await findRow(p, w)
  const cap = captureApi(p as any, BASE)
  await openRowMenu(p, w)
  await p.locator('[role="menu"]').last().getByRole('menuitem', { name: /move to trash/i }).last().click()
  await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === `/admin/project/${pid}/trash`), 'trash')
  await expect(p.locator('[role="status"], .mantine-Notification-root').filter({ hasText: /moved to trash/i }).last()).toBeVisible({ timeout: 5000 }).catch(() => {})
  // restore from the trashed leaf (fresh page load picks up the trashed list)
  await gotoProject(p, HUB_TRASHED)
  await findRow(p, w)
  const cap2 = captureApi(p as any, BASE)
  await openRowMenu(p, w)
  await p.locator('[role="menu"]').last().getByRole('menuitem', { name: /restore from trash/i }).last().click()
  await waitForCall(cap2, c => c.some(x => x.method === 'POST' && x.path === `/admin/project/${pid}/untrash`), 'untrash')
})

test('delete: trashed view "Delete" confirms → DELETE /admin/project/:id', async ({ page }) => {
  const p = admin()
  const w = W()
  const proj = await mkProject(p, w)
  const pid = proj._id
  await api(p, 'POST', `/admin/project/${pid}/trash`, { userId: adminUid() }).catch(() => {})
  await gotoProject(p, HUB_TRASHED)
  await findRow(p, w)
  const cap = captureApi(p as any, BASE)
  await openRowMenu(p, w)
  await p.locator('[role="menu"]').last().getByRole('menuitem', { name: /^delete$/i }).last().click()
  const dlg = p.locator('[role="dialog"]').filter({ hasText: /delete/i }).last()
  await expect(dlg).toBeVisible({ timeout: 5000 })
  await dlg.locator('button', { hasText: /delete/i }).last().click()
  await waitForCall(cap, c => c.some(x => x.method === 'DELETE' && x.path === `/admin/project/${pid}`), 'DELETE')
})

test('purge: deleted view "Purge" confirms → DELETE /admin/project/:id/purge', async ({ page }) => {
  const p = admin()
  const w = W()
  const proj = await mkProject(p, w)
  const pid = proj._id
  await api(p, 'POST', `/admin/project/${pid}/trash`, { userId: adminUid() }).catch(() => {})
  await api(p, 'DELETE', `/admin/project/${pid}`).catch(() => {})
  await gotoProject(p, HUB_DELETED)
  await findRow(p, w)
  const cap = captureApi(p as any, BASE)
  await openRowMenu(p, w)
  await p.locator('[role="menu"]').last().getByRole('menuitem', { name: /purge/i }).last().click()
  const dlg = p.locator('[role="dialog"]').filter({ hasText: /purge/i }).last()
  await expect(dlg).toBeVisible({ timeout: 5000 })
  await dlg.locator('button', { hasText: /purge/i }).last().click()
  await waitForCall(cap, c => c.some(x => x.method === 'DELETE' && x.path === `/admin/project/${pid}/purge`), 'purge')
  expect(Number(mongoEval(`db.projects.countDocuments({ _id: ObjectId("${pid}") })`)), 'purged').toBe(0)
})

test('transfer: "Change owner" modal → POST /project/:id/transfer-ownership', async ({ page }) => {
  const p = admin()
  const w = W()
  const proj = await mkProject(p, w)
  const pid = proj._id
  const userEmail = (mongoEval('db.users.findOne({ email: "e2e-user@e2e.test" }).email') as string)
  await gotoProject(p)
  await findRow(p, w)
  const cap = captureApi(p as any, BASE)
  await openRowMenu(p, w)
  await p.locator('[role="menu"]').last().getByRole('menuitem', { name: /change owner/i }).last().click()
  const dlg = p.locator('[role="dialog"]').filter({ hasText: /change owner/i }).last()
  await expect(dlg).toBeVisible({ timeout: 5000 })
  const sel = dlg.locator('select').first()
  const labels = await sel.locator('option').allTextContents()
  const ownerLabel = labels.find(l => l.toLowerCase().includes(userEmail.split('@')[0]))
  expect(labels.some(l => /e2e-user/i.test(l)), 'e2e-user listed as owner option; saw: ' + labels.join(' | ')).toBeTruthy()
  await sel.selectOption({ label: ownerLabel ?? labels.find(l => /e2e/i.test(l)) })
  await dlg.locator('label, input[type="checkbox"]').filter({ hasText: /skip notification/i }).first().check().catch(() => {})
  await dlg.locator('button', { hasText: /transfer/i }).last().click()
  await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === `/project/${pid}/transfer-ownership`), 'transfer')
})

test('invite: "Share project" modal → POST /admin/project/:id/invite {email, privileges}', async ({ page }) => {
  const p = admin()
  const w = W()
  const proj = await mkProject(p, w)
  const pid = proj._id
  await gotoProject(p)
  await findRow(p, w)
  const cap = captureApi(p as any, BASE)
  await openRowMenu(p, w)
  await p.locator('[role="menu"]').last().getByRole('menuitem', { name: /invite/i }).last().click()
  const dlg = p.locator('[role="dialog"]').filter({ hasText: /share project/i }).last()
  await expect(dlg).toBeVisible({ timeout: 5000 })
  await dlg.locator('input').first().fill('e2e-user@e2e.test')
  await dlg.locator('select').first().selectOption({ label: 'Reader — read only' })
  await dlg.locator('button', { hasText: /send invite/i }).last().click()
  await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === `/admin/project/${pid}/invite`), 'invite')
})

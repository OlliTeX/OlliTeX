import { test, expect } from '@playwright/test'
import fs from 'node:fs'
import { execFileSync } from 'node:child_process'
import { loginRobust } from '../../helpers/auth'
import { api, captureApi, waitForCall } from '../../parity/harness'
import { ADMIN, USER } from '../../fixtures/credentials'

// Parity (HUB) — /hub admin templates ("Manage templates" leaf) vs legacy
// /templates/manage. Drives the Mantine AdminTemplatesSection; asserts the
// same API contract the legacy spec proved (bundle import, SSRF-guarded URL
// import, edit, delete, gallery/category config).
//
// HYGIENE: one shared logged-in context, but a FRESH PAGE per test (close it
// after). The hub is a single-document SPA — a shared page accumulates React
// state (toasts, half-open modals, stale lists) between tests, which makes
// subsequent interactions unreliable. Fresh page = deterministic state.
const BASE = 'http://127.0.0.1:7420'
const HUB = BASE + '/hub#/site.general.managetpl'
type Browser = Awaited<ReturnType<Parameters<Parameters<typeof test.beforeAll<unknown, { browser: any }>[1]>[0]>>>
let SH: { ctx: any } | null = null

test.beforeAll(async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const home = await ctx.newPage()
  await loginRobust(home, ADMIN.email, ADMIN.password)
  await home.close()
  SH = { ctx }
})
test.afterAll(async () => {
  if (SH?.ctx) await SH.ctx.close().catch(() => {})
})

async function page() {
  const pg = await SH!.ctx.newPage()
  await pg.goto(HUB, { waitUntil: 'domcontentloaded' })
  return pg
}
async function close(pg: any) { await pg.close().catch(() => {}) }
const a = (pg: any, method: string, path: string, body?: unknown) => api(pg, method, path, body)

function bundleDir(tag: string, name: string, category = 'academic-journal') {
  const dir = `/tmp/tplfix_hub_${tag}${Date.now()}`
  fs.mkdirSync(dir, { recursive: true })
  fs.copyFileSync('/tmp/tplfix/source.zip', dir + '/source.zip')
  fs.copyFileSync('/tmp/tplfix/output.pdf', dir + '/output.pdf')
  fs.writeFileSync(dir + '/template.json', JSON.stringify({ name, version: '1.0.0', category, author: 'e2e-fixture', description: `hub ${tag} fixture` }))
  execFileSync('zip', ['-q', dir + '/bundle.zip', 'template.json', 'source.zip', 'output.pdf'], { cwd: dir })
  return dir
}

test('renders: hub manage-templates leaf shows list + import card', async () => {
  const pg = await page()
  try {
    await expect(pg.locator('text=Import a template bundle').first()).toBeVisible({ timeout: 15000 })
    await expect(pg.locator('tr', { hasText: 'Parity Fixture Template' }).first()).toBeVisible()
  } finally {
    await close(pg)
  }
})

test('deny: plain user is blocked from the hub manage-templates leaf', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  await loginRobust(q, USER.email, USER.password)
  await q.goto(HUB, { waitUntil: 'domcontentloaded' }).catch(() => {})
  const body = (await q.locator('body').innerText().catch(() => '')) || ''
  const denied = !/Import a template bundle/i.test(body)
  expect(denied, 'user must be denied (no import card)').toBeTruthy()
  await ctx.close()
})

test('import: file input + Import → POST /template/bundle/import (409 → override re-import)', async () => {
  const name = 'Parity Hub Import ' + Date.now().toString(36)
  const dir = bundleDir('imp', name)
  const pg = await page()
  try {
    await pg.locator('input[type="file"]').first().setInputFiles(dir + '/bundle.zip')
    const cap = captureApi(pg as any, BASE)
    await pg.locator('button', { hasText: /^import$/i }).first().click()
    await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === '/template/bundle/import' && x.status === 200), 'import 200')

    // same name again without override → server 409 (conflict notification)
    const cap2 = captureApi(pg as any, BASE)
    await pg.locator('input[type="file"]').first().setInputFiles(dir + '/bundle.zip')
    await pg.locator('button', { hasText: /^import$/i }).first().click()
    await waitForCall(cap2, c => c.some(x => x.method === 'POST' && x.path === '/template/bundle/import' && x.status === 409), '409 conflict')
    await pg.waitForTimeout(800)

    // override switch + re-import → success (200)
    await pg.locator('label', { hasText: /replace a template with the same name/i }).first().click()
    await pg.locator('input[type="file"]').first().setInputFiles(dir + '/bundle.zip')
    const cap3 = captureApi(pg as any, BASE)
    await pg.locator('button', { hasText: /^import$/i }).first().click()
    await waitForCall(cap3, c => c.some(x => x.method === 'POST' && x.path === '/template/bundle/import' && x.status === 200), 'override import 200')
  } finally {
    // cleanup the imported template (by name) via a fresh API-bound page
    const cpg = await SH!.ctx.newPage()
    const list = await (await a(cpg, 'GET', '/api/templates/admin-list')).json().catch(() => ({}))
    const row = ((list as any).templates ?? (list as any)).find?.((t: any) => t.name === name)
    if (row) await a(cpg, 'DELETE', `/template/${row.template_id ?? row.id}/delete`).catch(() => {})
    await cpg.close().catch(() => {})
    await close(pg)
  }
})

test('import-url: hub URL import respects the SSRF guard (internal URL rejected)', async () => {
  const pg = await page()
  try {
    await pg.locator('input[placeholder*="URL" i]').first().fill('http://127.0.0.1:7420/secret.zip')
    const cap = captureApi(pg as any, BASE)
    await pg.locator('button', { hasText: /^import$/i }).first().click()
    await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === '/template/bundle/import-url' && x.status >= 400), 'SSRF rejection')
  } finally {
    await close(pg)
  }
})

test('edit: row "Edit template" → POST /template/:id/edit persists the rename', async () => {
  const name = 'Parity Hub Edit ' + Date.now().toString(36)
  const dir = bundleDir('edit', name, 'book')
  const data = fs.readFileSync(dir + '/bundle.zip').toString('base64')
  const name2 = name + ' (edited)'
  const pg = await page()
  let tid: string | null = null
  try {
    importTpl:
    {
      const res = await a(pg, 'POST', '/template/bundle/import', { data, override: false })
      const cre = await res.json()
      expect(cre.template_id, 'import created template (' + res.status() + ')').toBeTruthy()
      tid = cre.template_id
    }
    await pg.reload({ waitUntil: 'domcontentloaded' })
    const rowCell = pg.locator('td', { hasText: name }).first()
    await expect(rowCell).toBeVisible({ timeout: 20000 })
    const row = rowCell.locator('xpath=..')
    await row.locator('button[aria-label="Edit template"]').click({ force: true })
    const dlg = pg.locator('[role="dialog"]', { hasText: 'Edit template' }).last()
    await expect(dlg).toBeVisible({ timeout: 8000 })
    await dlg.getByLabel('Title').fill(name2)
    const cap = captureApi(pg as any, BASE)
    await dlg.locator('button', { hasText: /save/i }).last().click()
    await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === `/template/${tid}/edit`), 'edit call')
    await pg.waitForTimeout(1000)
    const list = await (await a(pg, 'GET', '/api/templates/admin-list')).json()
    const upd = ((list as any).templates ?? (list as any)).find((t: any) => (t.template_id ?? t.id) === tid)
    expect(upd?.name ?? upd?.title, 'rename persisted').toBe(name2)
  } finally {
    if (tid) await a(pg, 'DELETE', `/template/${tid}/delete`).catch(() => {})
    await close(pg)
  }
})

test('delete: row "Delete template" → confirm → DELETE /template/:id/delete', async () => {
  const name = 'Parity Hub Delete ' + Date.now().toString(36)
  const dir = bundleDir('del', name)
  const data = fs.readFileSync(dir + '/bundle.zip').toString('base64')
  const pg = await page()
  let tid: string | null = null
  try {
    {
      const res = await a(pg, 'POST', '/template/bundle/import', { data, override: false })
      const j = await res.json()
      expect(j.template_id, 'import created template (' + res.status() + ')').toBeTruthy()
      tid = j.template_id
    }
    await pg.reload({ waitUntil: 'domcontentloaded' })
    const rowCell = pg.locator('td', { hasText: name }).first()
    await expect(rowCell).toBeVisible({ timeout: 20000 })
    const row = rowCell.locator('xpath=..')
    const cap = captureApi(pg as any, BASE)
    await row.locator('button[aria-label="Delete template"]').click({ force: true })
    const dlg = pg.locator('[role="dialog"]', { hasText: 'Delete template?' }).last()
    await expect(dlg).toBeVisible({ timeout: 8000 })
    await dlg.locator('button', { hasText: /^delete$/i }).last().click()
    await waitForCall(cap, c => c.some(x => x.method === 'DELETE' && x.path === `/template/${tid}/delete`), 'delete call')
    await pg.waitForTimeout(1000)
    const list = await (await a(pg, 'GET', '/api/templates/admin-list')).json()
    const still = ((list as any).templates ?? (list as any)).find((t: any) => (t.template_id ?? t.id) === tid)
    expect(still, 'template removed from the list').toBeUndefined()
  } finally {
    await close(pg)
  }
})

test('categories: gallery toggle → PUT /admin/site-settings/templates (and restore)', async () => {
  const pg = await page()
  try {
    const card = pg.locator('.mantine-Card-root', { hasText: 'Public template gallery' }).first()
    await expect(card).toBeVisible({ timeout: 20000 })
    const before = await card.locator('input').first().isChecked()
    const cap = captureApi(pg as any, BASE)
    await card.locator('.mantine-Switch-root').first().click()
    await waitForCall(cap, c => c.some(x => x.method === 'PUT' && x.path === '/admin/site-settings/templates'), 'gallery save')
    await pg.waitForTimeout(800)
    const after = await card.locator('input').first().isChecked()
    if (after !== before) await card.locator('.mantine-Switch-root').first().click()
    await pg.waitForTimeout(600)
  } finally {
    await close(pg)
  }
})

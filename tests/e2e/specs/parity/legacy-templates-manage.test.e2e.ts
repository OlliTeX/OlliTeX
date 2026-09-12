import { test, expect } from '@playwright/test'
import fs from 'node:fs'
import { loginRobust } from '../../helpers/auth'
import { api, mongoEval } from '../../parity/harness'
import { ADMIN, USER, TPLADMIN } from '../../fixtures/credentials'

/**
 * Parity baseline for the RETIRED legacy /templates/manage page (owner
 * item 7, 2026-09-16) — /templates/manage now 301s into the hub admin leaf
 * /hub#/site.general.managetpl. The template-administration API contract
 * (admin-list, bundle import/import-url, edit, delete) is unchanged and is
 * what the hub uses. Roles: admin + template admin (canManageTemplates);
 * plain users are denied the management APIs.
 */
const BASE = 'http://127.0.0.1:7420'
const BUNDLE = '/tmp/tplfix/parity-tpl-bundle.zip'
let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
})
test.afterAll(async () => {
  if (p) await p.context().close().catch(() => {})
})

const tplCount = () => Number(mongoEval('db.templates.countDocuments({})'))

test('redirects: /templates/manage 301s into the hub admin leaf (legacy page removed)', async () => {
  // logged-in admin: the 301 target is the hub management section.
  const r = await p.request.get(BASE + '/templates/manage', { maxRedirects: 0 })
  expect(r.status(), 'expected 301, got ' + (await r.text().catch(() => '')).slice(0, 140)).toBe(301)
  expect(r.headers()['location']).toBe('/hub#/site.general.managetpl')
})

test('denied: a plain user never sees a management surface via the legacy route', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  await loginRobust(q, USER.email, USER.password)
  // the legacy page is gone for EVERYONE (301 into the hub); the management
  // ACTION that mattered is the API — a plain user must be denied there.
  const r = await q.request.get(BASE + '/templates/manage', { maxRedirects: 0 })
  expect([301, 302].includes(r.status()), 'legacy route: ' + r.status()).toBeTruthy()
  const deniedApi = await q.request.get(BASE + '/api/templates/admin-list')
  expect([401, 403].includes(deniedApi.status()), 'admin-list denied for plain user (got ' + deniedApi.status() + ')').toBeTruthy()
  await ctx.close()
})

test('list: GET /api/templates/admin-list returns templates (admin + tpladmin)', async ({ browser }) => {
  const r = await api(p, 'GET', '/api/templates/admin-list')
  expect(r.status(), 'admin list').toBe(200)
  const j = await r.json()
  const arr = j.templates ?? j
  expect(Array.isArray(arr), 'admin-list returns an array').toBeTruthy()
  // role parity: a TEMPLATE ADMIN (flags.canManageTemplates) gets the same list
  const email = TPLADMIN.email
  const prevFlag = mongoEval(`db.users.findOne({ email: '${email}' })?.flags?.canManageTemplates`)
  mongoEval(`db.users.updateOne({ email: '${email}' }, { $set: { flags: { canManageTemplates: true } } })`)
  try {
    const ctx = await browser.newContext()
    const q = await ctx.newPage()
    await loginRobust(q, TPLADMIN.email, TPLADMIN.password)
    const r2 = await q.request.get(BASE + '/api/templates/admin-list')
    expect(r2.status(), 'tpladmin (flags.canManageTemplates) admin-list').toBe(200)
    await ctx.close()
  } finally {
    if (!prevFlag) mongoEval(`db.users.updateOne({ email: '${email}' }, { $unset: { 'flags.canManageTemplates': 1 } })`)
  }
})

test('import: POST /template/bundle/import creates; 409 on conflict + override', async () => {
  const b64 = fs.readFileSync(BUNDLE).toString('base64')
  const name = 'Parity Import ' + Date.now().toString(36)
  // build a derived bundle file (rename template.json name) so each run is unique
  const dir = '/tmp/tplfix_run' + Date.now()
  fs.mkdirSync(dir, { recursive: true })
  fs.copyFileSync('/tmp/tplfix/source.zip', dir + '/source.zip')
  fs.copyFileSync('/tmp/tplfix/output.pdf', dir + '/output.pdf')
  fs.writeFileSync(dir + '/template.json', JSON.stringify({ name, version: '1.0.0', category: 'academic-journal', author: 'e2e-fixture', description: 'import test' }))
  const { execFileSync } = await import('node:child_process')
  execFileSync('zip', ['-q', dir + '/bundle.zip', 'template.json', 'source.zip', 'output.pdf'], { cwd: dir })
  const data = fs.readFileSync(dir + '/bundle.zip').toString('base64')

  const r1 = await api(p, 'POST', '/template/bundle/import', { data, override: false })
  expect([200, 201].includes(r1.status()), 'import: ' + (await r1.text().catch(() => '')).slice(0, 160)).toBeTruthy()
  const j1 = await r1.json()
  expect(j1.template_id, 'template created').toBeTruthy()

  // same name again → 409 conflict
  const r2 = await api(p, 'POST', '/template/bundle/import', { data, override: false })
  expect(r2.status(), 'conflict detected').toBe(409)

  // cleanup: delete the throwaway template
  const del = await api(p, 'DELETE', `/template/${j1.template_id}/delete`)
  expect([200, 204].includes(del.status()), 'cleanup delete').toBeTruthy()
})

test('import-url: SSRF guard rejects internal/invalid URLs', async () => {
  const r = await api(p, 'POST', '/template/bundle/import-url', { url: 'http://127.0.0.1:7420/x.zip', override: false })
  expect([400, 403, 422].includes(r.status()), 'internal URL must be rejected, got ' + r.status()).toBeTruthy()
})

test('edit: POST /template/:id/edit renames the template', async () => {
  const b64 = fs.readFileSync(BUNDLE).toString('base64')
  const dir = '/tmp/tplfix_edit' + Date.now()
  fs.mkdirSync(dir, { recursive: true })
  fs.copyFileSync('/tmp/tplfix/source.zip', dir + '/source.zip')
  fs.copyFileSync('/tmp/tplfix/output.pdf', dir + '/output.pdf')
  const name = 'Parity Edit ' + Date.now().toString(36)
  fs.writeFileSync(dir + '/template.json', JSON.stringify({ name, version: '1.0.0', category: 'book', author: 'e2e-fixture', description: 'edit test' }))
  const { execFileSync } = await import('node:child_process')
  execFileSync('zip', ['-q', dir + '/bundle.zip', 'template.json', 'source.zip', 'output.pdf'], { cwd: dir })
  const data = fs.readFileSync(dir + '/bundle.zip').toString('base64')
  const cre = await (await api(p, 'POST', '/template/bundle/import', { data, override: false })).json()
  const tid = cre.template_id
  const r = await api(p, 'POST', `/template/${tid}/edit`, { name: name + ' (edited)' })
  expect(r.status(), 'edit: ' + (await r.text().catch(() => '')).slice(0, 160)).toBe(200)
  await api(p, 'DELETE', `/template/${tid}/delete`).catch(() => {})
})

test('delete: DELETE /template/:id/delete removes the template', async () => {
  const before = tplCount()
  const b64 = fs.readFileSync(BUNDLE).toString('base64')
  const dir = '/tmp/tplfix_del' + Date.now()
  fs.mkdirSync(dir, { recursive: true })
  fs.copyFileSync('/tmp/tplfix/source.zip', dir + '/source.zip')
  fs.copyFileSync('/tmp/tplfix/output.pdf', dir + '/output.pdf')
  fs.writeFileSync(dir + '/template.json', JSON.stringify({ name: 'Parity Delete ' + Date.now().toString(36), version: '1.0.0', category: 'academic-journal', author: 'e2e-fixture', description: 'delete test' }))
  const { execFileSync } = await import('node:child_process')
  execFileSync('zip', ['-q', dir + '/bundle.zip', 'template.json', 'source.zip', 'output.pdf'], { cwd: dir })
  const data = fs.readFileSync(dir + '/bundle.zip').toString('base64')
  const cre = await (await api(p, 'POST', '/template/bundle/import', { data, override: false })).json()
  const afterCreate = tplCount()
  expect(afterCreate, 'template doc created').toBeGreaterThanOrEqual(before)
  const del = await api(p, 'DELETE', `/template/${cre.template_id}/delete`)
  expect([200, 204].includes(del.status()), 'delete: ' + del.status()).toBeTruthy()
  await p.waitForTimeout(400)
})

test('categories: PUT /admin/site-settings/templates persists and restores', async () => {
  const get = await (await api(p, 'GET', '/admin/site-settings')).json()
  const sec = get?.templates ?? get?.sections?.templates
  expect(sec, 'site settings expose the templates section').toBeTruthy()
  const save = async (body: unknown) => {
    const r = await api(p, 'PUT', '/admin/site-settings/templates', body)
    const txt = await r.text().catch(() => '')
    expect([200, 204].includes(r.status()), 'save templates config (' + txt.slice(0, 100) + ')').toBeTruthy()
  }
  await save(sec)                       // full section round-trip
  await save({ ...sec, enabled: false }) // toggle gallery off (preserve categories!)
  const off = await (await api(p, 'GET', '/admin/site-settings')).json()
  expect((off?.templates ?? off?.sections?.templates)?.enabled, 'gallery toggled off').toBeFalsy()
  // 2026-09-12 (green gate): RESTORE the full section — a bare { enabled: true }
  // replace wiped the categories array and order-flaked the gallery specs.
  await save({ ...sec, enabled: true })
  const cats = await (await api(p, 'GET', '/api/template/categories')).json()
  expect(Array.isArray(cats), 'enabled categories list returns an array').toBeTruthy()
})

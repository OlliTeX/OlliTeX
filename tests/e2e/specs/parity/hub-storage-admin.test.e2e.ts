import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { ADMIN } from '../../fixtures/credentials'

// Owner 2026-09-14 direct ask: /hub admin has the Storage env-parameter section.
const BASE = 'http://127.0.0.1:7420'
let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
})
test.afterAll(async () => { if (p) await p.context().close().catch(() => {}) })
const a = (method: string, path: string, body?: unknown) => api(p, method, path, body)

test('hub /#/site.storage.local renders the storage env-parameter section', async () => {
  await p.goto(BASE + '/hub#/site.storage.local', { waitUntil: 'domcontentloaded' })
  await expect(p.locator('body').getByText('Storage (local object storage)').first()).toBeVisible({ timeout: 15_000 })
  for (const label of ['Local storage backend', 'Gateway endpoint', 'Access key ID', 'Secret access key', 'Template files bucket', 'Project blobs bucket', 'Global blobs bucket', 'Docstore archive bucket']) {
    await expect(p.locator('body').getByText(label, { exact: false }).first()).toBeVisible()
  }
  // fs/s3 options present
  const body = (await p.locator('body').innerText()) || ''
  expect(/Flat files \(fs\)/i.test(body)).toBeTruthy()
  expect(/SeaweedFS/i.test(body)).toBeTruthy()
})

test('API round trip: GET then PUT(s3) then PUT(fs-back) the storage section', async () => {
  const g = await (await a('GET', '/admin/site-settings/storage')).json().catch(() => ({}))
  expect(g, 'storage GET').toBeTruthy()
  expect(g.backend, 'GET has a backend').toBeTruthy()

  const put1 = await a('PUT', '/admin/site-settings/storage', {
    backend: 's3',
    s3Endpoint: 'http://127.0.0.1:8333',
    s3AccessKeyId: '',
    s3Secret: '',
    templateFilesBucket: 'filestore-template',
    projectBlobsBucket: 'filestore-blobs',
    globalBlobsBucket: 'filestore-global-blobs',
    docstoreArchiveBucket: 'docstore-archive',
  })
  expect(put1.status(), `PUT s3 (got ${put1.status()}: ${(await put1.text().catch(() => '')).slice(0, 160)})`).toBeLessThan(300)
  expect(put1.headers()['content-type'] ?? '', '').toBeTruthy()
  const j1 = await put1.json().catch(() => ({}))
  expect(j1.envLines, 'response lists the env lines it wrote').toBeTruthy()

  const g2 = await (await a('GET', '/admin/site-settings/storage')).json().catch(() => ({}))
  expect(g2.envManaged, 'managed fragment flag set').toBeTruthy()
  expect(g2.s3Endpoint).toBe('http://127.0.0.1:8333')
  // secret must be masked/absent on GET
  expect(g2.s3Secret, 'secret absent from GET').toBeFalsy()

  // validation: bad backend + bad bucket name
  const bad = await a('PUT', '/admin/site-settings/storage', { backend: 'gcs' })
  expect(bad.status(), 'gcs rejected').toBe(422)
  const bad2 = await a('PUT', '/admin/site-settings/storage', { templateFilesBucket: 'Bad Name!' })
  expect(bad2.status(), 'bad bucket rejected').toBe(422)

  // rollback to fs (the documented rollback path)
  const put2 = await a('PUT', '/admin/site-settings/storage', { backend: 'fs', s3Endpoint: '', s3AccessKeyId: '', s3Secret: '', templateFilesBucket: '', projectBlobsBucket: '', globalBlobsBucket: '', docstoreArchiveBucket: '' })
  expect(put2.status(), `PUT fs-back (got ${put2.status()}: ${(await put2.text().catch(() => '')).slice(0, 160)})`).toBeLessThan(300)
  const g3 = await (await a('GET', '/admin/site-settings/storage')).json().catch(() => ({}))
  expect(g3.backend, 'back to fs').toBe('fs')
})

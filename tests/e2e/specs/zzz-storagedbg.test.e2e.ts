import { test } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
const BASE = 'http://127.0.0.1:4000'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
test('overleafserver :4000 storage', async ({ browser }) => {
  const c = await browser.newContext()
  const p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
  const all = await p.request.get(BASE + '/admin/site-settings')
  const t = await all.text()
  console.log('ALL:', all.status(), 'has_storage=' + t.includes('storage'), 'has_typst=' + t.includes('typst'), 'len=' + t.length)

  const put = await p.request.put(BASE + '/admin/site-settings/storage', {
    data: { backend: 's3', s3Endpoint: 'http://127.0.0.1:8333', s3AccessKeyId: '', s3Secret: '', templateFilesBucket: 'filestore-template', projectBlobsBucket: 'filestore-blobs', globalBlobsBucket: 'filestore-global-blobs', docstoreArchiveBucket: 'docstore-archive' },
    headers: { 'Content-Type': 'application/json' },
  })
  console.log('PUT:', put.status(), (await put.text()).slice(0, 240))

  // hub UI
  await p.goto(BASE + '/hub#/site.storage.local', { waitUntil: 'domcontentloaded' })
  await p.waitForTimeout(4000)
  const body = (await p.locator('body').innerText().catch(() => '')) || ''
  console.log('HUB UI:', /Storage \(local object storage\)/.test(body), /Gateway endpoint/.test(body), /Flat files \(fs\)/.test(body))
  await c.close()
})

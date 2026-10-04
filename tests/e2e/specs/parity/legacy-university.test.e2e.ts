import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { USER } from '../../fixtures/credentials'

// /university retirement (owner decision 2026-10-05): the SaaS marketing
// pages + their /university/* family are fully removed from the Go web
// service (staticpages). The legacy Node oracle was a 302 rewrite into
// /i/university* (marketing-HTML service) — with the surface retired the
// contract is the Go 404 for every former path (anon = the global
// login bounce, logged-in = 404 page).
const BASE = 'http://127.0.0.1:7420'
let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, USER.email, USER.password)
})
test.afterAll(async () => { if (p) await p.context().close().catch(() => {}) })

test('retired: /university and /university/* are Go 404s now (staticpages removed 2026-10-05)', async () => {
  const hits: Array<[string, number]> = []
  for (const path of ['/university', '/university/Foo', '/university/a.html', '/university/A.HTML']) {
    const r = await p.request.get(BASE + path, { maxRedirects: 0 })
    hits.push([path, r.status()])
  }
  for (const [path, status] of hits) {
    expect(status, path + ' → 404').toBe(404)
  }
})

test('anchor: an unrelated live API still answers (retirement was route-scoped)', async () => {
  const r = await api(p, 'GET', '/api/templates')
  expect(r.status()).toBe(200)
})

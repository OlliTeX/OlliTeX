import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { USER, TPLADMIN } from '../../fixtures/credentials'

// Parity baseline (legacy /library) — personal reference library.
// Contract: /library/references {entries:[...]} (201 {items}), delete/restore
// {ids}, download (?ids=), count, trashed view, key suggestions.
const BASE = 'http://127.0.0.1:7420'
const PAGE = BASE + '/library'
let p: any = null
let entryId: string | null = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, USER.email, USER.password)
})
test.afterAll(async () => {
  // cleanup: permanent-delete our throwaway entry (best effort)
  if (entryId && p) {
    await p.request
      .post(BASE + '/library/references/delete', {
        headers: { 'X-CSRF-TOKEN': await p.locator('meta[name="ol-csrfToken"]').getAttribute('content'), 'Content-Type': 'application/json' },
        data: JSON.stringify({ ids: [entryId], permanent: true }),
      })
      .catch(() => {})
  }
  if (p) await p.context().close().catch(() => {})
})
const a = (method: string, path: string, body?: unknown) => api(p, method, path, body)
const KEY = 'paritylib' + Date.now().toString(36)

test('retired: /library PAGE is a Go 404 now (the hub #/library surface replaces it)', async () => {
  const r = await p.request.get(PAGE, { maxRedirects: 0 })
  expect(r.status(), '404 expected for the retired page').toBe(404)
  // the API surface the page used is untouched:
  const refs = await a('GET', '/library/references')
  expect(refs.status(), 'API stays live').toBe(200)
})

test('deny: guests hitting the retired /library page get 404 (no page, no wall needed)', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  const r = await q.request.get(PAGE, { maxRedirects: 0 })
  expect([404, 302].includes(r.status()), 'guest stopped from the retired page').toBeTruthy()
  await ctx.close()
})

test('list: GET /library/references + count agree', async () => {
  const r = await a('GET', '/library/references')
  expect(r.status()).toBe(200)
  const j = await r.json()
  expect(Array.isArray(j.items), 'items array').toBeTruthy()
  const cnt = await (await a('GET', '/library/references/count')).json()
  expect(Number(cnt.count) >= 0, 'count present').toBeTruthy()
})

test('create: POST /library/references {entries} persists; match + suggestions are graceful', async () => {
  const r = await a('POST', '/library/references', {
    entries: [{ type: 'article', key: KEY, title: 'Parity Library Entry', author: 'Parity Author', year: '2026' }],
  })
  expect([200, 201].includes(r.status()), 'create: ' + (await r.text().catch(() => '')).slice(0, 160)).toBeTruthy()
  const j = await r.json()
  const created = (j.items ?? [])[0]
  entryId = created?.id ?? created?._id ?? null
  expect(entryId, 'entry id returned').toBeTruthy()

  const list = await (await a('GET', '/library/references')).json()
  expect(list.items.some((x: any) => x.key === KEY || x.id === entryId), 'entry present in list').toBeTruthy()

  // dedupe match + key suggestions (graceful, no 5xx)
  const m = await a('POST', '/library/references/match', { entries: [{ type: 'article', key: KEY, title: 'Parity Library Entry' }] })
  expect(m.status(), 'match graceful').toBeLessThan(500)
  const sg = await a('GET', '/library/references/citation-key-suggestions?base=' + encodeURIComponent(KEY.slice(0, 6)))
  expect(sg.status(), 'suggestions graceful').toBeLessThan(500)
})

test('trash-restore: delete → trash (list gone) → restore (list back)', async () => {
  expect(entryId, 'entry created first').toBeTruthy()
  const del = await a('POST', '/library/references/delete', { ids: [entryId] })
  expect([200, 204].includes(del.status()), 'delete: ' + (await del.text().catch(() => '')).slice(0, 120)).toBeTruthy()
  let list = await (await a('GET', '/library/references')).json()
  expect(list.items.some((x: any) => x._id === entryId || x.id === entryId), 'entry gone from active list').toBeFalsy()
  const res = await a('POST', '/library/references/restore', { ids: [entryId] })
  expect([200, 204].includes(res.status()), 'restore: ' + (await res.text().catch(() => '')).slice(0, 120)).toBeTruthy()
  list = await (await a('GET', '/library/references')).json()
  expect(list.items.some((x: any) => x._id === entryId || x.id === entryId), 'entry back in list').toBeTruthy()
})

test('download: bulk .bib export returns entry content', async () => {
  expect(entryId, 'entry created first').toBeTruthy()
  const r = await a('GET', `/library/references/download?ids=${entryId}`)
  expect([200, 302].includes(r.status()), 'download: ' + r.status()).toBeTruthy()
  const body = await r.text().catch(() => '')
  expect(body.length > 0, 'download body non-empty').toBeTruthy()
})

test('citekey: citation-key suggestions endpoint responds gracefully', async () => {
  const r = await a('GET', '/library/references/citation-key-suggestions?base=parity')
  expect(r.status(), 'suggestions status ' + r.status()).toBeLessThan(500)
})

test('role: tpladmin has the same personal library', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  await loginRobust(q, TPLADMIN.email, TPLADMIN.password)
  const r = await q.request.get(BASE + '/library/references')
  expect(r.status(), 'tpladmin list').toBe(200)
  const j = await r.json()
  expect(Array.isArray(j.items), 'items array').toBeTruthy()
  await ctx.close()
})

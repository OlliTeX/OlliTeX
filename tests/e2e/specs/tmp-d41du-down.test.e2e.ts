// D41-DU slice 1 — LIVE proof with document-updater STOPPED in the container.
// All API calls go through the PAGE's own fetch (exactly the app contract:
// JSON body, X-Csrf-Token from meta, browser cookie binding).
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { ADMIN } from '../fixtures/credentials'

type Resp = { status: number; text: string; loc: string }
const api = (
  page: import('@playwright/test').Page,
  method: string,
  path: string,
  data?: unknown
): Promise<Resp> => page.evaluate(async ([m, p, d]: [string, string, unknown]) => {
  const csrfEl = document.querySelector('meta[name="ol-csrfToken"]')
  const csrf = (csrfEl && (csrfEl as HTMLMetaElement).content) || ''
  const r = await fetch(p, {
    method: m,
    redirect: 'manual',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
      'X-Csrf-Token': csrf,
    },
    credentials: 'same-origin',
    body: d === undefined ? undefined : JSON.stringify(d),
  })
  const text = await r.text().catch(() => '')
  return { status: r.status, text, loc: r.headers.get('location') || '' }
}, [method, path, data])

test('D41-DU down: create/ranges/clone/delete', async ({ page }) => {
  await loginRobust(page, ADMIN.email, ADMIN.password)
  await page.goto('/project', { waitUntil: 'domcontentloaded' })
  const haveCsrf = await page.evaluate(async () => {
    for (let i = 0; i < 30; i++) {
      const el = document.querySelector('meta[name="ol-csrfToken"]')
      if (el) return true
      await new Promise(r => setTimeout(r, 500))
    }
    return false
  })
  if (!haveCsrf) throw new Error('hub has no ol-csrfToken meta')

  let r = await api(page, 'POST', '/project/new', { projectName: 'd41du-live-' + Date.now() })
  console.log('create →', r.status, r.loc.slice(0, 60), r.text.slice(0, 80))
  const m = (r.loc.match(/\/project\/([0-9a-f]{24})/i) || r.text.match(/"project_id"\s*:\s*"([0-9a-f]{24})"/) || r.text.match(/\/project\/([0-9a-f]{24})/i) || [])
  const pid = (m as any)[1] || ''
  if (r.status >= 400 || !pid) throw new Error('create failed: ' + r.status + ' ' + r.text.slice(0, 200))

  await page.goto('/project/' + pid, { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(500)
  let rr = await api(page, 'GET', '/project/' + pid + '/ranges')
  console.log('ranges →', rr.status, rr.text.slice(0, 120))
  expect(rr.status).toBe(200)
  let ranges: any[] = []
  try { ranges = JSON.parse(rr.text) } catch (e) { throw new Error('ranges not JSON: ' + rr.text.slice(0, 200)) }
  expect(Array.isArray(ranges) && ranges.length).toBe(1)
  // id = the DU docset id (the docstore document key — a pathname like
  // "main.tex" or an ObjectID hex, depending on how the docstore keys it);
  // the wire contract is [{id:<non-empty>, ranges:{...}}], not a specific id type.
  expect(typeof ranges[0].id).toBe('string')
  expect(ranges[0].id.length).toBeGreaterThan(0)
  console.log('RANGES (DU down) OK →', JSON.stringify(ranges).slice(0, 180))

  const cl = await api(page, 'POST', '/Project/' + pid + '/clone', { projectName: 'd41du-clone-' + Date.now() })
  console.log('clone →', cl.status, cl.loc.slice(0, 60), cl.text.slice(0, 100))
  let cloneId = ''
  const ids = cl.text.match(/([0-9a-f]{24})/g) || []
  for (const c of ids) if (c !== pid) { cloneId = c; break }
  if (cl.status >= 400) throw new Error('clone failed: ' + cl.status + ' ' + cl.text.slice(0, 200))
  console.log('clone id →', cloneId || '(not resolved)')

  for (const id of [pid, cloneId]) {
    if (!id) continue
    const d = await api(page, 'DELETE', '/Project/' + id, {})
    console.log('delete ' + id + ' →', d.status, d.text.slice(0, 80))
    expect(d.status, 'delete ' + id).toBeLessThan(400)
  }

  const g = await api(page, 'GET', '/project/' + pid)
  console.log('post-delete GET →', g.status)
  expect([404, 302]).toContain(g.status)
  console.log('D41DU_DOWN_ALL_OK')
})

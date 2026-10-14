/**
 * F10-OPTION-B REGRESSION (ag-v66) — the strongest dangling-presence case:
 * the collab room is STILL CONNECTED when the project is hard-deleted (no
 * decay-window timing at all). Pre-normalization (Node-faithful) the handler
 * 500'd; after owner-sanctioned normalization (admin.go, 9ce36150) the
 * surface must be 200 and the deleted fixture must be absent.
 */
import { test, expect } from '@playwright/test'
import { ADMIN } from '../fixtures/credentials'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { api } from '../parity/harness'

const BASE = process.env.OL_BASE || 'http://127.0.0.1:7420'
const st = (r: any) => (typeof r.status === 'function' ? r.status() : r.status)

test('active-projects 200 + fixture absent while a LIVE-presence room is deleted (Option B)', async ({ browser, page }) => {
  await loginRobust(page, ADMIN.email, ADMIN.password)
  const csrf = async () => (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''

  // 1) fixture project (hub UI flow — the canonical create path)
  const pid = await createBlankProject(page)
  expect(pid, 'fixture projectId returned').toBeTruthy()

  // 2) LIVE presence: open the editor and wait for the collab room to attach
  await page.goto(BASE + '/editor/' + pid, { waitUntil: 'domcontentloaded' }).catch(() => {})
  const ready = await page.waitForSelector('.cm-content', { timeout: 120_000 }).then(() => true).catch(() => false)
  expect(ready, 'editor ready (presence registered in the collab plane)').toBe(true)
  await page.waitForTimeout(1500) // flushroom debounce margin

  // 3) presence IS the live list right now (pre-deletion sanity: fixture listed)
  const before = (await api(page, 'GET', '/admin/active-projects')) as any
  expect(st(before), 'active-projects 200 pre-deletion (got ' + st(before) + ')').toBe(200)
  const beforeBody = await before.text().catch(() => '')
  const listedBefore = beforeBody.includes(pid)
  console.log('[F10] listedBefore=' + listedBefore + ' status=' + st(before))

  // 4) HARD DELETE while the room is STILL CONNECTED (admin product route)
  const tr = (await api(page, 'POST', '/admin/project/' + pid + '/trash', { body: '{}', headers: { 'X-CSRF-Token': await csrf() } })) as any
  console.log('[F10] trash=' + st(tr))
  const del = (await api(page, 'DELETE', '/admin/project/' + pid, { headers: { 'X-CSRF-Token': await csrf() } })) as any
  expect(st(del) < 300, 'admin delete accepted (got ' + st(del) + ')').toBe(true)
  console.log('[F10] delete=' + st(del) + ' (room still connected in this context)')

  // 5) THE CONTRACT: normalized edge — 200, and the dangling fixture ABSENT
  const after = (await api(page, 'GET', '/admin/active-projects')) as any
  const afterBody = await after.text().catch(() => '')
  expect(st(after), 'F10 OPTION-B — active-projects 200 while the deleted project\\u2019s presence is dangling (pre-normalization this deterministically 500d; got ' + st(after) + ')').toBe(200)
  expect(afterBody.includes(pid), 'F10 OPTION-B — deleted fixture must be ABSENT from the active list').toBe(false)
  console.log('[F10] ' + 'after: status=' + st(after) + ' fixtureAbsent=' + !afterBody.includes(pid) + ' — OPTION B VERIFIED')

  // 6) post-delete surface spot-check (cosmetic recording; contract is step 5)
  const mm = (await api(page, 'GET', '/admin/project/' + pid + '/members')) as any
  console.log('[F10] post-delete members-surface=' + st(mm) + ' (recorded)')
  await page.close()
})

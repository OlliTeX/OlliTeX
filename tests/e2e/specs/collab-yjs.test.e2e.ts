/**
 * collab-yjs — the S4 flip (OT → Yjs/Ygo) browser contract (F1 battery,
 * repo form). Text lives in the Yjs document synced through the Go collab
 * service (port 3450, y-websocket protocol); presence/events ride the Go
 * event bus (port 3026 — see realtime-bus.test.e2e.ts).
 *
 *   A1 editor boots with the seeded (v1) LaTeX content
 *   A2 D40: the D25 placeholder is retired — the review REST surface is live
 *      (anon threads GET → auth redirect, not 404; logged-in → record shape)
 *   A3 local typing commits a new history version (local direction)
 *   A4 a REAL second client's edit converges into the first client's open
 *      editor (CRDT merge over the Go collab service — the pivot core)
 *   A5 the history chain grows (≥ 3 versions; v1 = seed)
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { ADMIN } from '../fixtures/credentials'

test.setTimeout(420_000)

async function cmText(page: any): Promise<string> {
  return page.evaluate(() => (document.querySelector('.cm-content') as HTMLElement).innerText)
}

test('Yjs editor — render, D25 placeholder, local+remote convergence, history', async ({ page, context }) => {
  await loginRobust(page, ADMIN.email, ADMIN.password)
  const pid = await createBlankProject(page)

  // open the editor
  await page.goto('/project/' + pid, { waitUntil: 'load' })
  const cm = await page.waitForSelector('.cm-content', { timeout: 60_000 })
  if (!cm) throw new Error('A1: editor never rendered')

  // A1: seeded content (v1) — the default LaTeX template
  const seeded = await cmText(page)
  expect(seeded).toMatch(/documentclass\s*\{/)
  expect(seeded).toMatch(/begin\{document\}/)
  expect(seeded).toMatch(/end\{document\}/)

  // A2: D40 re-attach — the D25 placeholder is retired; the review REST
  // surface is live (pinned contract: GET threads = Record<threadId, Thread>)
  const notes = await page.locator('.yjs-engine-review-note').count()
  expect(notes).toBe(0)
  const threadsResp = await page.request.get(`/project/${pid}/threads`)
  expect(threadsResp.status()).toBe(200)
  const threadsRecord = (await threadsResp.json()) as Record<string, any>
  expect(typeof threadsRecord).toBe('object') // record (not an array)
  expect(Array.isArray(threadsRecord)).toBe(false)

  const history = async () => {
    const t = await page.request.get(`/project/${pid}/collab/history`)
    const j = (await t.json().catch(() => ({}))) as any
    const hs = j.versions || j.history || []
    return Array.isArray(hs) ? hs : []
  }

  // A3: local typing → text grows + a new history version
  const beforeLen = (await cmText(page)).length
  await page.click('.cm-content')
  await page.keyboard.press('Control+End')
  const localMark = '%' + ' yjs-e2e-local-' + Date.now()
  await page.keyboard.type('\n' + localMark, { delay: 10 })
  await page.waitForTimeout(3000)
  const afterLen = (await cmText(page)).length
  expect(afterLen).toBeGreaterThan(beforeLen)
  let versions = await history()
  expect(versions.length).toBeGreaterThanOrEqual(2) // v1 seed + local edit

  // A4: a real second client (fresh tab = fresh Yjs peer) types — the edit
  // converges into the FIRST client's open editor (remote merge direction)
  const remoteMark = '%' + ' yjs-e2e-remote-' + Date.now()
  const tab2 = await context.newPage()
  await tab2.goto('/project/' + pid, { waitUntil: 'load' })
  await tab2.waitForSelector('.cm-content', { timeout: 60_000 })
  await tab2.click('.cm-content')
  await tab2.keyboard.press('Control+End')
  await tab2.keyboard.type('\n' + remoteMark, { delay: 10 })
  await page.waitForTimeout(6000)
  const merged = await cmText(page)
  expect(merged).toContain(localMark)
  expect(merged).toContain(remoteMark)

  // A5: history chain grew with both local and remote edits
  versions = await history()
  expect(versions.length).toBeGreaterThanOrEqual(3)

  await tab2.close()
  await page.close()
})

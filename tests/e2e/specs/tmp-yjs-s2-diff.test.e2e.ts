/**
 * d5dd23dd S2 LIVE PROBE — Yjs-native diff surfaces (doc-diff + filetree-diff)
 * on a fresh, editor-seeded room (login/create via the proven helpers).
 *
 *   - /updates rows carry the typed version (S1.2)
 *   - /doc/:any-oid/diff?from=0&to=N -> {diff:[...]} with the typed marker,
 *     Node part shape {u|i|d, meta?}
 *   - /filetree/diff?from=0&to=N -> {diff:[{pathname:'mainbasic.tex',operation:'edited'}]}
 *   - zero-width range -> a single u part
 */
import { test, expect, type Page } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'

test.describe('d5dd23dd S2 live diff surfaces', () => {
  test('doc-diff + filetree-diff serve the Yjs plane', async ({ page }) => {
    test.setTimeout(240000)
    await loginRobust(page, 'e2e-user@e2e.test', 'Ol-Fixture-3m2Q')
    const pid = await createBlankProject(page)

    // open the editor — the Yjs engine seeds the room (v1) from mainbasic.tex
    await page.goto('/project/' + pid, { waitUntil: 'load' })
    const cmContent = await page.waitForSelector('.cm-content', { timeout: 60000 })
    if (!cmContent) throw new Error('editor never rendered')
    // wait for the SEED to actually be visible (A1 pattern) — typing before
    // the engine connects races the seed sync.
    const text = () => page.evaluate(() => (document.querySelector('.cm-content') as HTMLElement).innerText)
    for (let i = 0; i < 40 && !/documentclass/.test(await text()); i++) await page.waitForTimeout(500)
    expect(await text()).toMatch(/documentclass/)
    // and the room must already hold the seed (v>=1)
    const doc0 = await (await page.request.get(`/project/${pid}/collab/doc`)).json()
    expect(doc0.version).toBeGreaterThanOrEqual(1)
    await page.click('.cm-content')
    await page.waitForTimeout(800)
    await page.keyboard.press('Control+End')
    await page.keyboard.type(' %% yjs-s2-live-marker\n', { delay: 10 })
    // poll until the typed edit persists as a new room version
    const poll = async () => {
      const d = await (await page.request.get(`/project/${pid}/collab/doc`)).json()
      return d.version || 0
    }
    let last = 0
    for (let i = 0; i < 24; i++) {
      last = await poll()
      if (last >= 2) break
      await page.waitForTimeout(1000)
    }
    expect(last, 'room version after typing').toBeGreaterThanOrEqual(2)

    const j = async (path: string) => {
      const body = await page.evaluate(async (p: string) => {
        const res = await fetch(p, { headers: { accept: 'application/json' } })
        return { status: res.status, text: await res.text() }
      }, path)
      return body
    }

    // 1: /updates sees the seeded + typed versions (S1.2)
    const upd = await j(`/project/${pid}/updates`)
    expect(upd.status).toBe(200, 'updates status: ' + upd.text.slice(0, 300))
    const updates: any[] = JSON.parse(upd.text).updates
    expect(updates.length).toBeGreaterThanOrEqual(1)
    const toV = updates[0].toV
    expect(toV).toBeGreaterThanOrEqual(2)

    // 2: filetree diff over the whole history
    const ftd = await j(`/project/${pid}/filetree/diff?from=0&to=${toV}`)
    expect(ftd.status).toBe(200, 'filetree/diff status: ' + ftd.text.slice(0, 300))
    const files = JSON.parse(ftd.text).diff
    expect(files.length).toBe(1)
    expect(files[0].pathname).toBe('main.tex') // live rootFolder name of the basic template
    expect(files[0].operation).toBe('edited')

    // 3: doc diff — the typed marker lands in the range, Node part shape
    const dd = await j(`/project/${pid}/doc/${pid}/diff?from=0&to=${toV}`)
    expect(dd.status).toBe(200, 'doc/diff status: ' + dd.text.slice(0, 300))
    const parts = JSON.parse(dd.text).diff
    const rebuilt = parts.map(p => p.u || p.i || p.d || '').join('')
    expect(rebuilt).toContain('yjs-s2-live-marker')
    const ins = parts.find(p => p.i !== undefined)
    expect(ins).toBeTruthy()
    if (ins.meta) {
      expect(Array.isArray(ins.meta.users)).toBeTruthy()
      expect(typeof ins.meta.end_ts).toBe('number')
    }

    // 4: zero-width range -> a single u part (state after to-1)
    const zw = await j(`/project/${pid}/doc/${pid}/diff?from=${toV}&to=${toV}`)
    expect(zw.status).toBe(200)
    const zwParts = JSON.parse(zw.text).diff
    expect(zwParts.length).toBe(1)
    expect(zwParts[0].u).toBeTruthy()
  })
})

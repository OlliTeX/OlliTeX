/**
 * ORCID import — pinned contract (2026-10-15, owner report "selected 3 works,
 * only 1 imported", fixed in ag-70):
 *
 *  A) MULTI-WORK IMPORT (the reported bug, pinned green): the exact user path —
 *     hub library → Add reference → ORCID → search name → Browse works → check
 *     3 rows → "Import (3)" — must import ALL three and show the success toast.
 *     Leg A also pre-checks the server chain (works + fetch-bib) to isolate
 *     server from UI failures.
 *
 *  B) PARTIAL-FAILURE VISIBILITY (the pinned fix): with one of the per-work
 *     fetch-bib calls failing, the toast must NAME the failed work and its
 *     error and still import the other two (partial import = legacy behaviour,
 *     preserved). Pre-fix the loop swallowed every per-work error (catch {}) —
 *     the user's lost works were invisible, which is exactly why "only 1 was
 *     imported" went undiagnosable.
 *
 *  C) (covered by A's precheck) fetch-bib on DOI-bearing works completes under
 *     the relaxed chain budget (Go parity: Node was 10 s PER HOP; the port's
 *     10 s TOTAL cap on the 2-hop chain is now a 30 s chain budget with 10 s
 *     per-hop).
 *
 * Dependency: live orcid.org egress (a real orcid.org outage fails A/B; triage
 * like the other external battery dependencies). Cleanup: title-scoped delete
 * of exactly the entries created.
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { USER } from '../fixtures/credentials'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const ORCID = '0000-0003-3082-8995' // Carol Davis — 6 public works WITH DOIs
const SEARCH_NAME = 'carol davis'

async function apiGet(page: any, path: string) {
  return (page as any).request.get(`${BASE + path}`)
}

async function listByTitle(page: any, title: string): Promise<any[]> {
  const q = await apiGet(page, '/library/references?q=' + encodeURIComponent(title.slice(0, 40)))
  if (q.status() !== 200) return []
  const body = await q.json()
  return body.items || []
}

async function deleteIds(page: any, ids: string[]) {
  if (!ids.length) return
  const csrf = await page.locator('meta[name=ol-csrfToken]').getAttribute('content')
  const d = await (page as any).request.post(`${BASE}/library/references/delete`, {
    headers: { 'x-csrf-token': csrf || '', 'content-type': 'application/json' },
    data: JSON.stringify({ ids, permanent: true }),
  })
  expect(d.status(), 'cleanup delete').toBeLessThan(300)
}

async function openHubOrcidWorks(page: any): Promise<void> {
  await page.goto(`${BASE}/hub#/library`, { waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: /Add reference/i }).click()
  await page.getByText(/ORCID/i).first().click()
  const nameInput = page.getByLabel(/Researcher name/i)
  await nameInput.fill(SEARCH_NAME)
  await page.getByRole('button', { name: /^Search$/i }).first().click()
  // results: pick the row with our ORCID (exact orcid match when present)
  const row = page.locator('div:visible', { hasText: ORCID }).locator('button', { hasText: /Browse works/i }).first()
  await row.click({ timeout: 20000 })
  // works table
  await page.locator('table >> input[type=checkbox]').first().toBeVisible({ timeout: 20000 })
}

async function firstThreeWorkTitles(page: any): Promise<string[]> {
  const rows = page.locator('table tr').filter({ has: page.locator('input[type=checkbox]') })
  const n = Math.min(3, await rows.count())
  const titles: string[] = []
  for (let i = 0; i < n; i += 1) {
    const t = (await rows.nth(i).locator('td').nth(1).innerText()).split('\n')[0]
    titles.push(t)
  }
  return titles
}

test.describe('ORCID import (hub library)', () => {
  test('A: three selected works all import via the exact user path', async ({ page }) => {
    await loginRobust(page, USER.email, USER.password)

    // 0) server chain precheck (isolates server from UI)
    const worksRes = await apiGet(page, `/orcid-picker/works?orcid=${ORCID}`)
    expect(worksRes.status(), 'works endpoint').toBe(200)
    const works = ((await worksRes.json()) as any).works || []
    expect(works.length, 'at least 3 public works').toBeGreaterThanOrEqual(3)
    const bibRes = await apiGet(page, `/orcid-picker/fetch-bib?orcid=${ORCID}&putCode=${works[0].putCode}`)
    expect(bibRes.status(), 'fetch-bib endpoint (DOI work)').toBe(200)
    const bib = ((await bibRes.json()) as any).bibtex || ''
    expect(bib, 'bibtex non-empty and well-formed').toMatch(/^@/)

    // 1) exact user path: search → browse works → check 3 → import (3)
    await openHubOrcidWorks(page)
    const titles = await firstThreeWorkTitles(page)
    expect(titles, 'three works visible').toHaveLength(3)
    const rows = page.locator('table tr').filter({ has: page.locator('input[type=checkbox]') })
    for (let i = 0; i < 3; i += 1) {
      await rows.nth(i).locator('input[type=checkbox]').first().check({ timeout: 10000 })
    }
    await page.getByRole('button', { name: /Import \(3\)/i }).click()

    // 2) success toast: exactly 3 imported, no partial-failure text
    const toast = page.getByText(/Imported 3 references from ORCID\.?/).first()
    await expect(toast, 'success toast (3/3)').toBeVisible({ timeout: 90000 })

    // 3) all three present in the library (title-scoped reads)
    for (const t of titles) {
      const items = await listByTitle(page, t)
      expect(items.length, `library should contain "${t.slice(0, 40)}"`).toBeGreaterThanOrEqual(1)
    }

    // 4) cleanup (title-scoped)
    for (const t of titles) {
      const items = await listByTitle(page, t)
      await deleteIds(page, items.map((e: any) => e._id || e.id).filter(Boolean))
    }
  })

  test('B: a per-work failure is REPORTED and the rest still import (not silent)', async ({ page }) => {
    await loginRobust(page, USER.email, USER.password)
    await openHubOrcidWorks(page)
    const titles = await firstThreeWorkTitles(page)
    expect(titles, 'three works visible').toHaveLength(3)
    const rows = page.locator('table tr').filter({ has: page.locator('input[type=checkbox]') })
    for (let i = 0; i < 3; i += 1) {
      await rows.nth(i).locator('input[type=checkbox]').first().check({ timeout: 10000 })
    }

    // Abort exactly the 2nd fetch-bib call (the loop is serial; selection order
    // = row order). Everything else proceeds.
    let calls = 0
    const failTitle = titles[1]
    const failRe = new RegExp(failTitle.slice(0, 25).replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
    await page.route('**/orcid-picker/fetch-bib**', async (route) => {
      calls += 1
      if (calls === 2) return route.abort('failed')
      return route.continue()
    })

    await page.getByRole('button', { name: /Import \(3\)/i }).click()

    // The toast must (1) report the other two imported and (2) name the
    // failed work with an error — the pre-fix silent catch is now a failure.
    const deadline = Date.now() + 90000
    let ok = false
    let bodyText = ''
    while (Date.now() < deadline) {
      bodyText = await page.evaluate(() => document.body.innerText)
      if (/Imported 2 references from ORCID/.test(bodyText) && /could not be imported/.test(bodyText) && failRe.test(bodyText)) {
        ok = true
        break
      }
      await page.waitForTimeout(1500)
    }
    expect(
      ok,
      `partial import must be visible: two imported + the failed work named (got: ${bodyText.match(/Imported [^\n]+|No references[^\n]+/)?.[0] || 'no import toast'}) calls=${calls}`,
    ).toBe(true)

    // cleanup: the two imported works (the failed one never landed)
    for (const t of [titles[0], titles[2]]) {
      const items = await listByTitle(page, t)
      await deleteIds(page, items.map((e: any) => e._id || e.id).filter(Boolean))
    }
  })
})

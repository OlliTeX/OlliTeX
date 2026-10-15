/**
 * ORCID import — pinned contract (2026-10-15, owner report "selected 3 works,
 * only 1 imported", fixed in ag-70):
 *
 *  A) MULTI-WORK IMPORT (the reported bug, pinned green): the exact user path —
 *     hub library → Add reference → ORCID → search name → Browse works → check
 *     3 rows → "Import (3)" — must import ALL three and show the success toast.
 *  B) PARTIAL-FAILURE VISIBILITY (the pinned fix): with one of the per-work
 *     fetch-bib calls failing, the toast must NAME the failed work and its
 *     error and still import the other two (partial import = legacy behaviour,
 *     preserved). Pre-fix the loop swallowed every per-work error (catch {}) —
 *     the user's lost works were invisible, which is exactly why "only 1 was
 *     imported" went undiagnosable.
 *  C) BUDGET PARITY (Go): fetch-bib on a DOI-bearing work completes — the
 *     port's old 10 s TOTAL cap on the 2-hop chain (Node original: 10 s PER
 *     HOP) was relaxed to a 30 s chain budget with 10 s per-hop (orcidpicker.go
 *     fetchBibTotalCtx); a budget too tight for a healthy slow DOI hop fails
 *     this leg.
 *
 * Robustness notes: orcid.org search results are UNSTABLE between calls
 * (same name → different author sets). Server legs therefore retry the pinned
 * ORCID (0000-0003-3082-8995, Carol Davis — works WITH DOIs) until it returns;
 * UI legs follow "any author with ≥3 works from the current results" (the
 * user's own flow) and read titles dynamically for assertions + cleanup.
 * A real orcid.org outage fails A-C and is triaged like the other external
 * battery dependencies (Dropbox precedent).
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { USER } from '../fixtures/credentials'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const FIXED_ORCID = '0000-0003-3082-8995' // 6 public works WITH DOIs
const SEARCH_NAME = 'carol davis'

async function apiGet(page: any, path: string) {
  return (page as any).request.get(`${BASE + path}`)
}

async function serverChainPrecheck(page: any): Promise<void> {
  // works list for the pinned DOI-carrying ORCID (retry over result flakiness)
  let works: any[] = []
  let last = ''
  for (let i = 0; i < 6; i += 1) {
    const r = await apiGet(page, `/orcid-picker/works?orcid=${FIXED_ORCID}`)
    last = `${r.status()} ${(await r.text().catch(() => '')).slice(0, 120)}`
    if (r.status() === 200) {
      works = ((await r.json()) as any).works || []
      if (works.length >= 3) break
    }
    await page.waitForTimeout(3000)
  }
  expect(works.length, `works list for ${FIXED_ORCID} (last: ${last})`).toBeGreaterThanOrEqual(3)
  // fetch-bib on the first work (DOI work — the multi-hop path)
  let ok = false
  let lastBib = ''
  for (let i = 0; i < 6; i += 1) {
    const r = await apiGet(page, `/orcid-picker/fetch-bib?orcid=${FIXED_ORCID}&putCode=${works[0].putCode}`)
    lastBib = `${r.status()} ${(await r.text().catch(() => '')).slice(0, 120)}`
    if (r.status() === 200) {
      const bib = ((await r.json()) as any).bibtex || ''
      if (/^@/.test(bib)) {
        ok = true
        break
      }
    }
    await page.waitForTimeout(3000)
  }
  expect(ok, `fetch-bib (DOI work) must complete with valid bibtex (last: ${lastBib})`).toBe(true)
}

async function listByTitle(page: any, title: string): Promise<any[]> {
  const q = await apiGet(page, '/library/references?q=' + encodeURIComponent(title.slice(0, 40)))
  if (q.status() !== 200) return []
  const body = await q.json()
  return body.items || []
}

async function deleteIds(page: any, ids: string[]): Promise<void> {
  if (!ids.length) return
  const csrf = await page.locator('meta[name=ol-csrfToken]').getAttribute('content')
  const d = await (page as any).request.post(`${BASE}/library/references/delete`, {
    headers: { 'x-csrf-token': csrf || '', 'content-type': 'application/json' },
    data: JSON.stringify({ ids, permanent: true }),
  })
  expect(d.status(), 'cleanup delete').toBeLessThan(300)
}

/**
 * Open the hub ORCID modal and reach the WORKS table of ANY author from the
 * current search results who has >= 3 listed works (results are unstable —
 * some same-name authors have empty records; the user's own flow is
 * "search → pick an author with works → select rows").
 * Returns the first three work titles (for assertion + cleanup).
 */
async function openAnyOrcidWorksPage(page: any): Promise<string[]> {
  await page.goto(`${BASE}/hub#/library`, { waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: /Add reference/i }).first().click()
  await page.locator('.mantine-Menu-item, [data-mantine-menu-item]').filter({ hasText: /ORCID/i }).first().click()
  await expect(page.getByText('Import from ORCID.org').first(), 'ORCID modal open').toBeVisible({ timeout: 15000 })

  for (let round = 0; round < 6; round += 1) {
    // the modal resets the name query on every mode round-trip → re-fill each round
    const nameInput = page.getByLabel(/Researcher name/i)
    await nameInput.fill(SEARCH_NAME)
    await page.getByRole('button', { name: /^Search$/i }).first().click()
    await page.waitForTimeout(2500)
    const browseBtns = page.locator('button:has-text("Browse works")')
    const n = await browseBtns.count()
    if (n === 0) {
      const modalText = await page.evaluate(() => document.body.innerText.slice(0, 300))
      throw new Error(`no authors in results round ${round + 1}: ${modalText.replace(/\n+/g, ' | ')}`)
    }
    await browseBtns.nth(round).click()
    // works table with >= 3 checkbox rows?
    for (let wait = 0; wait < 10; wait += 1) {
      const cbs = await page.locator('table tr:has(input[type=checkbox])').count()
      if (cbs >= 3) {
        const rows = page.locator('table tr').filter({ has: page.locator('input[type=checkbox]') })
        const titles: string[] = []
        for (let i = 0; i < 3; i += 1) {
          const t = (await rows.nth(i).locator('td').nth(1).innerText()).split('\n')[0]
          titles.push(t)
        }
        return titles
      }
      await page.waitForTimeout(1200)
    }
    // empty author — back to search, next result next round
    const again = page.getByRole('button', { name: /Search again/i })
    if ((await again.count()) > 0) {
      await again.click()
    }
  }
  throw new Error('no ORCID author with >=3 works in 6 rounds (orcid.org result flakiness — re-run/triage)')
}

test.describe('ORCID import (hub library)', () => {
  test('A: three selected works all import via the exact user path', async ({ page }) => {
    await loginRobust(page, USER.email, USER.password)
    await serverChainPrecheck(page)

    const titles = await openAnyOrcidWorksPage(page)
    const rows = page.locator('table tr').filter({ has: page.locator('input[type=checkbox]') })
    for (let i = 0; i < 3; i += 1) {
      await rows.nth(i).locator('input[type=checkbox]').first().check({ timeout: 10000 })
    }
    await page.getByRole('button', { name: /Import \(3\)/i }).click()

    await expect(page.getByText(/Imported 3 references from ORCID/).first(), 'success toast (3/3)')
      .toBeVisible({ timeout: 90000 })

    for (const t of titles) {
      const items = await listByTitle(page, t)
      expect(items.length, `library should contain "${t.slice(0, 40)}"`).toBeGreaterThanOrEqual(1)
    }

    for (const t of titles) {
      const items = await listByTitle(page, t)
      await deleteIds(page, items.map((e: any) => e._id || e.id).filter(Boolean))
    }
  })

  test('B: a per-work failure is REPORTED and the rest still import (not silent)', async ({ page }) => {
    await loginRobust(page, USER.email, USER.password)
    const titles = await openAnyOrcidWorksPage(page)
    const rows = page.locator('table tr').filter({ has: page.locator('input[type=checkbox]') })
    for (let i = 0; i < 3; i += 1) {
      await rows.nth(i).locator('input[type=checkbox]').first().check({ timeout: 10000 })
    }

    // Abort exactly the 2nd fetch-bib call (serial loop; selection order = row order).
    let calls = 0
    const failTitle = titles[1]
    const failRe = new RegExp(failTitle.slice(0, 25).replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
    await page.route('**/orcid-picker/fetch-bib**', async (route) => {
      calls += 1
      if (calls === 2) return route.abort('failed')
      return route.continue()
    })

    await page.getByRole('button', { name: /Import \(3\)/i }).click()

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
      `partial import must be visible: two imported + the failed work named (toast: ${bodyText.match(/Imported [^\n]+|No references[^\n]+/)?.[0] || 'none'} calls=${calls})`,
    ).toBe(true)

    for (const t of [titles[0], titles[2]]) {
      const items = await listByTitle(page, t)
      await deleteIds(page, items.map((e: any) => e._id || e.id).filter(Boolean))
    }
  })
})

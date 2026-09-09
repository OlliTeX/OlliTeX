/**
 * Editor console canary (owner request, 2026-09-06: "obviously something
 * the e2e test too [missed]" — the prod #130 React crash went through the
 * whole suite because no spec listened for client-side errors).
 *
 * Contract: login -> new blank project -> editor fully loaded, with ZERO
 * pageerrors, ZERO console.error entries, and ZERO failed fatal network
 * requests. Any future client-side render crash (React #130 & friends)
 * fails this spec immediately.
 *
 * Editor renovation P1 (2026-09-09): the SAME journey is also asserted on
 * the /editor/:id URL (dual-run contract row `canary`, due P1) — the renovated
 * shell (Mantine provider + token bridge) must load with zero client errors.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }

test.describe.configure({ mode: 'serial' })

type CanarySink = {
  pageErrors: string[]
  consoleErrors: string[]
  failedRequests: string[]
}

function attachCanary(page: import('playwright').Page, sink: CanarySink) {
  page.on('pageerror', (e) => sink.pageErrors.push(String(e).slice(0, 400)))
  page.on('console', (m) => { if (m.type() === 'error') sink.consoleErrors.push(m.text().slice(0, 400)) })
  page.on('requestfailed', (r) => {
    const entry = `${r.method()} ${r.url().slice(0, 120)} :: ${r.failure()?.errorText ?? ''}`
    // Known-harmless: the IDE issues POST /project/:id/flush as fire-and-forget
    // (history flush; server logs 200 + the IDE aborts its own copy — nginx
    // 499/ERR_ABORTED pairs are expected, verified 2026-09-06 against the
    // access log and HistoryController.proxyToHistoryApi). Everything else is
    // a real failure.
    if (/\/flush :: net::ERR_ABORTED$/.test(entry)) return
    sink.failedRequests.push(entry)
  })
}

async function canaryJourney(
  page: import('playwright').Page,
  projectId: string,
  route: '/project' | '/editor'
) {
  await page.goto(`${BASE}${route}/${projectId}`, { waitUntil: 'load' })
  await expect(page.locator(".cm-editor").first()).toBeVisible({ timeout: 90_000 })
  // give the IDE a beat to settle socket joins (owner crash happened post-join)
  await page.waitForTimeout(4000)
}

function assertClean(label: string, sink: CanarySink) {
  expect(sink.failedRequests, `${label} failed requests:\n` + sink.failedRequests.join('\n')).toEqual([])
  expect(sink.pageErrors, `${label} pageerrors:\n` + sink.pageErrors.join('\n')).toEqual([])
  // console.error: allow harmless dev noise, nothing with React/bundle crashes
  const fatal = sink.consoleErrors.filter((t) => /React error|#130|Minified React|Uncaught|is not a valid child|array|undefined is not/i.test(t))
  expect(fatal, `${label} fatal console errors:\n` + fatal.join('\n')).toEqual([])
}

test('editor loads with zero client-side errors', async ({ page }, testInfo) => {
  page.setDefaultTimeout(45_000)
  const sink: CanarySink = { pageErrors: [], consoleErrors: [], failedRequests: [] }
  attachCanary(page, sink)

  await loginRobust(page, ADMIN.email, ADMIN.password)
  const projectId = await createBlankProject(page)
  await canaryJourney(page, projectId, '/project')
  assertClean('legacy /Project', sink)

  if (process.env.DUMP_CANARY === '1')
    console.log('CANARY LOGS legacy', JSON.stringify(sink))
})

test('editor loads with zero client-side errors on /editor (renovated shell)', async ({ page }) => {
  page.setDefaultTimeout(45_000)
  const sink: CanarySink = { pageErrors: [], consoleErrors: [], failedRequests: [] }
  attachCanary(page, sink)

  await loginRobust(page, ADMIN.email, ADMIN.password)
  const projectId = await createBlankProject(page)
  await canaryJourney(page, projectId, '/editor')
  assertClean('/editor', sink)

  if (process.env.DUMP_CANARY === '1')
    console.log('CANARY LOGS editor', JSON.stringify(sink))
})

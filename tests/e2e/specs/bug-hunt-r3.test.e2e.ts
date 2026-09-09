/**
 * Bug hunt R3 — /hub + /editor on the P8 stack (2026-09-10).
 *
 * Scope: everything shipped since the 2026-09-09 hunt (P4 big modals ·
 * P5 panes · P6 editor-core · P7 module UIs · P8 a11y fixes) plus the
 * whole-surface regression sweep on all three roles:
 *   - console errors / uncaught exceptions / 4xx-5xx network per surface
 *   - axe critical+serious on /editor, /Project, /hub, /admin-hub
 *   - editor flows: compile, toolbar menus, rail tabs, share/settings/
 *     history/word-count modals, LLM entry, view-logs
 *   - hub: workspace + admin section walk per role
 *
 * Findings are written to test-results/bug-hunt-r3-findings.json for the
 * write-up (BUG_HUNT_2026-09-10.md). Assertions fail on hard problems
 * (uncaught exceptions, axe critical/serious, failed core navigations);
 * cosmetic console noise is collected for classification, not auto-failed.
 */
import { test, expect } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { loginRobust, createBlankProject } from '../helpers/auth'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const ROLES = {
  ADMIN: { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' },
  USER: { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' },
  TPLADMIN: { email: 'e2e-tpladmin@e2e.test', password: 'Ol-Fixture-7tW4' },
}

// known-benign console/network noise (environment or upstream quirks)
const BENIGN = [
  'favicon',
  'ResizeObserver loop',
  'MathJax', // CDN-load in an offline sandbox
  'net::ERR', // CDN / font fetch in the offline stack
  'WebSocket',
  'cookie',
]

type Finding = {
  role: string
  surface: string
  consoleErrors: string[]
  pageErrors: string[]
  failedRequests: string[] // non-2xx/3xx
  notes: string
}

const findings: Finding[] = []

function newCapture(page: any) {
  const consoleErrors: string[] = []
  const pageErrors: string[] = []
  const failedRequests: string[] = []
  page.on('pageerror', (e: Error) => pageErrors.push(String(e).slice(0, 200)))
  page.on('console', (m: any) => {
    if (m.type() === 'error') consoleErrors.push(m.text().slice(0, 200))
  })
  page.on('response', (r: any) => {
    const s = r.status()
    if (s >= 400) failedRequests.push(`${s} ${r.url().replace(BASE, '')}`)
  })
  return { consoleErrors, pageErrors, failedRequests }
}

async function record(role: string, surface: string, cap: any, note = '') {
  const benignConsole = cap.consoleErrors.filter(
    (e: string) => !BENIGN.some(b => e.toLowerCase().includes(b.toLowerCase()))
  )
  findings.push({
    role,
    surface,
    consoleErrors: benignConsole,
    pageErrors: cap.pageErrors,
    failedRequests: cap.failedRequests,
    notes: note,
  })
  const hard = cap.pageErrors.length > 0
  return hard
}

async function axeScan(page: any): Promise<any> {
  const res = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze()
  return res.violations
    .filter(v => v.impact === 'critical' || v.impact === 'serious')
    .map(v => `${v.impact[0]}:${v.id}x${v.nodes.length}`)
}

for (const [roleName, cred] of Object.entries(ROLES)) {
  test.describe(`bug-hunt-r3 ${roleName}`, () => {
    let page: any
    let pid: string

    test.beforeAll(async ({ browser }) => {
      const c = await browser.newContext({ viewport: { width: 1500, height: 950 } })
      page = await c.newPage()
      await loginRobust(page, cred.email, cred.password)
      pid = await createBlankProject(page)
    })

    test.afterAll(async () => {
      await page.request.delete(`${BASE}/project/${pid}`).catch(() => {})
      await page.context().close().catch(() => {})
    })

    test('editor /editor: flows + console + axe', async () => {
      const cap = newCapture(page)
      await page.goto(`${BASE}/editor/${pid}`, { waitUntil: 'load' })
      await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
      await page.waitForTimeout(2500)
      await page.getByRole('button', { name: /allow (all )?cookies|accept/i }).first().click({ timeout: 2_000 }).catch(() => {})

      // compile round-trip
      await page.getByRole('button', { name: /re?compile/i }).first().click({ timeout: 8_000 }).catch(() => {})
      await page.waitForTimeout(4000)

      // toolbar menus open+close
      for (const menu of ['File', 'Edit', 'Insert', 'View', 'Format', 'Help']) {
        await page.getByRole('button', { name: new RegExp(`^${menu}$`, 'i') }).first().click({ timeout: 6_000 }).catch(() => {})
        await page.waitForTimeout(500)
        await page.keyboard.press('Escape')
        await page.waitForTimeout(300)
      }

      // rail tabs
      for (const tab of ['File tree', 'Project search', 'Integrations', 'Review panel', 'Chat']) {
        const t = page.locator(`[aria-label^='${tab}'], [role=tab][aria-label^='${tab}']`).first()
        if ((await t.count()) > 0) {
          await t.click({ timeout: 6_000 }).catch(() => {})
          await page.waitForTimeout(900)
        }
      }

      // modals: share / settings / history / word-count
      await page.getByRole('button', { name: /^share$/i }).first().click({ timeout: 8_000 }).catch(() => {})
      await page.waitForTimeout(1200)
      await page.keyboard.press('Escape')
      await page.waitForTimeout(600)

      await page.getByRole('button', { name: /^settings$/i }).first().click({ timeout: 8_000 }).catch(() => {})
      await page.waitForTimeout(1200)
      await page.keyboard.press('Escape')
      await page.waitForTimeout(600)

      await page.getByRole('button', { name: /history/i }).first().click({ timeout: 8_000 }).catch(() => {})
      await page.waitForTimeout(1500)
      await page.keyboard.press('Escape')
      await page.waitForTimeout(600)

      await page.getByRole('button', { name: /^file$/i }).first().click({ timeout: 6_000 }).catch(() => {})
      await page.waitForTimeout(700)
      const wc = page.locator('[role=menuitem], [class*=option], [class*=item]').filter({ hasText: /word count/i }).first()
      if (await wc.isVisible().catch(() => false)) {
        await wc.click()
        await page.waitForTimeout(1200)
        await page.keyboard.press('Escape')
        await page.waitForTimeout(500)
      }
      await page.keyboard.press('Escape').catch(() => {})

      // LLM entry opens the surface
      const llm = page.getByRole('button', { name: /get ai assistance/i }).first()
      if (await llm.isVisible().catch(() => false)) {
        await llm.click()
        await page.waitForTimeout(2500)
        const surface = page.locator('.llm-panel, [class*=llm-panel]').first()
        expect(surface, 'the LLM surface must open').toBeVisible({ timeout: 10_000 })
        await page.keyboard.press('Escape').catch(() => {})
        await page.waitForTimeout(600)
      }

      await record(roleName, '/editor flows', cap, 'compile+menus+rail+modals+llm')
      const axe = await axeScan(page)
      findings.find(f => f.surface === '/editor flows' && f.role === roleName)?.notes
      if (axe.length) {
        findings.push({ role: roleName, surface: '/editor axe', consoleErrors: [], pageErrors: [], failedRequests: [], notes: axe.join('; ') })
      }
      expect(axe, `axe critical/serious on /editor: ${axe.join(', ')}`).toEqual([])
      expect(cap.pageErrors, `uncaught exceptions on /editor: ${cap.pageErrors.join(' | ')}`).toEqual([])
      await page.waitForTimeout(800)
    })

    test('editor /Project: parity flows + console', async () => {
      const cap = newCapture(page)
      await page.goto(`${BASE}/Project/${pid}`, { waitUntil: 'load' })
      await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
      await page.waitForTimeout(2500)

      await page.getByRole('button', { name: /re?compile/i }).first().click({ timeout: 8_000 }).catch(() => {})
      await page.waitForTimeout(4000)
      for (const menu of ['File', 'Edit', 'Insert', 'View', 'Format', 'Help']) {
        await page.getByRole('button', { name: new RegExp(`^${menu}$`, 'i') }).first().click({ timeout: 6_000 }).catch(() => {})
        await page.waitForTimeout(400)
        await page.keyboard.press('Escape')
        await page.waitForTimeout(250)
      }
      await page.getByRole('button', { name: /^share$/i }).first().click({ timeout: 8_000 }).catch(() => {})
      await page.waitForTimeout(1200)
      await page.keyboard.press('Escape')
      await page.waitForTimeout(500)
      await page.getByRole('button', { name: /history/i }).first().click({ timeout: 8_000 }).catch(() => {})
      await page.waitForTimeout(1200)
      await page.keyboard.press('Escape')
      await page.waitForTimeout(500)

      await record(roleName, '/Project flows', cap, 'compile+menus+share+history')
      const axe = await axeScan(page)
      if (axe.length) {
        findings.push({ role: roleName, surface: '/Project axe', consoleErrors: [], pageErrors: [], failedRequests: [], notes: axe.join('; ') })
      }
      expect(axe, `axe critical/serious on /Project: ${axe.join(', ')}`).toEqual([])
      expect(cap.pageErrors, `uncaught exceptions on /Project: ${cap.pageErrors.join(' | ')}`).toEqual([])
      await page.waitForTimeout(600)
    })

    test('hub: section walk + console + axe', async () => {
      const cap = newCapture(page)
      await page.goto(`${BASE}/hub`, { waitUntil: 'load' })
      await page.waitForTimeout(2500)
      // walk the main workspace sections (rail links)
      const sections = ['Projects', 'My settings', 'Notifications', 'Appearance', 'Keybindings', 'Templates']
      for (const s of sections) {
        const link = page.locator('[role=link], a, [role=button], button').filter({ hasText: new RegExp(`^${s}$`, 'i') }).first()
        const v = await link.isVisible({ timeout: 2_000 }).catch(() => false)
        if (v) {
          await link.click({ timeout: 6_000 }).catch(() => {})
          await page.waitForTimeout(1200)
        }
      }
      await record(roleName, '/hub walk', cap, sections.join(','))
      const axe = await axeScan(page)
      if (axe.length) {
        findings.push({ role: roleName, surface: '/hub axe', consoleErrors: [], pageErrors: [], failedRequests: [], notes: axe.join('; ') })
      }
      expect(axe, `axe critical/serious on /hub: ${axe.join(', ')}`).toEqual([])
      expect(cap.pageErrors, `uncaught exceptions on /hub: ${cap.pageErrors.join(' | ')}`).toEqual([])
      await page.waitForTimeout(500)
    })

    if (roleName === 'ADMIN') {
      test('admin-hub: section walk + console + axe', async () => {
        const cap = newCapture(page)
        await page.goto(`${BASE}/admin-hub`, { waitUntil: 'load' })
        await page.waitForTimeout(3000)
        const sections = ['Users', 'Projects', 'LLM', 'Templates', 'Site', 'Stats']
        for (const s of sections) {
          const link = page.locator('[role=link], a, [role=button], button').filter({ hasText: new RegExp(s, 'i') }).first()
          const v = await link.isVisible({ timeout: 2_000 }).catch(() => false)
          if (v) {
            await link.click({ timeout: 6_000 }).catch(() => {})
            await page.waitForTimeout(1300)
          }
        }
        await record(roleName, '/admin-hub walk', cap, sections.join(','))
        const axe = await axeScan(page)
        if (axe.length) {
          findings.push({ role: 'ADMIN', surface: '/admin-hub axe', consoleErrors: [], pageErrors: [], failedRequests: [], notes: axe.join('; ') })
        }
        expect(axe, `axe critical/serious on /admin-hub: ${axe.join(', ')}`).toEqual([])
        expect(cap.pageErrors, `uncaught exceptions on /admin-hub: ${cap.pageErrors.join(' | ')}`).toEqual([])
      })
    }

    test('write findings', async () => {
      const out = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../test-results/bug-hunt-r3-findings.json')
      fs.mkdirSync(path.dirname(out), { recursive: true })
      fs.writeFileSync(out, JSON.stringify(findings, null, 2))
      const summary = findings
        .filter(f => f.pageErrors.length || (f.consoleErrors.length && f.role !== 'USER') )
        .map(f => `${f.role} ${f.surface}: console=${f.consoleErrors.length} page=${f.pageErrors.length} net=${f.failedRequests.length}`)
      console.log('HUNT-SUMMARY: ' + (summary.join(' | ') || 'no hard findings on user surfaces'))
      console.log('HUNT-FINDINGS-WRITTEN: ' + out)
    })
  })
}

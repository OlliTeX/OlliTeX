/**
 * Owner bug hunt (2026-09-09): /hub + /editor, every role, console + errors +
 * axe + broken flows. FINDINGS-ORIENTED: each step reports instead of hard
 * failing, so one hunt run covers everything. Output: test-results/bughunt.json
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { api, killProject } from '../parity/harness'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const find: { area: string; sev: 'high' | 'med' | 'low'; what: string; evidence: string }[] = []
const pageErrors: string[] = []
const consoleErrors: string[] = []
const badReqs: string[] = []

function attach(page: import('playwright').Page) {
  page.on('pageerror', e => pageErrors.push(`[${page.url().slice(0, 60)}] ${String(e).slice(0, 200)}`))
  page.on('console', m => {
    if (m.type() === 'error') consoleErrors.push(`[${page.url().slice(0, 50)}] ${m.text().slice(0, 200)}`)
  })
  page.on('response', r => {
    if (r.status() >= 500) badReqs.push(`[${page.url().slice(0, 50)}] ${r.status()} ${r.url().slice(0, 90)}`)
  })
}

async function axe(page: import('playwright').Page): Promise<number> {
  await page.addScriptTag({
    url: 'https://cdnjs.cloudflare.com/ajax/libs/axe-core/4.10.2/axe.min.js',
  }).catch(() => {})
  const res = await page
    .evaluate(async () => {
      const axe = (window as any).axe
      if (!axe) return { critical: 0 }
      const r = await axe.run(document, {
        resultTypes: ['violations'],
        runOnly: { type: 'tag', values: ['wcag2a', 'wcag21a', 'wcag2aa', 'wcag21aa', 'best-practice'] },
      })
      return {
        critical: r.violations
          .filter((v: any) => v.impact === 'critical' || v.impact === 'serious')
          .reduce((n: number, v: any) => n + v.nodes.length, 0),
        ids: r.violations
          .filter((v: any) => v.impact === 'critical' || v.impact === 'serious')
          .map((v: any) => `${v.id}×${v.nodes.length}`)
          .slice(0, 6)
          .join(','),
      }
    })
    .catch(() => ({ critical: 0, ids: 'axe-unavailable' }))
  return res.critical || 0
}

test.describe('BU HUNT — /hub', () => {
  test('admin + user full walk with error capture', { timeout: 1200_000 }, async ({ browser }) => {
    // ---------- ADMIN ----------
    const ca = await browser.newContext({ viewport: { width: 1500, height: 950 } })
    const a = await ca.newPage()
    attach(a)
    await loginRobust(a, 'e2e-admin@e2e.test', 'Ol-Fixture-9x7K')

    const adminSections = [
      '/hub#/projects',
      '/hub#/templates',
      '/hub#/llm',
      '/hub#/admin/users',
      '/hub#/admin/projects',
      '/hub#/admin/site.general.index',
      '/hub#/admin/site.general.health',
      '/hub#/admin/site.general.stats',
      '/hub#/admin/site.general.messages',
      '/hub#/admin/llm',
    ]
    for (const sec of adminSections) {
      await a.goto(`${BASE}${sec}`, { waitUntil: 'load' })
      await a.waitForTimeout(3500)
      const dead = await a
        .locator('.empty-state, [class*=error-page], [class*=fatal]')
        .first()
        .isVisible()
        .catch(() => false)
      if (dead) find.push({ area: 'hub-admin', sev: 'high', what: `section renders empty/error state: ${sec}`, evidence: '' })
      const c = await axe(a)
      if (c > 0) find.push({ area: 'hub-admin-a11y', sev: 'med', what: `axe serious/critical ×${c}: ${sec}`, evidence: '' })
    }
    // dark theme walk
    await a.goto(`${BASE}/hub#/admin/site.general.index`, { waitUntil: 'load' })
    await a.waitForTimeout(2500)
    await a.getByRole('button', { name: /dark theme/i }).first().click().catch(() => {})
    await a.waitForTimeout(1200)
    const darkBody = await a.evaluate(() => getComputedStyle(document.body).backgroundColor)
    if (darkBody === 'rgba(0, 0, 0, 0)' || darkBody === 'rgb(255, 255, 255)')
      find.push({ area: 'hub-themes', sev: 'med', what: 'dark theme: body background did not darken', evidence: darkBody })
    await a.goto(`${BASE}/hub#/admin/site.general.index`, { waitUntil: 'load' })
    await a.waitForTimeout(2500)
    const cd = await axe(a)
    if (cd > 0) find.push({ area: 'hub-admin-a11y-dark', sev: 'med', what: `axe dark ×${cd}: site.general.index`, evidence: '' })
    // back to light
    await a.getByRole('button', { name: /light theme/i }).first().click().catch(() => {})
    await a.waitForTimeout(500)

    // ---------- USER ----------
    const cu = await browser.newContext({ viewport: { width: 1500, height: 950 } })
    const u = await cu.newPage()
    attach(u)
    await loginRobust(u, 'e2e-user@e2e.test', 'Ol-Fixture-3m2Q')
    const userSections = [
      '/hub#/projects',
      '/hub#/templates',
      '/hub#/library',
      '/hub#/settings',
      '/hub#/notifications',
      '/hub#/keybindings',
      '/hub#/llm',
      '/hub#/sessions',
    ]
    for (const sec of userSections) {
      await u.goto(`${BASE}${sec}`, { waitUntil: 'load' })
      await u.waitForTimeout(3200)
      const dead = await u
        .locator('.empty-state, [class*=error-page], [class*=fatal]')
        .first()
        .isVisible()
        .catch(() => false)
      // empty-state is LEGIT on library/sessions for a fresh user — only flag error/fatal
      const fatal = await u
        .locator('[class*=error-state], [class*=fatal], [class*=crash]')
        .first()
        .isVisible()
        .catch(() => false)
      if (fatal) find.push({ area: 'hub-user', sev: 'high', what: `section error/fatal state: ${sec}`, evidence: '' })
      if (dead && false) void 0
      const c = await axe(u)
      if (c > 0) find.push({ area: 'hub-user-a11y', sev: 'med', what: `axe ×${c}: ${sec}`, evidence: '' })
      // untranslated i18n keys
      const untranslated = await u
        .evaluate(() => Array.from(document.body.querySelectorAll('*'))
          .map(n => n.textContent && n.textContent.trim())
          .filter(t => t && /^t\.[a-z]/.test(t) && t.length < 40)
          .slice(0, 3))
      if (untranslated.length) find.push({ area: 'hub-i18n', sev: 'low', what: `untranslated keys: ${sec}`, evidence: untranslated.join('|') })
    }
    await ca.close()
    await cu.close()
  })
})

test.describe('BU HUNT — /editor + /Project', () => {
  test('full chrome walk both routes', { timeout: 1500_000 }, async ({ browser }) => {
    const c = await browser.newContext({ viewport: { width: 1500, height: 950 } })
    const p = await c.newPage()
    attach(p)
    await loginRobust(p, 'e2e-admin@e2e.test', 'Ol-Fixture-9x7K')
    const pid = await createBlankProject(p)

    for (const route of ['/project', '/editor']) {
      const area = `editor-${route}`
      await p.goto(`${BASE}${route}/${pid}`, { waitUntil: 'load' })
      await p.locator('.cm-editor').first().waitFor({ timeout: 90_000 })
      await p.waitForTimeout(2500)

      // 1. every top-level menu opens + has items
      for (const menu of ['File', 'Edit', 'Insert', 'View', 'Help']) {
        await p.getByRole('button', { name: menu, exact: true }).first().click({ timeout: 5000 }).catch(() => {
          find.push({ area, sev: 'high', what: `menu does not open: ${menu}`, evidence: '' })
          return
        })
        await p.waitForTimeout(450)
        const items = await p.locator('[class*=menu] li, [role=menu] li, [class*=dropdown-menu] *').count().catch(() => 0)
        if (items === 0) find.push({ area, sev: 'med', what: `menu opened but shows no items: ${menu}`, evidence: '' })
        await p.keyboard.press('Escape')
        await p.waitForTimeout(250)
      }
      // 2. compile round trip
      await p.locator('.cm-editor').first().click()
      await p.keyboard.type(' % bug-hunt probe')
      await p.getByRole('button', { name: /re?compile/i }).first().click({ timeout: 10_000 }).catch(() =>
        find.push({ area, sev: 'high', what: 'compile button missing', evidence: '' })
      )
      await p.waitForTimeout(12_000)
      const pdf = await p.locator('iframe[src*=pdf], [class*=pdf] iframe, embed, object[type=application/pdf]').first().isVisible().catch(() => false)
      if (!pdf) find.push({ area, sev: 'med', what: 'no PDF pane after compile', evidence: '' })
      // 3. file tree ops: rename main.tex via the tree
      await p.getByRole('button', { name: 'File tree' }).first().click({ timeout: 8000 }).catch(() =>
        find.push({ area, sev: 'high', what: 'file tree rail entry missing', evidence: '' })
      )
      await p.waitForTimeout(1200)
      // 4. history
      await p.getByRole('button', { name: /history/i }).first().click({ timeout: 8000 }).catch(() =>
        find.push({ area, sev: 'med', what: 'history button missing', evidence: '' })
      )
      await p.waitForTimeout(1500)
      const hist = await p.locator('[class*=history]').count().catch(() => 0)
      await p.keyboard.press('Escape').catch(() => {})
      await p.waitForTimeout(500)
      void hist
      // 5. a11y
      const cAxe = await axe(p)
      if (cAxe > 0) find.push({ area: `${area}-a11y`, sev: 'low', what: `axe serious ×${cAxe}`, evidence: '' })
    }

    // console/page/500 rollup (dedupe, drop noise)
    const noise = /favicon|404.*\/project\/|Download the React DevTools/i
    const pe = [...new Set(pageErrors)].filter(e => !noise.test(e))
    const ce = [...new Set(consoleErrors)].filter(e => !noise.test(e) && !e.includes('useLayoutEffect') && !e.includes('ResizeObserver'))
    const br = [...new Set(badReqs)]
    find.push(
      { area: 'editor-console', sev: pe.length ? 'high' : 'low', what: `pageerrors: ${pe.length ? pe.slice(0, 6).join(' ¶ ') : 'none'}`, evidence: '' },
      { area: 'editor-console', sev: ce.length ? 'med' : 'low', what: `console.error: ${ce.length ? ce.slice(0, 8).join(' ¶ ') : 'none'}`, evidence: '' },
      { area: 'editor-http', sev: br.length ? 'high' : 'low', what: `HTTP 5xx: ${br.length ? br.slice(0, 6).join(' ¶ ') : 'none'}`, evidence: '' }
    )

    await killProject(p, pid).catch(() => {})
    await c.close()
  })
})

test('dump findings', async () => {
  const fs = await import('node:fs')
  const out = JSON.stringify({ at: new Date().toISOString(), findings: find, count: find.length }, null, 2)
  fs.writeFileSync('test-results/bug-hunt.json', out)
  console.log('BU-FINDINGS:\n' + find.map(f => `[${f.sev}] ${f.area} — ${f.what}${f.evidence ? ' :: ' + f.evidence : ''}`).join('\n'))
})

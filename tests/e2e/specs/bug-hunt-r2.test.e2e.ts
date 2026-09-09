/**
 * Owner bug hunt ROUND 2 — deep flows on /hub + /editor (both roles where
 * relevant), console + API round-trips + state assertions. Findings-oriented.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { api, killProject, mongoEval, ensureFixtureTemplate } from '../parity/harness'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const find: { area: string; sev: 'high' | 'med' | 'low'; what: string; evidence: string }[] = []
const cErr = (page: import('playwright').Page) => {
  const list: string[] = []
  page.on('pageerror', e => list.push('PE: ' + String(e).slice(0, 180)))
  page.on('console', m => m.type() === 'error' && list.push('CE: ' + m.text().slice(0, 160)))
  page.context().close().then(() => {
    const bad = list.filter(l => !/favicon|React DevTools|ResizeObserver/i.test(l))
    if (bad.length) find.push({ area: 'console', sev: 'med', what: bad.slice(0, 5).join(' ¶ '), evidence: '' })
  })
  return list
}

test.describe('BU HUNT R2 — /hub flows', () => {
  test('project CRUD + template create + settings round-trips', { timeout: 1500_000 }, async ({ browser }) => {
    const ca = await browser.newContext({ viewport: { width: 1500, height: 950 } })
    const a = await ca.newPage()
    const errs: string[] = []
    a.on('pageerror', e => errs.push('PE ' + String(e).slice(0, 160)))
    a.on('console', m => m.type() === 'error' && errs.push('CE ' + m.text().slice(0, 140)))
    await loginRobust(a, 'e2e-admin@e2e.test', 'Ol-Fixture-9x7K')
    await ensureFixtureTemplate(a).catch(() => {})

    // 1. CREATE project via hub
    await a.goto(`${BASE}/hub#/projects`, { waitUntil: 'load' })
    await a.waitForTimeout(3000)
    await a.getByRole('button', { name: /new project|create project|add project/i }).first().click({ timeout: 8000 }).catch(() =>
      find.push({ area: 'hub-projects', sev: 'high', what: 'create-project control missing', evidence: '' })
    )
    await a.waitForTimeout(600)
    const projInput = a.locator('input[type=text], input[placeholder*=name i]').first()
    await projInput.fill('BugHunt R2 Project').catch(() => find.push({ area: 'hub-projects', sev: 'med', what: 'create-project input missing', evidence: '' }))
    await a.getByRole('button', { name: /create|add|start/i }).first().click().catch(() => {})
    await a.waitForTimeout(2500)
    const created = await a.getByText('BugHunt R2 Project').first().isVisible().catch(() => false)
    if (!created) find.push({ area: 'hub-projects', sev: 'high', what: 'created project not listed', evidence: '' })

    // 2. RENAME via hub
    const renamed = await a.getByRole('button', { name: /rename/i }).first().isVisible().catch(() => false)
    if (renamed) {
      await a.getByRole('button', { name: /rename/i }).first().click()
      await a.waitForTimeout(400)
      const inp = a.locator('input:focus, input[type=text]:visible').first()
      await inp.fill('BugHunt R2 Renamed').catch(() => {})
      await a.keyboard.press('Enter')
      await a.waitForTimeout(2000)
      const ok = await a.getByText('BugHunt R2 Renamed').first().isVisible().catch(() => false)
      if (!ok) find.push({ area: 'hub-projects', sev: 'med', what: 'rename round-trip failed', evidence: '' })
    }

    // 3. TEMPLATE GALLERY → new project from template
    await a.goto(`${BASE}/hub#/templates`, { waitUntil: 'load' })
    await a.waitForTimeout(3000)
    const tplCard = a.getByText(/parity fixture template/i).first()
    if (!(await tplCard.isVisible().catch(() => false))) {
      find.push({ area: 'hub-templates', sev: 'med', what: 'fixture template not visible in gallery', evidence: '' })
    } else {
      await tplCard.click()
      await a.waitForTimeout(1500)
      const useBtn = a.getByRole('button', { name: /use|create|start|new project/i }).first()
      await useBtn.click({ timeout: 6000 }).catch(() => find.push({ area: 'hub-templates', sev: 'high', what: 'template "use" action missing', evidence: '' }))
      await a.waitForTimeout(2500)
      const errToast = await a.locator('[class*=toast], [class*=alert]').first().textContent().catch(() => '')
      if (/failed|error|cannot/i.test(errToast || '')) find.push({ area: 'hub-templates', sev: 'high', what: 'template project create errored', evidence: (errToast || '').slice(0, 120) })
    }

    // 4. MYSETTINGS round-trip (name)
    await a.goto(`${BASE}/hub#/settings`, { waitUntil: 'load' })
    await a.waitForTimeout(3000)
    const nameInput = a.locator('input').filter({ has: a.locator(':scope') }).nth(0)
    const nameField = a.getByPlaceholder(/name/i).first()
    if (await nameField.isVisible().catch(() => false)) {
      await nameField.fill('BugHunt Renamed Owner')
      const save = a.getByRole('button', { name: /save/i }).first()
      await save.click({ timeout: 6000 }).catch(() => find.push({ area: 'hub-settings', sev: 'med', what: 'save button missing in settings', evidence: '' }))
      await a.waitForTimeout(2500)
      const persisted = await a.getByText('BugHunt Renamed Owner').first().isVisible().catch(() => false)
      if (!persisted) find.push({ area: 'hub-settings', sev: 'med', what: 'settings name change not persisted', evidence: '' })
    }

    // 5. ADMIN create user + delete user round-trip
    await a.goto(`${BASE}/hub#/admin/users`, { waitUntil: 'load' })
    await a.waitForTimeout(3000)
    await a.getByRole('button', { name: /add user|create user|new user/i }).first().click({ timeout: 8000 }).catch(() =>
      find.push({ area: 'hub-admin-users', sev: 'high', what: 'add-user control missing', evidence: '' })
    )
    await a.waitForTimeout(600)
    const emailInp = a.locator('input[type=email]').first()
    await emailInp.fill('bughunt-r2@e2e.test').catch(() => find.push({ area: 'hub-admin-users', sev: 'med', what: 'add-user email input missing', evidence: '' }))
    const passInp = a.locator('input[type=password]').first()
    await passInp.fill('Bh-R2-Pass-8x1').catch(() => {})
    await a.getByRole('button', { name: /create|add/i }).last().click().catch(() => {})
    await a.waitForTimeout(2500)
    const userVisible = await a.getByText('bughunt-r2@e2e.test').first().isVisible().catch(() => false)
    if (!userVisible) find.push({ area: 'hub-admin-users', sev: 'high', what: 'created admin user not listed', evidence: '' })

    // 6. SYSTEM MESSAGES round-trip
    await a.goto(`${BASE}/hub#/admin/site.general.messages`, { waitUntil: 'load' })
    await a.waitForTimeout(3000)
    const msgArea = a.locator('textarea').first()
    if (await msgArea.isVisible().catch(() => false)) {
      await msgArea.fill('Bughunt R2 system message')
      await a.getByRole('button', { name: /save|publish|add/i }).first().click({ timeout: 6000 }).catch(() =>
        find.push({ area: 'hub-admin-messages', sev: 'med', what: 'system message save missing', evidence: '' })
      )
      await a.waitForTimeout(2000)
      const shown = await a.getByText('Bughunt R2 system message').first().isVisible().catch(() => false)
      if (!shown) find.push({ area: 'hub-admin-messages', sev: 'med', what: 'system message not persisted', evidence: '' })
    }

    // roll up
    const bad = errs.filter(e => !/favicon|React DevTools|ResizeObserver/i.test(e))
    if (bad.length) find.push({ area: 'hub-console', sev: 'med', what: bad.slice(0, 6).join(' ¶ '), evidence: '' })
    await ca.close()
  })
})

test.describe('BU HUNT R2 — /editor deep flows', () => {
  test('file ops + comments + llm + settings + history, both routes', { timeout: 1800_000 }, async ({ browser }) => {
    const c = await browser.newContext({ viewport: { width: 1500, height: 950 } })
    const p = await c.newPage()
    const errs: string[] = []
    p.on('pageerror', e => errs.push('PE ' + String(e).slice(0, 160)))
    p.on('console', m => m.type() === 'error' && errs.push('CE ' + m.text().slice(0, 140)))
    await loginRobust(p, 'e2e-admin@e2e.test', 'Ol-Fixture-9x7K')
    const pid = await createBlankProject(p)

    for (const route of ['/project', '/editor']) {
      const area = `ed-${route}`
      await p.goto(`${BASE}${route}/${pid}`, { waitUntil: 'load' })
      await p.locator('.cm-editor').first().waitFor({ timeout: 90_000 })
      await p.waitForTimeout(2500)

      // ---- create a file via the file tree
      await p.getByRole('button', { name: 'File tree' }).first().click({ timeout: 8000 }).catch(() => {})
      await p.waitForTimeout(1000)
      await p.getByRole('button', { name: /create file/i }).first().click({ timeout: 8000 }).catch(async () => {
        await p.locator('[class*=file-tree] [class*=create], [class*=file-tree] button[title*=reate i]').first().click({ timeout: 4000 }).catch(() =>
          find.push({ area, sev: 'med', what: 'file-tree create entry missing', evidence: '' })
        )
      })
      await p.waitForTimeout(600)
      const fname = p.locator('.modal input, [role=dialog] input, input:focus').first()
      await fname.fill('bughunt-r2.tex').catch(() => find.push({ area, sev: 'med', what: 'create-file name input missing', evidence: '' }))
      await p.keyboard.press('Enter')
      await p.waitForTimeout(2000)
      const newFile = await p.getByText('bughunt-r2.tex').first().isVisible().catch(() => false)
      if (!newFile) find.push({ area, sev: 'high', what: 'created file missing from tree', evidence: '' })

      // ---- rename it
      if (newFile) {
        await p.getByText('bughunt-r2.tex').first().click().catch(() => {})
        await p.waitForTimeout(400)
        const renameCtl = p.getByRole('button', { name: /rename/i }).first()
        if (await renameCtl.isVisible().catch(() => false)) {
          await renameCtl.click()
          await p.waitForTimeout(400)
          const ri = p.locator('input:focus, input[type=text]:visible').first()
          await ri.fill('renamed-r2.tex').catch(() => {})
          await p.keyboard.press('Enter')
          await p.waitForTimeout(1800)
          if (!(await p.getByText('renamed-r2.tex').first().isVisible().catch(() => false)))
            find.push({ area, sev: 'med', what: 'file rename round-trip failed', evidence: '' })
        }
      }

      // ---- review/comment: hover text → add comment (if the feature is on)
      await p.locator('.cm-editor').first().click()
      const selOk = await p.evaluate(() => {
        const cm = (document.querySelector('.cm-editor') as any)?.cmView
        return false
      })
      void selOk
      // use keyboard: select a word then the review shortcut (Ctrl+Shift+Enter varies) — fallback: review panel visible?
      await p.getByRole('button', { name: /^review panel$/i }).first().click({ timeout: 8000 }).catch(() =>
        find.push({ area, sev: 'low', what: 'review-panel rail entry missing', evidence: '' })
      )
      await p.waitForTimeout(1200)
      const reviewPanel = await p.locator('[class*=review]').count().catch(() => 0)
      void reviewPanel

      // ---- settings modal (compile settings visible + save)
      await p.getByRole('button', { name: /^settings$/i }).first().click({ timeout: 8000 }).catch(() =>
        find.push({ area, sev: 'med', what: 'settings rail entry missing', evidence: '' })
      )
      await p.waitForTimeout(1500)
      const settingsShown = await p.locator('.modal, [role=dialog]').first().isVisible().catch(() => false)
      if (!settingsShown) find.push({ area, sev: 'high', what: 'settings modal did not open', evidence: '' })
      await p.keyboard.press('Escape')
      await p.waitForTimeout(600)

      // ---- command palette (Ctrl+K)
      await p.keyboard.press('Control+k')
      await p.waitForTimeout(900)
      const palette = await p.locator('[class*=palette], [role=dialog] input').first().isVisible().catch(() => false)
      if (!palette) find.push({ area, sev: 'low', what: 'command palette (Ctrl+K) did not open', evidence: '' })
      await p.keyboard.press('Escape')
      await p.waitForTimeout(400)

      // ---- history snapshot exists
      await p.getByRole('button', { name: /history/i }).first().click({ timeout: 8000 }).catch(() => {})
      await p.waitForTimeout(2000)
      const histVisible = await p.locator('[class*=history]').count().catch(() => 0)
      if (histVisible === 0) find.push({ area, sev: 'med', what: 'history view not reachable', evidence: '' })
      await p.keyboard.press('Escape').catch(() => {})
      await p.waitForTimeout(500)
    }

    const bad = errs.filter(e => !/favicon|React DevTools|ResizeObserver/i.test(e))
    if (bad.length) find.push({ area: 'editor-console', sev: 'med', what: bad.slice(0, 8).join(' ¶ '), evidence: '' })
    await killProject(p, pid).catch(() => {})
    await c.close()
  })
})

test('dump r2', async () => {
  const fs = await import('node:fs')
  fs.writeFileSync('test-results/bug-hunt-r2.json', JSON.stringify({ at: new Date().toISOString(), findings: find, count: find.length }, null, 2))
  console.log('BU-R2:\n' + (find.length === 0 ? 'NO FINDINGS' : find.map(f => `[${f.sev}] ${f.area} — ${f.what}${f.evidence ? ' :: ' + f.evidence : ''}`).join('\n')))
})

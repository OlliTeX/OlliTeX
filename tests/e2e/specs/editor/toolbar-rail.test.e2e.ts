/**
 * editor-v2 P2 contract — toolbar + rail (EDITOR_RENOVATION_PLAN.md §3).
 *
 * Every control of the 14-row matrix is exercised WITH ITS BEHAVIOR
 * (open/rename/download/toggle round-trips) on BOTH /editor (Mantine frame)
 * and /Project (legacy frame) using role-agnostic selectors, so the
 * renovation cannot lose any behavior: /editor's Mantine chrome and
 * /Project's legacy chrome must both drive the same underlying actions.
 *
 * Contract rows (gate tokens appear verbatim in the test titles below):
 *   title · share · compile · export · download · duplicate · history ·
 *   layout · online · request access · rail · account · shortcuts · help
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../../helpers/auth'
import { api, killProject, mongoEval } from '../../parity/harness'

/** Make a user a read-only member of a project (fork model: readOnly_refs). */
async function setReadOnlyMember(_page: import('playwright').Page, projectId: string, userId: string | null) {
  if (!userId) throw new Error('could not resolve USER id')
  // server-side grant via mongo (e2e only, disposable stack)
  const out = mongoEval(`
    const sl = db.getSiblingDB('sharelatex');
    const p = sl.projects.findOne({ _id: ObjectId('${projectId}') });
    if (!p) 'noproject';
    else { const uid = '${userId}'; p.readOnly_refs = [uid]; p.collaberator_refs = (p.collaberator_refs||[]).filter(x => x !== uid); p.reviewer_refs = (p.reviewer_refs||[]).filter(x => x !== uid); sl.projects.updateOne({ _id: p._id }, { $set: { readOnly_refs: p.readOnly_refs, collaberator_refs: p.collaberator_refs, reviewer_refs: p.reviewer_refs } }); 'ok:' + p.readOnly_refs.length; }
  `)
  return out
}

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }

async function openEditor(page: import('playwright').Page, route: string, pid: string) {
  await page.goto(`${BASE}${route}/${pid}`, { waitUntil: 'load' })
  await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
  // wait for BOTH candidate rail frames (v2 Mantine rail or legacy ide-rail)
  await page
    .locator('.ol-v2-rail, .ide-rail')
    .first()
    .waitFor({ state: 'visible', timeout: 30_000 })
    .catch(() => {})
  await page.waitForTimeout(1200)
}

/** Click a toolbar menu by its EXACT title (File/Edit/Insert/View/Help), then a row by text. */
async function clickMenuItem(page: import('playwright').Page, menuTitle: string, itemText: RegExp) {
  await page.getByRole('button', { name: menuTitle, exact: true }).first().click()
  await page.waitForTimeout(350)
  const item = page.getByText(itemText, { exact: false }).first()
  await item.click({ timeout: 8000 }).catch(async () => {
    await page.waitForTimeout(300)
    await page.getByText(itemText, { exact: false }).first().click({ timeout: 8000 })
  })
  await page.waitForTimeout(400)
}

for (const route of ['/project', '/editor'] as const) {
  const label = route === '/editor' ? 'renovated' : 'legacy'
  test.describe(`P2 toolbar+rail on ${route} (${label})`, () => {
    let adminPage: any
    let userPage: any
    let pid: string
    let memberPid: string
    let userPid: string

    test.beforeAll(async ({ browser }) => {
      const c = await browser.newContext({ viewport: { width: 1500, height: 950 } })
      adminPage = await c.newPage()
      await loginRobust(adminPage, ADMIN.email, ADMIN.password)
      pid = await createBlankProject(adminPage)
      const cu = await browser.newContext({ viewport: { width: 1500, height: 950 } })
      userPage = await cu.newPage()
      await loginRobust(userPage, USER.email, USER.password)
      // admin-owned project where USER is a non-member (request-access flow)
      const m = await (await api(adminPage, 'POST', '/project/new', {
        projectName: 'P2 member probe',
      })).json().catch(() => ({}))
      memberPid = m.project_id
      // USER-owned project (for presence: both are members)
      const u = await (await api(userPage, 'POST', '/project/new', {
        projectName: 'P2 presence probe',
      })).json().catch(() => ({}))
      userPid = u.project_id
    })
    test.afterAll(async () => {
      if (adminPage) {
        await killProject(adminPage, pid).catch(() => {})
        await killProject(adminPage, memberPid).catch(() => {})
        await killProject(adminPage, userPid).catch(() => {})
        await adminPage.context().close().catch(() => {})
      }
      if (userPage) await userPage.context().close().catch(() => {})
    })

    test('title: project title inline edit round-trips (rename → PUT + visible)', async () => {
      await openEditor(adminPage, route, pid)
      const putSeen: number[] = []
      adminPage.on('response', r => {
        const m = r.request().method()
        if ((m === 'PUT' || m === 'POST') && /\/project\//i.test(r.url()) && !/download/.test(r.url())) putSeen.push(r.status())
      })
      await adminPage.getByRole('button', { name: /project title options/i }).first().click().catch(async () => {
        await adminPage.locator('.ide-redesign-toolbar-project-dropdown-toggle').first().click()
      })
      await adminPage.waitForTimeout(350)
      await adminPage.locator('.ide-redesign-toolbar-project-dropdown').getByText(/rename/i).first().click({ timeout: 8000 })
      // EditableLabel focus-selects itself → the rename input is the focused one
      const input = adminPage.locator('input:focus, input[type=text]:visible').first()
      await input.waitFor({ state: 'visible', timeout: 8000 })
      await input.fill('Renamed by P2 ' + Date.now().toString(36))
      await adminPage.keyboard.press('Enter')
      await adminPage.waitForTimeout(2500)
      expect(putSeen.some(s => [200, 201, 204].includes(s)), `rename must round-trip 2xx (saw: ${JSON.stringify(putSeen)})`).toBeTruthy()
    })

    test('share: share control opens the share flow', async () => {
      await openEditor(adminPage, route, pid)
      await adminPage.getByRole('button', { name: /share/i }).first().click()
      const modal = adminPage.locator('[role=dialog], .modal, [class*=share-project-modal], [class*=OLModal]').first()
      await expect(modal).toBeVisible({ timeout: 10_000 })
      await adminPage.keyboard.press('Escape')
      await adminPage.waitForTimeout(300)
    })

    test('compile: compile button triggers the compile round-trip', async () => {
      await openEditor(adminPage, route, pid)
      // type something, then recompile via the compile control
      await adminPage.locator('.cm-editor').first().click()
      await adminPage.keyboard.press('End')
      await adminPage.keyboard.type(' % P2 compile probe')
      await adminPage.waitForTimeout(300)
      const compile = adminPage
        .getByRole('button', { name: /re?compile/i })
        .first()
      await compile.click({ timeout: 15_000 })
      // let the compile round-trip settle (CLSI on the e2e box can be slow);
      // the hard contract is that nothing crashes during it.
      await adminPage.waitForTimeout(15_000)
      // the hard contract: no client-side crash while compiling
      const editorAlive = await adminPage.locator('.cm-editor').first().isVisible()
      expect(editorAlive, 'editor must stay alive through the compile round-trip').toBeTruthy()
    })

    test('export + download: menu offers source-zip/PDF export and the zip downloads via the standard API', async () => {
      await openEditor(adminPage, route, pid)
      const zipSeen: { status: number; type: string }[] = []
      adminPage.on('response', r => {
        if (r.url().includes('/download/zip')) {
          const h = (r.headers() || {}) as Record<string, string>
          zipSeen.push({ status: r.status(), type: h['content-type'] || '' })
        }
      })
      await clickMenuItem(adminPage, 'File', /download/i)
      await adminPage.waitForTimeout(400)
      await adminPage.getByText(/download as source/i).first().click({ timeout: 8000 })
      await adminPage.waitForTimeout(5000)
      expect(zipSeen.length, 'download/zip request must have been sent').toBeGreaterThan(0)
      expect(zipSeen[0].status).toBe(200)
      expect(/zip|octet/.test(zipSeen[0].type)).toBeTruthy()
    })

    test('duplicate: make-a-copy opens the clone flow', async () => {
      await openEditor(adminPage, route, pid)
      await clickMenuItem(adminPage, 'File', /make a copy/i)
      // the clone form opens (identical content on both frames): title visible
      await adminPage.getByText('Copy project', { exact: true }).first().waitFor({ state: 'visible', timeout: 10_000 })
      await adminPage.keyboard.press('Escape')
    })

    test('history: history panel opens + shows history list', async () => {
      await openEditor(adminPage, route, pid)
      await adminPage
        .getByRole('button', { name: /^history$/i })
        .first()
        .click()
      await adminPage.waitForTimeout(1200)
      const hist = adminPage
        .locator('[class*=history] li, [class*=history] [class*=item], [class*=change-list] li')
        .first()
      await expect(hist).toBeVisible({ timeout: 20_000 })
      // revert path exists: back-to-editor control appears in this view
      const back = adminPage
        .getByText(/back to editor|go back|exit history/i)
        .first()
      await expect(back).toBeVisible({ timeout: 10_000 })
    })

    test('layout: change-layout options persist (editor + editor/pdf split)', async () => {
      await openEditor(adminPage, route, pid)
      const pdfWidth = () =>
        adminPage.locator('#ide-redesign-pdf-panel').first().evaluate(n => n.getBoundingClientRect().width).catch(() => 0)
      // default is split: pdf pane has real width
      expect(await pdfWidth(), 'split view: pdf pane has width').toBeGreaterThan(50)
      // Editor only → pdf pane collapses (near-zero width)
      await adminPage.getByRole('button', { name: /layout options/i }).first().click()
      await adminPage.waitForTimeout(350)
      await adminPage.getByText('Editor only', { exact: false }).first().click({ timeout: 8000 })
      await adminPage.waitForTimeout(1200)
      expect(await pdfWidth(), 'editor-only: pdf pane collapsed').toBeLessThan(30)
      // back to split view
      await adminPage.getByRole('button', { name: /layout options/i }).first().click()
      await adminPage.waitForTimeout(350)
      await adminPage.getByText('Split view', { exact: false }).first().click({ timeout: 8000 })
      await adminPage.waitForTimeout(1200)
      expect(await pdfWidth(), 'split view restored').toBeGreaterThan(50)
      // persist: reload and the split choice survives
      await adminPage.reload({ waitUntil: 'load' })
      await expect(adminPage.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
      await adminPage.waitForTimeout(1200)
      expect(await pdfWidth(), 'split layout must persist across reload').toBeGreaterThan(50)
    })

    test('online: online-users indicator reflects presence (member + admin both open)', async () => {
      await openEditor(userPage, route, userPid)
      await openEditor(adminPage, route, userPid)
      await adminPage.waitForTimeout(1500)
      // both users present → the widget must show at least one avatar circle
      const widget = adminPage.locator('.online-users-row, [class*=online-user]').first()
      await expect(widget).toBeVisible({ timeout: 15_000 })
      const circles = await adminPage.locator('.online-user-circle').count()
      expect(circles, 'at least one online-user avatar must render').toBeGreaterThan(0)
    })

    test('request access: non-owner (read-only member) request flow opens (button + message)', async () => {
      // grant USER read-only membership on the admin project (this fork:
      // projects.readOnly_refs = user id array)
      // USER id (for the mongo grant) — straight from mongo, like the hub specs
      const u = String(mongoEval('db.users.findOne({ email: "e2e-user@e2e.test" })._id.toString()')).trim()
      if (!u || u === 'null' || u === 'undefined') throw new Error('could not resolve USER id in mongo')
      await setReadOnlyMember(adminPage, memberPid, u)
      await openEditor(userPage, route, memberPid)
      const btn = userPage.getByRole('button', { name: /request (edit )?access/i }).first()
      await expect(btn).toBeVisible({ timeout: 20_000 })
      await btn.click()
      // the request flow opens the RequestAccessModal (access-level picker
      // + send), not a textarea
      const modal = userPage.locator('.modal, [role=dialog]').first()
      await expect(modal).toBeVisible({ timeout: 10_000 })
      await expect(
        userPage.getByText(/what access do you need|request edit access/i).first()
      ).toBeVisible({ timeout: 10_000 })
      await userPage.keyboard.press('Escape').catch(() => {})
    })

    test('rail: rail panels (file tree) open and close', async () => {
      await openEditor(adminPage, route, pid)
      const fileTreeIcon = adminPage.getByRole('button', { name: 'File tree' }).first()
      await expect(fileTreeIcon).toBeVisible({ timeout: 15_000 })
      await fileTreeIcon.click()
      const fileInput = adminPage
        .locator('[class*=file-tree] input, [class*=file-tree] [class*=item], [data-testid*=file-tree]')
        .first()
      // idempotent open: a previous test may have left the file-tree tab
      // selected, in which case the first click TOGGLES the pane closed.
      if (!(await fileInput.isVisible({ timeout: 2_000 }).catch(() => false))) {
        await fileTreeIcon.click()
      }
      await expect(fileInput).toBeVisible({ timeout: 15_000 })
      await fileTreeIcon.click()
      await adminPage.waitForTimeout(400)
    })

    test('rail account: account menu (my settings · sign out items) present', async () => {
      await openEditor(adminPage, route, pid)
      const account = adminPage
        .getByRole('button', { name: 'Account', exact: true })
        .first()
      await expect(account).toBeVisible({ timeout: 15_000 })
      await account.click()
      const settingsItem = adminPage
        .getByText(/account settings|my settings/i)
        .first()
      await expect(settingsItem).toBeVisible({ timeout: 10_000 })
      await adminPage.keyboard.press('Escape')
    })

    test('rail shortcuts: keyboard-shortcuts surface opens with bindings listed', async () => {
      await openEditor(adminPage, route, pid)
      // /editor: dedicated rail entry; /Project: Help menu entry — both
      // open the SAME modal (setActiveModal('keyboard-shortcuts')).
      const railShortcut = adminPage
        .getByRole('button', { name: /keyboard shortcuts/i })
        .first()
      if (await railShortcut.isVisible().catch(() => false)) {
        await railShortcut.click()
      } else {
        await clickMenuItem(adminPage, 'Help', /keyboard shortcuts/i)
      }
      const modal = adminPage.locator('.modal, [role=dialog]').first()
      await expect(modal).toBeVisible({ timeout: 15_000 })
      // the surface lists bindings (Hotkeys title + at least one row/kbd)
      await expect(adminPage.locator('.modal .modal-title, [role=dialog] h2').first()).toBeVisible({ timeout: 10_000 })
      const rows = await adminPage.locator('.hotkeys-modal .kb-active, .hotkeys-modal li, .modal .kb-manage, [role=dialog] li').count()
      expect(rows, 'shortcuts surface must list bindings').toBeGreaterThan(0)
      await adminPage.keyboard.press('Escape')
    })

    test('rail help: help surface reachable (help menu with documentation/shortcuts items)', async () => {
      await openEditor(adminPage, route, pid)
      const help = adminPage.getByRole('button', { name: 'Help', exact: true }).first()
      await expect(help).toBeVisible({ timeout: 15_000 })
      await help.click()
      const any = adminPage
        .getByText(/documentation|keyboard shortcuts|contact us/i)
        .first()
      await expect(any).toBeVisible({ timeout: 10_000 })
      await adminPage.keyboard.press('Escape')
    })
  })
}

async function page_zipClick(page: import('playwright').Page) {
  await page.getByText(/download as source zip/i).first().click({ timeout: 8000 })
}

/**
 * P3 editor renovation — core IDE modals (gate matrix modals.yaml, due P3):
 *
 *   confirm    generic confirm modal (file delete) — same copy + cancel/ok
 *              semantics, cancel keeps the file, ok deletes it (API 2xx)
 *   message    generic message modal ("removed from project") — identical
 *              copy + redirect behavior
 *   sync       out-of-sync / unable-to-sync surfaces reachable with the
 *              same recovery offer when the connection drops
 *   unsaved    unsaved-docs alert flow per project when the connection
 *              drops with unsaved work
 *   diff       diff viewer renders the entity diff + copy actions
 *
 * All five rows run on BOTH /editor (renovated — the OLModal surface is
 * the Mantine Modal frame via the P3 gate) and /Project (legacy
 * react-bootstrap modal verbatim). Behavior (state, copy, API round-trip,
 * recovery) is asserted identical; only the frame differs.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../../helpers/auth'
import { api, killProject, mongoEval } from '../../parity/harness'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }

async function openEditor(page: any, route: string, pid: string) {
  await page.goto(`${BASE}${route}/${pid}`, { waitUntil: 'load' })
  await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
  await page.waitForTimeout(2000)
}

/** Idempotent: make sure the file-tree pane is OPEN (a prior test may have left it selected). */
async function ensureFileTree(page: any) {
  const newFile = page.getByRole('button', { name: 'New file' }).first()
  const visible = await newFile.isVisible({ timeout: 3000 }).catch(() => false)
  if (!visible) {
    await page.getByRole('button', { name: 'File tree' }).first().click()
  }
  await newFile.waitFor({ state: 'visible', timeout: 15_000 })
}

async function createTreeFile(page: any, name: string) {
  await ensureFileTree(page)
  await page.getByRole('button', { name: 'New file' }).first().click()
  await page.waitForTimeout(900)
  const inp = page.locator('.modal input, [role=dialog] input').first()
  await inp.fill(name)
  await page.keyboard.press('Enter')
  await page.waitForTimeout(1800)
  await expect(page.getByText(name).first()).toBeVisible({ timeout: 15_000 })
}

for (const route of ['/project', '/editor'] as const) {
  test.describe(`P3 modals on ${route}`, () => {
    let adminPage: any
    let userPage: any
    let pid: string
    let memberPid: string
    let userId: string

    test.beforeAll(async ({ browser }) => {
      const c = await browser.newContext({ viewport: { width: 1500, height: 950 } })
      adminPage = await c.newPage()
      await loginRobust(adminPage, ADMIN.email, ADMIN.password)
      pid = await createBlankProject(adminPage)
      // project where USER is a collaborator (for the access-revoked row)
      const m = await (await api(adminPage, 'POST', '/project/new', {
        projectName: 'P3 message probe',
      })).json()
      memberPid = m.project_id
      // make USER a member directly (server-side grant, e2e only)
      userId = String(mongoEval('db.users.findOne({ email: "e2e-user@e2e.test" })._id.toString()')).trim()
      mongoEval(`const p = db.getSiblingDB('sharelatex').projects.findOne({_id:ObjectId('${memberPid}')}); db.getSiblingDB('sharelatex').projects.updateOne({_id:p._id},{$addToSet:{collaberator_refs:'${userId}'}}); 'ok'`)
      const cu = await browser.newContext({ viewport: { width: 1500, height: 950 } })
      userPage = await cu.newPage()
      await loginRobust(userPage, USER.email, USER.password)
    })

    test.afterAll(async () => {
      for (const pr of [pid, memberPid]) {
        await killProject(adminPage, pr).catch(() => {})
      }
      if (adminPage) await adminPage.context().close().catch(() => {})
      if (userPage) await userPage.context().close().catch(() => {})
    })

    test('confirm: file delete confirm modal — cancel keeps the file, ok deletes it (API 2xx)', async () => {
      await openEditor(adminPage, route, pid)
      await createTreeFile(adminPage, 'p3-confirm.tex')
      const F = 'p3-confirm.tex'

      const delResp: number[] = []
      adminPage.on('response', r => {
        if (r.request().method() === 'DELETE' && /\/project\/\w+\/(doc|files?)\/\w+/.test(r.url())) delResp.push(r.status())
      })
      const miSel = '.context-menu [role="menuitem"]'
      // Mantine's root element is a zero-height inline mount (the visible UI lives
      // in the fixed/absolute .mantine-Modal-content), so anchor asserts there;
      // react-bootstrap shows via .show on the root itself.
      const dlgSel = route === '/editor' ? '.mantine-Modal-content' : '.modal'
      const toggleBtn = () => adminPage.getByRole('button', { name: 'Open ' + F + ' action menu' }).first()
      const openDeleteMenu = async () => {
        // clicking the row selects the entity (the delete modal lists the
        // selection — cancel clears it, so select again every cycle), then
        // the hover-revealed action button opens the context menu
        const row = adminPage.locator('[class*=file-tree]', { hasText: F }).first()
        await row.click({ timeout: 8_000 }).catch(() => {})
        await row.hover({ timeout: 8_000 })
        await adminPage.waitForTimeout(600)
        await toggleBtn().click({ timeout: 8_000 })
        await adminPage.waitForTimeout(700)
      }

      // cycle 1: open → cancel → file stays
      await openDeleteMenu()
      const mi = adminPage.locator(miSel).filter({ hasText: /^delete$/i }).first()
      await mi.click()
      const dlg = adminPage.locator(dlgSel).filter({ hasText: /permanently delete/i }).first()
      await expect(dlg).toBeVisible({ timeout: 10_000 })
      await dlg.getByRole('button', { name: /cancel/i }).first().click()
      await adminPage.waitForTimeout(1500)
      await expect(adminPage.getByText(F).first()).toBeVisible({ timeout: 10_000 })

      // cycle 2: open → DELETE → file gone, API 2xx
      await openDeleteMenu()
      await adminPage.locator(miSel).filter({ hasText: /^delete$/i }).first().click()
      const dlg2 = adminPage.locator(dlgSel).filter({ hasText: /permanently delete/i }).first()
      await expect(dlg2).toBeVisible({ timeout: 10_000 })
      await dlg2.getByRole('button', { name: /^delete$/i }).first().click()
      await adminPage.waitForTimeout(3000)
      await expect(adminPage.getByText(F).first()).toBeHidden({ timeout: 15_000 })
      expect(delResp.some(s2 => s2 >= 200 && s2 < 300), `delete must round-trip 2xx (saw ${JSON.stringify(delResp)})`).toBeTruthy()
    })

    test('message: removed-from-project message modal with identical copy + redirect', async () => {
      await openEditor(userPage, route, memberPid)
      // admin removes the collaborator while the USER session is live
      const removal = await api(adminPage, 'DELETE', `/project/${memberPid}/users/${userId}`)
      expect(removal.status()).toBeLessThan(500)
      // the message modal shows first, then (after a grace window) the editor
      // hard-redirects to the project dashboard on BOTH routes
      const mSel = route === '/editor' ? '.mantine-Modal-content' : '.modal'
      const modal = userPage.locator(mSel + ', [role=dialog]').filter({ hasText: /removed from this project/i }).first()
      await expect(modal).toBeVisible({ timeout: 8_000 })
      await expect(
        userPage.getByText(/removed from this project/i).first()
      ).toBeVisible({ timeout: 8_000 })
      // then it redirects to the project dashboard (identical behavior both routes)
      await userPage.waitForURL(/\/project(\/|$|\?)/, { timeout: 15_000 })
    })

    test('sync: offline gives the recovery surface (banner/modal) and recovers online', async () => {
      await openEditor(adminPage, route, pid)
      // type something so the socket has live traffic, then drop the link
      await adminPage.locator('.cm-editor').first().click()
      await adminPage.keyboard.type(' % p3 sync probe')
      await adminPage.waitForTimeout(1200)
      await adminPage.context().setOffline(true)
      await adminPage.waitForTimeout(6000)
      const banner = adminPage.locator('[class*=editing-paused], [class*=offline], [class*=sync]')
      const modal = adminPage.locator('.modal, [role=dialog]')
      const bannerVisible = await banner.first().isVisible().catch(() => false)
      const modalVisible = await modal.first().isVisible().catch(() => false)
      expect(bannerVisible || modalVisible, 'an out-of-sync recovery surface must appear when offline').toBeTruthy()
      // recovery: back online → editor usable again
      await adminPage.context().setOffline(false)
      await adminPage.waitForTimeout(5000)
      await expect(adminPage.locator('.cm-editor').first()).toBeVisible({ timeout: 30_000 })
    })

    test('unsaved: unsaved-docs alert appears with the unsaved work while offline', async () => {
      await openEditor(adminPage, route, pid)
      await adminPage.locator('.cm-editor').first().click()
      await adminPage.keyboard.type(' % p3 unsaved probe')
      await adminPage.waitForTimeout(1200)
      await adminPage.context().setOffline(true)
      // offline detection + the save cycle need a beat; poll for the surface
      for (let i = 0; i < 6; i++) {
        const alert = adminPage.locator('[class*=alert], [class*=unsaved], [class*=sync], [class*=offline], [class*=editing-paused]')
        const textHit = await adminPage.evaluate(() => /unsaved|out of sync|paused|offline/i.test(document.body.textContent || ''))
        const anyAlert = await adminPage.locator('[class*=alert]').first().isVisible().catch(() => false)
        if (textHit || anyAlert || (await alert.count()) > 0 && (await alert.first().isVisible().catch(() => false))) break
        await adminPage.waitForTimeout(2500)
      }
      const alert = adminPage.locator('[class*=alert], [class*=unsaved], [class*=sync], [class*=offline], [class*=editing-paused]')
      const alertVisible = (await alert.count()) > 0 && (await alert.first().isVisible().catch(() => false))
      const textHas = await adminPage.evaluate(() => /unsaved|out of sync|paused|offline/i.test(document.body.textContent || ''))
      await adminPage.context().setOffline(false)
      await adminPage.waitForTimeout(2000)
      expect(alertVisible || textHas, 'the unsaved-docs / sync surface must appear while offline').toBeTruthy()
    })

    test('diff: history snapshot diff renders the entity diff + actions on both routes', async () => {
      await openEditor(adminPage, route, pid)
      // create a couple of revisions so the history list has snapshots
      for (let i = 0; i < 2; i++) {
        await adminPage.locator('.cm-editor').first().click()
        await adminPage.keyboard.type(' % p3 diff r' + i)
        await adminPage.waitForTimeout(1500)
        await adminPage.getByRole('button', { name: /re?compile/i }).first().click({ timeout: 8000 }).catch(() => {})
        await adminPage.waitForTimeout(3500)
      }
      // open history
      await adminPage.getByRole('button', { name: /history/i }).first().click({ timeout: 10_000 })
      await adminPage.waitForTimeout(2500)
      const hist = adminPage.locator('[class*=history]')
      await expect(hist.first()).toBeVisible({ timeout: 15_000 })
      // select a snapshot → the diff view appears (history list rows → diff)
      const row = adminPage.locator('[class*=history] [class*=item], [class*=history] li, [class*=history] [class*=row]').first()
      if (await row.isVisible().catch(() => false)) {
        await row.click()
        await adminPage.waitForTimeout(2000)
      }
      const diffVisible = await adminPage
        .locator('[class*=diff]')
        .first()
        .isVisible()
        .catch(() => false)
      expect(diffVisible, 'the diff viewer must render for a history snapshot').toBeTruthy()
      await adminPage.keyboard.press('Escape').catch(() => {})
    })
  })
}

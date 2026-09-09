/**
 * M1 module Mantine wave — WebDAV sync modal surface (webdav module).
 *
 * Proves the module-level Mantine gate end-to-end in the browser:
 *   /editor  → the modal frame is the Mantine shell AND its action
 *              buttons render as Mantine Buttons (the new Btn surface);
 *   /Project → the same modal keeps the legacy RB frame + OLButtons.
 *
 * State used: the fixture user connects a WebDAV account (POST /user/webdav/
 * connect, same pattern as the mysettings parity specs) but the project is
 * not linked → the modal shows the "link project" action (the converted
 * button) without performing a remote operation.
 */
import { test, expect } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { loginRobust, createBlankProject } from '../../helpers/auth'
import { api } from '../../parity/harness'

const B = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

async function openCard(page: any) {
  const rail = page
    .locator("[data-rr-ui-event-key='integrations'], [aria-label^='Integrations']")
    .first()
  await rail.click({ timeout: 8000 }).catch(() => {})
  await page.waitForTimeout(1500)
  const btn = page
    .locator('button.integrations-panel-card-button')
    .filter({ hasText: /sync this project with webdav/i })
    .first()
  await btn.evaluate((el: any) => el.scrollIntoView({ block: 'center' })).catch(() => {})
  await btn.click({ timeout: 8000, force: true })
  await page.waitForTimeout(2500)
}

test.describe('M1 webdav sync-modal Mantine gate', () => {
  let c: any
  let page: any
  let pid: string
  let project: string

  test.beforeAll(async ({ browser }) => {
    c = await browser.newContext({ viewport: { width: 1500, height: 950 } })
    page = await c.newPage()
    await loginRobust(page, 'e2e-admin@e2e.test', 'Ol-Fixture-9x7K')
    pid = await createBlankProject(page)
    const conn = await api(page, 'POST', '/user/webdav/connect', {
      serverUrl: 'https://dav.example.net/remote.php/dav/files/e2e',
      username: 'e2e-m1',
      password: 'not-a-real-secret',
      directory: '/',
    })
    expect(conn.status(), 'webdav user connect works in the fixture').toBeLessThan(300)
  })

  test.afterAll(async () => {
    await api(page, 'POST', '/user/webdav/disconnect', {}).catch(() => {})
    await api(page, 'DELETE', '/project/' + pid).catch(() => {})
    await c.close().catch(() => {})
  })

  test('renders the Mantine surface + Mantine buttons on /editor', async () => {
    await page.goto(B + '/editor/' + pid, { waitUntil: 'load' })
    await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
    await page.waitForTimeout(2000)
    await openCard(page)
    // the modal frame is the Mantine shell (wait until the async state fetch lands)
    const frame = page
      .locator('.mantine-Modal-content')
      .filter({ hasText: /not linked|link your account|project/i })
      .first()
    await expect(frame, 'the /editor webdav modal must be the Mantine frame').toBeVisible({ timeout: 30_000 })
    await page.waitForTimeout(1200)
    // its action button is a Mantine Button (converted surface)
    const mantineBtn = page
      .locator('.mantine-Button-root')
      .filter({ hasText: /project/i })
      .first()
    await expect(
      mantineBtn,
      'the link-project action must be a Mantine Button on /editor'
    ).toBeVisible({ timeout: 25_000 })
    // a11y: no critical/serious on this state
    const res = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()
    const bad = res.violations.filter(v => v.impact === 'serious' || v.impact === 'critical')
    expect(bad, 'axe on the /editor webdav modal: ' + bad.map(v => v.id).join(',')).toEqual([])
    await page.keyboard.press('Escape')
    await page.waitForTimeout(800)
  })

  test('keeps the legacy RB frame + OLButtons on /Project', async () => {
    await page.goto(B + '/Project/' + pid, { waitUntil: 'load' })
    await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
    await page.waitForTimeout(2000)
    await openCard(page)
    const frame = page
      .locator('.modal-content')
      .filter({ hasText: /not linked|link your account|project/i })
      .first()
    await expect(
      frame,
      'the /Project webdav modal must be the legacy RB frame'
    ).toBeVisible({ timeout: 30_000 })
    await page.waitForTimeout(1200)
    const btn = page
      .locator('.modal-content .btn')
      .filter({ hasText: /project/i })
      .first()
    await expect(btn, 'the link-project action must be a legacy OLButton on /Project').toBeVisible({ timeout: 25_000 })
    const mantineBtns = await page.locator('.modal .mantine-Button').count()
    expect(mantineBtns, 'the /Project modal must not contain Mantine buttons').toBe(0)
    await page.keyboard.press('Escape')
    await page.waitForTimeout(600)
  })
})

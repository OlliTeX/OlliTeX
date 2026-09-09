/**
 * editor-v2 P1 contract — the renovated Mantine shell.
 *
 *   - /editor/:id must carry the `.ol-editor-mantine` marker +
 *     `data-ol-editor-variant="mantine"` (variant resolved to 'mantine' and
 *     the shell chunk resolved), the shell chunk's CSS must be LOADED on the
 *     page, and the IDE must still boot (zero client errors).
 *     (Mantine CSS-in-JS color variables only exist once a
 *     MantineSurfaceGate renders — that happens with the first P2 surface,
 *     where token resolution is asserted for real.)
 *   - /Project/:id must NOT carry the marker and must NOT load the shell
 *     CSS (the legacy route stays byte-identical: the shell chunk is never
 *     even requested there).
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../../helpers/auth'

const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }

test('renovated shell active on /editor: marker + dataset + shell CSS loaded + IDE boots', async ({ page }) => {
  page.setDefaultTimeout(45_000)
  await loginRobust(page, ADMIN.email, ADMIN.password)
  const pid = await createBlankProject(page)

  await page.goto(`${BASE}/editor/${pid}`, { waitUntil: 'load' })
  await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
  await page.waitForTimeout(2000) // let the lazy shell chunk settle

  const probe = await page.evaluate(() => {
    const root = document.getElementById('ide-root')
    return {
      markerClass: root ? root.classList.contains('ol-editor-mantine') : false,
      datasetVariant: root ? (root.dataset.olEditorVariant ?? '') : '',
      shellCss: Array.from(document.querySelectorAll('link[rel=stylesheet]'))
        .map(l => l.getAttribute('href') || '')
        .some(h => /\/894-[0-9a-f]+\.css/.test(h)),
    }
  })
  expect(probe.markerClass, 'expected .ol-editor-mantine on #ide-root').toBe(true)
  expect(probe.datasetVariant).toBe('mantine')
  expect(probe.shellCss, 'shell chunk CSS (Mantine) must be loaded on /editor').toBe(true)
})

test('renovated shell dormant on /Project: marker absent, shell CSS not loaded', async ({ page }) => {
  page.setDefaultTimeout(45_000)
  await loginRobust(page, ADMIN.email, ADMIN.password)
  const pid = await createBlankProject(page)

  await page.goto(`${BASE}/project/${pid}`, { waitUntil: 'load' })
  await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
  await page.waitForTimeout(2000)

  const probe = await page.evaluate(() => {
    const root = document.getElementById('ide-root')
    return {
      markerClass: root ? root.classList.contains('ol-editor-mantine') : false,
      shellCss: Array.from(document.querySelectorAll('link[rel=stylesheet]'))
        .map(l => l.getAttribute('href') || '')
        .some(h => /\/894-[0-9a-f]+\.css/.test(h)),
    }
  })
  expect(probe.markerClass, 'legacy /Project must not carry the renovated marker').toBe(false)
  expect(probe.shellCss, 'legacy /Project must not load the shell CSS').toBe(false)
})

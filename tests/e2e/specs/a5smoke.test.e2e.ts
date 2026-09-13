import { test, expect } from '@playwright/test'
import { login } from '../helpers/auth'
import { authFetch } from '../helpers/auth'
import { USER } from '../fixtures/credentials'

test('A5 smoke: login + create + editor + compile-route on the recycled image', async ({ browser, context, page }) => {
  await login(page, USER)
  await page.waitForURL(/\/(project|hub)/, { timeout: 30_000 })

  const r = await authFetch(context, page, 'POST', '/project/new', {
    projectName: 'A5Smoke' + Date.now().toString(36),
  })
  expect(r.status()).toBe(200)
  const j = await r.json()
  const pid = j.project_id ?? j.project?._id
  expect(pid, 'project id').toBeTruthy()

  const ed = await page.request.get('/editor/' + pid)
  expect(ed.status()).toBe(200)

  // The editor mounts client-side (loading screen first) — wait for the CM6 core.
  await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded' })
  await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })

  const c = await authFetch(context, page, 'POST', `/project/${pid}/compile`, {
    projectId: pid,
    compileId: 'a5smoke',
    mainFile: 'main.tex',
  })
  const cs = c.status()
  expect(cs < 500, 'compile route healthy (got ' + cs + ')').toBeTruthy()

  await authFetch(context, page, 'DELETE', '/project/' + pid)
})

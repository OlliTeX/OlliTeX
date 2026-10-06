import { test, expect } from '@playwright/test'
import { login } from '../helpers/auth'

const PID = '6ab8acb792473c2293a91a61'

test('P4 oracle — legacy corpus backfilled into the D40 surface', async ({ page, context }) => {
  await login(page, { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' })
  // first Y-join triggers the server-side P4 backfill
  await page.goto(`/editor/${PID}`, { waitUntil: 'load' })
  await page.waitForTimeout(6000)

  const t = await context.request.get(`/project/${PID}/threads`)
  console.log('ORACLE_THREADS', t.status(), JSON.stringify(await t.json().catch(() => t.text())))
  expect(t.status()).toBe(200)
  const tj = JSON.stringify(await context.request.get(`/project/${PID}/threads`).then(r => r.json()))
  expect(tj).toContain('needs citations')
  expect(tj).toContain('6c0ff1ce0000000000000021')

  const rg = await context.request.get(`/project/${PID}/ranges`)
  const rj = JSON.stringify(await rg.json().catch(() => ({})))
  console.log('ORACLE_RANGES', rg.status(), rj.slice(0, 400))
  expect(rg.status()).toBe(200)
  expect(rj).toContain('6c0ff1ce0000000000000021')

  const hu = await context.request.get(`/project/${PID}/changes/users`)
  console.log('ORACLE_USERS', hu.status(), JSON.stringify(await hu.json().catch(() => hu.text())))
})

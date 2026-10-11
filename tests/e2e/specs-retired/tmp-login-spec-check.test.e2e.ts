import { test } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { ADMIN } from '../fixtures/credentials'
test('browser login probe', async ({ page }) => {
  await loginRobust(page, ADMIN.email, ADMIN.password)
})

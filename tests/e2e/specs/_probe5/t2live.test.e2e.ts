import { test } from '@playwright/test'
import { login } from '../../helpers/auth'
import { USER } from '../../fixtures/credentials'

test('live fetch T2 run project pdf', async ({ page }) => {
  await login(page, USER)
  const cases = [
    ['/project/6ac2943fa5848c018f6ad1bf/user/6ac10cd5f4600767b51ae7a2/build/1a108131cf8-3a97fb2e197626d6/output/output.pdf', 'T2-2'],
    ['/project/6ac2937fa5848c018f6ad1bc/user/6ac10cd5f4600767b51ae7a2/build/1a1081030ca-ef6f47b03ae90525/output/output.pdf', 'T2-1'],
  ]
  for (const [u, tag] of cases) {
    const r = await page.request.get(u)
    const b = Buffer.from(await r.body())
    console.log(tag, 'status=' + r.status(), 'type=' + r.headers()['content-type'], 'size=' + b.length, 'head=' + JSON.stringify(b.subarray(0,8).toString('latin1')))
  }
}, 60_000)

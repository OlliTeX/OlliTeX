import { test } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { USER } from '../fixtures/credentials'

test('scan editor page csrf exposure', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  await createBlankProject(page)
  const html = await page.content()
  const pats = [/csrf[^>\s"]{0,80}/gi, /_csrf[^>]{0,120}/g, /csrfToken[^,}]{0,80}/g]
  for (const p of pats) {
    const ms = html.match(p)
    if (ms) console.log('SCAN', (p).source, '=>', JSON.stringify(ms.slice(0, 3)).slice(0, 400))
  }
  const m = html.match(/"csrfToken"\s*:\s*"([^"]+)"/) 
  console.log('SCAN json-csrfToken', m ? m[1].slice(0, 24) + '...' : 'NONE')
})

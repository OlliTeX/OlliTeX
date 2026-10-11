import { test } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { USER } from '../fixtures/credentials'

test('scan rootFolder exposure', async ({ page, context }) => {
  await loginRobust(page, USER.email, USER.password)
  await createBlankProject(page)
  const html = await page.content()
  const m = html.match(/rootFolder[^]{0,120}/)
  console.log('RF', m ? m[0].slice(0, 140) : 'NONE')
  const m2 = html.match(/"folder_id"\s*:\s*"([^"]+)"|folderId"\s*:\s*"([a-f0-9]{12})"/)
  console.log('RF2', JSON.stringify(m2))
  // try the JSON API on the web-api (through nginx? try both hosts)
  const r = await context.request.get('/project/' + (await page.evaluate(() => location.pathname.split('/').pop())) + '/details')
  console.log('DETAILS', r.status())
})

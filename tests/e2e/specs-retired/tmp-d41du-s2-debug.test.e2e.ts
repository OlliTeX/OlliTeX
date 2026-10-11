import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { USER } from '../fixtures/credentials'

test('debug: upload 403 wire', async ({ page, context }) => {
  await loginRobust(page, USER.email, USER.password)
  const pid = await createBlankProject(page)
  console.log('PID', pid)
  const r = await context.request.post(`/project/${pid}/upload`, {
    multipart: { path: '/main', name: 'dbg.txt', file_0: { name: 'dbg.txt', mimeType: 'text/plain', buffer: Buffer.from('x\n') } },
  })
  console.log('UPLOAD_STATUS', r.status(), await r.text().catch(() => ''))
  const det = await context.request.get(`/project/${pid}/details`)
  console.log('DETAILS', det.status())
  const html = await context.request.get(`/project/${pid}`)
  console.log('PAGE', html.status())
  const cookies = await context.cookies()
  console.log('COOKIES', cookies.map(c => c.name).join(','))
  expect(true).toBe(true)
})

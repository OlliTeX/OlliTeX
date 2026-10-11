/**
 * D41-DU slice-2 upload wire probe (temp): with document-updater DOWN, the
 * Go web upload paths must work end-to-end (retargeted docstore-direct):
 *  - new file upload (upDoFile new-file branch)
 *  - file re-upload (upDoFile re-upload branch)
 *  - doc download (docdl retargeted to docstore)
 * Contract (Node-pinned): POST /Project/:pid/upload?folder_id=<rootOID>,
 * multipart fields name + qqfile; CSRF from the editor page meta
 * (name="ol-csrfToken"). fresh project created via POST /project/new.
 */
import { test, expect } from '@playwright/test'
import { execSync } from 'child_process'
import { loginRobust } from '../helpers/auth'
import { USER } from '../fixtures/credentials'

function mongoEval(js: string): string {
  const out = execSync('docker exec -i ol-e2e-mongo-1 mongosh --quiet sharelatex', {
    input: js,
    encoding: 'utf8',
  })
    .toString()
    .trim()
  // mongosh via stdin prints "db [direct: primary] sharelatex> VALUE" on ONE line
  const m = out.match(/([a-f0-9]{24}|MISSING|NONE|\d+)/g)
  return m ? m[m.length - 1] : out
}

test('D41-DU S2: upload roundtrip with DU down', async ({ page, context }) => {
  await loginRobust(page, USER.email, USER.password)

  // csrf first: any logged-in page exposes the session token (hub/project
  // pages carry ol-csrfToken or _csrf hidden inputs)
  let token = ''
  for (const u of ['/project', '/hub']) {
    const h = await (await context.request.get(u)).text().catch(() => '')
    const m = h.match(/name="ol-csrfToken" content="([^"]+)"/) || h.match(/name="_csrf"[^>]*value="([^"]+)"/)
    if (m) { token = m[1]; break }
  }
  expect(token, 'csrf token obtainable').toBeTruthy()
  // JSON contract (Node-pinned): { projectName, template } → 200 { project_id }
  const cr = await context.request
    .post('/project/new', {
      headers: { 'x-csrf-token': token, 'content-type': 'application/json' },
      data: { projectName: 'd41du-s2-' + (Date.now() % 100000), template: 'basic' },
    })
    .catch((e: any) => e?.response)
  let pid = ''
  if (cr) {
    const st = cr.status()
    console.log('NEWSTATUS', st, (await cr.text().catch(() => '')).slice(0, 120))
    if (st === 200) {
      try { pid = (await cr.json()).project_id || '' } catch { /* noop */ }
    }
  }
  expect(pid, 'fresh 24-hex pid from 200 {project_id}').toMatch(/^[a-f0-9]{24}$/)
  console.log('CREATE', 'pid=' + pid)

  // editor page → csrf token
  await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(900)
  const html = await page.content()
  const cm =
    html.match(/name="ol-csrfToken" content="([^"]+)"/) ||
    html.match(/name="_csrf"[^>]*value="([^"]+)"/)
  const csrf = cm ? { 'x-csrf-token': cm![1] } : {}
  const rid = mongoEval(
    'const p=db.getCollection("projects").findOne({_id:ObjectId("' + pid + '")});const rf=p&&p.rootFolder?(Array.isArray(p.rootFolder)?p.rootFolder[0]:p.rootFolder):null;print(rf?String(rf._id):"MISSING")'
  )
  console.log('ROOT', rid, 'csrf=' + (cm ? 'yes' : 'NO'))
  expect(rid, 'root folder exists').not.toBe('MISSING')

  const up = (fname: string, buf: string) =>
    context.request.post('/Project/' + pid + '/upload?folder_id=' + rid, {
      headers: { ...csrf, accept: 'application/json' },
      multipart: {
        name: fname,
        qqfile: { name: fname, mimeType: 'text/plain', buffer: Buffer.from(buf) },
      },
    })

  const r1 = await up('s2probe.txt', 'hello-s2\nline2\n')
  console.log('UP1', r1.status(), (await r1.text().catch(() => '')).slice(0, 160))
  expect([200, 201, 302]).toContain(r1.status())

  const r2 = await up('s2probe.txt', 'replaced-content-77\n')
  console.log('UP2', r2.status(), (await r2.text().catch(() => '')).slice(0, 160))
  expect([200, 201, 302]).toContain(r2.status())

  // doc download (docdl → docstore-direct now)
  const docId = mongoEval(
    'const p=db.getCollection("projects").findOne({_id:ObjectId("' + pid + '")});const rf=Array.isArray(p.rootFolder)?p.rootFolder[0]:p.rootFolder;const d=(rf&&rf.docs||[]).find(x=>x.name==="s2probe.txt");print(d?String(d._id):"NONE")'
  )
  console.log('DOC', docId)
  expect(docId).not.toBe('NONE')
  const dl = await context.request.get('/Project/' + pid + '/doc/' + docId + '/download')
  expect(dl.status()).toBe(200)
  expect(await dl.text()).toContain('replaced-content-77')
  console.log('ALL UP-GREEN')
})

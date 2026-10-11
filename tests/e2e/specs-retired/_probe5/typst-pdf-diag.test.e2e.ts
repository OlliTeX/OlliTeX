/**
 * Diagnostics (throwaway): drive the typst compile over the WEB API with a
 * browser session; then hit the synctex proxy routes (the uncommitted routes
 * under test) and the PDF download route. Pure API; no editor UI.
 */
import { test } from '@playwright/test'
import { login } from '../../helpers/auth'
import { USER } from '../../fixtures/credentials'

test('diag: web compile + synctex + pdf', async ({ page, context }) => {
  await login(page, USER)
  await page.goto('/hub')
  await page.waitForLoadState('domcontentloaded')

  // newest 'T2 basic' project id
  const listResp = await page.request.get('/hub/api/projects?type=owned')
    .catch(async () => page.request.get('/projects?type=owned'))
  console.log('list status:', listResp && listResp.status())
  // fallback: read the page DOM for the newest T2 basic project link
  // (the hub projects rail renders /editor/<id> links or a list of names)
  let pid = ''
  const resp = await page.request.get('/hub')
  const html = await resp.text()
  const m = [
    ...html.matchAll(/\/editor\/([a-f0-9]{24})/g),
    ...html.matchAll(/"project_id"\s*:\s*"([a-f0-9]{24})"/g),
  ].map(x => x[1])
  console.log('candidate project ids in /hub html:', [...new Set(m)].slice(0, 5).join(', '))
  if (m.length) pid = m[0]
  if (!pid) {
    const proj = page.locator('a:has-text("T2 basic")').first()
    const href = await proj.getAttribute('href').catch(() => null)
    console.log('T2 basic link href:', href)
    const mm = href && href.match(/([a-f0-9]{24})/)
    if (mm) pid = mm[1]
  }
  console.log('PID =', pid)
  if (!pid) throw new Error('no pid')

  const csrf = await page.evaluate(() => {
    const el = document.querySelector('meta[name="ol-csrfToken"]')
    return el ? el.getAttribute('content') : ''
  })
  console.log('csrf len:', csrf.length)

  // fresh compile over the web
  const cresp = await page.request.post(`/project/${pid}/compile`, {
    headers: { 'X-CSRF-Token': csrf, 'Content-Type': 'application/json' },
    data: JSON.stringify({ options: {} }),
  })
  const cbody = await cresp.text()
  console.log('COMPILE status:', cresp.status())
  console.log('COMPILE body head:', cbody.slice(0, 700))

  // extract buildId + output pdf url
  let j: any = {}
  try { j = JSON.parse(cbody) } catch {}
  let bid = ''
  const s = JSON.stringify(j)
  const bm = s.match(/"build":"([^"]+)"/) || s.match(/"buildId":\s*"([^"]+)"/)
  if (bm) bid = bm[1]
  console.log('buildId:', bid)
  const out = j.outputFiles || j.OutputFiles || []
  const pdfUrl = (out.find((o: any) => /output\.pdf/.test(o.url || o.URL || '')) || {}).url
    || (out.find((o: any) => /output\.pdf/.test(o.url || '')) || {}).url

  if (pdfUrl) {
    const fres = await page.request.get(pdfUrl)
    const fbuf = Buffer.from(await fres.body())
    console.log('PDF FETCH status:', fres.status(), 'type:', fres.headers()['content-type'], 'size:', fbuf.length)
    console.log('PDF HEAD:', JSON.stringify(fbuf.subarray(0, 10).toString('latin1')))
  } else {
    console.log('no output.pdf in response; trying canonical url')
    const fres = await page.request.get(`/project/${pid}/output/generated-files/${bid}/output.pdf`)
    const fbuf = Buffer.from(await fres.body())
    console.log('PDF(canonical) status:', fres.status(), 'type:', fres.headers()['content-type'], 'size:', fbuf.length,
      'head:', JSON.stringify(fbuf.subarray(0, 10).toString('latin1')))
  }

  // SYNCTEX (crown jewel, via the NEW uncommitted web routes)
  if (bid) {
    const sc = await page.request.get(`/project/${pid}/sync/code?file=main.typ&line=1&column=1&buildId=${bid}`)
    console.log('SYNC/CODE status:', sc.status(), 'body:', (await sc.text()).slice(0, 300))
    const sp = await page.request.get(`/project/${pid}/sync/pdf?page=1&h=100&v=100&buildId=${bid}`)
    console.log('SYNC/PDF status:', sp.status(), 'body:', (await sp.text()).slice(0, 300))
    const wc = await page.request.get(`/project/${pid}/wordcount?file=main.typ`)
    console.log('WORDCOUNT status:', wc.status(), 'body:', (await wc.text()).slice(0, 200))
  }
}, 180_000)

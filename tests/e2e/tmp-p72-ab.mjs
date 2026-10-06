const NODE = 'http://127.0.0.1:4000'
const GO = 'http://127.0.0.1:4010'
const { chromium } = await import('playwright')
const b = await chromium.launch({ headless: true })

async function sess(base) {
  const ctx = await b.newContext()
  const page = await ctx.newPage()
  await page.goto(base + '/login', { waitUntil: 'load' })
  await page.fill('#email', 'e2e-admin@e2e.test')
  await page.fill('#password', 'Ol-Fixture-9x7K')
  await page.click('button[type=submit]')
  await page.waitForURL(/hub|project/, { timeout: 30000 })
  const csrf = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  return { ctx, page, csrf }
}

const N = await sess(NODE)
const G = await sess(GO)

// project + doc via Node (oracle)
const rn = await N.ctx.request.post(NODE + '/project/new', {
  headers: { 'x-csrf-token': N.csrf, 'content-type': 'application/json' },
  data: { projectName: 'ab-' + Date.now() },
})
const jn = await rn.json().catch(() => ({}))
const pid = jn.project_id || jn._id
let docid = null
try {
  const er = await N.ctx.request.get(NODE + '/project/' + pid + '/entities', { headers: { accept: 'application/json' } })
  const ej = await er.json().catch(() => [])
  const doc = (Array.isArray(ej) ? ej : []).find((x) => x.type === 'doc')
  docid = doc ? doc._id : null
} catch (e) {}
const P = pid || '0'.repeat(24)
const D = docid || '0'.repeat(24)
console.log('node project/new:', rn.status(), pid, 'doc:', docid)

const CANDS = [
  ['POST', '/api/project'],
  ['GET', '/tag'],
  ['GET', '/project/' + P],
  ['GET', '/project/' + P + '/doc/' + D],
  ['GET', '/hub/'],
  ['GET', '/admin'],
  ['GET', '/home'],
  ['GET', '/admin-hub'],
  ['GET', '/user/github-sync/status'],
  ['GET', '/project/' + P + '/github-sync/state'],
  ['GET', '/user/git-servers'],
  ['GET', '/user/list'],
  ['GET', '/user/llm-providers'],
  ['GET', '/project/' + P + '/track_changes'],
  ['GET', '/project/' + P + '/threads'],
  ['GET', '/project/' + P + '/sharing-updates'],
  ['GET', '/user/contacts'],
  ['GET', '/user/mysettings'],
  ['GET', '/launchpad'],
  ['GET', '/library/references'],
  ['GET', '/planned_maintenance'],
  ['GET', '/template/' + D + '/preview'],
  ['GET', '/project/' + P + '/users/' + D],
  ['GET', '/project/' + P + '/webdav/state'],
  ['GET', '/project/' + P + '/entities'],
]
const rows = []
for (const [m, p] of CANDS) {
  let n = '-', g = '-'
  try {
    const opts = { method: m, headers: { accept: 'application/json' } }
    if (m !== 'GET') {
      opts.headers['x-csrf-token'] = N.csrf
      G.csrf = N.csrf // token is session-bound; for A/B status purposes only
    }
    const rn2 = await N.ctx.request.fetch(NODE + p, opts).catch(() => null)
    n = rn2 ? rn2.status() : 'EXC'
    const gn2 = await G.ctx.request.fetch(GO + p, { method: m, headers: { accept: 'application/json' } }).catch(() => null)
    g = gn2 ? gn2.status() : 'EXC'
  } catch (e) {}
  rows.push([m, p, n, g])
}
console.log('\nSTATUS A/B (NODE=4000 vs GO=4010):')
for (const [m, p, n, g] of rows) {
  const flag = n === g ? '  same' : '  GAP!'
  console.log(flag, m.padEnd(5), (n + '').padEnd(4), (g + '').padEnd(4), p.replace(P, 'PID').replace(D, 'DID').slice(0, 40))
}
await b.close()

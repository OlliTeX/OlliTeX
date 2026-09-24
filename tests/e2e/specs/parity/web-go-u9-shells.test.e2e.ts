/**
 * P7 U9 parity gate — hub/admin/home shell pages (WEB_GO_PLAN.md U9):
 *   /hub (+ /hub/, /HUB, /HUB/)            → 200 Mantine hub (path-aware
 *                                             currentUrl + alternate link)
 *   /admin (+ /admin/, /Admin, /ADMIN)     → 200 admin shell (LLM tab on
 *                                             in this stack: LLM_ENABLED=true;
 *                                             system-messages empty; open
 *                                             sockets empty <ul></ul>)
 *   /hub/admin, /hub/workspace (+ variants)→ 302 /hub
 *   / (root)                               → 302 /hub logged-in; anon gate
 *   /home                                  → 302 /login (home pug absent in
 *                                             this build; Node HomeController.
 *                                             home CE branch)
 *   non-admin /admin                       → 302 /restricted?from=%2Fadmin
 *   anon /admin|/home|/hub|/               → 401 "Unauthorized" (accept-json)
 *                                             302 /login (accept-html)
 *   /login, /register, /logout, /user/settings
 *                                          → 200; navbar showSignUpLink FLIPPED
 *                                             to false (SAML IdP enabled →
 *                                             hasFeature('registration-page')
 *                                             false — the 2026-09-19 TRUE pins
 *                                             were stale), /login logged-in
 *                                             navbar gains sessionUser
 *
 * Dual-port in-container battery (Node 127.0.0.1:4000 vs Go shadow :4010),
 * Node==Go==Node 3 legs — the U1/U2/U8 gate idiom. Read-only battery.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const BATTERY = `${process.cwd()}/specs/parity/u9-shells-matrix.cjs`
const NODE = 'http://127.0.0.1:4000'
const GO = 'http://127.0.0.1:4010'

function dexeStrict(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 256 * 1024 * 1024 })
}

function probe(url: string): string {
  let out = ''
  try {
    out = execFileSync('docker', ['exec', overleafC, 'sh', '-c', `curl -s -m 3 -o /dev/null -w "%{http_code}" ${url}`], {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    })
  } catch (e: any) {
    out = e && e.stdout ? e.stdout.toString() : ''
  }
  return (out || '').trim().split('\n')[0]
}

async function waitUp(): Promise<void> {
  for (let i = 0; i < 60; i++) {
    if (probe(`${NODE}/status`) === '200' && probe(`${GO}/status`) === '200') return
    await new Promise((r) => setTimeout(r, 500))
  }
  throw new Error(`services not up (node=${probe(NODE + '/status')} go=${probe(GO + '/status')})`)
}

function flushRateLimiters(): void {
  execFileSync(
    'docker',
    ['exec', 'ol-e2e-redis-1', "sh", "-c", `for k in $(redis-cli --scan --pattern "rate-limit:overleaf-login:*"); do redis-cli DEL \"$k\"; done`],
    { encoding: 'utf8' },
  )
}

const ktag = (t: string): string => t.replace(/\s+/g, ' ')
function runBattery(base: string): any[] {
  flushRateLimiters()
  const raw = dexeStrict(overleafC, `node /tmp/u9-matrix.cjs ${base}`)
  const start = raw.indexOf('BATTERY-JSON:')
  if (start < 0) throw new Error(`battery against ${base} produced no output: ${raw.slice(0, 300)}`)
  const recs = JSON.parse(raw.slice(start + 'BATTERY-JSON:'.length))
  for (const r of recs) r.tag = ktag(r.tag as string)
  return recs
}

// Wire normalisation (per-leg token differences are INTERNAL — the csrf
// token derives from each engine's view of the session secret; the U1 gate
// established value-normalisation for html csrf inputs + the ol-csrfToken
// meta. Path-aware fields (alternate link, currentUrl, sid) are request- or
// session-specific → tokenised. Everything else must be byte-identical.
const norm = (s: string): string =>
  s
    .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
    .replace(/name="_csrf"[^>]*value="[^"]*"/g, 'name="_csrf" value="CSRF"')
    .replace(/overleaf\.sid=s%3A[^;\s"]+/g, 'sid=S')
    .replace(/(<link rel="alternate" href=")[^"]*(" hreflang="en")/g, '$1ALT$2')
    .replace(/(&quot;currentUrl&quot;:&quot;)[^&]*(&quot;)/g, '$1P$2')

test.describe('U9 hub/admin/home shell parity (Node vs Go)', () => {
  test('3-leg matrix: Node == Go == Node', async () => {
    test.setTimeout(600_000)
    await waitUp()
    execFileSync('docker', ['cp', BATTERY, `${overleafC}:/tmp/u9-matrix.cjs`], { timeout: 30000 })

    const legs = [null, null, null] as any[]
    legs[0] = runBattery(NODE)
    legs[1] = runBattery(GO)
    legs[2] = runBattery(NODE)
    const N = legs[0].length
    expect(N).toBe(40)
    expect(legs[1].length).toBe(N)
    expect(legs[2].length).toBe(N)

    // ---- Node oracle pins (leg 0 — the authoritative stack) ----
    const byTag = (recs: any[]) => Object.fromEntries(recs.map((r: any) => [r.tag, r]))
    const o = byTag(legs[0])
    const wantTags = new Set(legs[0].map((r: any) => r.tag))
    for (const r of legs[1]) expect(wantTags.has(r.tag), `leg1 has tag from leg0: ${r.tag}`).toBe(true)
    for (const r of legs[2]) expect(wantTags.has(r.tag), `leg2 has tag from leg0: ${r.tag}`).toBe(true)

    // hub family: 200, html, etag present; no-variant case pins stay in the
    // cross-leg byte compare below.
    for (const t of ['admin GET /hub', 'admin GET /hub/', 'admin GET /HUB', 'admin GET /HUB/']) {
      const r = o[t]
      expect(r.status, t).toBe(200)
      expect(r.ct.startsWith('text/html; charset=utf-8'), t).toBe(true)
      expect(r.etaglen, t).toBeGreaterThan(0)
      expect(norm(r.body), t).toContain('hub')
    }
    // admin shell: 200 + LLM tab present (LLM_ENABLED=true in this stack)
    for (const t of ['admin GET /admin', 'admin GET /admin/', 'admin GET /Admin', 'admin GET /ADMIN']) {
      const r = o[t]
      expect(r.status, t).toBe(200)
      const b = norm(r.body)
      expect(b, t).toContain('<meta name="ol-adminOverallTheme" content="light-">')
      expect(b, t).toContain('>LLM Configuration</a></li>')
      expect(b, t).toContain('<iframe src="/admin/llm/settings"')
      expect(b, t).toContain('<div class="row-spaced"><ul></ul></div>') // open sockets empty
      expect(b, t).not.toContain('privileges-matrix') // saas off
      expect(r.etaglen, t).toBeGreaterThan(0)
    }
    // aliases → 302 /hub (plain express redirect body)
    for (const t of [
      'admin GET /hub/admin',
      'admin GET /hub/admin/',
      'admin GET /HUB/ADMIN',
      'admin GET /hub/workspace',
      'admin GET /hub/workspace/',
    ]) {
      const r = o[t]
      expect({ status: r.status, loc: r.loc }, t).toEqual({ status: 302, loc: '/hub' })
      expect(r.body, t).toBe('Found. Redirecting to /hub')
    }
    // root → 302 /hub; /home → 302 /login (both users, all three variants)
    for (const t of ['admin GET /', 'user GET /']) {
      expect({ status: o[t].status, loc: o[t].loc }, t).toEqual({ status: 302, loc: '/hub' })
    }
    for (const who of ['admin', 'user']) {
      for (const p of ['/home', '/Home', '/home/']) {
        const r = o[`${who} GET ${p}`]
        expect({ status: r.status, loc: r.loc }, `${who} ${p}`).toEqual({ status: 302, loc: '/login' })
        expect(r.body, `${who} ${p}`).toBe('Found. Redirecting to /login')
      }
    }
    // non-admin /admin variants → restricted bounce (path-encoded pathname —
    // case + trailing slash PRESERVED, Node oracle 2026-09-22)
    for (const [t, from] of [
      ['user GET /admin', '/restricted?from=%2Fadmin'],
      ['user GET /Admin', '/restricted?from=%2FAdmin'],
      ['user GET /admin/', '/restricted?from=%2Fadmin%2F'],
    ] as const) {
      const r = o[t]
      expect({ status: r.status, loc: r.loc }, t).toEqual({ status: 302, loc: from })
      expect(r.body, t).toBe('Found. Redirecting to ' + from)
    }
    // restricted page (user session): 200 HTML, session-filled metas
    {
      const r = o['user GET /restricted?from=%2Fadmin']
      expect(r.status).toBe(200)
      const b = norm(r.body)
      expect(b).toContain('ol-usersEmail" content="e2e-user@e2e.test"')
      expect(b).toContain('ol-user_id" content="6aa4b8b573ef0e5094f4cbc0"')
    }
    // login (logged-in): navbar sessionUser filled; anon: absent
    {
      const b = norm(o['user GET /login (logged-in)'].body)
      expect(b).toContain('&quot;sessionUser&quot;:{&quot;email&quot;:&quot;e2e-user@e2e.test&quot;}')
      expect(b).toContain('ol-usersEmail" content="e2e-user@e2e.test"')
      expect(o['user GET /login (logged-in)'].body).toContain('showSignUpLink&quot;:false')
    }
    {
      const b = o['anon GET /login'].body
      expect(b).not.toContain('sessionUser')
      expect(b).toContain('showSignUpLink&quot;:false')
      expect(b).toContain('<meta name="ol-usersEmail" content="">')
      expect(b).toContain('<meta name="ol-user_id">')
    }
    {
      const b = o['anon GET /register'].body
      expect(b).toContain('showSignUpLink&quot;:false')
    }
    // settings + logout: exposed-settings menu flag by session role
    expect(norm(o['admin GET /user/settings'].body)).toContain('&quot;canManageTemplatesMenu&quot;:true')
    expect(norm(o['admin GET /logout'].body)).toContain('&quot;canManageTemplatesMenu&quot;:true')
    expect(norm(o['user GET /logout'].body)).toContain('&quot;canManageTemplatesMenu&quot;:false')
    // anonymous gate chains
    for (const t of [
      'anon GET /admin (accept-json)',
      'anon GET /home (accept-json)',
      'anon GET /hub (accept-json)',
      'anon GET / (accept-json)',
      'anon GET /restricted (accept-json)',
    ]) {
      expect({ status: o[t].status, body: o[t].body }, t).toEqual({ status: 401, body: 'Unauthorized' })
    }
    for (const t of [
      'anon GET /admin (accept-html)',
      'anon GET /home (accept-html)',
      'anon GET / (accept-html)',
      'anon GET /hub (accept-html)',
    ]) {
      const r = o[t]
      expect({ status: r.status, loc: r.loc }, t).toEqual({ status: 302, loc: '/login' })
      // express renders the redirect body per Accept (html → <p> wrapper)
      expect(r.body, t).toBe('<p>Found. Redirecting to /login</p>')
    }

    // ---- cross-leg parity: Node == Go == Node (wire-level) ----
    for (let i = 0; i < N; i++) {
      const a = legs[0][i]
      const b = legs[1][i]
      const c = legs[2][i]
      for (const [leg, r] of [['go', b], ['node2', c] as const]) {
        expect(r.tag, `${a.tag} [${leg} tag]`).toBe(a.tag)
        expect(r.status, `${a.tag} [${leg} status]`).toBe(a.status)
        expect(r.loc, `${a.tag} [${leg} location]`).toBe(a.loc)
        expect(r.ct, `${a.tag} [${leg} content-type]`).toBe(a.ct)
        if (a.status === 200) {
          expect(r.etaglen, `${a.tag} [${leg} etag-len]`).toBe(a.etaglen)
          expect(norm(r.body), `${a.tag} [${leg} body]`).toBe(norm(a.body))
        } else {
          expect(r.body, `${a.tag} [${leg} body]`).toBe(a.body)
        }
      }
    }
  })
})

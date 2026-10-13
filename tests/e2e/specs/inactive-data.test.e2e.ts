/**
 * e2e-inactive-data — b63post-2 InactiveData live acceptance (TODO-deb4b4ba).
 *
 * The Node oracle (InactiveData feature, router.mjs:995/1000) is converted in
 * Go (projectlist internalops.go/docapi.go) as api-profile routes:
 *
 *   POST /internal/project/:project_id/deactivate      (per-project)
 *   POST /internal/deactivateOldProjects               (bulk; image cron
 *                                                       driver, defaults
 *                                                       limit=10 days=360)
 *
 * Both are Route.APIOnly: the WEB profile skips them (Node parity — Node's
 * web stack never wires these; web tail observed live 2026-11-16: anon GET →
 * 302 /login, POST no-csrf → 403 "Forbidden"). The API profile (Go web
 * binary, ENABLED_SERVICES=api, listens 0.0.0.0:3000) serves them behind
 * WEB_API_USER/WEB_API_PASSWORD basic-auth (APIBasicGate401: unauth/wrong →
 * 401 "Unauthorized" 12B + WWW-Authenticate: OverleafLogin).
 *
 * The Go binary runs DUAL profiles in this world (discovered live 2026-11-16):
 * web profile (PID 83, :80 + :4000) AND a STANDING api-profile process
 * (PID 90, 0.0.0.0:3000 → 172.20.0.2:3000 on the bridge, host-reachable).
 * (An earlier "3000 closed" read was a busybox-wget exit-code trap: non-2xx →
 * nonzero exit.) Probes run INSIDE the container (loopback 127.0.0.1:3000)
 * so the basic-auth secrets never leave the container env.
 *
 * Contracts (unit-pinned in deactivateold_test.go + pinned live Node :3000
 * 2026-09-24 in code comments):
 *  - per-project: unauth/wrong → 401; bad-oid → 404 JSON VA
 *    ("params.project_id"); valid oid (ghost OR real) → 200 text "OK"
 *    (+active:false side effect for real projects — verified in mongo)
 *  - bulk: unauth → 401; success → 200 JSON array of {_id,lastOpened} found
 *    projects; zod logOnly semantics — bad body fields NEVER 400 (coerce or
 *    default)
 *
 * Deactivation semantics (D41 slice-2, this build): active:false in projects;
 * docstore archive best-effort; DU retired (Node's flush+delete removed).

 * OPEN item (web-tail client quirk, non-blocking): anon GET on the unmounted
 * path returns the login surface in TWO client-dependent forms (curl-class →
 * 302 /login; undici/Playwright → 200 with the login HTML). Same contract
 * (anon → login surface), different negotiation; mechanism not isolated.
 * Test 10 pins both forms; the no-mount pin is the stable POST→403 tail.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'child_process'
import * as pathmod from 'path'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { mongoEval } from '../helpers/host'

const WEB = process.env.E2E_WEB_CONTAINER || 'ol-e2e-overleaf-1'
const API = 'http://127.0.0.1:3000'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }

test.describe.configure({ mode: 'serial' })

function cexec(sh: string): string {
  try {
    return execFileSync('docker', ['exec', WEB, 'sh', '-c', sh], {
      encoding: 'utf8', timeout: 60_000, stdio: ['ignore', 'pipe', 'pipe'],
    })
  } catch (e: any) {
    return 'EXEC_FAIL::' + String(e.stdout || '') + String(e.stderr || '')
  }
}

interface Probe { status: number; body: string; raw: string }

// Probe INSIDE the container against the standing api listener (127.0.0.1:3000).
// Container node runs a static script (busybox wget drops 4xx bodies); secrets
// never leave the container env (the script reads them from process.env in the
// container namespace). Invocation contract: spec runs with cwd = tests/e2e.
let probeScriptInstalled = false
function ensureProbeScript(): void {
  if (probeScriptInstalled) return
  const local = pathmod.join(process.cwd(), 'scripts', 'inactive-probe.cjs')
  execFileSync('docker', ['cp', local, WEB + ':/tmp/idprobe.cjs'], { timeout: 30_000, stdio: 'pipe' })
  probeScriptInstalled = true
}


// The bulk find runs with readpref SECONDARY (Node parity, ReadPreference
// from InactiveProjectManager.findInactiveProjects). If the world's replica
// set has no healthy SECONDARY (e2e world = single-member set, discovered
// 2026-11-16), the find FAILS and the faithful parity response is 500
// "Internal Server Error" (Node: findInactiveProjects throw → 500) — for
// BOTH Node and Go (same preference, same throw). Branch on the actual
// world state so the pin is deterministic either way.
function mongoHasHealthySecondary(): boolean {
  const out = mongoEval(
    `var n = 0; try { n = rs.status().members.filter(function(m){return m.stateStr==="SECONDARY" && m.health===1}).length } catch(e) {} print(n >= 1 ? "YES" : "NO")`
  )
  const t = out.trim()
  if (/^YES/.test(t)) return true
  if (/^NO\b/.test(t)) return false
  throw new Error('mongoHasHealthySecondary: ambiguous mongo answer: ' + JSON.stringify(t).slice(0, 120))
}

function probe(path: string, opts: { auth?: 'none' | 'wrong' | 'right'; post?: boolean; body?: string } = {}): Probe {
  ensureProbeScript()
  // body is our own JSON (no single quotes) — safe inside sh single quotes
  const sh = `U='${API}${path}' A='${opts.auth || 'none'}' M='${opts.post ? 'POST' : 'GET'}' B='${(opts.body || '').replace(/'/g, '')}' node /tmp/idprobe.cjs`
  let out = ''
  try {
    out = execFileSync('docker', ['exec', WEB, 'sh', '-c', sh], { encoding: 'utf8', timeout: 60_000, stdio: ['ignore', 'pipe', 'pipe'] })
  } catch (e: any) {
    out = 'EXEC_FAIL::' + String(e.stdout || '') + String(e.stderr || '')
  }
  const status = Number((out.match(/PROBE_STATUS=(\d+)/) || [])[1] || 0)
  const bs = out.indexOf('PROBE_BODYSTART')
  const bodyOut = bs >= 0 ? out.slice(bs + 15).trim() : ''
  return { status, body: bodyOut, raw: out }
}

test.describe('InactiveData b63post-2 (api profile, standing listener on :3000)', () => {
test('1: unauth POST per-project → 401 Unauthorized + auth-challenge header (APIBasicGate401 wire)', () => {
    const p = probe('/internal/project/aaaaaaaaaaaaaaaaaaaaaaaa/deactivate', { post: true })
    expect(p.status).toBe(401)
    expect(p.raw, 'challenge header').toContain('PROBE_AUTH=OverleafLogin')
    expect(p.body, 'body').toBe('Unauthorized')
  })

  test('2: wrong basic cred → 401 (constant-time compare, still 401)', () => {
    const p = probe('/internal/project/aaaaaaaaaaaaaaaaaaaaaaaa/deactivate', { post: true, auth: 'wrong' })
    expect(p.status).toBe(401)
    expect(p.body).toBe('Unauthorized')
  })

  test('3: valid cred + bad project_id → 404 JSON VA "params.project_id"', () => {
    const p = probe('/internal/project/notanoid/deactivate', { post: true, auth: 'right' })
    expect(p.status).toBe(404)
    expect(p.body).toContain('Validation error: Invalid Mongo ObjectId')
    expect(p.body).toContain('params.project_id')
  })

  test('4: valid cred + ghost oid → 200 "OK" (no-op)', () => {
    const p = probe('/internal/project/aaaaaaaaaaaaaaaaaaaaaaaa/deactivate', { post: true, auth: 'right' })
    expect(p.status).toBe(200)
    expect(p.body).toBe('OK')
  })

  test('5: REAL project — create → deactivate → mongo active:false (core acceptance)', async ({ page }) => {
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const pid = await createBlankProject(page)
    expect(pid, 'pid hex24').toMatch(/^[0-9a-f]{24}$/)
    const before = mongoEval(`print(db.projects.findOne({_id:ObjectId("${pid}")}).active)`)
    expect(before, 'created active:true').toMatch(/true/)
    const p = probe(`/internal/project/${pid}/deactivate`, { post: true, auth: 'right' })
    expect(p.status).toBe(200)
    expect(p.body).toBe('OK')
    const after = mongoEval(`print(db.projects.findOne({_id:ObjectId("${pid}")}).active)`)
    expect(after, 'deactivated active:false (persisted)').toMatch(/false/)
  })

  test('6: bulk route unauth → 401 (same gate)', () => {
    const p = probe('/internal/deactivateOldProjects', { post: true })
    expect(p.status).toBe(401)
    expect(p.body).toBe('Unauthorized')
  })

  test('7: bulk valid cred, empty body → 200 array (secondary up) | 500 ISE (no healthy secondary)', () => {
    const p = probe('/internal/deactivateOldProjects', { post: true, auth: 'right' })
    const sec = mongoHasHealthySecondary()
    if (sec) {
      console.log('BULK-BRANCH: healthy secondary → expect 200 array')
      expect(p.status).toBe(200)
      expect(Array.isArray(JSON.parse(p.body || '[]')), 'array body').toBeTruthy()
    } else {
      console.log('BULK-BRANCH: no healthy secondary (single-member rs) → parity 500 ISE')
      expect(p.status, p.body.slice(0, 120)).toBe(500)
      expect(p.body).toBe('Internal Server Error')
    }
  })

  test('8: bulk bad body fields NEVER 400 (zod logOnly/coerce pin)', () => {
    const p = probe('/internal/deactivateOldProjects', {
      post: true, auth: 'right',
      body: JSON.stringify({ numberOfProjectsToArchive: 'junk', ageOfProjects: 'junk' }),
    })
    // logOnly semantics: bad fields fall back to defaults — validation never
    // 400s. The OUTCOME is then the same secondary-dependent branch as test 7
    // (200 array when a secondary answers, 500 ISE when it does not) — but it
    // must not be a 400/4xx rejection.
    const sec = mongoHasHealthySecondary()
    expect(p.status, p.body.slice(0, 120)).not.toBe(400)
    expect(p.status, p.body.slice(0, 120)).not.toBe(422)
    if (sec) {
      expect(p.status).toBe(200)
      expect(Array.isArray(JSON.parse(p.body || '[]')), 'array body').toBeTruthy()
    } else {
      expect(p.status).toBe(500)
    }
  })

  test('9: bulk numeric-string body accepted (z.coerce parity), never 4xx', () => {
    const p = probe('/internal/deactivateOldProjects', {
      post: true, auth: 'right',
      body: JSON.stringify({ numberOfProjectsToArchive: '5', ageOfProjects: '45' }),
    })
    const sec = mongoHasHealthySecondary()
    if (sec) {
      expect(p.status).toBe(200)
      expect(JSON.stringify(JSON.parse(p.body || '[]'))).toMatch(/^\[[\s\S]*\]$/)
    } else {
      expect(p.status, p.body.slice(0, 120)).toBe(500)
    }
    expect(p.status).not.toBe(400)
  })

  test('10: WEB-profile tail pins — APIOnly routes skipped on web (re-arm)', async ({ page }) => {
    const base = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
    // Anon web GET: the route is NOT mounted on the web profile (Node parity) —
    // the anonymous tail serves the LOGIN SURFACE. Two live-verified forms
    // (client-negotiation quirk, mechanism not yet isolated — OPEN item below):
    //   302 -> /login          (curl-class clients, incl. host curl)
    //   200 + /login HTML      (undici / Playwright request context)
    // BOTH = "anon routed to the login surface". The re-arm signal is the
    // route's OWN wire (401 challenge / 200 'OK' / 404 JSON VA), which would
    // replace this pair if the route were ever mounted on the web profile.
    const g = await page.request.get(base + '/internal/project/aaaaaaaaaaaaaaaaaaaaaaaa/deactivate')
    expect([302, 200], 'anon GET -> login-surface forms').toContain(g.status())
    if (g.status() === 302) {
      expect(g.headers()['location'] || '', '302 -> /login').toContain('/login')
    } else {
      const body = await g.text()
      expect(body, '200 = login page surface').toMatch(/login/i)
    }
    // POST (anon, no CSRF): the csrf tail 403 is client-stable (host-curl and
    // container probes agree) — the strongest no-mount pin.
    const po = await page.request.post(base + '/internal/project/aaaaaaaaaaaaaaaaaaaaaaaa/deactivate')
    expect(po.status(), 'web POST no-csrf -> 403 tail (route NOT mounted on web)').toBe(403)
    const pb = await page.request.post(base + '/internal/deactivateOldProjects', { data: '' })
    expect(pb.status(), 'web bulk POST no-csrf -> 403 tail').toBe(403)
  })

})

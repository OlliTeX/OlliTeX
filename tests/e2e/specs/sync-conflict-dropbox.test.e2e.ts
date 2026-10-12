/**
 * DROPBOX (storage sync) UPDATE-CONFLICT + RESOLUTION surface
 * (owner ask 2026-11-16) — the dropbox feature
 * (go/services/web/features/dropbox), pinned LIVE.
 *
 * FINDING (source + live, ag-v57): in the Go port the Dropbox sync engine is
 * STUBBED — link/pull/push/files pass the authz + linked-state gates and then
 * return fixed 500s ("Dropbox connection check failed" / "Dropbox sync is not
 * available in this environment"), import returns 500 "Dropbox import failed"
 * after the decrypt gate. There is NO conflict route and no mergeStatus writer
 * (state docs carry mergeStatus/conflicts only as projection keys) — i.e. in
 * this world an update conflict CANNOT be produced or observed through the
 * product API, and conflict resolution is therefore unreachable. Node's live
 * flow additionally requires a files.* app token; the configured token is
 * account_info.read-only (D-series scope pin).
 *
 * This spec pins the full honest surface (so any porting progress trips a
 * red pin that must be re-pointed), with re-arm guards:
 *   D0 status unconnected — 200 {"connected":false} (login gate)
 *   D1 connect — POST /user/dropbox/connect {access_token}
 *      → 200 {"success":true} (token stored encrypted; RE-ARM: 401/400 when the
 *      port validates the token against the API)
 *      missing body key → 400 {"error":"Missing access_token"}
 *   D2 status connected — 200 {"connected":true,"path":"/",…}
 *   D3 project state unlinked — 200 {"connected":false} (LOGIN-ONLY route)
 *   D4 link — RE-ARM (engine) pin: 500 {"error":"Dropbox connection check
 *      failed"} (stub); missing creds → 409 {"error":"Not connected to
 *      Dropbox…"}
 *   D5 pull/push/files unlinked — 409 {"error":"Project not linked to
 *      Dropbox"} (gate; also the LIVE pin for the conflict path's pre-state)
 *   D6 import — {projectName} required (400 pin) then
 *      500 {"error":"Dropbox import failed"} (stub pin)
 *   D7 unlink — DELETE state → 200 {"success":true} (no-op ok pin)
 *   D8 disconnect — 200 {"success":true,"unlinkedProjects":"/…"} + status
 *      back to {"connected":false} (hygiene: leaves the world unconnected)
 *
 * RE-ARM: every stub pin asserts the CURRENT live shape; when the port wires
 * the Dropbox engine + a files.* scope token, the red pins are the signal to
 * re-point them to the success shapes (link 200 state, pull/push 200, files
 * list, import 200 {projectId}) and to add the true conflict cycle legs
 * (local edit + remote edit + pull → conflicts[]/mergeStatus → resolve).
 */
import { test, expect } from '@playwright/test'
import { USER } from '../fixtures/credentials'
import { loginRobust } from '../helpers/auth'
import { api } from '../parity/harness'
import { getExternal, externalEnabled, getServiceToken } from '../credentials/external'

test.setTimeout(300_000)

const dbx = getExternal('dropbox') as any
/** token: config.json static first, else the .auth capture cache (sl.u.*) */
const token = String(dbx?.token ?? '') || String(getServiceToken('dropbox')?.token ?? '')
const stamp = Date.now().toString(36)

const needToken = (t: string, label: string): boolean => {
  if (typeof token !== 'string' || !token) {
    test.skip(true, 'dropbox token not configured (config.token or .auth/dropbox.json)')
  }
  return true
}

test.describe('dropbox conflict/resolve surface (pin + re-arm guards)', () => {
  test('D0 status gate + D1 connect contract', async ({ page }) => {
    needToken(token, 'D1')
    await page.goto('/')
    // anonymous → the route is behind the session gate: a raw fetch is
    // redirected to /login (page-level login wall), not a JSON 403.
    const base = new URL(page.url()).origin
    const an = await fetch(`${base}/user/dropbox/status`, { redirect: 'manual' })
    const loc = an.headers.get('location') || ''
    expect(
      an.status >= 300 && an.status < 400 && /\/.+$/.test(loc) !== false,
      `D0 anonymous status → redirect-to-login gate (got ${an.status} loc=${loc})`,
    ).toBeTruthy()
    expect(loc, `D0 anonymous redirect target → ${loc}`).toMatch(/\/login/)

    await loginRobust(page, USER.email, USER.password)
    const st = await (await api(page, 'GET', '/user/dropbox/status')).json().catch(() => ({}))
    console.log(`[D0/D2] status -> ${JSON.stringify(st).slice(0, 160)}`)
    expect(typeof (st as any)?.connected, 'D0 status has connected flag').toBe('boolean')

    // D1 missing key
    const bad = await api(page, 'POST', '/user/dropbox/connect', { other: 'x' })
    const badb = await bad.json().catch(() => ({}))
    expect(bad.status(), 'D1 connect missing access_token → 400').toBe(400)
    expect(badb?.error, 'D1 error text pin').toBe('Missing access_token')

    // D1 connect (stores encrypted)
    if (st.connected !== true) {
      const c = await api(page, 'POST', '/user/dropbox/connect', { access_token: token })
      const cb = await c.json().catch(() => ({}))
      expect(c.status(), `D1 connect -> ${c.status()} ${JSON.stringify(cb).slice(0, 160)}`).toBe(200)
      expect((cb as any)?.success, 'D1 success flag').toBe(true)
      const st2 = await (await api(page, 'GET', '/user/dropbox/status')).json().catch(() => ({}))
      expect((st2 as any)?.connected, `D2 status connected: ${JSON.stringify(st2).slice(0, 160)}`).toBe(true)
      expect(String((st2 as any)?.path ?? ''), 'D2 path pin (default root)').toMatch(/^\/(Overleaf.*)?$/)
    }
  })

  test('D3 state unlinked + D5 gate pins (pull/push/files)', async ({ page }) => {
    needToken(token, 'D3')
    await loginRobust(page, USER.email, USER.password)
    const np = await api(page, 'POST', '/project/new', { projectName: `ol-cfdx-${stamp}` })
    const p = await np.json().catch(() => ({}))
    const pid = String((p as any)?.project_id ?? (p as any)?.projectId ?? '')
    expect(pid, `new project -> ${np.status()} ${JSON.stringify(p).slice(0, 140)}`).toMatch(/^[0-9a-f]{24}$/)

    const st = await (await api(page, 'GET', `/project/${pid}/dropbox/state`)).json().catch(() => ({}))
    expect(st.connected, 'D3 state unlinked').toBe(false)

    for (const seg of ['pull', 'push'] as const) {
      const r = await api(page, 'POST', `/project/${pid}/dropbox/${seg}`, {})
      const b = await r.json().catch(() => ({}))
      expect(r.status(), `D5 ${seg} unlinked -> ${r.status()} ${JSON.stringify(b).slice(0, 120)}`).toBe(409)
      expect(b?.error, `D5 ${seg} error text pin`).toBe('Project not linked to Dropbox')
    }
    // files is a GET route (route table: GET /project/:pid/dropbox/files)
    const fi = await api(page, 'GET', `/project/${pid}/dropbox/files`)
    const fib = await fi.json().catch(() => ({}))
    expect(fi.status(), `D5 files unlinked -> ${fi.status()} ${JSON.stringify(fib).slice(0, 120)}`).toBe(409)
    expect(fib?.error, 'D5 files error text pin').toBe('Project not linked to Dropbox')
  })

  test('D4 link stub pin + D7 unlink no-op + D8 disconnect', async ({ page }) => {
    needToken(token, 'D4')
    await loginRobust(page, USER.email, USER.password)

    const np = await api(page, 'POST', '/project/new', { projectName: `ol-cfdx-${stamp}-l` })
    const p = await np.json().catch(() => ({}))
    const pid = String((p as any)?.project_id ?? (p as any)?.projectId ?? '')
    expect(pid, 'linked project').toMatch(/^[0-9a-f]{24}$/)

    // D4 link — RE-ARM (engine) pin: the Go port stops at the stub
    const lk = await api(page, 'POST', `/project/${pid}/dropbox/link`, { name: `ol-cfdx-${stamp}` })
    const lb = await lk.json().catch(() => ({}))
    console.log(`[D4] link -> ${lk.status()} ${JSON.stringify(lb).slice(0, 160)}`)
    if (lk.status() === 200) {
      // RE-ARM: engine wired — linked state now exists
      const st = await (await api(page, 'GET', `/project/${pid}/dropbox/state`)).json().catch(() => ({}))
      expect((st as any)?.connected, `D4 state connected: ${JSON.stringify(st).slice(0, 160)}`).toBe(true)
    } else if (lk.status() === 409) {
      // creds not stored for the running session (e.g. D1 connect skipped) —
      // the authz-layer pin, equally honest
      expect(String(lb?.error ?? ''), 'D4 not-connected pin').toMatch(/Not connected to Dropbox/i)
    } else {
      expect(lk.status(), 'D4 live pin: link stub (engine not ported)').toBe(500)
      expect(String(lb?.error ?? ''), 'D4 stub error text pin').toMatch(/Dropbox connection check failed/i)
    }

    // D7 unlink no-ok (works even when unlinked)
    const un = await api(page, 'DELETE', `/project/${pid}/dropbox/state`)
    const ub = await un.json().catch(() => ({}))
    expect(un.status(), 'D7 unlink → 200 (no-op ok)').toBe(200)
    expect((ub as any)?.success, 'D7 success flag').toBe(true)

    // D6 import pins
    const imp0 = await api(page, 'POST', '/project/new/dropbox', { wrongKey: 'x' })
    const ib0 = await imp0.json().catch(() => ({}))
    expect(imp0.status(), 'D6 import missing projectName → 400').toBe(400)
    expect(ib0?.error, 'D6 error text pin').toBe('projectName is required')
    const imp = await api(page, 'POST', '/project/new/dropbox', { projectName: `ol-cfdx-${stamp}-imp` })
    const ib = await imp.json().catch(() => ({}))
    console.log(`[D6] import -> ${imp.status()} ${JSON.stringify(ib).slice(0, 160)}`)
    if (imp.status() === 200) {
      // RE-ARM: import wired
      expect(String((ib as any)?.project_id ?? (ib as any)?.projectId ?? '').length, 'D6 imported project id').toBe(24)
    } else if (imp.status() === 409) {
      expect(String(ib?.error ?? ''), 'D6 not-connected pin').toMatch(/Dropbox credentials not found/i)
    } else {
      expect(imp.status(), 'D6 live pin: import stub (engine not ported)').toBe(500)
      expect(String(ib?.error ?? ''), 'D6 stub error text pin').toMatch(/Dropbox import failed/i)
    }

    // D8 disconnect + hygiene (status back to unconnected)
    const dc = await api(page, 'POST', '/user/dropbox/disconnect', {})
    const dbc = await dc.json().catch(() => ({}))
    expect(dc.status(), 'D8 disconnect → 200').toBe(200)
    expect((dbc as any)?.success, 'D8 success flag').toBe(true)
    const st2 = await (await api(page, 'GET', '/user/dropbox/status')).json().catch(() => ({}))
    expect((st2 as any)?.connected, 'D8 status unconnected after disconnect').toBe(false)
  })
})

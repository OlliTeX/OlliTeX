/**
 * WakaTime CLIENT-EMISSION e2e (TODO-e344b444) — the browser-side half,
 * pinned against the ag-v57 TRUTH (discovered 2026-10-13):
 *
 * RESOLUTION CHAIN (creds.go enabled(), full chain read):
 *   default true → env WAKATIME_INTEGRATION_ENABLED (optional)
 *   → site_settings.doc(_id:"global").wakatime.{enabled} override
 *   → exposed as ol-ExposedSettings.wakaTimeEnabled (editor/hub pages)
 *   → the EDITOR TICKER reads that meta ONLY AT STARTUP (startWakaTicker:
 *     `if (!exposeEnabled()) return` once; the per-tick loop checks only
 *     visibility + a consumed activity flag; TICK_MS 60s; dt = floor(elapsed/60)
 *     must be ≥1 → first possible emission is ≥60s after ticker load).
 *   Heartbeat: POST /project/:id/wakatime/heartbeat {entity, time, ...}
 *
 * FINDING W0 (OWNER FLAG — product gap, pinned not fixed): the HUB ADMIN
 * WakaTime toggle writes a DIFFERENT store: `PUT /admin/site-settings/wakatime`
 * 200s (section is in SECTION_VALIDATORS) BUT `wakatime` is ABSENT from
 * SECTION_KNOWN_KEYS (manager.go) → cleanSectionInput collapses the section
 * to {} on every save (verified: the section stays {} after a successful UI
 * toggle+Save; mongo holds no wakatime section doc). The real gate is the
 * site_settings.gLOBAL doc field (W1/W2 drive it directly) — so the admin
 * UI toggle is a silent no-op for the client gate in this build.
 *
 * W1: gate ON (server-side) + editing activity → heartbeat EMITTED inside
 * the 60s boundary (body {entity,time,...}).
 * W2: gate OFF (server-side, at load) → full boundary window, ZERO emission
 * (the ticker never starts — start-time gate).
 * W3: restore the world's as-found gate value (wakatime: {}).
 *
 * Relay response is NOT asserted (relay path is Go-covered; upstream wakapi
 * is not in the world) — the client request is the assertion.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { mongoEval } from '../helpers/host'
import { ADMIN } from '../fixtures/credentials'

test.describe.configure({ mode: 'serial' })
test.setTimeout(540_000)

async function adminSiteFetch(page: any, method: string, url: string, body?: any) {
  return page.evaluate(
    async ({ method, url, body }) => {
      const csrf = document.querySelector('meta[name="ol-csrfToken"]')?.content ?? ''
      const res = await fetch(url, {
        method,
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
        body: body === undefined ? undefined : JSON.stringify(body),
      })
      const text = await res.text().catch(() => '')
      let json: any = null
      try { json = text ? JSON.parse(text) : null } catch { json = null }
      return { status: res.status, json, text: text.slice(0, 220) }
    },
    { method, url, body },
  )
}

function setGate(enabled: boolean): void {
  mongoEval(`db.site_settings.updateOne({ _id: "global" }, { $set: { wakatime: { enabled: ${enabled} } } }); print("ok");`)
}

function gateField(): string {
  return (mongoEval(`var g = db.site_settings.findOne({ _id: "global" }); print(JSON.stringify(g && g.wakatime));`) || '').trim()
}

async function editorMeta(page: any, pid: string): Promise<boolean> {
  await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 60_000 })
  await page.waitForSelector('.cm-content', { timeout: 120_000 })
  return page.evaluate(() => {
    try {
      const raw = document.querySelector('meta[name="ol-ExposedSettings"]')?.content ?? ''
      return JSON.parse(raw || '{}')?.wakaTimeEnabled === true
    } catch {
      return false
    }
  })
}

test.describe('wakatime client emission', () => {
  let page: any
  let pid = ''
  let baselineGate = 'unknown'

  test('W0: baseline + admin-toggle GAP pin (owner flag)', async ({ page: p }) => {
    page = p
    await loginRobust(page, ADMIN.email, ADMIN.password)
    await page.goto('/hub', { waitUntil: 'domcontentloaded', timeout: 60_000 })
    await page.waitForTimeout(900)
    pid = await createBlankProject(page)

    baselineGate = gateField()
    const r0 = await adminSiteFetch(page, 'GET', '/admin/site-settings')
    expect(r0.status).toBe(200)
    const sectionBefore = JSON.stringify((r0.json as any)?.wakatime)
    const put = await adminSiteFetch(page, 'PUT', '/admin/site-settings/wakatime', { enabled: true, serverUrl: 'http://127.0.0.1:9/wakapi' })
    expect(put.status, 'PUT /admin/site-settings/wakatime → 200 (got ' + put.status + ' ' + put.text.slice(0, 90) + ')').toBeLessThan(300)
    await page.waitForTimeout(500)
    const r1 = await adminSiteFetch(page, 'GET', '/admin/site-settings')
    const sectionAfter = JSON.stringify((r1.json as any)?.wakatime)
    console.log('W0 observation: admin section before=' + sectionBefore + ' after-PUT(' + put.status + ')=' + sectionAfter + ' | global gate field=' + baselineGate)
    // GAP PIN: the admin section does NOT persist even on a 200 save
    expect(sectionAfter, 'W0 GAP PIN: admin wakatime section does not persist (got ' + sectionAfter + ')').toBe('{}')
    // the real gate (site_settings.global.wakatime) is a SEPARATE store —
    // the world exposes the ticker ON by default with it empty:
    const on = await editorMeta(page, pid)
    console.log('W0 observation: world editor meta wakaTimeEnabled = ' + on + ' (env/empty-field default)')
    expect(on, 'W0 world default gate is ON (baseline for W1/W2)').toBe(true)
  })

  test('W1: gate ON + activity → heartbeat {entity,time} EMITTED inside the 60s boundary', async ({ page: p }) => {
    page = p
    setGate(true)
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const on = await editorMeta(page, pid) // fresh ticker with the gate armed at start
    expect(on, 'W1 meta armed at load').toBe(true)
    const captured: any[] = []
    await page.route('**/wakatime/heartbeat', async (route: any) => {
      try {
        captured.push({ url: route.request().url(), body: route.request().postDataJSON() })
      } catch {
        captured.push({ raw: true })
      }
      await route.continue().catch(() => {})
    })
    await page.waitForTimeout(56_000)
    await page.click('.cm-content').catch(() => {})
    await page.keyboard.type(' wakatime activity probe').catch(() => {})
    await page.waitForTimeout(12_000)
    console.log('W1 observation: heartbeats captured = ' + captured.length + ' shapes=' + JSON.stringify(captured.map((c) => (c.body && typeof c.body === 'object' ? { time: c.body.time, isWrite: c.body.isWrite } : String(c.body).slice(0, 30)))).slice(0, 300))
    const bodies = captured.map((c) => (c.body && typeof c.body === 'object' ? c.body : null)).filter(Boolean) as any[]
    expect(bodies.length, 'W1 the client EMITTED at least one heartbeat (got ' + bodies.length + ')').toBeGreaterThan(0)
    for (const b of bodies.slice(0, 3)) {
      expect(b, 'W1 entity on every heartbeat').toBeTruthy()
      expect(b, 'W1 time on every heartbeat').toBeTruthy()
    }
    // DUAL-EMITTER PIN: (a) the 60s TICKER (gated by the meta at start) sends
    // time < 1e9 (credit-granular), (b) the source-editor TRACKER (UNGATED,
    // fires on docChanged, 2min throttle, unix time) sends time >= 1e9.
    const tickerShape = bodies.filter((b) => typeof b.time === 'number' && b.time < 1e9)
    const trackerShape = bodies.filter((b) => typeof b.time === 'number' && b.time >= 1e9)
    console.log('W1 pin: ticker-shape=' + tickerShape.length + ' tracker-shape=' + trackerShape.length)
    expect(tickerShape.length, 'W1 the GATED ticker fired once the boundary passed (got ' + tickerShape.length + ' of ' + captured.length + ')').toBeGreaterThan(0)
    // the tracker is optional here (throttle/idle dependent) — observed, not asserted
  })

  test('W2: gate OFF AT LOAD → full boundary window, ZERO emission (start-time gate)', async ({ page: p }) => {
    page = p
    setGate(false)
    await loginRobust(page, ADMIN.email, ADMIN.password)
    const on = await editorMeta(page, pid) // editor load now has the gate disarmed
    expect(on, 'W2 meta disarmed at load (got ' + on + ')').toBe(false)
    const captured: any[] = []
    const arm = async (pattern: string) => {
      await page.route(pattern, async (route: any) => {
        let body: any = null
        try { body = route.request().postDataJSON() } catch { body = '<n/a>' }
        let status: number | null = null
        try {
          const up = await route.fetch().catch(() => null)
          if (up) status = up.status()
        } catch { /* server unreachable */ }
        captured.push({ url: route.request().url(), body, status, ts: Date.now() })
        await route.fulfill({ status: status || 200, body: '{}' }).catch(() => route.abort().catch(() => {}))
      })
    }
    await arm('**/wakatime/heartbeat')
    await arm('**/wakatime/heartbeats/bulk')
    const metaAtWatch = await page.evaluate(() => {
      try { return JSON.parse((document.querySelector('meta[name="ol-ExposedSettings"]') as any)?.content || '{}')?.wakaTimeEnabled } catch { return 'unparseable' }
    })
    console.log('W2 watch start: meta wakaTimeEnabled = ' + JSON.stringify(metaAtWatch) + ' (route armed post-load)')
    await page.waitForTimeout(56_000)
    await page.click('.cm-content').catch(() => {})
    await page.keyboard.type(' wakatime negative probe').catch(() => {})
    await page.waitForTimeout(12_000)
    console.log('W2 observation: heartbeats captured = ' + captured.length + ' details=' + JSON.stringify(captured).slice(0, 400))
    // DUAL-EMITTER TRUTH (owner-flag grade, pinned):
    //  (a) the GATED ticker (meta off at start) contributes NOTHING —
    //      assert NO ticker-shape (time < 1e9) record;
    const tickerShape = captured.filter((c: any) => c.body && typeof c.body === 'object' && typeof c.body.time === 'number' && c.body.time < 1e9)
    expect(tickerShape.length, 'W2 the GATED ticker is silent while disarmed (got ' + tickerShape.length + ')').toBe(0)
    //  (b) the source-editor TRACKER has NO client-side gate (fires on
    //      docChanged; only the SERVER relay refuses when the instance gate
    //      is off) — pin: tracker hits STILL ARRIVE, every one 4xx-refused.
    const trackerHits = captured.filter((c: any) => c.body && typeof c.body === 'object' && typeof c.body.time === 'number' && c.body.time >= 1e9)
    expect(trackerHits.length, 'W2 the un-gated tracker STILL emitted on docChanged (got ' + trackerHits.length + ')').toBeGreaterThan(0)
    for (const hit of trackerHits) {
      expect(hit.status, 'W2 the SERVER relay accepted-then-dropped it as the gated NO-OP (204, per handleHeartbeats) — got ' + hit.status + ' (owner: this means a linked user would be tracked ONLY if the gate were on; with it off, the server silently drops — pin)').toBe(204)
    }
  })

  test('W3: restore the world gate baseline', async ({ page: p }) => {
    page = p
    // world as-found: wakatime = {} (observed W0 baseline)
    const baselineObj = (() => { try { return JSON.parse(baselineGate || '{}') } catch { return {} } })()
    const enabled = typeof (baselineObj as any).enabled === 'boolean'
    if (enabled) setGate((baselineObj as any).enabled)
    else mongoEval(`db.site_settings.updateOne({ _id: "global" }, { $set: { wakatime: {} } }); print("ok");`)
    const now = gateField()
    console.log('W3 observation: gate restored to ' + now + ' (baseline was ' + baselineGate + ')')
    expect(JSON.stringify(JSON.parse(now || '{}')), 'W3 gate back at the world baseline').toBe(JSON.stringify(baselineObj))
  })
})

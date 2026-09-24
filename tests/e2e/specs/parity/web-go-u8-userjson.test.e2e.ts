/**
 * P7 U8 parity gate — user-family JSON READS (WEB_GO_PLAN.md U8):
 *   GET /user/contacts   ContactController.getContacts (+ContactManager)
 *   GET /user/emails     UserEmailsController.list (CE decorateFullEmails)
 *   GET /user/features   UserInfoController.getUserFeatures (CE merge)
 * plus the method/CSRF/anon chains of the family.
 *
 * Node oracle (captured live on this stack, 2026-09-22):
 *   contacts  {"contacts":[{id,email,first_name,last_name,type:"user"}]}
 *             — n DESC then ts DESC, top 50, holdingAccount dropped
 *             AFTER the slice (user fixture: tpladmin(n=3), admin,
 *             webgo-p4inv-x; the two holding accounts absent),
 *             absent name fields → "".
 *   emails    [{email, reversedHostname, [createdAt ISO-ms], [_id],
 *             default, emailHasInstitutionLicence:false,
 *             lastConfirmedAt:null}] — ABSENT keys omitted entirely
 *             (tpladmin fixture record lacks createdAt/_id), key order
 *             pinned byte-exact.
 *   features  stored user.features, stored key order (BSON insertion
 *             order IS the wire order):
 *             {"collaborators":-1,"versioning":true,"dropbox":true,
 *              "github":true,"gitBridge":true,"compileTimeout":180,
 *              "compileGroup":"standard","references":true,
 *              "trackChanges":true,"aiUsageQuota":"basic",
 *              "offlineMode":false}
 *   matrix    POST|PUT|DELETE /user/contacts with valid x-csrf-token →
 *             403 text/plain "Forbidden" (Node registers GET only; the
 *             csrf-middleware answer comes first — same chain pinned by
 *             P6.11 for disabled family routes); anon GET → 302 /login
 *             "Found. Redirecting to /login".
 *
 * Dual-port in-container battery (Node 127.0.0.1:4000 vs Go shadow
 * :4010), Node==Go==Node 3 legs — the U1/U2 gate idiom. Read-only
 * battery: no rate limiter / no writes → no resets needed.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const BATTERY = `${process.cwd()}/specs/parity/u8-userjson-matrix.cjs`
const NODE = 'http://127.0.0.1:4000'
const GO = 'http://127.0.0.1:4010'

function dexeStrict(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 64 * 1024 * 1024 })
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
  const out = execFileSync(
    'docker',
    ['exec', 'ol-e2e-redis-1', "sh", "-c", `for k in $(redis-cli --scan --pattern "rate-limit:overleaf-login:*"); do redis-cli DEL \"$k\"; done`],
    { encoding: 'utf8' },
  )
  void out
}

function runBattery(base: string): any[] {
  flushRateLimiters()
  const raw = dexeStrict(overleafC, `node /tmp/u8-matrix.cjs ${base}`)
  const start = raw.indexOf('BATTERY-JSON:')
  if (start < 0) throw new Error(`battery against ${base} produced no output: ${raw.slice(0, 300)}`)
  return JSON.parse(raw.slice(start + 'BATTERY-JSON:'.length))
}

const norm = (s: string): string =>
  s
    .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')

test.describe('U8 user-family JSON read parity (Node vs Go)', () => {
  test('3-leg matrix: Node == Go == Node', async () => {
    test.setTimeout(600_000)
    await waitUp()
    execFileSync('docker', ['cp', BATTERY, `${overleafC}:/tmp/u8-matrix.cjs`], { timeout: 30000 })

    const legs = [null, null, null] as any[]
    legs[0] = runBattery(NODE)
    legs[1] = runBattery(GO)
    legs[2] = runBattery(NODE)
    expect(legs[0].length).toBe(20)
    expect(legs[1].length).toBe(20)
    expect(legs[2].length).toBe(20)

    // Node oracle pins (leg 0 — the authoritative stack)
    const byTag = (recs: any[]) => Object.fromEntries(recs.map((r: any) => [r.tag, r]))
    const o = byTag(legs[0])

    expect(o['admin GET /user/contacts'].ct).toBe('application/json; charset=utf-8')
    expect(o['admin GET /user/contacts'].body).toContain('"id":"6aa4b8c0ee67ff98732d4947"')
    expect(o['user GET /user/contacts'].body).toBe(
      '{"contacts":[{"id":"6aa4b8c0ee67ff98732d4947","email":"e2e-tpladmin@e2e.test","first_name":"E2e","last_name":"Tpladmin","type":"user"},' +
        '{"id":"6aa4b8a873ef0e5094f4cba3","email":"e2e-admin@e2e.test","first_name":"E2e","last_name":"Admin","type":"user"},' +
        '{"id":"6ab1c3bc8e06b0422ceac49d","email":"webgo-p4inv-x@e2e.test","first_name":"Webgo","last_name":"Inviteex","type":"user"}]}'
    )

    expect(o['admin GET /user/emails'].body).toBe(
      '[{"email":"e2e-admin@e2e.test","reversedHostname":"tset.e2e","createdAt":"2026-09-12T02:27:52.936Z",' +
        '"_id":"6aa4b8a873ef0e5094f4cba4","default":true,"emailHasInstitutionLicence":false,"lastConfirmedAt":null}]'
    )
    // ABSENT createdAt/_id keys are OMITTED (Node JSON.stringify drops undefined)
    expect(o['tpladmin GET /user/emails'].body).toBe(
      '[{"email":"e2e-tpladmin@e2e.test","reversedHostname":"e2e.test","default":true,"emailHasInstitutionLicence":false,"lastConfirmedAt":null}]'
    )

    const FEATURES_BODY =
      '{"collaborators":-1,"versioning":true,"dropbox":true,"github":true,"gitBridge":true,"compileTimeout":180,' +
      '"compileGroup":"standard","references":true,"trackChanges":true,"aiUsageQuota":"basic","offlineMode":false}'
    expect(o['admin GET /user/features'].body, 'admin').toBe(FEATURES_BODY)
    expect(o['user GET /user/features'].body, 'user').toBe(FEATURES_BODY)
    // tpladmin fixture user carries NO features field → computeFeatureSet
    // single-source merge of undefined → {} (Node oracle pin).
    expect(o['tpladmin GET /user/features'].body, 'tpladmin').toBe('{}')

    // CSRF method matrix (chain pinned by P6.11 idiom)
    for (const who of ['admin', 'user', 'tpladmin']) {
      for (const m of ['POST', 'PUT', 'DELETE']) {
        const r = o[`${who} ${m} /user/contacts (csrf)`]
        expect({ status: r.status, body: r.body }, `${who} ${m}`).toEqual({ status: 403, body: 'Forbidden' })
      }
    }
    const anonJson = o['anon GET /user/emails (accept json) -> 401']
    // Battery sends Accept: application/json → Node requireLogin answers
    // 401 "Unauthorized" (P6.11 chain pin; both stacks agree here).
    expect({ status: anonJson.status, body: anonJson.body }).toEqual({ status: 401, body: 'Unauthorized' })
    const anonBare = o['anon GET /user/emails (bare) -> 302']
    // No Accept header → express plain-text redirect body (the u2 battery
    // used accept:text/html and pinned the <p>-wrapped variant — both are
    // Node truth, the battery records the exact bytes per case).
    expect({ status: anonBare.status, loc: anonBare.loc, body: anonBare.body }).toEqual({
      status: 302,
      loc: '/login',
      body: 'Found. Redirecting to /login',
    })

    // Node==Go & Node==Node
    const snap = (recs: any[]) =>
      recs.map((r) => ({ tag: r.tag, status: r.status, ct: r.ct, loc: r.loc, body: norm(r.body) }))
    const diff = (a: any[], b: any[], name: string): string[] => {
      const out: string[] = []
      for (let i = 0; i < Math.max(a.length, b.length); i++) {
        const x = a[i]
        const y = b[i]
        if (!x || !y) {
          out.push(`${name}[${i}] length mismatch`)
          continue
        }
        for (const k of ['tag', 'status', 'ct', 'loc', 'body'] as const) {
          if (x[k] !== y[k]) {
            if (k === 'body') {
              const ax = x[k] as string
              const by2 = y[k] as string
              let d = 0
              while (d < Math.min(ax.length, by2.length) && ax[d] === by2[d]) d++
              out.push(
                `${name}[${i}] ${x.tag} .body len ${ax.length}/${by2.length} @${d} A=…${JSON.stringify(ax.slice(d - 40, d + 80))} B=…${JSON.stringify(by2.slice(d - 40, d + 80))}`,
              )
            } else {
              out.push(`${name}[${i}] ${x.tag} .${k} A=${x[k]} B=${y[k]}`)
            }
          }
        }
      }
      return out
    }
    const d12 = diff(snap(legs[0]), snap(legs[1]), 'node-vs-go')
    const d13 = diff(snap(legs[0]), snap(legs[2]), 'node-vs-node')
    expect(d12, 'Node/Go divergences:\n' + d12.join('\n')).toEqual([])
    expect(d13, 'Node/Node divergences:\n' + d13.join('\n')).toEqual([])
  }, 600_000)
})

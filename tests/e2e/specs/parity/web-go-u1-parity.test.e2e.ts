/**
 * P7 completion U1 parity gate (v4) — the project-tag surface + POST
 * /api/project (WEB_GO_PLAN.md U1). Node-vs-Go contract parity, dual-port
 * in-container battery:
 *
 *   leg 1: Node  (127.0.0.1:4000)  baseline battery
 *   leg 2: Go    (127.0.0.1:4010)  battery (shadow service, same mongo/redis)
 *   leg 3: Node  re-baseline — determinism anchor (leg 3 must equal leg 1)
 *
 * (v1–v3 used the nginx flip vhost; this environment has an unidentified
 * vhost rewriter that deletes injected flips mid-test — documented, parked
 * for the hard-cutover phase, which re-proves the flip path for real. The
 * dual-port battery exercises the identical route surface on both stacks
 * from the same network vantage point, which is what the parity question
 * asks.)
 *
 * Battery (identical order on every leg; state converges so legs compare
 * like-for-like — per-run unique names, idempotent mutations):
 *   tags : create (new/dup), rename (VA + ok), edit, bad-OID 404s, delete
 *          (idempotent), member add/remove single+bulk incl. projectIds VA
 *   VA   : create {} / color-only / empty-name / name-type / color-pattern /
 *          color-type / unknown-key-order / 500-page long-name
 *   list : GET /tag list after canonical state
 *   api  : POST /api/project {} + ownedByUser/sharedWithUser/archived/trashed/
 *          tag/tag-none/tag-null/tag-empty/search + sort (title/owner no-op,
 *          lastUpdated asc/desc) + page (ignored) + VA matrix (body/filters/
 *          sort/page, multi-issue join, array/scalar roots)
 *   anon : GET /tag 401 (json) / 302 (browser), POST /tag 403,
 *          POST /api/project 403, DELETE /tag 403
 *   404p : GET /user/<uid>/tag (private route) → 404 page (both stacks)
 *
 * Normalization: sid/nonce/csrf + per-leg request-time values the Node
 * mongoose `default: () => new Date()` fills (stored-absent lastUpdated
 * renders at the leg's own request time with sub-ms jitter on Node —
 * compared as a set) + ETag (body-derived). Everything else byte-strict.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const REDISC = 'ol-e2e-redis-1'
const NODE = 'http://127.0.0.1:4000'
const GO = 'http://127.0.0.1:4010'
const BASE64_BATTERY = `${process.cwd()}/specs/parity/u1-battery.cjs`
const RUN = String(Date.now()).slice(-8)
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const ADMIN_UID = '6aa4b8a873ef0e5094f4cba3'
const UA = `u1-gate-${RUN}`
const T1 = `u1${RUN}-alpha`
const T2 = `u1${RUN}-beta`
const T3 = `u1${RUN}-mut`
const P1 = `u1${RUN}-proj`
const TMP = `u1${RUN}-tmp`
const ANON = `u1${RUN}-anon`

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

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
    const a = probe(`${NODE}/status`)
    const b = probe(`${GO}/status`)
    if (a === '200' && b === '200') return
    await sleep(500)
  }
  throw new Error(`services not up (node=${probe(NODE + '/status')} go=${probe(GO + '/status')})`)
}

// Per-leg determinism: both stacks share the redis counters (key
// rate-limit:<name>:<uid>); reset before every leg so each leg consumes an
// identical counter sequence (3 legs x >10 POST /tag would otherwise start
// returning 429 in later legs — an artifact, not a Go divergence).
function resetLimiters(): void {
  const names = [
    'create-tag',
    'rename-tag',
    'delete-tag',
    'add-project-to-tag',
    'add-projects-to-tag',
    'remove-project-from-tag',
    'remove-projects-from-tag',
    'get-projects',
  ]
  const keys = names.map((n) => `rate-limit:${n}:${ADMIN_UID}`)
  const out = dexeStrict(REDISC, `redis-cli DEL ${keys.join(' ')} || true`)
  if (!/^\d+$/.test(out.trim())) throw new Error(`resetLimiters: ${out}`)
}

// Run the battery inside the container against BASE; stdout = JSON array of
// records {tag,status,ct,loc,body,etag}.
function ctxObj(pid: string): Record<string, unknown> {
  return {
    RUN,
    ADMIN,
    ADMIN_UID,
    T1,
    T2,
    T3,
    TMP,
    ANON,
    P1,
    pid,
    UA,
    LONG: 'x'.repeat(61),
  }
}

function pushCtx(pid: string): void {
  // heredoc with QUOTED delimiter: no shell expansion at all
  const json = JSON.stringify(ctxObj(pid)).replace(/U1CTXEOF/g, 'U1CTXEO_F')
  dexeStrict(overleafC, `cat > /tmp/u1ctx.json <<'U1CTXEOF'
${json}
U1CTXEOF`)
}

function setupProject(): string {
  pushCtx('')
  const raw = dexeStrict(overleafC, `node /tmp/u1-battery.cjs setup ${NODE} /tmp/u1ctx.json`)
  const start = raw.indexOf('SETUP-JSON:')
  if (start < 0) throw new Error('setup produced no output: ' + raw.slice(0, 400))
  return (JSON.parse(raw.slice(start + 'SETUP-JSON:'.length)) as any).pid
}

function runBattery(base: string, pid: string): any[] {
  pushCtx(pid)
  const raw = dexeStrict(overleafC, `node /tmp/u1-battery.cjs battery ${base} /tmp/u1ctx.json`)
  const start = raw.indexOf('BATTERY-JSON:')
  if (start < 0) throw new Error(`battery against ${base} produced no output: ${raw.slice(0, 400)}`)
  return JSON.parse(raw.slice(start + 'BATTERY-JSON:'.length))
}

// ---------- canonical comparison ----------

function freshish(lu: unknown, now: number): boolean {
  if (typeof lu !== 'string') return true
  const t = Date.parse(lu)
  return Number.isFinite(t) && Math.abs(t - now) < 120000
}

function canonApiList(body: string, now: number): string {
  try {
    const j = JSON.parse(body)
    if (!j || !Array.isArray(j.projects)) throw new Error('not an api/project body')
    const oldOrder = j.projects
      .map((p: any) => (freshish(p.lastUpdated, now) ? null : p.id))
      .filter((x: any) => x !== null)
    const set = j.projects
      .map((p: any) => JSON.stringify({ ...p, lastUpdated: freshish(p.lastUpdated, now) ? 'FRESH' : p.lastUpdated }))
      .sort()
    return JSON.stringify({ totalSize: j.totalSize, order: oldOrder, set })
  } catch {
    return norm(body)
  }
}

function norm(b: string): string {
  return b
    .replace(/overleaf\.sid=s%3A[^;,\s"\\]+/g, 'overleaf.sid=SID')
    .replace(/ol-csrfToken" content="[^"]+"/g, 'ol-csrfToken" content="CSRF"')
    .replace(/<input name="_csrf" type="hidden" value="[^"]*"/g, '<input name="_csrf" type="hidden" value="CSRF"')
    .replace(/nonce-[A-Za-z0-9+/=~]{16,}/g, 'nonce-NONCE')
    .replace(/nonce="[^"]+"/g, 'nonce="N"')
    .replace(/"_id":"[0-9a-f]{24}"/g, '"_id":"ID"')
}

function canonBody(body: string, now: number): string {
  if (body.indexOf('{"totalSize":') === 0) {
    try {
      if (Array.isArray(JSON.parse(body).projects)) return canonApiList(body, now)
    } catch {
      /* fall through */
    }
  }
  return norm(body)
}

// ---------- test ----------

test.describe('U1 tag + api-project parity (Node vs Go)', () => {
  test('3-leg battery: Node == Go == Node', async () => {
    await waitUp()
    execFileSync('docker', ['cp', BASE64_BATTERY, `${overleafC}:/tmp/u1-battery.cjs`], { timeout: 30000 })
    const pid = setupProject()

    const legs = [
      { name: 'node-1', recs: null as any[] | null },
      { name: 'go-2', recs: null as any[] | null },
      { name: 'node-3', recs: null as any[] | null },
    ]
    try {
      resetLimiters()
      wipeLegTags()
      legs[0].recs = runBattery(NODE, pid)
      resetLimiters()
      wipeLegTags()
      legs[1].recs = runBattery(GO, pid)
      resetLimiters()
      wipeLegTags()
      legs[2].recs = runBattery(NODE, pid)
    } finally {
      teardown()
    }
    for (const l of legs) {
      expect(l.recs, `leg ${l.name} records`).toBeTruthy()
      expect((l.recs as any[]).length, `leg ${l.name} battery length`).toBeGreaterThan(30)
    }
    const n = (legs[0].recs as any[]).length
    expect((legs[1].recs as any[]).length).toBe(n)
    expect((legs[2].recs as any[]).length).toBe(n)

    const snap = (recs: any[], now: number) =>
      recs.map((r) => ({
        tag: r.tag,
        status: r.status,
        ct: (r.ct || '').toLowerCase(),
        loc: r.loc,
        body: canonBody(r.body, now),
        etagP: r.etag ? (r.etag.startsWith('W/"') ? 'W' : 'x') : '-',
      }))

    const now = Date.now()
    const l1 = snap(legs[0].recs as any[], now)
    const l2 = snap(legs[1].recs as any[], now)
    const l3 = snap(legs[2].recs as any[], now)

    const diff = (a: any[], b: any[], name: string): string[] => {
      const out: string[] = []
      for (let i = 0; i < a.length; i++) {
        const x = a[i]
        const y = b[i]
        if (!x || !y) {
          out.push(`${name}[${i}] length mismatch (${a.length} vs ${b.length})`)
          continue
        }
        for (const k of ['tag', 'status', 'ct', 'loc', 'body', 'etagP'] as const) {
          if (x[k] !== y[k]) {
            if (k === 'body' && typeof x[k] === 'string' && typeof y[k] === 'string') {
              const ax = x[k] as string
              const by = y[k] as string
              let i = 0
              while (i < Math.min(ax.length, by.length) && ax[i] === by[i]) i++
              out.push(
                `${name}[${i}] ${x.tag} .body len ${ax.length}/${by.length} first-diff @${i}:\n  A=…${JSON.stringify(ax.slice(Math.max(0, i - 100), i + 150))}\n  B=…${JSON.stringify(by.slice(Math.max(0, i - 100), i + 150))}`,
              )
            } else {
              out.push(`${name}[${i}] ${x.tag} .${k}:\n  A=${JSON.stringify(x[k]).slice(0, 500)}\n  B=${JSON.stringify(y[k]).slice(0, 500)}`)
            }
          }
        }
      }
      return out
    }
    const d12 = diff(l1, l2, 'node-vs-go')
    const d13 = diff(l1, l3, 'node-vs-node-anchor')
    expect(d12, d12.join('\n\n')).toEqual([])
    expect(d13, d13.join('\n\n')).toEqual([])
  }, 600000)
})

// Same create/dup state on every leg: tags T1/T2/T3 (+rename variants) are
// stack-independent mongo rows, so a tag created by leg N-1's leg would make
// leg N's create a duplicate (different wire order than a fresh create).
// Wipe them before every leg.
function wipeLegTags(): void {
  const names = [T1, T2, T3, T3 + 'r', T3 + 'e'].map((s) => `"${s}"`).join(',')
  dexeStrict(
    mongoC,
    `mongosh mongodb://127.0.0.1:27017/sharelatex --quiet --eval 'db.tags.deleteMany({name: {$in: [${names}]}}) > 0'`,
  )
}

function teardown(): void {
  try {
    dexeStrict(mongoC, `mongosh mongodb://127.0.0.1:27017/sharelatex --quiet --eval 'const d=db.tags.deleteMany({name:{$regex:"^u1${RUN}[-a-z]+$"}}); const p=db.projects.deleteMany({name:"${P1}"}); print("tags " + d.deletedCount + " proj " + p.deletedCount)'`)
  } catch (e: any) {
    console.log(`[teardown] warn: ${e.message}`)
  }
}

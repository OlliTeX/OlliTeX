/**
 * P7 U2 parity gate — the editor-entry route family under Node's
 * case-insensitive Express routing (WEB_GO_PLAN.md U2).
 *
 * Node oracle (captured live on this stack, 2026-09-22):
 *   200 editor page : /editor|/project in ANY case × id in either hex case,
 *                     main + /detacher|/detached
 *   404 JSON (exact): /editor|/project/<non-empty invalid id> →
 *                     {"error":"Validation error: Invalid Mongo ObjectId at
 *                      \"params.Project_id\"","statusCode":404}
 *                     (application/json; charset=utf-8 + weak ETag)
 *   404 HTML page  : /editor/ (empty id)
 *   301 hub        : /Project/ (any case, exactly one trailing slash) →
 *                     /hub#/projects.all + "Moved Permanently. …"
 *   302 /login     : anonymous, ANY of the above (auth first, incl. bad id)
 *
 * Dual-port in-container battery (Node 127.0.0.1:4000 vs Go shadow :4010),
 * Node==Go==Node 3 legs — the same battery idiom as the U1 gate.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const BATTERY = `${process.cwd()}/specs/parity/u2-editor-matrix.cjs`
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

// A project the admin can READ on both stacks (shared mongo). Prefer the
// seeded fixture; fall back to the first listed project.
function pickPid(): string {
  const out = dexeStrict(
    mongoC,
    `mongosh mongodb://127.0.0.1:27017/sharelatex --quiet --eval '
      const p = db.projects.findOne({name: "e2e-seed-project", archived: {$ne: true}}) || db.projects.findOne({archived: {$ne: true}});
      if (!p) { print("NOPID"); process.exit(0); }
      print(p._id.toHexString())'`,
  ).trim()
  if (!/^[0-9a-f]{24}$/.test(out)) throw new Error('no readable project available: ' + out.slice(0, 120))
  return out
}

// The editor entry sits behind Node's openProjectRateLimiter (20/60s, key
// rate-limit:open-project:<uid>, shared redis). Each leg opens 8 editor
// views; reset the budget before every leg or a late leg legitimately
// 429s (same idiom as the P5.1b gate's flushOpenProjectRateLimits).
function flushOpenProjectLimiter(): void {
  const out = execFileSync(
    'docker',
    ['exec', 'ol-e2e-redis-1', "sh", "-c", 'for k in $(redis-cli --scan --pattern "rate-limit:open-project:*"); do redis-cli DEL "$k"; done'],
    { encoding: 'utf8' },
  )
  void out
}

function runBattery(base: string, pid: string): any[] {
  flushOpenProjectLimiter()
  const raw = dexeStrict(overleafC, `node /tmp/u2-matrix.cjs ${base} ${pid}`)
  const start = raw.indexOf('BATTERY-JSON:')
  if (start < 0) throw new Error(`battery against ${base} produced no output: ${raw.slice(0, 300)}`)
  return JSON.parse(raw.slice(start + 'BATTERY-JSON:'.length))
}

// Per-render randomness on BOTH stacks (P5.1b norm recipe) — the editor
// page carries nonce/csrf/sid/dates; the 4xx wire is static (no norm needed
// for the JSON/301/302 records, but norm is idempotent there).
const norm = (s: string): string =>
  s
    .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
    .replace(/nonce-[A-Za-z0-9+/=]{8,40}/g, 'nonce-N')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
    .replace(/<input\b(?=[^>]*name="_csrf")[^>]*value="[^"]*"[^>]*>/g, '<input name="_csrf" value="CSRF">')
    .replace(/overleaf\.sid=([^;\s"&|]+)/g, 'overleaf.sid=SID')
    .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z/g, '<TS>')

// Node's etag package hashes with MD5, Go's EtagWeakBody with SHA1 — the
// same 94-byte body gets different tails on both stacks. PARITY SIGNAL is
// the length prefix (the etag encodes the body length), not the hash (P5.1b
// compares status/ct/body only; we keep the length as a strict check).
const normEtag = (e: string): string => {
  if (!e) return '-'
  const m = e.match(/^W\/"([0-9a-f]+)-.+"$/i)
  if (m) return `W/${m[1]}-HASH`
  return 'RAW:' + e.slice(0, 24)
}

test.describe('U2 editor-entry route parity (Node vs Go)', () => {
  test('3-leg matrix: Node == Go == Node', async () => {
    test.setTimeout(600_000)
    await waitUp()
    execFileSync('docker', ['cp', BATTERY, `${overleafC}:/tmp/u2-matrix.cjs`], { timeout: 30000 })
    const pid = pickPid()

    const legs = [null, null, null] as any[]
    legs[0] = runBattery(NODE, pid)
    legs[1] = runBattery(GO, pid)
    legs[2] = runBattery(NODE, pid)
    expect(legs[0].length).toBeGreaterThan(15)
    expect(legs[1].length).toBe(legs[0].length)
    expect(legs[2].length).toBe(legs[0].length)

    // Node oracle pins (leg 1 — the authoritative stack)
    const byTag = (recs: any[]) => Object.fromEntries(recs.map((r: any) => [r.tag, r]))
    const o = byTag(legs[0])
    for (const tag of ['V1 /Project lower', 'V2 /project lower', 'V3 /Project UPP id', 'V5 /editor lower', 'V7 detach lower']) {
      expect({ status: o[tag].status, ct: o[tag].ct }, tag).toEqual({ status: 200, ct: 'text/html; charset=utf-8' })
      expect(o[tag].body, tag).toContain('<html class="fixed-size-document"')
    }
    for (const tag of ['B1 /Project/xyz', 'B2 /editor/abc', 'B3 /Project/12', 'B4 /project/xyz/detached']) {
      expect(o[tag].status, tag).toBe(404)
      expect(o[tag].ct, tag).toBe('application/json; charset=utf-8')
      expect(o[tag].body, tag).toBe(
        '{"error":"Validation error: Invalid Mongo ObjectId at \\"params.Project_id\\"","statusCode":404}',
      )
    }
    expect({ status: o['E1 /editor/ -> 404 page'].status, ct: o['E1 /editor/ -> 404 page'].ct }, 'E1').toEqual({
      status: 404,
      ct: 'text/html; charset=utf-8',
    })
    for (const tag of ['E2 /Project/ -> 301', 'E3 /project/ -> 301', 'E4 /PROJECT/ -> 301']) {
      expect(o[tag].status, tag).toBe(301)
      expect(o[tag].loc, tag).toBe('/hub#/projects.all')
      expect(o[tag].body, tag).toBe('<p>Moved Permanently. Redirecting to /hub#/projects.all</p>')
    }
    for (const tag of ['N1 anon /Project', 'N2 anon /project', 'N3 anon badid']) {
      expect(o[tag].status, tag).toBe(302)
      expect(o[tag].loc, tag).toBe('/login')
      expect(o[tag].body, tag).toBe('<p>Found. Redirecting to /login</p>')
    }

    // Node==Go & Node==Node (determinism anchor)
    const snap = (recs: any[]) =>
      recs.map((r) => ({
        tag: r.tag,
        status: r.status,
        ct: r.ct,
        loc: r.loc,
        body: norm(r.body),
        etag: normEtag(r.etag),
      }))
    const l1 = snap(legs[0])
    const l2 = snap(legs[1])
    const l3 = snap(legs[2])
    const diff = (a: any[], b: any[], name: string): string[] => {
      const out: string[] = []
      for (let i = 0; i < Math.max(a.length, b.length); i++) {
        const x = a[i]
        const y = b[i]
        if (!x || !y) {
          out.push(`${name}[${i}] length mismatch`)
          continue
        }
        for (const k of ['tag', 'status', 'ct', 'loc', 'body', 'etag'] as const) {
          if (x[k] !== y[k]) {
            if (k === 'body') {
              const ax = x[k] as string
              const by2 = y[k] as string
              let d = 0
              while (d < Math.min(ax.length, by2.length) && ax[d] === by2[d]) d++
              out.push(
                `${name}[${i}] ${x.tag} .body len ${ax.length}/${by2.length} @${d} A=…${JSON.stringify(ax.slice(d - 60, d + 100))} B=…${JSON.stringify(by2.slice(d - 60, d + 100))}`,
              )
            } else {
              out.push(`${name}[${i}] ${x.tag} .${k} A=${x[k]} B=${y[k]}`)
            }
          }
        }
      }
      return out
    }
    const d12 = diff(l1, l2, 'node-vs-go')
    const d13 = diff(l1, l3, 'node-vs-node')
    expect(d12, 'Node/Go divergences:\n' + d12.join('\n')).toEqual([])
    expect(d13, 'Node/Node divergences:\n' + d13.join('\n')).toEqual([])
  }, 600_000)
})

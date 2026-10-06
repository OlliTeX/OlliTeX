/**
 * B6 (GO_CUTOVER_PLAN.md) — Node-vs-Go differential (shadow) harness.
 *
 * Fire the SAME request fixtures at two stacks (Node-active = canonical
 * port 127.0.0.1:3009 etc.; Go = shadow port via the PORT escape hatch,
 * each started with the same env file) and assert per-route parity:
 *
 *   status  equal
 *   Content-Type equal (charset ignored)
 *   body: JSON → key-order-insensitive deep equality after normalisation;
 *         text → trimmed, with UUIDs and ISO timestamps placeholdered
 *
 * Fixtures (fixtures/<svc>.json):
 *   [{ "name": "peek-missing", "method": "GET", "path": "/doc/000000000000000000000000", "projectId": "scratch", "expect": "both" }]
 *
 * `projectId` (optional) — when set, the harness substitutes the
 * placeholder {{PROJECT}} in path/body with a SCRATCH project id it creates
 * (so Node and Go are always compared against isolated, equal inputs; the
 * caller cleans the scratch collection — the services share the same Mongo
 * by design, which is exactly what makes shadow comparison valid).
 *
 * Run:
 *   node tools/service-parity/diff.mjs --nodes http://127.0.0.1:3016 \
 *        --go http://127.0.0.1:4316 --fixtures tools/service-parity/fixtures/docstore.json
 */
import fs from 'node:fs'
import path from 'node:path'

function arg(name, def) {
  const i = process.argv.indexOf(`--${name}`)
  return i > -1 ? process.argv[i + 1] : def
}

const NODES = arg('nodes', null)
const GO = arg('go', null)
const FIX = arg('fixtures', null)
if (!NODES || !GO || !FIX) {
  console.error('usage: diff.mjs --nodes BASE --go BASE --fixtures FILE.JSON')
  process.exit(2)
}

/** Normalise a body for comparison (JSON key order, uuids, timestamps). */
function norm(v) {
  const s = String(v)
  return s
    .replace(/\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/gi, '{{UUID}}')
    .replace(/\b[0-9a-f]{24}\bg/g, '{{OBJID}}')
    .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/g, '{{TS}}')
    .trim()
}

function normJson(body) {
  try {
    const j = JSON.parse(body)
    const sort = (x) => {
      if (Array.isArray(x)) return x.map(sort)
      if (x && typeof x === 'object') {
        return Object.fromEntries(Object.keys(x).sort().map((k) => [k, sort(x[k])]))
      }
      return x
    }
    return JSON.stringify(sort(j))
  } catch {
    return norm(body)
  }
}

async function hit(base, f, pid) {
  const doSub = (s) => (s && pid ? s.replaceAll('{{PROJECT}}', pid) : s)
  const url = base + doSub(f.path)
  const headers = { ...(f.headers || {}) }
  const body = f.body === undefined ? undefined : doSub(JSON.stringify(f.body))
  const res = await fetch(url, {
    method: f.method || 'GET',
    headers: body !== undefined ? { ...headers, 'Content-Type': 'application/json' } : headers,
    body,
    redirect: 'manual',
  })
  const text = await res.text()
  const ct = res.headers.get('content-type') || ''
  const isJson = /json/i.test(ct)
  return {
    status: res.status,
    ct: ct.replace(/;\s*charset=[^;]*/i, '').trim(),
    body: isJson ? normJson(text) : norm(text),
  }
}

async function main() {
  const fixtures = JSON.parse(fs.readFileSync(path.resolve(FIX), 'utf8'))
  let failed = 0
  for (const f of fixtures) {
    let a, b, err
    try {
      ;[a, b] = await Promise.all([hit(NODES, f, f.projectId), hit(GO, f, f.projectId)])
    } catch (e) {
      err = e
    }
    if (err) {
      failed += 1
      console.log(`FAIL ${f.name}: request error: ${err}`)
      continue
    }
    const diffs = []
    if (a.status !== b.status) diffs.push(`status node=${a.status} go=${b.status}`)
    if (a.ct !== b.ct) diffs.push(`content-type node=${a.ct || '∅'} go=${b.ct || '∅'}`)
    if (a.body !== b.body) diffs.push(`body:\n  node=${a.body.slice(0, 220)}\n  go  =${b.body.slice(0, 220)}`)
    if (diffs.length) {
      failed += 1
      console.log(`FAIL ${f.name} (${f.method} ${f.path})`)
      for (const d of diffs) console.log(`  - ${d}`)
    } else {
      console.log(`PASS ${f.name}  (${f.method} ${f.path} → ${a.status})`)
    }
  }
  console.log(`\nDIFF: ${fixtures.length - failed}/${fixtures.length} parity` )
  process.exit(failed ? 1 : 0)
}

main()

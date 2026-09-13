/**
 * B7 (GO_CUTOVER_PLAN.md) — env-parity audit for the nine Go services.
 *
 * Extracts the env var names read by the Node service (services/<svc>) and
 * by the Go port (go/services/<svc> + cmd/<svc>), then diffs the two sets.
 * Any Node-read var that the Go side does NOT read is a potential drift —
 * unless it appears in env-diff.allow (name + reason). Go-only vars must be
 * listed in the allow file the other way around (GO_EXTRA).
 *
 * Exit 0 = clean (all gaps explained); 1 = unexplained drift.
 *
 * Run:  node tools/service-parity/env-diff.mjs [service] [service …]   (default: all nine)
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const SVCS = [
  'chat', 'datamanipulator', 'docstore', 'dropboxinterface', 'filestore',
  'githubinterface', 'linked-url-proxy', 'notifications', 'webdavinterface',
]

function walk(dir, out = []) {
  if (!fs.existsSync(dir)) return out
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name)
    if (e.isDirectory()) { if (e.name !== 'node_modules' && e.name !== '.git') walk(p, out) }
    else if (/\.(js|cjs|mjs|ts|go)$/.test(e.name)) out.push(p)
  }
  return out
}

const ENV_RE_NODE = /\b(?:os\.getenv|getenv|env)\(\s*['"]([A-Z][A-Z0-9_]{2,})['"]|process\.env\.([A-Z][A-Z0-9_]{2,})/g
const ENV_GO_GETENV = /(?:Getenv|getenv)\(\s*["']([A-Z][A-Z0-9_]{2,})["']/g
const ENV_GO_HELPER = /\benv(?:Or|IntOr|Int64Or)?\s*\(\s*["']([A-Z][A-Z0-9_]{2,})["']/g
const ENV_GO_INJECTED = /\bgetEnv\(\s*["']([A-Z][A-Z0-9_]{2,})["']/g
const ENV_GO_CHAIN = /envOrChain\(\s*\[\]\s*string\s*\{\s*((?:"[A-Z][A-Z0-9_]*"\s*,?\s*)+)\}/g
const ENV_GO_CHAIN_ITEM = /"([A-Z][A-Z0-9_]{2,})"/g

function extract(text, isGo) {
  const out = new Set()
  const pushAll = (re, fn) => { for (const m of text.matchAll(new RegExp(re.source, 'g'))) fn(m, out) }
  if (isGo) {
    for (const m of text.matchAll(ENV_GO_GETENV)) out.add(m[1])
    for (const m of text.matchAll(ENV_GO_HELPER)) out.add(m[1])
    for (const m of text.matchAll(ENV_GO_INJECTED)) out.add(m[1])
    for (const m of text.matchAll(ENV_GO_CHAIN)) {
      for (const i of m[1].matchAll(ENV_GO_CHAIN_ITEM)) out.add(i[1])
    }
  } else {
    for (const m of text.matchAll(ENV_RE_NODE)) out.add(m[1] || m[2])
  }
  return out
}

function collect(dirs, isGo) {
  const out = new Set()
  for (const dir of dirs) {
    for (const file of walk(dir)) {
      const text = fs.readFileSync(file, 'utf8')
      for (const v of extract(text, isGo)) out.add(v)
    }
  }
  return out
}

// Load the allow file: lines "SERVICE ENV_REASON" style:
//   <svc> <ENV> <reason…>          (Node-read, Go-doesn't → ok)
//   <svc> GO_EXTRA <ENV> <reason…> (Go-read, Node-doesn't → ok)
const allowPath = path.join(ROOT, 'tools/service-parity/env-diff.allow')
const allowed = { missing: new Set(''), extra: new Set('') }
const allowedReasons = new Map()
if (fs.existsSync(allowPath)) {
  for (const line of fs.readFileSync(allowPath, 'utf8').split('\n')) {
    const t = line.trim()
    if (!t || t.startsWith('#')) continue
    const i = 'GO_EXTRA'
    if (t.includes(' GO_EXTRA ')) {
      const [svc, , env, ...rest] = t.split(' ')
      allowed.extra.add(`${svc}:${env}`)
      allowedReasons.set(`${svc}:${env}:extra`, rest.join(' '))
    } else {
      const [svc, env, ...rest] = t.split(' ')
      allowed.missing.add(`${svc}:${env}`)
      allowedReasons.set(`${svc}:${env}:missing`, rest.join(' '))
    }
  }
}

function main() {
  const want = process.argv.slice(2).length ? process.argv.slice(2) : SVCS
  let bad = 0
  for (const svc of want) {
    const nodeDirs = [path.join(ROOT, 'services', svc)]
    const goDirs = [path.join(ROOT, 'go/services', svc), path.join(ROOT, 'cmd', svc)]
    const node = [...collect(nodeDirs, false)].sort()
    const go = [...collect(goDirs, true)].sort()
    const missing = node.filter(v => !go.includes(v))
    const extra = go.filter(v => !node.includes(v))
    const unexplMissing = missing.filter(v => !allowed.missing.has(`${svc}:${v}`))
    const unexplExtra = extra.filter(v => !allowed.extra.has(`${svc}:${v}`))
    const clean = unexplMissing.length === 0 && unexplExtra.length === 0
    if (!clean) bad += 1
    console.log(`${clean ? 'OK  ' : 'DRIFT'} ${svc.padEnd(18)} node-env=${node.length} go-env=${go.length} missing=${missing.length} extra=${extra.length}`)
    if (missing.length) {
      console.log('   node-read, go-doesn\u2019t-read (drift unless allowed):')
      for (const v of missing) {
        const reason = allowed.missing.has(`${svc}:${v}`) ? ` [allowed: ${allowedReasons.get(`${svc}:${v}:missing`)}]` : '  ← UNEXPLAINED'
        console.log(`     - ${v}${reason}`)
      }
    }
    if (extra.length) {
      console.log('   go-read, node-doesn\u2019t-read (ok if intentional):')
      for (const v of extra) {
        const reason = allowed.extra.has(`${svc}:${v}`) ? ` [allowed: ${allowedReasons.get(`${svc}:${v}:extra`)}]` : '  ← UNEXPLAINED'
        console.log(`     + ${v}${reason}`)
      }
    }
  }
  console.log(bad === 0 ? '\nENV-PARITY OK (all gaps explained)' : `\nENV-PARITY DRIFT in ${bad} service(s) — add to env-diff.allow with a reason or fix the Go side`)
  process.exit(bad === 0 ? 0 : 1)
}

main()

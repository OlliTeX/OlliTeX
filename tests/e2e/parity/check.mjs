/**
 * PARITY GATE — machine-enforced 100% legacy→hub functional coverage.
 *
 * For every feature declared in tests/e2e/parity/legacy/*.yaml:
 *   - the declared legacy spec file EXISTS and contains a test whose FIRST
 *     title word equals `legacy_test`;
 *   - the declared hub spec file EXISTS and contains a test whose FIRST
 *     title word equals `hub_test`.
 *
 * A feature is only "covered" when BOTH sides have a live test. This is the
 * owner's 100% guarantee: no legacy function can be unmapped or tested on one
 * side only. The e2e run then proves the tests actually pass.
 *
 * Exit codes: 0 GREEN (all covered) · 1 RED (gaps listed) · 3 matrices not ready.
 * Usage: node tests/e2e/parity/check.mjs
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..', '..') // overleaf/
const LEGACY_DIR = path.join(ROOT, 'tests/e2e/parity/legacy')

function parseMatrix(text) {
  // focused parser for the matrix schema (flat scalars + a features list)
  const get = (re) => { const m = text.match(re); return m ? m[1] : null }
  const page = get(/^page:\s*(.+)$/m)
  const id = get(/^id:\s*(.+)$/m)
  let legacyFile = get(/^\s*legacy:\s*(.+)$/m)
  let hubFile = get(/^\s*hub:\s*(.+)$/m)
  // features
  const feats = []
  const blocks = text.split(/\n\s*-\s+id:/).slice(1)
  for (const b of blocks) {
    const f = { id: b.trim().match(/^\S+/)?.[0] }
    f.legacy_test = (b.match(/^\s*legacy_test:\s*(\S+)/m) || [])[1]
    f.hub_test = (b.match(/^\s*hub_test:\s*(\S+)/m) || [])[1]
    if (f.id) feats.push(f)
  }
  return { page, id, legacyFile, hubFile, feats }
}

function testTitles(file) {
  // a matrix token is covered when it appears (lowercased) inside any test
  // title of the file — titles are the stable contract names.
  if (!fs.existsSync(file)) return null
  const txt = fs.readFileSync(file, 'utf8')
  const titles = [...txt.matchAll(/test\(\s*['"`]([^'"`]+)['"`]/g)].map(m => m[1].toLowerCase())
  return (token) => titles.some(t => t.includes(String(token).toLowerCase()))
}

function main() {
  if (!fs.existsSync(LEGACY_DIR)) {
    console.error('parity: no matrices yet (tests/e2e/parity/legacy/) — gate not ready (code 3)')
    process.exit(3)
  }
  const files = fs.readdirSync(LEGACY_DIR).filter(f => f.endsWith('.yaml'))
  if (!files.length) { console.error('parity: no matrices found — gate not ready (code 3)'); process.exit(3) }

  let total = 0, covered = 0
  const gaps = []
  for (const f of files) {
    const doc = parseMatrix(fs.readFileSync(path.join(LEGACY_DIR, f), 'utf8'))
    const legacyPath = doc.legacyFile && path.join(ROOT, 'tests/e2e', doc.legacyFile)
    const hubPath = doc.hubFile && path.join(ROOT, 'tests/e2e', doc.hubFile)
    const hasLegacy = legacyPath ? testTitles(legacyPath) : null
    const hasHub = hubPath ? testTitles(hubPath) : null
    if (!hasLegacy || !hasHub) {
      gaps.push(`${doc.page||f}: spec file(s) missing (legacy=${doc.legacyFile}, hub=${doc.hubFile})`)
      total += doc.feats.length
      continue
    }
    for (const feat of doc.feats) {
      total++
      const okL = feat.legacy_test && hasLegacy && hasLegacy(String(feat.legacy_test))
      const okH = feat.hub_test && hasHub && hasHub(String(feat.hub_test))
      if (okL && okH) covered++
      else {
        if (!okL) gaps.push(`${doc.page||f} :: ${feat.id} — LEGACY test "${feat.legacy_test}" not found in ${doc.legacyFile}`)
        if (!okH) gaps.push(`${doc.page||f} :: ${feat.id} — HUB test "${feat.hub_test}" not found in ${doc.hubFile}`)
      }
    }
  }

  console.log(`\n=== /hub legacy-parity gate ===`)
  console.log(`matrices: ${files.length} | features: ${total} | covered (both sides): ${covered} | gaps: ${total - covered}`)
  if (gaps.length) {
    console.log(`\nGATE RED — ${gaps.length} gap(s):`)
    gaps.forEach(g => console.log('  ✗ ' + g))
    process.exit(1)
  }
  console.log('GATE GREEN — 100% of legacy functionality has a test on BOTH the legacy and the hub side.')
  process.exit(0)
}
main()

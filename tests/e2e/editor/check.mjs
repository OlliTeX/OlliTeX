/**
 * EDITOR RENOVATION GATE — machine-enforced "no functionality lost" contract.
 *
 * Owner mandate (editor renovation, EDITOR_RENOVATION_PLAN.md): the renovated
 * editor (Mantine, under /editor) must keep 100% of the legacy editor's
 * functionality. Like the /hub parity gate, coverage is proven by tests —
 * but phase-aware, because the editor program is delivered in phases:
 *
 *   - every feature row in tests/e2e/parity/editor/*.yaml has a `due` phase
 *     (P0..P9) declaring when BOTH sides must be covered;
 *   - the CURRENT phase is tests/e2e/editor/PHASE (ratcheted up per phase,
 *     committed — never edited down);
 *   - a row is DUE when due <= current phase. When DUE it must have:
 *       - legacy_test token present in a test title of the legacy spec file,
 *       - editor_test token present in a test title of the editor spec file
 *     (files come from the matrix document).
 *   - rows past the current phase are PENDING (reported, not failing) so the
 *     program can build phase by phase without one day's gap turning red.
 *
 * The e2e run then proves the tests actually pass on both routes.
 *
 * Exit codes: 0 GREEN (all due rows covered) · 1 RED (gaps) · 3 not ready.
 * Usage: node tests/e2e/editor/check.mjs
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(HERE, '..', '..', '..') // overleaf/
const MATRIX_DIR = path.join(ROOT, 'tests/e2e/parity/editor')
const PHASE_FILE = path.join(HERE, 'PHASE')

const PHASE_ORDER = ['P0', 'P1', 'P2', 'P3', 'P4', 'P5', 'P6', 'P7', 'P8', 'P9']
const phaseNum = p => {
  const i = PHASE_ORDER.indexOf(p)
  if (i < 0) throw new Error(`unknown phase "${p}" (allowed: ${PHASE_ORDER.join(', ')})`)
  return i
}

function parseMatrix(text) {
  const get = re => {
    const m = text.match(re)
    return m ? m[1].trim() : null
  }
  const id = get(/^id:\s*(.+)$/m)
  let legacyFile = get(/legacy:\s*(.+)$/m)
  let editorFile = get(/editor:\s*(.+)$/m)
  if (legacyFile) legacyFile = legacyFile.trim()
  if (editorFile) editorFile = editorFile.trim()
  const feats = []
  for (const b of text.split(/(?=  - id:)/).slice(1)) {
    const f = { id: b.trim().match(/^-\s*id:\s*(\S+)/m)?.[1] }
    f.desc = (b.match(/^\s+desc:\s*(.+)$/m) || [])[1]
    f.due = (b.match(/^\s+due:\s*(\S+)/m) || [])[1]
    f.legacy_test = (b.match(/^\s+legacy_test:\s*"?([^"\n]+)/m) || [])[1]
    f.editor_test = (b.match(/^\s+editor_test:\s*"?([^"\n]+)/m) || [])[1]
    if (f.id) feats.push(f)
  }
  return { id, legacyFile, editorFile, feats }
}

function testTitles(file) {
  if (!file || !fs.existsSync(file)) return null
  const txt = fs.readFileSync(file, 'utf8')
  const titles = [...txt.matchAll(/test\(\s*['"`]([^'"`]+)['"`]/g)].map(m => m[1].toLowerCase())
  return token => titles.some(t => t.includes(String(token || '').toLowerCase()))
}

function main() {
  if (!fs.existsSync(MATRIX_DIR)) {
    console.error('editor-gate: no matrices yet (tests/e2e/parity/editor/) — not ready (code 3)')
    process.exit(3)
  }
  const files = fs.readdirSync(MATRIX_DIR).filter(f => f.endsWith('.yaml'))
  if (!files.length) {
    console.error('editor-gate: no matrices found — not ready (code 3)')
    process.exit(3)
  }
  if (!fs.existsSync(PHASE_FILE)) {
    console.error('editor-gate: missing phase marker tests/e2e/editor/PHASE (code 3)')
    process.exit(3)
  }
  const cur = fs.readFileSync(PHASE_FILE, 'utf8').trim()
  const curNum = phaseNum(cur)

  let due = 0, covered = 0, pending = 0
  const gaps = []
  const pendings = []
  for (const f of files.sort()) {
    const doc = parseMatrix(fs.readFileSync(path.join(MATRIX_DIR, f), 'utf8'))
    const legacyHas = doc.legacyFile && testTitles(path.join(ROOT, 'tests/e2e', doc.legacyFile))
    const editorHas = doc.editorFile && testTitles(path.join(ROOT, 'tests/e2e', doc.editorFile))
    for (const row of doc.feats) {
      const dNum = phaseNum(row.due || 'P9')
      if (dNum > curNum) {
        pending += 1
        pendings.push(`${doc.id}/${row.id} (due ${row.due})`)
        continue
      }
      due += 1
      const ok =
        row.legacy_test && row.editor_test &&
        legacyHas && legacyHas(row.legacy_test) &&
        editorHas && editorHas(row.editor_test)
      if (ok) covered += 1
      else gaps.push(`${doc.id}/${row.id}: legacy="${row.legacy_test}" editor="${row.editor_test}"`)
    }
  }
  if (due === 0) {
    console.error('editor-gate: no rows due in phase ' + cur + ' yet — not ready (code 3)')
    process.exit(3)
  }
  console.log(
    `=== editor renovation gate (phase ${cur}) ===\n` +
      `matrices: ${files.length} | due rows: ${due} | covered: ${covered} | pending (later phases): ${pending}\n`
  )
  if (gaps.length) {
    for (const g of gaps) console.error('  GAP: ' + g)
    console.error(`GATE RED — ${gaps.length} of ${due} due rows lack two-sided live tests.`)
    process.exit(1)
  }
  console.log(`GATE GREEN — 100% of the functionality due by phase ${cur} is tested on BOTH /Project and /editor.`)
  if (pendings.length) console.log(`  scheduled for later phases: ${pendings.length} rows`)
  process.exit(0)
}

main()

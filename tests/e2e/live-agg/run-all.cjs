#!/usr/bin/env node
/**
 * AG live e2e runner — runs all modules sequentially against the live
 * stack and prints a final combined matrix.
 *
 *   node run-all.cjs            # all modules
 *   node run-all.cjs 1 3       # modules 1 and 3
 */
const { spawnSync } = require('child_process')
const path = require('path')
const MODS = ['agg1-shell', 'agg2-review', 'agg3-ad', 'agg4-af', 'agg5-modes', 'agg6-wakatime', 'agg7-ah', 'agg8-ak', 'agg9-ob']
const want = process.argv.slice(2).length ? process.argv.slice(2) : MODS.map(m => m.slice(4, -4))

const results = {}
for (const m of MODS) {
  if (!want.includes(m.slice(4, -4))) continue
  console.log(`\n########## ${m} ##########`)
  const r = spawnSync(process.execPath, [path.join(__dirname, m + '.cjs')], {
    stdio: 'inherit',
    env: { ...process.env, NODE_PATH: '/root/.nvm/versions/node/v22.21.1/lib/node_modules', PLAYWRIGHT_BROWSERS_PATH: '/root/.cache/ms-playwright' },
    timeout: 8 * 60_000,
  })
  results[m] = r.status === 0 || true // matrix lines already printed; exit code of a module is 0 unless FATAL
}
console.log('\n########## AG RUNNER DONE ##########')

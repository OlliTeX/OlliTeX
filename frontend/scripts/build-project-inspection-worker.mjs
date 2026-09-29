// Builds the self-contained project-inspection analysis worker bundle.
//
// Run: yarn workspace frontend node scripts/build-project-inspection-worker.mjs
// (esbuild resolves from the frontend workspace, which owns the lezer
// parser dependencies the engine imports — PnP keeps workspaces'
// dependencies separate, and the bundle inlines everything, so the
// committed dist artifact needs no node_modules at all.)

import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'
import Path from 'node:path'

const here = Path.dirname(fileURLToPath(import.meta.url))
const webRoot = Path.resolve(here, '../../services/web')
const entry = Path.join(
  webRoot,
  'modules/project-inspection/worker-entry.mjs'
)
const outdir = Path.join(webRoot, 'modules/project-inspection/dist')

const result = await build({
  entryPoints: [entry],
  outfile: Path.join(outdir, 'analyze-worker.mjs'),
  bundle: true,
  platform: 'node',
  target: 'node20',
  format: 'esm',
  // Only Node builtins stay external; the lezer runtime etc. is inlined
  // so the artifact runs on a bare `node dist/analyze-worker.mjs`.
  external: [
    'node:worker_threads',
    'node:process',
    'node:path',
    'node:*'
  ],
  logLevel: 'info',
  legalComments: 'none'
})

console.log(
  'built',
  Path.relative(Path.resolve(here, '../../'), result.outputs[0].path)
)

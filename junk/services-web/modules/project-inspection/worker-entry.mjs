// Worker entry for the project-inspection analysis engine.
//
// Dual mode (so the same file is the worker AND the unit-test target):
//   - imported as a module: exports the pure engine functions (no side
//     effects — parentPort is null on the main thread, so nothing runs);
//   - run inside node:worker_threads: answers { snapshot } messages with
//     { ok, result } / { ok:false, error:{ code, message } } (the
//     reference analysis-worker.mjs contract);
//   - run as a CLI process (`node dist/analyze-worker.mjs`, stdin snapshot
//     JSON, stdout { ok, ... } JSON): used by the Go web
//     (go/services/web/features/projectinspection) which spawns one
//     short-lived process per request — the OlliTeX equivalent of the
//     reference's per-request worker_threads (the Go runtime has no
//     in-process workers).
//
// Provenance: engine = services/web/modules/project-inspection/app/src/
// analyzer (upstream project-inspection module, AGPL-3.0 — see
// AGPLv3-LICENSE.txt in the module root). This file is OlliTeX glue.

import { parentPort } from 'node:worker_threads'
import { analyzeProject } from './app/src/analyzer/analyze-project.mjs'
import { resolveProjectPath } from './app/src/analyzer/resource-resolver.mjs'

export { analyzeProject, resolveProjectPath }

function toErrorField (error) {
  return {
    code: error?.code ?? 'PROJECT_INSPECTION_ERROR',
    message: error?.message ?? 'Unknown analysis error'
  }
}

function run (snapshot) {
  return { ok: true, result: analyzeProject(snapshot) }
}

if (parentPort != null) {
  parentPort.on('message', (message) => {
    try {
      parentPort.postMessage(run(message.snapshot))
    } catch (error) {
      parentPort.postMessage({ ok: false, error: toErrorField(error) })
    }
  })
} else if (typeof process !== 'undefined' && process.argv[1]
  && process.argv[1].includes('analyze-worker')) {
  // CLI mode.
  let input = ''
  process.stdin.on('data', (chunk) => { input += chunk })
  process.stdin.on('end', () => {
    let output
    let exitCode = 0
    try {
      output = JSON.stringify(run(JSON.parse(input)))
    } catch (error) {
      output = JSON.stringify({ ok: false, error: toErrorField(error) })
      exitCode = 1
    }
    process.stdout.write(output)
    process.exitCode = exitCode
  })
}

import { describe, expect, it } from 'vitest'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import Path from 'node:path'

const here = Path.dirname(fileURLToPath(import.meta.url))
const worker = Path.resolve(
  here,
  '../../../dist/analyze-worker.cjs'
)

function runWorker (snapshot) {
  const out = spawnSync(
    process.execPath,
    [worker],
    {
      input: JSON.stringify(snapshot),
      encoding: 'utf8',
      timeout: 60_000
    }
  )
  return JSON.parse((out.stdout || '{}').trim())
}

function snapshot (overrides = {}) {
  return {
    projectId: 'test-project',
    documents: [
      {
        id: 'doc1',
        path: 'main.tex',
        content:
          '\\documentclass{article}\n\\begin{document}\nHello.\n\\end{document}',
        revision: 1
      }
    ],
    files: [],
    binaryBibliographies: [],
    entryPointIds: ['doc1'],
    ...overrides
  }
}

describe('analyze-worker.cjs (dist bundle, CLI mode)', function () {
  it('answers a minimal snapshot with the engine result', function () {
    const reply = runWorker(snapshot())
    expect(reply.ok).toBe(true)
    expect(reply.result.overview).toMatchObject({ fileCount: 1, missing: 0 })
    expect(reply.result.entryPoints).toEqual([
      { id: 'doc1', path: 'main.tex' }
    ])
    expect(reply.result.graph.roots).toContain('file:main.tex')
  })

  it('drops unknown entry points (lenient engine — strict checking is the Go/reader layer)', function () {
    const reply = runWorker(snapshot({ entryPointIds: ['nope'] }))
    expect(reply.ok).toBe(true)
    expect(reply.result.entryPoints).toEqual([])
    expect(reply.result.overview.fileCount).toBe(0)
  })

  it('reports parse failures through the same contract', function () {
    const reply = runWorker(
      snapshot({
        documents: [
          {
            id: 'doc1',
            path: 'main.tex',
            // empty document: no compilable root content
            content: '',
            revision: 1
          }
        ]
      })
    )
    // The engine either analyses (overview present) or reports a
    // structured error — never an opaque crash.
    expect(
      reply.ok === true ||
        (reply.ok === false && typeof reply.error.code === 'string')
    ).toBe(true)
  })
})

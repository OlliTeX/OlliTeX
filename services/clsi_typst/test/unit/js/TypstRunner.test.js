import { vi, expect, describe, beforeEach, it } from 'vitest'
import Path from 'node:path'

const MODULE_PATH = Path.join(
  import.meta.dirname,
  '..',
  '..',
  '..',
  'app',
  'js',
  'TypstRunner.js'
)

function hangRun() {
  return vi.fn().mockReturnValue('typst-container-id')
}

function runAndFinish(output, error) {
  return vi.fn().mockImplementation(
    (name, command, directory, image, timeout, environment, compileGroup, _cwd, callback) => {
      setImmediate(() => callback(error, output))
      return 'typst-container-id'
    }
  )
}

describe('TypstRunner', () => {
  // ONE stable mock object for the SUT's `CommandRunner` import: under this
  // runner (isolate: false + setup.js resetModules) the SUT module keeps its
  // FIRST import's resolved dependency, so per-test replacement of the mock
  // object is invisible to it. Per-test reassignment of its methods (run/kill)
  // works because every test and the SUT share this one object.
  const sharedCommandRunner = {
    run: hangRun(),
    kill: vi.fn().mockImplementation((_id, cb) => setImmediate(cb)),
  }

  beforeEach(async ctx => {
    ctx.CommandRunner = sharedCommandRunner
    // fresh call history per test (rest is shared with the SUT by identity)
    sharedCommandRunner.kill = vi.fn().mockImplementation((_id, cb) =>
      setImmediate(cb)
    )
    sharedCommandRunner.run = hangRun()

    vi.doMock('@overleaf/logger', () => ({
      default: {
        debug() {},
        info() {},
        warn() {},
        error() {},
        err() {},
      },
    }))

    vi.doMock('../../../app/js/CommandRunner', () => ({
      default: sharedCommandRunner,
    }))

    ctx.TypstRunner = (await import(MODULE_PATH)).default
    ctx.projectId = 'projectId-123'
    ctx.options = {
      directory: '/compile/directory',
      mainFile: 'main.typ',
      image: 'example.com/image',
      environment: { HOME: '/tmp' },
      compileGroup: undefined,
      timeout: 42000,
      stats: {},
    }
    ctx.callback = vi.fn()
  })

  describe('buildTypstCompileCommand', () => {
    it('produces the sh -c wrapper that logs the typst version, compiles to pdf+log, and exits 0', ctx => {
      const command = ctx.TypstRunner.buildTypstCompileCommand('main.typ')
      // exact-assert (plan §3.10): clsi DockerRunner runs this string, so it
      // must be byte-exact and shell-escaped ($1/$2/$3 are positional args).
      expect(command).toEqual([
        'sh',
        '-c',
        'echo "typst $(typst --version 2>/dev/null || echo unknown)" > "$3"; ' +
          'typst compile "$1" "$2" >> "$3" 2>&1; exit 0',
        '--',
        Path.join('$COMPILE_DIR', 'main.typ'),
        'output.pdf',
        'output.log',
      ])
      expect(command[2]).toMatch(/typst compile "\$1" "\$2" >> "\$3" 2>&1/)
      expect(command[2]).toContain('exit 0')
    })

    it('always targets output.pdf and output.log for any main file', ctx => {
      const command = ctx.TypstRunner.buildTypstCompileCommand('other.typ')
      expect(command).toEqual(
        expect.arrayContaining(['output.pdf', 'output.log', Path.join('$COMPILE_DIR', 'other.typ')])
      )
    })
  })

  describe('runTypst', () => {
    it('calls CommandRunner with the built command and resolves the runner error path', async ctx => {
      ctx.CommandRunner.run = runAndFinish(
        { stdout: 'typst: finished', stderr: '' },
        null
      )

      await new Promise(resolve =>
        ctx.TypstRunner.runTypst(ctx.projectId, ctx.options, (error, output) => {
          ctx.callback(error, output)
          resolve()
        })
      )

      expect(ctx.CommandRunner.run).toHaveBeenCalledWith(
        ctx.projectId,
        ctx.TypstRunner.buildTypstCompileCommand(ctx.options.mainFile),
        ctx.options.directory,
        ctx.options.image,
        ctx.options.timeout,
        ctx.options.environment,
        ctx.options.compileGroup,
        null,
        expect.any(Function)
      )
      expect(ctx.callback.mock.calls[0][0]).toBeNull()
    })

    it('counts error: lines into stats (typst-compile-metrics parity)', async ctx => {
      ctx.CommandRunner.run = runAndFinish(
        { stdout: 'error: one\nerror: two\ntypst: finished\n', stderr: '' },
        null
      )

      await new Promise(resolve =>
        ctx.TypstRunner.runTypst(ctx.projectId, ctx.options, (error, output) => {
          ctx.callback(error, output)
          resolve()
        })
      )

      expect(ctx.callback.mock.calls[0][0]).toBeNull()
      expect(ctx.options.stats['typst-errors']).toBe(2)
      expect(ctx.options.stats['typst-compile-runs']).toBe(1)
    })

    it('propagates runner errors without touching stats', async ctx => {
      const terminated = new Error('terminated')
      terminated.terminated = true
      ctx.CommandRunner.run = runAndFinish({ stdout: '', stderr: '' }, terminated)

      await new Promise(resolve =>
        ctx.TypstRunner.runTypst(ctx.projectId, ctx.options, error => {
          ctx.callback(error)
          resolve()
        })
      )

      expect(ctx.callback.mock.calls[0][0]).toBe(terminated)
      expect(ctx.options.stats['typst-compile-runs']).toBeUndefined()
    })

    it('tracks running state in the ProcessTable', async ctx => {
      // keep the compile "running" — CommandRunner.run hangs (no callback)
      ctx.TypstRunner.runTypst(ctx.projectId, ctx.options, ctx.callback)
      expect(ctx.TypstRunner.isRunning(ctx.projectId)).toBe(true)

      // kill: called with the container id recorded during the run
      await new Promise(resolve => {
        ctx.TypstRunner.killTypst(ctx.projectId, resolve)
      })
      expect(ctx.CommandRunner.kill).toHaveBeenCalledWith(
        'typst-container-id',
        expect.any(Function)
      )
    })

    it('killTypst without a running compile is a no-op callback', async ctx => {
      await new Promise(resolve => {
        ctx.TypstRunner.killTypst('unstarted-project', resolve)
      })
      expect(ctx.CommandRunner.kill).not.toHaveBeenCalled()
      expect(ctx.TypstRunner.isRunning('unstarted-project')).toBe(false)
    })
  })
})

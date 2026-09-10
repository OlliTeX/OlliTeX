/**
 * DockerRunner `allowedImages` gate (dedicated file).
 *
 * Why a separate file: under this runner (vitest `isolate: false` + the
 * `vi.resetModules()` in test/unit/setup.js) a module resolved in one test
 * keeps the mocked `@overleaf/settings` object captured during the FIRST
 * import of that file. The big DockerRunner.test.js file therefore sees a
 * STALE settings object in its gate tests (its `ctx.Settings` reassignment
 * is invisible to the already-resolved module), which made the gate tests
 * order-dependent. In this file the SUT is imported for the first time
 * under exactly these mocks, so the patch below deterministically reaches
 * the gate.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'


describe('DockerRunner (slim) — allowedImages gate', () => {
  let container

  beforeEach(async ctx => {
    container = {}

    ctx.Settings = {
      clsi: {
        docker: {
          image: 'default-typst-image',
          user: '33:33',
          env: { HOME: '/tmp', TYPST_PACKAGE_CACHE_PATH: '/tmp/.cache/typst' },
        },
      },
      path: {},
    }

    vi.doMock('@overleaf/settings', () => ({
      default: ctx.Settings,
    }))

    vi.doMock('@overleaf/logger', () => ({
      default: {
        debug() {},
        info() {},
        warn() {},
        error() {},
        err() {},
      },
    }))

    const Docker = class Docker {
      getContainer() {
        return container
      }
    }

    vi.doMock('dockerode', () => ({
      default: Docker,
    }))

    vi.doMock('../../../app/js/DockerLockManager', () => ({
      default: {
        runWithLock(key, runner, callback) {
          return runner(() => callback())
        },
      },
    }))

    vi.doMock('../../../app/js/LastProjectAccess', () => ({
      getLastProjectAccessTime: () => 0,
    }))

    ctx.Settings.path.sandboxedCompilesHostDirCompiles = '/host/compiles'
    ctx.Settings.path.sandboxedCompilesHostDirOutput = '/host/output'

    ctx.Docker = Docker
    // FIRST import of the SUT in this file — resolves the settings mock
    // registered above (deterministic gate visibility).
    ctx.DockerRunner = (
      await import('../../../app/js/DockerRunner.mjs')
    ).default
    ctx.DockerRunner._getContainerOptions = vi.fn().mockReturnValue({ name: 'stub' })
    ctx.DockerRunner._fingerprintContainer = vi.fn().mockReturnValue('fingerprint')
    ctx.DockerRunner._runAndWaitForContainer = vi
      .fn()
      .mockImplementation((options, volumes, timeout, cb) =>
        setImmediate(() => cb(null, { stdout: 'mock-output' }))
      )


    ctx.project_id = 'project-id-123'
    ctx.directory = '/var/lib/overleaf/data/compiles/xyz'
    ctx.env = {}
    ctx.timeout = 42000
    ctx.command = [
      'sh',
      '-c',
      'typst compile "$1" "$2" >> "$3"',
      '--',
      '$COMPILE_DIR/main.typ',
      'output.pdf',
      'output.log',
    ]
    ctx.callback = vi.fn()
  })

  it('rejects an image outside ALLOWED_IMAGES before building the container', async ctx => {
    ctx.Settings.clsi.docker.allowedImages = ['allowed/image:1']
    await new Promise(resolve =>
      ctx.DockerRunner.run(
        ctx.project_id,
        ctx.command,
        ctx.directory,
        'something/evil:1337',
        ctx.timeout,
        ctx.env,
        'compile',
        null,
        error => {
          ctx.callback(error)
          resolve()
        }
      )
    )
    expect(ctx.callback.mock.calls[0][0].message).toBe('image not allowed')
    // rejected before the container was built
    expect(ctx.DockerRunner._getContainerOptions).not.toHaveBeenCalled()
    expect(ctx.DockerRunner._runAndWaitForContainer).not.toHaveBeenCalled()
  })

  it('permits an allowed image and proceeds to build', async ctx => {
    ctx.Settings.clsi.docker.allowedImages = ['allowed/image:1']
    await new Promise(resolve =>
      ctx.DockerRunner.run(
        ctx.project_id,
        ctx.command,
        ctx.directory,
        'allowed/image:1',
        ctx.timeout,
        ctx.env,
        'compile',
        null,
        (error, output) => {
          ctx.callback(error, output)
          resolve()
        }
      )
    )
    expect(ctx.callback.mock.calls[0][0]).toBeNull()
    expect(ctx.DockerRunner._getContainerOptions).toHaveBeenCalled()
  })
})

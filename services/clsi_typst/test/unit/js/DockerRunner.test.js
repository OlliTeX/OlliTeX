import { vi, expect, describe, beforeEach, afterEach, it } from 'vitest'
import Path from 'node:path'

const MODULE_PATH = Path.join(
  import.meta.dirname,
  '..',
  '..',
  '..',
  'app',
  'js',
  'DockerRunner.mjs'
)

describe('DockerRunner (slim)', () => {
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

    // fake dockerode: DockerRunner does `new Docker()` at module scope, then
    // getContainer(name) on the instance.
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

    // clsi_typst uses DockerLockManager (per-container-ops lock) — passthrough
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
    ctx.DockerRunner = (await import(MODULE_PATH)).default

    ctx.project_id = 'project-id-123'
    ctx.directory = '/var/lib/overleaf/data/compiles/xyz'
    ctx.image = 'example.com/typst/image:3.0'
    ctx.env = {}
    ctx.timeout = 42000
    ctx.command = ['sh', '-c', 'typst compile "$1" "$2" >> "$3"', '--', '$COMPILE_DIR/main.typ', 'output.pdf', 'output.log']
    ctx.callback = vi.fn()
  })

  afterEach(ctx => {
    ctx.DockerRunner.stopContainerMonitor()
  })

  describe('run', () => {
    describe('_getContainerOptions (typst deltas)', () => {
      beforeEach(ctx => {
        ctx.volumes = { [ctx.directory]: '/compile' }
        ctx.options = ctx.DockerRunner._getContainerOptions(
          ctx.command,
          ctx.image,
          ctx.volumes,
          ctx.timeout,
          ctx.env,
          'compile',
          null
        )
      })

      it('clears the Entrypoint (pandoc/typst ships its own ENTRYPOINT)', ctx => {
        expect(ctx.options.Entrypoint).toEqual([])
      })

      it('runs as the settings user (uid 33; the typst image has no www-data)', ctx => {
        expect(ctx.options.User).toEqual('33:33')
      })

      it('disables the network (plan §3: NetworkDisabled, no per-group allow)', ctx => {
        expect(ctx.options.NetworkDisabled).toBe(true)
      })

      it('merges settings docker env (HOME + TYPST_PACKAGE_CACHE_PATH)', ctx => {
        expect(ctx.options.Env).toEqual(
          expect.arrayContaining([
            'HOME=/tmp',
            'TYPST_PACKAGE_CACHE_PATH=/tmp/.cache/typst',
          ])
        )
        expect(ctx.options.HostConfig.Runtime).toBeUndefined()
      })

      it('binds the compile directory to /compile rw', ctx => {
        expect(ctx.options.HostConfig.Binds).toEqual([
          `${ctx.directory}:/compile:rw`,
        ])
      })

      it('does not mount a read-only /tmp or synctex volumes', ctx => {
        // clsi mounts a read-only /tmp for synctex; clsi_typst must not (no
        // synctex surface, plan §7.5)
        expect(Object.keys(ctx.options.Volumes ?? {})).toEqual([])
        expect(ctx.options.HostConfig.Tmpfs).toBeUndefined()
      })

      it('sets Cmd to the rewritten compile command', ctx => {
        expect(ctx.options.Cmd).toEqual(ctx.command)
      })

      it('caps memory at the clsi 1 TiB convention', ctx => {
        expect(ctx.options.Memory).toEqual(1024 * 1024 * 1024 * 1024)
      })
    })

    describe('the run() flow', () => {
      beforeEach(async ctx => {
        ctx.DockerRunner._getContainerOptions = vi
          .fn()
          .mockReturnValue((ctx.options = { mockoptions: 'foo' }))
        ctx.DockerRunner._fingerprintContainer = vi
          .fn()
          .mockReturnValue((ctx.fingerprint = 'fingerprint'))
        ctx.DockerRunner._runAndWaitForContainer = vi
          .fn()
          .mockImplementation((options, volumes, timeout, cb) =>
            setImmediate(() => cb(null, { stdout: 'mock-output' }))
          )
      })

      it('rewrites $COMPILE_DIR to /compile in the command', async ctx => {
        await new Promise(resolve =>
          ctx.DockerRunner.run(
            ctx.project_id,
            ['$COMPILE_DIR/sub/file.typ', 'main.typ'],
            ctx.directory,
            ctx.image,
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
        expect(ctx.DockerRunner._getContainerOptions.mock.calls[0][0]).toEqual([
          '/compile/sub/file.typ',
          'main.typ',
        ])
      })

      // clsi renames project-<projectId>- to clsi-project-... per project type;
      // clsi_typst uses the typst- prefix (destroyOldContainers depends on it).
      it('names the container typst-project-<projectId>-<fingerprint>', async ctx => {
        await new Promise(resolve =>
          ctx.DockerRunner.run(
            ctx.project_id,
            ctx.command,
            ctx.directory,
            ctx.image,
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
        const passedOptions = ctx.DockerRunner._runAndWaitForContainer.mock.calls[0][0]
        expect(passedOptions.name).toEqual(
          `typst-project-${ctx.project_id}-${ctx.fingerprint}`
        )
      })

      it('rewrites the bind directory to the sandboxed compiles host dir', async ctx => {
        await new Promise(resolve =>
          ctx.DockerRunner.run(
            ctx.project_id,
            ctx.command,
            ctx.directory,
            ctx.image,
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
        const passedVolumes = ctx.DockerRunner._runAndWaitForContainer.mock.calls[0][1]
        expect(passedVolumes).toEqual({
          '/host/compiles/xyz': '/compile',
        })
      })

      it('uses the default image when image is null', async ctx => {
        await new Promise(resolve =>
          ctx.DockerRunner.run(
            ctx.project_id,
            ctx.command,
            ctx.directory,
            null,
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
        expect(
          ctx.DockerRunner._getContainerOptions.mock.calls[0][1]
        ).toEqual('default-typst-image')
      })
    })
  })

  describe('kill', () => {
    it('tolerates "cannot kill container ... is not running"', async ctx => {
      container.kill = vi.fn(cb =>
        cb(
          new Error('Cannot kill container typst-project-abc is not running')
        )
      )
      const finished = new Promise(resolve => {
        ctx.DockerRunner.kill('typst-project-abc', resolve)
      })
      await finished
      expect(container.kill).toHaveBeenCalled()
    })

    it('propagates other kill errors', async ctx => {
      container.kill = vi.fn(cb => cb(new Error('boom')))
      const promise = new Promise((resolve, reject) => {
        ctx.DockerRunner.kill('typst-project-abc', error => {
          if (error) {
            reject(error)
          } else {
            resolve()
          }
        })
      })
      await expect(promise).rejects.toThrow('boom')
    })
  })
})

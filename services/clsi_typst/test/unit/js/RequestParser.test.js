import { vi, expect, describe, beforeEach, it } from 'vitest'
import Path from 'node:path'

const MODULE_PATH = Path.join(
  import.meta.dirname,
  '..',
  '..',
  '..',
  'app',
  'js',
  'RequestParser.js'
)

// run RequestParser.parse and return { error, response } (callback form, as
// CompileController uses it)
async function parsedRequest(module, body) {
  return new Promise(resolve => {
    module.parse(body, (error, response) => resolve({ error, response }))
  })
}

describe('RequestParser', () => {
  // ONE stable settings object for the SUT's `@overleaf/settings` import:
  // under this runner (isolate: false + setup.js resetModules) the SUT keeps
  // its FIRST import's resolved object, so a fresh per-test ctx.settings is
  // invisible to it. Tests mutate this shared object (allowedImages set/delete)
  // and the SUT sees the mutation.
  const sharedSettings = {
    pdfCachingMinChunkSize: 0.1,
    clsi: { docker: {} },
  }

  beforeEach(async ctx => {
    ctx.settings = sharedSettings

    vi.doMock('@overleaf/settings', () => ({
      default: sharedSettings,
    }))

    vi.doMock('../../../../clsi/app/js/OutputCacheManager', () => ({
      default: { BUILD_REGEX: /^[0-9a-f]+-[0-9a-f]+$/ },
    }))

    ctx.RequestParser = (await import(MODULE_PATH)).default
  })

  describe('with no compile attribute', () => {
    it('should return an error', async ctx => {
      const { error, response } = await parsedRequest(ctx.RequestParser, {})
      expect(response).toBeUndefined()
      expect(error.message).toEqual(
        'top level object should have a compile attribute'
      )
    })
  })

  describe('compiler', () => {
    it('should set the compiler to typst by default', async ctx => {
      const { response } = await parsedRequest(ctx.RequestParser, {
        compile: { options: {} },
      })
      expect(response.compiler).toEqual('typst')
    })

    // clsi_typst: only one compiler; pdflatex/latex/etc are rejected
    // (plan §0 — single-compiler surface)
    it('should throw for pdflatex', async ctx => {
      const { error } = await parsedRequest(ctx.RequestParser, {
        compile: { options: { compiler: 'pdflatex' } },
      })
      expect(error.message).toEqual('compiler attribute should be one of: typst')
    })

    it('should throw for any invalid compiler', async ctx => {
      const { error } = await parsedRequest(ctx.RequestParser, {
        compile: { options: { compiler: 'xetex' } },
      })
      expect(error.message).toEqual('compiler attribute should be one of: typst')
    })

    it('should set the compiler to typst', async ctx => {
      const { response } = await parsedRequest(ctx.RequestParser, {
        compile: { options: { compiler: 'typst' } },
      })
      expect(response.compiler).toEqual('typst')
    })
  })

  describe('timeout', () => {
    it('should set the timeout to MAX_TIMEOUT (seconds) by default', async ctx => {
      const { response } = await parsedRequest(ctx.RequestParser, {
        compile: { options: {} },
      })
      expect(response.timeout).toEqual(ctx.RequestParser.MAX_TIMEOUT * 1000)
      expect(ctx.RequestParser.MAX_TIMEOUT).toEqual(600)
    })

    it('should clamp timeouts above MAX_TIMEOUT', async ctx => {
      const { response } = await parsedRequest(ctx.RequestParser, {
        compile: { options: { timeout: 9999 } },
      })
      expect(response.timeout).toEqual(ctx.RequestParser.MAX_TIMEOUT * 1000)
    })

    it('should set the timeout (in milliseconds)', async ctx => {
      const { response } = await parsedRequest(ctx.RequestParser, {
        compile: { options: { timeout: 42 } },
      })
      expect(response.timeout).toEqual(42 * 1000)
    })
  })

  describe('imageName', () => {
    beforeEach(ctx => {
      ctx.settings.clsi.docker.allowedImages = ['valid/image:1', 'other:2']
    })

    it('should set the imageName', async ctx => {
      const { response } = await parsedRequest(ctx.RequestParser, {
        compile: { options: { imageName: 'valid/image:1' } },
      })
      expect(response.imageName).toEqual('valid/image:1')
    })

    it('should throw an error for an image outside ALLOWED_IMAGES', async ctx => {
      const { error } = await parsedRequest(ctx.RequestParser, {
        compile: { options: { imageName: 'something/evil:1337' } },
      })
      expect(error.message).toEqual(
        'imageName attribute should be one of: valid/image:1, other:2'
      )
    })

    it('should accept any string when allowedImages is unset', async ctx => {
      delete ctx.settings.clsi.docker.allowedImages
      const { response } = await parsedRequest(ctx.RequestParser, {
        compile: { options: { imageName: 'anything/goes:42' } },
      })
      expect(response.imageName).toEqual('anything/goes:42')
    })
  })

  describe('resources', () => {
    it('should return a resource with no modified/url as undefined', async ctx => {
      const { response } = await parsedRequest(ctx.RequestParser, {
        compile: { resources: [{ path: 'main.typ', content: 'a' }] },
      })
      expect(response.resources).toEqual([
        {
          path: 'main.typ',
          modified: undefined,
          url: undefined,
          fallbackURL: undefined,
          content: 'a',
        },
      ])
    })

    it('should return an error for a resource without a path', async ctx => {
      const { error } = await parsedRequest(ctx.RequestParser, {
        compile: { resources: [{ content: 'a' }] },
      })
      expect(error.message).toEqual(
        'all resources should have a path attribute'
      )
    })

    it('should return an error when neither url nor content is given', async ctx => {
      const { error } = await parsedRequest(ctx.RequestParser, {
        compile: { resources: [{ path: 'main.typ' }] },
      })
      expect(error.message).toEqual(
        'all resources should have either a url or content attribute'
      )
    })
  })

  describe('rootResourcePath', () => {
    it("should set the root resource path to 'main.typ' by default", async ctx => {
      const { response } = await parsedRequest(ctx.RequestParser, {
        compile: { options: {} },
      })
      expect(response.rootResourcePath).toEqual('main.typ')
    })

    it('should return the given path', async ctx => {
      const { response } = await parsedRequest(ctx.RequestParser, {
        compile: { rootResourcePath: 'sub/dir/nested.typ' },
      })
      expect(response.rootResourcePath).toEqual('sub/dir/nested.typ')
    })

    it('should reject relative (traversal) paths', async ctx => {
      const { error } = await parsedRequest(ctx.RequestParser, {
        compile: { rootResourcePath: '../../etc/passwd' },
      })
      expect(error.message).toEqual('relative path in root resource')
    })
  })

  describe('baseHistoryVersion', () => {
    it('should echo the number', async ctx => {
      const { response } = await parsedRequest(ctx.RequestParser, {
        compile: { baseHistoryVersion: 42, options: {} },
      })
      expect(response.baseHistoryVersion).toEqual(42)
    })

    it('should error for a string', async ctx => {
      const { error } = await parsedRequest(ctx.RequestParser, {
        compile: { baseHistoryVersion: '42', options: {} },
      })
      expect(error.message).toEqual(
        'baseHistoryVersion attribute should be a number'
      )
    })
  })

  describe('buildId', () => {
    it('should accept a build id matching OUTPUT build regex', async ctx => {
      const { response } = await parsedRequest(ctx.RequestParser, {
        compile: { options: { buildId: '1a2b3c4d-5e6f7a8b' } },
      })
      expect(response.buildId).toEqual('1a2b3c4d-5e6f7a8b')
    })

    it('should reject a malformed build id', async ctx => {
      const { error } = await parsedRequest(ctx.RequestParser, {
        compile: { options: { buildId: 'not-a-build-id!' } },
      })
      expect(error.message).toEqual(
        'buildId attribute does not match regex /^[0-9a-f]+-[0-9a-f]+$/'
      )
    })
  })

  describe('stopOnFirstError', () => {
    it('should default to false', async ctx => {
      const { response } = await parsedRequest(ctx.RequestParser, {
        compile: { options: {} },
      })
      expect(response.stopOnFirstError).toBe(false)
    })

    it('should be settable', async ctx => {
      const { response } = await parsedRequest(ctx.RequestParser, {
        compile: { options: { stopOnFirstError: true } },
      })
      expect(response.stopOnFirstError).toBe(true)
    })
  })
})

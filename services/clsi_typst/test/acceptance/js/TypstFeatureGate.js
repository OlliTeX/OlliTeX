import Settings from '@overleaf/settings'
import Client from './helpers/Client.js'
import ClsiApp from './helpers/ClsiApp.js'
import { expect } from 'chai'

const TYPST_DOC = `\
#let name = "world"

Hello, #name!
`

// Feature gate (TYPST_INTEGRATION_PLAN §0): clsi_typst starts DISABLED by
// default; while off, every route returns 501. The gate is a plain Settings
// field read live by the middleware in app.js, so flipping the shared
// singleton exercises the live behaviour (the test env sources
// COMPILE_TYPEST_ENABLED=true, i.e. enabled by default here).
describe('Typst feature gate', function () {
  before(async function () {
    await ClsiApp.ensureRunning()
    this.originalEnabled = Settings.compile_typst_enabled
  })

  after(function () {
    // restore shared singleton state; later files rely on it being on
    Settings.compile_typst_enabled = this.originalEnabled
  })

  describe('when disabled', function () {
    before(function () {
      Settings.compile_typst_enabled = false
    })

    after(function () {
      Settings.compile_typst_enabled = true
    })

    beforeEach(function () {
      this.project_id = Client.randomId()
    })

    it('compile returns 501', async function () {
      const error = await Client.compile(this.project_id, {
        rootResourcePath: 'main.typ',
        resources: [{ path: 'main.typ', content: TYPST_DOC }],
        options: {},
      }).catch(e => e)
      expect(error).to.exist
      expect(error.response.status).to.equal(501)
      expect(error.body).to.include('typst compilation not enabled')
    })

    it('stop compile returns 501', async function () {
      const error = await Client.stopCompile(this.project_id).catch(e => e)
      expect(error).to.exist
      expect(error.response.status).to.equal(501)
    })

    it('clear cache returns 501', async function () {
      const error = await Client.clearCache(this.project_id).catch(e => e)
      expect(error).to.exist
      expect(error.response.status).to.equal(501)
    })

    it('status returns 501', async function () {
      const error = await Client.status(this.project_id).catch(e => e)
      expect(error).to.exist
      expect(error.response.status).to.equal(501)
    })
  })

  describe('when enabled', function () {
    before(function () {
      Settings.compile_typst_enabled = true
    })

    beforeEach(async function () {
      this.project_id = Client.randomId()
    })

    it('compile returns 200 (not 501)', async function () {
      try {
        this.body = await Client.compile(this.project_id, {
          rootResourcePath: 'main.typ',
          resources: [{ path: 'main.typ', content: TYPST_DOC }],
          options: {},
        })
      } catch (e) {
        this.error = e
      }
      expect(
        this.error,
        'compile should not fail when the gate is on'
      ).to.not.exist
    })
  })
})

import Client from './helpers/Client.js'
import ClsiApp from './helpers/ClsiApp.js'
import { expect } from 'chai'

// clsi reference: test/acceptance/js/AllowedImageNamesTests.js, reduced to
// what clsi_typst exposes (no sync routes — no synctex, §7.5). The valid
// image is process.env.TYPST_DOCKER_IMAGE (the default compile image, part
// of ALLOWED_IMAGES per test/acceptance/env.sh).
describe('Allowed image names (Typst)', function () {
  const TYPST_DOC = `\
#let name = "world"

Hello, #name!
`

  before(async function () {
    await ClsiApp.ensureRunning()
  })

  describe('with a valid name', function () {
    before(async function () {
      this.project_id = Client.randomId()
      this.request = {
        rootResourcePath: 'main.typ',
        resources: [{ path: 'main.typ', content: TYPST_DOC }],
        options: {
          imageName: process.env.TYPST_DOCKER_IMAGE,
        },
      }
    })

    beforeEach(async function () {
      try {
        this.body = await Client.compile(this.project_id, this.request)
      } catch (error) {
        this.error = error
      }
    })

    it('should return success', function () {
      expect(this.error).not.to.exist
      expect(this.body.compile.status).to.equal('success')
    })

    it('should return a PDF', function () {
      const pdf = this.body && Client.getOutputFile(this.body, 'pdf')
      expect(pdf).to.exist
    })
  })

  describe('with an invalid name', function () {
    before(async function () {
      this.project_id = Client.randomId()
      this.request = {
        rootResourcePath: 'main.typ',
        resources: [{ path: 'main.typ', content: TYPST_DOC }],
        options: {
          imageName: 'something/evil:1337',
        },
      }
    })

    beforeEach(async function () {
      try {
        this.body = await Client.compile(this.project_id, this.request)
      } catch (error) {
        this.error = error
      }
    })

    it('should return 500', function () {
      expect(this.error).to.exist
      expect(this.error.response.status).to.equal(500)
    })

    it('should not return a PDF', function () {
      const pdf = this.body && Client.getOutputFile(this.body, 'pdf')
      expect(pdf).to.not.exist
    })
  })
})

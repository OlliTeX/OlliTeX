import { promisify } from 'node:util'
import Client from './helpers/Client.js'
import ClsiApp from './helpers/ClsiApp.js'
import SlowCompileDoc from './helpers/SlowCompileDoc.js'
import TypstRunner from '../../../app/js/TypstRunner.js'
import { expect } from 'chai'

const sleep = promisify(setTimeout)

// clsi reference: test/acceptance/js/StopCompile.js. The recursive \x macro
// (busy compile) is replaced by SlowCompileDoc (range overflow, ~28s, no
// PDF); the rest of the contract is the same.
describe('Stop compile (Typst)', function () {
  before(async function () {
    this.project_id = Client.randomId()
    this.request = {
      resources: [{ path: 'main.typ', content: SlowCompileDoc }],
      options: {
        timeout: 100,
      }, // seconds
    }
    await ClsiApp.ensureRunning()

    // start the compile in the background
    this.compilePromise = Client.compile(this.project_id, this.request)
      .then(body => {
        this.compileResult = { body }
      })
      .catch(error => {
        this.compileResult = { error }
      })

    // Deterministic start gate (clsi sleeps a fixed 1s and hopes; here we
    // wait for the app to register the compile, then a grace period so the
    // docker container is created and echo-written output.log before the
    // kill lands):
    const deadline = Date.now() + 12000
    while (!TypstRunner.isRunning(this.project_id)) {
      if (Date.now() > deadline) {
        throw new Error('typst compile did not start')
      }
      await sleep(100)
    }
    await sleep(1000)

    const res = await Client.stopCompile(this.project_id)
    this.stopResult = { res }

    // allow the killed compile to terminate the original compile request
    await this.compilePromise
  })

  it('should return 204 for the stop', function () {
    expect(this.stopResult.error).not.to.exist
    expect(this.stopResult.res.status).to.equal(204)
  })

  it('should force a compile response with an error status', function () {
    expect(this.compileResult.error).not.to.exist
    expect(this.compileResult.body.compile.status).to.equal('terminated')
    expect(this.compileResult.body.compile.error).to.equal('terminated')
  })

  it('should return the log output file name', function () {
    const outputFilePaths = this.compileResult.body.compile.outputFiles.map(
      x => x.path
    )
    // killed mid-compile: only the early-written output.log exists (clsi
    // would also have output.synctex(busy); Typst has no synctex, §7.5)
    expect(outputFilePaths).to.include('output.log')
    expect(outputFilePaths).to.not.include('output.pdf')
  })

  it('should carry the buildId in the terminated envelope', function () {
    expect(this.compileResult.body.compile.buildId).to.match(
      /^[0-9a-f]+-[0-9a-f]+$/
    )
  })

  it('should work with not pending compile', async function () {
    const res = await Client.stopCompile(this.project_id)
    expect(res.status).to.equal(204)
  })
})

import { promisify } from 'node:util'
import fs from 'node:fs'
import Path from 'node:path'
import Client from './helpers/Client.js'
import ClsiApp from './helpers/ClsiApp.js'
import SlowCompileDoc from './helpers/SlowCompileDoc.js'
import TypstRunner from '../../../app/js/TypstRunner.js'
import Settings from '@overleaf/settings'
import { expect } from 'chai'

const sleep = promisify(setTimeout)

// clsi reference: test/acceptance/js/ClearCache.js. DELETE /project/:id stops
// the pending compile (the compile request resolves "terminated") and removes
// the compile dir; clsi_typst additionally wipes output/{id} (no CLSI cache
// sharding to reconcile, plan §3.2).
describe('Clear cache (Typst)', function () {
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

    // deterministic start gate + container-creation grace (see StopCompile)
    const deadline = Date.now() + 12000
    while (!TypstRunner.isRunning(this.project_id)) {
      if (Date.now() > deadline) {
        throw new Error('typst compile did not start')
      }
      await sleep(100)
    }
    await sleep(1000)

    const res = await Client.clearCache(this.project_id)
    this.clearResult = { res }

    // allow the killed compile to terminate, and the clear to finish
    await this.compilePromise
  })

  it('should return 204 for the clearing', function () {
    expect(this.clearResult.error).not.to.exist
    expect(this.clearResult.res.status).to.equal(204)
  })

  it('should emit a compile response with terminated status', function () {
    expect(this.compileResult.error).not.to.exist
    expect(this.compileResult.body.compile.status).to.equal('terminated')
    expect(this.compileResult.body.compile.error).to.equal('terminated')
  })

  it('should return the log output file name', function () {
    const outputFilePaths = this.compileResult.body.compile.outputFiles.map(
      x => x.path
    )
    expect(outputFilePaths).to.include('output.log')
    expect(outputFilePaths).to.not.include('output.pdf')
  })

  it('should remove the compile directory', function () {
    expect(
      fs.existsSync(Path.join(Settings.path.compilesDir, this.project_id))
    ).to.be.false
    expect(
      fs.existsSync(Path.join(Settings.path.outputDir, this.project_id))
    ).to.be.false
  })

  it('should work with not pending compile', async function () {
    const res = await Client.clearCache(this.project_id)
    expect(res.status).to.equal(204)
  })
})

import fs from 'node:fs'
import Path from 'node:path'
import { fileURLToPath } from 'node:url'
import Client from './helpers/Client.js'
import ClsiApp from './helpers/ClsiApp.js'
import Settings from '@overleaf/settings'
import { expect } from 'chai'

// clsi reference: test/acceptance/js/BrokenLatexFileTests.js, adapted to the
// Typst error contract (plan §3.4): the wrapper shell exits 0, so a document
// that fails to compile is a 200 "failure" envelope with the diagnostic in
// output.log (surfaced to web via the log-parser), not a 5xx.
describe('Typst compile errors go to output.log', function () {
  before(async function () {
    // 04-syntax-error.typ is a committed fixture with a known expected
    // diagnostic (examples/errors/04-syntax-error.typ.expected.txt).
    const fixturePath = Path.resolve(
      Path.dirname(fileURLToPath(import.meta.url)),
      '../../../examples/errors/04-syntax-error.typ'
    )
    this.project_id = Client.randomId()
    this.request = {
      rootResourcePath: 'main.typ',
      resources: [
        { path: 'main.typ', content: fs.readFileSync(fixturePath, 'utf8') },
      ],
      options: {
        timeout: 60,
      },
    }
    await ClsiApp.ensureRunning()
    this.body = await Client.compile(this.project_id, this.request)
  })

  it('should return 200 with a failure status', function () {
    expect(this.body).to.exist
    expect(this.body.compile.status).to.equal('failure')
  })

  it('should return output.log in outputFiles', function () {
    const outputFilePaths = this.body.compile.outputFiles.map(x => x.path)
    expect(outputFilePaths).to.include('output.log')
  })

  it('should not return output.pdf', function () {
    // a broken document produces no PDF at all (the wrapper exits 0, so
    // the compile is a 200 "failure" with diagnostics in output.log only)
    const outputFilePaths = this.body.compile.outputFiles.map(x => x.path)
    expect(outputFilePaths).to.not.include('output.pdf')
  })

  it('should contain the error messages in output.log', function () {
    const logFile = this.body.compile.outputFiles.find(
      file => file.path === 'output.log'
    )
    expect(logFile).to.exist
    const build = logFile.build
    const logPath = Path.join(
      Settings.path.outputDir,
      this.project_id,
      'generated-files',
      build,
      'output.log'
    )
    const log = fs.readFileSync(logPath, 'utf8')
    // the diagnostic line format (examples/errors/04-syntax-error.typ.expected.txt)
    expect(log).to.match(/^error: .*/m)
  })
})

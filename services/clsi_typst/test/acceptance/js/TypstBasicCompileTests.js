import Client from './helpers/Client.js'
import ClsiApp from './helpers/ClsiApp.js'
import { expect } from 'chai'
import fs from 'node:fs'
import Path from 'node:path'
import Settings from '@overleaf/settings'

const TYPST_DOC = `\
#let name = "world"

Hello, #name!

This is a plain Typst document.
`

describe('Typst basic compile', function () {
  before(async function () {
    this.project_id = Client.randomId()
    this.request = {
      rootResourcePath: 'main.typ',
      resources: [{ path: 'main.typ', content: TYPST_DOC }],
      options: {
        compiler: 'typst',
        timeout: 100,
      },
    }
    await ClsiApp.ensureRunning()

    // clsi runs in docker with SANDBOXED_COMPILES; make the compile/output
    // dirs world-writable so the uid-33 container can write output.pdf (the
    // P0 spike convention; production runs clsi as www-data/uid 33).
    for (const dir of [Settings.path.compilesDir, Settings.path.outputDir]) {
      fs.rmSync(dir, { recursive: true, force: true })
      fs.mkdirSync(dir, { recursive: true })
      try {
        fs.chownSync(dir, 33, 33)
      } catch (e) {
        fs.chmodSync(dir, 0o777)
      }
    }

    try {
      this.body = await Client.compile(this.project_id, this.request)
    } catch (error) {
      this.error = error
    }
  })

  it('should return 200 and compile.status success', function () {
    expect(this.error, 'compile threw').to.not.exist
    expect(this.body.compile.status).to.equal('success')
  })

  it('should return the PDF', function () {
    const pdf = Client.getOutputFile(this.body, 'pdf')
    expect(pdf, 'no output.pdf').to.exist
    expect(pdf.type).to.equal('pdf')
  })

  it('should return the log', function () {
    const log = Client.getOutputFile(this.body, 'log')
    expect(log, 'no output.log').to.exist
    expect(log.type).to.equal('log')
  })

  // clsi contract (TYPST_INTEGRATION_PLAN §3.3): buildId is a hex string
  // (^[0-9a-f]+-[0-9a-f]+$), NOT an int.
  it('returns a hex buildId (^[0-9a-f]+-[0-9a-f]+$)', function () {
    const buildId = this.body.compile.buildId
    expect(buildId, `buildId is ${JSON.stringify(buildId)}`).to.match(
      /^[0-9a-f]+-[0-9a-f]+$/
    )
  })

  // Contract envelope: the keys clsi always populates. baseHistoryVersion is
  // only echoed when the request provides it (clsi controller §3.3), and
  // clsiCacheShard is always undefined in clsi_typst (no CLSI cache shards),
  // so neither key is serialized by res.send when undefined. The request in
  // this suite does not send baseHistoryVersion, so it is absent here.
  it('returns the clsi envelope keys', function () {
    const compile = this.body.compile
    for (const key of [
      'status',
      'error',
      'stats',
      'timings',
      'buildId',
      'isSpotInstance',
      'outputUrlPrefix',
      'outputFiles',
    ]) {
      expect(Object.keys(compile), 'missing ' + key).to.include(key)
    }
    expect(compile.error).to.be.null
    for (const key of [
      'clsiCacheShard',
      'instanceType',
      'zone',
    ]) {
      expect(Object.keys(compile), 'clsi_typst sends ' + key + ' as undefined (absent in JSON)').to.not.include(key)
    }
    for (const file of compile.outputFiles) {
      for (const key of ['url', 'path', 'type', 'build']) {
        expect(Object.keys(file), 'missing file key ' + key).to.include(key)
      }
      // clsi attaches size only to output.pdf (OutputCacheManager.collectOutputPdfSize)
      if (file.path === 'output.pdf') {
        expect(Object.keys(file), 'missing size on output.pdf').to.include('size')
      }
      expect(file.url).to.include('/output/')
      expect(file.url).to.include('build/')
    }
  })

  // No synctex: never an output.txt / .synctex.gz entry (§7.5 / F1.19).
  it('outputFiles must not include synctex entries', function () {
    for (const file of this.body.compile.outputFiles) {
      expect(file.path).to.not.match(/\.synctex\.gz$/)
      expect(file.path).to.not.equal('output.txt')
    }
  })

  // P1 exit gate: the PDF exists on disk and is a real PDF (openable).
  it('output.pdf on disk is a valid PDF', function () {
    const build = this.body.compile.outputFiles[0].build
    const pdfDir = Path.join(
      Settings.path.outputDir,
      this.project_id,
      'generated-files',
      build
    )
    const pdfPath = Path.join(pdfDir, 'output.pdf')
    expect(fs.existsSync(pdfPath), 'output.pdf missing on disk').to.be.true
    const magic = fs.readFileSync(pdfPath).slice(0, 5)
    expect(magic.toString()).to.equal('%PDF-')
  })
})

import Client from './helpers/Client.js'
import ClsiApp from './helpers/ClsiApp.js'
import { expect } from 'chai'
import Settings from '@overleaf/settings'
import fs from 'node:fs'

// Deterministic test document (wordometer 0.1.5 semantics, verified in this
// env against pandoc/typst:3-alpine / Typst 0.14.2):
//
//   = My Document Title           heading: 3 words
//   This is the lead paragraph of the document.   body: 8
//   = Section One                 heading: 2
//   Alpha beta gamma is three words plus.         body: 7
//   = Section Two                 heading: 2
//   Delta epsilon zeta makes the count complete.  body: 7
//
//   heading words: 3 + 2 + 2 = 7  (numHeadings: 3)
//   body words:    8 + 7 + 7     = 22
//   wordometer #total-words counts *all* content (headings included):
//   textWords = 22 + 7 = 29
//
//   (v1 parity note: texcount's "Words in text" is body-only (22 here);
//    wordometer's #total-words includes headings, so clsi_typst reports
//    29 for textWords in v1. The plan §3.6 wordometer contract maps
//    #total-words -> textWords and the heading stats -> headWords/headers.
//    The rendered marker itself lives on its own final page (hard
//    #pagebreak) so it does not count into #total-words.)
//
// The response keeps clsi's texcount envelope (§3.6): web's ClsiManager
// reads `textWords`/`headers` from `{ texcount: { ... } }`.
const TEST_DOC = [
  '= My Document Title',
  '',
  'This is the lead paragraph of the document.',
  '',
  '= Section One',
  '',
  'Alpha beta gamma is three words plus.',
  '',
  '= Section Two',
  '',
  'Delta epsilon zeta makes the count complete.',
  '',
].join('\n')

const EXPECTED_WORDCOUNTER = {
  encode: 'utf-8',
  textWords: 29,
  headWords: 7,
  outside: 0,
  headers: 3,
  elements: 0,
  mathInline: 0,
  mathDisplay: 0,
  errors: 0,
  messages: '',
}

function _prepareDirs() {
  for (const dir of [Settings.path.compilesDir, Settings.path.outputDir]) {
    fs.rmSync(dir, { recursive: true, force: true })
    fs.mkdirSync(dir, { recursive: true })
    try {
      fs.chownSync(dir, 33, 33)
    } catch (e) {
      fs.chmodSync(dir, 0o777)
    }
  }
}

describe('Typst wordcount (wordometer)', function () {
  before(async function () {
    this.duration = 30 * 1000
    this.project_id = Client.randomId()
    this.request = {
      rootResourcePath: 'main.typ',
      resources: [{ path: 'main.typ', content: TEST_DOC }],
      options: { compiler: 'typst', timeout: 100 },
    }
    await ClsiApp.ensureRunning()
    _prepareDirs()
  })

  describe('POST (carries the project state, clsi wordcountWithSync shape)', function () {
    before(async function () {
      try {
        this.body = await Client.wordcount(
          this.project_id,
          'main.typ',
          this.request
        )
      } catch (error) {
        this.error = error
      }
    })
    it('should return wordcount info', function () {
      expect(this.error, 'wordcount threw').to.not.exist
      expect(this.body).to.deep.equal({
        texcount: EXPECTED_WORDCOUNTER,
      })
    })
  })

  describe('GET after POST (no request body this time)', function () {
    it('counts the synced project from disk', async function () {
      const result = await Client.wordcount(this.project_id, 'main.typ')
      expect(result).to.deep.equal({ texcount: EXPECTED_WORDCOUNTER })
    })
  })
})

describe('Typst wordcount without a prior compile', function () {
  before(async function () {
    this.duration = 20 * 1000
    this.project_id = Client.randomId()
    this.request = {
      rootResourcePath: 'main.typ',
      resources: [{ path: 'main.typ', content: TEST_DOC }],
      options: { compiler: 'typst' },
    }
    await ClsiApp.ensureRunning()
    _prepareDirs()
  })

  it('should sync the resources and return counts from the POST alone', async function () {
    // deliberately no Client.compile(): this clsi has never seen the project,
    // as when the editor served the PDF straight from clsi-cache
    const result = await Client.wordcount(
      this.project_id,
      'main.typ',
      this.request
    )
    expect(result).to.deep.equal({ texcount: EXPECTED_WORDCOUNTER })
  })

  it('should leave the project on disk for a second GET', async function () {
    const result = await Client.wordcount(this.project_id, 'main.typ')
    expect(result.texcount.messages).to.equal('')
    expect(result.texcount.textWords).to.deep.equal(29)
    expect(result.texcount.headers).to.deep.equal(3)
  })
})

describe('Typst wordcount degradation', function () {
  before(async function () {
    this.duration = 20 * 1000
    await ClsiApp.ensureRunning()
    _prepareDirs()
  })

  it('should fall back to wc when the wordometer compile cannot run', async function () {
    this.project_id = Client.randomId()
    // A document that does not compile, so the wordometer marker cannot be
    // produced; word count degrades (plan §9 mitigation: it is a wordcount
    // fallback, not a correctness requirement) — rough wc count,
    // texcount-shape envelope with the heading fields 0.
    const result = await Client.wordcount(this.project_id, 'main.typ', {
      rootResourcePath: 'main.typ',
      resources: [
        {
          path: 'main.typ',
          content: '#let x = 1\n#let x = { broken { } }\nSome text to wc.\n',
        },
      ],
      options: { compiler: 'typst' },
    })
    expect(result.texcount.messages).to.equal('')
    expect(result.texcount.textWords).to.be.greaterThan(0)
    expect(result.texcount.headers).to.equal(0)
  })

  it('should report a 404 when nothing has been synced', async function () {
    this.project_id = Client.randomId()
    let error
    try {
      await Client.wordcount(this.project_id, 'main.typ')
      // fetch-utils throws on non-2xx
      this.skip = true
    } catch (error2) {
      error = error2
    }
    expect(
      error,
      'expected the missing-compile-dir wordcount to be rejected'
    ).to.exist
  })
})

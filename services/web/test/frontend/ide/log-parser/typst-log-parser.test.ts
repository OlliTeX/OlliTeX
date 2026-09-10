/**
 * Typst compile-log parser tests (F3.5 + F3.9 corpus).
 *
 * The corpus fixtures mirror clsi_typst/examples/errors/* (the live-typed
 * P0 corpus, typst 0.14.2 — see `modules/typst/test/fixtures/typst-corpus/`;
 * that directory is a copy of clsi_typst/examples/errors/*.typ{, .expected.txt}
 * kept in sync with clsi_typst as single source of truth).
 *
 * Run via `yarn test:frontend`.
 */
import { expect } from 'chai'
import fs from 'fs'
import path from 'path'
import {
  parseTypstLog,
  parseTypstLogEntries,
} from '../../../../frontend/js/ide/log-parser/typst-log-parser'

// The fixtures live under modules/typst/test/fixtures (see F3.9 ledger note:
// clsi_typst keeps the same files as source of truth; this copy is used by
// the frontend).
const FIXTURE_DIR = path.resolve(__dirname, '../../../../modules/typst/test/fixtures/typst-corpus')

// clsi_typst prepends a banner before typst's own diagnostics (the
// `echo "typst $(typst --version)"` line from P1 buildTypstCompileCommand);
// the parser must skip it (the fixtures carry it verbatim).

describe('parseTypstLog (F3.5)', function () {
  it('parses the 01-undefined-ident corpus case (single error, file:line:col)', function () {
    const raw = fs.readFileSync(
      path.join(FIXTURE_DIR, '01-undefined-ident.typ.expected.txt'),
      'utf8',
    )
    const entries = parseTypstLog(raw)
    expect(entries).to.have.length(1)
    const e = entries[0]
    expect(e.level).to.equal('error')
    expect(e.message).to.contain('unknown variable')
    expect(e.message).to.contain('x')
    expect(e.file).to.equal('main.typ')
    expect(e.line).to.equal(7)
    expect(e.column).to.equal(14)
  })

  it('parses the 02-missing-args corpus case', function () {
    const raw = fs.readFileSync(
      path.join(FIXTURE_DIR, '02-missing-args.typ.expected.txt'),
      'utf8',
    )
    const entries = parseTypstLog(raw)
    expect(entries).to.have.length(1)
    expect(entries[0].level).to.equal('error')
    expect(entries[0].file).to.equal('main.typ')
    expect(entries[0].line).to.equal(4)
    expect(entries[0].column).to.equal(1)
  })

  it('parses the 03-unknown-include corpus case', function () {
    const raw = fs.readFileSync(
      path.join(FIXTURE_DIR, '03-unknown-include.typ.expected.txt'),
      'utf8',
    )
    const entries = parseTypstLog(raw)
    expect(entries).to.have.length(1)
    expect(entries[0].file).to.equal('main.typ')
    expect(entries[0].line).to.equal(3)
    expect(entries[0].column).to.equal(8)
    expect(entries[0].message).to.contain('file not found')
  })

  it('parses the 04-syntax-error corpus case (multiple errors, one per block)', function () {
    const raw = fs.readFileSync(
      path.join(FIXTURE_DIR, '04-syntax-error.typ.expected.txt'),
      'utf8',
    )
    const entries = parseTypstLog(raw)
    expect(entries).to.have.length(2)
    // typst emits both (unclosed delimiter + invalid `#` char); we parse
    // them as two error entries in order of appearance.
    expect(entries[0].file).to.equal('main.typ')
    expect(entries[0].line).to.equal(3)
    expect(entries[0].column).to.equal(9)
    expect(entries[0].message).to.contain('unclosed delimiter')
    expect(entries[1].file).to.equal('main.typ')
    expect(entries[1].line).to.equal(4)
    expect(entries[1].column).to.equal(0)
    expect(entries[1].message).to.contain('not valid in code')
  })

  it('parses the 05-weak-include corpus case', function () {
    const raw = fs.readFileSync(
      path.join(FIXTURE_DIR, '05-weak-include.typ.expected.txt'),
      'utf8',
    )
    const entries = parseTypstLog(raw)
    expect(entries).to.have.length(1)
    const e = entries[0]
    expect(e.level).to.equal('error')
    expect(e.file).to.equal('main.typ')
    // clsi_typst corpus (live typst 0.14.2) location is `main.typ:3:8` —
    // line 3, column 8 (the include arg). The parser passes the
    // location line through verbatim (file, line, column 0-based typst
    // offset) — exactly what the click-to-source hook needs.
    expect(e.line).to.equal(3)
    expect(e.column).to.equal(8)
  })

  it('ignores the clsi_typst typst banner line', function () {
    const raw =
      'typst 0.14.2 (1e0913a)\n' +
      fs.readFileSync(
        path.join(FIXTURE_DIR, '01-undefined-ident.typ.expected.txt'),
        'utf8',
      )
    const entries = parseTypstLog(raw)
    expect(entries).to.have.length(1)
    expect(entries[0].level).to.equal('error')
  })

  it('returns [] for empty / null input', function () {
    expect(parseTypstLog('')).to.deep.equal([])
    expect(parseTypstLog(null)).to.deep.equal([])
    expect(parseTypstLog(undefined)).to.deep.equal([])
  })

  it('parseTypstLogEntries groups errors and warnings', function () {
    const raw =
      'error: a\n' +
      '  ┌─ main.typ:1:1\n' +
      '  │\n' +
      '1 │ foo\n' +
      '  │ ^\n' +
      '\n' +
      'warning: b\n' +
      '  ┌─ main.typ:2:1\n' +
      '  │\n' +
      '2 │ bar\n' +
      '  │ ^\n'
    const r = parseTypstLogEntries(raw)
    expect(r.errors).to.have.length(1)
    expect(r.warnings).to.have.length(1)
    expect(r.typesetting).to.deep.equal([])
    expect(r.errors[0].file).to.equal('main.typ')
    expect(r.errors[0].line).to.equal(1)
    expect(r.errors[0].column).to.equal(1)
  })
})

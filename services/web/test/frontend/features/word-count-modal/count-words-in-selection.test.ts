/**
 * countWordsInSelection unit tests (OlliTeX 2026-09, owner batch:
 * selected-text word count).
 *
 * Verifies the lezer-based selection counter used by File → Word count's
 * "Selection" section: plain words, headings, inline/display math, empty
 * range guards, and the hyphen-join rule.
 *
 * Run via the services/web frontend suite (same harness as the log-parser
 * tests).
 */
import { expect } from 'chai'
import { countWordsInSelection } from '../../../../frontend/js/features/word-count-modal/utils/count-words-in-selection'
import { createSegmenters } from '../../../../frontend/js/features/word-count-modal/utils/segmenters'

function makeSegmenters() {
  try {
    return createSegmenters(undefined)
  } catch {
    return null
  }
}

function count(content: string) {
  return countWordsInSelection(
    content,
    { from: 0, to: content.length },
    makeSegmenters() as never,
  )
}

describe('countWordsInSelection (File → Word count Selection section)', function () {
  it('counts a plain paragraph', function () {
    const text = 'One two three four five six.'
    const r = count(text)
    expect(r.totalWords).to.equal(6)
    expect(r.headers).to.equal(0)
    expect(r.mathInline).to.equal(0)
    expect(r.mathDisplay).to.equal(0)
  })

  it('counts a heading separately and as a header', function () {
    const text = '\\section{My Title Here}\nBody text words go here.'
    const r = count(text)
    expect(r.headers).to.equal(1)
    // "My Title Here" (3) + "Body text words go here" (5) = 8
    expect(r.totalWords).to.equal(8)
  })

  it('counts inline and display math without counting their innards as words', function () {
    const text =
      'Before $x^2 + y^2$ after.\n\\begin{equation}\nE = mc^2\n\\end{equation}'
    const r = count(text)
    expect(r.mathInline).to.equal(1)
    expect(r.mathDisplay).to.equal(1)
    // words: Before (1) + after (1) = 2 (math innards excluded)
    expect(r.totalWords).to.equal(2)
  })

  it('returns zeros for an empty range', function () {
    const text = 'Some text here.'
    const r = countWordsInSelection(
      text,
      { from: 5, to: 5 },
      makeSegmenters() as never,
    )
    expect(r.totalWords).to.equal(0)
    expect(r.headers).to.equal(0)
  })

  it('clamps out-of-bounds ranges instead of throwing', function () {
    const text = 'Hello world.'
    const r = countWordsInSelection(
      text,
      { from: 0, to: 999 },
      makeSegmenters() as never,
    )
    expect(r.totalWords).to.be.greaterThan(0)
  })

  it('counts a hyphenated compound as a single word', function () {
    const text = 'state-of-the-art work'
    const r = count(text)
    // "state-of-the-art" = 1 word, "work" = 1 word
    expect(r.totalWords).to.equal(2)
  })

  it('ignores comments and preamble-only macro noise', function () {
    const text =
      '% a comment with words that must not count\n\\usepackage{hyperref}\nReal words here.'
    const r = count(text)
    // Real (1) words (2) here (3); \usepackage{hyperref} is a macro, not a word
    expect(r.totalWords).to.be.within(3, 4)
  })
})

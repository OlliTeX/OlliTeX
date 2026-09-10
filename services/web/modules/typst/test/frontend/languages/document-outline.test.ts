/**
 * F3.6 — Typst outline, unit tests.
 *
 * Covers the two Node-safe pieces of the typst outline. No lezer wasm grammar is
 * loaded here, so nothing in this file may depend on `typst()` / the parser:
 *
 *   1. `parseTypstOutline` (module, pure `=`-run parser over document text) —
 *      the stable, testable spec of the heading rules.
 *   2. `makeTypstHeadingOutlineItem` (core `tree-operations/outline`, the
 *      positions-based extraction that the *runtime* lezer `enterNode` path
 *      actually pushes into the `documentOutline` projection field) — proves
 *      the runtime extraction (level/line/title) for `=`, `==`, … runs and the
 *      no-marker fallback.
 *
 * The two are kept behaviourally aligned by construction: both skip a title-less
 * `=`-run (Typst headings require a title) and both number levels 1-based
 * (`=` → 1, `==` → 2, …). The full runtime walk (lezer `Heading`/`HeadingMarker`
 * nodes → pane) plus outline click→jump is the browser e2e / manual gate, since
 * the lezer wasm grammar cannot be loaded in Node.
 */
import { expect } from 'chai'
import { EditorState } from '@codemirror/state'
import { parseTypstOutline } from '../../../frontend/js/languages/typst/document-outline'
import { makeTypstHeadingOutlineItem } from '../../../../../frontend/js/features/source-editor/utils/tree-operations/outline'

describe('modules/typst: document outline (F3.6)', function () {
  it('parses =-run headings with 1-based levels, skipping code/comments', function () {
    const items = parseTypstOutline(
      [
        '= Introduction',
        'Body text.',
        '// = not a heading (comment)',
        '#let x = 1',
        '== Background',
        '=== Deep detail',
        '= Methods<sec:methods>',
        '',
      ].join('\n')
    )
    expect(items.map(i => [i.level, i.title, i.line])).to.deep.equal([
      [1, 'Introduction', 1],
      [2, 'Background', 5],
      [3, 'Deep detail', 6],
      [1, 'Methods', 7], // trailing label stripped
    ])
  })

  it('returns empty for a doc with no headings', function () {
    expect(parseTypstOutline('just text\n#code line\n// comment')).to.be.empty
  })

  it('skips a title-less =-run (Typst headings require a title)', function () {
    // A bare `=` / `= ` yields no heading, matching the runtime extraction;
    // the real heading on the next line is preserved with its 1-based level.
    expect(parseTypstOutline('=\n== Sub')).to.deep.equal([
      { line: 2, title: 'Sub', level: 2 },
    ])
    expect(parseTypstOutline('= \n   ')).to.be.empty
    expect(parseTypstOutline('=')).to.be.empty
  })

  it('does NOT treat `=foo` (no space after the run) as a heading', function () {
    expect(parseTypstOutline('=foo\nbar')).to.be.empty
  })
})

describe('core: makeTypstHeadingOutlineItem (F3.6 runtime extraction)', function () {
  const state = (doc: string) => EditorState.create({ doc })

  it('uses the marker run length as the 1-based level and slices the title', function () {
    const s = state('= Intro\nbody\n== Sub\nbody2\n')
    //   '= Intro' spans 0..7 (marker 0..1); '== Sub' spans 13..19 (marker 13..15)
    const a = makeTypstHeadingOutlineItem(s, 0, 7, 0, 1) as {
      title: string
      level: number
      line: number
    }
    expect(a).to.deep.include({ title: 'Intro', level: 1, line: 1 })
    const b = makeTypstHeadingOutlineItem(s, 13, 19, 13, 15) as {
      title: string
      level: number
      line: number
    }
    expect(b).to.deep.include({ title: 'Sub', level: 2, line: 3 })
  })

  it('falls back to the whole span (level 1) when there is no marker child', function () {
    const s = state('Title here\n')
    // no distinct marker span: markerFrom === markerTo === headingFrom
    const a = makeTypstHeadingOutlineItem(s, 0, 10, 0, 0) as {
      title: string
      level: number
    }
    expect(a).to.deep.include({ title: 'Title here', level: 1 })
  })

  it('returns false for a heading with an empty title', function () {
    const s = state('=\n')
    expect(makeTypstHeadingOutlineItem(s, 0, 1, 0, 1)).to.equal(false)
  })
})

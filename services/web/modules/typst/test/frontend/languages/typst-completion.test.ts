/**
 * Typst autocomplete (P4, F4.1 + F4.2 + F4.3) — module frontend unit.
 *
 * Run via `yarn test:frontend` (mocha + frontend bootstrap). This file
 * exercises the *pure* reference engine only —
 * `frontend/js/languages/typst/reference-completion.ts` (lezer/wasm/React/
 * CodeMirror-state-free). It deliberately does NOT import the CodeMirror
 * glue `typstCompletionSource` (`.../completion.ts`): that module's `@/...`
 * core imports can only be loaded through the webpack/babel chain, and the
 * glue is a three-line delegation that is covered by the webpack prod
 * build + the F5.7 browser e2e journey (the P4 exit gate).
 *
 * Coverage (DoD ledger P4: "module frontend unit: reference completion"),
 * extended per `improve_typst.md` (items 7 / 8 / 9 / 11 / 13 / 18):
 *   (1) label extraction — the four `#label` declaration forms + heading
 *       `<label>` markers, first-seen, deduped; comment/math/other-code
 *       lines skipped.
 *   (2) option records — item 7: `label` is the *bare* matching token (no
 *       `§`, so the typed partial prefix-matches at position 0, no
 *       FuzzyMatcher penalty); the fork `§` marker lives in `displayLabel`
 *       (tooltip only). Item 18: the `#cite` offer carries a *function*
 *       `apply` (the installed @codemirror/autocomplete fork has no
 *       `extend` field, so the standard CM apply function is the
 *       auto-close mechanism) that inserts `)` unless the char after the
 *       cursor is already `)`; `autoClose=false` (the `@` form, no parens)
 *       and every other trigger keep a string `apply`.
 *   (3) trigger resolution — `#cite(§` / `#label(§` / `#ref(§` / `@` /
 *       `#include "` / `#import "` spans (the span is the typed partial, so
 *       `apply` is the bare token and the `§`/quotes stay in place). Item 11:
 *       `#include` and `#import` fire *distinct* trigger kinds (`include` /
 *       `import`), so the offer's `detail` matches the keyword typed. Item
 *       13c: the quoted label form fires only for the *live* partial
 *       inside the opening quote (a closed `#label "x"` does not re-match).
 *   (4) the offers — `#cite(§` from the project `.bib` keys (capped at
 *       `MAX_OPTIONS`, item 13a), `#label(§` / `#ref(§` / `@` from the
 *       current doc's labels (never the bib keys for `#ref`), `#include "…`
 *       / `#import "…` from the project file tree (`.typ` only, `_`-prefixed
 *       skipped, relative to the open doc's slash-encoded folder — `null`
 *       and `''` docFolder behave the same, item 9; the live Folder shape
 *       with `_id` / `fileRefs` drives the offer, item 13b; the open doc is
 *       excluded from its own offer via `openDocPath`, item 8) — and `null`
 *       when there is nothing to offer.
 *
 * The registration itself (`index.ts` `typstCompletionLanguageData`, a
 * standalone `EditorState.languageData.of(provider)` — the wasm grammar's
 * plain `Language` never attaches our `data` facet, see its docstring)
 * is covered by the webpack prod build + the F5.7 browser e2e journey.
 */
import { expect } from 'chai'
import {
  extractTypstLabels,
  buildLabelCompletions,
  buildCitationCompletions,
  resolveTrigger,
  buildTypstCompletion,
  collectTypPaths,
  relativeToDocFolder,
  MAX_OPTIONS,
  TypstFolder,
} from '../../../frontend/js/languages/typst/reference-completion'

describe('modules/typst: reference completion spec (F4.1)', function () {
  it('extracts the label declaration forms, first-seen, deduped', function () {
    const src = [
      '= Introduction',
      '',
      '= Results <sec:results>',
      '#label(§sec:extra)',
      '#label "sec:quoted"',
      '#label("sec:func")',
      'body text',
    ].join('\n')
    expect(extractTypstLabels(src)).to.deep.equal([
      'sec:results',
      'sec:extra',
      'sec:quoted',
      'sec:func',
    ])
  })

  it('skips comment, math, and non-#label `#` lines', function () {
    const src = [
      '// #label(§commented)',
      '  // #label(§indented)',
      '#label(§kept)',
      '$math #label(§not)',
      '#set page(margin: 1cm)',
    ].join('\n')
    // only the bare `#label(§kept)` line is a declaration
    expect(extractTypstLabels(src)).to.deep.equal(['kept'])
  })

  it('dedupes labels declared twice (same and mixed forms)', function () {
    expect(extractTypstLabels('#label(§dup)\n#label(§dup)')).to.deep.equal([
      'dup',
    ])
    // same label via heading marker AND via the `#label` form:
    expect(extractTypstLabels('= A <sec:x>\n#label(§sec:x)')).to.deep.equal([
      'sec:x',
    ])
  })

  it('label option record (item 7): label = bare, displayLabel = §<label>, apply = bare', function () {
    const opts = buildLabelCompletions(['sec:results'], 'ref')
    expect(opts).to.have.lengthOf(1)
    const o = opts[0]
    expect(o.type).to.equal('ref')
    // item 7: the matching label is bare (no § prefix), so the typed partial
    // prefix-matches at position 0 (no FuzzyMatcher penalty); § is display-only
    expect(o.label).to.equal('sec:results')
    expect(o.displayLabel).to.equal('§sec:results')
    expect(o.detail).to.equal('ref')
    expect(o.info).to.equal('Label: #label(§sec:results)')
    expect(o.apply).to.equal('sec:results')
  })

  it('citation option record (items 7 + 18): label = bare key, displayLabel = §<key>, apply = function', function () {
    const opts = buildCitationCompletions(new Set(['kxxt2019']))
    expect(opts).to.have.lengthOf(1)
    const o = opts[0]
    expect(o.type).to.equal('bib')
    // item 7: matching label is bare; § shows only in displayLabel
    expect(o.label).to.equal('kxxt2019')
    expect(o.displayLabel).to.equal('§kxxt2019')
    expect(o.detail).to.equal('cite')
    // item 18: the #cite offer carries a *function* apply (not a string);
    // the installed @codemirror/autocomplete fork has no `extend` field, so
    // the standard CM apply function is the auto-close mechanism
    expect(typeof o.apply).to.equal('function')
    expect(o.info).to.equal('Citation: #cite(§kxxt2019)')
  })

  it('item 18: the function apply inserts `)` unless the char after the cursor is already `)`', function () {
    const opts = buildCitationCompletions(new Set(['kxxt2019']), true)
    // `apply` is typed `string | Function`; item 18 makes it a function
    // for `#cite` — narrow once, call many times.
    const applyFn = opts[0].apply as (
      view: unknown,
      completion: unknown,
      from: number,
      to: number
    ) => void
    // Stub the CM view the engine casts to: `{ state: { doc: { sliceString }
    // }, dispatch(tr) }`. The engine reads `view.state.doc.sliceString(to,
    // to + 1)` (the char *after* the cursor) and dispatches one change
    // `{from, to, insert}`. `to` = 0 here (the span is empty — the partial
    // is nothing, so the insert lands at the cursor position); the stub doc
    // starts AT the cursor with the "next char" so `sliceString(0, 1)` is
    // exactly that char.
    const makeView = (nextChar: string) => {
      const calls: string[] = []
      const view = {
        state: {
          doc: {
            sliceString(a: number, b: number) {
              return nextChar.slice(a, b)
            },
          },
        },
        dispatch(tr: { changes: { insert: string } }) {
          calls.push(tr.changes.insert)
        },
      }
      return { view, calls }
    }
    // the char after the cursor is `)` — the user typed `#cite(§k)` and the
    // closing paren is *outside* the span — so the apply inserts the BARE
    // key (no double `)`)
    {
      const { view, calls } = makeView(')')
      applyFn(view, {}, 0, 0)
      expect(calls).to.deep.equal(['kxxt2019'])
    }
    // the char after the cursor is anything else (empty line / space / …) —
    // the call is still open — so the apply inserts `key + ')'`
    {
      const { view, calls } = makeView('')
      applyFn(view, {}, 0, 0)
      expect(calls).to.deep.equal(['kxxt2019)'])
    }
  })

  it('item 18: autoClose = false (the `@` form, no parens) falls back to a string apply', function () {
    const opts = buildCitationCompletions(new Set(['kxxt2019']), false)
    expect(opts).to.have.lengthOf(1)
    const o = opts[0]
    // `@` is not a function call — no paren to auto-close — so the engine
    // keeps the pre-item-18 string apply.
    expect(typeof o.apply).to.equal('string')
    expect(o.apply).to.equal('kxxt2019')
  })
})

describe('modules/typst: trigger resolution (F4.1/F4.2)', function () {
  // Convention: `pos` = cursor offset = `before.length` (line-local here;
  // the glue passes the cursor's absolute document offset — the engine only
  // does `pos - partialLength`, so it's consistent in either coordinate),
  // and `before` is the text *of the line* up to the cursor (the engine's
  // regex is written for that line-relative coordinate)
  it('#cite(\u00a7 \u2014 span is the keyed partial (the \u00a7 stays outside the span)', function () {
    const trigger = resolveTrigger('#cite(\u00a7kxxt2019', 14)
    expect(trigger).to.not.be.null
    expect(trigger && trigger.kind).to.equal('cite')
    // partial `kxxt2019` (8) ends at 14 \u2192 from 6
    expect(trigger && trigger.from).to.equal(6)
  })

  it('#label(\u00a7 (fork) and #label " \u2014 both label triggers, span = partial', function () {
    // fork / ledger form: partial `sec:extra` (9) ends at 15 \u2192 from 6
    const t1 = resolveTrigger('#label(\u00a7sec:extra', 15)
    expect(t1 && t1.kind).to.equal('label')
    expect(t1 && t1.from).to.equal(6)
    // quoted modern form: partial `sec:q` (5) ends at 13 \u2192 from 8 (inside quotes)
    const t2 = resolveTrigger('#label "sec:q', 13)
    expect(t2 && t2.kind).to.equal('label')
    expect(t2 && t2.from).to.equal(8)
  })

  it('#ref(\u00a7 \u2014 span is the partial after the \u00a7', function () {
    const trigger = resolveTrigger('#ref(\u00a7sec:results', 17)
    expect(trigger && trigger.kind).to.equal('ref')
    // partial `sec:results` (11) ends at 17 \u2192 from 6
    expect(trigger && trigger.from).to.equal(6)
  })

  it('@ \u2014 span after the @; a space (prose) kills it', function () {
    const k = resolveTrigger('@kx', 3)
    expect(k && k.kind).to.equal('at')
    // partial `kx` (2) ends at 3 \u2192 from 1
    expect(k && k.from).to.equal(1)
    // `hello @ world` \u2014 @ is mid-prose, not the start of a reference
    expect(resolveTrigger('hello @ world', 13)).to.be.null
  })

  it('#include " / #import " \u2014 span is the path inside the open quote; kind matches the keyword (item 11)', function () {
    // `#include "` (10) + partial `chapters.in` (11) = 21 chars; from = 21-11 = 10
    const line1 = '#include "chapters.in'
    expect(line1.length).to.equal(21)
    const inc = resolveTrigger(line1, line1.length)
    expect(inc && inc.kind).to.equal('include')
    expect(inc && inc.from).to.equal(10)
    // empty partial (cursor right after the opening quote) \u2192 from = pos = 9
    const line2 = '#import "'
    expect(line2.length).to.equal(9)
    const imp = resolveTrigger(line2, line2.length)
    // item 11 (improve_typst.md): pre-F4.1 the regex dropped the keyword
    // group and *both* kinds fired as `include`; the engine now captures
    // the keyword and returns `'import'` for `#import "` so the offer's
    // `detail` (and the chip) matches the user's typed keyword
    expect(imp && imp.kind).to.equal('import')
    expect(imp && imp.from).to.equal(9)
    // a plain `#` line with no trigger \u2192 null
    expect(resolveTrigger('#no-such-trigger', 16)).to.be.null
  })

  it('item 13c: the quoted label form fires only *live* (inside the open quote)', function () {
    // live partial: cursor between the quotes — `#label "sec:q` (13 chars)
    // → `label`, span starts at 13 - 5 = 8 (the typed partial)
    const live = resolveTrigger('#label "sec:q', 13)
    expect(live && live.kind).to.equal('label')
    expect(live && live.from).to.equal(8)
    // terminated: the closing quote was typed (14 chars, cursor at 14) —
    // the anchored regex no longer matches → no offer
    expect(resolveTrigger('#label "sec:q"', 14)).to.be.null
    // the *fork* form (no quotes) also only fires while the partial is
    // live: `#label(§sec:q` (13 chars, no closing paren yet) → fires; the
    // closed `#label(§sec:q)` (14 chars) → the `$` anchor fails → null
    expect(resolveTrigger('#label(\u00a7sec:q)', 14)).to.be.null
    const liveFork = resolveTrigger('#label(\u00a7sec:q', 13)
    expect(liveFork && liveFork.kind).to.equal('label')
  })
})

describe('modules/typst: completion offers (F4.1/F4.2)', function () {
  function metaOf(referenceKeys: string[], fileTree: TypstFolder | null) {
    return {
      referenceKeys: new Set(referenceKeys),
      fileTreeData: fileTree,
    }
  }
  const tree = {
    name: 'project',
    docs: [{ name: 'main.typ' }, { name: 'notes.txt' }],
    folders: [
      {
        name: 'chapters',
        docs: [{ name: 'introduction.typ' }],
        folders: [
          { name: 'deep', docs: [{ name: 'x.typ' }], folders: [] },
        ],
      },
      { name: '_ignored', docs: [{ name: 'hidden.typ' }], folders: [] },
    ],
  }

  it('#cite(§ — every .bib key, bare label, § displayLabel, function apply (items 7 + 18)', function () {
    const before = '#cite(§'
    const res = buildTypstCompletion(
      before,
      before.length,
      metaOf(['kxxt2019', 'lam2020'], null),
      null,
      before
    )
    expect(res).to.not.be.null
    // span starts right after `#cite(§`
    expect(res && res.from).to.equal(7)
    expect(res && res.to).to.equal(7)
    // item 7: matching label is bare; § lives only in displayLabel (the
    // FuzzyMatcher prefix-matches the typed partial at position 0)
    expect(
      (res?.options ?? []).map(o => o.label)
    ).to.deep.equal(['kxxt2019', 'lam2020'])
    expect(
      (res?.options ?? []).map(o => o.displayLabel)
    ).to.deep.equal(['§kxxt2019', '§lam2020'])
    // item 18: #cite apply is a function (auto-close), never a string
    ;(res?.options ?? []).forEach(o => {
      expect(o.type).to.equal('bib')
      expect(o.detail).to.equal('cite')
      expect(typeof o.apply).to.equal('function')
    })
  })

  it('#cite(§ returns null when the project has no .bib keys', function () {
    const before = '#cite(§'
    expect(
      buildTypstCompletion(before, before.length, metaOf([], null), null)
    ).to.be.null
  })

  it('item 13a: #cite(§ caps the offer at MAX_OPTIONS = 50', function () {
    // 60 keys in, 50 offers out — more keys stay reachable by typing
    const many = Array.from({ length: 60 }, (_, i) => `key${i}`)
    const before = '#cite(§'
    const res = buildTypstCompletion(
      before,
      before.length,
      metaOf(many, null),
      null,
      before
    )
    expect(res).to.not.be.null
    expect(res?.options).to.have.lengthOf(MAX_OPTIONS)
    expect(MAX_OPTIONS).to.equal(50)
  })

  it('#label(§ and #ref(§ — labels of the current doc (type ref), bare label, § displayLabel, never the bib keys (item 7)', function () {
    const before = '#label(§'
    const res = buildTypstCompletion(
      before,
      before.length,
      metaOf(['only-bib'], null),
      null,
      '= Results <sec:results>\n' + before
    )
    expect(res).to.not.be.null
    expect(res?.options).to.have.lengthOf(1)
    const l = res?.options[0]
    expect(l && l.type).to.equal('ref')
    expect(l && l.detail).to.equal('label')
    // item 7: bare matching label; § tooltip-only
    expect(l && l.label).to.equal('sec:results')
    expect(l && l.displayLabel).to.equal('§sec:results')
    // `apply` is the bare label (a string — the decl trigger has no paren to close)
    expect(l && (l.apply as string)).to.equal('sec:results')
    // #ref is the same label list (the modern reference call) — and it does
    // NOT leak citation keys
    const refBefore = '#ref(§'
    const ref = buildTypstCompletion(
      refBefore,
      refBefore.length,
      metaOf(['only-bib'], null),
      null,
      '= Results <sec:results>\n' + refBefore
    )
    expect(ref?.options.map(o => o.label)).to.deep.equal(['sec:results'])
    expect(ref?.options.map(o => o.displayLabel)).to.deep.equal([
      '§sec:results',
    ])
    expect(ref?.options[0]?.detail).to.equal('ref')
  })

  it('@ — combined citation keys + labels (classic typst @key): bare labels, § displayLabels, string apply (items 7 + 18)', function () {
    const before = '@'
    const res = buildTypstCompletion(
      before,
      before.length,
      metaOf(['kxxt2019'], null),
      null,
      '= Introduction <sec:intro>\n' + before
    )
    expect(res).to.not.be.null
    // item 7: bare matching labels; § in displayLabels
    expect((res?.options ?? []).map(o => o.label)).to.have.members([
      'kxxt2019',
      'sec:intro',
    ])
    expect((res?.options ?? []).map(o => o.displayLabel)).to.have.members([
      '§kxxt2019',
      '§sec:intro',
    ])
    // both kinds present: citation key (type bib) + label (type ref)
    expect((res?.options ?? []).map(o => o.type)).to.have.members([
      'bib',
      'ref',
    ])
    // item 18: `@` is not a call → *string* apply (no auto-close)
    ;(res?.options ?? []).forEach(o => {
      expect(typeof o.apply).to.equal('string')
    })
  })

  it('#include " — .typ from the file tree, _-prefixed/`.txt` skipped, relative to doc folder (openDocPath = null → pre-item-8 full offer)', function () {
    const before = '#include "'
    const atRoot = buildTypstCompletion(
      before,
      before.length,
      metaOf([], tree),
      null
    )
    expect(atRoot).to.not.be.null
    // `notes.txt` is not `.typ`; `_ignored/hidden.typ` is skipped;
    // `collectTypPaths` is the tree walk (root-stripped, `_`-skipped)
    expect(
      (atRoot?.options ?? []).map(o => o.label)
    ).to.deep.equal([
      'main.typ',
      'chapters/introduction.typ',
      'chapters/deep/x.typ',
    ])
    expect(collectTypPaths(tree)).to.deep.equal([
      'main.typ',
      'chapters/introduction.typ',
      'chapters/deep/x.typ',
    ])
    // the doc is in `chapters` (the `docFolderFacet` slash encoding, the
    // value of `dirname(doc_id)`): a sibling resolves to its bare name, a
    // root-level file to `../<file>`
    const inChapters = buildTypstCompletion(
      before,
      before.length,
      metaOf([], tree),
      'chapters'
    )
    expect((inChapters?.options ?? []).map(o => o.label)).to.deep.equal([
      '../main.typ',
      'introduction.typ',
      'deep/x.typ',
    ])
    // no `.typ` anywhere → nothing to offer
    const empty = {
      name: 'project',
      docs: [{ name: 'notes.txt' }],
      folders: [],
    }
    expect(
      buildTypstCompletion(before, before.length, metaOf([], empty), null)
    ).to.be.null
  })

  it('item 9: falsy docFolder ("" and null) both resolve to the bare tree path', function () {
    // the engine uses a falsy test (`if (!docFolder) return target`), so
    // `''` and `null` are equivalent at the entry point
    expect(relativeToDocFolder('', 'main.typ')).to.equal('main.typ')
    expect(relativeToDocFolder(null, 'main.typ')).to.equal('main.typ')
    // contrast: a real docFolder still relativizes
    expect(relativeToDocFolder('chapters', 'chapters/introduction.typ')).to.equal(
      'introduction.typ'
    )
    expect(relativeToDocFolder('', 'chapters/deep/x.typ')).to.equal(
      'chapters/deep/x.typ'
    )
  })

  it('item 13b: the live Folder shape (with _id / fileRefs) drives the offer', function () {
    // the real core `Folder` tree (web/types/folder.ts) carries `_id`,
    // `extension`, `fileRefs`, ... — the pure engine's `TypstFolder` reads
    // only `name` / `docs` / `folders`. Cast through `unknown` to model
    // the runtime shape (the webpack glue passes the core shape as-is).
    const liveTree = {
      _id: 'id-proj',
      name: 'project',
      docs: [
        {
          _id: 'id-main',
          name: 'main.typ',
          extension: 'typ',
          fileRefs: {},
        },
        {
          _id: 'id-readme',
          name: 'README.txt',
          extension: 'txt',
          fileRefs: {},
        },
      ],
      folders: [
        {
          _id: 'id-ch',
          name: 'chapters',
          docs: [{ _id: 'id-intro', name: 'introduction.typ' }],
          folders: [
            { _id: 'id-deep', name: 'deep', docs: [{ _id: 'id-x', name: 'x.typ' }] },
          ],
        },
      ],
      fileRefs: [],
    } as unknown as TypstFolder
    const before = '#include "'
    // `README.txt` is not `.typ` (the live shape's `name` is the only field
    // the engine reads); the result matches the minimal-shape offer
    const atRoot = buildTypstCompletion(
      before,
      before.length,
      metaOf([], liveTree),
      null
    )
    expect((atRoot?.options ?? []).map(o => o.label)).to.deep.equal([
      'main.typ',
      'chapters/introduction.typ',
      'chapters/deep/x.typ',
    ])
  })

  it('item 8: the open doc is excluded from its own #include offer (openDocPath)', function () {
    const before = '#include "'
    const twoDocTree = {
      name: 'project',
      docs: [{ name: 'main.typ' }, { name: 'other.typ' }],
      folders: [],
    }
    // openDocPath = null → pre-item-8: *every* .typ offered (no filter)
    const resAll = buildTypstCompletion(
      before,
      before.length,
      metaOf([], twoDocTree),
      null,
      before,
      null
    )
    expect((resAll?.options ?? []).map(o => o.label)).to.deep.equal([
      'main.typ',
      'other.typ',
    ])
    // openDocPath = 'main.typ' (the slug path the glue dispatches from
    // `pathInFolder(fileTreeData, doc_id)`) → that doc is filtered out of
    // its *own* offer
    const resSelf = buildTypstCompletion(
      before,
      before.length,
      metaOf([], twoDocTree),
      null,
      before,
      'main.typ'
    )
    expect((resSelf?.options ?? []).map(o => o.label)).to.deep.equal(['other.typ'])
    // a doc tree with *only* the open doc → the offer is empty
    const onlySelf = {
      name: 'project',
      docs: [{ name: 'main.typ' }],
      folders: [],
    }
    expect(
      buildTypstCompletion(
        before,
        before.length,
        metaOf([], onlySelf),
        null,
        before,
        'main.typ'
      )
    ).to.be.null
  })

  it('item 11: #include/#import offers carry kind-matched `detail` (the chip shows the typed keyword)', function () {
    const tree = {
      name: 'project',
      docs: [{ name: 'a.typ' }],
      folders: [],
    }
    const incBefore = '#include "'
    const inc = buildTypstCompletion(
      incBefore,
      incBefore.length,
      metaOf([], tree),
      null,
      incBefore
    )
    const impBefore = '#import "'
    const imp = buildTypstCompletion(
      impBefore,
      impBefore.length,
      metaOf([], tree),
      null,
      impBefore
    )
    // pre-item-11 both offers' `detail` was the constant 'include'; the
    // engine now uses the trigger's *kind* as the `detail`
    expect((inc?.options ?? []).map(o => o.detail)).to.deep.equal(['include'])
    expect((imp?.options ?? []).map(o => o.detail)).to.deep.equal(['import'])
  })
})


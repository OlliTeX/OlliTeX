/* eslint-disable import/no-absolute-path */
/**
 * Typst language support for CodeMirror (lezer-based, F2.6).
 *
 * Salvaged from the old typst fork (Levi Zim, kxxt) and ported onto this
 * repo's module path. The wasm grammar (TypstParser) ships inside the
 * `codemirror-lang-typst` package (npm `codemirror-lang-typst`, wasm-pack
 * `bundler` target) — webpack loads `typst_syntax_bg.wasm` as an async
 * WebAssembly module (see webpack.config.js rules).
 *
 * Wiring: `frontend/js/features/source-editor/languages/index.ts` adds the
 * `typ` extension via the module import (the `@modules/...` path is
 * available in the mocha bootstrap aliases + webpack).
 */
import { tags, styleTags } from '@lezer/highlight'
import { EditorState } from '@codemirror/state'
import type { Extension } from '@codemirror/state'
import type { CompletionSource } from '@codemirror/autocomplete'
import {
  HighlightStyle,
  LanguageSupport,
  Language,
  syntaxHighlighting,
  defineLanguageFacet,
} from '@codemirror/language'

export const typstHighlight = styleTags({
  Shebang: tags.documentMeta,
  'LineComment BlockComment': tags.comment,

  Text: tags.content,
  Linebreak: tags.contentSeparator,
  Escape: tags.escape,
  Shorthand: tags.contentSeparator,
  SmartQuote: tags.quote,
  'Strong/...': tags.strong,
  'Emph/...': tags.emphasis,
  RawLang: tags.annotation,
  RawDelim: tags.controlKeyword,
  Raw: tags.monospace,
  Link: tags.link,
  Label: tags.labelName,
  'Ref/...': tags.labelName,
  'Heading/...': tags.heading,
  ListMarker: tags.list,
  EnumMarker: tags.list,
  TermMarker: tags.definitionOperator,

  MathText: tags.special(tags.string),
  MathIdent: tags.special(tags.variableName),
  'MathShorthand MathAlignPoint MathDelimited MathAttach MathPrimes MathFrac MathRoot':
    tags.special(tags.contentSeparator),

  Error: tags.invalid,

  Hash: tags.controlKeyword,
  'LeftBrace RightBrace': tags.brace,
  'LeftBracket RightBracket': tags.bracket,
  'LeftParen RightParen': tags.paren,
  Comma: tags.separator,
  'Semicolon Colon Dot Dots': tags.punctuation,
  Dollar: tags.controlKeyword,
  'Plus Minus Slash Hat': tags.arithmeticOperator,
  Prime: tags.typeOperator,
  'Eq PlusEq HyphEq SlashEq StarEq': tags.updateOperator,
  'EqEq ExclEq Lt LtEq Gt GtEq': tags.compareOperator,
  Arrow: tags.controlOperator,
  Root: tags.arithmeticOperator,

  'Not And Or': tags.operatorKeyword,
  'None Auto': tags.literal,
  'If Else For While Break Continue Return': tags.controlKeyword,
  'Import Include': tags.moduleKeyword,
  'Let Set Show Context': tags.definitionKeyword,
  'As In': tags.operatorKeyword,

  Code: tags.monospace,
  Ident: tags.variableName,
  Bool: tags.bool,
  Int: tags.integer,
  Float: tags.float,
  Numeric: tags.number,
  Str: tags.string,
})

const data = defineLanguageFacet({
  commentTokens: { block: { open: '/*', close: '*/' }, line: '//' },
})

export const TypstHighlightStyle = HighlightStyle.define([
  { tag: tags.link, textDecoration: 'underline' },
  { tag: tags.heading, fontWeight: 'bold', textDecoration: 'underline' },
  { tag: tags.emphasis, fontStyle: 'italic' },
  { tag: tags.strong, fontWeight: 'bold' },
  { tag: tags.literal, fontWeight: 'bold' },
  { tag: tags.punctuation, fontWeight: 'bold' },
  { tag: tags.controlKeyword, fontWeight: 'bold' },
  { tag: tags.annotation, fontWeight: 'bold' },
  { tag: tags.moduleKeyword, fontWeight: 'bold' },
  { tag: tags.operatorKeyword, fontWeight: 'bold' },
  { tag: tags.definitionKeyword, fontWeight: 'bold' },
  { tag: tags.contentSeparator, fontWeight: 'bold' },
  { tag: tags.definitionOperator, fontWeight: 'bold' },
  { tag: tags.list, fontWeight: 'bold' },
  { tag: tags.special(tags.contentSeparator), fontWeight: 'bolder' },
  {
    tag: tags.labelName,
    textDecoration: 'dotted blue underline',
    fontWeight: 'bold',
  },
  { tag: tags.monospace, fontFamily: 'monospace' },
])

/**
 * The wasm grammar is loaded lazily (dynamic import) so that importing this
 * module in a Node-only context (mocha smoke tests, tsc) does not eagerly
 * touch `codemirror-lang-typst/wasm/*.wasm` — webpack rewrites the dynamic
 * import to a real async WebAssembly chunk at build time.
 */
export async function typst(): Promise<LanguageSupport> {
  /**
   * Lazy: the lezer wasm grammar (codemirror-lang-typst) and this module's
   * `@/...` core imports (shortcuts / typstFormat pull in the LaTeX lezer
   * `.mjs` terms, webpack-only) load only here, at activation. Importing this
   * module in Node (mocha smoke, tsc) stays wasm-free + core-free.
   */
  const { TypstParser } = await import('codemirror-lang-typst')
  const { shortcuts } = await import('./shortcuts')
  const { typstLinter } = await import('./linter')
  // Autocomplete (F4.1/F4.2): the `#cite(§`/`#label(§`/`@`/`#include` source,
  // registered via a standalone `languageData` provider (below) — the wasm
  // grammar's plain `Language` has no usable `languageDataProp` attach point.
  const { typstCompletionSource } = await import('./completion')
  // Outline (F3.6): the shared CodeMirror `documentOutline` projection state
  // field (core — the lezer tree walk in `tree-operations/outline.enterNode`
  // now handles Typst `Heading` nodes). Registered here so a `.typ` document
  // in the source editor populates the right-rail outline pane. Loaded lazily
  // (webpack-only) so this module stays core-free in Node (mocha/tsc).
  const { documentOutline } = await import(
    '@/features/source-editor/languages/latex/document-outline'
  )
  // The 0.4.0 d.ts doesn't declare the constructor the runtime accepts.
  // (Confirmed in glue: `constructor(highlighting)`, `new TypstParser(...)`.)
  const Ctor = TypstParser as unknown as new (
    h: unknown,
  ) => InstanceType<typeof TypstParser>
  const parser = new Ctor(typstHighlight as never)
  const updateListener = parser.updateListener()
  const typstLanguage = new Language(
    data,
    parser,
    [
      updateListener,
      syntaxHighlighting(TypstHighlightStyle),
      documentOutline,
      shortcuts(),
      typstLinter(),
    ],
    'typst',
  )
  return new LanguageSupport(typstLanguage, [
    typstCompletionLanguageData(typstCompletionSource),
  ])
}

/**
 * F4.1/F4.2 (autocomplete): the typst completion source, registered as a
 * **standalone** `EditorState.languageData.of(...)` provider.
 *
 * Why not `typstLanguage.data.of({ autocomplete: ... })`? The `codemirror-
 * lang-typst` 0.4.0 wasm grammar instantiates a *plain* `Language` (the
 * constructor in `@codemirror/language` L68-74 — not `LRLanguage.define`) and
 * **never attaches** our `data` facet to its top node via `languageDataProp`
 * (the only `languageDataProp.add` in the package is `LRLanguage.define`,
 * L169-172; the wasm grammar configures no parser props). `Language.extension`'s
 * built-in `languageData.of(...)` reads `topNodeAt(...).type.prop(
 * languageDataProp)`, finds `undefined`, and returns `[]` — so a
 * `typstLanguage.data.of({ autocomplete })` extension would be dead code.
 *
 * The standalone provider below is *always* registered for states that
 * include this `LanguageSupport` (which the core language description does
 * only for the `typ` extension, so it is inert in `.tex` docs), and the
 * engine in `reference-completion.ts` already filters by trigger —
 * `#cite(§`/`#label(§`/`@`/`#include "` — so it returns `null` everywhere
 * else (no leakage into LaTeX docs: they never include this extension,
 * they get their completion sources from `languages/latex/complete.ts`).
 *
 * Covered by the F5.7 browser e2e journey (compile-gate caveat, F3.8);
 * the pure engine is unit-covered by F4.3.
 */
function typstCompletionLanguageData(
  source: CompletionSource
): Extension {
  return EditorState.languageData.of(() => [{ autocomplete: source }])
}

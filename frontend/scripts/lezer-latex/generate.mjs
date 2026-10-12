// Lezer grammar generator (recreated 2026-10-12).
//
// The original script was lost in the 2026-10-11 rm-rf incident. Contract
// reconstructed from two surviving consumers:
//  - webpack-plugins/lezer-grammar-compiler.js  → require()s this module and
//    uses its default export { compile, grammars }; each grammar entry must
//    carry grammarPath / parserOutputPath / termsOutputPath.
//  - frontend/package.json "lezer-latex:generate": node scripts/lezer-latex/generate.mjs
//
// Per lezer-latex/README.md: the generated outputs (latex.mjs +
// latex.terms.mjs, and likewise for bibtex) are git-ignored local artifacts
// compiled from the .grammar + ./tokens.mjs sources by @lezer/generator.
import fs from 'node:fs'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
import { buildParserFile } from '@lezer/generator'

const here = path.dirname(new URL(import.meta.url).pathname) // scripts/lezer-latex
const feRoot = path.resolve(here, '../..') // frontend/
const src = 'js/features/source-editor'

export const grammars = [
  {
    name: 'latex',
    grammarPath: path.join(feRoot, src, 'lezer-latex', 'latex.grammar'),
    parserOutputPath: path.join(feRoot, src, 'lezer-latex', 'latex.mjs'),
    termsOutputPath: path.join(feRoot, src, 'lezer-latex', 'latex.terms.mjs'),
  },
  {
    name: 'bibtex',
    grammarPath: path.join(feRoot, src, 'lezer-bibtex', 'bibtex.grammar'),
    parserOutputPath: path.join(feRoot, src, 'lezer-bibtex', 'bibtex.mjs'),
    termsOutputPath: path.join(feRoot, src, 'lezer-bibtex', 'bibtex.terms.mjs'),
  },
]

/**
 * Compile one grammar in place. @lezer/generator 1.8.0's buildParserFile()
 * returns { parser, terms } file text; the generated parser carries the
 * grammar's own relative tokenizer imports (e.g. "./tokens.mjs"), so the
 * outputs MUST be written next to the grammar + tokens sources.
 */
export function compile(grammar) {
  const text = fs.readFileSync(grammar.grammarPath, 'utf8')
  const { parser, terms } = buildParserFile(text, {
    fileName: grammar.grammarPath,
    moduleStyle: 'es',
  })
  fs.writeFileSync(grammar.parserOutputPath, parser)
  fs.writeFileSync(grammar.termsOutputPath, terms)
}

export default { compile, grammars }

// CLI mode (yarn lezer-latex:generate): compile every grammar.
const entry = process.argv[1]
if (entry && import.meta.url === pathToFileURL(entry).href) {
  for (const grammar of grammars) {
    process.stdout.write(
      `[lezer] ${path.basename(grammar.grammarPath)} -> ${path.dirname(grammar.parserOutputPath)}\n`,
    )
    compile(grammar)
  }
}

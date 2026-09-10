import Path from 'node:path'
import fsPromises from 'node:fs/promises'
import logger from '@overleaf/logger'

// Wordometer word-count injection (plan §3.6, F3.1).
//
// clsi runs `texcount` in docker; Typst has no texcount, so clsi_typst does a
// *wordometer-style* compile (the texlyre approach, ported per plan §3.6):
//
//   0. copy the vendored wordometer (vendor/wordometer.typ, plan §3.6(a))
//      into the compile dir
//   1. copy the project out of the project dir? No — inject in place:
//      the root resource is imported by an injected driver document
//      (`__clsi_wc_main.typ`) that imports the vendored wordometer under a
//      private alias `wo` and renders the counts on a final, marker-only page
//   2. Docker compiles that injected document
//   3. app/js/ClsiWordText.js reads the marker back out of the PDF
//
// The injected artifacts are `__clsi_wc_*` files that are removed after the
// count. The user's resources are never mutated.

const WC_MAIN = '__clsi_wc_main.typ'
const WC_WORDOMETER = '__clsi_wordometer.typ'
const WC_OUT_PDF = '__clsi_wc_out.pdf'

// Rendered marker (read back from the PDF with pdfjs-dist). Whitespace is
// tolerant: pdfjs splits the rendered text into fragments, so spaces between
// tokens are not stable.
export const WORDOMETER_MARKER =
  /TOTAL_WORDS:\s*(\d+)\s*HEADING_WORDS:\s*(\d+)\s*NUM_HEADINGS:\s*(\d+)/

// Typst string escape for an #include path (backslash + double-quote).
function _typEscape(s) {
  return s.replace(/\\/g, '\\\\').replace(/"/g, '\\"')
}

// Build the injected document from the root resource path (relative to the
// compile dir). The alias `wo` is private to this document (a sibling
// `#include` of the user file), so user-level `word-count` etc. cannot
// collide; `#show: wo.word-count` propagates through `#include`/`#import`.
function buildInjectedDoc(rootResourcePath) {
  return `#import "${_typEscape(
    WC_WORDOMETER
  )}" as wo
#show: wo.word-count

#include "${_typEscape(rootResourcePath)}"

#pagebreak()
#context {
  let heads = query(heading)
  let headWords = 0
  for h in heads { headWords += wo.word-count-of(h.body).words }
  [WORDOMETER_OUTPUT_START TOTAL_WORDS: #wo.total-words HEADING_WORDS: #headWords NUM_HEADINGS: #heads.len() WORDOMETER_OUTPUT_END]
}
`
}

async function _writeFile(filePath, content) {
  try {
    await fsPromises.writeFile(filePath, content)
  } catch (err) {
    if (err.code === 'ENOENT') {
      await fsPromises.mkdir(Path.dirname(filePath), { recursive: true })
      await fsPromises.writeFile(filePath, content)
    } else {
      throw err
    }
  }
}

const VENDOR_WORDOMETER = Path.join(
  import.meta.dirname,
  '../../vendor/wordometer.typ'
)

/**
 * Write the injected wordometer artifacts next to the project files in
 * `compileDir`:
 *  - `WC_WORDOMETER` (vendored copy of vendor/wordometer.typ)
 *  - `WC_MAIN`       (injected driver that includes the root resource)
 */
async function injectWordometer(compileDir, rootResourcePath) {
  const wordMeterContent = await fsPromises.readFile(
    VENDOR_WORDOMETER,
    'utf8'
  )
  await _writeFile(Path.join(compileDir, WC_WORDOMETER), wordMeterContent)
  await _writeFile(
    Path.join(compileDir, WC_MAIN),
    buildInjectedDoc(rootResourcePath)
  )
  logger.debug({ compileDir }, 'injected wordometer artifacts')
}

async function removeArtifacts(compileDir) {
  for (const name of [WC_MAIN, WC_WORDOMETER, WC_OUT_PDF]) {
    await fsPromises.rm(Path.join(compileDir, name), { force: true }).catch(
      () => {}
    )
  }
}

export default {
  WC_MAIN,
  WC_WORDOMETER,
  WC_OUT_PDF,
  WORDOMETER_MARKER,
  buildInjectedDoc,
  injectWordometer,
  removeArtifacts,
}

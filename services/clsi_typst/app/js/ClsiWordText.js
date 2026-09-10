import { fileURLToPath } from 'node:url'
import fsPromises from 'node:fs/promises'
import { createRequire } from 'node:module'

// pdfjs-dist runs on Node via the legacy build (the browser build expects
// DOMMatrix); import lazily so the service boots fine.
let _pdfjs
async function _getPdfjs() {
  if (!_pdfjs) {
    const req = createRequire(fileURLToPath(import.meta.url))
    const pdfjs = await import(req.resolve('pdfjs-dist/legacy/build/pdf.mjs'))
    pdfjs.GlobalWorkerOptions.workerSrc = req.resolve(
      'pdfjs-dist/legacy/build/pdf.worker.mjs'
    )
    _pdfjs = pdfjs
  }
  return _pdfjs
}

// Wordometer v1: the counts are rendered into the PDF (the vendored lib's
// build — pandoc/typst:3-alpine, Typst 0.14.2 — exposes no file-write API at
// compile time), so read the marker back from the PDF (plan §3.6). Returns
// a match against `match` over the rendered text of the last pages, or null.
async function pdfLastPagesMarkerText(pdfPath, match, lastPages = 2) {
  const pdfjs = await _getPdfjs()
  const buf = await fsPromises.readFile(pdfPath)
  const data = new Uint8Array(buf.buffer, buf.byteOffset, buf.byteLength)
  const doc = await pdfjs.getDocument({ data, isEvalSupported: false }).promise
  const pages = Math.min(
    lastPages,
    doc.numPages
  )
  for (let i = doc.numPages - pages + 1; i <= doc.numPages; i++) {
    const page = await doc.getPage(i)
    const textContent = await page.getTextContent()
    const text = textContent.items.map(item => item.str).join(' ')
    const m = text.match(match)
    if (m) {
      return m
    }
  }
  return null
}

export default { pdfLastPagesMarkerText }

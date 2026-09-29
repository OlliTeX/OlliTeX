/**
 * vitest extras for the TypstT1 specs (TYPST_INTEGRATION_PLAN.md §6) —
 * layered on top of test/frontend/editor-renovation/vitest.setup.ts.
 *
 * (1) fetch-mock: the mocha harness' `test/frontend/bootstrap.js` enables
 *     global mock mode; do the equivalent here so the component tests'
 *     `fetchMock.post(...)` routes actually intercept `fetch`.
 * (2) jsdom does not implement `Text.getClientRects` —
 *     CodeMirror's LineView.measureTextSize asks a Range/text node for
 *     rects; give the standard zero-rect answers so CM runs headless.
 */
import fetchMock from 'fetch-mock'

// mocha bootstrap (test/frontend/bootstrap.js) parity:
//   fetchMock.spyGlobal(); fetchMock.config.fetch = global.fetch;
//   fetchMock.config.Response = fetch.Response
fetchMock.spyGlobal()
fetchMock.config.fetch = globalThis.fetch
fetchMock.config.Response = Response

if (typeof window !== 'undefined') {
  const zeroRect = () =>
    ({
      x: 0,
      y: 0,
      top: 0,
      left: 0,
      right: 0,
      bottom: 0,
      width: 0,
      height: 0,
    }) as DOMRect
  const emptyList = () => ({ length: 0, item: () => null as unknown as DOMRect })
  for (const proto of [
    Range && (Range.prototype as object),
    Text && (Text.prototype as object),
    Element && (Element.prototype as object),
  ]) {
    if (!proto) {
      continue
    }
    const p = proto as unknown as {
      getClientRects: unknown
      getBoundingClientRect: unknown
    }
    if (typeof p.getClientRects !== 'function') {
      p.getClientRects = emptyList
    }
    if (typeof p.getBoundingClientRect !== 'function') {
      p.getBoundingClientRect = zeroRect
    }
  }
  const nodeProto = Node.prototype as unknown as {
    getClientRects: unknown
    getBoundingClientRect: unknown
  }
  if (typeof nodeProto.getClientRects !== 'function') {
    nodeProto.getClientRects = emptyList
  }
}

export {}

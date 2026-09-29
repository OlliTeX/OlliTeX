import { describe, it, expect } from 'vitest'
import {
  TIKZ_STORAGE_KEY,
  handleTikzEmbedMessage,
  readTikzSource,
  tikzEmbedUrl,
  isTikzFile,
  encodeTikzHostMessage,
} from '../../../frontend/js/util/tikz-protocol.ts'

const DOC = '\\begin{tikzpicture}\\end{tikzpicture}\n'

describe('isTikzFile', function () {
  it('claims .tikz and .pgf (any case)', function () {
    expect(isTikzFile('figures/drawing.tikz')).toBe(true)
    expect(isTikzFile('figure.PGF')).toBe(true)
    expect(isTikzFile('a.Tikz')).toBe(true)
  })

  it('declines other extensions', function () {
    expect(isTikzFile('main.tex')).toBe(false)
    expect(isTikzFile('diagram.svg')).toBe(false)
    expect(isTikzFile('README.md')).toBe(false)
    expect(isTikzFile('notikz')).toBe(false)
    expect(isTikzFile(null)).toBe(false)
    expect(isTikzFile(undefined)).toBe(false)
  })
})

describe('readTikzSource', function () {
  it('prefers source, falls back to the xml alias', function () {
    expect(readTikzSource({ source: DOC })).toBe(DOC)
    expect(readTikzSource({ source: '   ', xml: DOC })).toBe(DOC)
    expect(readTikzSource({ xml: DOC })).toBe(DOC)
    expect(readTikzSource({})).toBe(null)
    expect(readTikzSource({ source: 42 })).toBe(null)
  })
})

describe('handleTikzEmbedMessage', function () {
  it('maps init to a boot-load of the current doc', function () {
    const action = handleTikzEmbedMessage({ event: 'init' }, { doc: DOC })
    expect(action).toEqual({ kind: 'boot-load', source: DOC })
  })

  it('maps loaded to ready', function () {
    expect(handleTikzEmbedMessage({ event: 'loaded' }, { doc: '' })).toEqual({
      kind: 'ready'
    })
  })

  it.each(['change', 'autosave', 'save'])(
    '%s with a source updates the doc',
    function (event) {
      const action = handleTikzEmbedMessage(
        { event, source: DOC, xml: DOC },
        { doc: '' }
      )
      expect(action).toEqual({ kind: 'doc-update', source: DOC })
    }
  )

  it('ignores change events without a usable source', function () {
    expect(
      handleTikzEmbedMessage({ event: 'change' }, { doc: DOC })
    ).toEqual({ kind: 'noop' })
    expect(
      handleTikzEmbedMessage({ event: 'save', source: '   ' }, { doc: DOC })
    ).toEqual({ kind: 'noop' })
  })

  it('resolves export data from data/svg/source (in order)', function () {
    expect(
      handleTikzEmbedMessage(
        { event: 'export', format: 'svg', data: '<svg/>', svg: '<other/>' },
        { doc: DOC }
      )
    ).toEqual({ kind: 'export-result', data: '<svg/>' })
    expect(
      handleTikzEmbedMessage({ event: 'export', svg: '<svg/>' }, { doc: DOC })
    ).toEqual({ kind: 'export-result', data: '<svg/>' })
    expect(
      handleTikzEmbedMessage(
        { event: 'export', source: DOC, xml: DOC },
        { doc: DOC }
      )
    ).toEqual({ kind: 'export-result', data: DOC })
  })

  it('stores persistence-save pairs and ignores malformed ones', function () {
    expect(
      handleTikzEmbedMessage(
        { event: 'persistence-save', key: 'a', value: 'b' },
        { doc: '' }
      )
    ).toEqual({ kind: 'persistence-save', key: 'a', value: 'b' })
    expect(
      handleTikzEmbedMessage(
        { event: 'persistence-save', key: 'a' },
        { doc: '' }
      )
    ).toEqual({ kind: 'noop' })
  })

  it('surfaces embed errors', function () {
    expect(
      handleTikzEmbedMessage({ error: 'boom' }, { doc: '' })
    ).toEqual({ kind: 'error', message: 'boom' })
  })

  it('noops unknown shapes', function () {
    expect(handleTikzEmbedMessage(null, { doc: '' })).toEqual({ kind: 'noop' })
    expect(handleTikzEmbedMessage('text', { doc: '' })).toEqual({
      kind: 'noop'
    })
    expect(handleTikzEmbedMessage({ event: 'unknown' }, { doc: '' })).toEqual(
      { kind: 'noop' }
    )
  })
})

describe('tikzEmbedUrl', function () {
  it('points at the vendored same-origin app', function () {
    expect(tikzEmbedUrl(null)).toBe('/static/tikz-editor/index.html')
  })

  it('carries the persisted storage pair in the hash fragment', function () {
    expect(tikzEmbedUrl({ theme: 'dark' })).toBe(
      '/static/tikz-editor/index.html#storage=' +
      encodeURIComponent(JSON.stringify({ theme: 'dark' }))
    )
  })
})

describe('host->iframe encoding + storage key', function () {
  it('encodes as the JSON-string contract the embed expects', function () {
    expect(
      JSON.parse(encodeTikzHostMessage({ action: 'load', source: DOC }))
    ).toEqual({ action: 'load', source: DOC })
  })

  it('uses a product-scoped storage key', function () {
    expect(TIKZ_STORAGE_KEY).toBe('ollitex:tikz-editor:storage')
  })
})

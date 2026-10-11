import { screen, waitFor } from '@testing-library/react'
import fetchMock from 'fetch-mock'
import { expect } from 'chai'

import { renderWithEditorContext } from '../../../helpers/render-with-context'
import FileViewSvg from '../../../../../js/features/file-view/components/file-view-svg'
import { svgFile } from '../util/files'

describe('<FileViewSvg/>', function () {
  beforeEach(function () {
    fetchMock.removeRoutes().clearHistory()
    window.metaAttributesCache.set('ol-preventCompileOnLoad', true)
    window.URL.createObjectURL = function () {
      return 'blob:fake-svg-url'
    } as unknown as typeof URL.createObjectURL
    window.URL.revokeObjectURL = function () {} as unknown as typeof URL.revokeObjectURL
  })

  it('renders an image from the fetched svg blob', async function () {
    fetchMock.get(
      'express:/project/:project_id/blob/:hash',
      '<svg xmlns="http://www.w3.org/2000/svg"></svg>'
    )

    renderWithEditorContext(
      <FileViewSvg file={svgFile} onError={() => {}} onLoad={() => {}} />
    )

    const img = await screen.findByRole('img')
    expect(img.getAttribute('src')).to.equal('blob:fake-svg-url')
  })

  it('calls onError when the request fails', async function () {
    fetchMock.get('express:/project/:project_id/blob/:hash', 500)
    let errorCalls = 0
    const onError = () => {
      errorCalls += 1
    }

    renderWithEditorContext(
      <FileViewSvg file={svgFile} onError={onError} onLoad={() => {}} />
    )

    await waitFor(() => expect(errorCalls).to.be.greaterThan(0))
  })
})

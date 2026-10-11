import {
  screen,
  waitForElementToBeRemoved,
  fireEvent,
} from '@testing-library/react'
import fetchMock from 'fetch-mock'
import getMeta from '@/utils/meta'

import { renderWithEditorContext } from '../../../helpers/render-with-context'
import FileView from '../../../../../js/features/file-view/components/file-view'
import { imageFile, svgFile, textFile } from '../util/files'

describe('<FileView/>', function () {
  beforeEach(function () {
    fetchMock.removeRoutes().clearHistory()
    // The routing decision reads the exposed-settings meta (text extensions
    // + editable filenames); the test world doesn't seed it.
    Object.assign(getMeta('ol-ExposedSettings'), {
      textExtensions: ['tex', 'bib', 'cls', 'bst', 'txt'],
      editableFilenames: ['main.tex'],
    })
  })
  beforeEach(function () {
    fetchMock.removeRoutes().clearHistory()
    window.metaAttributesCache.set('ol-preventCompileOnLoad', true)
  })

  describe('for a text file', function () {
    it('shows a loading indicator while the file is loading', async function () {
      fetchMock.head('express:/project/:project_id/blob/:hash', {
        status: 201,
        headers: { 'Content-Length': 10000 },
      })
      fetchMock.get(
        'express:/project/:project_id/blob/:hash',
        'Text file content'
      )

      renderWithEditorContext(<FileView file={textFile} />)

      await waitForElementToBeRemoved(() =>
        screen.getByTestId('loading-panel-file-view')
      )
    })

    it('shows messaging if the text view could not be loaded', async function () {
      const unpreviewableTextFile = {
        ...textFile,
        name: 'example.not-tex',
      }

      renderWithEditorContext(<FileView file={unpreviewableTextFile} />)

      await screen.findByText('Sorry, no preview is available', {
        exact: false,
      })
    })
  })

  describe('for an image file', function () {
    it('shows a loading indicator while the file is loading', async function () {
      renderWithEditorContext(<FileView file={imageFile} />)

      screen.getByTestId('loading-panel-file-view')
    })

    it('shows messaging if the image could not be loaded', async function () {
      renderWithEditorContext(<FileView file={imageFile} />)

      // Fake the image request failing as the request is handled by the browser
      fireEvent.error(screen.getByRole('img'))

      await screen.findByText('Sorry, no preview is available', {
        exact: false,
      })
    })
  })

  describe('for an svg file', function () {
    beforeEach(function () {
      window.URL.createObjectURL = function () {
        return 'blob:fake-svg-url'
      } as unknown as typeof URL.createObjectURL
      window.URL.revokeObjectURL = function () {
      } as unknown as typeof URL.revokeObjectURL
    })

    it('renders the svg as an image', async function () {
      fetchMock.get(
        'express:/project/:project_id/blob/:hash',
        '<svg xmlns="http://www.w3.org/2000/svg"></svg>'
      )

      renderWithEditorContext(<FileView file={svgFile} />)

      const img = await screen.findByRole('img')
      expect(img.getAttribute('src')).to.equal('blob:fake-svg-url')
    })
  })
})

// @vitest-environment jsdom
// Project inspection panel — unit tests (candidate G).
//
// The panel is the UI half of the candidate-G arc: it resolves the
// project's entry documents from the standard project JSON and posts to
// POST /project/:id/project-inspection/analyze (Go feature
// go/services/web/features/projectinspection). Rendered in a jsdom with
// the two context providers mocked. (Workspace convention: .test.mjs.)

import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import React from 'react'
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'

const projectId = 'aaaaaaaaaaaaaaaaaaaaaaaa'
const mainDoc = 'cccccccccccccccccccccccc'

const h = React.createElement

const resultFixture = {
  schemaVersion: 1,
  analysisId: '9b1c...',
  analyzedAt: '2026-09-29T12:00:00Z',
  entryPoints: [{ id: mainDoc, path: 'main.tex' }],
  overview: {
    fileCount: 3, figureCount: 2, tableCount: 1, citationCount: 7,
    missing: 1, unusedUnreferenced: 2, duplicate: 0, circular: 0,
  },
  graph: { roots: ['file:main.tex'], nodes: [], edges: [], cycles: [], truncated: false },
  issues: {
    byId: {
      'issue-1': { id: 'issue-1', title: 'Missing file: sections/intro.tex', target: 'sections/intro.tex', status: 'missing', type: 'missing-file' },
      'issue-2': { id: 'issue-2', title: 'File is possibly unused: old.tex', target: 'old.tex', status: 'unused', type: 'possibly-unused-file' },
      'issue-3': { id: 'issue-3', title: 'File is possibly unused: scratch.tex', target: 'scratch.tex', status: 'unused', type: 'possibly-unused-file' },
    },
    truncated: false,
  },
  views: {
    missing: ['issue-1'],
    unused: ['issue-2', 'issue-3'],
    duplicate: [],
    circular: [],
    citation: { missing: [], unused: [], duplicate: [] },
  },
  coverage: { parsedFiles: 3, parseErrors: [], dynamicReferences: [], ambiguousReferences: [], skippedFiles: [], suppressedCitationChecks: [], truncated: false },
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (k) => k }),
}))

vi.mock('@/features/ide-react/context/ide-react-context', () => ({
  useIdeReactContext: () => ({ projectId }),
}))

vi.mock('@/infrastructure/local-storage', () => ({
  default: { getItem: () => null, setItem: () => {} },
}))

const getJSONMock = vi.fn()
const postJSONMock = vi.fn()
vi.mock('@/infrastructure/fetch-json', () => ({
  getJSON: (...args) => getJSONMock(...args),
  postJSON: (...args) => postJSONMock(...args),
}))

// Import the panel AFTER the mocks are in place.
const { default: Panel } = await import('../../../../../../../frontend/modules/project-inspection/frontend/js/panel.tsx')

function projectFixture() {
  return {
    _id: projectId,
    rootFolder: [
      {
        _id: 'ffffffffffffffffffffffff',
        name: '/',
        docs: [
          { _id: mainDoc, name: 'main.tex' },
          { _id: 'dddddddddddddddddddddddd', name: 'notes.typ' },
        ],
        fileRefs: [{ _id: 'eeeeeeeeeeeeeeeeeeeeeeee', name: 'refs.bib', hash: 'cafe' }],
        folders: [
          {
            _id: 'aaaaaaaaaaaaaaaaaaaaaaaa',
            name: 'sections',
            docs: [{ _id: 'bbbbbbbbbbbbbbbbbbbbbbbb', name: 'intro.tex' }],
            fileRefs: [],
            folders: [],
          },
        ],
      },
    ],
  }
}

beforeEach(() => {
  getJSONMock.mockReset()
  postJSONMock.mockReset()
  getJSONMock.mockResolvedValue(projectFixture())
})

afterEach(() => {
  cleanup()
})

describe('project inspection panel', () => {
  it('resolves entry docs from the project JSON and posts the analysis request', async () => {
    postJSONMock.mockResolvedValue(resultFixture)
    render(h(Panel, { order: 3 }))
    const btn = screen.getByRole('button')
    fireEvent.click(btn)
    await waitFor(() => {
      expect(postJSONMock).toHaveBeenCalledOnce()
    })
    const [url, opts] = postJSONMock.mock.calls[0]
    expect(url).toBe(`/project/${projectId}/project-inspection/analyze`)
    expect(opts.body.entryPointIds).toEqual([
      mainDoc,
      'dddddddddddddddddddddddd',
      'bbbbbbbbbbbbbbbbbbbbbbbb',
    ])
  })

  it('renders the overview + issue sections from the engine result', async () => {
    postJSONMock.mockResolvedValue(resultFixture)
    render(h(Panel, { order: 3 }))
    fireEvent.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByTestId('pi-overview')).toBeTruthy()
    })
    expect(screen.getByTestId('pi-missing').textContent).toContain('Missing file: sections/intro.tex')
    expect(screen.getByTestId('pi-unused').textContent).toContain('2')
    expect(screen.getByTestId('pi-meta').textContent).toContain('2026-09-29T12:00:00Z')
  })

  it('maps a 413 PROJECT_TOO_LARGE failure to the i18n error message', async () => {
    postJSONMock.mockRejectedValue(Object.assign(new Error('413'), {
      status: 413,
      error: 'PROJECT_TOO_LARGE',
    }))
    render(h(Panel, { order: 3 }))
    fireEvent.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByTestId('pi-error').textContent).toBe(
        'project_inspection_error_too_large',
      )
    })
  })

  it('maps an INVALID_ENTRY_POINT 400 to its message', async () => {
    postJSONMock.mockRejectedValue(Object.assign(new Error('400'), {
      status: 400,
      error: 'INVALID_ENTRY_POINT',
    }))
    render(h(Panel, { order: 3 }))
    fireEvent.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByTestId('pi-error').textContent).toBe(
        'project_inspection_error_invalid_entry',
      )
    })
  })

  it('maps timeout (504) and cancel (499) to their messages', async () => {
    postJSONMock.mockRejectedValue({ status: 504, error: 'ANALYSIS_TIMEOUT' })
    render(h(Panel, { order: 3 }))
    fireEvent.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByTestId('pi-error').textContent).toBe(
        'project_inspection_error_timeout',
      )
    })

    cleanup()
    vi.clearAllMocks()
    getJSONMock.mockResolvedValue(projectFixture())
    postJSONMock.mockRejectedValue({ status: 499, error: 'ANALYSIS_CANCELLED' })
    render(h(Panel, { order: 3 }))
    fireEvent.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByTestId('pi-error').textContent).toBe(
        'project_inspection_error_cancelled',
      )
    })
  })
})

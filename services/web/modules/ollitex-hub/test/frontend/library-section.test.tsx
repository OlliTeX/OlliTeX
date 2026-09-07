import React from 'react'
import { describe, it, expect, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent, within } from '@testing-library/react'
import LibrarySection from '../../frontend/js/sections/workspace/library-section'
import { setHubMeta, renderHub, stubFetch } from './helpers/hub-utils'

describe('<LibrarySection />', () => {
  beforeEach(() => {
    setHubMeta({})
  })

  it('lists library + trash from the library REST API', async () => {
    stubFetch([
      {
        match: '/library/references?trashed=true',
        body: { items: [{ key: 'gone', type: 'book', fields: [{ name: 'title', value: 'Old' }] }], nextCursor: null },
      },
      {
        match: '/library/references',
        body: { items: [{ key: 'knuth1984', type: 'book', fields: [{ name: 'title', value: 'TeXbook' }] }], nextCursor: null },
      },
    ])
    renderHub(<LibrarySection />)

    expect(await screen.findByText('knuth1984')).toBeTruthy()
  })

  it('exposes the full Add reference menu (owner #31 parity)', async () => {
    stubFetch([
      { match: '/library/references?trashed=true', body: { items: [], nextCursor: null } },
      { match: '/library/references', body: { items: [], nextCursor: null } },
    ])
    renderHub(<LibrarySection />)
    const addButtons = await screen.findAllByText(/Add reference/)
    expect(addButtons.length).toBeGreaterThan(0)

    fireEvent.click(addButtons[0])
    const dropdown = await screen.findByRole('menu')
    for (const item of [
      'Enter manually',
      'Paste references',
      '.bib file',
      'ORCID',
      'Zotero',
      // owner review B11: item descriptions present
      'Search by name or ORCID iD',
      'Browse your Zotero libraries',
    ]) {
      expect(
        within(dropdown).getAllByText(new RegExp(item, 'i')).length,
        `menu missing "${item}"`
      ).toBeGreaterThan(0)
    }
  })

  it('manual entry opens the manual reference form', async () => {
    stubFetch([
      { match: '/library/references?trashed=true', body: { items: [], nextCursor: null } },
      { match: '/library/references', body: { items: [], nextCursor: null } },
    ])
    renderHub(<LibrarySection />)
    const addButtons = await screen.findAllByText(/Add reference/)
    expect(addButtons.length).toBeGreaterThan(0)

    fireEvent.click(addButtons[0])
    const manual = await screen.findByText('Enter manually')
    fireEvent.click(manual)

    // the legacy manual modal (title/type/fields) is up: look for the type select label
    await waitFor(() => {
      expect(document.body.textContent).toMatch(/Title|type|Author/i)
    })
  })
})

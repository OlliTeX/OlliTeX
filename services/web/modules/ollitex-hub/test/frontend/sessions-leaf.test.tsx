import React from 'react'
import { describe, it, expect, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import SessionsLeaf from '../../frontend/js/sections/workspace/sessions-section'
import { setHubMeta, renderHub, stubFetch } from './helpers/hub-utils'

describe('<SessionsLeaf /> (mysettings.sessions)', () => {
  beforeEach(() => {
    setHubMeta({})
  })

  it('renders the revoke action and the session-list link', async () => {
    stubFetch([])
    renderHub(<SessionsLeaf />)

    expect(await screen.findByRole('button', { name: /Revoke all other sessions/ })).toBeTruthy()
    const link = screen.getByRole('link', { name: /View sessions list/i })
    expect(link.getAttribute('href')).toBe('/user/sessions')
  })

  it('revokes other sessions via POST /user/sessions/clear', async () => {
    const { calls } = stubFetch([
      { method: 'POST', match: '/user/sessions/clear', body: {} },
    ])
    renderHub(<SessionsLeaf />)

    fireEvent.click(await screen.findByRole('button', { name: /Revoke all other sessions/ }))

    await waitFor(() => {
      const post = calls.find(c => c.method === 'POST' && c.url.includes('/user/sessions/clear'))
      expect(post).toBeTruthy()
    })
  })
})

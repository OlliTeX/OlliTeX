import React from 'react'
import { describe, it, expect, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import SessionsLeaf from '../../frontend/js/sections/workspace/sessions-section'
import { setHubMeta, renderHub, stubFetch } from './helpers/hub-utils'

describe('<SessionsLeaf /> (mysettings.sessions)', () => {
  beforeEach(() => {
    setHubMeta({})
  })

  it('renders the session list inline (no link to the soon-removed classic page)', async () => {
    stubFetch([
      {
        match: '/user/sessions/list',
        body: {
          currentSession: { ip_address: '10.0.0.5', session_created: '2026-09-01T10:00:00Z' },
          sessions: [
            { ip_address: '10.0.0.9', session_created: '2026-08-30T09:00:00Z' },
            { ip_address: '10.0.1.4', session_created: '2026-08-29T08:00:00Z' },
          ],
        },
      },
    ])
    renderHub(<SessionsLeaf />)

    expect(await screen.findByRole('button', { name: /Revoke all other sessions/ })).toBeTruthy()
    // the list that used to be the linked /user/sessions page is now inline
    await waitFor(() => {
      expect(document.body.textContent).toContain('10.0.0.9')
      expect(document.body.textContent).toContain('10.0.1.4')
    })
    // nothing links out to the classic page anymore
    expect(document.querySelector('a[href="/user/sessions"]')).toBeNull()
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

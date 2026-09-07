import React from 'react'
import { describe, it, expect, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import HubRoot from '../../frontend/js/hub/hub-root'
import { accordionState } from '../../frontend/js/hub/accordion-state'
import { setHubMeta, renderHub, stubFetch } from './helpers/hub-utils'

describe('<HubRoot /> shell', () => {
  beforeEach(() => {
    window.location.hash = ''
    stubFetch([
      { match: '/admin/instance-stats/api/series', body: { points: [{ day: 1, values: [42] }] } },
      { match: '/system/messages', body: [] },
    ])
  })

  it('renders the workspace rail for a member (no admin surfaces)', async () => {
    setHubMeta({ 'ol-hub-admin': false })
    renderHub(<HubRoot />)

    const projects = await screen.findAllByText('Projects')
    expect(projects.length).toBeGreaterThan(0)
    expect(screen.queryByText(/Reference library/)).toBeTruthy()
    // admin-only surfaces must be absent for members
    expect(screen.queryByText('Site settings')).toBeNull()
    expect(screen.queryByText('Overview & activity')).toBeNull()
  })

  it('renders the full admin rail (incl. Site settings) for an admin', async () => {
    setHubMeta({ 'ol-hub-admin': true })
    renderHub(<HubRoot />)

    const site = await screen.findAllByText('Site settings')
    expect(site.length).toBeGreaterThan(0)
    const overview = screen.getAllByText(/Overview & activity/)
    expect(overview.length).toBeGreaterThan(0)
  })

  it('deep-links: #/site.general.messages renders the System messages leaf', async () => {
    setHubMeta({ 'ol-hub-admin': true })
    window.location.hash = '#site.general.messages'
    renderHub(<HubRoot />)

    await waitFor(() => {
      expect(document.body.textContent).toMatch(/Add message/)
    })
  })

  it('shows the unknown-section fallback for a bad hash', async () => {
    setHubMeta({ 'ol-hub-admin': false })
    window.location.hash = '#nope/not-a-leaf'
    renderHub(<HubRoot />)

    await waitFor(() => {
      expect(document.body.textContent).toMatch(/Unknown section/i)
    })
  })

  it('toggles rail folders open and closed (accordion behaviour)', async () => {
    setHubMeta({ 'ol-hub-admin': true })
    accordionState.close('site') // deterministic start (state persists via localStorage)
    renderHub(<HubRoot />)

    // children are hidden while the folder is collapsed
    expect(screen.queryByText('Full site settings')).toBeNull()
    let folder = screen.getAllByText('Site settings')[0]

    fireEvent.click(folder)
    await waitFor(() => {
      expect(screen.getByText('Full site settings')).toBeTruthy()
    })

    // collapsing again hides the children
    fireEvent.click(screen.getAllByText('Site settings')[0])
    await waitFor(() => {
      expect(screen.queryByText('Full site settings')).toBeNull()
    })
  })

  it('header logo links home and honours the instance custom logo', async () => {
    setHubMeta({
      'ol-hub-admin': false,
      'ol-navbar': { customLogo: '/img/custom-logo.png', customLogoDark: '' },
    })
    renderHub(<HubRoot />)

    const logo = await screen.findByRole('img', { name: 'LibreLeaf' })
    expect(logo.getAttribute('src')).toBe('/img/custom-logo.png')
    expect(logo.closest('a')?.getAttribute('href')).toBe('/')
  })
})

import React from 'react'
import { describe, it, expect, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent, within } from '@testing-library/react'
import ProjectsSection from '../../frontend/js/sections/workspace/projects-section'
import { setHubMeta, renderHub, stubFetch } from './helpers/hub-utils'

const PROJECTS = [
  {
    id: 'p1',
    name: 'Paper draft',
    owner: { email: 'a@b.c', firstName: 'Ada' },
    lastUpdated: '2026-09-01T10:00:00Z',
    accessLevel: 'owner',
  },
]

describe('<ProjectsSection />', () => {
  beforeEach(() => {
    setHubMeta({})
    stubFetch([
      { method: 'POST', match: '/api/project', body: { projects: PROJECTS } },
      { match: '/tag', body: { tags: [{ _id: 't1', name: 'work' }] } },
      { match: '/api/templates', body: { templates: [{ _id: 'tpl1', title: 'Thesis' }] } },
    ])
  })

  it('renders the project list (POST /api/project filters contract)', async () => {
    const { calls } = stubFetch([
      { method: 'POST', match: '/api/project', body: { projects: PROJECTS } },
      { match: '/tag', body: { tags: [] } },
    ])
    renderHub(<ProjectsSection />)

    expect(await screen.findByText('Paper draft')).toBeTruthy()
    // the list call must be a POST with filters + sort (owner #3 parity)
    await waitFor(() => {
      const call = calls.find(c => c.url.includes('/api/project'))
      expect(call).toBeTruthy()
      const body = JSON.parse(call!.body!)
      expect(body).toHaveProperty('filters')
      expect(body).toHaveProperty('sort')
    })
  })

  it('exposes the full New project menu (owner #30 parity)', async () => {
    renderHub(<ProjectsSection defaultFilter="all" />)
    await screen.findByText('Paper draft')

    fireEvent.click(screen.getByRole('button', { name: /New project/ }))

    const dropdown = await screen.findByRole('menu')
    for (const item of [
      'Blank project',
      'From template',
      'Existing project',
      'Word document',
      'Markdown file',
    ]) {
      expect(within(dropdown).queryByText(new RegExp(item, 'i')), `menu missing "${item}"`).toBeTruthy()
    }
  })

  it('opens the blank-project form from the menu', async () => {
    renderHub(<ProjectsSection />)
    await screen.findByText('Paper draft')

    fireEvent.click(screen.getByRole('button', { name: /New project/ }))
    const blank = await screen.findByText('Blank project')
    fireEvent.click(blank)

    // modal with the name + template fields (legacy /project parity)
    expect(await screen.findByText(/Project name/i)).toBeTruthy()
    expect(screen.queryByText(/Start from/i)).toBeTruthy()
  })

  it('opens the zip upload modal from the menu (owner #30 .zip import)', async () => {
    renderHub(<ProjectsSection />)
    await screen.findByText('Paper draft')

    fireEvent.click(screen.getByRole('button', { name: /New project/ }))
    const zip = await screen.findByText(/Existing project/i)
    fireEvent.click(zip)

    // OLModal mounts a node with id=upload-project-modal (Uppy dashboard inside)
    await waitFor(() => {
      expect(document.getElementById('upload-project-modal')).toBeTruthy()
    })
  })
})

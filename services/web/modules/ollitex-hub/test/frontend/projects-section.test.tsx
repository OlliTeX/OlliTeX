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
      'Existing project',
      'Word document',
      'Markdown file',
      'Import from GitHub',
      // owner review B4/10e: Templates section with "More templates"
      'Example project',
      'More templates',
    ]) {
      expect(
        within(dropdown).getAllByText(new RegExp(item, 'i')).length,
        `menu missing "${item}"`
      ).toBeGreaterThan(0)
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

  it('row menu has Download PDF (10b parity) and triggers a silent compile', async () => {
    const { calls } = stubFetch([
      { method: 'POST', match: '/api/project', body: { projects: PROJECTS } },
      { match: '/tag', body: { tags: [] } },
      { match: '/api/templates', body: { templates: [] } },
      { method: 'POST', match: '/project/p1/compile', body: {} },
    ])
    window.open = (window.open || (() => undefined)) as any
    const origOpen = window.open
    const opens: string[] = []
    window.open = ((url: string) => { opens.push(url) }) as any
    renderHub(<ProjectsSection />)
    await screen.findByText('Paper draft')

    fireEvent.click(screen.getAllByRole('button', { name: 'More actions' })[0])
    const pdfItem = await screen.findByText('Download PDF')
    expect(pdfItem).toBeTruthy()
    fireEvent.click(pdfItem)

    await waitFor(() => {
      const compileCall = calls.find(c => c.url.includes('/project/p1/compile') && c.method === 'POST')
      expect(compileCall).toBeTruthy()
      const body = JSON.parse(compileCall!.body!)
      expect(body.check).toBe('silent')
    })
    expect(opens).toContain('/project/p1/pdf')
    window.open = origOpen as any
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

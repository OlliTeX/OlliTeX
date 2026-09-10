import React from 'react'
import { describe, it, expect, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent, within } from '@testing-library/react'
import AdminUsersSection from '../../frontend/js/sections/admin/admin-users-section'
import AdminProjectsSection from '../../frontend/js/sections/admin/admin-projects-section'
import { ZoteroSection } from '../../frontend/js/sections/admin/site/simple-sections'
import SiteSettingsIndexSection from '../../frontend/js/sections/admin/site/index-section'
import { setHubMeta, renderHub, stubFetch } from './helpers/hub-utils'

describe('<ZoteroSection /> (owner B16: native zotero admin section)', () => {
  beforeEach(() => setHubMeta({}))

  it('renders the section and saves client key + secret via PUT /admin/site-settings/zotero', async () => {
    const { calls } = stubFetch([
      { match: '/admin/site-settings', body: { zotero: { enabled: false, clientKey: '', clientSecretSet: false } } },
      { method: 'PUT', match: '/admin/site-settings/zotero', body: { enabled: true, clientKey: 'zotero_k', clientSecretSet: true } },
    ])
    renderHub(<ZoteroSection />)
    expect((await screen.findAllByText(/Client key/i)).length).toBeGreaterThan(0)
    expect(screen.getByPlaceholderText('zotero_xxx')).toBeTruthy()

    // enable + fill key + save
    fireEvent.click(screen.getAllByRole('switch')[0])
    fireEvent.change(screen.getByPlaceholderText('zotero_xxx'), { target: { value: 'zotero_k' } })
    const saveBtn = await screen.findByRole('button', { name: /^Save$/i })
    fireEvent.click(saveBtn)

    await waitFor(() => {
      const save = calls.find(c => c.url.includes('/admin/site-settings/zotero') && c.method === 'PUT')
      expect(save).toBeTruthy()
      const body = JSON.parse(save!.body!)
      expect(body.enabled).toBe(true)
      expect(body.clientKey).toBe('zotero_k')
    })
  })
})

describe('<SiteSettingsIndexSection /> (owner B16: enclose leaf = native map)', () => {
  beforeEach(() => setHubMeta({}))

  it('lists the site-settings leaves as working hash links', async () => {
    stubFetch([])
    renderHub(<SiteSettingsIndexSection />)
    expect(await screen.findByText(/Email \/ SMTP/i)).toBeTruthy()
    expect(screen.getByText(/retired/i)).toBeTruthy()
    const links = screen.getAllByRole('link')
    const hrefs = links.map(a => a.getAttribute('href'))
    expect(hrefs).toContain('#/site.services.email')
    expect(hrefs.some(h => h && h.startsWith('#/site.integrations.zotero'))).toBe(true)
  })
})

describe('<AdminUsersSection /> (owner B25: row Info + Update)', () => {
  beforeEach(() => setHubMeta({}))

  it('row menu has User info + Update; update saves via /admin/user/:id/update', async () => {
    const { calls } = stubFetch([
      {
        match: '/admin/users',
        body: {
          users: [
            { _id: 'u1', email: 'joe@example.org', firstName: 'Joe', lastName: 'Doe', isAdmin: false, canManageTemplates: false, suspended: false, signUpDate: '2026-01-01T00:00:00Z' },
          ],
          total: 1,
        },
      },
      { method: 'POST', match: '/admin/user/u1/update', body: { email: 'joe.doe@example.org' } },
    ])
    renderHub(<AdminUsersSection />)
    await screen.findAllByText('joe@example.org')

    fireEvent.click(screen.getAllByRole('button', { name: 'Actions' })[0])
    expect(await screen.findByText('User info')).toBeTruthy()
    expect(screen.getByText('Edit…')).toBeTruthy()

    // Update flow: change email → save (menu label “Edit…”, modal title “Update user”)
    fireEvent.click(screen.getByText('Edit…'))
    const modal = await screen.findByText('Edit user')
    expect(modal).toBeTruthy()
    const emailInput = (screen.getAllByLabelText('Email').find(el => (el as HTMLInputElement).tagName === 'INPUT')) as HTMLInputElement
    expect(emailInput).toBeTruthy()
    fireEvent.change(emailInput, { target: { value: 'joe.doe@example.org' } })
    fireEvent.click(screen.getByRole('button', { name: /^Save changes$/i }))

    await waitFor(() => {
      const save = calls.find(c => c.url.includes('/admin/user/u1/update'))
      expect(save).toBeTruthy()
      const body = JSON.parse(save!.body!)
      expect(body.email).toBe('joe.doe@example.org')
      expect(body.firstName).toBe('Joe')
    })
  })
})

describe('<AdminProjectsSection /> (owner B28/B29: row Share/invite)', () => {
  beforeEach(() => setHubMeta({}))

  it('row menu has Share (invite) and sends POST /admin/project/:id/invite', async () => {
    const { calls } = stubFetch([
      {
        match: '/admin/user/null/projects',
        body: {
          projects: [
            { id: 'p1', name: 'Thesis', owner: { email: 'owner@example.org', firstName: 'O', lastName: 'W' }, lastUpdated: '2026-09-01T00:00:00Z', accessLevel: 'owner' },
          ],
          total: 1,
        },
      },
      { method: 'POST', match: '/admin/project/p1/invite', status: 201, body: {} },
    ])
    renderHub(<AdminProjectsSection />)
    await screen.findByText('Thesis')

    fireEvent.click(screen.getAllByRole('button', { name: 'Actions' })[0])
    const share = await screen.findByText(/Share…/)
    expect(share).toBeTruthy()
    fireEvent.click(share)

    expect(await screen.findByText('Access level')).toBeTruthy()
    const email = (screen.getAllByLabelText('Email address').find(el => (el as HTMLInputElement).tagName === 'INPUT') || screen.getAllByLabelText('Email address')[0]) as HTMLInputElement
    fireEvent.change(email, { target: { value: 'colleague@example.org' } })
    fireEvent.click(screen.getByRole('button', { name: /Send invite/i }))

    await waitFor(() => {
      const call = calls.find(c => c.url.includes('/admin/project/p1/invite') && c.method === 'POST')
      expect(call).toBeTruthy()
      const body = JSON.parse(call!.body!)
      expect(body.email).toBe('colleague@example.org')
      expect(body.privileges).toBe('editor')
    })
  })
})

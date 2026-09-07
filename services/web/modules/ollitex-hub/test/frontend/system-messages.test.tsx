import React from 'react'
import { describe, it, expect, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent, within } from '@testing-library/react'
import SystemMessagesSection from '../../frontend/js/sections/admin/system-messages-section'
import { setHubMeta, renderHub, stubFetch } from './helpers/hub-utils'

describe('<SystemMessagesSection />', () => {
  beforeEach(() => {
    setHubMeta({ 'ol-hub-admin': true })
  })

  it('lists messages from GET /system/messages', async () => {
    stubFetch([{ match: '/system/messages', body: [{ _id: 'm1', content: 'Planned maintenance' }] }])
    renderHub(<SystemMessagesSection />)

    expect(await screen.findByText('Planned maintenance')).toBeTruthy()
    expect(screen.getByText('Add message')).toBeTruthy()
  })

  it('shows the empty state when no messages are set', async () => {
    stubFetch([{ match: '/system/messages', body: [] }])
    renderHub(<SystemMessagesSection />)

    expect(await screen.findByText(/No system messages/i)).toBeTruthy()
    expect((screen.getByRole('button', { name: /Clear all/ }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('adds a message with the strict POST /admin/messages body { content }', async () => {
    const { calls } = stubFetch([
      { match: '/system/messages', body: [] },
      { method: 'POST', match: '/admin/messages', body: {} },
    ])
    renderHub(<SystemMessagesSection />)
    await screen.findByText(/No system messages/i)

    const input = screen.getByPlaceholderText(/Maintenance/i)
    fireEvent.change(input, { target: { value: 'HUB TEST MESSAGE' } })
    fireEvent.click(screen.getByRole('button', { name: /Add message/ }))

    await waitFor(() => {
      const post = calls.find(c => c.method === 'POST' && c.url.includes('/admin/messages'))
      expect(post).toBeTruthy()
      expect(JSON.parse(post!.body!)).toEqual({ content: 'HUB TEST MESSAGE' })
    })
  })

  it('clears all messages via POST /admin/messages/clear (with confirm)', async () => {
    const { calls } = stubFetch([
      { match: '/system/messages', body: [{ _id: 'm1', content: 'Old message' }] },
      { method: 'POST', match: '/admin/messages/clear', body: {} },
    ])
    renderHub(<SystemMessagesSection />)
    await screen.findByText('Old message')

    fireEvent.click(screen.getByRole('button', { name: /Clear all/ }))
    // ConfirmModal confirmation button — scope to the dialog so we do not
    // re-match the toolbar trigger
    const modal = await screen.findByRole('dialog')
    const confirmButton = within(modal).getByRole('button', { name: /Clear all/ })
    fireEvent.click(confirmButton)

    await waitFor(() => {
      const post = calls.find(c => c.method === 'POST' && c.url.includes('/admin/messages/clear'))
      expect(post).toBeTruthy()
    })
  })

  it('does not post an empty message', async () => {
    const { calls } = stubFetch([
      { match: '/system/messages', body: [] },
      { method: 'POST', match: '/admin/messages', body: {} },
    ])
    renderHub(<SystemMessagesSection />)
    await screen.findByText(/No system messages/i)

    fireEvent.click(screen.getByRole('button', { name: /Add message/ }))
    await new Promise(r => setTimeout(r, 50))
    expect(calls.some(c => c.method === 'POST' && c.url.includes('/admin/messages'))).toBe(false)
  })
})

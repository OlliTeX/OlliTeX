import React from 'react'
import { describe, it, expect, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import InstanceStatsSection from '../../frontend/js/sections/admin/instance-stats-section'
import { setHubMeta, renderHub, stubFetch } from './helpers/hub-utils'

describe('<InstanceStatsSection />', () => {
  beforeEach(() => {
    setHubMeta({ 'ol-hub-admin': true })
  })

  it('renders metric cards from the instance-stats series API', async () => {
    const { calls } = stubFetch([
      {
        match: '/admin/instance-stats/api/series',
        body: { metric: 'user_count', window: 'month', points: [{ day: 1, values: [42] }] },
      },
    ])
    renderHub(<InstanceStatsSection />)

    await waitFor(() => {
      expect(document.body.textContent).toMatch(/Users/)
    })
    // the "Users" card shows the latest point (42)
    expect(document.body.textContent).toMatch(/Users42/)
    // the default window is "month"
    const series = calls.filter(c => c.url.includes('/admin/instance-stats/api/series'))
    expect(series.length).toBeGreaterThan(0)
    expect(series[0].url).toMatch(/window=month/)
    expect(series[0].url).toMatch(/metric=user_count/)
  })

  it('formats byte metrics in GB', async () => {
    stubFetch([
      {
        match: '/admin/instance-stats/api/series?metric=disk_usage',
        body: { metric: 'disk_usage', window: 'month', points: [{ day: 1, values: [2.5 * 1024 ** 3] }] },
      },
      {
        match: '/admin/instance-stats/api/series',
        body: { metric: 'user_count', window: 'month', points: [{ day: 1, values: [7] }] },
      },
    ])
    renderHub(<InstanceStatsSection />)

    await waitFor(() => {
      expect(document.body.textContent).toMatch(/2\.5 GB/)
    })
  })

  it('links to the full charts page and switches window on demand', async () => {
    const { calls } = stubFetch([
      {
        match: '/admin/instance-stats/api/series',
        body: { metric: 'user_count', window: 'month', points: [{ day: 1, values: [1] }] },
      },
    ])
    renderHub(<InstanceStatsSection />)
    await waitFor(() => expect(document.body.textContent).toMatch(/Users/))

    const link = screen.getByRole('link', { name: /Full charts/i })
    expect(link.getAttribute('href')).toBe('/admin/instance-stats')

    const select = screen.getByRole('combobox') as HTMLSelectElement
    fireEvent.change(select, { target: { value: '6m' } })
    await waitFor(() => {
      const sixMonths = calls.find(c => c.url.includes('window=6m'))
      expect(sixMonths).toBeTruthy()
    })
  })
})

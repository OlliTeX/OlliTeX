import React from 'react'
import { describe, it, expect, beforeEach } from 'vitest'
import { screen, waitFor, fireEvent } from '@testing-library/react'
import InstanceStatsSection from '../../frontend/js/sections/admin/instance-stats-section'
import { setHubMeta, renderHub, stubFetch } from './helpers/hub-utils'

describe('<InstanceStatsSection />', () => {
  beforeEach(() => {
    setHubMeta({ 'ol-hub-admin': true })
  })

  it('renders the User / Projects / Storage / System sub-sections with inline charts', async () => {
    const { calls } = stubFetch([
      {
        match: '/admin/instance-stats/api/series',
        body: { metric: 'user_count', window: 'month', points: [{ day: Date.UTC(2026, 8, 1), values: [42] }] },
      },
    ])
    renderHub(<InstanceStatsSection />)

    // every legacy sub-section is present in /hub (owner request 2026-09-08)
    for (const heading of ['User', 'Projects', 'Storage', 'System', 'Alert settings']) {
      await waitFor(() => {
        expect(document.body.textContent).toMatch(new RegExp(heading))
      })
    }
    // the "Users" metric card shows its latest point (42)
    expect(document.body.textContent).toMatch(/Users/)
    expect(document.body.textContent).toMatch(/42/)
    // the default window is "month" and every metric was fetched
    const series = calls.filter(c => c.url.includes('/admin/instance-stats/api/series'))
    expect(series.length).toBeGreaterThanOrEqual(13)
    expect(series[0].url).toMatch(/window=month/)
    // inline SVG charts render (the bar/line graphs live in the hub, not behind a link)
    expect(document.querySelectorAll('svg[role="img"]').length).toBeGreaterThan(0)
  })

  it('formats byte metrics in GB on the latest-value readout', async () => {
    stubFetch([
      {
        match: '/admin/instance-stats/api/series?metric=disk_usage',
        body: { metric: 'disk_usage', window: 'month', points: [{ day: Date.UTC(2026, 8, 1), values: [2.5 * 1024 ** 3, 100 * 1024 ** 3] }] },
      },
      {
        match: '/admin/instance-stats/api/series',
        body: { metric: 'user_count', window: 'month', points: [{ day: Date.UTC(2026, 8, 1), values: [7] }] },
      },
    ])
    renderHub(<InstanceStatsSection />)

    await waitFor(() => {
      expect(document.body.textContent).toMatch(/2\.5 GB/)
    })
  })

  it('re-fetches every series when the window changes (no external charts page)', async () => {
    const { calls } = stubFetch([
      {
        match: '/admin/instance-stats/api/series',
        body: { metric: 'user_count', window: 'month', points: [{ day: Date.UTC(2026, 8, 1), values: [1] }] },
      },
    ])
    renderHub(<InstanceStatsSection />)
    await waitFor(() => expect(document.body.textContent).toMatch(/Users/))

    // nothing links out to the soon-removed classic page anymore
    expect(document.querySelector('a[href="/admin/instance-stats"]')).toBeNull()

    const select = screen.getByRole('combobox') as HTMLSelectElement
    fireEvent.change(select, { target: { value: '6m' } })
    await waitFor(() => {
      const sixMonths = calls.find(c => c.url.includes('window=6m'))
      expect(sixMonths).toBeTruthy()
    })
  })
})

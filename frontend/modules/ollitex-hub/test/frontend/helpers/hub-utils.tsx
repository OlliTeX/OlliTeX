import React from 'react'
import { render, RenderResult, cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'
import OllitexProvider from '@/shared/mantine/provider'
import { SplitTestProvider } from '@/shared/context/split-test-context'
import { UserSettingsProvider } from '@/shared/context/user-settings-context'

export function setHubMeta(overrides: Record<string, unknown> = {}): void {
  const base: Record<string, unknown> = {
    'ol-csrfToken': 'test-csrf',
    'ol-hub-theme': null,
    'ol-hub-admin': false,
    'ol-gitBridgeEnabled': true,
    'ol-ExposedSettings': {
      githubSyncEnabled: true,
      gitSyncEnabled: true,
      zoteroEnabled: true,
      webdavEnabled: true,
      dropbox: { enabled: false },
      mendeley: { enabled: true },
    },
    'ol-overallThemes': {
      system: { overallTheme: 'system' },
      dark: { overallTheme: 'dark' },
      light: { overallTheme: 'light' },
    },
    'ol-navbar': { customLogo: '', customLogoDark: '' },
    'ol-userSettings': {
      overallTheme: 'system',
      zotero: { enabled: true, groups: [], disablePersonalLibrary: false },
      mendeley: { enabled: true, groups: [], disablePersonalLibrary: false },
      papers: { enabled: true, groups: [], disablePersonalLibrary: false },
    },
  }
  const merged = { ...base, ...overrides }
  for (const [k, v] of Object.entries(merged)) {
    window.metaAttributesCache.set(k, v)
  }
}

export type FetchRoute = {
  method?: string
  match: string | RegExp
  status?: number
  body?: unknown
}

/**
 * Replace global fetch with a route table (first route whose `match`
 * matches the URL wins). Records every call for assertions:
 *   const { calls } = stubFetch([{ method: 'POST', match: '/admin/messages', body: {} }])
 *   await submit()
 *   expect(calls.at(-1).body).toEqual(JSON.stringify({ content: 'Hello' }))
 */
export function stubFetch(routes: FetchRoute[] = []) {
  const calls: Array<{ url: string; method: string; body: string | null }> = []
  const impl = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : (input as Request).url
    const method = (init?.method || 'GET').toUpperCase()
    calls.push({
      url,
      method,
      body: typeof init?.body === 'string' ? init.body : null,
    })
    for (const r of routes) {
      const hit =
        typeof r.match === 'string'
          ? url.includes(r.match) || new URL(url, 'https://www.test-overleaf.com').pathname === r.match
          : r.match.test(url)
      if (!hit) continue
      if (r.method && r.method.toUpperCase() !== method) continue
      const body = r.body === undefined ? '' : JSON.stringify(r.body)
      return Promise.resolve(
        new Response(body, {
          status: r.status ?? 200,
          headers: { 'Content-Type': 'application/json' },
        })
      )
    }
    return Promise.reject(new Error(`stubFetch: no route for ${method} ${url}`))
  })
  vi.stubGlobal('fetch', impl)
  return { calls, impl }
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

/**
 * Render a hub component inside the same provider stack the /hub page uses
 * (Appearance theme provider + split-test + user-settings), so legacy embeds
 * resolve their contexts exactly like in the browser.
 */
export function renderHub(ui: React.ReactElement): RenderResult {
  return render(
    <OllitexProvider>
      <SplitTestProvider>
        <UserSettingsProvider>{ui}</UserSettingsProvider>
      </SplitTestProvider>
    </OllitexProvider>
  )
}

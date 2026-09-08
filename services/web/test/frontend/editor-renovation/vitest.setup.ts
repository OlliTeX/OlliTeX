/**
 * vitest setup for the editor-renovation baseline suite (P0e test-debt wave).
 * These specs freeze the CURRENT (legacy) editor surfaces' behavior BEFORE
 * the Mantine renovation touches them (EDITOR_RENOVATION_PLAN.md).
 *
 * Seeded like the hub frontend suite: the legacy components' i18n bootstrap
 * reads the `ol-i18n` page meta at module scope (production pages render it
 * server-side — here we seed it), plus the standard jsdom shims.
 */
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'

if (typeof window !== 'undefined' && !window.metaAttributesCache) {
  window.metaAttributesCache = new Map()
}
if (typeof window !== 'undefined' && window.metaAttributesCache) {
  if (!window.metaAttributesCache.has('ol-i18n')) {
    window.metaAttributesCache.set('ol-i18n', { currentLangCode: 'en', lang: 'en' })
  }
  if (!window.metaAttributesCache.has('ol-csrfToken')) {
    window.metaAttributesCache.set('ol-csrfToken', { token: 'test-csrf' })
  }
  if (!window.metaAttributesCache.has('ol-ExposedSettings')) {
    window.metaAttributesCache.set('ol-ExposedSettings', { appName: 'Overleaf' })
  }
}

if (typeof window !== 'undefined' && !window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => undefined,
      removeListener: () => undefined,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList
}

if (typeof window !== 'undefined') {
  if ((globalThis as any).ResizeObserver === undefined) {
    ;(globalThis as any).ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
  }
}

if (typeof window !== 'undefined') {
  if (!window.HTMLElement.prototype.scrollIntoView) {
    window.HTMLElement.prototype.scrollIntoView = () => undefined
  }
}

// i18n: the legacy components' react-i18next usage needs the app i18next
// instance initialised (it reads the `ol-i18n` page meta at module scope —
// seeded above). The app's i18n module initialises on import; await it so
// renders that follow have the instance ready.
await import('@/i18n')

afterEach(() => {
  cleanup()
})

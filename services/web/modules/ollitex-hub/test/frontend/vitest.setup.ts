import { afterEach } from 'vitest'

// jsdom environment for the hub frontend integration suite.
if (typeof window !== 'undefined' && !window.metaAttributesCache) {
  window.metaAttributesCache = new Map()
}

// The hub leaves embed legacy feature components (llm usage meter, compliance
// settings, grammar settings, linking widgets) whose i18n bootstrap reads the
// `ol-i18n` page meta at module scope. Seed it so those modules initialise
// in the test environment (production pages render the same meta server-side).
if (typeof window !== 'undefined' && window.metaAttributesCache) {
  if (!window.metaAttributesCache.has('ol-i18n')) {
    window.metaAttributesCache.set('ol-i18n', { currentLangCode: 'en', lang: 'en' })
  }
}

// Standard jsdom shims the UI tree (Mantine color scheme, Uppy, virtualized
// lists) expects at load time.
if (typeof window !== 'undefined') {
  if (!window.matchMedia) {
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
      } as unknown as MediaQueryList)
  }
  const ResizeObserverStub = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  if (!('ResizeObserver' in window)) {
    ;(window as any).ResizeObserver = ResizeObserverStub
  }
  const IntersectionObserverStub = class {
    observe() {}
    unobserve() {}
    disconnect() {}
    takeRecords() {
      return []
    }
  }
  if (!('IntersectionObserver' in window)) {
    ;(window as any).IntersectionObserver = IntersectionObserverStub
  }
  if (!Element.prototype.scrollTo) {
    Element.prototype.scrollTo = function () {}
  }
  if (!Element.prototype.scrollIntoView) {
    Element.prototype.scrollIntoView = function () {}
  }
  if (!window.HTMLElement.prototype.hasPointerCapture) {
    window.HTMLElement.prototype.hasPointerCapture = function () {
      return false
    }
  }
}

// Page meta read at MODULE SCOPE by components embedded in the hub
// (legacy settings sections, navbar, etc.) must exist before the first
// import of the hub tree. setHubMeta() in the test helpers can override.
if (typeof window !== 'undefined') {
  const set = (name, value) => window.metaAttributesCache.set(name, value)
  set('ol-csrfToken', 'test-csrf')
  set('ol-hub-admin', false)
  set('ol-hub-theme', null)
  set('ol-gitBridgeEnabled', true)
  set('ol-ExposedSettings', {
    githubSyncEnabled: true,
    gitSyncEnabled: true,
    zoteroEnabled: true,
    webdavEnabled: true,
    dropbox: { enabled: false },
    mendeley: { enabled: true },
    enableTemplateTags: false,
  })
  set('ol-userSettings', {
    overallTheme: 'system',
    zotero: { enabled: true, groups: [], disablePersonalLibrary: false },
    mendeley: { enabled: true, groups: [], disablePersonalLibrary: false },
    papers: { enabled: true, groups: [], disablePersonalLibrary: false },
  })
  set('ol-overallThemes', {
    system: { overallTheme: 'system' },
    dark: { overallTheme: 'dark' },
    light: { overallTheme: 'light' },
  })
  set('ol-navbar', { customLogo: '', customLogoDark: '' })
}

afterEach(() => {
  if (typeof window !== 'undefined') {
    window.metaAttributesCache?.clear()
    window.localStorage?.clear()
    window.location.hash = ''
  }
  // restore a clean DOM between tests
  if (typeof document !== 'undefined') {
    document.body.innerHTML = ''
  }
})

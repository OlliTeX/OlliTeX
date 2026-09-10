import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import getMeta from '@/utils/meta'
import { resources } from '@/i18n-resources/writefull'

const LANG = getMeta('ol-i18n').currentLangCode

// Since we are rendering React from Angular, the initialisation is
// synchronous on page load (but hidden behind the loading screen). This
// means that translations must be initialised without any actual
// translation strings, and load those manually ourselves later

i18n.use(initReactI18next).init({
  lng: LANG,

  // 2026-09-04 (owner #11): all keys in locales/en.json are FLAT and may
  // contain literal dots ("adminSite.templates", ...). i18next's default
  // nested resolution (keySeparator '.') split those into nonexistent paths
  // and returned the raw key — the /admin/site sidebar showed
  // "adminSite.templates" etc. Disable separator parsing: key = exact key.
  keySeparator: false,
  nsSeparator: false,

  // still using the v3 plural suffixes
  compatibilityJSON: 'v3',

  react: {
    // Since we are manually waiting on the translations data to
    // load, we don't need to use Suspense
    useSuspense: false,

    // Trigger a re-render when a language is added. Since we load the
    // translation strings asynchronously, we need to trigger a re-render once
    // they've loaded
    bindI18nStore: 'added',

    // Disable automatic conversion of basic markup to React components
    transSupportBasicHtmlNodes: false,
  },

  interpolation: {
    // We use the legacy v1 JSON format, so configure interpolator to use
    // underscores instead of curly braces
    prefix: '__',
    suffix: '__',
    unescapeSuffix: 'HTML',

    // Disable nesting in interpolated values, preventing user input
    // injection via another nested value
    skipOnVariables: true,

    // Do not escape values, as `t` + React will already escape them
    // (`escapeValue: true` and `shouldUnescape` must be set on each use of `Trans`)
    escapeValue: false,

    defaultVariables: {
      appName: getMeta('ol-ExposedSettings').appName,
    },
  },
})

// The webpackChunkName here will name this chunk (and thus the requested
// script) according to the file name. See https://webpack.js.org/api/module-methods/#magic-comments
// for details
const localesPromise = import(
  /* webpackChunkName: "[request]" */ `../../locales/${LANG}.json`
).then(lang => {
  // `import()` resolves to a MODULE NAMESPACE in both webpack and vite/SSR.
  // Namespace enumeration only exposes keys that are valid JS identifiers,
  // so reserving-word-ish keys ("import", "default", ...) were silently
  // missing from every i18next bundle — the New-project dropdown rendered
  // the raw key "import" instead of "Import" (and similar for any other
  // non-identifier key). The full object is always at namespace `default`
  // (a CJS-style JSON module), so unwrap it when present. A plain object
  // (no bundler) passes through unchanged; a legitimate `"default"`
  // translation entry survives because the unwrapped object carries it.
  const raw = lang && typeof lang === 'object' ? lang : {}
  const bundle =
    raw.default && typeof raw.default === 'object'
      ? (raw.default as Record<string, string>)
      : (raw as Record<string, string>)
  i18n.addResourceBundle(LANG, 'translation', bundle)
  i18n.addResourceBundle(
    LANG,
    'writefull',
    LANG === 'es' ? resources.es.writefull : resources.en.writefull
  )
})

export default localesPromise

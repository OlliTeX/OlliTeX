import '../utils/webpack-public-path' // configure dynamically loaded assets (via webpack) to be downloaded from CDN
import '../infrastructure/error-reporter' // set up error reporting, including Sentry
import '../infrastructure/hotjar' // set up Hotjar
import '../infrastructure/mixpanel-autocapture' // set up mixpanel autocapture
import localesReady from '@/i18n' // default export: promise that resolves when the locale bundles are in the i18next store
import { createRoot } from 'react-dom/client'
import IdeRoot from '@/features/ide-react/components/ide-root'
import { EditorUiShell } from '@/features/editor-v2/variant'

const container = document.getElementById('ide-root')
if (container) {
  const root = createRoot(container)
  const render = () =>
    // EditorUiShell (editor renovation P1, EDITOR_RENOVATION_PLAN.md):
    // /editor gets the Mantine shell (lazy chunk; /Project never loads it).
    // While the shell resolves, legacy surfaces render unchanged, so the
    // page can never fail to open because of the renovation.
    root.render(<EditorUiShell><IdeRoot /></EditorUiShell>)

  // Owner #10/#12 (2026-09-13 editor wave): the app inits i18next with
  // useSuspense:false + bindI18nStore:'added', but i18next 23.x NEVER
  // emits the 'added' event for addResourceBundle (verified by repro),
  // so any component rendered before the locale chunk resolves freezes
  // a RAW KEY forever — e.g. the bib list count rendered
  // "many_references" instead of "42 references" (even for English
  // users), and the Import-from-library modal buttons showed the key.
  // Block the FIRST render until the translations are stored; on a load
  // failure we still render, so /editor never hard-fails.
  if (localesReady && typeof localesReady.then === 'function') {
    localesReady.then(render, render)
  } else {
    render()
  }
}

// work around Safari 15's incomplete support for dvh units
// https://github.com/overleaf/internal/issues/18109
try {
  if (
    document.body.parentElement &&
    document.body.parentElement?.clientHeight < document.body.clientHeight
  ) {
    const rootElement = document.querySelector<HTMLDivElement>('#ide-root')
    if (rootElement) {
      rootElement.style.height = '100vh'
    }
  }
} catch {
  // ignore errors
}

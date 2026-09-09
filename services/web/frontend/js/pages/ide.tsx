import '../utils/webpack-public-path' // configure dynamically loaded assets (via webpack) to be downloaded from CDN
import '../infrastructure/error-reporter' // set up error reporting, including Sentry
import '../infrastructure/hotjar' // set up Hotjar
import '../infrastructure/mixpanel-autocapture' // set up mixpanel autocapture
import { createRoot } from 'react-dom/client'
import IdeRoot from '@/features/ide-react/components/ide-root'
import { EditorUiShell } from '@/features/editor-v2/variant'

const container = document.getElementById('ide-root')
if (container) {
  const root = createRoot(container)
  // EditorUiShell (editor renovation P1, EDITOR_RENOVATION_PLAN.md):
  // /editor gets the Mantine shell (lazy chunk; /Project never loads it).
  // While the shell resolves, legacy surfaces render unchanged, so the
  // page can never fail to open because of the renovation.
  root.render(<EditorUiShell><IdeRoot /></EditorUiShell>)
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

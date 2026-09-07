import React from 'react'
import { createRoot } from 'react-dom/client'
import HubRoot from '../hub/hub-root'

// Material Symbols icon font (icon glyphs in the hub chrome/sections).
import '../../../../../frontend/fonts/material-symbols/material-symbols.css'
// Side-effect import: initialise this bundle's i18next instance (shared
// frontend i18n module) so useTranslation() in the hub sections — including
// the wrapped legacy components — resolves real strings instead of raw keys.
import '@/i18n'

const element = document.getElementById('hub-root')
if (element) {
  const root = createRoot(element)
  // HubRoot renders its own shared OlliTProvider (M2.5: it carries the
  // Appearance theme patch + per-scheme CSS variables).
  root.render(<HubRoot />)
}

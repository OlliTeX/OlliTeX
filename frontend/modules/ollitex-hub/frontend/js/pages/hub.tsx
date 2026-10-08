import React from 'react'
import { createRoot } from 'react-dom/client'
import HubRoot from '../hub/hub-root'
import {
  UserSettingsPage,
  AdminSettingsPage,
  UserSettingsSection,
  AdminSettingsSection,
} from '../hub/settings-pages'

// Material Symbols icon font (icon glyphs in the hub chrome/sections).
import '../../../../../fonts/material-symbols/material-symbols.css'
// Thin scrollbar + hub chrome styles (Wave A #8).
import '../hub/hub.css'
// Theme bridge for legacy Bootstrap widgets embedded in hub cards/modals
// (owner issues 2026-09-13 #8/#9: git-sync/webdav/dropbox/orcid/bib chrome).
import '../hub/hub-legacy-bridge.css'
// Side-effect import: initialise this bundle's i18next instance (shared
// frontend i18n module) so useTranslation() in the hub sections — including
// the wrapped legacy components — resolves real strings instead of raw keys.
import '@/i18n'

const element = document.getElementById('hub-root')
if (element) {
  const root = createRoot(element)
  // Dispatch (AJ-1, 2026-10-08): the settings pages are INDIVIDUAL pages —
  // a landing grid plus ONE section per route:
  //   /user-settings            → landing
  //   /user-settings/<id>       → one section
  //   /admin-settings           → landing
  //   /admin-settings/<id>      → one section
  // Everything else → the hub.
  const path = window.location.pathname.replace(/\/+$/, '') || '/'
  let page: React.ReactNode
  const userSectionMatch = path.match(/^\/user-settings\/([^\/]+)$/)
  const adminSectionMatch = path.match(/^\/admin-settings\/([^\/]+)$/)
  if (userSectionMatch) {
    page = <UserSettingsSection id={decodeURIComponent(userSectionMatch[1])} />
  } else if (adminSectionMatch) {
    page = <AdminSettingsSection id={decodeURIComponent(adminSectionMatch[1])} />
  } else if (path === '/user-settings') {
    page = <UserSettingsPage />
  } else if (path === '/admin-settings') {
    page = <AdminSettingsPage />
  } else {
    page = <HubRoot />
  }
  root.render(page)
}

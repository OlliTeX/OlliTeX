// 2026-09-03 (S+P, owner): /user/notification-preferences joins the
// user-settings family. This is the page's (intentionally small) JS entry:
// it mounts the SHARED down-left account menu (same component as the golden
// /admin/site sidebar footer) into #notif-prefs-account-root — the navbar
// account pill is hidden by CSS and the menu lives in the down-left corner.
// 2026-09 (parity, owner): the save form carries data-ol-async-form, so the
// page MUST hydrate the async-form helper (CSRF-aware fetch submit). Without
// this import the native form POST 403s (missing CSRF) — saved by the parity
// gate (PG-NP-1).
import '../../features/form-helpers/hydrate-form'
import React from 'react';
import { createRoot } from 'react-dom/client';
import { DsPageAccountMenuWithProviders } from '@/shared/components/navbar/ds-page-account-menu';

(() => {
  const el =
    (document.getElementById('notif-prefs-account-root') as HTMLElement | null) ||
    null;
  if (!el) {
    return;
  }
  const root = createRoot(el);
  root.render(
    <DsPageAccountMenuWithProviders rootId="notif-prefs-account-menu" />
  );
})();

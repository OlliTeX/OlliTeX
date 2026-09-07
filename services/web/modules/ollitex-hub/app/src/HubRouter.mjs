import logger from '@overleaf/logger'
import AuthenticationController from '../../../../app/src/Features/Authentication/AuthenticationController.mjs'
import AuthorizationMiddleware from '../../../../app/src/Features/Authorization/AuthorizationMiddleware.mjs'
import HubController from './HubController.mjs'

export default {
  apply(webRouter) {
    logger.debug({}, 'Init OlliTeX hub router')

    // Unified OlliTeX hub (owner 2026-09-07, nav_structure.md): one page
    // for the whole instance — workspace + administration in a single
    // nested-accordion rail. Admin-only branches hide for members.
    webRouter.get(
      '/hub',
      AuthenticationController.requireLogin(),
      HubController.hubPage
    )

    // Old two-hub URLs (owner 2026-09-06) — superseded by /hub; keep the
    // bookmarks alive with redirects.
    webRouter.get(
      '/hub/admin',
      AuthenticationController.requireLogin(),
      HubController.redirectToHub
    )

    webRouter.get(
      '/hub/workspace',
      AuthenticationController.requireLogin(),
      HubController.redirectToHub
    )

    // M2.5 Appearance (owner 2026-09-07): instance-wide /hub theme JSON.
    // GET is for any logged-in user (the theme is instance branding seen by
    // everyone); PUT/DELETE are admin-only (nav_structure.md §8.8).
    webRouter.get('/api/hub-theme',
      AuthenticationController.requireLogin(),
      HubController.getTheme
    )
    webRouter.put('/api/hub-theme',
      AuthorizationMiddleware.ensureUserIsSiteAdmin,
      HubController.saveTheme
    )
    webRouter.delete('/api/hub-theme',
      AuthorizationMiddleware.ensureUserIsSiteAdmin,
      HubController.clearTheme
    )
  },
}

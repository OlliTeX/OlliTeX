import logger from '@overleaf/logger'
import AuthorizationMiddleware from '../../../../app/src/Features/Authorization/AuthorizationMiddleware.mjs'
import AuthenticationController from '../../../../app/src/Features/Authentication/AuthenticationController.mjs'

/**
 * PSH — Page shells router (UI-R10 W8).
 *
 * Both legacy shell pages are removed — the hubs are the single settings
 * surfaces:
 *   GET /admin/panel     → 301 /hub#/overview   (owner queue 4, 2026-09-10)
 *   GET /user/mysettings → 301 /hub#/mysettings.account
 *                          (owner late item, 2026-09-12: "remove the old
 *                          page https://…/user/mysettings"; the workspace
 *                          hub #/mysettings.* leaves are the equivalent)
 */
const PageShellsRouter = {
  apply(webRouter) {
    logger.debug({}, 'Init PageShells router')

    webRouter.get(
      '/user/mysettings',
      AuthenticationController.requireLogin(),
      function (req, res) {
        res.redirect(301, '/hub#/mysettings.account')
      }
    )

    // 2026-09-10 (owner queue 4): the legacy admin panel page is removed —
    // the admin hub is the single admin surface. Redirect (pattern:
    // /admin/instance-stats); the hub #/overview leaf is the equivalent.
    webRouter.get(
      '/admin/panel',
      AuthorizationMiddleware.ensureUserIsSiteAdmin,
      function (req, res) {
        res.redirect(301, '/hub#/overview')
      }
    )
  },
}

export default PageShellsRouter

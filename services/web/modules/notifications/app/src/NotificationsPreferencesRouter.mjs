// @ts-check

import AuthenticationController from '../../../../app/src/Features/Authentication/AuthenticationController.mjs'
import NotificationsPreferencesController from './NotificationsPreferencesController.mjs'
import { startProjectNotificationConsumer } from './ProjectNotificationQueueConsumer.mjs'

function apply(webRouter, privateApiRouter, publicApiRouter) {
  const auth = AuthenticationController.requireLogin()

  // Global ("mute all") preferences — used by /user/notification-preferences
  webRouter.get(
    '/notifications/preferences',
    auth,
    NotificationsPreferencesController.getGlobalPreferences
  )

  webRouter.post(
    '/notifications/preferences',
    auth,
    NotificationsPreferencesController.updateGlobalPreferences
  )

  // Project-level preferences (frontend hook contract:
  // use-project-notification-preferences.ts)
  webRouter.get(
    '/notifications/preferences/project/:projectId',
    auth,
    NotificationsPreferencesController.getProjectPreferences
  )

  webRouter.post(
    '/notifications/preferences/project/:projectId',
    auth,
    NotificationsPreferencesController.saveProjectPreferences
  )

  // 2026-09-10 (owner queue 3): the legacy page is removed — email
  // preferences live in the hub (#/mysettings.email). Redirect (pattern:
  // /admin/instance-stats). The JSON preference API above is the hub's
  // live contract and stays. (The legacy form POST goes with the page.)
  webRouter.get(
    '/user/notification-preferences',
    auth,
    function (req, res) {
      res.redirect(301, '/hub#/mysettings.email')
    }
  )

  webRouter.post(
    '/user/notification-preferences',
    auth,
    function (req, res) {
      res.redirect(301, '/hub#/mysettings.email')
    }
  )

  webRouter.post(
    '/user/send-test-email',
    auth,
    NotificationsPreferencesController.sendTestEmail
  )

  // CE: the QueueWorkers consumer is saas-gated and never runs in CE
  // (Settings.overleaf undefined), so own the `project-notification`
  // worker here. No-op when the saas feature is enabled (or in tests).
  startProjectNotificationConsumer()
}

export default {
  apply,
}

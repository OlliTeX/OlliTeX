// @ts-check

import logger from '@overleaf/logger'
import SessionManager from '../../../../app/src/Features/Authentication/SessionManager.mjs'
import EmailHandler from '../../../../app/src/Features/Email/EmailHandler.mjs'
import UserGetter from '../../../../app/src/Features/User/UserGetter.mjs'
import { parseReq, z } from '../../../../app/src/infrastructure/Validation.mjs'
import NotificationsPreferencesHandler from './NotificationsPreferencesHandler.mjs'


const globalPreferencesShape = z.object({
  muteAllNotifications: z.boolean(),
  notificationDelayMinutes: z
    .number()
    .int()
    .min(1)
    .max(10080)
    .nullable()
    .optional(),
})
// PG-NP-2 (parity): parseReq validates against the REQUEST object (req.body,
// req.query, req.params are fields of req) — the original flat schema made
// every JSON POST to /notifications/preferences 400 with
// "expected boolean, received undefined". Wrap in { body } like the form
// schema so req.body is validated.
const globalPreferencesSchema = z.object({ body: globalPreferencesShape })

const projectPreferencesSchema = z.looseObject({})

async function getGlobalPreferences(req, res, next) {
  try {
    const userId = SessionManager.getLoggedInUserId(req.session)
    const preferences = await NotificationsPreferencesHandler.promises.getGlobalPreferences(
      userId
    )
    res.json(preferences)
  } catch (err) {
    next(err)
  }
}

async function updateGlobalPreferences(req, res, next) {
  try {
    const { body } = parseReq(req, globalPreferencesSchema)
    const userId = SessionManager.getLoggedInUserId(req.session)

    await NotificationsPreferencesHandler.promises.saveGlobalPreferences(
      userId,
      body
    )

    res.json(body)
  } catch (err) {
    next(err)
  }
}

// 2026-09-10 (owner queue 3): the legacy /user/notification-preferences
// form handler is gone with the page (the hub saves via the JSON
// /notifications/preferences endpoint).

async function getProjectPreferences(req, res, next) {
  try {
    const userId = SessionManager.getLoggedInUserId(req.session)
    const preferences = await NotificationsPreferencesHandler.promises.getProjectPreferences(
      userId,
      req.params.projectId
    )
    res.json(preferences)
  } catch (err) {
    next(err)
  }
}

async function saveProjectPreferences(req, res, next) {
  try {
    const { body } = parseReq(req, projectPreferencesSchema)
    const userId = SessionManager.getLoggedInUserId(req.session)

    await NotificationsPreferencesHandler.promises.saveProjectPreferences(
      userId,
      req.params.projectId,
      body
    )

    res.json(null)
  } catch (err) {
    next(err)
  }
}

async function sendTestEmail(req, res, next) {
  try {
    const userId = SessionManager.getLoggedInUserId(req.session)
    const user = await UserGetter.promises.getUser(userId, { email: 1 })

    if (!user || !user.email) {
      throw new Error('User email not found')
    }

    await EmailHandler.promises.sendEmail('testEmail', { to: user.email })

    res.json({ message: res.locals.translate('email_sent') })
  } catch (err) {
    logger.warn({ err, userId: req.session?.userId }, 'failed to send test email')
    next(err)
  }
}

export default {
  getGlobalPreferences,
  updateGlobalPreferences,
  getProjectPreferences,
  saveProjectPreferences,
  sendTestEmail,
}

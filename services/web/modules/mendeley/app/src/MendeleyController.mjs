import crypto from 'node:crypto'
import logger from '@overleaf/logger'
import SessionManager from '../../../../app/src/Features/Authentication/SessionManager.mjs'
import MendeleyApiClient from './MendeleyApiClient.mjs'
import {
  MendeleyAccountNotLinkedError,
  MendeleyExpiredError,
  MendeleyForbiddenError,
} from './MendeleyApiClient.mjs'

/**
 * The account-settings references surface on this fork (2026-09-07): the
 * unified /hub My settings references section. OAuth popup flows land back
 * here once complete (the widget re-checks the connection on mount, so the
 * popup round-trip updates the UI without a query-string flag).
 */
const REFERENCES_PAGE = '/hub#mysettings.references'

/**
 * GET /user/mendeley/status
 * { configured, connected } — configured: the instance has Mendeley
 * credentials; connected: the user has a linked account. Lets the widgets
 * render the right state WITHOUT a 5xx when the connector is unconfigured.
 */
async function status(req, res) {
  const configured = MendeleyApiClient.isServiceConfigured()
  const userId = SessionManager.getLoggedInUserId(req.session)
  let connected = false
  if (configured && userId) {
    try {
      connected = await MendeleyApiClient.isLinked(userId)
    } catch {
      connected = false
    }
  }
  res.json({ configured, connected })
}

/**
 * GET /mendeley/groups
 * Returns the user's Mendeley groups (for the create-file modal).
 */
async function getGroups(req, res) {
  if (!MendeleyApiClient.isServiceConfigured()) {
    return res.status(403).json({
      error: 'not_configured',
      message: 'mendeley_groups_relink',
    })
  }
  const userId = SessionManager.getLoggedInUserId(req.session)
  try {
    const groups = await MendeleyApiClient.getGroupsForUser(userId)
    res.json({ groups })
  } catch (err) {
    // _authHeaders refreshes the token before the request, so an expired or
    // unlinked account surfaces here rather than as an API 401/403.
    if (
      err instanceof MendeleyForbiddenError ||
      err instanceof MendeleyExpiredError ||
      err instanceof MendeleyAccountNotLinkedError
    ) {
      return res.status(403).json({
        error: 'forbidden',
        message: 'mendeley_groups_relink',
      })
    }
    logger.err({ err, userId }, 'error fetching Mendeley groups')
    res.status(500).json({
      error: 'internal',
      message: 'mendeley_groups_loading_error',
    })
  }
}

/**
 * GET /user/mendeley/oauth
 * Start the Mendeley OAuth 2.0 flow: generate a CSRF `state`, remember it in
 * the session and redirect the user to Mendeley's authorization page.
 */
async function oauth(req, res) {
  const userId = SessionManager.getLoggedInUserId(req.session)
  try {
    const state = crypto.randomBytes(16).toString('hex')
    req.session.mendeleyOAuthState = state
    res.redirect(MendeleyApiClient.getOAuthAuthorizeUrl(state).toString())
  } catch (err) {
    logger.err({ err, userId }, 'error starting Mendeley OAuth flow')
    res.redirect(REFERENCES_PAGE)
  }
}

/**
 * GET /user/mendeley/oauth/callback
 * Complete the Mendeley OAuth flow: verify `state`, exchange the
 * authorization code for access + refresh tokens and store them encrypted.
 */
async function oauthCallback(req, res) {
  const userId = SessionManager.getLoggedInUserId(req.session)
  const { code, state } = req.query
  const expectedState = req.session.mendeleyOAuthState
  delete req.session.mendeleyOAuthState

  if (!code || !state || !expectedState || state !== expectedState) {
    return res.redirect(REFERENCES_PAGE)
  }

  try {
    const tokens = await MendeleyApiClient.exchangeCodeForToken(code)
    await MendeleyApiClient.storeCredentials(userId, tokens)
    res.redirect(REFERENCES_PAGE)
  } catch (err) {
    logger.err({ err, userId }, 'error completing Mendeley OAuth flow')
    res.redirect(REFERENCES_PAGE)
  }
}

/**
 * POST /mendeley/unlink
 * Unlinks the user's Mendeley account.
 */
async function unlink(req, res) {
  const userId = SessionManager.getLoggedInUserId(req.session)
  try {
    await MendeleyApiClient.unlinkAccount(userId)
    res.sendStatus(200)
  } catch (err) {
    logger.err({ err, userId }, 'error unlinking Mendeley')
    res.sendStatus(500)
  }
}

export default {
  status,
  getGroups,
  oauth,
  oauthCallback,
  unlink,
}

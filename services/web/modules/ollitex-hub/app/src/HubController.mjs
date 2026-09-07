import Path from 'path'
import { fileURLToPath } from 'node:url'
import { expressify } from '@overleaf/promise-utils'
import Settings from '@overleaf/settings'
import SessionManager from '../../../../app/src/Features/Authentication/SessionManager.mjs'
import { User } from '../../../../app/src/models/User.mjs'
import AuthorizationManager from '../../../../app/src/Features/Authorization/AuthorizationManager.mjs'
import UserSettingsHelper from '../../../../app/src/Features/Project/UserSettingsHelper.mjs'
import { getHubTheme, saveHubTheme, clearHubTheme } from './HubTheme.mjs'

const __dirname = Path.dirname(fileURLToPath(import.meta.url))

// Same derivation as admin-tools UserListController (the Users manager's
// new-user form reads these at module level and crashes without them).
const externalAuth = process.env.EXTERNAL_AUTH
  ? process.env.EXTERNAL_AUTH.split(/\s+/).filter(m => m && m !== 'none')
  : []
const availableAuthMethods = ['local', ...externalAuth]
const userIsAdminUpdatedOnLogin = Object.fromEntries(
  availableAuthMethods.map(m => [
    m,
    Boolean(Settings[m]?.attAdmin) && Boolean(Settings[m]?.valAdmin),
  ])
)
const userDetailsUpdatedOnLogin = Object.fromEntries(
  availableAuthMethods.map(m => [
    m,
    Boolean(Settings[m]?.updateUserDetailsOnLogin),
  ])
)

/**
 * OlliTeX hub (owner 2026-09-06/07): ONE unified page — /hub.
 * Workspace + administration in a single nested-accordion rail
 * (nav_structure.md). /hub/admin and /hub/workspace redirect there.
 */
async function buildLocals(req, res) {
  const userId = SessionManager.getLoggedInUserId(req.session)
  const user = userId ? await User.findById(userId, 'ace') : null
  const userSettings = user
    ? await UserSettingsHelper.buildUserSettings(req, res, user)
    : {}
  let hubAdmin = false
  try {
    hubAdmin = userId ? await AuthorizationManager.promises.isUserSiteAdmin(userId) : false
  } catch {
    hubAdmin = false
  }
  let hubTheme = null
  try {
    hubTheme = await getHubTheme()
  } catch {
    hubTheme = null
  }
  return {
    userSettings,
    hubAdmin,
    hubTheme,
    availableAuthMethods,
    userIsAdminUpdatedOnLogin,
    userDetailsUpdatedOnLogin,
  }
}

async function hubPage(req, res) {
  const locals = await buildLocals(req, res)
  res.render(
    Path.resolve(__dirname, '../views/hub'),
    {
      title: 'OlliTeX Hub',
      ...locals,
    }
  )
}

async function redirectToHub(req, res) {
  res.redirect(302, '/hub')
}

// M2.5 Appearance theme API (nav_structure.md §8.8)
async function getTheme(req, res) {
  const theme = await getHubTheme()
  res.json(theme)
}

async function saveTheme(req, res) {
  try {
    const theme = await saveHubTheme(req && req.body ? req.body : {})
    res.json(theme)
  } catch (err) {
    res.status(400)
    res.json({ error: (err && err.message) || 'Invalid theme' })
  }
}

async function clearTheme(req, res) {
  await clearHubTheme()
  res.json({ ok: true })
}

export default {
  hubPage: expressify(hubPage),
  redirectToHub: expressify(redirectToHub),
  getTheme: expressify(getTheme),
  saveTheme: expressify(saveTheme),
  clearTheme: expressify(clearTheme),

  // kept for one release for in-flight tabs/bookmarks beyond the redirects
  adminHubPage: expressify(async (req, res) => {
    res.redirect(302, '/hub')
  }),

  workspaceHubPage: expressify(async (req, res) => {
    res.redirect(302, '/hub')
  }),
}

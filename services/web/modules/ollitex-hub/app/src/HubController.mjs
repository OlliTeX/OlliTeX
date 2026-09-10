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
  // 2026-09-07 (owner #6b): the hub's sync + references leaves embed the
  // real provider widgets, which read ol-ExposedSettings / ol-gitBridgeEnabled
  // at render time — expose both as page meta.
  return {
    userSettings,
    hubAdmin,
    hubTheme,
    hubUserJson: user ? JSON.stringify(user) : 'null',
    gitBridgeEnabled: Settings.gitBridgeEnabled === 'true',
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

// overleaf-lab #14 (2026-09-08): server core health for the /hub
// "Hub health" diagnostics leaf. Read-only, best-effort per probe: a failing
// sub-probe reports its error string instead of failing the whole endpoint
// (the point of the page is to SHOW which part is down).
async function hubHealth(req, res) {
  const out = {
    ok: true,
    generatedAt: new Date().toISOString(),
    uptimeSec: Math.round(process.uptime()),
    platform: {
      node: process.version,
      arch: process.arch,
      pid: process.pid,
      os: `${process.platform}`,
    },
    instance: {
      appName: Settings.appName,
      env: Settings.env,
      siteUrl: Settings.siteUrl,
      overleaf: Settings.overleaf != null,
    },
    mongo: null,
    featureGates: null,
  }

  // mongo link state (mongoose + native connection are the ones the web
  // service runs its API on)
  try {
    const Mongoose = (await import('../../../../app/src/infrastructure/Mongoose.mjs')).default
    const st = Mongoose.connection.readyState
    const names = { 0: 'disconnected', 1: 'connecting', 2: 'connected', 3: 'disconnecting' }
    let pingMs = null
    try {
      const t0 = Date.now()
      if (Mongoose.connection.db && typeof Mongoose.connection.db.admin === 'function') {
        await Mongoose.connection.db.admin().ping()
      }
      pingMs = Date.now() - t0
    } catch {
      pingMs = null
    }
    out.mongo = { readyState: st, state: names[st] || `unknown(${st})`, pingMs }
  } catch (err) {
    out.mongo = { readyState: null, state: 'probe-failed', error: String((err && err.message) || err) }
  }

  // the feature gates that explain why widgets/entries may be hidden in the
  // hub (mirrors the ExposedSettings derivation in ExpressLocals, best-effort)
  try {
    let languageToolDisabledByAdmin = false
    let llmDisabledByAdmin = false
    let llmCfg = null
    try {
      // readAdminSettings reads a JSON file; report null in contexts where
      // the admin settings file does not exist
      const mod = await import('../../../../modules/languagetool/app/src/adminConfig.mjs')
      if (mod && typeof mod.readAdminSettings === 'function') {
        const cfg = await mod.readAdminSettings()
        if (cfg) {
          languageToolDisabledByAdmin = cfg.languageToolDisabledByAdmin === true
          llmDisabledByAdmin = cfg.llmDisabledByAdmin === true
          llmCfg = cfg
        }
      }
    } catch {
      // admin settings unavailable → report the env-only view
    }
    out.featureGates = {
      llmEnabled: Settings.llm?.enabled ?? false,
      llmAdminEnabled: !llmDisabledByAdmin,
      llmAllowUserSettings: Settings.llm?.allowUserSettings ?? false,
      llmServerConfigured: !!(
        (llmCfg?.llmApiUrl && llmCfg?.llmApiKey) ||
        (process.env.LLM_API_URL && process.env.LLM_API_KEY) ||
        (Settings.llm?.apiUrl && Settings.llm?.apiKey)
      ),
      zotero: !!(Settings.zotero?.clientKey && Settings.zotero?.clientSecret) ||
        (Array.isArray(Settings.enabledLinkedFileTypes) && Settings.enabledLinkedFileTypes.includes('zotero')),
      webdav: !!Settings.webdav,
      mendeley: !!(Settings.mendeley && (Settings.mendeley.CLIENT_ID || process.env.MENDELEY_CLIENT_ID)),
      dropbox: !!(process.env.DROPBOX_APP_KEY && process.env.DROPBOX_APP_SECRET),
      githubSync: !!(Settings.githubSync?.clientID && Settings.githubSync?.clientSecret),
      languageToolAvailable: !languageToolDisabledByAdmin && !!(
        process.env.LANGUAGE_TOOL_URL || process.env.LANGUAGE_TOOL_HOST ||
        process.env.LANGUAGE_TOOL_PORT || process.env.LANGUAGETOOL_URL
      ),
      email: !!(Settings.email && Settings.email.SMTP_URL),
    }
  } catch (err) {
    out.featureGates = { error: String((err && err.message) || err) }
  }

  res.json(out)
}

export default {
  hubPage: expressify(hubPage),
  redirectToHub: expressify(redirectToHub),
  getTheme: expressify(getTheme),
  saveTheme: expressify(saveTheme),
  clearTheme: expressify(clearTheme),
  hubHealth: expressify(hubHealth),

  // kept for one release for in-flight tabs/bookmarks beyond the redirects
  adminHubPage: expressify(async (req, res) => {
    res.redirect(302, '/hub')
  }),

  workspaceHubPage: expressify(async (req, res) => {
    res.redirect(302, '/hub')
  }),
}

import UserGetter from './UserGetter.mjs'
import OError from '@overleaf/o-error'
import UserSessionsManager from './UserSessionsManager.mjs'
import logger from '@overleaf/logger'
import Settings from '@overleaf/settings'
import AuthenticationController from '../Authentication/AuthenticationController.mjs'
import SessionManager from '../Authentication/SessionManager.mjs'
import { getSection } from '../SiteSettings/SiteSettingsManager.mjs'
import _ from 'lodash'
import { expressify } from '@overleaf/promise-utils'
import Features from '../../infrastructure/Features.mjs'
import Modules from '../../infrastructure/Modules.mjs'
import SplitTestHandler from '../SplitTests/SplitTestHandler.mjs'
import {
  z,
  parseReq,
  getRawReqInput,
} from '../../infrastructure/Validation.mjs'

function popSessionValue(session, key) {
  const value = session[key]
  if (value) {
    delete session[key]
  }
  return value
}

const settingsPageSchema = z.object({
  query: z.object({
    remove: z.string().optional(),
    'oauth-complete': z.string().optional(),
  }),
})

async function settingsPage(req, res) {
  const { query } = parseReq(req, settingsPageSchema, { logOnly: true })
  const userId = SessionManager.getLoggedInUserId(req.session)
  const reconfirmationRemoveEmail = query.remove
  // SSO
  delete req.session.ssoError
  const ssoErrorMessage = popSessionValue(req.session, 'ssoErrorMessage')
  const projectSyncSuccessMessage = popSessionValue(
    req.session,
    'projectSyncSuccessMessage'
  )
  const projectSyncErrorMessage = popSessionValue(
    req.session,
    'projectSyncErrorMessage'
  )
  const referenceLinkingErrorMessage = popSessionValue(
    req.session,
    'referenceLinkingErrorMessage'
  )
  // Institution SSO
  let institutionLinked = _.get(req.session, ['saml', 'linked'])
  if (institutionLinked) {
    // copy object if exists because _.get does not
    institutionLinked = Object.assign(
      {
        hasEntitlement: _.get(req.session, ['saml', 'hasEntitlement']),
      },
      institutionLinked
    )
  }
  const samlError = _.get(req.session, ['saml', 'error'])
  const institutionEmailNonCanonical = _.get(req.session, [
    'saml',
    'emailNonCanonical',
  ])
  const institutionRequestedEmail = _.get(req.session, [
    'saml',
    'requestedEmail',
  ])

  const reconfirmedViaSAML = _.get(req.session, ['saml', 'reconfirmed'])
  delete req.session.saml
  let shouldAllowEditingDetails = true
  const externalAuth = req.user?.externalAuth
  if (externalAuth && Settings[externalAuth].updateUserDetailsOnLogin) {
    shouldAllowEditingDetails = false
  }
  const oauthProviders = Settings.oauthProviders || {}

  const user = await UserGetter.promises.getUser(userId)
  if (!user) {
    // The user has just deleted their account.
    return UserSessionsManager.removeSessionsFromRedis(
      { _id: userId },
      null,
      () => res.redirect('/')
    )
  }

  let personalAccessTokens
  try {
    const results = await Modules.promises.hooks.fire(
      'listPersonalAccessTokens',
      user._id
    )
    personalAccessTokens = results?.[0] ?? []
  } catch (error) {
    const err = OError.tag(error, 'listPersonalAccessTokens hook failed')
    logger.error({ err, userId }, err.message)
  }

  let currentManagedUserAdminEmail // SaaS managed users: always undefined in free fork

  // OlliTeX fork (free-only): SaaS "groups with Group SSO enabled" membership
  // removed with the group-subscription feature.


  await SplitTestHandler.promises.getAssignment(req, res, 'email-notifications')

  res.render('user/settings', {
    title: 'account_settings',
    user: {
      id: user._id,
      isAdmin: user.isAdmin,
      email: user.email,
      allowedFreeTrial: user.allowedFreeTrial,
      first_name: user.first_name,
      last_name: user.last_name,
      alphaProgram: user.alphaProgram,
      betaProgram: user.betaProgram,
      labsProgram: user.labsProgram,
      features: {
        dropbox: user.features.dropbox,
        github: user.features.github,
        mendeley: user.features.mendeley,
        zotero: user.features.zotero,
        papers: user.features.papers,
        references: user.features.references,
      },
      refProviders: {
        mendeley: Boolean(user.refProviders?.mendeley),
        zotero: Boolean(user.refProviders?.zotero),
        papers: Boolean(user.refProviders?.papers),
      },
    },
    showAiFeatures: Boolean(user.aiFeatures?.enabled),
    labsExperiments: user.labsExperiments ?? [],
    hasPassword: !!user.hashedPassword,
    shouldAllowEditingDetails,
    oauthProviders: UserPagesController._translateProviderDescriptions(
      oauthProviders,
      req
    ),
    institutionLinked,
    samlError,
    institutionEmailNonCanonical:
      institutionEmailNonCanonical && institutionRequestedEmail
        ? institutionEmailNonCanonical
        : undefined,
    reconfirmedViaSAML,
    reconfirmationRemoveEmail,
    samlBeta: req.session.samlBeta,
    ssoErrorMessage,
    thirdPartyIds: UserPagesController._restructureThirdPartyIds(user),
    projectSyncSuccessMessage,
    projectSyncErrorMessage,
    referenceLinkingErrorMessage,
    personalAccessTokens,
    emailAddressLimit: Settings.emailAddressLimit,
    isManagedAccount: !!req.managedBy,
    userRestrictions: Array.from(req.userRestrictions || []),
    currentManagedUserAdminEmail,
    gitBridgeEnabled: Settings.enableGitBridge,
    isSaas: Features.hasFeature('saas'),
    memberOfSSOEnabledGroups: [],
    capabilities: [...req.capabilitySet],
  })
}

async function accountSuspended(req, res) {
  if (SessionManager.isUserLoggedIn(req.session)) {
    return res.redirect('/project')
  }
  res.render('user/accountSuspended', {
    title: 'your_account_is_suspended',
  })
}

async function logout(req, res) {
  const isLoggedIn = SessionManager.isUserLoggedIn(req.session)
  if (!isLoggedIn) {
    return res.redirect('/')
  }
  res.render('user/logout')
}

async function reconfirmAccountPage(req, res) {
  const pageData = {
    reconfirm_email: req.session.reconfirm_email,
  }

  res.render('user/reconfirm', pageData)
}

async function emailPreferencesPage(req, res) {
  const userId = SessionManager.getLoggedInUserId(req.session)
  const user = await UserGetter.promises.getUser(userId, {
    _id: 1,
    email: 1,
    first_name: 1,
    last_name: 1,
  })

  if (!user) {
    throw new Error('User not found')
  }

  let subscribed = false

  try {
    const [preferences] = await Modules.promises.hooks.fire(
      'getSubscriptionPreferences',
      userId
    )

    subscribed = Boolean(preferences?.newsletter)
  } catch (err) {
    logger.error({ err, userId }, 'Error fetching newsletter subscription')
  }

  res.render('user/email-preferences', {
    title: 'newsletter_info_title',
    customerIoEnabled: true,
    subscribed,
    user,
  })
}

const loginPageSchema = z.object({
  query: z.object({
    redir: z.string().optional(),
  }),
})

const UserPagesController = {
  accountSuspended: expressify(accountSuspended),
  logout: expressify(logout),

  registerPage(req, res) {
    const sharedProjectData = req.session.sharedProjectData || {}

    const newTemplateData = {}
    if (req.session.templateData != null) {
      newTemplateData.templateName = req.session.templateData.templateName
    }

    res.render('user/register', {
      title: 'register',
      sharedProjectData,
      newTemplateData,
      samlBeta: req.session.samlBeta,
    })
  },

  loginPage(req, res) {
    const { query } = parseReq(req, loginPageSchema, { logOnly: true })

    // if user is being sent to /login with explicit redirect (redir=/foo),
    // such as being sent from the editor to /login, then set the redirect explicitly
    if (
      query.redir != null &&
      AuthenticationController.getRedirectFromSession(req) == null
    ) {
      AuthenticationController.setRedirectInSession(req, query.redir)
    }
    const metadata = { robotsNoindexNofollow: false }
    // any query param at all (not just `redir`) marks this page as noindex
    // -- e.g. error/SSO-flow flags added by other callers -- read the raw
    // query here rather than by name (case 1: verbatim forwarding)
    if (Object.keys(getRawReqInput(req).query).length !== 0) {
      metadata.robotsNoindexNofollow = true
    }
    // SSO multi-provider (2026-08-29): the login view reads
    // settings.ldap/saml/oidc (buttons, form hints). Those are now
    // admin-managed (site settings; stored wins over env) — merge the
    // resolved sections into a REQUEST-LOCAL settings object (never
    // mutate the global Settings object).
    const renderLogin = () => {
      res.render('user/login', {
        title: Settings.nav?.login_support_title || 'login',
        login_support_title: Settings.nav?.login_support_title,
        login_support_text: Settings.nav?.login_support_text,
        metadata,
        // OLiT Mantine auth surface: the React login page reads its config
        // (SSO buttons, LDAP field, support text) from ol-auth-config.
        authConfigJson: JSON.stringify({
          sso: [
            res.locals.samlEnable
              ? { label: res.locals.samlName, href: '/saml/login' }
              : null,
            res.locals.oidcEnable
              ? { label: res.locals.oidcName, href: '/oidc/login' }
              : null,
          ].filter(Boolean),
          ldapEnabled: !!res.locals.ldapEnable,
          ldapPlaceholder: res.locals.ldapEnable
            ? res.locals.ldapPlaceholder
            : undefined,
          supportTitle: Settings.nav?.login_support_title,
          supportText: Settings.nav?.login_support_text,
        }),
      })
    }
    return Promise.all([
      getSection('sso-saml', Settings).catch(() => ({ enabled: false })),
      getSection('sso-oidc', Settings).catch(() => ({ enabled: false })),
      getSection('sso-ldap', Settings).catch(() => ({ enabled: false })),
    ]).then(([saml, oidc, ldap]) => {
      res.locals.settings = {
        ...Settings,
        saml: {
          enable: saml.enabled === true,
          identityServiceName:
            saml.identityServiceName || 'Log in with SAML IdP',
        },
        oidc: {
          enable: oidc.enabled === true,
          identityServiceName:
            oidc.identityServiceName || 'Log in with SSO (OIDC)',
        },
        ldap: {
          enable: ldap.enabled === true,
          placeholder: ldap.placeholder || 'Username',
        },
      }
      res.locals.samlEnable = saml.enabled === true
      res.locals.samlName = saml.identityServiceName || 'Log in with SAML IdP'
      res.locals.oidcEnable = oidc.enabled === true
      res.locals.oidcName = oidc.identityServiceName || 'Log in with SSO (OIDC)'
      res.locals.ldapEnable = ldap.enabled === true
      res.locals.ldapPlaceholder = ldap.placeholder || 'Username'
      renderLogin()
    }).catch((err) => {
      // SSO resolution must never break the login page.
      logger.warn({ err }, 'login page: SSO section resolution failed')
      renderLogin()
    })
  },

  /**
   * Landing page for users who may have received one-time login
   * tokens from the read-only maintenance site.
   *
   * We tell them that Overleaf is back up and that they can login normally.
   */
  oneTimeLoginPage(req, res, next) {
    res.render('user/one_time_login')
  },

  renderReconfirmAccountPage: expressify(reconfirmAccountPage),

  settingsPage: expressify(settingsPage),

  sessionsPage(req, res, next) {
    const user = SessionManager.getSessionUser(req.session)
    logger.debug({ userId: user._id }, 'loading sessions page')
    const currentSession = {
      ip_address: user.ip_address,
      session_created: user.session_created,
    }
    UserSessionsManager.getAllUserSessions(
      user,
      [req.sessionID],
      (err, sessions) => {
        if (err != null) {
          OError.tag(err, 'error getting all user sessions', {
            userId: user._id,
          })
          return next(err)
        }
        res.render('user/sessions', {
          title: 'sessions',
          currentSession,
          sessions,
        })
      }
    )
  },

  // 2026-09-08 (overleaf-lab, owner): JSON twin of the sessions page for the
  // /hub sessions leaf — the hub must not link out to the classic page (the
  // legacy pages are being removed once /hub parity is complete).
  sessionsList(req, res, next) {
    const user = SessionManager.getSessionUser(req.session)
    const currentSession = {
      ip_address: user.ip_address,
      session_created: user.session_created,
    }
    UserSessionsManager.getAllUserSessions(user, [req.sessionID], (err, sessions) => {
      if (err != null) {
        OError.tag(err, 'error getting all user sessions', { userId: user._id })
        return next(err)
      }
      res.json({ currentSession, sessions })
    })
  },

  emailPreferencesPage: expressify(emailPreferencesPage),

  async compromisedPasswordPage(req, res) {
    res.render('user/compromised_password')
  },

  _restructureThirdPartyIds(user) {
    // 3rd party identifiers are an array of objects
    // this turn them into a single object, which
    // makes data easier to use in template
    if (
      !user.thirdPartyIdentifiers ||
      user.thirdPartyIdentifiers.length === 0
    ) {
      return null
    }
    return user.thirdPartyIdentifiers.reduce((obj, identifier) => {
      obj[identifier.providerId] = identifier.externalUserId
      return obj
    }, {})
  },

  _translateProviderDescriptions(providers, req) {
    const result = {}
    if (providers) {
      for (const provider in providers) {
        const data = providers[provider]
        data.description = req.i18n.translate(
          data.descriptionKey,
          Object.assign({}, data.descriptionOptions)
        )
        result[provider] = data
      }
    }
    return result
  },
}

export default UserPagesController

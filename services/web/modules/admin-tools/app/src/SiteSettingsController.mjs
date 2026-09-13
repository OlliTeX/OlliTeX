/**
 * Admin "Manage Site" API — SiteSettings sections
 * (templates / zotero / externalUrl / signup), per the user-designed
 * admin console (BIB_ORCID_TEMPLATES_PLAN.md §2.1 + decision 3.0).
 *
 * All endpoints are site-admin only (AuthorizationMiddleware
 * .ensureUserIsSiteAdmin in the router). Secrets are masked in GET
 * responses; PUT with an empty secret field keeps the stored value.
 */
import Path from 'node:path'
import { fileURLToPath } from 'node:url'
import Settings from '@overleaf/settings'
import logger from '@overleaf/logger'
import { expressify } from '@overleaf/promise-utils'
import SessionManager from '../../../../app/src/Features/Authentication/SessionManager.mjs'
import { User } from '../../../../app/src/models/User.mjs'
import UserSettingsHelper from '../../../../app/src/Features/Project/UserSettingsHelper.mjs'
import HttpErrorHandler from '../../../../app/src/Features/Errors/HttpErrorHandler.mjs'
import {
  getSection,
  setSection,
  cleanSectionInput,
  maskSecrets,
  SECTION_VALIDATORS,
} from '../../../../app/src/Features/SiteSettings/SiteSettingsManager.mjs'
import {
  buildStorageEnvLines,
  readStorageEnv,
  removeStorageEnv,
  renderStorageEnvFile,
  storageEnvPath,
  writeStorageEnv,
} from '../../../../app/src/Features/SiteSettings/StorageEnvFile.mjs'
import fs from 'node:fs/promises'
import { getRawReqInput } from '../../../../app/src/infrastructure/Validation.mjs'
import { Template } from '../../../template-gallery/app/src/models/Template.mjs'

const __dirname = Path.dirname(fileURLToPath(import.meta.url))

export default {
  /**
   * Render the Manage Site admin page (Tabs: Templates, Zotero,
   * External URLs, Sign Up).
   */
  manageSitePage: expressify(async (req, res) => {
    const userId = SessionManager.getLoggedInUserId(req.session)
    const user = await User.findById(userId, 'ace')

    const userSettings = await UserSettingsHelper.buildUserSettings(
      req,
      res,
      user
    )

    res.render(
      Path.resolve(__dirname, '../views/manage-site-react'),
      {
        title: 'Manage Extensions',
        userSettings,
      }
    )
  }),

  /**
   * GET /admin/site-settings — all sections (secrets masked) +
   * templates: per-category template counts.
   */
  getSiteSettings: expressify(async (req, res) => {
    const [templates, zotero, mendeley, externalUrl, signup, ssoSaml, ssoOidc, ssoLdap, sandboxedCompiles, gitIntegration, typst, githubSync, email, linkedFileTypes, pandoc, webdav, dropbox, misc, languagetool, llm, branding, services, storage] = await Promise.all([
      getSection('templates', Settings),
      getSection('zotero', Settings),
      getSection('mendeley', Settings),
      getSection('externalUrl', Settings),
      getSection('signup', Settings),
      getSection('sso-saml', Settings),
      getSection('sso-oidc', Settings),
      getSection('sso-ldap', Settings),
      getSection('sandboxed-compiles', Settings),
      getSection('git-integration', Settings),
      getSection('typst', Settings),
      getSection('github-sync', Settings),
      getSection('email', Settings),
      getSection('linked-file-types', Settings),
      getSection('pandoc', Settings),
      getSection('webdav', Settings),
      getSection('dropbox', Settings),
      getSection('misc', Settings),
      // 2026-09-09 (owner R10 #3): new admin/site sections
      getSection('languagetool', Settings),
      getSection('llm', Settings),
      getSection('branding', Settings),
      getSection('services', Settings),
      getSection('storage', Settings),
    ])

    // Template counts per category (same source as the gallery).
    const counts = {}
    const categories = templates.categories || []
    await Promise.all(
      categories.map(async (cat) => {
        const query =
          cat.key === 'all' ? {} : { category: `/templates/${cat.key}` }
        try {
          counts[cat.key] = await Template.countDocuments(query).exec()
        } catch (err) {
          logger.warn({ err, key: cat.key }, 'site-settings: template count failed')
          counts[cat.key] = null
        }
      })
    )

    // 2026-09-14 (owner): local storage — stored section wins; when the
    // admin never saved one, fall back to the live (effective) env values
    // so the hub shows the state of the running stack, plus a flag for
    // whether a managed env fragment is in place.
    const maskedStorage = maskSecrets('storage', storage) || {}
    const managedEnv = readStorageEnv()
    const storageBase = (maskedStorage && Object.keys(maskedStorage).length > 0)
      ? maskedStorage
      : (managedEnv && managedEnv.section) || {}
    const storageOut = {
      ...storageBase,
      backend: storageBase.backend || 'fs',
      envManaged: Boolean(managedEnv),
      envPath: managedEnv ? managedEnv.path : undefined,
      appliesOn: 'next container restart',
    }
    // secret never echoed back via the env fallback (UI shows an empty
    // "leave empty to keep" field instead)
    if (!maskedStorage || Object.keys(maskedStorage).length === 0) storageOut.s3Secret = undefined

    res.json({
      templates: { ...maskSecrets('templates', templates), counts },
      zotero: maskSecrets('zotero', zotero),
      mendeley: maskSecrets('mendeley', mendeley),
      externalUrl: maskSecrets('externalUrl', externalUrl),
      signup: maskSecrets('signup', signup),
      'sso-saml': maskSecrets('sso-saml', ssoSaml),
      'sso-oidc': maskSecrets('sso-oidc', ssoOidc),
      'sso-ldap': maskSecrets('sso-ldap', ssoLdap),
      'sandboxed-compiles': maskSecrets('sandboxed-compiles', sandboxedCompiles),
      'git-integration': maskSecrets('git-integration', gitIntegration),
      typst: maskSecrets('typst', typst),
      'github-sync': maskSecrets('github-sync', githubSync),
      email: maskSecrets('email', email),
      'linked-file-types': maskSecrets('linked-file-types', linkedFileTypes),
      pandoc: maskSecrets('pandoc', pandoc),
      webdav: maskSecrets('webdav', webdav),
      dropbox: maskSecrets('dropbox', dropbox),
      misc: maskSecrets('misc', misc),
      // 2026-09-09 (owner R10 #3): new admin/site sections
      languagetool: maskSecrets('languagetool', languagetool),
      llm: maskSecrets('llm', llm),
      branding: maskSecrets('branding', branding),
      services: maskSecrets('services', services),
      storage: storageOut,
    })
  }),

  /**
   * PUT /admin/site-settings/:section — replace one section
   * (validated; secrets encrypted at rest).
   */
  updateSiteSettings: expressify(async (req, res) => {
    const { params, body } = getRawReqInput(req)
    const section = params.section
    const validator = SECTION_VALIDATORS[section]
    if (!validator) {
      return HttpErrorHandler.unprocessableEntity(
        req,
        res,
        `Unknown section: ${section}`
      )
    }
    const errors = validator(body)
    if (errors.length > 0) {
      return HttpErrorHandler.unprocessableEntity(req, res, errors.join('; '))
    }

    const clean = cleanSectionInput(section, body)

    // Storage (2026-09-14, owner): the env fragment is written FIRST and
    // the DB save second; a DB failure rolls the fragment back so the two
    // never disagree. When the section is cleared to "fs", the fragment
    // still writes the fs backend (explicit rollback path).
    let previousManaged = null
    let restoreEnv = null
    if (section === 'storage') {
      previousManaged = await readStorageEnv()
      if (clean && Object.keys(clean).length > 0) {
        await writeStorageEnv(clean) // throws → 500, DB untouched
      } else {
        await removeStorageEnv()
      }
    }

    let result
    try {
      result = await setSection(section, clean)
    } catch (err) {
      if (section === 'storage') {
        // roll the fragment back so env and DB never disagree
        try {
          if (previousManaged) {
            await fs.writeFile(
              storageEnvPath(),
              renderStorageEnvFile(previousManaged.section),
              { mode: 0o644 }
            )
          } else {
            await removeStorageEnv()
          }
        } catch (rollbackErr) {
          logger.error(
            { err: rollbackErr, section },
            'site-settings: storage env rollback failed'
          )
        }
      }
      throw err
    }

    if (section === 'storage') {
      result = { ...result, appliesOn: 'next container restart', envLines: buildStorageEnvLines(clean || {}) }
    }
    logger.info(
      { section, result, userId: req.session?.user?.id },
      'site-settings: section updated'
    )
    // SSO: re-register the passport strategy from the new stored config so
    // the change applies without a container restart (best effort — the
    // login start also refreshes).
    const ssoType = section === 'sso-saml' ? 'saml'
      : section === 'sso-oidc' ? 'oidc'
        : section === 'sso-ldap' ? 'ldap' : null
    if (ssoType) {
      try {
        const runtime = await import('../../../authentication/sso-runtime.mjs')
        await runtime.refreshSsoStrategy(ssoType)
      } catch (err) {
        logger.warn({ err, section }, 'site-settings: SSO strategy refresh failed')
      }
    }
    res.json({ ok: true, ...result })
  }),
}

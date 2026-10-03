/**
 * Boot env-hydrator (2026-08-29, R9 §7.2/§7.4; 2026-09-04 module-gate hardening).
 *
 * Admin-managed sections (sandboxed compiles, git integration, GitHub
 * sync, email, linked file types, pandoc, webdav, dropbox, misc) replace
 * compose env vars. Consumers are **boot-time** readers (Settings snapshot,
 * docker-runner env, compile flags, …) — they read `process.env` while
 * their modules load. So, at web boot — BEFORE the Settings/config modules
 * evaluate — every stored section overrides the matching env var. Stored
 * wins (D2: changes apply on the next container cycle; no live restart
 * from the UI).
 *
 * SSO sections are intentionally NOT hydrated here (D7: SSO is purely
 * admin-managed, resolved per request by modules/authentication).
 *
 * 2026-09-04 hardening: ESM evaluation order cannot be relied upon to run
 * this module before every feature-module gate (observed live: the WebDAV
 * module gate evaluated between two of this module's awaits, and DropBox
 * shortly after). Therefore each feature-module index that gates on these
 * env vars calls `ensureEnvForSection(<its section>)` right before its
 * gate check — order-independent, same env mapping, single source of truth.
 */
import logger from '@overleaf/logger'

/**
 * 2026-09-09 (live R11 #5): standalone stored-section reader.
 *
 * CRITICAL: this module must NOT import SiteSettingsManager (or anything
 * that imports @overleaf/settings) — ESM evaluates an importer's static
 * graph BEFORE the top-level await below runs, and the first import of
 * config/settings.js freezes the Settings snapshot (enablePandocConversions,
 * splitTest overrides, email fields, …) with the pre-hydration env. That is
 * exactly what disabled pandoc conversions (Download Word/MD + New Project
 * import vanished) in the field. The reader below uses the raw mongodb
 * driver only — an app-graph-free dep that triggers no config build.
 */
let _globalDocPromise = null
async function readGlobalDoc() {
  if (!_globalDocPromise) {
    _globalDocPromise = (async () => {
      try {
        const url = process.env.OVERLEAF_MONGO_URL
        if (!url) return null
        const { MongoClient } = await import('mongodb')
        const client = new MongoClient(url, { serverSelectionTimeoutMS: 4000 })
        try {
          await client.connect()
          const doc = await client
            .db()
            .collection('site_settings')
            .findOne({ _id: 'global' })
          return doc || null
        } finally {
          await client.close()
        }
      } catch (err) {
        // allow retries on later calls instead of caching a failed attempt
        _globalDocPromise = null
        throw err
      }
    })()
  }
  return _globalDocPromise
}

const b = (v) => (v ? 'true' : 'false')

/* ------------------------------------------------------------------ */
/* Per-section env maps (single source of truth for the mapping).      */
/* ------------------------------------------------------------------ */

const toDaysMs = (days) =>
  Number.isFinite(Number(days)) && Number(days) > 0
    ? String(Math.round(Number(days) * 24 * 60 * 60 * 1000))
    : undefined

const SECTION_ENV_MAPS = {
  'sandboxed-compiles': (sc) => {
    const images = Array.isArray(sc.images) ? sc.images : []
    const enabled = !!sc.enabled
    return {
      SANDBOXED_COMPILES: b(enabled),
      SANDBOXED_COMPILES_SIBLING_CONTAINERS: b(enabled),
      SIBLING_CONTAINERS_ENABLED: b(enabled || !!sc.dockerRunner),
      DOCKER_RUNNER: b(enabled || !!sc.dockerRunner),
      SANDBOXED_COMPILES_HOST_DIR: sc.hostDir || '',
      COMPILES_HOST_DIR: sc.hostDir || '',
      DOCKER_SOCKET_PATH: sc.socketPath || '',
      TEX_COMPILER_EXTRA_FLAGS: sc.extraFlags || '',
      TEXLIVE_IMAGE_USER: sc.imageUser || '',
      ALL_TEX_LIVE_DOCKER_IMAGES: images.map((r) => r?.image).filter(Boolean).join(','),
      ALL_TEX_LIVE_DOCKER_IMAGE_NAMES: images
        .map((r) => (r?.name || r?.image || '').trim())
        .join(','),
      TEX_LIVE_DOCKER_IMAGE:
        sc.defaultImage || (images[0] && images[0].image) || '',
      // 2026-09-09 (owner R10 #3): compose cleanup — compile body limit
      // moves to admin/site (sandboxed compiles tab) as well.
      COMPILE_BODY_SIZE_LIMIT_MB: sc.compileBodySizeLimitMb ?? '',
    }
  },
  'git-integration': (git) => ({
    GIT_BRIDGE_ENABLED: b(git.enabled),
    GIT_BRIDGE_HOST: git.host || '',
    GIT_BRIDGE_PORT: git.port ?? '',
  }),
  'typst': (t) => ({
    // default ON if absent (product default: COMPILE_TYPEST_ENABLED !== 'false')
    COMPILE_TYPEST_ENABLED: b(t.enabled === undefined ? true : t.enabled),
    CLSI_TYPEST_URL: t.url ?? '',
  }),
  'github-sync': (gh) => ({
    GITHUB_SYNC_ENABLED: b(gh.enabled),
    GITHUB_SYNC_CLIENT_ID: gh.clientId || gh.clientID || '',
    GITHUB_SYNC_CLIENT_SECRET: gh.clientSecret || '',
    GITHUB_TOKEN_CIPHER_FILE: gh.cipherFile || '',
    GITHUB_TOKEN_CIPHER_LABEL: gh.cipherLabel || '',
  }),
  // CE's Settings email block reads LONG OVERLEAF_EMAIL_* names
  // (server-ce/config/settings.js) — hydrate those, not short EMAIL_*.
  email: (email) => ({
    EMAIL_CONFIRMATION_DISABLED: b(email.skipConfirmation),
    OVERLEAF_EMAIL_FROM_ADDRESS: email.fromAddress || '',
    OVERLEAF_EMAIL_REPLY_TO: email.replyTo || '',
    OVERLEAF_EMAIL_DRIVER: email.driver || 'smtp',
    OVERLEAF_EMAIL_SMTP_HOST: email.host || '',
    OVERLEAF_EMAIL_SMTP_PORT: email.port ?? '',
    OVERLEAF_EMAIL_SMTP_SECURE: b(email.secure),
    OVERLEAF_EMAIL_SMTP_IGNORE_TLS: b(email.ignoreTLS),
    OVERLEAF_EMAIL_SMTP_NAME: email.name || '',
    OVERLEAF_EMAIL_SMTP_USER: email.user || '',
    OVERLEAF_EMAIL_SMTP_PASS: email.pass || '',
    OVERLEAF_EMAIL_AWS_SES_ACCESS_KEY_ID: email.accessKeyId || '',
    OVERLEAF_EMAIL_AWS_SES_SECRET_KEY: email.sesSecret || '',
    OVERLEAF_EMAIL_AWS_SES_REGION: email.sesRegion || '',
    // 2026-09-09 (owner R10 #3): admin contact email + custom footer
    OVERLEAF_ADMIN_EMAIL: email.adminEmail || '',
    OVERLEAF_CUSTOM_EMAIL_FOOTER: email.customFooter || '',
  }),
  'linked-file-types': (lft) => {
    const types = Array.isArray(lft.enabledTypes) ? lft.enabledTypes : []
    // D5: the fixed pair is always present, in the canonical first slots.
    const merged = ['project_file', 'project_output_file']
    for (const t of types) if (!merged.includes(t)) merged.push(t)
    return { ENABLED_LINKED_FILE_TYPES: merged.join(',') }
  },
  pandoc: (pandoc) => ({
    ENABLE_PANDOC_CONVERSIONS: b(pandoc.enabled),
    PANDOC_IMAGE: pandoc.image || '',
  }),
  webdav: (webdav) => ({
    WEBDAV_ENABLED: b(webdav.enabled),
    WEBDAV_ROOT_PATH: webdav.rootPath || '',
    WEBDAV_REQUEST_TIMEOUT_MS: webdav.requestTimeoutMs ?? '',
    WEBDAV_RETRY_COUNT: webdav.retryCount ?? '',
    WEBDAV_RETRY_DELAY_MS: webdav.retryDelayMs ?? '',
    WEBDAV_TOKEN_CIPHER_LABEL: webdav.cipherLabel || '',
    WEBDAV_TOKEN_CIPHER_PASSWORD: webdav.cipherPassword || '',
    WEBDAV_TOKEN_CIPHER_PREVIOUS_LABEL: webdav.previousCipherLabel || '',
    WEBDAV_TOKEN_CIPHER_PREVIOUS_PASSWORD: webdav.previousCipherPassword || '',
  }),
  dropbox: (dropbox) => ({
    DROPBOX_ENABLED: b(dropbox.enabled),
    DROPBOX_APP_KEY: dropbox.appKey || '',
    DROPBOX_APP_SECRET: dropbox.appSecret || '',
  }),
  // 2026-09-09 (owner R10 #3): new admin/site sections replacing compose env
  templates: (tpl) => ({
    OVERLEAF_NON_ADMIN_CAN_PUBLISH_TEMPLATES: b(tpl.nonAdminCanPublishTemplates),
  }),
  languagetool: (lt) => ({
    LANGUAGETOOL_URL: lt.enabled !== false ? lt.url || '' : '',
  }),
  llm: (llm) => ({
    LLM_ENABLED: b(llm.enabled),
    LLM_ALLOW_USER_SETTINGS: b(llm.allowUserSettings),
    LLM_USER_RATE_PER_MINUTE: llm.userRatePerMinute ?? '',
    LLM_ADMIN_RATE_PER_MINUTE: llm.adminRatePerMinute ?? '',
    LLM_USER_DAILY_TOKENS: llm.userDailyTokens ?? '',
  }),
  branding: (br) => ({
    OVERLEAF_NAV_TITLE: br.navTitle || '',
    OVERLEAF_LEFT_FOOTER: br.leftFooter || '',
    OVERLEAF_RIGHT_FOOTER: br.rightFooter || '',
  }),
  services: (sv) => ({
    V1_HISTORY_URL: sv.v1HistoryUrl || '',
    GITHUBINTERFACE_API_URL: sv.githubInterfaceUrl || '',
    GITHUBINTERFACE_WORKDIR_ROOT:
      sv.githubInterfaceWorkdirRoot || '/var/lib/overleaf/ghif',
    WEBDAVINTERFACE_API_URL: sv.webdavInterfaceUrl || '',
    DROPBOXINTERFACE_API_URL: sv.dropboxInterfaceUrl || '',
    DATAMANIPULATOR_API_URL: sv.dataManipulatorUrl || '',
  }),
  misc: (misc) => ({
    APP_NAME: misc.appName || '',
    NAV_HIDE_POWERED_BY: b(misc.navHidePoweredBy),
    ROBOTS_NOINDEX: b(misc.robotsNoindex),
    // 2026-09-09 (owner R10 #3): project-change notification delay
    PROJECT_CHANGE_NOTIFICATION_MIN_DELAY_MS: misc.projectChangeNotificationDelayMs ?? '',
    OVERLEAF_ALLOW_PUBLIC_ACCESS: b(misc.allowPublicAccess),
    OVERLEAF_ALLOW_ANONYMOUS_READ_AND_WRITE_SHARING:
      b(misc.allowAnonymousReadWriteSharing),
    OVERLEAF_DISABLE_LINK_SHARING: b(misc.disableLinkSharing),
    OVERLEAF_DISABLE_CHAT: b(misc.disableChat),
    OVERLEAF_PROJECT_HARD_DELETION_DELAY: toDaysMs(misc.projectHardDeletionDelayDays),
    OVERLEAF_USER_HARD_DELETION_DELAY: toDaysMs(misc.userHardDeletionDelayDays),
    OVERLEAF_HISTORY_RESTORE: b(misc.historyRestore),
    ENABLE_PDF_CACHING: b(misc.enablePdfCaching),
    // python-runner CE feature flag (admin/site misc.pythonRunner — the
    // admin toggle is the canonical control; ENABLE_PYTHON_RUNNER in
    // compose is only a seed for first boot, see SiteSettingsManager misc
    // seed). Read by settings.defaults splitTestOverrides['overleaf-code'].
    ENABLE_PYTHON_RUNNER: b(misc.pythonRunner),
    MAX_UPLOAD_SIZE: misc.maxUploadSizeMiB ? String(misc.maxUploadSizeMiB) : '',
    MAX_ENTITIES_PER_PROJECT: misc.maxEntitiesPerProject
      ? String(misc.maxEntitiesPerProject)
      : '',
    DEFAULT_LATEX_COMPILER: misc.defaultLatexCompiler || '',
  }),
}

function applyEntries(entries) {
  for (const [envName, value] of Object.entries(entries)) {
    if (value === undefined || value === null) continue
    process.env[envName] = String(value)
  }
  // 2026-09-09 (R11 #9): stored env values just changed — invalidate any
  // already-frozen @overleaf/settings snapshot so the lazy proxy
  // (services/web/config/settings.defaults.js) re-bakes with the stored
  // values. No-op in processes without that proxy.
  if (typeof globalThis.OL_SETTINGS_REBUILD === 'function') {
    try {
      globalThis.OL_SETTINGS_REBUILD()
    } catch {
      /* never block boot on the cache stamp */
    }
  }
}

/**
 * Hydrate env for ONE section from the stored site settings (no-op when the
 * admin never saved the section — existing env/defaults stand). Safe to call
 * repeatedly (idempotent) and from anywhere in the boot graph.
 *
 * @param {string} name section key (e.g. 'webdav', 'dropbox', 'pandoc')
 * @returns {Promise<boolean>} true when stored values were applied
 */
export async function ensureEnvForSection(name) {
  const map = SECTION_ENV_MAPS[name]
  if (!map) return false
  const stored = (await readGlobalDoc())?.[name]
  if (!stored || typeof stored !== 'object') return false
  applyEntries(map(stored))
  logger.info({ section: name }, 'boot: env hydrated from stored site settings')
  return true
}

/**
 * @returns {Promise<void>} resolves after env is hydrated for every stored
 * section. Never throws — boot must not fail because of optional settings.
 */
export async function hydrateEnvFromStoredSiteSettings() {
  for (const name of Object.keys(SECTION_ENV_MAPS)) {
    try {
      await ensureEnvForSection(name)
    } catch (err) {
      // Boot must never fail because of optional stored settings.
      logger.warn({ err, section: name }, 'boot: stored site-settings hydration failed (env defaults stand)')
    }
  }
}

// 2026-09-09 (R11 live fix, owner): pandoc/conversions REGRESSED in R10's
// lazy module-gate design — the gate fires only for sections a module
// imports (llm/webdav/dropbox/languagetool/github-sync); PANDOC has no
// such module, so ENABLE_PANDOC_CONVERSIONS never reached the process
// env in time → Download "Word/md" + New project imports vanished.
//
// Live probing proved the deeper issue: in the real web process the
// @overleaf/settings snapshot can be frozen BEFORE this top-level await
// finishes (some ESM import evaluates first), so TLA order alone is NOT
// the guarantee. The guarantee is the stamp: applyEntries() calls
// globalThis.OL_SETTINGS_REBUILD() and web's config/settings.defaults.js
// exports a lazy proxy that re-bakes once per stamp bump. The TLA below
// is kept because it hydrates early in the common case; the stamps make
// correctness order-independent. The per-module ensureEnvForSection()
// gates stay as defense for out-of-order importers.
await hydrateEnvFromStoredSiteSettings()

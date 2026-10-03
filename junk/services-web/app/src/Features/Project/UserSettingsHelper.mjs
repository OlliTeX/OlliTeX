const SYSTEM_THEME_USER_CUTOFF_DATE = new Date(Date.UTC(2026, 2, 2, 12, 0, 0)) // 12pm GMT on March 2, 2026

import logger from '@overleaf/logger'

function getOverallTheme(user) {
  if (user.ace.overallTheme != null) {
    return user.ace.overallTheme
  }

  if (user.signUpDate < SYSTEM_THEME_USER_CUTOFF_DATE) {
    // default / dark
    return ''
  }

  return 'system'
}

/**
 * Build the settings for a reference provider (zotero, mendeley, papers).
 *
 * The group entries are rebuilt explicitly so that the `_id` stored on older
 * documents doesn't leak to the frontend, which posts these settings back to us
 * unchanged.
 *
 * @param {object | undefined} settings the provider settings from `user.ace`
 */
function buildRefProviderSettings(settings) {
  if (settings == null) {
    return settings
  }
  return {
    enabled: settings.enabled,
    disablePersonalLibrary: settings.disablePersonalLibrary,
    groups: (settings.groups ?? []).map(group => ({ id: group.id })),
  }
}

function getInitialTheme(overallThemeSetting) {
  switch (overallThemeSetting) {
    case 'light-':
      return 'light'
    case '':
      return 'dark'
    case 'system':
      return 'system'
    default:
      return 'dark'
  }
}

/**
 * 2026-09-09 (live R11 #5): the keybindings field is a Mongoose Map.
 * On fully-hydrated documents it is a Map (iterable), but callers pass
 * LEAN/plain user objects (e.g. `User.findOne().lean()` in
 * LLMSettingsController) where it is a plain object — and
 * `Object.fromEntries(plainObject)` throws "object is not iterable",
 * which crashed the web process on /user/llm-settings and
 * /templates/manage (502s in the field). Handle both shapes.
 */
function customKeybindingsToPlain(ck) {
  if (!ck) return {}
  try {
    if (typeof ck.entries === 'function') {
      // Mongoose Map / JS Map on hydrated documents
      return Object.fromEntries(ck)
    }
    if (typeof ck === 'object') {
      // lean / toJSON documents
      const out = {}
      for (const [k, v] of Object.entries(ck)) {
        if (typeof v === 'string') out[k] = v
      }
      return out
    }
  } catch (err) {
    logger.warn(err, 'user settings: customKeybindings readback failed')
  }
  return {}
}

async function buildUserSettings(_req, _res, user) {
  return {
    mode: user.ace.mode,
    editorTheme: user.ace.theme,
    editorLightTheme: user.ace.lightTheme,
    editorDarkTheme: user.ace.darkTheme,
    fontSize: user.ace.fontSize,
    autoComplete: user.ace.autoComplete,
    autoPairDelimiters: user.ace.autoPairDelimiters,
    pdfViewer: user.ace.pdfViewer,
    syntaxValidation: user.ace.syntaxValidation,
    previewTabs: user.ace.previewTabs ?? false,
    fontFamily: user.ace.fontFamily || 'lucida',
    lineHeight: user.ace.lineHeight || 'normal',
    overallTheme: getOverallTheme(user),
    mathPreview: user.ace.mathPreview,
    breadcrumbs: user.ace.breadcrumbs,
    editorTabs: user.ace.editorTabs ?? true,
    nonBlinkingCursor: user.ace.nonBlinkingCursor ?? false,
    referencesSearchMode: user.ace.referencesSearchMode,
    darkModePdf: user.ace.darkModePdf ?? false,
    floatingMenu: user.ace.floatingMenu ?? true,
    // 2026-09-09 (owner R9 #4): custom keybindings (command id → key string;
    // '' = cleared). Map → plain object for the frontend (live R11 #5:
    // Map AND plain-object safe).
    customKeybindings: customKeybindingsToPlain(user.ace.customKeybindings),
    zotero: buildRefProviderSettings(user.ace.zotero),
    mendeley: buildRefProviderSettings(user.ace.mendeley),
    papers: buildRefProviderSettings(user.ace.papers),
  }
}

export default {
  buildUserSettings,
  getInitialTheme,
}

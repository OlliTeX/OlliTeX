import logger from '@overleaf/logger'
import Settings from '@overleaf/settings'
import { getSection } from '../../../../app/src/Features/SiteSettings/SiteSettingsManager.mjs'

/**
 * Resolve the live Mendeley connector settings (owner #8, 2026-09): the
 * SiteSettings `mendeley` section (admin-editable, clientSecret encrypted
 * at rest) wins over the MENDELEY_CLIENT_ID/SECRET env seed. Mirrors
 * modules/zotero/app/src/ZoteroSection.mjs.
 */
export async function getMendeleySettings() {
  try {
    const section = await getSection('mendeley', Settings)
    return {
      enabled: Boolean(section?.enabled ?? true),
      clientId: section?.clientId || Settings.mendeley?.clientID || '',
      clientSecret: section?.clientSecret || '',
      callbackURL: Settings.mendeley?.callbackURL || '/user/mendeley',
    }
  } catch (err) {
    logger.warn({ err }, 'mendeley: site-settings read failed; using env seed')
    return {
      enabled: true,
      clientId: Settings.mendeley?.clientID || '',
      clientSecret: '',
      callbackURL: Settings.mendeley?.callbackURL || '/user/mendeley',
    }
  }
}

export function isConfigured(m) {
  return Boolean(m?.clientId && m?.clientSecret)
}

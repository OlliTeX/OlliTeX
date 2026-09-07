import Settings from '@overleaf/settings'
import MendeleyRouter from './app/src/MendeleyRouter.mjs'

/**
 * Mendeley reference connector (2026-09-07, ported from tpr-webmodule of the
 * pro fork — the Mendeley half of the former Third-Party-References module;
 * the zotero half belongs to modules/zotero).
 *
 * Same shape as the zotero module:
 *  - ALWAYS registered; runtime availability = the MENDELEY_CLIENT_ID /
 *    MENDELEY_CLIENT_SECRET env seeds (a Mendeley OAuth app). When
 *    unconfigured the endpoints still answer GRACEFUL 2xx/4xx
 *    (status { configured:false }), never 5xx.
 *  - per-user tokens: OAuth 2.0 authorization-code flow, encrypted at rest
 *    (AccessTokenEncryptorHelper), transparent refresh.
 */
const { default: MendeleyLinkedFileAgent } = await import(
  './app/src/MendeleyLinkedFileAgent.mjs'
)

const siteUrl = (Settings.siteUrl || 'http://localhost').replace(/\/+$/, '')
Settings.mendeley = {
  clientID: process.env.MENDELEY_CLIENT_ID,
  clientSecret: process.env.MENDELEY_CLIENT_SECRET,
  callbackURL: `${siteUrl}/user/mendeley/oauth/callback`,
}

const MendeleyModule = {
  router: MendeleyRouter,
  linkedFileAgents: {
    mendeley: () => MendeleyLinkedFileAgent,
  },
}

export default MendeleyModule

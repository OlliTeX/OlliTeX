// @ts-check

import logger from '@overleaf/logger'
import UserGetter from '../../Features/User/UserGetter.mjs'
import FeatureUsageRateLimiter from './FeatureUsageRateLimiter.mjs'
import Settings from '@overleaf/settings'
import AnalyticsManager from '../../Features/Analytics/AnalyticsManager.mjs'
import UserAuditLogHandler from '../../Features/User/UserAuditLogHandler.mjs'

class AiFeatureUsageRateLimiter extends FeatureUsageRateLimiter {
  constructor() {
    super('aiFeatureUsage')
  }

  /**
   * When an AI feature is used at or over quota: write an `ai-quota-breach`
   * audit entry (every at-limit request) and emit `ai-usage-limit-reached`
   * once per period.
   *
   * @param {string} userId
   * @param {import('express').Response} res
   * @param {{periodStart?: Date, usage?: number}} featureUsage
   * @param {number} allowance
   * @param {{auditLogTool?: string}} options
   */
  async _onFeatureUsed(userId, res, featureUsage, allowance, options) {
    const usage = featureUsage.usage ?? 0
    if (usage < allowance) {
      return
    }

    if (options.auditLogTool) {
      UserAuditLogHandler.addEntryInBackground(
        userId,
        'ai-quota-breach',
        userId,
        res.req?.ip,
        { tool: options.auditLogTool }
      )
    }

    // Guard against a missing periodStart: without it the marker would change
    // every call and re-fire the event on every request.
    const periodStart = featureUsage.periodStart
    if (
      periodStart &&
      (await this._recordFirstLimitReach(userId, periodStart))
    ) {
      this._recordLimitReachedEvent(
        userId,
        allowance,
        options.auditLogTool
      ).catch(() => {})
    }
  }

  /**
   * Records the `ai-usage-limit-reached` event when the user reaches their AI
   * usage quota.
   *
   * @param {string} userId
   * @param {number} limit
   * @param {string} [tool]
   */
  async _recordLimitReachedEvent(userId, limit, tool) {
    // OlliTeX fork (free-only): plan-code/plan-type analytics removed with the
    // subscription feature; the event is still emitted for usage observability.
    AnalyticsManager.recordEventForUserInBackground(
      userId,
      'ai-usage-limit-reached',
      {
        limit,
        ...(tool ? { feature: tool } : {}),
      }
    )
  }

  /**
   * @param {string} userId
   * @returns {Promise<number>}
   */
  async _getAllowance(userId) {
    const user = await UserGetter.promises.getUser(userId, {
      features: 1,
    })

    // OlliTeX fork (free-only): Writefull premium tiers and subscription
    // feature-merge no longer exist; the allowance tier is the user's stored
    // aiUsageQuota feature (admin-configurable) or the configured free tier.
    const quotaTier = user?.features?.aiUsageQuota ||
      Settings.aiFeatures.freeQuota
    return _quotaTierToAllowance(quotaTier)
  }
}

/**
 * Maps a quota tier identifier to its corresponding numeric allowance
 * using the configured quota grants for AI features.
 *
 * @param {string} quotaTier - The quota tier identifier for the user
 * @returns {number} The numeric allowance for the given tier
 */
function _quotaTierToAllowance(quotaTier) {
  const quota = Settings.quotaGrants.ai[quotaTier]
  if (typeof quota !== 'number') {
    throw new Error(`Quota tier "${quotaTier}" is not initialized in settings`)
  }
  return Math.floor(quota)
}

export default new AiFeatureUsageRateLimiter()

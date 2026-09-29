import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import useAsync from '@/shared/hooks/use-async'
import { getJSON } from '@/infrastructure/fetch-json'
import { debugConsole } from '@/utils/debugging'
import ZoteroLogo from '@/shared/svgs/zotero-logo'
import MaterialIcon from '@/shared/components/material-icon'
import IntegrationCard from '@/features/integrations-panel/integration-card'

/**
 * Owner #8 (2026-09-13 editor wave): the Zotero integrations tile no longer
 * opens a "linked with your account" modal. Instead:
 *
 *   - linked  → a green check on the tile (state is visible at a glance)
 *   - not linked → the tile links to the /hub Zotero user setting
 *     (mysettings.references), where linking/living happens
 *
 * In BOTH cases clicking the tile opens that hub page (target=_blank via
 * IntegrationCard's href mode) — there is nothing to confirm inline.
 */
const ZOTERO_SETTING_URL = '/hub#/mysettings.references'

const ZoteroLinkCard = () => {
  const { t } = useTranslation()

  const {
    runAsync,
    data: isConnected
  } = useAsync<boolean>()

  useEffect(() => {
    runAsync(getJSON('/user/zotero/status')).catch(err => {
      debugConsole.error(
        err?.data?.message || err?.message || err
      )
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const linked = isConnected === true

  return (
    <IntegrationCard
      title={t('zotero')}
      description={
        linked
          ? t('zotero_sync_description', 'Import references from your Zotero library')
          : t('zotero_link_from_mysettings', 'Not linked — connect your account in My settings')
      }
      icon={<ZoteroLogo size={32} />}
      showPaywallBadge={false}
      titleBadge={
        linked ? (
          <span
            className="integrations-panel-card-status"
            aria-label={t('zotero_account_linked_successfully')}
            style={{ color: 'var(--mantine-color-ollitex-6, #2f9e63)' }}
          >
            <MaterialIcon type="check_circle" style={{ fontSize: 18 }} />
          </span>
        ) : (
          <span
            className="integrations-panel-card-status"
            aria-label={t('zotero_link_from_mysettings')}
          >
            <MaterialIcon
              type="link_off"
              unfilled
              style={{ fontSize: 18, color: 'var(--mantine-color-gray-5, #8d96a5)' }}
            />
          </span>
        )
      }
      href={ZOTERO_SETTING_URL}
    />
  )
}

export default ZoteroLinkCard

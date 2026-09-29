import { useTranslation } from 'react-i18next'
import useAsync from '@/shared/hooks/use-async'
import { getJSON } from '@/infrastructure/fetch-json'
import { debugConsole } from '@/utils/debugging'
import MendeleyLogo from '@/shared/svgs/mendeley-logo'
import IntegrationCard from '@/features/integrations-panel/integration-card'

/**
 * Mendeley card in the editor's Integrations panel.
 *  - linked account  → the references settings section (manage/unlink)
 *  - not linked      → the Mendeley OAuth popup (sign in to link)
 *
 * Registered via overleafModuleImports.importProjectFromGithubModalWrapper
 * (the integrations panel card array).
 */
const MendeleyIntegrationCard = () => {
  const { t } = useTranslation()

  const {
    isLoading: isChecking,
    runAsync,
    data: status,
  } = useAsync<{ configured: boolean; connected: boolean }>()

  const handleClick = () => {
    if (status?.connected) {
      window.location.assign('/hub/#mysettings.references')
      return
    }
    runAsync(getJSON('/user/mendeley/status')).catch(err =>
      debugConsole.error(err?.data?.message || err?.message || err)
    )
    window.open(
      '/user/mendeley/oauth?popup=1',
      '_blank',
      'width=600,height=700'
    )
  }

  if (isChecking) {
    return null
  }

  if (!status?.configured) {
    return null
  }

  return (
    <IntegrationCard
      title={t('mendeley')}
      description={t('cite_directly_or_import_references')}
      icon={<MendeleyLogo size={32} />}
      showPaywallBadge={false}
      onClick={handleClick}
    />
  )
}

export default MendeleyIntegrationCard

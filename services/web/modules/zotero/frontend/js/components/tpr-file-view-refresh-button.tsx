import { useTranslation } from 'react-i18next'
import OLButton from '@/shared/components/ol/ol-button'
import type { LinkedFile, LinkedFileData } from '@/features/file-view/types/binary-file'
import { getReferenceProvider } from '../reference-providers'

type TPRFileViewRefreshButtonProps = {
  file: LinkedFile<keyof LinkedFileData>
  refreshFile: (isTPR: boolean | null) => void
  refreshing: boolean
}

/**
 * Zotero-specific refresh button for the file view.
 * Tells the system this is a TPR file so references are re-indexed.
 * Registered via overleafModuleImports.tprFileViewRefreshButton.
 */
export function TPRFileViewRefreshButton({
  file,
  refreshFile,
  refreshing,
}: TPRFileViewRefreshButtonProps) {
  const { t } = useTranslation()
  const refProviderIsProvider = Boolean(getReferenceProvider(file))

  return (
    <OLButton
      variant="primary"
      onClick={() => refreshFile(refProviderIsProvider)}
      isLoading={refreshing}
    >
      {t('refresh')}
    </OLButton>
  )
}

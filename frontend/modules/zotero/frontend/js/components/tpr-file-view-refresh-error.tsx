import { useTranslation } from 'react-i18next'
import OLNotification from '@/shared/components/notification'
import type { LinkedFile, LinkedFileData } from '@/features/file-view/types/binary-file'
import { getReferenceProvider } from '../reference-providers'

/**
 * Zotero-specific error messages when refreshing a linked file fails.
 * Registered via overleafModuleImports.tprFileViewRefreshError.
 */

type TPRFileViewRefreshErrorProps = {
  file: LinkedFile<keyof LinkedFileData>
  refreshError: string
}

export function TPRFileViewRefreshError({
  file,
  refreshError,
}: TPRFileViewRefreshErrorProps) {
  const { t } = useTranslation()

  let message = refreshError

  const provider = getReferenceProvider(file)
  if (provider) {
    if (!refreshError) {
      message = t(provider.i18n.loadingError)
    } else if (refreshError?.includes('not linked')) {
      message = t(provider.i18n.loadingErrorNotLinked)
    } else if (refreshError === 'forbidden' || refreshError?.includes('403')) {
      message = t(provider.i18n.loadingErrorForbidden)
    } else if (refreshError === 'expired' || refreshError?.includes('token expired')) {
      message = t(provider.i18n.loadingErrorExpired)
    }
  }

  return (
    <div className="file-view-error">
      <OLNotification type="error" content={message} />
    </div>
  )
}

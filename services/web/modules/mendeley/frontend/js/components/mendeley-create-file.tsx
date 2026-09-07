import { useTranslation } from 'react-i18next'
import { useEffect, useCallback, useState } from 'react'
import * as eventTracking from '@/infrastructure/event-tracking'
import { getJSON } from '@/infrastructure/fetch-json'
import useAsync from '@/shared/hooks/use-async'
import { debugConsole } from '@/utils/debugging'
import OLButton from '@/shared/components/ol/ol-button'
import OLNotification from '@/shared/components/notification'
import { useFileTreeActionable } from '@/features/file-tree/contexts/file-tree-actionable'
import FileTreeModalCreateFileMode from '@/features/file-tree/components/file-tree-create/file-tree-modal-create-file-mode'
import FileTreeCreateNameProvider from '@/features/file-tree/contexts/file-tree-create-name'
import FileTreeImportFromMendeley from './file-tree-import-from-mendeley'

type MendeleyGroup = {
  id: string
  name: string
}

export function CreateFileMode() {
  const { t } = useTranslation()

  return (
    <FileTreeModalCreateFileMode
      mode="mendeley"
      icon="library_books"
      label={t('from_provider', { provider: 'Mendeley' })}
    />
  )
}

export function CreateFilePane() {
  const { newFileCreateMode } = useFileTreeActionable()
  const { t } = useTranslation()
  const isMendeleyMode = newFileCreateMode === 'mendeley'

  const {
    runAsync,
    data: groups,
    isSuccess: isGroupsLoaded,
    isError: isGroupsError,
    isLoading: isGroupsLoading,
  } = useAsync<MendeleyGroup[]>()

  const loadGroups = useCallback(() => {
    return runAsync(
      getJSON('/mendeley/groups').then((data: { groups?: MendeleyGroup[] }) =>
        data?.groups || null
      )
    ).catch(err => {
      debugConsole.error(err?.data?.message || err?.message || err)
    })
  }, [runAsync])

  useEffect(() => {
    if (!isMendeleyMode) return

    loadGroups()
  }, [loadGroups, isMendeleyMode])

  if (!isMendeleyMode) return null

  if (isGroupsLoading) {
    return (
      <>
        <br/>
        <div role="status" className="loading d-flex justify-content-center align-items-center fs-5">
          <div
            aria-hidden="true"
            className="spinner-border spinner-border-sm"
          ></div>
          {t('loading') + '…'}
        </div>
      </>
    )
  } else if (isGroupsLoaded) {
    if (groups) {
      return (
        <FileTreeCreateNameProvider initialName="mendeley.bib">
          <FileTreeImportFromMendeley groups={groups} />
        </FileTreeCreateNameProvider>
      )
    }
    return (
      <div className="referencesImportModal">
        <p>{t('mendeley_sync_description')}</p>
        <p>
          <OLButton
            variant="primary"
            onClick={() => {
              window.open(
                '/user/mendeley/oauth?popup=1',
                '_blank',
                'width=600,height=700'
              )
            }}
          >
            {t('link_to_mendeley')}
          </OLButton>
        </p>
      </div>
    )
  } else if (isGroupsError) {
    return (
      <OLNotification
        type="error"
        content={t('mendeley_groups_loading_error', {
          provider: t('mendeley'),
        })}
      />
    )
  }
}

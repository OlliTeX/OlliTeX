import { useTranslation } from 'react-i18next'
import getMeta from '@/utils/meta'
import { formatTime, relativeDate } from '@/features/utils/format-date'
import { LinkedFileIcon } from '@/features/file-view/components/file-view-icons'
import { getReferenceProvider } from '../reference-providers'
import type { LinkedFile, LinkedFileData } from '@/features/file-view/types/binary-file'

/**
 * Shows "Imported from Zotero at <date>" in the owner's file view header or
 * "Imported from Zotero at <date> by <owner>" in the collaborator's file view header
 * when viewing a Zotero-linked .bib file.
 * Registered via overleafModuleImports.tprFileViewInfo.
 */

type TPRFileViewInfoProps = {
  file: LinkedFile<keyof LinkedFileData>
}

export function TPRFileViewInfo({ file }: TPRFileViewInfoProps) {
  const { t } = useTranslation()

  const provider = getReferenceProvider(file)
  if (!provider) return null

  const importedAt = (file.linkedFileData as any)?.importedAt || file.created
  const formattedDate = formatTime(importedAt)
  const relative = relativeDate(importedAt)

  const importedByUserId = (file.linkedFileData as any)?.importedByUserId
  const importedByName = (file.linkedFileData as any)?.importedByName || 'Unknown'

  return (
    <p>
      <LinkedFileIcon />
      &nbsp;
      {(importedByUserId === getMeta('ol-user_id')) ? (
        t(provider.i18n.importedAtDate, {
          formattedDate,
          relativeDate: relative,
        })
      ) : (
        t(provider.i18n.importedAtDateBy, {
          formattedDate,
          relativeDate: relative,
          importedByName,
        })
      )}
    </p>
  )
}

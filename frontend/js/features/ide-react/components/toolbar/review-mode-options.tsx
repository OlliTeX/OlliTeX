import { OLDropdownDivider } from '@/shared/components/ol/ol-dropdown-menu'
import { useTranslation } from 'react-i18next'
import { useEditorPropertiesContext } from '../../context/editor-properties-context'
import DropdownMenuItem from '@/shared/components/dropdown/dropdown-menu-item'
import { sendMB } from '@/infrastructure/event-tracking'
import { useIdeReactContext } from '../../context/ide-react-context'
import { usePermissionsContext } from '../../context/permissions-context'
import { useProjectContext } from '@/shared/context/project-context'
import { NestedMenuBarDropdown } from '@/shared/components/menu-bar/menu-bar-dropdown'
import { useUserContext } from '@/shared/context/user-context'

function getMode(permissionsLevel: string, wantTrackChanges: boolean) {
  if (permissionsLevel === 'readOnly') {
    return 'view'
  }
  if (permissionsLevel === 'review') {
    return 'review'
  }
  if (wantTrackChanges) {
    return 'review'
  }
  return 'edit'
}

const ReviewModeOptions: React.FC = () => {
  const { t } = useTranslation()
  const { wantTrackChanges } = useEditorPropertiesContext()
  const { write, trackedWrite } = usePermissionsContext()
  const { permissionsLevel } = useIdeReactContext()
  const { features } = useProjectContext()
  const user = useUserContext()

  const mode = getMode(permissionsLevel, wantTrackChanges)
  const showViewOption = mode === 'view'

  if (!features.trackChangesVisible) {
    return null
  }

  return (
    <>
      <OLDropdownDivider />
      <NestedMenuBarDropdown id="editing-mode-group" title={t('editing_mode')}>
        <DropdownMenuItem
          as="button"
          disabled={!write || !user.id}
          onClick={() => {
            if (mode === 'edit') {
              return
            }
            sendMB('editing-mode-change', {
              role: permissionsLevel,
              previousMode: mode,
              newMode: 'edit',
            })
            window.dispatchEvent(new Event('toggle-track-changes'))
          }}
          leadingIcon="edit"
          active={write && mode === 'edit'}
        >
          {t('editing')}
        </DropdownMenuItem>
        <DropdownMenuItem
          as="button"
          disabled={permissionsLevel === 'readOnly' || !user.id}
          onClick={() => {
            if (mode === 'review') {
              return
            }
            // SaaS sweep (2026-09-16, owner): OlliTeX is fully open source and
            // has no paid tier — track changes is free, no upgrade modal.
            sendMB('editing-mode-change', {
              role: permissionsLevel,
              previousMode: mode,
              newMode: mode,
            })
            window.dispatchEvent(new Event('toggle-track-changes'))
          }}
          leadingIcon="rate_review"
          active={trackedWrite && mode === 'review'}
        >
          {t('reviewing')}
        </DropdownMenuItem>
        {showViewOption && (
          <DropdownMenuItem
            as="button"
            leadingIcon="visibility"
            active={mode === 'view'}
          >
            {t('viewing')}
          </DropdownMenuItem>
        )}
      </NestedMenuBarDropdown>
    </>
  )
}

export default ReviewModeOptions

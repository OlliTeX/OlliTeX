import AddCollaborators from './add-collaborators'
import OLRow from '@/shared/components/ol/ol-row'
import classnames from 'classnames'
import { useFeatureFlag } from '@/shared/context/split-test-context'

// OlliTeX fork (free-only): SaaS collaborator-limit upsell components
// (AddCollaboratorsUpgrade, CollaboratorsLimitUpgrade, AccessLevelsChanged)
// were removed — this fork has unlimited collaborators. The SaaS props from
// the caller are intentionally accepted and ignored for API compatibility.
export default function SendInvites({
  canAddCollaborators,
}: {
  canAddCollaborators: boolean
  hasExceededCollaboratorLimit?: boolean
  haveAnyEditorsBeenDowngraded?: boolean
  somePendingEditorsResolved?: boolean
}) {
  const isSharingUpdatesEnabled = useFeatureFlag('sharing-updates')

  return (
    <OLRow
      className={classnames('invite-controls', {
        'pb-3': isSharingUpdatesEnabled,
      })}
    >
      <AddCollaborators readOnly={!canAddCollaborators} />
    </OLRow>
  )
}

import DropdownListItem from '@/shared/components/dropdown/dropdown-list-item'
import { OLDropdownItem } from '@/shared/components/ol/ol-dropdown-menu'
import { useEditorAnalytics } from '@/shared/hooks/use-editor-analytics'
import { useNestableDropdown } from '@/shared/hooks/use-nestable-dropdown'
import { MouseEventHandler, ReactNode, useCallback } from 'react'

type MenuBarOptionProps = {
  title: string
  onClick?: MouseEventHandler
  disabled?: boolean
  leadingIcon?: ReactNode
  trailingIcon?: ReactNode
  href?: string
  target?: string
  rel?: string
  eventKey?: string
}

export const MenuBarOption = ({
  title,
  onClick: clickHandler,
  href,
  disabled,
  leadingIcon,
  trailingIcon,
  target,
  rel,
  eventKey,
}: MenuBarOptionProps) => {
  const { setSelected, inline } = useNestableDropdown()
  const { sendEvent } = useEditorAnalytics()
  const onClick: MouseEventHandler = useCallback(
    e => {
      if (eventKey) {
        sendEvent('menu-bar-option-click', { key: eventKey })
      }
      return clickHandler?.(e)
    },
    [clickHandler, eventKey, sendEvent]
  )
  return (
    <DropdownListItem>
      <OLDropdownItem
        // 2026-10-09 (owner round-3): in the v2 rail's INLINE groups the
        // owner wants them kept unfolded — hovering a sibling row must not
        // collapse the open group (legacy flyout keeps close-on-hover).
        onMouseEnter={() => {
          if (inline) {
            return
          }
          setSelected(null)
        }}
        onClick={onClick}
        disabled={disabled}
        leadingIcon={leadingIcon}
        trailingIcon={trailingIcon}
        href={href}
        rel={rel}
        target={target}
      >
        {title}
      </OLDropdownItem>
    </DropdownListItem>
  )
}

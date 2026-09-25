import {
  PanelResizeHandle,
  PanelResizeHandleProps,
} from 'react-resizable-panels'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'
import classNames from 'classnames'
import { useStripResizeHandleInteractive } from './strip-resize-handle-a11y'

export function VerticalResizeHandle(props: PanelResizeHandleProps) {
  const { t } = useTranslation()
  const innerRef = useRef<HTMLDivElement | null>(null)
  // a11y (P8): same treatment as the horizontal handle — the library's
  // interactive marker conflicts with its aria-value attributes once the
  // role is gone (aria-allowed-attr); keep the node plain.
  useStripResizeHandleInteractive(innerRef)

  return (
    <PanelResizeHandle {...props}>
      <div
        ref={innerRef}
        className={classNames('vertical-resize-handle', {
          'vertical-resize-handle-enabled': !props.disabled,
        })}
        title={t('resize')}
      />
    </PanelResizeHandle>
  )
}

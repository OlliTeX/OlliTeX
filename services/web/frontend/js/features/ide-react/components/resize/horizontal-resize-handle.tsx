import {
  PanelResizeHandle,
  PanelResizeHandleProps,
} from 'react-resizable-panels'
import { FC, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import classNames from 'classnames'
import { useStripResizeHandleInteractive } from './strip-resize-handle-a11y'

type HorizontalResizeHandleOwnProps = {
  resizable?: boolean
  onDoubleClick?: () => void
}

export const HorizontalResizeHandle: FC<
  React.PropsWithChildren<
    HorizontalResizeHandleOwnProps & PanelResizeHandleProps
  >
> = ({ children, resizable = true, onDoubleClick, ...props }) => {
  const { t } = useTranslation()
  const [isDragging, setIsDragging] = useState(false)
  const innerRef = useRef<HTMLDivElement | null>(null)
  // a11y (P8): the panel library marks its handle as interactive (role +
  // tabindex) and applies those AFTER our props — strip them so the
  // keyboard-reachable children (toggler/synctex) are not nested-interactive.
  useStripResizeHandleInteractive(innerRef)

  function handleDragging(isDraggingParam: boolean) {
    if (isDragging || resizable) {
      setIsDragging(isDraggingParam)
    }
  }

  // Only call onDragging prop when the pointer moves after starting a drag
  useEffect(() => {
    if (isDragging) {
      const handlePointerMove = () => {
        props.onDragging?.(true)
      }

      document.addEventListener('pointermove', handlePointerMove)
      return () => {
        document.removeEventListener('pointermove', handlePointerMove)
      }
    } else {
      props.onDragging?.(false)
    }
  }, [isDragging, props])

  return (
    <PanelResizeHandle
      disabled={!resizable && !isDragging}
      {...props}
      onDragging={handleDragging}
    >
      <div
        ref={innerRef}
        className={classNames('horizontal-resize-handle', {
          'horizontal-resize-handle-enabled': resizable,
        })}
        title={t('resize')}
        onDoubleClick={() => onDoubleClick?.()}
      >
        {children}
      </div>
    </PanelResizeHandle>
  )
}

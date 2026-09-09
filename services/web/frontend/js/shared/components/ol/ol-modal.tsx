import FocusTrap from '../focus-trap'
import {
  Modal,
  ModalProps,
  ModalHeaderProps,
  ModalTitleProps,
  ModalFooterProps,
} from 'react-bootstrap'
import { ModalBodyProps } from 'react-bootstrap/ModalBody'
import type { Options as FocusTrapOptions } from 'focus-trap'
import { Modal as MantineModal } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import classNames from 'classnames'
import {
  canUseMantineSurface,
  MantineSurfaceGate,
  useEditorUiVariant,
} from '@/features/editor-v2/variant'

type OLModalProps = ModalProps & {
  size?: 'sm' | 'lg'
  onHide: () => void
  show?: boolean
  themed?: boolean
  className?: string
} & Pick<
    FocusTrapOptions,
    | 'escapeDeactivates'
    | 'clickOutsideDeactivates'
    | 'returnFocusOnDeactivate'
    | 'initialFocus'
  >

type OLModalHeaderProps = ModalHeaderProps & {
  closeButton?: boolean
}

export function OLModal({
  children,
  show = false,
  onHide,
  returnFocusOnDeactivate = true, // Return focus to trigger element when modal closes
  escapeDeactivates = false, // Let React-Bootstrap Modal handle Escape key to avoid double Escape key handling
  clickOutsideDeactivates = true, // Allow focus trap to deactivate on outside click and let React-Bootstrap Modal handle it
  initialFocus,
  themed = false,
  className,
  backdropClassName,
  size,
  ...props
}: OLModalProps) {
  // P3 editor renovation: on the /editor route (once the P1 shell gate is
  // ready) the modal surface is the Mantine Modal frame — same API surface
  // (show/onHide/size), same children, Mantine's built-in focus trap and
  // ESC/outside-click close. /project renders the legacy react-bootstrap
  // modal with its focus-trap wiring verbatim. This component's identity is
  // stable — only the framed output swaps, per-surface (the IDE tree is
  // never remounted).
  const ctx = useEditorUiVariant()
  if (canUseMantineSurface(ctx)) {
    return (
      <MantineSurfaceGate>
        <MantineModal
          opened={show}
          onClose={onHide}
          size={size === 'sm' ? 'sm' : 'lg'}
          withinPortal
          trapFocus
          position="center"
          className={classNames('ol-mant-modal', { 'modal-themed': themed }, className)}
        >
          {children}
        </MantineModal>
      </MantineSurfaceGate>
    )
  }
  return (
    <Modal
      show={show}
      onHide={onHide}
      className={classNames({ 'modal-themed': themed }, className)}
      backdropClassName={classNames(
        { 'modal-backdrop-themed': themed },
        backdropClassName
      )}
      {...props}
    >
      <FocusTrap
        active={show}
        focusTrapOptions={{
          escapeDeactivates,
          clickOutsideDeactivates,
          returnFocusOnDeactivate,
          initialFocus,
        }}
      >
        {children}
      </FocusTrap>
    </Modal>
  )
}

export function OLModalHeader({
  children,
  closeButton = true,
  ...props
}: OLModalHeaderProps) {
  const { t } = useTranslation()
  return (
    <Modal.Header
      closeButton={closeButton}
      closeLabel={t('close_dialog')}
      {...props}
    >
      {children}
    </Modal.Header>
  )
}

export function OLModalTitle({ children, ...props }: ModalTitleProps) {
  return (
    <Modal.Title as="h2" {...props}>
      {children}
    </Modal.Title>
  )
}

export function OLModalBody({ children, ...props }: ModalBodyProps) {
  return <Modal.Body {...props}>{children}</Modal.Body>
}

export function OLModalFooter({ children, ...props }: ModalFooterProps) {
  return <Modal.Footer {...props}>{children}</Modal.Footer>
}

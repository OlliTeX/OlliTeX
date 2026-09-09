/**
 * editor-v2 — module Mantine wave (MODULE_MANTINE_PLAN.md), M1+.
 *
 * Shared surface components for module UI code (zotero/mendeley/webdav/…):
 * on the /editor variant they render Mantine (Button / Alert) inside the
 * editor's OlliT provider; on /Project they render the legacy OL
 * equivalents verbatim. Modules call `useMantineSurface()` and drop
 * `<S.Btn …>` where an `<OLButton …>` used to be — one import, one gate,
 * stable behaviour on both routes until /Project retires.
 */
import type { ReactElement, ReactNode } from 'react'
import { Alert, Button } from '@mantine/core'
import OLButton from '@/shared/components/ol/ol-button'
import OLNotification from '@/shared/components/notification'
import {
  canUseMantineSurface,
  useEditorUiVariant,
} from './variant'

export type SurfaceBtnVariant = 'primary' | 'secondary' | 'danger' | 'danger-ghost'

export interface SurfaceBtnProps {
  children: ReactNode
  variant?: SurfaceBtnVariant
  onClick?: () => void
  disabled?: boolean
  size?: 'xs' | 'sm' | 'md' | 'lg'
  // anchor-style (href/target): rendered as the legacy anchor button on
  // BOTH routes (link semantics, e.g. the zotero oauth popup link) — the
  // converted action surface is for real actions only.
  href?: string
  target?: string
}

export interface MantineSurface {
  /** The /editor Mantine variant is active right now. */
  mantine: boolean
  /** OLButton ↔ Mantine Button, gated (href buttons stay legacy). */
  Btn: (props: SurfaceBtnProps) => ReactElement
  /** OLNotification(warning) ↔ Mantine Alert, gated. */
  Warn: (props: { children: ReactNode }) => ReactElement
  /** OLNotification(error) ↔ Mantine Alert, gated. */
  Error: (props: { children: ReactNode }) => ReactElement
}

export function useMantineSurface(): MantineSurface {
  const ctx = useEditorUiVariant()
  const mantine = canUseMantineSurface(ctx)

  const Btn = ({
    children,
    variant = 'secondary',
    onClick,
    disabled,
    size = 'xs',
    href,
    target,
    loading,
    loadingLabel,
    leftIcon,
  }: SurfaceBtnProps): ReactElement =>
    mantine && !href ? (
      <ctx.Provider>
        <Button
          size={size}
          disabled={disabled}
          onClick={onClick}
          loading={loading}
          loaderProps={loadingLabel ? { label: loadingLabel } : undefined}
          leftSection={leftIcon}
          color={
            variant === 'danger' || variant === 'danger-ghost' ? 'red' : undefined
          }
          variant={
            variant === 'secondary'
              ? 'light'
              : variant === 'danger-ghost'
                ? 'subtle'
                : 'fill'
          }
        >
          {children}
        </Button>
      </ctx.Provider>
    ) : (
      <OLButton
        variant={variant}
        onClick={onClick}
        disabled={disabled}
        href={href}
        target={target}
        isLoading={loading}
        loadingLabel={loadingLabel}
        leadingIcon={leftIcon}
      >
        {children}
      </OLButton>
    )

  const Warn = ({ children }: { children: ReactNode }): ReactElement =>
    mantine ? (
      <ctx.Provider>
        <Alert color="yellow" variant="light">
          {children}
        </Alert>
      </ctx.Provider>
    ) : (
      <OLNotification type="warning" content={children as string} />
    )

  const ErrorNotif = ({ children }: { children: ReactNode }): ReactElement =>
    mantine ? (
      <ctx.Provider>
        <Alert color="red" variant="light">
          {children}
        </Alert>
      </ctx.Provider>
    ) : (
      <OLNotification type="error" content={children as string} />
    )

  return { mantine, Btn, Warn, Error: ErrorNotif }
}

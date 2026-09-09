/**
 * editor-v2 P2 — the stable toolbar slot (the ONLY edit to main-layout.tsx
 * for the rail is INSIDE rail.tsx itself — see the P2 note there).
 *
 * Stability contract (same rule that saved P1 — the IDE tree must never be
 * remounted): ChromeToolbar has module-level constant identity; /Project
 * renders the literal legacy Toolbar (byte-identical behavior); /editor
 * renders the Mantine toolbar frame once the shell is ready, and the
 * legacy toolbar until it is (and if it ever fails — degradation by
 * design). The swap only ever happens inside the chrome subtree — never
 * around IdeRoot.
 *
 * The rail is deliberately NOT swapped here: its pane is a member of a
 * react-resizable-panels PanelGroup whose layout assumes the legacy DOM
 * skeleton (TabContainer > nav > panel). Swapping the whole rail subtree
 * collapsed the pane (width 0). Instead, rail.tsx renders the Mantine
 * icon list in place (MantineRailNavChrome) behind the same runtime gate.
 */
import { Toolbar } from '@/features/ide-react/components/toolbar/toolbar'
import { useEditorUiVariant, MantineSurfaceGate } from '../variant'
import { MantineToolbar } from './mantine-toolbar'

function isV2Chrome(ctx: ReturnType<typeof useEditorUiVariant>): boolean {
  return ctx.variant === 'mantine' && ctx.shellReady
}

export const ChromeToolbar = () => {
  const ctx = useEditorUiVariant()
  if (isV2Chrome(ctx)) {
    return (
      <MantineSurfaceGate>
        <MantineToolbar />
      </MantineSurfaceGate>
    )
  }
  return <Toolbar />
}

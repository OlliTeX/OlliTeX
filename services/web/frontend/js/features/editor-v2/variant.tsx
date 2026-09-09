/**
 * editor-v2 — UI variant runtime (EDITOR_RENOVATION_PLAN.md §2, P1).
 *
 * The renovated (Mantine) editor lives under /editor/:ProjectId; the legacy
 * /Project/:ProjectId stays untouched as the permanent fallback. The variant
 * is a RUNTIME decision made from the URL (no server flag).
 *
 * HARD RULE (learned the hard way — see the P1 #130 incident):
 *   The IDE tree mounts exactly ONCE and is NEVER remounted. Swapping the
 *   identity of a component that wraps IdeRoot (e.g. NOOP-skeleton →
 *   Mantine-Provider) forces a full remount of the whole editor, which
 *   crashes it (React #130) because the editor's singletons (CM6, sockets,
 *   module registries) survive only one mount. Therefore:
 *     1. EditorUiShell renders children through a STABLE passthrough whose
 *        identity never changes after module load.
 *     2. The Mantine provider is applied per-surface via MantineSurfaceGate
 *        (P2+), whose OWN identity is stable — only a Mantine surface's
 *        subtree appears when the shell is ready; IdeRoot never moves.
 *     3. If the shell chunk FAILS to load, `shellReady` stays false and all
 *        surfaces render their legacy implementation — the editor never
 *        fails to open because of the renovation.
 */
import React, {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react'

export type EditorUiVariant = 'legacy' | 'mantine'

/** Pure path rule (unit-tested): /editor/:ProjectId is the renovated editor. */
export function variantForPathname(pathname: string): EditorUiVariant {
  return pathname === '/editor' || pathname.startsWith('/editor/')
    ? 'mantine'
    : 'legacy'
}

export function detectEditorUiVariant(): EditorUiVariant {
  if (typeof window === 'undefined') return 'legacy'
  return variantForPathname(window.location.pathname)
}

export type EditorShellProviderProps = { children: React.ReactNode }

export type EditorUiContextValue = {
  variant: EditorUiVariant
  // True once the shell chunk (Mantine provider + CSS) resolved on this page.
  shellReady: boolean
  // The shell's provider component, available in the SAME commit as
  // shellReady. Surfaces render Mantine variants only when this is set.
  // (Exported for tests that override the context.)
  Provider: React.ComponentType<EditorShellProviderProps> | null
}

export const EditorUiContext = createContext<EditorUiContextValue>({
  variant: 'legacy',
  shellReady: false,
  Provider: null,
})

export function useEditorUiVariant(): EditorUiContextValue {
  return useContext(EditorUiContext)
}

/** True when the surface should render its Mantine variant right now. */
export function canUseMantineSurface(ctx: EditorUiContextValue): boolean {
  return ctx.variant === 'mantine' && ctx.shellReady && ctx.Provider != null
}

const STABLE_SHELL: React.ComponentType<EditorShellProviderProps> = ({
  children,
}) => <>{children}</>

/**
 * Mount wrapper for the editor app (pages/ide.tsx). Renders children through
 * STABLE_SHELL — a constant component identity — so the IDE tree is NEVER
 * remounted by the renovation, on either route.
 */
export function EditorUiShell({ children }: { children: React.ReactNode }) {
  const [variant] = useState<EditorUiVariant>(detectEditorUiVariant)
  const [shellReady, setShellReady] = useState(false)
  const [Provider, setProvider] = useState<
    React.ComponentType<EditorShellProviderProps> | null
  >(null)

  useEffect(() => {
    if (variant !== 'mantine') return undefined
    let cancelled = false
    import('./mantine-shell')
      .then(mod => {
        if (cancelled) return
        // Named first, default as fallback (see mantine-shell.tsx export note).
        const ShellComponent =
          (mod as any).EditorMantineShell ?? (mod as any).default
        // Apply the marker BEFORE React re-renders, so the scoped
        // token-bridge CSS (`.ol-editor-mantine ...`) applies on the first
        // Mantine paint.
        const root = document.getElementById('ide-root')
        if (root) {
          root.classList.add('ol-editor-mantine')
          root.dataset.olEditorVariant = 'mantine'
        }
        // Same commit as shellReady=true — consumers see both together.
        setProvider(() => ShellComponent)
        setShellReady(true)
      })
      .catch(() => {
        // shell chunk failed: keep shellReady=false → every surface renders
        // its legacy implementation (degradation path, by design).
        if (!cancelled) setShellReady(false)
      })
    return () => {
      cancelled = true
    }
  }, [variant])

  const value = useMemo(
    () => ({ variant, shellReady, Provider }),
    [variant, shellReady, Provider]
  )

  // STABLE_SHELL identity is a module-level constant — the IDE tree under it
  // is never remounted (see HARD RULE above).
  return (
    <EditorUiContext.Provider value={value}>
      <STABLE_SHELL>{children}</STABLE_SHELL>
    </EditorUiContext.Provider>
  )
}

/**
 * Per-surface Mantine gate for P2+ dispatchers:
 *   - when the context says "mantine + shell ready", wraps children with the
 *     shell's Mantine provider (Mantine components need it in context);
 *   - otherwise passes children through untouched.
 * The gate's OWN identity is stable, so using it never remounts anything —
 * only this surface's Mantine subtree appears when ready.
 */
export function MantineSurfaceGate({
  children,
}: {
  children: React.ReactNode
}) {
  const { variant, shellReady, Provider } = useEditorUiVariant()
  if (variant !== 'mantine' || !shellReady || Provider == null) {
    return <>{children}</>
  }
  return <Provider>{children}</Provider>
}

// default export too (ESM tooling convenience); webpack consumers use the
// NAME — see the export note in mantine-shell.tsx.
export default EditorUiShell

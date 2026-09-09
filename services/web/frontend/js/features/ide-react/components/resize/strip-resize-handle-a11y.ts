import { useEffect, type RefObject } from 'react'

/**
 * a11y (P8): react-resizable-panels renders every PanelResizeHandle as an
 * INTERACTIVE node — `role="separator"` + `tabindex` — applied AFTER our
 * props (they cannot be overridden from the component API). The PDF handle
 * carries interactive children on top of it (panel toggler, synctex
 * controls), which makes the pair a nested-interactive violation.
 *
 * The children are the real keyboard-reachable controls; the drag handle
 * itself is a mouse affordance. This observer strips the interactive
 * attributes from the nearest `[data-resize-handle]` element and keeps
 * them stripped against library re-renders. The `title` (native resize
 * hint) stays intact.
 */
export function useStripResizeHandleInteractive(
  ref: RefObject<HTMLElement | null>
) {
  useEffect(() => {
    const inner = ref.current
    if (!inner) return
    const handleEl = inner.closest('[data-resize-handle]') as HTMLElement | null
    if (!handleEl) return

    const strip = () => {
      handleEl.removeAttribute('role')
      handleEl.removeAttribute('tabindex')
      // the library also paints aria-* attributes that are only legal for
      // the role we removed (aria-allowed-attr) — drop them together
      for (const attr of Array.from(handleEl.attributes)) {
        if (attr.name.startsWith('aria-')) {
          handleEl.removeAttribute(attr.name)
        }
      }
    }
    strip()

    // safety net: the panel library re-applies the interactive attributes
    // on its own re-renders (it writes them AFTER our props and on layout
    // effects) — a low-frequency sweep keeps the node plain without any
    // perceptible cost (one element).
    const interval = window.setInterval(strip, 250)

    const observer = new MutationObserver(mutationRecords => {
      for (const record of mutationRecords) {
        if (
          record.type === 'attributes' &&
          (record.attributeName === 'role' ||
            record.attributeName === 'tabindex') &&
          (handleEl.hasAttribute('role') ||
            handleEl.hasAttribute('tabindex'))
        ) {
          strip()
        }
      }
    })
    observer.observe(handleEl, { attributes: true })
    return () => {
      window.clearInterval(interval)
      observer.disconnect()
    }
  }, [ref])
}

import { useCallback, useEffect, useRef, useState } from 'react'

// React 19 removed ReactDOM.findDOMNode from react-dom (the build warns: "export
// 'findDOMNode' was not found in 'react-dom'" — it is undefined at runtime
// under React 19.2). react-bootstrap v0.x still passes the *component instance*
// to a callback ref, so we resolve its backing DOM node without findDOMNode:
// DOM nodes resolve directly; component instances resolve through the pinned
// runtime's internal fiber (stable in the React 19.x we pin, and the only way
// to reach the host node of a library class component without findDOMNode).
// If no node can be resolved we return null and gracefully degrade: the
// click-outside listener simply cannot test containment, so the dropdown
// still toggles and closes via its own rootClose/onHide paths.
function resolveNode(value: unknown): HTMLElement | null {
  if (typeof document !== 'undefined' && value instanceof HTMLElement) {
    return value
  }
  const inst = value as any
  if (!inst || typeof inst !== 'object') return null
  let fiber: any = inst._reactInternals ?? inst._reactInternalFiber ?? null
  while (fiber) {
    const stateNode = fiber.stateNode
    if (stateNode && stateNode.nodeType === 1) return stateNode as HTMLElement
    fiber = fiber.return
  }
  return null
}

export default function useDropdown(defaultOpen = false) {
  const [open, setOpen] = useState(defaultOpen)

  // store the dropdown's DOM node for the "click outside" event listener
  const ref = useRef<HTMLElement | null>(null)

  // react-bootstrap v0.x passes `component` instead of `node` to the ref callback
  const handleRef = useCallback((component: any) => {
    ref.current = resolveNode(component)
  }, [])

  // prevent a click on the dropdown toggle propagating to the original handler
  const handleClick = useCallback((event: any) => {
    event.stopPropagation()
  }, [])

  // handle dropdown toggle
  const handleToggle = useCallback((value: any) => {
    setOpen(Boolean(value))
  }, [])

  // close the dropdown on click outside the dropdown
  const handleDocumentClick = useCallback(
    (event: any) => {
      if (ref.current && !ref.current.contains(event.target)) {
        setOpen(false)
      }
    },
    [ref]
  )

  // add/remove listener for click anywhere in document
  useEffect(() => {
    if (open) {
      document.addEventListener('mousedown', handleDocumentClick)
    }

    return () => {
      document.removeEventListener('mousedown', handleDocumentClick)
    }
  }, [open, handleDocumentClick])

  // return props for the Dropdown component
  return { ref: handleRef, onClick: handleClick, onToggle: handleToggle, open }
}

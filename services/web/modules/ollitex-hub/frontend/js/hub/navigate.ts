// overleaf-lab (Wave A): cross-section hub navigation.
//
// Sections rendered by leaves.tsx cannot receive a bound onSelect callback;
// a same-origin window event keeps the plumbing out of the leaf registry
// (used e.g. by the Overview "Instance management" shortcuts, #2).
const HUB_NAVIGATE_EVENT = 'ol-hub-navigate'

export function hubNavigate(id: string): void {
  if (typeof id !== 'string' || !id) return
  try {
    window.dispatchEvent(new CustomEvent(HUB_NAVIGATE_EVENT, { detail: id }))
  } catch {
    // tests without CustomEvent
  }
}

export function onHubNavigate(fn: (id: string) => void): () => void {
  const handler = (e: Event) => {
    const id = (e as CustomEvent).detail
    if (typeof id === 'string' && id) fn(id)
  }
  try {
    window.addEventListener(HUB_NAVIGATE_EVENT, handler)
  } catch {
    // jsdom
  }
  return () => {
    try {
      window.removeEventListener(HUB_NAVIGATE_EVENT, handler)
    } catch {
      // jsdom
    }
  }
}

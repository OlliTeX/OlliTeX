import { Ranges } from '@/features/review-panel/context/ranges-context'
import { Threads } from '@/features/review-panel/context/threads-context'

export const hasActiveRange = (
  ranges: Ranges | undefined,
  threads: Threads | undefined
): boolean | undefined => {
  // 2026-10-07 (AG-2 flake): tracked changes need NO threads data —
  // a change present in ranges is an active review item even while the
  // threads context is still loading. Only the COMMENT branch requires
  // threads (a comment is "active" while its thread is unresolved).
  if (!ranges) {
    // ranges data isn't loaded yet
    return undefined
  }

  if (ranges.changes.length > 0) {
    // at least one tracked change
    return true
  }

  if (!threads) {
    // comments can't be judged yet — data isn't fully loaded
    return undefined
  }

  for (const comment of ranges.comments) {
    const thread = threads[comment.op.t]
    if (thread && !thread.resolved) {
      return true
    }
  }

  return false
}

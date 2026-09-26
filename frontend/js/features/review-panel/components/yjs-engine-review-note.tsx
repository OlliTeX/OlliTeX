import classNames from 'classnames'

// D25 (S4 flip): comments + tracked changes were OT-document-native
// (HistoryOTShareDoc ranges/accept-reject) and do NOT survive the
// collaboration hard cut to Yjs. This is the honest placeholder that
// replaces the review panel mount: the capability is absent on this
// engine, not hidden. The review-panel feature stays in git and is
// re-attachable once a Y.Doc-native model exists (owner decision).
export function YjsEngineReviewNote() {
  return (
    <div
      className={classNames(
        'yjs-engine-review-note',
        'fixed bottom-0 left-0 right-0 z-10',
        'flex items-center justify-center gap-2',
        'border-t bg-gray-50 text-xs text-gray-500 dark:bg-gray-800 dark:text-gray-400',
        'px-4 py-1.5'
      )}
      role="status"
    >
      <span aria-hidden="true" style={{ opacity: 0.6 }}>
        ⓘ
      </span>
      <span>
        Tracked changes and comments are not available on the Yjs
        collaboration engine for this document.
      </span>
    </div>
  )
}

export default YjsEngineReviewNote

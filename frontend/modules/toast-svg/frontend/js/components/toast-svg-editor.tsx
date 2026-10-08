/**
 * AD (owner 2026-10-07): the SVG EDITOR, mirroring the toast-image
 * two-stage approach:
 *
 *   Stage 1 — the file view (content preview + header Download button,
 *             which the file view already provides) plus THIS module's
 *             "Edit SVG" header button (fileViewButtons, next to Download).
 *   Stage 2 — pressing "Edit SVG" opens this FULL-SIZE modal: a source
 *             editor (left) with a live render preview (right), Save /
 *             Close. Same dirty-guard + replace-by-reupload save flow as
 *             the image editor (proven endpoints, same CSRF handling).
 */
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useProjectContext } from '@/shared/context/project-context'
import { useFileTreeData } from '@/shared/context/file-tree-data-context'
import { useFileTreeOpenContext } from '@/features/ide-react/context/file-tree-open-context'
import { findInTree } from '@/features/file-tree/util/find-in-tree'
import OLButton from '@/shared/components/ol/ol-button'
import MaterialIcon from '@/shared/components/material-icon'
import LoadingSpinner from '@/shared/components/loading-spinner'
import OLNotification from '@/shared/components/notification'
import {
  OLModal,
  OLModalBody,
  OLModalFooter,
  OLModalHeader,
  OLModalTitle,
} from '@/shared/components/ol/ol-modal'
import getMeta from '@/utils/meta'

type EditorFile = {
  _id: string
  name: string
  hash: string
}

function fileExtension(name: string): string | null {
  const idx = name.lastIndexOf('.')
  if (idx <= 0 || idx === name.length - 1) return null
  return name.slice(idx + 1).toLowerCase()
}

function isSvgFile(name: string): boolean {
  return fileExtension(name) === 'svg'
}

type TreeNode = {
  _id?: string
  name?: string
  docs?: TreeNode[]
  fileRefs?: TreeNode[]
  folders?: TreeNode[]
  [key: string]: unknown
}

/** Recursively find the parent folder ID for a given entity (image flow). */
function findParentFolderId(node: TreeNode, entityId: string): string | null {
  if (node.docs?.some(d => d._id === entityId)) return node._id ?? null
  if (node.fileRefs?.some(f => f._id === entityId)) return node._id ?? null
  for (const sub of node.folders ?? []) {
    if (sub._id === entityId) return node._id ?? null
    const found = findParentFolderId(sub, entityId)
    if (found) return found
  }
  return null
}

// ------------------------------------------------------------------------
// Stage 1 — the file-view header button (next to Download).
// ------------------------------------------------------------------------
export default function ToastSvgFileViewButton({ file }: { file: EditorFile }) {
  if (!isSvgFile(file.name)) return null
  const { t } = useTranslation()
  const [showEditor, setShowEditor] = useState(false)
  return (
    <>
      <div style={{ display: 'inline-block', marginLeft: '8px' }}>
        <OLButton variant="secondary" onClick={() => setShowEditor(true)}>
          <MaterialIcon type="edit" className="align-middle" />{' '}
          <span>{t('edit_svg', 'Edit SVG')}</span>
        </OLButton>
      </div>
      {showEditor && (
        <SvgEditorModal file={file} onClose={() => setShowEditor(false)} />
      )}
    </>
  )
}

// ------------------------------------------------------------------------
// Stage 2 — the full-size editor modal (source + live preview + Save).
// ------------------------------------------------------------------------
function SvgEditorModal({
  file,
  onClose,
}: {
  file: EditorFile
  onClose: () => void
}) {
  const { t } = useTranslation()
  const { projectId } = useProjectContext()
  const { fileTreeData } = useFileTreeData()
  const { handleFileTreeSelect } = useFileTreeOpenContext()

  const [text, setText] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [failed, setFailed] = useState(false)
  const [confirmDiscard, setConfirmDiscard] = useState(false)

  const initialText = useRef<string | null>(null)
  const editorElRef = useRef<HTMLTextAreaElement | null>(null)

  // Load the SVG source.
  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const res = await fetch(
          `/project/${projectId}/blob/${file.hash}`,
          { credentials: 'same-origin' }
        )
        if (!res.ok) throw new Error(`status ${res.status}`)
        const body = await res.text()
        if (cancelled) return
        initialText.current = body
        setText(body)
      } catch (err: unknown) {
        if (cancelled) return
        setFailed(true)
        setError((err as Error)?.message || t('svg_edit_load_failed'))
      }
    })()
    return () => {
      cancelled = true
    }
  }, [projectId, file.hash, t])

  // Live preview (debounced; object URL revocation kept honest).
  const [previewUrl, setPreviewUrl] = useState<string | null>(null)
  useEffect(() => {
    if (text == null) return
    const timer = window.setTimeout(() => {
      const url = URL.createObjectURL(
        new Blob([text], { type: 'image/svg+xml' })
      )
      setPreviewUrl(prev => {
        if (prev) URL.revokeObjectURL(prev)
        return url
      })
    }, 300)
    return () => window.clearTimeout(timer)
  }, [text])
  useEffect(
    () => () => {
      setPreviewUrl(prev => {
        if (prev) URL.revokeObjectURL(prev)
        return null
      })
    },
    []
  )

  const dirty = text != null && text !== initialText.current

  const requestClose = () => {
    if (dirty && !saving) {
      setConfirmDiscard(true)
      return
    }
    onClose()
  }

  const handleSave = useCallback(async () => {
    if (text == null || saving) return
    setSaving(true)
    try {
      const csrfToken = getMeta('ol-csrfToken') ?? ''
      const folderId =
        (fileTreeData?.root &&
          findParentFolderId(fileTreeData.root as TreeNode, file._id)) ||
        ''
      const formData = new FormData()
      formData.append('qqfile', new Blob([text], { type: 'image/svg+xml' }), file.name)
      formData.append('name', file.name)

      const uploadRes = await fetch(
        `/project/${projectId}/upload?folder_id=${encodeURIComponent(folderId || '')}`,
        {
          method: 'POST',
          headers: { 'X-CSRF-TOKEN': csrfToken },
          body: formData,
        }
      )
      if (!uploadRes.ok) {
        throw new Error(t('svg_edit_save_failed', { status: uploadRes.status }))
      }
      let uploadedId: string | null = null
      try {
        const data = await uploadRes.clone().json()
        uploadedId = data?.entity_id ?? null
      } catch (e) {
        // Non-JSON response: the upload itself succeeded; continue.
      }

      // Canonical post-save refresh (image-editor pattern): land on the
      // (re)placed file so the view re-renders the new content.
      if (uploadedId) {
        const treeRef = fileTreeData?.root
        const deadline = Date.now() + 10000
        let newRef: TreeNode | null = null
        const sleep = (ms: number) => new Promise(r => setTimeout(r, ms))
        while (Date.now() < deadline) {
          const currentTree = fileTreeData?.root as TreeNode | undefined
          if (currentTree) {
            newRef = findInTree(currentTree as unknown as Folder, uploadedId)
          }
          if (newRef) break
          await sleep(150)
        }
        if (newRef) {
          handleFileTreeSelect([newRef])
        }
      }
      onClose()
    } catch (err: unknown) {
      setError((err as Error)?.message || t('svg_edit_save_failed'))
      setSaving(false)
    }
  }, [text, saving, file, projectId, fileTreeData, handleFileTreeSelect, t, onClose])

  return (
    <OLModal
      // AD (2026-10-07): same full-space treatment as the image editor.
      size="full"
      style={{ padding: 8 }}
      show
      className="toast-svg-editor-modal"
      onHide={requestClose}
    >
      <OLModalHeader>
        <OLModalTitle>
          {t('edit_svg', 'Edit SVG')} – {file.name}
        </OLModalTitle>
      </OLModalHeader>
      <OLModalBody style={{ flex: 1, minHeight: 0, overflow: 'hidden' }}>
        {failed ? (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              gap: 12,
              padding: '48px 24px',
            }}
          >
            <p>{error || t('svg_edit_load_failed')}</p>
            <OLButton variant="primary" onClick={() => window.location.reload()}>
              {t('retry', 'Retry')}
            </OLButton>
          </div>
        ) : text == null ? (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              gap: 10,
              padding: '24px 0',
            }}
          >
            <LoadingSpinner />
            <span style={{ fontSize: 13, opacity: 0.7 }}>
              {t('svg_edit_loading', 'Loading the SVG editor…')}
            </span>
          </div>
        ) : (
          <>
            {error && <OLNotification type="error" content={error} />}
            <div
              style={{
                display: 'flex',
                gap: 'var(--mantine-spacing-md)',
                height: 'calc(100vh - 170px)',
                minHeight: 320,
              }}
            >
              {/* Stage 2 editor: SVG source. A plain textarea — deterministic,
                  zero extra bundle weight, and SVG is plain XML text. */}
              <textarea
                ref={editorElRef}
                value={text}
                onChange={e => setText(e.target.value)}
                spellCheck={false}
                aria-label={t('svg_source', 'SVG source')}
                style={{
                  flex: 1,
                  minWidth: 0,
                  resize: 'none',
                  fontFamily:
                    "var(--mantine-font-family-monospace, 'DM Mono', monospace)",
                  fontSize: 13,
                  lineHeight: 1.5,
                  padding: 12,
                  border: '1px solid var(--border-divider-themed, #ccc)',
                  borderRadius: 8,
                  background: 'var(--bg-primary-themed, #fff)',
                  color: 'inherit',
                  outline: 'none',
                }}
              />
              <div
                style={{
                  flex: 1,
                  minWidth: 0,
                  border: '1px solid var(--border-divider-themed, #ccc)',
                  borderRadius: 8,
                  background: '#fff',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  overflow: 'auto',
                  padding: 12,
                }}
                aria-label={t('svg_preview', 'SVG preview')}
              >
                {previewUrl ? (
                  <img
                    src={previewUrl}
                    alt={t('svg_preview_alt', 'SVG preview')}
                    style={{ maxWidth: '100%', maxHeight: '100%' }}
                  />
                ) : (
                  <span style={{ opacity: 0.6, fontSize: 13 }}>
                    {t('svg_preview_missing', 'No preview available')}
                  </span>
                )}
              </div>
            </div>
          </>
        )}
      </OLModalBody>
      <OLModalFooter>
        {confirmDiscard ? (
          <>
            <span style={{ marginRight: 'auto' }}>
              {t('unsaved_svg_changes', 'Unsaved SVG changes')}
            </span>
            <OLButton
              variant="secondary"
              onClick={() => setConfirmDiscard(false)}
            >
              {t('keep_editing', 'Keep editing')}
            </OLButton>
            <OLButton
              variant="danger"
              onClick={() => {
                setConfirmDiscard(false)
                onClose()
              }}
            >
              {t('discard', 'Discard')}
            </OLButton>
          </>
        ) : (
          <>
            <OLButton variant="secondary" onClick={requestClose}>
              {t('close', 'Close')}
            </OLButton>
            <OLButton
              variant="primary"
              onClick={() => void handleSave()}
              disabled={text == null || saving}
            >
              {t('save', 'Save')}
            </OLButton>
          </>
        )}
      </OLModalFooter>
    </OLModal>
  )
}

// Folder type alias for the findInTree signature (mirrors toast-image).
type Folder = { name?: string }

import { Project } from '../../../../services/web/types/project'
import { PermissionsLevel } from '@/features/ide-react/types/permissions'
import type { DocumentContainer } from '@/features/ide-react/editor/document-container'
import { GotoLineOptions } from '@/features/ide-react/types/goto-line-options'
import { GotoOffsetOptions } from '@/features/ide-react/context/editor-manager-context'
import { CursorPosition } from '@/features/ide-react/types/cursor-position'
import { FileTreeFindResult } from '@/features/ide-react/types/file-tree'

export type IdeEvents = {
  'project:joined': [{ project: Project; permissionsLevel: PermissionsLevel }]
  'document:closed': [doc: DocumentContainer]
  'doc:changed': [{ doc_id: string }]
  'doc:saved': [{ doc_id: string }]
  'ide:opAcknowledged': [{ doc_id: string; op: any }]
  'store-doc-position': []
  'editor:gotoOffset': [options: GotoOffsetOptions]
  'editor:gotoLine': [options: GotoLineOptions]
  'cursor:editor:update': [position: CursorPosition]
  'outline-toggled': [isOpen: boolean]
  'cursor:editor:syncToPdf': []
  'scroll:editor:update': [middleVisibleLine?: number]
  'comment:start_adding': []
  'history:toggle': []
  'entity:deleted': [entity: FileTreeFindResult]
  'ide:unableToSyncOfflineChanges': [
    {
      docId: string
      editorContent: string
      baseContent: string
      docName: string
      reloadAfterClose?: boolean
    },
  ]
  'ide:offlineChangesSynced': [{ docId: string }]
}

export class IdeEventEmitter extends EventTarget {
  emit<T extends keyof IdeEvents>(eventName: T, ...detail: IdeEvents[T]) {
    this.dispatchEvent(new CustomEvent<IdeEvents[T]>(eventName, { detail }))
  }

  on<T extends keyof IdeEvents>(
    eventName: T,
    listener: (event: CustomEvent<IdeEvents[T]>) => void
  ) {
    this.addEventListener(eventName, listener as EventListener)
  }

  once<T extends keyof IdeEvents>(
    eventName: T,
    listener: (event: CustomEvent<IdeEvents[T]>) => void
  ) {
    this.addEventListener(eventName, listener as EventListener, { once: true })
  }

  off<T extends keyof IdeEvents>(
    eventName: T,
    listener: (event: CustomEvent<IdeEvents[T]>) => void
  ) {
    this.removeEventListener(eventName, listener as EventListener)
  }
}

export function createIdeEventEmitter() {
  return new IdeEventEmitter()
}

import { findInTree } from '@/features/file-tree/util/find-in-tree'
import { Folder } from '../../../../../services/web/types/folder'
import { Doc } from '../../../../../services/web/types/doc'
import { FileRef } from '../../../../../services/web/types/file-ref'

export function findDocEntityById(fileTreeData: Folder, docId: string) {
  const item = findInTree(fileTreeData, docId)
  if (!item || item.type !== 'doc') {
    return null
  }
  return item.entity as Doc
}

export function findFileRefEntityById(fileTreeData: Folder, docId: string) {
  const item = findInTree(fileTreeData, docId)
  if (!item || item.type !== 'fileRef') {
    return null
  }
  return item.entity as FileRef
}

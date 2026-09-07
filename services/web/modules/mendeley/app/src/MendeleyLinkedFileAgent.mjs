import logger from '@overleaf/logger'
import { callbackify } from '@overleaf/promise-utils'
import { Project } from '../../../../app/src/models/Project.mjs'
import ProjectLocator from '../../../../app/src/Features/Project/ProjectLocator.mjs'
import UserGetter from '../../../../app/src/Features/User/UserGetter.mjs'
import LinkedFilesHandler from '../../../../app/src/Features/LinkedFiles/LinkedFilesHandler.mjs'
import LinkedFilesErrors from '../../../../app/src/Features/LinkedFiles/LinkedFilesErrors.mjs'
import MendeleyApiClient from './MendeleyApiClient.mjs'
import {
  MendeleyForbiddenError,
  MendeleyExpiredError,
  MendeleyAccountNotLinkedError,
  MendeleyNotConfiguredError,
} from './MendeleyApiClient.mjs'

const {
  AccessDeniedError,
  NotOriginalImporterError,
  RemoteServiceError,
} = LinkedFilesErrors

/**
 * Create a linked .bib file from Mendeley (either My Library or a group).
 *
 * linkedFileData shape:
 *   {
 *     provider: 'mendeley'
 *     mendeleyGroupId?: string
 *     importedAt: Date | string
 *     importedByUserId?: string
 *     importedByName?: string
 *   }
 *
 *  - If mendeleyGroupId is present, export that group's library.
 *  - Otherwise, export the user's personal library ("My Library").
 */
async function createLinkedFile(
  projectId,
  linkedFileData,
  name,
  parentFolderId,
  userId
) {
  logger.debug(
    { projectId, userId, groupId: linkedFileData.mendeleyGroupId },
    'creating Mendeley linked file'
  )

  linkedFileData.importedByUserId = userId
  linkedFileData.importedByName = await _getUserName(userId) || 'Unknown'

  const bibtex = await _getBibtex(linkedFileData)

  const file = await LinkedFilesHandler.promises.importContent(
    projectId,
    bibtex,
    _sanitizeData(linkedFileData),
    name,
    parentFolderId,
    userId
  )
  return file._id
}

/**
 * Refresh an existing Mendeley linked .bib file.
 */
async function refreshLinkedFile(
  projectId,
  linkedFileData,
  name,
  parentFolderId,
  userId
) {
  logger.debug(
    { projectId, userId, linkedFileData },
    'refreshing Mendeley linked file'
  )

  // refresh importer's displayed name
  // if the importer is the owner, name is not displayed, refresh is not needed
  // if the importer is not available, the old name is preserved
  const userName = await _getUserName(linkedFileData.importedByUserId)
  if (userName && linkedFileData.importedByUserId != userId) {
    linkedFileData.importedByName = userName
    const { element, path } = await ProjectLocator.promises.findElement({
      project_id: projectId,
      element_id: parentFolderId,
      type: 'folders'
    })
    const fileIndex = element.fileRefs.findIndex(file => file.name === name)
    const updatePath = `${path.mongo}.fileRefs.${fileIndex}.linkedFileData.importedByName`
    await Project.updateOne({ _id: projectId }, { $set: { [updatePath]: userName } })
  }

  const bibtex = await _getBibtex(linkedFileData)

  const file = await LinkedFilesHandler.promises.importContent(
    projectId,
    bibtex,
    _sanitizeData(linkedFileData),
    name,
    parentFolderId,
    userId
  )
  return file._id
}

async function _getBibtex(linkedFileData) {
  const userId = linkedFileData.importedByUserId
  try {
    if (linkedFileData.mendeleyGroupId) {
      return await MendeleyApiClient.getGroupLibraryBibtex(
        userId,
        linkedFileData.mendeleyGroupId
      )
    }
    return await MendeleyApiClient.getUserLibraryBibtex(userId)
  } catch (err) {
    if (err instanceof MendeleyForbiddenError || err instanceof MendeleyExpiredError) {
      logger.debug({ linkedFileData, err }, 'Mendeley access denied')
      throw new AccessDeniedError('Mendeley access denied').withCause(err)
    }
    if (
      err instanceof MendeleyAccountNotLinkedError ||
      err instanceof MendeleyNotConfiguredError
    ) {
      logger.debug({ linkedFileData, err }, 'Mendeley account not linked')
      throw new AccessDeniedError('Mendeley account not linked').withCause(err)
    }
    logger.error({ linkedFileData, err }, 'failed to retrieve bib file from Mendeley')
    throw new RemoteServiceError('Error retrieving bib file from Mendeley').withCause(err)
  }
}

function _sanitizeData(data) {
  return {
    provider: 'mendeley',
    ...(data.mendeleyGroupId && {
      mendeleyGroupId: data.mendeleyGroupId,
    }),
    importedAt: data.importedAt,
    ...(data.importedByUserId && {
      importedByUserId: data.importedByUserId,
    }),
    importedByName: data.importedByName || 'Unknown',
  }
}

async function _getUserName(userId) {
  let user = null
  try {
    user = await UserGetter.promises.getUser(userId, {'email': 1, 'first_name': 1, 'last_name': 1})
  }
  catch (err) {
    logger.error({ userId, err }, 'failed to get user info')
  }
  if (!user) return null

  const { email, first_name, last_name } = user
  const name = (first_name || last_name) ?
    [first_name, last_name].filter(n => n != null).join(' ') : email
  return name
}

export default {
  createLinkedFile: callbackify(createLinkedFile),
  refreshLinkedFile: callbackify(refreshLinkedFile),
  promises: { createLinkedFile, refreshLinkedFile }
}

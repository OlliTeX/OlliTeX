import ProjectDeleter from '../Project/ProjectDeleter.mjs'
import EditorController from './EditorController.mjs'
import ProjectGetter from '../Project/ProjectGetter.mjs'
import AuthorizationManager from '../Authorization/AuthorizationManager.mjs'
import ProjectEditorHandler from '../Project/ProjectEditorHandler.mjs'
import Metrics from '@overleaf/metrics'
import CollaboratorsInviteGetter from '../Collaborators/CollaboratorsInviteGetter.mjs'
import PrivilegeLevels from '../Authorization/PrivilegeLevels.mjs'
import SessionManager from '../Authentication/SessionManager.mjs'
import Errors from '../Errors/Errors.js'
import { expressify } from '@overleaf/promise-utils'
import Settings from '@overleaf/settings'
import CollaboratorsGetter from '../Collaborators/CollaboratorsGetter.mjs'
import { z, zz, parseReq } from '../../infrastructure/Validation.mjs'
import { Project } from '../../models/Project.mjs'
import { File } from '../../models/File.mjs'
import DocstoreManager from '../Docstore/DocstoreManager.mjs'
import ProjectEntityUpdateHandler from '../Project/ProjectEntityUpdateHandler.mjs'
import EditorRealTimeController from './EditorRealTimeController.mjs'
import { generateDuplicateName } from '../Project/ProjectDuplicator.mjs'

const ProjectAccess = CollaboratorsGetter.ProjectAccess

/**
 * Walk the project's inline folder tree (project.rootFolder) to locate the
 * entity with the given id. Entities are stored inline on the project document
 * (this CE build does not use a separate `folders` collection), so we walk
 * project.rootFolder directly. Returns { folder, ref, kind: 'doc' | 'file' }|null.
 */
function _findEntityInProject(project, targetId) {
  const walk = (folder) => {
    for (const d of folder.docs || []) {
      if (d._id && d._id.toString() === targetId) {
        return { folder, ref: d, kind: 'doc' }
      }
    }
    for (const f of folder.fileRefs || []) {
      if (f._id && f._id.toString() === targetId) {
        return { folder, ref: f, kind: 'file' }
      }
    }
    for (const sub of folder.folders || []) {
      const found = walk(sub)
      if (found) return found
    }
    return null
  }
  for (const root of project.rootFolder || []) {
    const found = walk(root)
    if (found) return found
  }
  return null
}

/**
 * New 2 (2026-08-28): duplicate a single file in the project file tree.
 * - text entities (doc: tex/bib/txt/...) — their content lives in the
 *   docstore, so the copy is created with addDoc + the source lines
 * - binary entities (file: jpg/png/pdf/...) — the filestore blob already
 *   exists under the source hash, so the copy just references it
 * Naming: a.b -> a_copy.b -> a_copy(1).b ... (generateDuplicateName).
 */
async function duplicateEntity(req, res, next) {
  const projectId = req.params.Project_id
  const entityType = req.params.entity_type
  const entityId = req.params.entity_id
  const userId = SessionManager.getLoggedInUserId(req.session)
  if (entityType !== 'doc' && entityType !== 'file') {
    return res.sendStatus(400)
  }
  try {
    const project = await Project.findById(projectId).exec()
    const rootFolderId = project?.rootFolder?.[0]?._id
    if (!project || !rootFolderId) {
      return res.sendStatus(404)
    }
    const found = _findEntityInProject(project, entityId)
    if (!found || found.kind !== entityType) {
      return res.sendStatus(404)
    }
    const { folder, ref } = found
    const folderId = folder._id.toString()
    const siblings = [
      ...(folder.docs || []).map(d => d.name),
      ...(folder.fileRefs || []).map(f => f.name),
    ]
    const newName = generateDuplicateName(ref.name, siblings)

    if (entityType === 'doc') {
      let lines = []
      try {
        ;({ lines } = await DocstoreManager.promises.getDoc(
          projectId.toString(),
          entityId.toString()
        ))
      } catch (err) {
        lines = []
      }
      const doc = await EditorController.promises.addDoc(
        projectId,
        folderId,
        newName,
        lines || [],
        'editor',
        userId
      )
      return res.json(doc)
    }

    // The filestore blob already exists under the source hash: build a fresh
    // File doc (own _id) that shares it — no re-upload (createdBlob: false).
    const fileRef = new File({
      name: newName,
      hash: ref.hash,
      linkedFileData: ref.linkedFileData,
    })
    const { fileRef: created } =
      await ProjectEntityUpdateHandler.promises.duplicateFile(
        projectId,
        folderId,
        fileRef,
        'editor',
        userId
      )
    EditorRealTimeController.emitToRoom(
      projectId,
      'reciveNewFile',
      folderId,
      created,
      'editor',
      ref.linkedFileData,
      userId
    )
    return res.json(created)
  } catch (err) {
    return next(err)
  }
}

export default {
  joinProject: expressify(joinProject),
  addDoc: expressify(addDoc),
  addFolder: expressify(addFolder),
  renameEntity: expressify(renameEntity),
  moveEntity: expressify(moveEntity),
  duplicateEntity: expressify(duplicateEntity),
  deleteDoc: expressify(deleteDoc),
  deleteFile: expressify(deleteFile),
  deleteFolder: expressify(deleteFolder),
  _nameIsAcceptableLength,
}

export const joinProjectSchema = z.object({
  params: z.strictObject({
    Project_id: zz.objectId(),
  }),
  body: z.strictObject({
    // the real-time service sends the sentinel 'anonymous-user' for
    // link-sharing visitors with no account (see real-time's
    // Router.js/WebsocketController.js), otherwise a genuine user id
    userId: zz.objectId().or(z.literal('anonymous-user')),
    anonymousAccessToken: z.string().optional(),
  }),
})

// Rollout-temporary fallback (pre-refinement schema from main); delete
// when this route's REQ_VALIDATION_MODE instrumentation is removed.
const joinProjectFallbackSchema = z.object({
  params: z.object({
    Project_id: zz.objectId(),
  }),
  body: z.object({
    userId: z.string(),
    anonymousAccessToken: z.string().optional(),
  }),
})

async function joinProject(req, res, next) {
  const { params, body } = parseReq(req, joinProjectSchema, {
    fallbackSchema: joinProjectFallbackSchema,
  })
  const projectId = params.Project_id
  let userId = body.userId
  if (userId === 'anonymous-user') {
    userId = null
  }
  Metrics.inc('editor.join-project')
  const {
    project,
    privilegeLevel,
    isRestrictedUser,
    isTokenMember,
    isInvitedMember,
  } = await _buildJoinProjectView(projectId, userId, body.anonymousAccessToken)
  if (!project) {
    return res.sendStatus(403)
  }
  // Only show the 'renamed or deleted' message once
  if (project.deletedByExternalDataSource) {
    await ProjectDeleter.promises.unmarkAsDeletedByExternalSource(projectId)
  }

  if (project.spellCheckLanguage) {
    project.spellCheckLanguage = await chooseSpellCheckLanguage(
      project.spellCheckLanguage
    )
  }

  res.json({
    project,
    privilegeLevel,
    isRestrictedUser,
    isTokenMember,
    isInvitedMember,
  })
}

async function _buildJoinProjectView(projectId, userId, token) {
  const project = await ProjectGetter.promises.getProject(projectId)
  if (project == null) {
    throw new Errors.NotFoundError('project not found')
  }
  const projectAccess = new ProjectAccess(project)
  const privilegeLevel =
    await AuthorizationManager.promises.getPrivilegeLevelForProjectWithProjectAccess(
      userId,
      projectId,
      token,
      projectAccess
    )
  if (privilegeLevel == null || privilegeLevel === PrivilegeLevels.NONE) {
    return { project: null, privilegeLevel: null, isRestrictedUser: false }
  }
  const isTokenMember = projectAccess.isUserTokenMember(userId)
  const isInvitedMember = projectAccess.isUserInvitedMember(userId)
  const isRestrictedUser = AuthorizationManager.isRestrictedUser(
    userId,
    privilegeLevel,
    isTokenMember,
    isInvitedMember
  )
  let ownerMember
  let members = []
  let invites = []
  if (isRestrictedUser) {
    ownerMember = await projectAccess.loadOwner()
  } else {
    ;({ ownerMember, members } =
      await projectAccess.loadOwnerAndInvitedMembers())
    invites = await CollaboratorsInviteGetter.promises.getAllInvites(projectId)
  }
  const accessRequestData = {}
  if (privilegeLevel === PrivilegeLevels.OWNER) {
    accessRequestData.editAccessRequests =
      await projectAccess.loadAccessRequestsView()
  } else if (userId) {
    // Caller's own request only, so safe for restricted (link-share) viewers.
    accessRequestData.myAccessRequest =
      projectAccess.getAccessRequestForUser(userId)
  }
  return {
    project: ProjectEditorHandler.buildProjectModelView(
      project,
      ownerMember,
      members,
      invites,
      isRestrictedUser,
      accessRequestData
    ),
    privilegeLevel,
    isTokenMember,
    isInvitedMember,
    isRestrictedUser,
  }
}

function _nameIsAcceptableLength(name) {
  return name != null && name.length < 150 && name.length !== 0
}

const addEntitySchema = z.object({
  params: z.strictObject({
    Project_id: zz.objectId(),
  }),
  body: z.strictObject({
    // an unacceptable (missing/empty/too-long) name is handled below by
    // _nameIsAcceptableLength, which replies with a bare 400 -- kept
    // optional here so that path (not a validation-error response) still
    // runs for a missing name
    name: z.string().optional(),
    // top-level creation (directly under the project root) omits this
    // field entirely, or sends it as null -- see file-tree-actionable.tsx's
    // getSelectedParentFolderId, which does pass the root folder's own id
    // explicitly for the interactive editor's "new file/folder" UI, but
    // other real callers (e.g. ProjectStructureTests.mjs's
    // createExampleFolder, mirroring third-party API usage) legitimately
    // omit it or send null. ProjectEntityMongoUpdateHandler's
    // _confirmFolder() already treats a missing/null parentFolderId
    // (`== null`) as "use the project's root folder", exactly like
    // ProjectUploadController's analogous folder_id/targetFolderId fields,
    // so this is modeled the same way here rather than requiring every
    // caller to resolve the root folder id up front.
    parent_folder_id: zz.objectId().nullish(),
  }),
})

async function addDoc(req, res, next) {
  const { params, body } = parseReq(req, addEntitySchema, { logOnly: true })
  const projectId = params.Project_id
  const { name } = body
  const parentFolderId = body.parent_folder_id
  const userId = SessionManager.getLoggedInUserId(req.session)

  if (!_nameIsAcceptableLength(name)) {
    return res.sendStatus(400)
  }
  try {
    const doc = await EditorController.promises.addDoc(
      projectId,
      parentFolderId,
      name,
      [],
      'editor',
      userId
    )
    res.json(doc)
  } catch (err) {
    if (err instanceof Errors.TooManyFilesError) {
      res.status(400).json(
        req.i18n.translate('project_has_too_many_files_limit', {
          limit: Settings.maxEntitiesPerProject,
        })
      )
    } else {
      next(err)
    }
  }
}

async function addFolder(req, res, next) {
  const { params, body } = parseReq(req, addEntitySchema, { logOnly: true })
  const projectId = params.Project_id
  const { name } = body
  const parentFolderId = body.parent_folder_id
  const userId = SessionManager.getLoggedInUserId(req.session)
  if (!_nameIsAcceptableLength(name)) {
    return res.sendStatus(400)
  }
  try {
    const doc = await EditorController.promises.addFolder(
      projectId,
      parentFolderId,
      name,
      'editor',
      userId
    )
    res.json(doc)
  } catch (err) {
    if (err instanceof Errors.TooManyFilesError) {
      res.status(400).json(
        req.i18n.translate('project_has_too_many_files_limit', {
          limit: Settings.maxEntitiesPerProject,
        })
      )
    } else if (err.message === 'invalid element name') {
      res.status(400).json(req.i18n.translate('invalid_file_name'))
    } else {
      next(err)
    }
  }
}

const entityActionParamsSchema = z.strictObject({
  Project_id: zz.objectId(),
  entity_id: zz.objectId(),
  entity_type: z.enum(['doc', 'file', 'folder']),
})

const renameEntitySchema = z.object({
  params: entityActionParamsSchema,
  body: z.strictObject({
    name: z.string().optional(),
    source: z.string().optional(),
  }),
})

async function renameEntity(req, res, next) {
  const { params, body } = parseReq(req, renameEntitySchema, {
    logOnly: true,
  })
  const projectId = params.Project_id
  const entityId = params.entity_id
  const entityType = params.entity_type
  const { name, source = 'editor' } = body
  if (!_nameIsAcceptableLength(name)) {
    return res.sendStatus(400)
  }
  const userId = SessionManager.getLoggedInUserId(req.session)
  await EditorController.promises.renameEntity(
    projectId,
    entityId,
    entityType,
    name,
    userId,
    source
  )
  res.sendStatus(204)
}

const moveEntitySchema = z.object({
  params: entityActionParamsSchema,
  body: z.strictObject({
    folder_id: zz.objectId(),
    source: z.string().optional(),
  }),
})

async function moveEntity(req, res, next) {
  const { params, body } = parseReq(req, moveEntitySchema, { logOnly: true })
  const projectId = params.Project_id
  const entityId = params.entity_id
  const entityType = params.entity_type
  const folderId = body.folder_id
  const source = body.source ?? 'editor'
  const userId = SessionManager.getLoggedInUserId(req.session)
  await EditorController.promises.moveEntity(
    projectId,
    entityId,
    folderId,
    entityType,
    userId,
    source
  )
  res.sendStatus(204)
}

const deleteEntitySchema = z.object({
  params: z.strictObject({
    Project_id: zz.objectId(),
    entity_id: zz.objectId(),
  }),
})

async function deleteDoc(req, res, next) {
  await _deleteEntity(req, res, 'doc')
}

async function deleteFile(req, res, next) {
  await _deleteEntity(req, res, 'file')
}

async function deleteFolder(req, res, next) {
  await _deleteEntity(req, res, 'folder')
}

async function _deleteEntity(req, res, entityType) {
  const { params } = parseReq(req, deleteEntitySchema, { logOnly: true })
  const projectId = params.Project_id
  const entityId = params.entity_id
  const userId = SessionManager.getLoggedInUserId(req.session)
  await EditorController.promises.deleteEntity(
    projectId,
    entityId,
    entityType,
    'editor',
    userId
  )
  res.sendStatus(204)
}

const supportedSpellCheckLanguages = new Set(
  Settings.languages
    // only include spell-check languages that are available in the client
    .filter(language => language.dic !== undefined)
    .map(language => language.code)
)

async function chooseSpellCheckLanguage(spellCheckLanguage) {
  if (supportedSpellCheckLanguages.has(spellCheckLanguage)) {
    return spellCheckLanguage
  }

  // Preserve the value in the database so they can use it again once we add back support.
  // Map some server-only languages to a specific variant, or disable spell checking for currently unsupported spell check languages.
  switch (spellCheckLanguage) {
    case 'en':
      // map "English" to "English (American)"
      return 'en_US'

    case 'no':
      // map "Norwegian" to "Norwegian (Bokmål)"
      return 'nb_NO'

    default:
      // map anything else to "off"
      return ''
  }
}

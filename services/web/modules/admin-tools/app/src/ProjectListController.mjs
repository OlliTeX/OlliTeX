import _ from 'lodash'
import { ObjectId } from 'mongodb'
import { expressify } from '@overleaf/promise-utils'
import SessionManager from '../../../../app/src/Features/Authentication/SessionManager.mjs'
import ProjectHelper from '../../../../app/src/Features/Project/ProjectHelper.mjs'
import ProjectDeleter from '../../../../app/src/Features/Project/ProjectDeleter.mjs'
import { Project } from '../../../../app/src/models/Project.mjs'
import { DeletedProject } from '../../../../app/src/models/DeletedProject.mjs'
import { OError } from '../../../../app/src/Features/Errors/Errors.js'


async function getProjectsJson(req, res) {
  const { filters, page, sort } = req.body
  // The admin "All projects" list calls POST /admin/user/null/projects with the
  // literal string "null" (see api.ts). Normalize it here — without this the
  // query becomes { owner_ref: "null" } and mongoose throws a CastError (500).
  const rawUserId = req.params.userId
  const userId = !rawUserId || rawUserId === 'null' ? null : rawUserId

  const projectsPage = await _getProjects(userId, filters, sort, page)
  res.json(projectsPage)
}

async function _getProjects(
  userId = null,
  filters = {},
  sort = { by: 'lastUpdated', order: 'desc' },
  page = { size: 20 }
) {

  const projection = {
    _id: 1,
    name: 1,
    lastUpdated: 1,
    lastUpdatedBy: 1,
    lastOpened: 1,
    trashed: 1,
    owner_ref: 1,
  }

  const actualProjects = await Project.find(
    userId == null ? {} : { owner_ref: userId },
    projection,
  ).lean().exec()

  const delProjection = Object.fromEntries(
    Object.keys(projection).map(k => [`project.${k}`, 1])
  )
  delProjection['deleterData.deletedAt'] = 1
  delProjection['deleterData.deleterId'] = 1

  const deletedProjects = await DeletedProject.find(
    userId == null ? { project: { $type: 'object' } } : { 'project.owner_ref': userId },
    delProjection
  ).lean().exec()

  const formattedActualProjects = _formatProjects(actualProjects, _formatProjectInfo)
  const formattedDeletedProjects = _formatProjects(deletedProjects, _formatDeletedProjectInfo)
  const formattedProjects = [...formattedActualProjects, ...formattedDeletedProjects]
  const filteredProjects = _applyFilters(formattedProjects, filters)
  const projects = _sortAndPaginate(filteredProjects, sort, page)

  return {
    totalSize: filteredProjects.length,
    projects,
  }
}

function _formatProjects(projects, formatProjectInfo) {
  const yearAgo = new Date()
  yearAgo.setFullYear(yearAgo.getFullYear() - 1)
  const formattedProjects = []

  for (const project of projects) {
    formattedProjects.push(
      formatProjectInfo(project, yearAgo)
    )
  }
  return formattedProjects
}

function _applyFilters(projects,  filters) {
  if (!_hasActiveFilter(filters)) {
    return projects
  }
  return projects.filter(project => _matchesFilters(project, filters))
}

function _sortAndPaginate(projects, sort, page) {
  if (
    (sort.by && !['lastUpdated', 'title', 'deletedAt', 'owner'].includes(sort.by)) ||
    (sort.order && !['asc', 'desc'].includes(sort.order))
  ) {
    throw new OError('Invalid sorting criteria', { sort })
  }

// sorting by owner is not implemented, it is not needed
  const sortedProjects =
    sort.by === 'title'
      ? [...projects].sort((a, b) =>
          (a.name ?? '\uffff').localeCompare(b.name ?? '\uffff')
        )
      : _.orderBy(
          projects,
          [sort.by || 'lastUpdated'],
          [sort.order || 'desc']
        )
  return sortedProjects
}

function _formatProjectInfo(project, maxDate) {
  const owner_ref = project.owner_ref
  const trashed = owner_ref ? ProjectHelper.isTrashed(project, owner_ref) : false

  return {
    id: project._id.toString(),
    name: project.name,
    owner: project.owner_ref,
    lastUpdated: project.lastUpdated?.toISOString(),
    lastUpdatedBy: project.lastUpdatedBy,
    inactive: project.lastOpened < maxDate, 
    trashed,
    deleted: false,
  }
}

function _formatDeletedProjectInfo(deletedProject, maxDate) {
  const project = deletedProject.project
  const owner_ref = project.owner_ref
  const trashed = owner_ref ? ProjectHelper.isTrashed(project, owner_ref) : false

  return {
    id: project._id.toString(),
    name: project.name,
    owner: owner_ref,
    lastUpdated: project.lastUpdated?.toISOString(),
    lastUpdatedBy: project.lastUpdatedBy,
    inactive: project.lastOpened < maxDate,
    trashed,
    deleted: true,
    deletedAt: deletedProject.deleterData?.deletedAt?.toISOString(),
    deleterId: deletedProject.deleterData?.deleterId,
  }
}

function _matchesFilters(project, filters) {
  if (filters.owned && (project.trashed || project.deleted)) {
    return false
  }
  if (filters.trashed && (!project.trashed || project.deleted)) {
    return false
  }
  if (filters.deleted && !project.deleted) {
    return false
  }
  if (filters.inactive && (project.trashed || project.deleted || !project.inactive)) {
    return false
  }
  if (
    filters.search?.length &&
    project.name.toLowerCase().indexOf(filters.search.toLowerCase()) === -1
  ) {
    return false
  }
  return true
}

function _hasActiveFilter(filters) {
  return Boolean(
    filters.owned ||
      filters.inactive ||
      filters.trashed ||
      filters.deleted ||
      filters.search?.length
  )
}

async function resolveProjectUserId(body, projectId) {
  // 2026-09 (hub admin-projects regression): the hub's client-side owner
  // resolver can deliver an empty/invalid userId, which made
  // `new ObjectId('')` throw BSONError (500) in ProjectDeleter. Prefer a
  // valid client value, else fall back to the project's own owner so the
  // trash/untrash/undelete bookkeeping stays deterministic.
  const raw = body && body.userId
  const s = raw == null ? '' : String(raw)
  if (ObjectId.isValid(s)) {
    return s
  }
  let id = null
  try {
    // Promise API (mongoose-wrapper statics are callback-shaped; mirror the
    // Project.find(...).exec() pattern already used by _getProjects above).
    const proj = await Project.findById(projectId, {
      owner_ref: 1,
      'owner._id': 1,
    })
      .lean()
      .exec()
    id =
      (proj
        ? proj.owner_ref != null
          ? String(proj.owner_ref)
          : proj.owner && proj.owner._id != null
            ? String(proj.owner._id)
            : null
        : null) || null
  } catch (err) {
    if (err && err.name === 'ObjectValidationError') throw err
  }
  if (id == null) {
    throw new OError('cannot resolve a valid user for project trash bookkeeping')
  }
  return id
}

async function trashProjectForUser(req, res) {
  const projectId = req.params.project_id
  const userId = await resolveProjectUserId(req.body, projectId)
  await ProjectDeleter.promises.trashProject(projectId, userId)
  res.sendStatus(200)
}

async function untrashProjectForUser(req, res) {
  const projectId = req.params.project_id
  const userId = await resolveProjectUserId(req.body, projectId)
  await ProjectDeleter.promises.untrashProject(projectId, userId)
  res.sendStatus(200)
}

async function deleteProject(req, res) {
  const projectId = req.params.project_id
  const deleterId = SessionManager.getLoggedInUserId(req.session)
  const options = { deleterUser: {_id: deleterId } }
  const deletedProjectData = await ProjectDeleter.promises.deleteProject(projectId, options)
  return res.json(deletedProjectData)
}

async function undeleteProject(req, res) {
  const projectId = req.params.project_id
  const userId = await resolveProjectUserId(req.body, projectId)
  const undelededProject = await ProjectDeleter.promises.undeleteProject(projectId, { userId })
  await ProjectDeleter.promises.untrashProject(projectId, userId)

  return res.json({
    name: undelededProject.name,
  })
}

async function purgeDeletedProject(req, res) {
  const projectId = req.params.project_id
  await ProjectDeleter.promises.expireDeletedProject(projectId)
  res.sendStatus(200)
}

export default {
  getProjectsJson: expressify(getProjectsJson),
  deleteProject: expressify(deleteProject),
  undeleteProject: expressify(undeleteProject),
  purgeDeletedProject: expressify(purgeDeletedProject),
  trashProjectForUser: expressify(trashProjectForUser),
  untrashProjectForUser: expressify(untrashProjectForUser),
}

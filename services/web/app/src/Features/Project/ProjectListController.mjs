// @ts-check
import _ from 'lodash'

import ProjectHelper from './ProjectHelper.mjs'
import ProjectGetter from './ProjectGetter.mjs'
import PrivilegeLevels from '../Authorization/PrivilegeLevels.mjs'
import SessionManager from '../Authentication/SessionManager.mjs'
import Sources from '../Authorization/Sources.mjs'
import UserGetter from '../User/UserGetter.mjs'
import TagsHandler from '../Tags/TagsHandler.mjs'
import { expressify } from '@overleaf/promise-utils'
import { OError } from '../Errors/Errors.js'
import { parseReq, z, zz } from '../../infrastructure/Validation.mjs'

const getProjectsJsonSchema = z.object({
  body: z.strictObject({
    filters: z
      .strictObject({
        ownedByUser: z.boolean().optional(),
        sharedWithUser: z.boolean().optional(),
        archived: z.boolean().optional(),
        trashed: z.boolean().optional(),
        // null is a distinct, meaningful value (see _hasActiveFilter) --
        // not the same as the field being absent
        tag: z.string().nullish(),
        search: z.string().optional(),
      })
      .optional(),
    sort: z
      .strictObject({
        by: z.enum(['lastUpdated', 'title', 'owner']).optional(),
        order: z.enum(['asc', 'desc']).optional(),
      })
      .optional(),
    page: z
      .strictObject({
        size: z.number().int().positive().optional(),
        lastId: zz.objectId().optional(),
      })
      .optional(),
  }),
})

/**
 * Load user's projects with pagination, sorting and filters
 *
 * @param {GetProjectsRequest} req the request
 * @param {GetProjectsResponse} res the response
 * @returns {Promise<void>}
 */
async function getProjectsJson(req, res) {
  const { body } = parseReq(req, getProjectsJsonSchema, { logOnly: true })
  const { filters, page, sort } = body
  const userId = SessionManager.getLoggedInUserId(req.session)
  const projectsPage = await _getProjects(userId, filters, sort, page)
  res.json(projectsPage)
}

/**
 * @param {string} userId
 * @param {Filters} filters
 * @param {Partial<Sort>} sort
 * @param {Partial<Page>} page
 * @returns {Promise<{totalSize: number, projects: Project[]}>}
 * @private
 */
async function _getProjects(
  userId,
  filters = {},
  sort = { by: 'lastUpdated', order: 'desc' },
  page = { size: 20 }
) {
  /** @type {[AllUsersProjects, MongoTag[]]} */
  const results = await Promise.all([
    ProjectGetter.promises.findAllUsersProjects(
      userId,
      'name lastUpdated lastUpdatedBy publicAccesLevel archived trashed owner_ref tokens'
    ),
    TagsHandler.promises.getAllTags(userId),
  ])
  const [allProjects, tags] = results
  const formattedProjects = _formatProjects(allProjects, userId)
  const filteredProjects = _applyFilters(
    formattedProjects,
    tags,
    filters,
    userId
  )
  const pagedProjects = _sortAndPaginate(filteredProjects, sort, page)

  const projects = await _injectProjectUsers(pagedProjects)

  return {
    totalSize: filteredProjects.length,
    projects,
  }
}

/**
 * @param {AllUsersProjects} projects
 * @param {string} userId
 * @returns {FormattedProject[]}
 * @private
 */
function _formatProjects(projects, userId) {
  const {
    owned,
    review,
    readAndWrite,
    readOnly,
    tokenReadAndWrite,
    tokenReadOnly,
  } = projects

  const formattedProjects = /** @type {FormattedProject[]} **/ []
  for (const project of owned) {
    formattedProjects.push(
      _formatProjectInfo(project, 'owner', Sources.OWNER, userId)
    )
  }
  // Invite-access
  for (const project of readAndWrite) {
    formattedProjects.push(
      _formatProjectInfo(project, 'readWrite', Sources.INVITE, userId)
    )
  }
  for (const project of review) {
    formattedProjects.push(
      _formatProjectInfo(project, 'review', Sources.INVITE, userId)
    )
  }
  for (const project of readOnly) {
    formattedProjects.push(
      _formatProjectInfo(project, 'readOnly', Sources.INVITE, userId)
    )
  }
  // Token-access
  // Only add these formattedProjects if they're not already present, this gives us cascading access
  // from 'owner' => 'token-read-only'
  for (const project of tokenReadAndWrite) {
    if (!formattedProjects.some(p => p.id === project._id.toString())) {
      formattedProjects.push(
        _formatProjectInfo(project, 'readAndWrite', Sources.TOKEN, userId)
      )
    }
  }
  for (const project of tokenReadOnly) {
    if (!formattedProjects.some(p => p.id === project._id.toString())) {
      formattedProjects.push(
        _formatProjectInfo(project, 'readOnly', Sources.TOKEN, userId)
      )
    }
  }

  return formattedProjects
}

/**
 * @param {FormattedProject[]} projects
 * @param {MongoTag[]} tags
 * @param {Filters} filters
 * @param {string} userId
 * @returns {FormattedProject[]}
 * @private
 */
function _applyFilters(projects, tags, filters, userId) {
  if (!_hasActiveFilter(filters)) {
    return projects
  }
  return projects.filter(project => _matchesFilters(project, tags, filters))
}

/**
 * @param {FormattedProject[]} projects
 * @param {Partial<Sort>} sort
 * @param {Partial<Page>} page
 * @returns {FormattedProject[]}
 * @private
 */
function _sortAndPaginate(projects, sort, page) {
  if (
    (sort.by && !['lastUpdated', 'title', 'owner'].includes(sort.by)) ||
    (sort.order && !['asc', 'desc'].includes(sort.order))
  ) {
    throw new OError('Invalid sorting criteria', { sort })
  }
  const sortedProjects = _.orderBy(
    projects,
    [sort.by || 'lastUpdated'],
    [sort.order || 'desc']
  )
  // TODO handle pagination
  return sortedProjects
}

/**
 * @param {MongoProject} project
 * @param {ProjectAccessLevel} accessLevel
 * @param {Source} source
 * @param {string} userId
 * @returns {FormattedProject}
 * @private
 */
function _formatProjectInfo(project, accessLevel, source, userId) {
  const archived = ProjectHelper.isArchived(project, userId)
  // If a project is simultaneously trashed and archived, we will consider it archived but not trashed.
  const trashed = ProjectHelper.isTrashed(project, userId) && !archived
  const readOnlyTokenAccess =
    accessLevel === PrivilegeLevels.READ_ONLY && source === Sources.TOKEN

  return {
    id: project._id.toString(),
    name: project.name,
    owner_ref: readOnlyTokenAccess ? null : project.owner_ref,
    lastUpdated: project.lastUpdated,
    lastUpdatedBy: readOnlyTokenAccess ? null : project.lastUpdatedBy,
    accessLevel,
    source,
    archived,
    trashed,
  }
}

/**
 * @param {FormattedProject[]} projects
 * @returns {Promise<Project[]>}
 * @private
 */
async function _injectProjectUsers(projects) {
  const userIds = new Set()
  for (const project of projects) {
    if (project.owner_ref != null) {
      userIds.add(project.owner_ref.toString())
    }
    if (project.lastUpdatedBy != null) {
      userIds.add(project.lastUpdatedBy.toString())
    }
  }

  const projection = {
    first_name: 1,
    last_name: 1,
    email: 1,
  }
  /** @type {Record<string, UserRef>} */
  const users = {}
  for (const user of await UserGetter.promises.getUsers(userIds, projection)) {
    const userId = user._id.toString()
    users[userId] = {
      id: userId,
      email: user.email,
      firstName: user.first_name,
      lastName: user.last_name,
    }
  }

  return projects.map(project => ({
    id: project.id,
    name: project.name,
    archived: project.archived,
    trashed: project.trashed,
    accessLevel: project.accessLevel,
    source: project.source,
    lastUpdated: project.lastUpdated.toISOString(),
    lastUpdatedBy:
      project.lastUpdatedBy == null
        ? null
        : users[project.lastUpdatedBy.toString()] || null,
    owner:
      project.owner_ref == null
        ? undefined
        : users[project.owner_ref.toString()],
    owner_ref: undefined,
  }))
}

/**
 * @param {any} project
 * @param {MongoTag[]} tags
 * @param {Filters} filters
 * @private
 */
function _matchesFilters(project, tags, filters) {
  if (filters.ownedByUser && project.accessLevel !== 'owner') {
    return false
  }
  if (filters.sharedWithUser && project.accessLevel === 'owner') {
    return false
  }
  if (filters.archived && !project.archived) {
    return false
  }
  if (filters.trashed && !project.trashed) {
    return false
  }
  if (
    filters.tag &&
    !_.find(
      tags,
      tag =>
        filters.tag === tag.name && (tag.project_ids || []).includes(project.id)
    )
  ) {
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

/**
 * @param {Filters} filters
 * @returns {boolean}
 * @private
 */
function _hasActiveFilter(filters) {
  return Boolean(
    filters.ownedByUser ||
    filters.sharedWithUser ||
    filters.archived ||
    filters.trashed ||
    filters.tag === null ||
    filters.tag?.length ||
    filters.search?.length
  )
}

export default {
  getProjectsJson: expressify(getProjectsJson),
}

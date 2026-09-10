// POST /project/new/typst
//
// Creates a blank "typst" project and seeds its project files from a template
// (F2.5 empty-ish "basic" + F3.8 "article"). See `templates/build-templates.mjs`.
//
// The compiler is set to 'typst' at creation (safeCompilers allow-list, §5.2).
// This mirrors web's own `createBlankProject` path (which uses project.compiler).
import logger from '@overleaf/logger'
import { expressify } from '@overleaf/promise-utils'
import SessionManager from '../../../../app/src/Features/Authentication/SessionManager.mjs'
import AuthenticationController from '../../../../app/src/Features/Authentication/AuthenticationController.mjs'
import ProjectCreationHandler from '../../../../app/src/Features/Project/ProjectCreationHandler.mjs'
import ProjectAuditLogHandler from '../../../../app/src/Features/Project/ProjectAuditLogHandler.mjs'
import ProjectEntityUpdateHandler from '../../../../app/src/Features/Project/ProjectEntityUpdateHandler.mjs'
import { Project } from '../../../../app/src/models/Project.mjs'
import { RateLimiter } from '../../../../app/src/infrastructure/RateLimiter.mjs'
import RateLimiterMiddleware from '../../../../app/src/Features/Security/RateLimiterMiddleware.mjs'
import { parseReq, z } from '../../../../app/src/infrastructure/Validation.mjs'
import { buildTemplateFiles } from '../templates/build-templates.mjs'

const createTypstProjectRateLimiter = new RateLimiter('create-typst-project', {
  points: 20,
  duration: 60,
})

// F3.8: the modal radio sends `template` in the POST body; anything
// unrecognized falls back to 'basic' so a malformed value can never create
// an empty project. Parsed with parseReq (repo rule @overleaf/no-raw-req-access)
// rather than reading req.body directly.
const newTypstProjectSchema = z.object({
  body: z.object({
    projectName: z
      .string()
      .trim()
      .max(100)
      .optional()
      .transform(name => (name ? name : undefined)),
    template: z.string().max(50).optional(),
  }),
})

async function newTypstProject(req, res, next) {
  const currentUser = SessionManager.getSessionUser(req.session)
  if (!currentUser) {
    return next()
  }
  const { _id: userId } = currentUser
  // request-body parsing via parseReq (no raw req.body reads)
  const { body } = parseReq(req, newTypstProjectSchema)
  const projectName = body.projectName

  // template selection ('basic' default; 'article', 'example' — owner
  // 2026-09-13: Typst translation of the TeX example project). Anything
  // unrecognized falls back to 'basic' so a malformed value can never
  // create an empty project.
  const TEMPLATES_PROJECT_IDS = ['basic', 'article', 'example']
  const templateId = TEMPLATES_PROJECT_IDS.includes(body.template)
    ? body.template
    : 'basic'

  // create project with compiler 'typst' (safeCompilers allow-list, plan §5.2)
  const project = await ProjectCreationHandler.promises.createBlankProject(
    userId,
    projectName,
    { compiler: 'typst' }
  )

  // seed the project files (basic: one doc, article: main.typ + references.bib,
  // example: main.typ + sample.bib + frog.jpg binary). Doc entries go via
  // addDoc; binary assets (filePath) via addFile — the same mechanism the
  // TeX example project uses for frog.jpg. The first entry is always the
  // root doc (§3.9).
  const templateFiles = buildTemplateFiles(projectName, templateId)
  let mainDoc
  for (let i = 0; i < templateFiles.length; i++) {
    const tf = templateFiles[i]
    if (tf.filePath) {
      await ProjectEntityUpdateHandler.promises.addFile(
        project._id,
        project.rootFolder[0]._id,
        tf.name,
        tf.filePath,
        null,
        userId,
        null
      )
      continue
    }
    const { doc } = await ProjectEntityUpdateHandler.promises.addDoc(
      project._id,
      project.rootFolder[0]._id,
      tf.name,
      tf.lines,
      userId,
      null
    )
    if (i === 0) {
      mainDoc = doc
    }
  }
  const { _id: projectId } = project

  // designate the root document (compiler is already 'typst' from attributes)
  await Project.updateOne(
    { _id: projectId },
    { $set: { rootDoc_id: mainDoc._id } }
  )

  ProjectAuditLogHandler.addEntryIfManagedInBackground(
    projectId,
    'project-created',
    project.owner_ref,
    req.ip
  )
  res.json({
    project_id: projectId,
    owner_ref: project.owner_ref,
    owner: {
      first_name: currentUser.first_name,
      last_name: currentUser.last_name,
      email: currentUser.email,
      _id: userId,
    },
  })
}

const TypstRouter = {
  apply(webRouter) {
    logger.debug({}, 'Init Typst router')
    webRouter.post(
      '/project/new/typst',
      AuthenticationController.requireLogin(),
      RateLimiterMiddleware.rateLimit(createTypstProjectRateLimiter),
      expressify(newTypstProject)
    )
  },
  // exported for the mocha/vitest unit test (avoids pulling in the session
  // chain; the router itself is exercised by the acceptance F1.19 suite).
  newTypstProject,
}

export default TypstRouter

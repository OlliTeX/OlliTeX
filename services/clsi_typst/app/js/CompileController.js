// clsi_typst CompileController — mirrors clsi CompileController for the
// compile/stop/clearCache/wordcount/status routes (plan §3.3). No sync
// routes (no Typst synctex, §7.5) and no wordcountWithSync split (the
// /wordcount POST just carries a body; it is unused in the v1 flow).
import OError from '@overleaf/o-error'
import RequestParser from './RequestParser.js'
import CompileManager from './CompileManager.js'
import Settings from '@overleaf/settings'
import logger from '@overleaf/logger'
import Errors from '../../../clsi/app/js/Errors.js'
import ProjectPersistenceManager from './ProjectPersistenceManager.js'
import { parseReq, z, zz } from '@overleaf/validation-tools'
import { compileRequestBodySchema } from '../../../clsi/app/js/schemas.js'
import Metrics from '@overleaf/metrics'

let lastSuccessfulCompileTimestamp = 0

function timeSinceLastSuccessfulCompile() {
  return Date.now() - lastSuccessfulCompileTimestamp
}

// project_id is a Mongo ObjectId in the common case, but this service is
// also hit directly by clsi-perf and by v1 submission/export flows using a
// bare alphanumeric submission id; user_id, when present, is a Mongo ObjectId.
const projectOrUserParamsSchema = z.strictObject({
  project_id: zz.objectId().or(zz.submissionId()),
  user_id: zz.objectId().optional(),
})

// clsi_typst accepts 'typst' as the compiler. The shared tex schema stays
// tex-only BY DESIGN (the tex service must never be fed typst, and clsi_typst
// must never accept a latex dialect — its RequestParser is typst-only), so
// the widening happens HERE, in a derived schema: only options.compiler
// gains one member; every other field keeps the shared, strictObject-
// enforced contract.
const TYPST_VALID_COMPILERS = ['pdflatex', 'latex', 'xelatex', 'lualatex', 'typst']
const typstCompileRequestBodySchema = compileRequestBodySchema.extend({
  compile: compileRequestBodySchema.shape.compile.extend({
    options: (compileRequestBodySchema.shape.compile.shape.options.unwrap
      ? compileRequestBodySchema.shape.compile.shape.options.unwrap()
      : compileRequestBodySchema.shape.compile.shape.options).extend({
        compiler: z.enum(TYPST_VALID_COMPILERS).optional(),
      }),
  }),
})

const compileSchema = z.object({
  params: projectOrUserParamsSchema,
  body: typstCompileRequestBodySchema,
})

function compile(req, res, next) {
  const { params, body } = parseReq(req, compileSchema, { logOnly: true })
  const timer = new Metrics.Timer('compile-request')
  RequestParser.parse(body, function (error, request) {
    if (error) {
      return next(error)
    }
    timer.opts = request.metricsOpts
    request.project_id = params.project_id
    if (params.user_id != null) {
      request.user_id = params.user_id
    }
    ProjectPersistenceManager.markProjectAsJustAccessed(
      request.project_id,
      function (error) {
        if (error) {
          return next(error)
        }
        const stats = {}
        const timings = {}
        CompileManager.doCompileWithLock(
          request,
          stats,
          timings,
          (error, result) => {
            let { buildId, outputFiles, baseHistoryVersion } = result || {}
            let code, status
            if (outputFiles == null) {
              outputFiles = []
            }
            if (error instanceof Errors.AlreadyCompilingError) {
              code = 423 // Http 423 Locked
              status = 'compile-in-progress'
            } else if (error instanceof Errors.FilesOutOfSyncError) {
              code = 409 // Http 409 Conflict
              status = 'conflict'
              logger.warn(
                {
                  projectId: request.project_id,
                  userId: request.user_id,
                },
                'files out of sync, please retry'
              )
            } else if (error instanceof Errors.MissingUpdatesError) {
              code = 409
              status = 'missing-updates'
              baseHistoryVersion = error.info.baseHistoryVersion
            } else if (
              error?.code === 'EPIPE' ||
              error instanceof Errors.TooManyCompileRequestsError
            ) {
              // docker returns EPIPE when shutting down
              code = 503 // send 503 Unavailable response
              status = 'unavailable'
            } else if (error?.terminated) {
              status = 'terminated'
            } else if (error?.timedout) {
              status = 'timedout'
              logger.debug(
                { err: error, projectId: request.project_id },
                'timeout running compile'
              )
            } else if (error) {
              status = 'error'
              code = 500
              logger.error(
                { err: error, projectId: request.project_id },
                'error running compile'
              )
            } else {
              // Typst compiles always emit output.pdf + output.log; the
              // compile is a "successful compile with errors" when the PDF is
              // empty (error: lines are in output.log, surfaced to web via the
              // log-parser — plan §3.4).
              if (
                outputFiles.some(
                  file => file.path === 'output.pdf' && file.size > 0
                )
              ) {
                status = 'success'
                lastSuccessfulCompileTimestamp = Date.now()
              } else if (request.stopOnFirstError) {
                status = 'stopped-on-first-error'
              } else {
                status = 'failure'
                logger.warn(
                  { projectId: request.project_id, outputFiles },
                  'project failed to compile successfully, no output.pdf generated'
                )
              }
            }

            if (error) {
              outputFiles = error.outputFiles || []
              buildId = error.buildId
            }

            timer.done()
            res.status(code || 200).send({
              compile: {
                status,
                error: error?.message || error,
                baseHistoryVersion,
                stats,
                timings,
                buildId,
                clsiCacheShard: undefined,
                instanceType: Settings.apis.clsi.instanceType,
                zone: Settings.apis.clsi.zone,
                isSpotInstance: Settings.apis.clsi.isSpotInstance,
                outputUrlPrefix: Settings.apis.clsi.outputUrlPrefix,
                outputFiles: outputFiles.map(file => ({
                  url:
                    `${Settings.apis.clsi.downloadHost}/project/${request.project_id}` +
                    (request.user_id != null
                      ? `/user/${request.user_id}`
                      : '') +
                    `/build/${file.build}/output/${file.path}`,
                  ...file,
                })),
              },
            })
          }
        )
      }
    )
  })
}

const projectOrUserOnlyParamsSchema = z.object({
  params: projectOrUserParamsSchema,
})

function stopCompile(req, res, next) {
  const { params } = parseReq(req, projectOrUserOnlyParamsSchema, {
    logOnly: true,
  })
  const { project_id: projectId, user_id: userId } = params
  CompileManager.stopCompile(projectId, userId, function (error) {
    if (error) {
      return next(error)
    }
    res.sendStatus(204)
  })
}

function clearCache(req, res, next) {
  const { params } = parseReq(req, projectOrUserOnlyParamsSchema, {
    logOnly: true,
  })
  const { project_id: projectId, user_id: userId } = params
  CompileManager.stopCompile(projectId, userId, error => {
    if (error) return next(OError.tag(error, 'stop compile'))
    ProjectPersistenceManager.clearProject(projectId, userId, error => {
      if (error) return next(OError.tag(error, 'clear project'))
      res.sendStatus(204)
    })
  })
}

const wordcountSchema = z.object({
  params: projectOrUserParamsSchema,
  query: z.strictObject({
    file: zz.filepath().default('main.typ'),
    image: z.string().optional(),
  }),
})

async function wordcount(req, res, next) {
  const { params, query } = parseReq(req, wordcountSchema, {
    logOnly: true,
  })
  const { file, image } = query
  const { project_id: projectId, user_id: userId } = params
  logger.debug({ image, file, projectId }, 'word count request')
  // A POST /wordcount carries the project state as a compile request body
  // (same as clsi wordcountWithSync); a GET is a plain count on the last
  // synced sources.
  let request
  try {
    if (req.body != null) {
      request = await RequestParser.promises.parse(req.body)
      request.project_id = projectId
      if (userId != null) {
        request.user_id = userId
      }
    }
    const result = await CompileManager.promises.wordcount(
      projectId,
      userId,
      file,
      image,
      request
    )
    res.json({
      texcount: result,
    })
  } catch (error) {
    next(error)
  }
}

function status(req, res, next) {
  res.send('OK')
}

export default {
  compile,
  stopCompile,
  clearCache,
  wordcount,
  status,
  timeSinceLastSuccessfulCompile,
}

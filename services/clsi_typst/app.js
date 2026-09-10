// clsi_typst: a clsi-lookalike compile service for Typst (plan §3.1–3.4).
//
// Routes mirror clsi (plan §3.3) so web is agnostic which node serves a
// project. Differences from clsi:
//   - no sync/* routes (no Typst synctex, plan §7.5)
//   - no /convert/* conversion routes (LaTeX-specific)
//   - feature-gated: `COMPILE_TYPEST_ENABLED=false` (default) → 501
//
// Compile plumbing is imported directly from ../clsi (P0-approved; the
// clsi-core extraction is the follow-up P1b, see TYPST_INTEGRATION_PLAN §3.2).
import '@overleaf/metrics/initialize.js'

import express from 'express'
import logger from '@overleaf/logger'
import loggerSerializers from './app/js/LoggerSerializers.js'
import Metrics from '@overleaf/metrics'
import Settings from '@overleaf/settings'
import Errors from '../clsi/app/js/Errors.js'
import OutputCacheManager from '../clsi/app/js/OutputCacheManager.js'
import ProjectPersistenceManager from './app/js/ProjectPersistenceManager.js'
import OutputController from './app/js/OutputController.js'
import CompileController from './app/js/CompileController.js'
import { handleValidationError } from '@overleaf/validation-tools'
import net from 'node:net'
import os from 'node:os'

logger.initialize('clsi_typst')
logger.logger.serializers.clsiRequest = loggerSerializers.clsiRequest

Metrics.open_sockets.monitor(true)
Metrics.memory.monitor(logger)
Metrics.leaked_sockets.monitor(logger)

ProjectPersistenceManager.init()
OutputCacheManager.init()

const app = express()

Metrics.injectMetricsRoute(app)
app.use(Metrics.http.monitor(logger))

// Compile requests can take longer than the default two minutes.
const TIMEOUT = 630 * 1000 // 10.5 minutes - 30 seconds download allowance
app.use((req, res, next) => {
  req.setTimeout(TIMEOUT)
  res.setTimeout(TIMEOUT)
  res.removeHeader('X-Powered-By')
  next()
})

// Feature gate (plan §0 decision: off by default; 501 when disabled).
app.use((req, res, next) => {
  if (Settings.compile_typst_enabled === false) {
    return res.status(501).json({ error: 'typst compilation not enabled' })
  }
  next()
})

app.post(
  '/project/:project_id/compile',
  express.json({ limit: Settings.compileSizeLimit }),
  CompileController.compile
)
app.post('/project/:project_id/compile/stop', CompileController.stopCompile)
app.delete('/project/:project_id', CompileController.clearCache)

app.get('/project/:project_id/wordcount', CompileController.wordcount)
app.post(
  '/project/:project_id/wordcount',
  express.json({ limit: Settings.compileSizeLimit }),
  CompileController.wordcount
)
app.get('/project/:project_id/status', CompileController.status)
app.post('/project/:project_id/status', CompileController.status)

// Per-user containers
app.post(
  '/project/:project_id/user/:user_id/compile',
  express.json({ limit: Settings.compileSizeLimit }),
  CompileController.compile
)
app.post(
  '/project/:project_id/user/:user_id/compile/stop',
  CompileController.stopCompile
)
app.delete('/project/:project_id/user/:user_id', CompileController.clearCache)

app.get(
  '/project/:project_id/user/:user_id/wordcount',
  CompileController.wordcount
)
app.post(
  '/project/:project_id/user/:user_id/wordcount',
  express.json({ limit: Settings.compileSizeLimit }),
  CompileController.wordcount
)

// This needs to be before GET /project/:project_id/build/:build_id/output/*
app.get(
  '/project/:project_id/build/:build_id/output/output.zip',
  express.json(),
  OutputController.createOutputZip
)

// This needs to be before GET /project/:project_id/user/:user_id/build/:build_id/output/*
app.get(
  '/project/:project_id/user/:user_id/build/:build_id/output/output.zip',
  express.json(),
  OutputController.createOutputZip
)

app.get('/status', (req, res) => res.send('clsi_typst is alive\n'))

app.get('/health_check', (req, res) => {
  if (ProjectPersistenceManager.isAnyDiskCriticalLow()) {
    return res.status(500).json({ diskCritical: true })
  }
  return res.status(200).json({ ok: true })
})

app.use(handleValidationError)

app.use((error, req, res, next) => {
  if (error instanceof Errors.NotFoundError) {
    logger.debug({ err: error, url: req.url }, 'not found error')
    res.sendStatus(404)
  } else if (error instanceof Errors.InvalidParameter) {
    res.status(400).send(error.message)
  } else if (error.code === 'EPIPE') {
    // inspect container returns EPIPE when shutting down
    res.sendStatus(503) // send 503 Unavailable response
  } else {
    logger.error({ err: error, url: req.url }, 'server error')
    res.sendStatus(error.statusCode || 500)
  }
})

let STATE = 'up'

const loadTcpServer = net.createServer((socket) => {
  socket.on('error', function (err) {
    if (err.code === 'ECONNRESET') {
      // this always comes up, we don't know why
      return
    }
    logger.err({ err }, 'error with socket on load check')
    socket.destroy()
  })

  if (STATE === 'up' && Settings.internal.load_balancer_agent.report_load) {
    let availableWorkingCpus
    const currentLoad = os.loadavg()[0]

    if (os.cpus().length === 1) {
      availableWorkingCpus = 1
    } else {
      availableWorkingCpus = os.cpus().length - 1
    }

    const freeLoad = availableWorkingCpus - currentLoad
    let freeLoadPercentage = Math.round((freeLoad / availableWorkingCpus) * 100)
    if (ProjectPersistenceManager.isAnyDiskCriticalLow()) {
      freeLoadPercentage = 0
    }
    if (ProjectPersistenceManager.isAnyDiskLow()) {
      freeLoadPercentage = freeLoadPercentage / 2
    }

    if (
      Settings.internal.load_balancer_agent.allow_maintenance &&
      freeLoadPercentage <= 0
    ) {
      // When its 0 the server is set to drain implicitly.
      socket.write('maint, 0%\n', 'ASCII')
    } else {
      // Ready will cancel the maint state.
      socket.write(`up, ready, ${Math.max(freeLoadPercentage, 1)}%\n`, 'ASCII')
      if (freeLoadPercentage <= 0) {
        // This metric records how often we would have gone into maintenance mode.
        Metrics.inc('clsi_typst-prevented-maint')
      }
    }
    socket.end()
  } else {
    socket.write(`${STATE}\n`, 'ASCII')
    socket.end()
  }
})

const loadHttpServer = express()

loadHttpServer.post('/state/up', (req, res, next) => {
  STATE = 'up'
  logger.debug('clsi_typst set to up')
  res.sendStatus(204)
})

loadHttpServer.post('/state/down', (req, res, next) => {
  STATE = 'down'
  logger.debug('clsi_typst set to down')
  res.sendStatus(204)
})

loadHttpServer.post('/state/maint', (req, res, next) => {
  STATE = 'maint'
  logger.debug('clsi_typst set to maint')
  res.sendStatus(204)
})

const port = Settings.internal.clsi.port
const host = Settings.internal.clsi.host

const loadTcpPort = Settings.internal.load_balancer_agent.load_port
const loadHttpPort = Settings.internal.load_balancer_agent.local_port

if (import.meta.main) {
  // Called directly

  // handle uncaught exceptions when running in production
  if (Settings.catchErrors) {
    process.removeAllListeners('uncaughtException')
    process.on('uncaughtException', (error) =>
      logger.error({ err: error }, 'uncaughtException')
    )
  }

  app.listen(port, host, (error) => {
    if (error) {
      logger.fatal({ error }, `Error starting clsi_typst on ${host}:${port}`)
    } else {
      logger.debug(`clsi_typst starting up, listening on ${host}:${port}`)
    }
  })

  loadTcpServer.listen(loadTcpPort, host, (error) => {
    if (error != null) {
      throw error
    }
    logger.debug(`Load tcp agent listening on load port ${loadTcpPort}`)
  })

  loadHttpServer.listen(loadHttpPort, host, (error) => {
    if (error != null) {
      throw error
    }
    logger.debug(`Load http agent listening on load port ${loadHttpPort}`)
  })
}

export default app

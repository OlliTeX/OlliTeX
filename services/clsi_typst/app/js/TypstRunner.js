import { promisify } from 'node:util'
import Path from 'node:path'
import CommandRunner from './CommandRunner.js'
import logger from '@overleaf/logger'

const ProcessTable = {} // table of currently running jobs (docker container names)

/**
 * Builds the `sh -c` command array for a single `typst compile` run, mirroring
 * clsi LatexRunner._buildLatexCommand shape so clsi's DockerRunner can run it
 * (compiler-agnostic).
 *
 * The `exit 0` is deliberate: Typst reports error/warning lines in output.log
 * (captured via >> "$3" 2>&1); a non-zero exit would make clsi treat a
 * "successful compile with errors" as failed — same convention as clsi.
 */
function buildTypstCompileCommand(mainFile) {
  return [
    'sh',
    '-c',
    'echo "typst $(typst --version 2>/dev/null || echo unknown)" > "$3"; ' +
      'typst compile "$1" "$2" >> "$3" 2>&1; exit 0',
    '--',
    Path.join('$COMPILE_DIR', mainFile),
    'output.pdf',
    'output.log',
  ]
}

function runTypst(projectId, options, callback) {
  const { directory, mainFile, image, environment, compileGroup, stats } =
    options
  const timeout = options.timeout || 60000 // milliseconds

  logger.debug(
    {
      directory,
      timeout,
      mainFile,
      environment,
      compileGroup,
    },
    'starting typst compile'
  )

  const command = buildTypstCompileCommand(mainFile)

  const id = `${projectId}` // record running project under this id

  ProcessTable[id] = CommandRunner.run(
    projectId,
    command,
    directory,
    image,
    timeout,
    environment,
    compileGroup,
    null,
    (error, output) => {
      delete ProcessTable[id]
      if (error) {
        return callback(error)
      }
      // informational stats (clsi_compile_metrics parity)
      const errMatches = (output?.stdout || '').match(/^error: .*/gm) || []
      stats['typst-errors'] = errMatches.length
      stats['typst-compile-runs'] = 1
      callback(null, output)
    }
  )
}

function isRunning(projectId) {
  const id = `${projectId}`
  return ProcessTable[id] != null
}

function killTypst(projectId, callback) {
  const id = `${projectId}`
  logger.debug({ id }, 'killing running typst compile')
  if (!isRunning(id)) {
    logger.warn({ id }, 'no such project to kill')
    return callback()
  }
  CommandRunner.kill(ProcessTable[id], callback)
}

export default {
  isRunning,
  runTypst,
  killTypst,
  buildTypstCompileCommand,
  promises: {
    runTypst: promisify(runTypst),
    killTypst: promisify(killTypst),
  },
}

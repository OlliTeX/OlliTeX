// clsi_typst CompileManager — the typst-only compile path.
//
// Mirrors clsi CompileManager's orchestration (lock → sync resources →
// run in docker → save outputs) but with no LaTeX/synctex/tikz/draft-mode/
// latexmk branches (plan §3.2/§3.4). LatexRunner is replaced by TypstRunner;
// DockerRunner is the clsi_typst copy (Entrypoint: [], typst container names);
// everything else (ResourceWriter, OutputCacheManager, LockManager, Errors)
// is imported from ../clsi.
//
// The clsi-core library extraction (plan §3.2) is the P1b follow-up; for
// P1 the direct import approach is shipping (owner decision).
import fsPromises from 'node:fs/promises'
import Path from 'node:path'
import { callbackify } from 'node:util'
import Settings from '@overleaf/settings'
import logger from '@overleaf/logger'
import ResourceWriter from '../../../clsi/app/js/ResourceWriter.js'
import OutputFileFinder from '../../../clsi/app/js/OutputFileFinder.js'
import OutputCacheManager from '../../../clsi/app/js/OutputCacheManager.js'
import LockManager from '../../../clsi/app/js/LockManager.js'
import Errors from '../../../clsi/app/js/Errors.js'
import TypstRunner from './TypstRunner.js'
import CommandRunner from './CommandRunner.js'
import WordcountInjector from './WordcountInjector.js'
import ClsiWordText from './ClsiWordText.js'

// clsi_typst env for the compile container (plan §3.4). The `HOME=/tmp` is
// required because the typst image user (www-data/33) has no home dir; the
// package cache is kept in /tmp so a uid-33 container can write it.
const TYPST_DOCKER_ENV = {
  HOME: '/tmp',
  TYPST_PACKAGE_CACHE_PATH: '/tmp/.cache/typst',
}

function getCompileName(projectId, userId) {
  if (userId != null) {
    return `${projectId}-${userId}`
  } else {
    return projectId
  }
}

function getCompileDir(projectId, userId) {
  return Path.join(
    Settings.path.compilesDir,
    getCompileName(projectId, userId)
  )
}

function getOutputDir(projectId, userId) {
  return Path.join(
    Settings.path.outputDir,
    getCompileName(projectId, userId)
  )
}

async function doCompileWithLock(request, stats, timings) {
  const compileDir = getCompileDir(request.project_id, request.user_id)
  request.isInitialCompile =
    (await fsPromises.mkdir(compileDir, { recursive: true })) === compileDir
  await _prepareCompileDir(compileDir)
  // prevent simultaneous compiles
  const lock = LockManager.acquire(compileDir)
  try {
    return await doCompile(request, stats, timings)
  } finally {
    lock.release()
  }
}

/**
 * clsi runs as www-data (uid 33), the same uid its containers use, so its
 * compile dirs are writable in-container. clsi_typst containers use the
 * numeric uid 33 too (pandoc/typst has no www-data user, plan §3.8); make
 * the dir accessible to that uid: chown best-effort (privileged production),
 * fall back to world-writable (unprivileged local run — the P0 spike
 * convention).
 */
async function _prepareCompileDir(compileDir) {
  try {
    await fsPromises.chown(compileDir, 33, 33)
  } catch (err) {
    await fsPromises.chmod(compileDir, 0o777).catch(() => {})
  }
}

async function doCompile(request, stats, timings) {
  const { project_id: projectId, user_id: userId } = request
  const compileDir = getCompileDir(request.project_id, request.user_id)

  const e2eCompileStart = Date.now()

  const syncStart = Date.now()
  logger.debug(
    { projectId: request.project_id, userId: request.user_id },
    'syncing resources to disk'
  )
  const resourceList = await ResourceWriter.promises.syncResourcesToDisk(
    request,
    compileDir
  )
  if (resourceList.length === 0) {
    throw new Errors.NotFoundError('no resources provided to compile')
  }
  timings.sync = Date.now() - syncStart
  logger.debug(
    {
      projectId: request.project_id,
      userId: request.user_id,
      timeTaken: timings.sync,
    },
    'written files to disk'
  )

  const compileStart = Date.now()
  const compileName = getCompileName(request.project_id, request.user_id)

  const env = {
    OVERLEAF_PROJECT_ID: request.project_id,
    ...TYPST_DOCKER_ENV,
  }

  try {
    await TypstRunner.promises.runTypst(compileName, {
      directory: compileDir,
      mainFile: request.rootResourcePath,
      image: request.imageName,
      timeout: request.timeout,
      environment: env,
      compileGroup: request.compileGroup,
      stats,
      timings,
    })
  } catch (error) {
    // Save the output files (in particular output.log) so the user can read
    // the compile errors, even when the compile timed out / was terminated.
    try {
      const { outputFiles, buildId } = await _saveOutputFiles({
        request,
        compileDir,
        resourceList,
        stats,
        timings,
      })
      error.outputFiles = outputFiles
      error.buildId = buildId
    } catch (saveError) {
      logger.error(
        { err: saveError, projectId },
        'error saving output files on compile failure'
      )
    }
    throw error
  }

  timings.compile = Date.now() - compileStart

  const { outputFiles, buildId } = await _saveOutputFiles({
    request,
    compileDir,
    resourceList,
    stats,
    timings,
  })
  timings.compileE2E = Date.now() - e2eCompileStart

  return {
    outputFiles,
    buildId,
    baseHistoryVersion: request.baseHistoryVersion,
  }
}

async function _saveOutputFiles({
  request,
  compileDir,
  resourceList,
  stats,
  timings,
}) {
  const start = Date.now()
  const outputDir = getOutputDir(request.project_id, request.user_id)

  const { outputFiles: rawOutputFiles } =
    await OutputFileFinder.promises.findOutputFiles(resourceList, compileDir)

  const { buildId, outputFiles } =
    await OutputCacheManager.promises.saveOutputFiles(
      { request, stats, timings },
      rawOutputFiles,
      compileDir,
      outputDir
    )

  timings.output = Date.now() - start
  return { outputFiles, buildId }
}

async function stopCompile(projectId, userId) {
  const compileName = getCompileName(projectId, userId)
  const lock = LockManager.getExistingLock(getCompileDir(projectId, userId))
  let lockReleased
  if (lock) {
    lockReleased = lock.waitForRelease()
  } else {
    if (!TypstRunner.isRunning(compileName)) return
    logger.warn({ projectId, userId }, 'found running compile without lock')
    lockReleased = Promise.resolve()
  }
  await TypstRunner.promises.killTypst(compileName)
  await lockReleased
}

async function clearProject(projectId, userId) {
  const compileDir = getCompileDir(projectId, userId)
  await fsPromises.rm(compileDir, { force: true, recursive: true })
}

async function clearExpiredProjects(maxCacheAgeMs) {
  const now = Date.now()
  const dirs = await _findAllDirs()
  for (const dir of dirs) {
    let stats
    try {
      stats = await fsPromises.stat(dir)
    } catch (err) {
      // ignore errors checking directory
      continue
    }

    const age = now - stats.mtime
    if (age > maxCacheAgeMs) {
      await fsPromises.rm(dir, { force: true, recursive: true })
    }
  }
}

async function _findAllDirs() {
  const root = Settings.path.compilesDir
  const files = await fsPromises.readdir(root).catch(() => [])
  const allDirs = files.map(file => Path.join(root, file))
  return allDirs
}

// Word count (plan §3.6 / F3.1) — wordometer-style compile (texlyre
// approach, ported per plan §3.6):
//   0. copy the vendored wordometer (vendor/wordometer.typ, plan §3.6(a))
//      and an injected driver doc into the compile dir (user resources are
//      never mutated)
//   1. have Docker compile the driver (typst compile __clsi_wc_main.typ
//      __clsi_wc_out.pdf)
//   2. read the rendered marker back from the PDF (app/js/ClsiWordText.js,
//      pdfjs-dist — the vendored build of the lib exposes no compile-time
//      file write, so the counts are rendered, per plan §3.6)
// If the wordometer compile is unusable (e.g. broken project, a typst
// feature missing, etc.), fall back to `wc -w` over the .typ source
// (plan §9 mitigation: it is a rough *wordcount* fallback, not a
// correctness requirement).
async function wordcount(projectId, userId, file, image, request) {
  const compileDir = getCompileDir(projectId, userId)
  const environment = {
    ...Settings.clsi.docker.env,
    ...TYPST_DOCKER_ENV,
  }
  const rootResource =
    file || (request && request.rootResourcePath) || 'main.typ'

  await fsPromises.mkdir(compileDir, { recursive: true })
  await _prepareCompileDir(compileDir)

  // A POST /wordcount carries the project state as a compile request body
  // (same as clsi wordcountWithSync); a GET is a count on the last synced
  // sources.
  if (request && request.resources) {
    await ResourceWriter.promises.syncResourcesToDisk(request, compileDir)
  }

  const rootPath = Path.join(compileDir, rootResource)
  let rootExists
  try {
    await fsPromises.access(rootPath)
    rootExists = true
  } catch (err) {
    rootExists = false
  }
  if (!rootExists) {
    throw new Errors.NotFoundError(
      `no compiled state to word count: ${rootResource} not synced`
    )
  }

  try {
    await WordcountInjector.injectWordometer(compileDir, rootResource)
    // Not the 'wordcount' compile group: that group mounts /compile
    // read-only (texcount parity), but the wordometer driver has to
    // write the marker PDF.
    await CommandRunner.promises.run(
      projectId,
      [
        'typst',
        'compile',
        WordcountInjector.WC_MAIN,
        WordcountInjector.WC_OUT_PDF,
      ],
      compileDir,
      image,
      (request && request.timeout) || 60 * 1000,
      environment,
      null,
      null
    )
    const pdfPath = Path.join(compileDir, WordcountInjector.WC_OUT_PDF)
    const marker = await ClsiWordText.pdfLastPagesMarkerText(
      pdfPath,
      WordcountInjector.WORDOMETER_MARKER
    )
    if (!marker) {
      throw new Error('wordometer marker not found in output PDF')
    }
    // `#total-words` includes heading words (verified against wordometer
    // 0.1.5 in this env: headings are not subtracted), so keep that parity.
    const [totalWords, headWords, numHeadings] = marker.slice(1, 4).map(
      Number
    )
    logger.debug(
      { projectId, userId, totalWords, headWords, numHeadings },
      'word count results (wordometer)'
    )
    return {
      encode: 'utf-8',
      textWords: totalWords,
      headWords,
      outside: 0,
      headers: numHeadings,
      elements: 0,
      mathInline: 0,
      mathDisplay: 0,
      errors: 0,
      messages: '',
    }
  } catch (err) {
    logger.warn(
      { err, projectId, userId, rootResource },
      'wordometer wordcount failed; falling back to wc -w (plan §9)'
    )
    return _wordcountWcFallback(projectId, userId, rootResource, image, request)
  } finally {
    await WordcountInjector.removeArtifacts(compileDir)
  }
}

// Fallback (plan §9): `wc -w` over the .typ source in the compile directory.
// (clsi runs texcount in docker; this is intentionally less accurate for
// Typst — it counts code tokens too — and exists so word count degrades
// instead of failing.)
function _wordcountWcFallback(
  projectId,
  userId,
  file,
  image,
  request
) {
  return new Promise((resolve, reject) => {
    const compileDir = getCompileDir(projectId, userId)
    const environment = {
      ...Settings.clsi.docker.env,
      ...TYPST_DOCKER_ENV,
    }
    CommandRunner.run(
      getCompileName(projectId, userId),
      [
        'sh',
        '-c',
        `wc -w "${Path.join('$COMPILE_DIR', file)}" || echo "0 /compile/${file}"`,
      ],
      compileDir,
      image,
      (request && request.timeout) || 5 * 60 * 1000,
      environment,
      'wordcount',
      null,
      (error, output) => {
        if (error) {
          return reject(error)
        }
        const result = _parseWordcountOutput(output?.stdout || '')
        resolve(result)
      }
    )
  })
}

function _parseWordcountOutput(stdout) {
  // clsi's texcount stdout: "Words in text: N", "Words in headings: N", ...
  // clsi_typst fallback (wc -w) stdout: "<N> <file>" → map to the clsi
  // result shape (headWords/outside/etc. 0 — see _wordcountWcFallback).
  const firstNumber = (stdout.match(/\d+/) || [])[0]
  const textWords = parseInt(firstNumber, 10) || 0
  return {
    encode: 'utf-8',
    textWords,
    headWords: 0,
    outside: 0,
    headers: 0,
    elements: 0,
    mathInline: 0,
    mathDisplay: 0,
    errors: 0,
    messages: '',
  }
}

export default {
  doCompileWithLock: callbackify(doCompileWithLock),
  stopCompile: callbackify(stopCompile),
  clearProject: callbackify(clearProject),
  clearExpiredProjects: callbackify(clearExpiredProjects),
  wordcount,
  TYPST_DOCKER_ENV,
  promises: {
    doCompileWithLock,
    stopCompile,
    clearProject,
    clearExpiredProjects,
    wordcount,
  },
}

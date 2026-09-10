// clsi_typst ProjectPersistenceManager — a reduced copy of clsi's
// (plan §3.2: clsi's PPM drags in clsi CompileManager → LatexRunner →
// DockerRunner, which is the whole module graph we want to avoid).
//
// Pure filesystem persistence: compile-dir expiry (EXPIRY_TIMEOUT =
// Settings.project_cache_length_ms, same as clsi), per-project last-access
// tracking, and the disk-space gauges clsi's /health_check depends on.
import fs from 'node:fs'
import Path from 'node:path'
import Metrics from '@overleaf/metrics'
import Settings from '@overleaf/settings'
import logger from '@overleaf/logger'

const oneDay = 24 * 60 * 60 * 1000
const EXPIRY_TIMEOUT = Settings.project_cache_length_ms || oneDay * 2.5

// projectId -> timestamp mapping (same shape as clsi LAST_ACCESS).
const LAST_ACCESS = new Map()

let ANY_DISK_LOW = false
let ANY_DISK_CRITICAL_LOW = false

async function collectDiskStats() {
  const paths = [
    Settings.path.compilesDir,
    Settings.path.outputDir,
    Settings.path.clsiCacheDir,
  ]

  let anyDiskLow = false
  let anyDiskCriticalLow = false
  for (const path of paths) {
    try {
      const { blocks, bavail, bsize } = await fs.promises.statfs(path)
      const stats = {
        // Warning: these values will be wrong by a factor in Docker-for-Mac.
        // See https://github.com/docker/for-mac/issues/2136
        total: blocks * bsize, // Total size of the file system in bytes
        available: bavail * bsize, // Free space available to unprivileged users.
      }
      const diskAvailablePercent = (stats.available / stats.total) * 100
      Metrics.gauge('disk_available_percent', diskAvailablePercent, 1, { path })
      const lowDisk = diskAvailablePercent < 10
      anyDiskLow = anyDiskLow || lowDisk
      const criticalLowDisk = diskAvailablePercent < 3
      anyDiskCriticalLow = anyDiskCriticalLow || criticalLowDisk
    } catch (err) {
      logger.err({ err, path }, 'error getting disk usage')
    }
  }
  ANY_DISK_LOW = anyDiskLow
  ANY_DISK_CRITICAL_LOW = anyDiskCriticalLow
  return { anyDiskLow, anyDiskCriticalLow }
}

export const ProjectPersistenceManager = {
  EXPIRY_TIMEOUT,

  isAnyDiskLow() {
    return ANY_DISK_LOW
  },
  isAnyDiskCriticalLow() {
    return ANY_DISK_CRITICAL_LOW
  },

  init(callback) {
    fs.readdir(Settings.path.compilesDir, (err, dirs) => {
      if (err) {
        logger.warn({ err }, 'cannot get project listing')
        dirs = []
      }
      for (const projectAndUserId of dirs) {
        const projectDir = Path.join(Settings.path.compilesDir, projectAndUserId)
        fs.stat(projectDir, (err, stats) => {
          if (err) {
            // Schedule for immediate cleanup
            LAST_ACCESS.set(projectAndUserId, 0)
          } else {
            // Cleanup eventually.
            LAST_ACCESS.set(
              projectAndUserId.slice(0, 24),
              stats.mtime.getTime()
            )
          }
        })
      }
      setInterval(() => {
        collectDiskStats().catch(err => {
          logger.err({ err }, 'low level error collecting disk stats')
        })
        ProjectPersistenceManager.clearExpiredProjects(
          ProjectPersistenceManager.EXPIRY_TIMEOUT,
          err => {
            if (err) {
              logger.error({ err }, 'clearing expired projects failed')
            }
          }
        )
      }, 10 * 60 * 1000)
      collectDiskStats().catch(err => {
        logger.err({ err }, 'low level error collecting disk stats')
      })
      if (callback) callback()
    })
  },

  markProjectAsJustAccessed(projectId, callback) {
    const prev = LAST_ACCESS.get(projectId) ?? 0
    LAST_ACCESS.set(projectId, Math.max(Date.now(), prev))
    callback()
  },

  clearProject(projectId, userId, callback) {
    LAST_ACCESS.delete(projectId)
    LAST_ACCESS.delete(
      userId != null ? `${projectId}-${userId}` : projectId
    )
    ;(async () => {
      for (const uid of [undefined, userId]) {
        if (userId != null && uid === undefined) continue
        const dir = Path.join(
          Settings.path.compilesDir,
          uid != null ? `${projectId}-${uid}` : projectId
        )
        await fs.promises.rm(dir, { force: true, recursive: true })
        const outputDir = Path.join(
          Settings.path.outputDir,
          uid != null ? `${projectId}-${uid}` : projectId
        )
        await fs.promises.rm(outputDir, { force: true, recursive: true })
      }
      callback()
    })().catch(callback)
  },

  async clearExpiredProjects(maxCacheAgeMs, _callback) {
    const now = Date.now()
    const root = Settings.path.compilesDir
    const files = await fs.promises.readdir(root).catch(() => [])
    for (const file of files) {
      const dir = Path.join(root, file)
      let stats
      try {
        stats = await fs.promises.stat(dir)
      } catch (err) {
        continue
      }
      const age = now - stats.mtime
      if (age > maxCacheAgeMs) {
        await fs.promises.rm(dir, { force: true, recursive: true })
      }
    }
  },
}

export default ProjectPersistenceManager

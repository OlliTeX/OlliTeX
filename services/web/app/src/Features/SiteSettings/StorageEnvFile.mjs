/**
 * Storage section env sync (owner 2026-09-14).
 *
 * The hub's Site → Storage section stores the filestore/docstore backend
 * choice (fs vs s3/SeaweedFS) + gateway endpoint/buckets/credentials in the
 * siteSettings document. To make it effective for the *other* in-container
 * services (filestore, docstore — separate processes that read env, not the
 * web Mongo), the saved values are ALSO written to a managed env fragment:
 *
 *   /etc/overleaf/env.d/ollitex-storage.sh
 *
 * which /etc/overleaf/env.sh sources for every runit service at start
 * (the env.d loop — added 2026-09-14). Every line uses ${VAR:-value} form
 * so explicit container/compose env ALWAYS wins over the admin UI value
 * (same semantics as the existing ${OVERLEAF_APP_NAME:-OlliTeX} pattern).
 *
 * The dir pre-exists (www-data-owned; server-ce/Dockerfile) so saving from
 * the hub is a plain file write by the web service (www-data). Changes
 * apply on the next container cycle (D2 convention across this hub).
 *
 * OVERLEAF_STORAGE_ENV_FILE overrides the path (unit tests, dev boxes).
 */
import fs from 'node:fs'
import fsp from 'node:fs/promises'
import Path from 'node:path'

const DEFAULT_PATH =
  process.env.OVERLEAF_STORAGE_ENV_FILE ||
  '/etc/overleaf/env.d/ollitex-storage.sh'

const HEADER =
  '# Managed by OlliTeX admin → Site → Storage (2026-09-14). ' +
  'Do not edit by hand — change it from the hub.\n' +
  '# ${VAR:-value} form: explicit container/compose env always wins.\n'

/**
 * Build the export lines for a storage section. Empty values are skipped
 * (an empty endpoint/bucket keeps the service default). Every line is
 * ${VAR:-value} so an explicit container/compose env always wins.
 */

// shell-safe without quoting: URL-ish + bucket names
const SAFE = /^[A-Za-z0-9_/:=+.\-~]*$/

function envLine(name, value) {
  if (value === undefined || value === null) return null
  const s = String(value)
  if (s === '') return null
  if (SAFE.test(s)) {
    return `export ${name}=\${${name}:-${s}}`
  }
  // quote + escape embedded single quotes
  const q = s.replace(/'/g, "'\\''")
  return `export ${name}=\${${name}:-'${q}'}`
}

export function buildStorageEnvLines(section) {
  const lines = []
  const add = (name, value) => {
    const l = envLine(name, value)
    if (l) lines.push(l)
  }
  const backend = section.backend === 's3' ? 's3' : section.backend === 'fs' ? 'fs' : ''

  // ----- filestore (CE + Go services) -----
  if (backend) add('OVERLEAF_FILESTORE_BACKEND', backend)
  add('OVERLEAF_FILESTORE_S3_ENDPOINT', section.s3Endpoint)
  add('OVERLEAF_FILESTORE_S3_ACCESS_KEY_ID', section.s3AccessKeyId)
  add('OVERLEAF_FILESTORE_S3_SECRET_ACCESS_KEY', section.s3Secret)
  add('OVERLEAF_FILESTORE_TEMPLATE_FILES_BUCKET_NAME', section.templateFilesBucket)
  add('OVERLEAF_HISTORY_PROJECT_BLOBS_BUCKET', section.projectBlobsBucket)
  add('OVERLEAF_HISTORY_BLOBS_BUCKET', section.globalBlobsBucket)

  // ----- docstore (Node + Go docstore config) -----
  if (backend) add('BACKEND', backend)
  add('BUCKET_NAME', section.docstoreArchiveBucket)
  add('AWS_S3_ENDPOINT', section.s3Endpoint)
  add('AWS_ACCESS_KEY_ID', section.s3AccessKeyId)
  add('AWS_SECRET_ACCESS_KEY', section.s3Secret)

  return lines
}

/** Render the full managed file content for a section. */
export function renderStorageEnvFile(section) {
  return HEADER + buildStorageEnvLines(section).join('\n') + '\n'
}

/** Parse a managed file back into section-ish values (best effort). */
export function parseStorageEnvFile(content) {
  const out = {}
  const re = /^export\s+([A-Z][A-Z0-9_]*)=\$\{[A-Z][A-Z0-9_]*:-([^}]*)\}/gm
  let m
  const map = {
    OVERLEAF_FILESTORE_BACKEND: 'backend',
    OVERLEAF_FILESTORE_S3_ENDPOINT: 's3Endpoint',
    OVERLEAF_FILESTORE_S3_ACCESS_KEY_ID: 's3AccessKeyId',
    OVERLEAF_FILESTORE_TEMPLATE_FILES_BUCKET_NAME: 'templateFilesBucket',
    OVERLEAF_HISTORY_PROJECT_BLOBS_BUCKET: 'projectBlobsBucket',
    OVERLEAF_HISTORY_BLOBS_BUCKET: 'globalBlobsBucket',
    BUCKET_NAME: 'docstoreArchiveBucket',
  }
  while ((m = re.exec(content)) !== null) {
    const k = map[m[1]]
    if (k) out[k] = m[2]
  }
  return out
}

export function storageEnvPath() {
  return DEFAULT_PATH
}

/**
 * Write the managed fragment (atomic temp+rename in the same dir).
 * Throws with a clear message when the dir is missing/unwritable so the
 * controller can answer 500 with a helpful hint (image too old → rebuild).
 */
export async function writeStorageEnv(section) {
  const target = DEFAULT_PATH
  const dir = Path.dirname(target)
  let st
  try {
    st = fs.statSync(dir)
  } catch {
    throw new Error(
      `Storage: env dir ${dir} is missing — the image needs the env.d support ` +
      '(rebuild via make image; this build predates the env.d loop)'
    )
  }
  if (!st.isDirectory()) throw new Error(`Storage: ${dir} is not a directory`)
  await fsp.mkdir(dir, { recursive: true }).catch(() => {})
  const tmp = `${target}.tmp-${process.pid}-${Date.now()}`
  try {
    await fsp.writeFile(tmp, renderStorageEnvFile(section), { mode: 0o644 })
    await fsp.rename(tmp, target)
  } catch (err) {
    await fsp.unlink(tmp).catch(() => {})
    if (err && (err.code === 'EPERM' || err.code === 'EACCES')) {
      throw new Error(
        `Storage: cannot write ${target} (permissions — the dir must be writable ` +
        'by the web service user www-data; see server-ce/Dockerfile env.d setup)'
      )
    }
    throw err
  }
  return target
}

/** Read the current managed fragment back (null when absent). */
export async function readStorageEnv() {
  try {
    const content = fs.readFileSync(DEFAULT_PATH, 'utf8')
    return { path: DEFAULT_PATH, content, section: parseStorageEnvFile(content) }
  } catch {
    return null
  }
}

/** Remove the managed fragment (admin cleared the section). */
export async function removeStorageEnv() {
  try {
    await fsp.unlink(DEFAULT_PATH)
    return true
  } catch {
    return false
  }
}

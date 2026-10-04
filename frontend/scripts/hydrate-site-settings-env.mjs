/**
 * Container boot: admin-managed site settings → process env (R9 §7.2/§7.4).
 *
 * Run by images/main-amd64/init_scripts/950_hydrate_site_settings_env.sh
 * BEFORE the runit services start, so every service that sources
 * /etc/overleaf/env.sh sees stored admin values override compose env.
 * Prints `export KEY=VALUE` lines on stdout (the shell script keeps them
 * between managed markers). No stored doc ⇒ no output.
 *
 * 2026-10-05 (owner directive: no junk/ in the image): this script lived in
 * the retired junk/services-web oracle tree and imported its app
 * infrastructure. It is now self-contained in the frontend workspace:
 *  - mongo URL from env (OVERLEAF_MONGO_URL / MONGO_URL / MONGO_HOST+PORT),
 *  - the secret cipher is the SAME @overleaf/access-token-encryptor
 *    layout/label the Go site-settings cipher and the old SecretCipher.mjs
 *    used (compatibility with stored values).
 * Never throws to the shell (the script tolerates failure — compose env
 * then stands). Terminates after printing (no lingering db handle).
 */
import mongodb from 'mongodb'
import fs from 'node:fs'
import Path from 'node:path'
import AccessTokenEncryptorClass from '@overleaf/access-token-encryptor'

const b = (v) => (v ? 'true' : 'false')
const q = (v) => `"${String(v ?? '').replace(/(["\\$'!])/g, '\\$1')}"`

// ---- mongo url -------------------------------------------------------------
function mongoUrl () {
  const u =
    process.env.OVERLEAF_MONGO_URL ||
    process.env.MONGO_URL ||
    process.env.MONGO_CONNECTION_STRING
  if (u) return u
  const host = process.env.MONGO_HOST || process.env.MONGOHOST || 'mongo'
  const port = process.env.MONGO_PORT || 27017
  const db = process.env.MONGO_DB || process.env.MONGO_DBNAME || 'sharelatex'
  const rs = process.env.MONGO_REPLICA_SET || process.env.MONGO_REPLSET
  const user = process.env.MONGO_USERNAME
  const pass = process.env.MONGO_PASSWORD
  let url = `mongodb://${host}:${port}/${db}`
  if (rs) url += `?replicaSet=${rs}`
  if (user && pass) {
    url = url.replace(
      `mongodb://`,
      `mongodb://${encodeURIComponent(user)}:${encodeURIComponent(pass)}@`
    )
  }
  return url
}

// ---- cipher (same family/label as the Go site-settings + legacy helpers) ----
let encryptorInstance = null
function getEncryptor () {
  if (encryptorInstance) return encryptorInstance
  const CIPHER_FILE =
    process.env.SITE_SETTINGS_CIPHER_FILE ||
    process.env.TOKEN_CIPHER_FILE ||
    '/var/lib/overleaf/data/.token-cipher.json'
  const CIPHER_LABEL =
    process.env.SITE_SETTINGS_CIPHER_LABEL ||
    process.env.TOKEN_CIPHER_LABEL ||
    'OL_CEP-v3'
  let data = null
  if (process.env.TOKEN_CIPHER_PASSWORD) {
    data = {
      cipherLabel: CIPHER_LABEL,
      cipherPasswords: { [CIPHER_LABEL]: process.env.TOKEN_CIPHER_PASSWORD },
    }
  } else {
    try {
      data = JSON.parse(fs.readFileSync(CIPHER_FILE, 'utf8'))
    } catch (err) {
      if (err.code !== 'ENOENT') throw err
      // No cipher file and no env password: a genuine failure when a stored
      // secret must be decrypted. (Fresh boots without stored secrets never
      // reach this path.)
      throw new Error(
        `site-settings cipher file missing: ${CIPHER_FILE} (set TOKEN_CIPHER_PASSWORD or SITE_SETTINGS_CIPHER_FILE)`
      )
    }
  }
  encryptorInstance = new AccessTokenEncryptorClass(data)
  return encryptorInstance
}
async function decryptText (value) {
  if (typeof value !== 'string' || value === '') return ''
  const body = value.startsWith('ss::') ? value.slice(4) : value
  return await getEncryptor().promises.decryptToJson(body)
}

// ---- hydrate ---------------------------------------------------------------
const lines = []
const add = (name, value) => {
  if (value === undefined || value === null || value === '') return
  lines.push(`export ${name}=${q(value)}`)
}

async function main () {
  const url = mongoUrl()
  const client = new mongodb.MongoClient(url, { serverSelectionTimeoutMS: 8000 })
  let doc = null
  try {
    await client.connect()
    const db = client.db()
    doc = await db.collection('siteSettings').findOne({ _id: 'global' })
  } catch (err) {
    console.error('hydrate: site_settings read failed:', err.message)
    process.exitCode = 1
  } finally {
    try { await client.close() } catch (_) {}
  }
  if (!doc) {
    // stored site settings absent — compose env stands (normal in fresh envs)
    return
  }
  const section = (name) => {
    const s = doc[name]
    return s && typeof s === 'object' ? s : null
  }
  const sec = async (name, secretFields = []) => {
    const s = section(name)
    if (!s) return null
    for (const f of secretFields) {
      if (s[f] && typeof s[f] === 'string') {
        try { s[f] = await decryptText(s[f]) }
        catch (err) {
          console.error(`hydrate: decrypt ${name}.${f} failed:`, err.message)
          s[f] = ''
        }
      }
    }
    return s
  }

  const sc = await sec('sandboxed-compiles')
  if (sc) {
    const images = Array.isArray(sc.images) ? sc.images : []
    const enabled = !!sc.enabled
    add('SANDBOXED_COMPILES', b(enabled))
    add('SANDBOXED_COMPILES_SIBLING_CONTAINERS', b(enabled))
    add('SIBLING_CONTAINERS_ENABLED', b(enabled || !!sc.dockerRunner))
    add('DOCKER_RUNNER', b(enabled || !!sc.dockerRunner))
    add('SANDBOXED_COMPILES_HOST_DIR', sc.hostDir)
    add('COMPILES_HOST_DIR', sc.hostDir)
    add('DOCKER_SOCKET_PATH', sc.socketPath)
    add('TEX_COMPILER_EXTRA_FLAGS', sc.extraFlags)
    add('TEXLIVE_IMAGE_USER', sc.imageUser)
    add(
      'ALL_TEX_LIVE_DOCKER_IMAGES',
      images.map(r => r && r.image).filter(Boolean).join(',')
    )
    add(
      'ALL_TEX_LIVE_DOCKER_IMAGE_NAMES',
      images.map(r => ((r && (r.name || r.image)) || '').trim()).join(',')
    )
    add('TEX_LIVE_DOCKER_IMAGE', sc.defaultImage || ((images[0] && images[0].image) || ''))
  }

  const git = await sec('git-integration')
  if (git) {
    add('GIT_BRIDGE_ENABLED', b(git.enabled))
    add('GIT_BRIDGE_HOST', git.host)
    add('GIT_BRIDGE_PORT', git.port)
  }

  const gh = await sec('github-sync', ['clientSecret'])
  if (gh) {
    add('GITHUB_SYNC_ENABLED', b(gh.enabled))
    add('GITHUB_SYNC_CLIENT_ID', gh.clientId || gh.clientID)
    add('GITHUB_SYNC_CLIENT_SECRET', gh.clientSecret)
    add('GITHUB_TOKEN_CIPHER_FILE', gh.cipherFile)
    add('GITHUB_TOKEN_CIPHER_LABEL', gh.cipherLabel)
  }

  const email = await sec('email', ['pass', 'sesSecret'])
  if (email) {
    add('EMAIL_CONFIRMATION_DISABLED', b(email.skipConfirmation))
    add('OVERLEAF_EMAIL_FROM_ADDRESS', email.fromAddress)
    add('OVERLEAF_EMAIL_REPLY_TO', email.replyTo)
    add('OVERLEAF_EMAIL_DRIVER', email.driver || 'smtp')
    add('OVERLEAF_EMAIL_SMTP_HOST', email.host)
    add('OVERLEAF_EMAIL_SMTP_PORT', email.port)
    add('OVERLEAF_EMAIL_SMTP_SECURE', b(email.secure))
    add('OVERLEAF_EMAIL_SMTP_IGNORE_TLS', b(email.ignoreTLS))
    add('OVERLEAF_EMAIL_SMTP_NAME', email.name)
    add('OVERLEAF_EMAIL_SMTP_USER', email.user)
    add('OVERLEAF_EMAIL_SMTP_PASS', email.pass)
    add('OVERLEAF_EMAIL_AWS_SES_ACCESS_KEY_ID', email.accessKeyId)
    add('OVERLEAF_EMAIL_AWS_SES_SECRET_KEY', email.sesSecret)
    add('OVERLEAF_EMAIL_AWS_SES_REGION', email.sesRegion)
  }

  const lft = await sec('linked-file-types')
  if (lft) {
    const types = Array.isArray(lft.enabledTypes) ? lft.enabledTypes : []
    const merged = ['project_file', 'project_output_file']
    for (const t of types) if (!merged.includes(t)) merged.push(t)
    add('ENABLED_LINKED_FILE_TYPES', merged.join(','))
  }

  const pandoc = await sec('pandoc')
  if (pandoc) {
    add('ENABLE_PANDOC_CONVERSIONS', b(pandoc.enabled))
    add('PANDOC_IMAGE', pandoc.image)
  }

  const saml = await sec('saml')
  if (saml) {
    add('SAML_ENABLED', b(saml.enabled))
    add('SAML_SP_ENTITY_ID', saml.spEntityId)
    add('SAML_IDP_ENTITY_ID', saml.idpEntityId)
    add('SAML_IDP_SSO_URL', saml.ssoUrl)
    add('SAML_IDP_SLO_URL', saml.sloUrl)
  }

  const ldap = await sec('ldap', ['adminPassword'])
  if (ldap) {
    add('LDAP_ENABLED', b(ldap.enabled))
    add('LDAP_URL', ldap.url)
    add('LDAP_BIND_DN', ldap.bindDn)
    add('LDAP_ADMIN_PASSWORD', ldap.adminPassword)
    add('LDAP_SEARCH_BASE', ldap.searchBase)
  }

  for (const line of lines) console.log(line)
  process.exitCode = 0
}

main().catch(err => {
  console.error('hydrate: failure:', err.message)
  process.exitCode = 1
})

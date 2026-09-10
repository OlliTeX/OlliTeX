const Path = require('node:path')
const os = require('node:os')
const fs = require('node:fs')

const isSpotInstance = process.env.PREEMPTIBLE === 'TRUE'
const CLSI_TYPST_SERVER_ID = os.hostname().replace('-ctr', '')
const LISTEN_HOST = process.env.LISTEN_ADDRESS || '127.0.0.1'
const LISTEN_PORT = parseInt(process.env.CLSI_TYPST_PORT, 10) || 3014

module.exports = {
  // Feature gate (TYPST_INTEGRATION_PLAN §0): clsi_typst is ENABLED by
  // default (OWNER DECISION 2026-09-10: flag ON day one, parity with the
  // web default `COMPILE_TYPEST_ENABLED !== 'false'`). Set
  // COMPILE_TYPEST_ENABLED=false in env for the rollback path: every route
  // then returns 501 ('typst compilation not enabled'), which web surfaces
  // as a clean compile-unavailable state.
  compile_typst_enabled: process.env.COMPILE_TYPEST_ENABLED !== 'false',

  compileSizeLimit: process.env.COMPILE_SIZE_LIMIT || '7mb',

  processLifespanLimitMs:
    parseInt(process.env.PROCESS_LIFE_SPAN_LIMIT_MS) || 60 * 60 * 24 * 1000 * 2,

  catchErrors: process.env.CATCH_ERRORS === 'true',

  path: {
    compilesDir:
      process.env.CLSI_TYPST_COMPILES_PATH ||
      Path.resolve(__dirname, '../compiles'),
    outputDir:
      process.env.CLSI_TYPST_OUTPUT_PATH || Path.resolve(__dirname, '../output'),
    clsiCacheDir:
      process.env.CLSI_TYPST_CACHE_PATH || Path.resolve(__dirname, '../cache'),
    uploadFolder:
      process.env.CLSI_TYPST_UPLOAD_PATH ||
      Path.resolve(__dirname, '../uploads'),
    synctexBaseDir(projectId) {
      return Path.join(this.compilesDir, projectId)
    },
  },

  maxUploadSize: 50 * 1024 * 1024,
  preciousFilePattern: process.env.PRECIOUS_FILE_PATTERN || '',

  internal: {
    clsi: {
      port: LISTEN_PORT,
      host: LISTEN_HOST,
    },

    load_balancer_agent: {
      report_load: process.env.LOAD_BALANCER_AGENT_REPORT_LOAD !== 'false',
      load_port: parseInt(process.env.CLSI_TYPST_LOAD_PORT, 10) || 3046,
      local_port: parseInt(process.env.CLSI_TYPST_LOCAL_PORT, 10) || 3047,
      allow_maintenance:
        (
          process.env.LOAD_BALANCER_AGENT_ALLOW_MAINTENANCE ?? ''
        ).toLowerCase() !== 'false',
    },
  },
  apis: {
    clsi: {
      // Internal requests (used by tests only at the time of writing).
      url: `http://${process.env.CLSI_TYPST_HOST || LISTEN_HOST}:${LISTEN_PORT}`,
      // External url prefix for output files, e.g. for requests via load-balancers.
      outputUrlPrefix: `${process.env.ZONE ? `/zone/${process.env.ZONE}` : ''}`,
      clsiServerId: process.env.CLSI_TYPST_SERVER_ID || CLSI_TYPST_SERVER_ID,
      instanceType: process.env.INSTANCE_TYPE,
      zone: process.env.ZONE,
      isSpotInstance,

      downloadHost: process.env.DOWNLOAD_HOST || 'http://localhost:8080',
    },
    clsiPerf: {
      host: `${process.env.CLSI_PERF_HOST || '127.0.0.1'}:${
        process.env.CLSI_PERF_PORT || '3043'
      }`,
    },
    clsiCache: {
      enabled: !!process.env.CLSI_CACHE_INSTANCES,
      shards: JSON.parse(process.env.CLSI_CACHE_INSTANCES || '[]').filter(
        ({ zone, readOnly }) => zone === process.env.ZONE && !readOnly
      ),
      currentShards: parseInt(process.env.CLSI_CACHE_CURRENT_SHARDS, 10),
      desiredShards: parseInt(process.env.CLSI_CACHE_DESIRED_SHARDS, 10),
      reshardFrom: new Date(process.env.CLSI_CACHE_RESHARD_FROM),
      reshardUntil: new Date(process.env.CLSI_CACHE_RESHARD_UNTIL),
    },
    filestore: {
      url:
        process.env.FILESTORE_DOMAIN_OVERRIDE ||
        `http://${process.env.FILESTORE_HOST || '127.0.0.1'}:3009`,
    },
  },

  smokeTest: process.env.SMOKE_TEST || false,
  project_cache_length_ms: 1000 * 60 * 60 * 24,
  parallelFileDownloads:
    parseInt(process.env.FILESTORE_PARALLEL_FILE_DOWNLOADS, 10) || 1,
  filestoreDomainOveride: process.env.FILESTORE_DOMAIN_OVERRIDE,
  texliveImageNameOveride: process.env.TEX_LIVE_DOCKER_IMAGE_ROOT_4TYPST,
  texliveOpenoutAny: process.env.TEXLIVE_OPENOUT_ANY,
  texliveMaxPrintLine: process.env.TEXLIVE_MAX_PRINT_LINE,
  enablePdfCaching: process.env.ENABLE_PDF_CACHING === 'true',
  enablePdfCachingDark: process.env.ENABLE_PDF_CACHING_DARK === 'true',
  pdfCachingMinChunkSize:
    parseInt(process.env.PDF_CACHING_MIN_CHUNK_SIZE, 10) || 1024,
  pdfCachingMaxProcessingTime:
    parseInt(process.env.PDF_CACHING_MAX_PROCESSING_TIME, 10) || 10 * 1000,
  pdfCachingEnableWorkerPool: process.env.PDF_CACHING_ENABLE_WORKER_POOL === 'true',
  pdfCachingWorkerPoolSize:
    parseInt(process.env.PDF_CACHING_WORKER_POOL_SIZE, 10) || 4,
  pdfCachingWorkerPoolBackLogLimit:
    parseInt(process.env.PDF_CACHING_WORKER_POOL_BACKLOG_LIMIT, 10) || 40,
  compileConcurrencyLimit: isSpotInstance ? 32 : 64,
  performanceLogSamplingPercentage:
    parseFloat(process.env.CLSI_PERFORMANCE_LOG_SAMPLING, 10) || 0,
}

if (process.env.ALLOWED_COMPILE_GROUPS) {
  try {
    module.exports.allowedCompileGroups =
      process.env.ALLOWED_COMPILE_GROUPS.split(' ')
  } catch (error) {
    console.error(error, 'could not apply allowed compile group setting')
    process.exit(1)
  }
}

if ((process.env.DOCKER_RUNNER || process.env.SANDBOXED_COMPILES) === 'true') {
  if (
    !fs.existsSync(Path.join(__dirname, '..', 'app', 'js', 'DockerRunner.mjs')) // 2026-09 (K, owner): 6.3.0 ships the runner as ESM (DockerRunner.mjs)
  ) {
    console.error(
      'Sandboxed compiles are only available with Overleaf Server Pro. Compare Server Pro with Community Edition here: https://docs.overleaf.com/on-premises/welcome/server-pro-vs.-community-edition'
    )
    process.exit(1)
  }

  module.exports.clsi = {
    dockerRunner: true,
    docker: {
      runtime: process.env.DOCKER_RUNTIME,
      image:
        process.env.TYPST_IMAGE ||
        process.env.TYPST_DOCKER_IMAGE ||
        (
          process.env.ALL_TYPST_DOCKER_IMAGES ||
          // OWNER DECISION 2026-09-10: current stable typst 0.15.1, digest
          // pinned (see TYPST_INTEGRATION_PLAN.md §5.3 / §8).
          'pandoc/typst:latest-alpine@sha256:ae9dfa3c58cae72d363484442993b761ff4bc30202ec12823bc1e59fa952c892'
        ).split(',')[0].trim(),
      env: {
        HOME: '/tmp',
        CLSI: 1,
      },
      socketPath: '/var/run/docker.sock',
      // pandoc/typst has no www-data; run as numeric uid/gid 33 (P0-verified).
      user: process.env.TYPST_IMAGE_USER || '33:33',
    },
    optimiseInDocker: true,
    expireProjectAfterIdleMs: 24 * 60 * 60 * 1000,
    checkProjectsIntervalMs: 10 * 60 * 1000,
  }

  try {
    // Override individual docker settings using path-based keys, e.g.:
    // compileGroupDockerConfigs = {
    //    priority: { 'HostConfig.CpuShares': 100 }
    //    beta: { 'dotted.path.here', 'value'}
    // }
    const compileGroupConfig = JSON.parse(
      process.env.COMPILE_GROUP_DOCKER_CONFIGS || '{}'
    )
    // Automatically clean up wordcount containers
    const defaultCompileGroupConfig = {
      wordcount: { 'HostConfig.AutoRemove': true },
    }
    module.exports.clsi.docker.compileGroupConfig = Object.assign(
      defaultCompileGroupConfig,
      compileGroupConfig
    )
  } catch (error) {
    console.error(error, 'could not apply compile group docker configs')
    process.exit(1)
  }

  let seccompProfilePath
  try {
    // clsi_typst ships its own copy of the seccomp profile (owner verified
    // during P0 that it loads and Typst compiles under it — plan §3.8).
    seccompProfilePath = Path.resolve(__dirname, '../seccomp/clsi-profile.json')
    module.exports.clsi.docker.seccomp_profile =
      process.env.SECCOMP_PROFILE ||
      JSON.stringify(
        JSON.parse(require('node:fs').readFileSync(seccompProfilePath))
      )
  } catch (error) {
    console.error(
      error,
      `could not load seccomp profile from ${seccompProfilePath}`
    )
    process.exit(1)
  }

  if (process.env.APPARMOR_PROFILE) {
    try {
      module.exports.clsi.docker.apparmor_profile =
        process.env.APPARMOR_PROFILE
    } catch (error) {
      console.error(error, 'could not apply apparmor profile setting')
      process.exit(1)
    }
  }

  if (process.env.NEW_APPARMOR_PROFILE) {
    try {
      module.exports.clsi.docker.new_apparmor_profile =
        process.env.NEW_APPARMOR_PROFILE
    } catch (error) {
      console.error(error, 'could not apply apparmor profile setting')
      process.exit(1)
    }
  }

  if (process.env.ALLOWED_IMAGES) {
    try {
      module.exports.clsi.docker.allowedImages =
        process.env.ALLOWED_IMAGES.split(' ')
    } catch (error) {
      console.error(error, 'could not apply allowed images setting')
      process.exit(1)
    }
  }

  module.exports.path.synctexBaseDir = () => '/compile'

  module.exports.path.sandboxedCompilesHostDirCompiles =
    process.env.SANDBOXED_COMPILES_HOST_DIR_COMPILES ||
    process.env.SANDBOXED_COMPILES_HOST_DIR ||
    process.env.COMPILES_HOST_DIR
  if (!module.exports.path.sandboxedCompilesHostDirCompiles) {
    throw new Error(
      'SANDBOXED_COMPILES enabled, but SANDBOXED_COMPILES_HOST_DIR_COMPILES not set'
    )
  }

  // Host path of the clsi cache dir, so that the png2pdf conversion container
  // (a sibling container started via the docker socket) can bind-mount a
  // project's cache dir and convert downloaded PNGs in place.
  module.exports.path.sandboxedCompilesHostDirCache =
    process.env.SANDBOXED_COMPILES_HOST_DIR_CACHE

  module.exports.path.sandboxedCompilesHostDirOutput =
    process.env.SANDBOXED_COMPILES_HOST_DIR_OUTPUT ||
    process.env.OUTPUT_HOST_DIR
  if (!module.exports.path.sandboxedCompilesHostDirOutput) {
    // TODO(das7pad): Enforce in a future major version of Server Pro.
    // throw new Error(
    //   'SANDBOXED_COMPILES enabled, but SANDBOXED_COMPILES_HOST_DIR_OUTPUT not set'
    // )
  }
}

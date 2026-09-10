# clsi_typst acceptance test environment (local runs: `yarn test:acceptance:local`
# sources this before mocha; the CI/compose setup provides the same env).
#
# SANDBOXED_COMPILES=true → app/js/CommandRunner.js picks DockerRunner.mjs and
# Settings.clsi.docker.* (image/user/seccomp/allowedImages) exists. The app-side
# compile/output/cache dirs and the host dirs the compile containers bind-mount
# are the same local directories for a single-host local run (the P0 spike
# convention).
export SANDBOXED_COMPILES=true
export TYPST_DOCKER_IMAGE="${TYPST_DOCKER_IMAGE:-pandoc/typst:3-alpine}"
export COMPILE_TYPEST_ENABLED="${COMPILE_TYPEST_ENABLED:-true}"
export CLSI_TYPST_COMPILES_PATH="${CLSI_TYPST_COMPILES_PATH:-$PWD/compiles}"
export CLSI_TYPST_OUTPUT_PATH="${CLSI_TYPST_OUTPUT_PATH:-$PWD/output}"
export CLSI_TYPST_CACHE_PATH="${CLSI_TYPST_CACHE_PATH:-$PWD/cache}"
export SANDBOXED_COMPILES_HOST_DIR_COMPILES="${SANDBOXED_COMPILES_HOST_DIR_COMPILES:-$PWD/compiles}"
export SANDBOXED_COMPILES_HOST_DIR_OUTPUT="${SANDBOXED_COMPILES_HOST_DIR_OUTPUT:-$PWD/output}"
export SANDBOXED_COMPILES_HOST_DIR_CACHE="${SANDBOXED_COMPILES_HOST_DIR_CACHE:-$PWD/cache}"
# clsi parity: ALLOWED_IMAGES — the default compile image must be allowed.
export ALLOWED_IMAGES="${ALLOWED_IMAGES:-$TYPST_DOCKER_IMAGE}"

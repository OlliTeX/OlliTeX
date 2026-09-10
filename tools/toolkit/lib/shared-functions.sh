# shellcheck shell=bash
# shellcheck disable=SC2034
# shellcheck source-path=..
#
# OlliTeX Toolkit — shared shell functions.
# Image versions: ONE file, lib/images.env (owner decision 2026-09-13).

function source_image_versions() {
  # lib/images.env holds the default image versions (single source of
  # truth). User files (overleaf.rc / variables.env) are sourced AFTER,
  # so user overrides win.
  if [[ -f "$TOOLKIT_ROOT/lib/images.env" ]]; then
    # shellcheck source=/dev/null
    source "$TOOLKIT_ROOT/lib/images.env"
  fi
}

function read_config() {
  source_image_versions
  source "$TOOLKIT_ROOT/lib/default.rc"
  # shellcheck source=/dev/null
  source "$TOOLKIT_ROOT/config/overleaf.rc"

  # Defaults for anything the user did not set.
  : "${OLLITEX_IMAGE:=sharelatex/sharelatex:$IMAGE_VERSION}"
  export OLLITEX_IMAGE
  IMAGE="$OLLITEX_IMAGE"
  export IMAGE

  : "${MONGO_DOCKER_IMAGE:=${MONGO_IMAGE:-mongo:8.3}}"
  export MONGO_DOCKER_IMAGE
  : "${REDIS_IMAGE:=redis:8.6-alpine}"
  export REDIS_IMAGE
  : "${NGINX_IMAGE:=nginx:1.30-alpine}"
  export NGINX_IMAGE
}

function read_image_version() {
  IMAGE_VERSION="$(head -n 1 "$TOOLKIT_ROOT/config/version")"
  if [[ ! "$IMAGE_VERSION" =~ ^([0-9]+)\.([0-9]+)\.([0-9])+(-RC[0-9]*)?(-with-texlive-full)?$ ]]; then
    echo "ERROR: invalid version '${IMAGE_VERSION}'"
    exit 1
  fi
  IMAGE_VERSION_MAJOR=${BASH_REMATCH[1]}
  IMAGE_VERSION_MINOR=${BASH_REMATCH[2]}
  IMAGE_VERSION_PATCH=${BASH_REMATCH[3]}
}

function read_mongo_version() {
  # MONGO_DOCKER_IMAGE comes from lib/images.env (default mongo:8.3).
  local mongo_image="${MONGO_DOCKER_IMAGE:-${MONGO_IMAGE:-}}"
  if [[ -z "$mongo_image" ]]; then
    echo "ERROR: no mongo image configured (set MONGO_IMAGE or MONGO_DOCKER_IMAGE)"
    exit 1
  fi
  MONGO_DOCKER_IMAGE="$mongo_image"
  # mongosh for mongo >= 6 (the 8.x line uses mongosh)
  if [[ "$MONGO_DOCKER_IMAGE" =~ ^mongo:([0-9]+) ]]; then
    local major=${BASH_REMATCH[1]}
    if [[ "$major" -ge 6 ]]; then
      MONGOSH="mongosh"
    else
      MONGOSH="mongo"
    fi
  else
    MONGOSH="mongosh"
  fi
  export MONGO_DOCKER_IMAGE MONGOSH
}

# Kept for air-gapped / retracted-version safety (upstream 5.0.1 bug).
function check_retracted_version() {
  if [[ "${OVERLEAF_SKIP_RETRACTION_CHECK:-null}" == "$IMAGE_VERSION" ]]; then
    return
  fi
  local version="$IMAGE_VERSION_MAJOR.$IMAGE_VERSION_MINOR.$IMAGE_VERSION_PATCH"
  if [[ "$version" == "5.0.1" ]]; then
    echo "-------------------------------------------------------"
    echo "---------------------  WARNING  -----------------------"
    echo "-------------------------------------------------------"
    echo "  You are currently using a retracted version, $version."
    echo ""
    echo "  A known bug in a database migration can cause data loss in"
    echo "  the history system. Upgrade before continuing."
    echo "-------------------------------------------------------"
    exit 1
  fi
}

prompt() {
    read -p "$1 (y/n): " choice
    if [[ ! "$choice" =~ [Yy] ]]; then
        echo "Exiting."
        exit 1
    fi
}

function read_variable() {
  local name=$1
  grep -E "^$name=" "$TOOLKIT_ROOT/config/variables.env" \
  | sed -r "s/^$name=([\"']?)(.+)\1\$/\2/"
}

function read_configuration() {
  local name=$1
  grep -E "^$name=" "$TOOLKIT_ROOT/config/overleaf.rc" \
  | sed -r "s/^$name=([\"']?)(.+)\1\$/\2/"
}

# Compatibility guard for installs upgrading from the old toolkit:
# the toolkit speaks the OVERLEAF_ env prefix; flag old SHARELATEX_ configs.
function check_sharelatex_env_vars() {
  local env_occurrences=0
  if [[ -f "$TOOLKIT_ROOT/config/variables.env" ]]; then
    env_occurrences=$(set +o pipefail && grep -o "SHARELATEX_" "$TOOLKIT_ROOT/config/variables.env" | wc -l | sed 's/ //g')
    if [[ "$env_occurrences" -gt 0 ]]; then
      echo "---------------------  WARNING  -----------------------"
      echo "  Your config/variables.env still uses the legacy SHARELATEX_"
      echo "  prefix ($env_occurrences lines). The OlliTeX toolkit expects"
      echo "  OVERLEAF_-prefixed variables. Migrate with:"
      echo ""
      echo "    toolkit\$ bin/rename-env-vars-5-0"
      echo ""
      exit 1
    fi
  fi
  local rc_occurrences=0
  if [[ -f "$TOOLKIT_ROOT/config/overleaf.rc" ]]; then
    rc_occurrences=$(set +o pipefail && grep -o "SHARELATEX_" "$TOOLKIT_ROOT/config/overleaf.rc" | wc -l | sed 's/ //g')
    if [[ "$rc_occurrences" -gt 0 ]]; then
      echo "  Your config/overleaf.rc still uses the legacy SHARELATEX_"
      echo "  prefix ($rc_occurrences lines). Migrate with:"
      echo ""
      echo "    toolkit\$ bin/rename-rc-vars"
      echo ""
      exit 1
    fi
  fi
}

# One-shot migration helper: SHARELATEX_ -> OVERLEAF_ in a config file
# (used by bin/rename-env-vars-5-0, bin/rename-rc-vars, bin/upgrade).
rebrand_sharelatex_env_variables() {
  local filename=$1
  local silent=${2:-no}
  local sharelatex_occurrences
  sharelatex_occurrences=$(set +o pipefail && grep -o "SHARELATEX_" "$TOOLKIT_ROOT/config/$filename" | wc -l | sed 's/ //g')
  if [ "$sharelatex_occurrences" -gt 0 ]; then
    echo "Rebranding legacy SHARELATEX_ variables to OVERLEAF_"
    echo "  Found $sharelatex_occurrences lines with SHARELATEX_ in config/$filename"
    local timestamp=$(date "+%Y.%m.%d-%H.%M.%S")
    local backup_filename="__old-$filename.$timestamp"
    prompt "  Proceed with the renaming in config/$filename?"
    echo "  Creating backup file config/$backup_filename"
    cp "$TOOLKIT_ROOT/config/$filename" "$TOOLKIT_ROOT/config/$backup_filename"
    echo "  Replacing 'SHARELATEX_' with 'OVERLEAF_' in config/$filename"
    sed -i "s/SHARELATEX_/OVERLEAF_/g" "$TOOLKIT_ROOT/config/$filename"
    echo "  Updated $sharelatex_occurrences lines in config/$filename"
  else
    if [[ "$silent" != "silent_if_no_match" ]]; then
      echo "Rebranding legacy SHARELATEX_ variables to OVERLEAF_"
      echo "  No 'SHARELATEX_' occurrences found in config/$filename"
    fi
  fi
}

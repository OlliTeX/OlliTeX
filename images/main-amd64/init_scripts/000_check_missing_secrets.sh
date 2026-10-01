#!/bin/bash

set -e

REQUIRED_SECRETS="
OVERLEAF_INVITE_TOKEN_SECRET
"

MISSING_ITEMS=""
for name in ${REQUIRED_SECRETS}; do
  if [[ -z "${!name}" ]]; then
    MISSING_ITEMS="$MISSING_ITEMS $name"
  fi
done

if [[ "$MISSING_ITEMS" == "" ]]; then
  exit 0
fi

MISSING_ITEMS=$(echo "$MISSING_ITEMS" | xargs -n1 | sed 's/^/      - /')
N=$(echo "$MISSING_ITEMS" | wc -l)

# 2026-09-27 (owner feedback: the old hint always said "sharelatex service
# in docker-compose.yml" — useless outside that exact setup, and it hid the
# real common cause: a compose RECREATE without the stack env file, so
# ${NAME} interpolated to ""). Print exactly what THIS container sees.
SECRET_LOOKING_ENV=""
for var in $(printenv | cut -d= -f1 | sort -u); do
  case "$var" in
    *SECRET* | *TOKEN* | *KEY* | *PASSWORD*)
      if [[ -z "${!var}" ]]; then
        SECRET_LOOKING_ENV="$SECRET_LOOKING_ENV\n      - $var  (EMPTY in the container env)"
      else
        SECRET_LOOKING_ENV="$SECRET_LOOKING_ENV\n      - $var  (set)"
      fi
      ;;
  esac
done
# collapse "\n" into newlines (values are never printed — names only)
SECRET_LOOKING_ENV=$(echo -e "$SECRET_LOOKING_ENV")

CONTEXT_HINT=""
if [[ -n "${COMPOSE_PROJECT_NAME:-}" ]]; then
  SVC="${COMPOSE_SERVICE_NAME:-<service>}"
  CONTEXT_HINT="
  You are inside a docker compose container (project: ${COMPOSE_PROJECT_NAME}, service: ${SVC}).
  A MISSING secret here is almost always a recreation without the stack env
  file (empty-string interpolation), NOT a missing compose 'environment:'
  entry. Verify with the EXACT invocation you would use to recreate:

    docker compose -p <project> -f <compose-file> --env-file <stack-env-file> config <service> | grep <NAME>

  (this repo's e2e example: tests/e2e/.env.test must be passed as
   --env-file whenever the ol-e2e overleaf container is (re)created)
"
else
  CONTEXT_HINT="
  docker compose setups:

    Add the missing variable(s) to the overleaf/sharelatex service
    environment AND set them in the stack env file used by compose
    (--env-file .env.test / env_file:). A compose recreate without the
    env file resolves \${NAME} to \"\" even if the service block references
    it — compose expands \${...} from the invocation env, not the
    container's later env.

  Overleaf toolkit setups:

    Add the missing variable(s) to config/variables.env and restart:

      bin/up
"
fi

cat <<EOF
------------------------------------------------------------------------

                     Missing required secrets
                     ------------------------

  Your configuration is missing $N required secret(s):
$MISSING_ITEMS

  These secrets must be set to persistent random values and kept
   stable across container restarts and upgrades. Regenerating them
   will invalidate previously issued tokens stored in the database.

  Generate a value with, e.g.:

    openssl rand -base64 32

  What this container actually sees (names only — NO values printed):
$SECRET_LOOKING_ENV
$CONTEXT_HINT
  Refusing to startup, exiting in 10s.

------------------------------------------------------------------------
EOF

sleep 10
exit 101

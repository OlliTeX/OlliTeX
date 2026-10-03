import mongodb from 'mongodb-legacy'
import {
  connectionPromise,
  db,
} from '../../../app/src/infrastructure/mongodb.mjs'

const { ObjectId } = mongodb

const DEFAULT_MIN_MONGO_VERSION = [8, 0]
const DEFAULT_MIN_MONGO_FCV = [7, 0]
// overleaf-lab (e2e, 2026-09): host kernel 7.0 + kernel-incompatible fresh
// mongo 8.x builds (SERVER-121912 / 8.x segfaults under load) make a
// disposable test stack need mongo 6.0. Override the gates via env when
// running such a stack; PRODUCTION DEFAULTS UNCHANGED (8.0 / FCV 7.0).
function versionPair(envValue, fallback) {
  if (!envValue) return fallback
  const [a, b] = String(envValue).split('.').map(n => parseInt(n, 10))
  return [a, b || 0]
}
const MIN_MONGO_VERSION = versionPair(process.env.OL_MIN_MONGO_VERSION, DEFAULT_MIN_MONGO_VERSION)
const MIN_MONGO_FEATURE_COMPATIBILITY_VERSION = versionPair(
  process.env.OL_MIN_MONGO_FCV,
  DEFAULT_MIN_MONGO_FCV
)

// Allow ignoring admin check failures via an environment variable
const OVERRIDE_ENV_VAR_NAME = 'ALLOW_MONGO_ADMIN_CHECK_FAILURES'

function shouldSkipAdminChecks() {
  return process.env[OVERRIDE_ENV_VAR_NAME] === 'true'
}

function handleUnauthorizedError(err, feature) {
  if (
    err instanceof mongodb.MongoServerError &&
    err.codeName === 'Unauthorized'
  ) {
    console.warn(`Warning: failed to check ${feature} (not authorised)`)
    if (!shouldSkipAdminChecks()) {
      console.error(
        `Please ensure the MongoDB user has the required permissions, for more information see
https://docs.overleaf.com/on-premises/maintenance/updating-mongodb#creating-a-custom-role
or set the environment variable ${OVERRIDE_ENV_VAR_NAME}=true to ignore this check.`
      )
      process.exit(1)
    }
    console.warn(
      `Ignoring ${feature} check failure (${OVERRIDE_ENV_VAR_NAME}=${process.env[OVERRIDE_ENV_VAR_NAME]})`
    )
  } else {
    throw err
  }
}

async function main() {
  let mongoClient
  try {
    mongoClient = await connectionPromise
  } catch (err) {
    console.error('Cannot connect to mongodb')
    throw err
  }

  try {
    await checkMongoVersion(mongoClient)
  } catch (err) {
    handleUnauthorizedError(err, 'MongoDB version')
  }
  try {
    await checkFeatureCompatibilityVersion(mongoClient)
  } catch (err) {
    handleUnauthorizedError(err, 'MongoDB feature compatibility version')
  }

  try {
    await testTransactions(mongoClient)
  } catch (err) {
    console.error("Mongo instance doesn't support transactions")
    throw err
  }
}

async function testTransactions(mongoClient) {
  const session = mongoClient.startSession()
  try {
    await session.withTransaction(async () => {
      await db.users.findOne({ _id: new ObjectId() }, { session })
    })
  } finally {
    await session.endSession()
  }
}

async function checkMongoVersion(mongoClient) {
  const buildInfo = await mongoClient.db().admin().buildInfo()
  const [major, minor] = buildInfo.versionArray
  const [minMajor, minMinor] = MIN_MONGO_VERSION

  if (major < minMajor || (major === minMajor && minor < minMinor)) {
    const version = buildInfo.version
    const minVersion = MIN_MONGO_VERSION.join('.')
    console.error(
      `The MongoDB server has version ${version}, but Overleaf requires at least version ${minVersion}. Aborting.`
    )
    process.exit(1)
  }
}

async function checkFeatureCompatibilityVersion(mongoClient) {
  const {
    featureCompatibilityVersion: { version },
  } = await mongoClient
    .db()
    .admin()
    .command({ getParameter: 1, featureCompatibilityVersion: 1 })
  const [major, minor] = version.split('.').map(v => parseInt(v))
  const [minMajor, minMinor] = MIN_MONGO_FEATURE_COMPATIBILITY_VERSION

  if (major < minMajor || (major === minMajor && minor < minMinor)) {
    const minVersion = MIN_MONGO_FEATURE_COMPATIBILITY_VERSION.join('.')
    console.error(`
The MongoDB server has featureCompatibilityVersion=${version}, but Overleaf requires at least version ${minVersion}.

Open a mongo shell:
- Overleaf Toolkit deployments: $ bin/mongo
- Legacy docker-compose.yml deployments: $ docker exec -it mongo mongosh localhost/sharelatex

In the mongo shell:
> db.adminCommand( { setFeatureCompatibilityVersion: "${minMajor}.${minMinor}" } )

Verify the new value:
> db.adminCommand( { getParameter: 1, featureCompatibilityVersion: 1 } )
 ...
 {
    featureCompatibilityVersion: { version: ${minMajor}.${minMinor}' },
...

Aborting.
`)
    process.exit(1)
  }
}

main()
  .then(() => {
    console.error('Mongodb is up.')
    process.exit(0)
  })
  .catch(err => {
    console.error(err)
    process.exit(1)
  })

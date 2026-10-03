import { callbackify } from 'node:util'
import { db, READ_PREFERENCE_SECONDARY } from '../../infrastructure/mongodb.mjs'

// OlliTeX fork (free-only): populateTeamInvites (SaaS legacy team-invite
// backfill) removed with the subscription feature.

async function countActiveUsers() {
  const oneYearAgo = new Date()
  oneYearAgo.setFullYear(oneYearAgo.getFullYear() - 1)
  return await db.users.countDocuments(
    { lastActive: { $gte: oneYearAgo } },
    { readPreference: READ_PREFERENCE_SECONDARY }
  )
}

export default {
  countActiveUsers: callbackify(countActiveUsers),
  promises: {
    countActiveUsers,
  },
}

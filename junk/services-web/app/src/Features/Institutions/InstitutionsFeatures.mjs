// OlliTeX fork (2026-09-05): SaaS plan / add-on feature resolution
// (getInstitutionsPlan, getInstitutionsFeatures) removed with the
// subscription feature. Kept: hasLicence — the CE institutional-SSO licence
// check (SAML login), which only consults the user's confirmed emails.

import { callbackifyAll } from '@overleaf/promise-utils'
import UserGetter from '../User/UserGetter.mjs'

async function hasLicence(userId) {
  const emailsData = await UserGetter.promises.getUserFullEmails(userId)
  return emailsData.some(emailData => emailData.emailHasInstitutionLicence)
}

const InstitutionsFeatures = {
  hasLicence,
}

export default {
  promises: InstitutionsFeatures,
  ...callbackifyAll(InstitutionsFeatures),
}

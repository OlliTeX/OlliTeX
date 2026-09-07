import AuthenticationController from '../../../../app/src/Features/Authentication/AuthenticationController.mjs'
import MendeleyController from './MendeleyController.mjs'

export default {
  apply(webRouter) {
    // Connection status (drives the reference-linking widgets)
    webRouter.get(
      '/user/mendeley/status',
      AuthenticationController.requireLogin(),
      MendeleyController.status
    )

    // Get Mendeley groups for the create-file modal
    webRouter.get(
      '/mendeley/groups',
      AuthenticationController.requireLogin(),
      MendeleyController.getGroups
    )

    // Start the Mendeley OAuth flow by redirecting to Mendeley's authorization page
    webRouter.get(
      '/user/mendeley/oauth',
      AuthenticationController.requireLogin(),
      MendeleyController.oauth
    )

    // Callback for the Mendeley OAuth flow
    webRouter.get(
      '/user/mendeley/oauth/callback',
      AuthenticationController.requireLogin(),
      MendeleyController.oauthCallback
    )

    // Unlink Mendeley account
    webRouter.post(
      '/mendeley/unlink',
      AuthenticationController.requireLogin(),
      MendeleyController.unlink
    )
  },
}

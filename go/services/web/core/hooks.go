package core

// hooks.go — SSO seams (Node modules/authentication logout chain +
// P1c finishLogin update parity). The core app owns POST /logout and the
// password /login finish; the SSO feature (features/sso) plugs in here
// so authpages stays SSO-free (Node: modules/authentication/logout.mjs
// dispatches by user.externalAuth; AuthenticationManager sets
// $unset ssoLoginProviderId on password logins).

// SSOLogoutHook runs at the START of POST /logout. Return true when the
// SSO flow produced the complete response (SAML SLO redirect / OIDC
// logout redirect) and the standard /logout tail must be skipped. The
// hook itself performs the session destroy (Node doLogout) before
// redirecting, exactly like modules/authentication/{saml,oidc}.
type SSOLogoutHook func(cxt *Cxt, res *Res) bool

// PasswordLoginHook runs after a successful PASSWORD login (not the SSO
// flows) with a fresh committed session; used by the SSO feature to clear
// the last-SSO provider marker (Node P1c: non-SSO login => 'local').
type PasswordLoginHook func(cxt *Cxt)

// LoginSlotHook — the /login page ol-auth-config slot builder (fedgap-2,
// Node SSOAdminRouter webRouter.use('/login') res.locals.ssoProviders parity).
// Returns the HTML-escaped JSON meta content (e.g.
// `{&quot;sso&quot;:[{&quot;label&quot;:...,&quot;href&quot;:...}],&quot;ldapEnabled&quot;:false}`);
// nil/"" renders the anonymous default (sso:[] + ldapEnabled:false).
// Registered by the sso feature; called by authpages' /login handler.
type LoginSlotHook func(cxt *Cxt) string

func (a *App) SetSSOLogoutHook(h SSOLogoutHook)         { a.ssoLogoutHook = h }
func (a *App) SetPasswordLoginHook(h PasswordLoginHook) { a.passwordLoginHook = h }
func (a *App) SetLoginSlotHook(h LoginSlotHook)         { a.loginSlotHook = h }

// SSOLogout — call from the /logout handler before the default flow.
func (a *App) SSOLogout(cxt *Cxt, res *Res) bool {
	if a.ssoLogoutHook == nil {
		return false
	}
	return a.ssoLogoutHook(cxt, res)
}

// PasswordLoginHooked — call from the password-login success path.
func (a *App) PasswordLoginHooked(cxt *Cxt) {
	if a.passwordLoginHook != nil {
		a.passwordLoginHook(cxt)
	}
}

// LoginSlotJSON — the /login ol-auth-config meta content ("" when no hook).
func (a *App) LoginSlotJSON(cxt *Cxt) string {
	if a.loginSlotHook != nil {
		return a.loginSlotHook(cxt)
	}
	return ""
}

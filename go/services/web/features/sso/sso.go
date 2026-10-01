// Package sso — N-provider (N x) SSO login: SAML 2.0 service provider +
// OIDC relying party, admin-managed providers (ssoConfigs doc) with env
// fallback, login-page slot, P1c attrFilter roles (local/guest/blocked).
//
// Distinct from features/federation (instance-to-instance identity
// federation): this is the institution-facing LOGIN surface.
//
// # Wire oracles (byte-pinned)
//
// SAML (overleaf-fed modules/authentication/saml is the N-provider
// oracle; @node-saml/passport-saml 5.1.1 + node-saml 5.1.0 vendored at
// /tmp/saml-oracle):
//
//   - GET /saml/login → set session samlProviderId, then 302 to
//     entryPoint?SAMLRequest=<base64(deflate-Raw(AuthnRequest))>
//     [RelayState]. AuthnRequest: samlp/saml xmlns, ID=_+hex(40),
//     Version 2.0, IssueInstant, ProtocolBinding HTTP-POST,
//     Destination=entryPoint, AssertionConsumerServiceURL=callback,
//     saml:Issuer, samlp:NameIDPolicy (AllowCreate, default format
//     urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress),
//     samlp:RequestedAuthnContext (exact /
//     PasswordProtectedTransport); authnRequestBinding=HTTP-POST ⇒
//     auto-submit HTML form instead of 302.
//   - GET /saml/login/:providerId (DB row id; 'saml' env synthetic
//     accepted; unknown ⇒ 404 "SAML provider '<id>' not found or
//     disabled").
//   - POST /saml/login/callback (SINGLE ACS for all providers;
//     provider resolved from session samlProviderId):
//     SAMLResponse = base64 XML (no compression); wantAuthnResponse
//     Signed default TRUE ⇒ response or assertion signed (XML-DSig,
//     RSA-PKCS1v1_5 / ECDSA, digest sha1|256|512 over exc-c14n);
//     EncryptedAssertion = xml-encryption (AES-CBC key-wrap RSA-1_5 +
//     HKDF-SHA1); timestamps with acceptedClockSkewMs (0 default, -1
//     disables); audience check when configured; InResponseTo per
//     validateInResponseTo (never|ifPresent|always; default never);
//     non-200 Response status ⇒ reject; Issuer must match idp
//     entityID.
//   - Profile: nameID + saml:Attribute (Name → scalar|array) +
//     SubjectConfirmationData SessionIndex.
//   - SLO: POST /logout (user.externalAuth==='saml') ⇒ destroy
//     session (doLogout parity) then 302 {logoutUrl}?SAMLRequest
//     (deflated LogoutRequest); IdP-initiated SLO lands on
//     GET /saml/logout/callback ⇒ 302 /login.
//   - GET /saml/meta ⇒ SP metadata XML (EntityDescriptor,
//     SPSSODescriptor, ACS HTTP-POST isDefault index=1),
//     Content-Disposition: attachment; filename="<issuer>-meta.xml".
//
// OIDC (passport-openidconnect 0.1.x base overleaf 6.3.0_post +
// overleaf-fed N-provider manager):
//
//   - GET /oidc/login[/:providerId] ⇒ 302
//     {authorizationURL|discovery}?response_type=code&client_id&
//     redirect_uri={site}/oidc/login/callback&scope (+state+nonce
//     random hex).
//   - GET /oidc/login/callback ⇒ POST {tokenURL} form
//     (code, client_id, client_secret, grant_type=authorization_code,
//     redirect_uri) ⇒ {access_token, id_token?}; profile from
//     userinfo when userInfoURL set, else id_token claims. Go adds
//     id_token verification via go-oidc v3 (RS256/ES256/HS256, jwks,
//     iss/aud/exp/nonce) — Go-first hardening over the Node default
//     (checkIdToken off) — recorded deviation.
//   - JIT: ThirdPartyIdentity {providerId, providerUserId} → user;
//     else email lookup (allowedEmailDomains gate `*.` suffix/exact);
//     else create (holdingAccount:false, analyticsId); link
//     (emails.0.confirmedAt + oidcProviderId); loginEpoch++
//     optimistic lock (mismatch ⇒ 4xx parallel-login); $unset
//     hashedPassword; attAdmin → isAdmin.
//   - externalAuth='oidc', session.idToken; SLO: POST /logout ⇒
//     destroy session then 302 {logoutURL}?id_token_hint=..&
//     post_logout_redirect_uri={siteUrl}; GET /oidc/logout/callback
//     ⇒ 302 {siteUrl}.
//   - POST /user/oauth-unlink ⇒ drop the user's ThirdPartyIdentity
//     rows (scoped to the provider when given).
//
// Config (ssoConfigLoader parity): Mongo doc `ssoConfigs` _id
// 'sso-settings' {ldap:{...}, providers:[{id,name,type,enabled,order,
// identityServiceName, attrFilter, …type fields}], spMetadata:{…}};
// ABSENT ⇒ env mode (synthetic provider ids 'saml'/'oidc' from
// OVERLEAF_SAML_* / OVERLEAF_OIDC_*, gated by EXTERNAL_AUTH
// membership). Admin surface: /admin/sso/* (fedgap-2).
//
// # P1c roles (ssoRoleEvaluator parity, overleaf-fed)
//
//   - provider.attrFilter rows {attribute, values[]≤10, match
//     equals|includes|regex, role guest|blocked|local, caseSensitive}
//     — array order, first matching row wins; no match ⇒ local; rows
//     with role 'local' are inert.
//   - blocked ⇒ login refused BEFORE account creation: 401
//     {"message":{"text":"Login denied by SSO role filter","type":
//     "error","status":401}} + audit 'sso-login-denied'.
//   - local/guest ⇒ user.ssoRoles[providerId] = {role, at} +
//     user.ssoLoginProviderId = providerId (same loginEpoch++ update);
//     PASSWORD login $unsets ssoLoginProviderId (core hook).
//   - guest/blocked owners: creation refusal at the
//     project-creation choke (fedgap-3: ProjectCreationHandler guard).

package sso

import (
	"context"
	"errors"
	"regexp"
	"time"

	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

var errNoMongo = errors.New("sso: no database (a.Mongo not wired)")

// ---- provider configuration (Node ssoConfigs shape — camelCase keys) ----

// AttrRule — P1c attrFilter row (sanitized shape:
// {attribute, values≤10 strings, match, role, caseSensitive}).
type AttrRule struct {
	Attribute     string   `bson:"attribute" json:"attribute"`
	Values        []string `bson:"values" json:"values"`
	Match         string   `bson:"match" json:"match"`
	Role          string   `bson:"role" json:"role"`
	CaseSensitive bool     `bson:"caseSensitive" json:"caseSensitive"`
}

// SAMLProvider — one DB row (type saml).
type SAMLProvider struct {
	ID                  string     `bson:"id"`
	Name                string     `bson:"name,omitempty"`
	Type                string     `bson:"type"`
	Enabled             bool       `bson:"enabled"`
	Order               int        `bson:"order,omitempty"`
	IdentityServiceName string     `bson:"identityServiceName,omitempty"`
	AttrFilter          []AttrRule `bson:"attrFilter,omitempty"`
	// SSO strategy options (Node OptionsContract):
	Issuer                   string `bson:"issuer,omitempty"`
	EntryPoint               string `bson:"entryPoint,omitempty"`
	Audience                 string `bson:"audience,omitempty"`
	IdpCert                  string `bson:"idpCert,omitempty"`
	PrivateKey               string `bson:"privateKey,omitempty"`
	DecryptionPvk            string `bson:"decryptionPvk,omitempty"`
	DecryptionCert           string `bson:"decryptionCert,omitempty"`
	PublicCert               string `bson:"publicCert,omitempty"`
	SignatureAlgorithm       string `bson:"signatureAlgorithm,omitempty"`
	WantAssertionsSigned     bool   `bson:"wantAssertionsSigned"`
	WantAuthnResponseSigned  bool   `bson:"wantAuthnResponseSigned"`
	ForceAuthn               bool   `bson:"forceAuthn"`
	IsPassive                bool   `bson:"isPassive,omitempty"`
	IdentifierFormat         string `bson:"identifierFormat,omitempty"`
	AuthnContext             string `bson:"authnContext,omitempty"`
	DisableRequestedAuthn    bool   `bson:"disableRequestedAuthnContext,omitempty"`
	LogoutURL                string `bson:"logoutUrl,omitempty"`
	AcceptedClockSkewMs      int64  `bson:"acceptedClockSkewMs,omitempty"`
	RequestIdExpirationMs    int64  `bson:"requestIdExpirationPeriodMs,omitempty"`
	ValidateInResponseTo     string `bson:"validateInResponseTo,omitempty"` // never|ifPresent|always
	AttributeConsumingSvcIdx string `bson:"attributeConsumingServiceIndex,omitempty"`
	AuthnRequestBinding      string `bson:"authnRequestBinding,omitempty"` // default http-redirect
	// attribute mappings (Node Settings.saml.att* / DB row fields):
	AttUserID         string `bson:"attUserId,omitempty"`
	AttEmail          string `bson:"attEmail,omitempty"`
	AttFirstName      string `bson:"attFirstName,omitempty"`
	AttLastName       string `bson:"attLastName,omitempty"`
	AttAdmin          string `bson:"attAdmin,omitempty"`
	ValAdmin          string `bson:"valAdmin,omitempty"`
	UpdateUserDetails bool   `bson:"updateUserDetailsOnLogin"`
}

// OIDCProvider — one DB row (type oidc).
type OIDCProvider struct {
	ID                  string     `bson:"id"`
	Name                string     `bson:"name,omitempty"`
	Type                string     `bson:"type"`
	Enabled             bool       `bson:"enabled"`
	Order               int        `bson:"order,omitempty"`
	IdentityServiceName string     `bson:"identityServiceName,omitempty"`
	AttrFilter          []AttrRule `bson:"attrFilter,omitempty"`
	Issuer              string     `bson:"issuer,omitempty"`
	AuthorizationURL    string     `bson:"authorizationURL,omitempty"`
	TokenURL            string     `bson:"tokenURL,omitempty"`
	UserInfoURL         string     `bson:"userInfoURL,omitempty"`
	LogoutURL           string     `bson:"logoutURL,omitempty"`
	ClientID            string     `bson:"clientID,omitempty"`
	ClientSecret        string     `bson:"clientSecret"`
	Scope               string     `bson:"scope,omitempty"`
	UserIDField         string     `bson:"userIdField,omitempty"`
	EmailField          string     `bson:"emailField,omitempty"`
	AdminField          string     `bson:"adminField,omitempty"`
	AdminValue          string     `bson:"adminValue,omitempty"`
	AllowedEmailDomains string     `bson:"allowedEmailDomains,omitempty"`
}

// LDAPProvider — one DB section (6b).
type LDAPProvider struct {
	Enabled                  bool       `bson:"enabled"`
	IdentityServiceName      string     `bson:"identityServiceName,omitempty"`
	URL                      string     `bson:"url,omitempty"`
	SearchBase               string     `bson:"searchBase,omitempty"`
	BindDN                   string     `bson:"bindDN,omitempty"`
	BindCredentials          string     `bson:"bindCredentials"`
	SearchFilter             string     `bson:"searchFilter,omitempty"`
	SearchScope              string     `bson:"searchScope,omitempty"`
	Placeholder              string     `bson:"placeholder,omitempty"`
	EmailAtt                 string     `bson:"emailAtt,omitempty"`
	FirstNameAtt             string     `bson:"firstNameAtt,omitempty"`
	LastNameAtt              string     `bson:"lastNameAtt,omitempty"`
	IsAdminAtt               string     `bson:"isAdminAtt,omitempty"`
	UpdateUserDetailsOnLogin bool       `bson:"updateUserDetailsOnLogin"`
	Timeout                  int        `bson:"timeout,omitempty"`
	AttrFilter               []AttrRule `bson:"attrFilter,omitempty"`
}

// SPConfig — SP metadata extras (plan 11).
type SPConfig struct {
	EntityID    string `bson:"entityID,omitempty"`
	Name        string `bson:"name"`
	ContactName string `bson:"contactName,omitempty"`
	Email       string `bson:"email,omitempty"`
	OrgName     string `bson:"orgName,omitempty"`
	PrivateCert string `bson:"privateKey"`
	PublicCert  string `bson:"publicCert"`
}

// SSOConfig — the `ssoConfigs` doc (_id 'sso-settings').
type SSOConfig struct {
	ID         string        `bson:"_id,omitempty"`
	Ldap       *LDAPProvider `bson:"ldap,omitempty"`
	Providers  []bson.Raw    `bson:"providers,omitempty"`
	SPMetadata *SPConfig     `bson:"spMetadata,omitempty"`
}

const ssoConfigID = "sso-settings"

// ---- routes (Node parity; fedgap-2 adds /admin/sso/*) ----

var (
	samlLoginPattern    = regexp.MustCompile(`^/saml/login$`)
	samlProviderPattern = regexp.MustCompile(`^/saml/login/(?P<providerId>[A-Za-z0-9][A-Za-z0-9_-]{1,63})$`)
	samlCBPattern       = regexp.MustCompile(`^/saml/login/callback$`)
	samlLogoutPattern   = regexp.MustCompile(`^/saml/logout/callback$`)
	samlMetaPattern     = regexp.MustCompile(`^/saml/meta$`)
	oidcLoginPattern    = regexp.MustCompile(`^/oidc/login$`)
	oidcProviderPattern = regexp.MustCompile(`^/oidc/login/(?P<providerId>[A-Za-z0-9][A-Za-z0-9_-]{1,63})$`)
	oidcCBPattern       = regexp.MustCompile(`^/oidc/login/callback$`)
	oidcLogoutPattern   = regexp.MustCompile(`^/oidc/logout/callback$`)
	unlinkPattern       = regexp.MustCompile(`^/user/oauth-unlink$`)
)

// Feature — the N-provider SSO surface.
func Feature(a *core.App) core.Feature {
	a.SetSSOLogoutHook(ssoLogoutHook(a))
	a.SetPasswordLoginHook(clearSSOMarker(a))
	return core.Feature{
		Name: "sso",
		Routes: []core.Route{
			{Method: "GET", Pattern: samlLoginPattern, NoLogin: true, Handler: samlLogin(a)},
			{Method: "GET", Pattern: samlProviderPattern, NoLogin: true, Handler: samlLogin(a)},
			{Method: "POST", Pattern: samlCBPattern, NoLogin: true, NoCSRF: true, Handler: samlACS(a)},
			{Method: "GET", Pattern: samlLogoutPattern, NoLogin: true, NoCSRF: true, Handler: samlLogoutCallbackH(a)},
			{Method: "GET", Pattern: samlMetaPattern, NoLogin: true, Handler: samlSPMetadata(a)},
			{Method: "GET", Pattern: oidcLoginPattern, NoLogin: true, Handler: oidcLogin(a)},
			{Method: "GET", Pattern: oidcProviderPattern, NoLogin: true, Handler: oidcLogin(a)},
			{Method: "GET", Pattern: oidcCBPattern, NoLogin: true, NoCSRF: true, Handler: oidcCallback(a)},
			{Method: "GET", Pattern: oidcLogoutPattern, NoLogin: true, NoCSRF: true, Handler: oidcLogoutCallbackH(a)},
			{Method: "POST", Pattern: unlinkPattern, Handler: oauthUnlink(a)},
		},
	}
}

// ---- store helpers ----

func ssoDB(a *core.App, c *core.Cxt) (*mongo.Database, error) {
	if a == nil || a.Mongo == nil {
		return nil, errNoMongo
	}
	return a.Mongo.DB(c.Req.Context())
}

// loadSSOConfig — ssoConfigs doc or nil (env mode).
func loadSSOConfig(db *mongo.Database, c *core.Cxt) *SSOConfig {
	var raw bson.Raw
	if err := db.Collection("ssoConfigs").FindOne(c.Req.Context(),
		bson.D{{Key: "_id", Value: ssoConfigID}}).Decode(&raw); err != nil {
		return nil
	}
	var cfg SSOConfig
	if uerr := bson.Unmarshal(raw, &cfg); uerr != nil {
		return nil
	}
	return &cfg
}

// samlProviderByID — DB row by id (nil when absent).
func samlProviderByID(cfg *SSOConfig, id string) *SAMLProvider {
	if cfg == nil {
		return nil
	}
	for i := range cfg.Providers {
		var p SAMLProvider
		if uerr := bson.Unmarshal(cfg.Providers[i], &p); uerr != nil {
			continue
		}
		if p.ID == id && p.Type == "saml" {
			out := p
			return &out
		}
	}
	return nil
}

// oidcProviderByID — DB row by id (nil when absent).
func oidcProviderByID(cfg *SSOConfig, id string) *OIDCProvider {
	if cfg == nil {
		return nil
	}
	for i := range cfg.Providers {
		var p OIDCProvider
		if uerr := bson.Unmarshal(cfg.Providers[i], &p); uerr != nil {
			continue
		}
		if p.ID == id && p.Type == "oidc" {
			out := p
			return &out
		}
	}
	return nil
}

func dbErr500(cxt *core.Cxt, res *core.Res) {
	res.BareWrite(500, []byte("Database connection failed"))
}

var _ = context.Background
var _ = time.Now
var _ = bson.M{}

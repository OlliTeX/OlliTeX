// Leaf identity (npm leaf.mjs oracle) — entity id, origin FQDN, OIDC
// endpoints, leaf EC builder (pairwise: NO authority_hints, leaf EC check
// 16: openid_provider.issuer == the OP issuer).
package federation

import (
	"net/url"
)

// getEntityIdGo — node `getEntityId`: the HTTPS origin of Settings.siteUrl
// without port ("https://<host>"). Must be https: (OIDF 1.0 entity id
// scheme rule).
func getEntityIdGo(siteURL string) (string, error) {
	u, err := url.Parse(siteURL)
	if err != nil {
		return "", Errorf("federation: Settings.siteUrl must be set (entity id is derived from it): %v", err)
	}
	if u.Scheme != "https" {
		return "", Errorf("federation: entity id must use https: (Settings.siteUrl is %q)", u.Scheme+"://")
	}
	return "https://" + stripPort(u.Host), nil
}

// getOriginGo — node `getOrigin`: bare FQDN (no scheme, no port); the S2S
// wire identity unit (03 §2).
func getOriginGo(siteURL string) (string, error) {
	u, err := url.Parse(siteURL)
	if err != nil {
		return "", Errorf("federation: Settings.siteUrl must be set (origin is derived from it): %v", err)
	}
	if u.Scheme != "https" {
		return "", Errorf("federation: origin requires https (Settings.siteUrl is %q)", u.Scheme+"://")
	}
	return stripPort(u.Host), nil
}

func stripPort(host string) string {
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return host[:i]
		}
		if host[i] < '0' || host[i] > '9' {
			break
		}
	}
	return host
}

// getClientIdGo — node `getClientId` (04 §7): one per instance, one per
// peer pair; the S2S assertion iss/sub.
func getClientIdGo(origin string) string {
	return "urn:overleaf-federation:client:" + origin
}

// getS2sEndpointGo — node `getS2sEndpoint`: our S2S endpoint (the `aud`
// we check on receive, 03 §2 step 2).
func getS2sEndpointGo(entityId string) string {
	return entityId + "/federation/s2s"
}

// OidcEndpointsGo — node `oidcEndpoints` (05 §8.6): absolute OP endpoints
// for this instance (the Go S4 OIDC provider mounts them).
type OidcEndpoints struct {
	Issuer        string
	Authorization string
	Token         string
	Jwks          string
	Callback      string
	EndSession    string
}

func oidcEndpointsGo(siteURL string) (OidcEndpoints, error) {
	entityId, err := getEntityIdGo(siteURL)
	if err != nil {
		return OidcEndpoints{}, err
	}
	base := entityId
	issuer := base + "/federation/oidc"
	return OidcEndpoints{
		Issuer:        issuer,
		Authorization: issuer + "/auth",
		Token:         issuer + "/token",
		Jwks:          issuer + "/jwks",
		Callback:      issuer + "/rp/callback",
		EndSession:    issuer + "/session/end",
	}, nil
}

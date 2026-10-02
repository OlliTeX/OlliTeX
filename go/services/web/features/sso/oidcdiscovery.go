package sso

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// oidcDiscovery — the fields the admin probe reports for an OIDC provider.
type oidcDiscovery struct {
	Issuer   string `json:"issuer"`
	Auth     string `json:"authorization_endpoint"`
	Token    string `json:"token_endpoint"`
	Userinfo string `json:"userinfo_endpoint"`
}

// fetchOIDCDiscovery — the POST /admin/sso/test OIDC discovery probe (Node
// _testOIDCProvider parity): GET {issuer}/.well-known/openid-configuration and
// surface the issuer + the three endpoints. Extracted (from the inline admin
// handler) so it is unit- and live-testable in isolation.
func fetchOIDCDiscovery(ctx context.Context, issuer string) (*oidcDiscovery, error) {
	issuer = strings.TrimRight(issuer, "/")
	if issuer == "" {
		return nil, fmt.Errorf("no issuer URL configured")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC discovery failed: HTTP %d", resp.StatusCode)
	}
	var d oidcDiscovery
	if uerr := json.Unmarshal(data, &d); uerr != nil {
		return nil, uerr
	}
	return &d, nil
}

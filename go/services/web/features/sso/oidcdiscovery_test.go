package sso

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchOIDCDiscovery(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"issuer":"https://idp.example/realms/master",
			"authorization_endpoint":"https://idp.example/realms/master/protocol/openid-connect/auth",
			"token_endpoint":"https://idp.example/realms/master/protocol/openid-connect/token",
			"userinfo_endpoint":"https://idp.example/realms/master/protocol/openid-connect/userinfo"
		}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	d, err := fetchOIDCDiscovery(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if d.Issuer == "" || d.Auth == "" || d.Token == "" || d.Userinfo == "" {
		t.Fatalf("incomplete discovery: %+v", d)
	}
}

func TestFetchOIDCDiscovery_bad(t *testing.T) {
	if _, err := fetchOIDCDiscovery(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty issuer")
	}
	// A server that returns non-JSON / non-200 must be an error, not silent.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := fetchOIDCDiscovery(context.Background(), srv.URL); err == nil {
		t.Fatal("expected error for non-200 discovery")
	}
}

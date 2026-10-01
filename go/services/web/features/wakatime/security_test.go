package wakatime

// audit 034-N1 regression tests: the user-configurable WakaTime/Wakapi
// endpoint policy (URL shape, host lockdown, optional private-IP
// rejection) — with the self-hosted-Wakapi default (private hosts OK
// unless the instance opts in) preserved.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestN1_InvalidURLShapesRejected(t *testing.T) {
	cases := []string{
		"file:///etc/passwd",
		"javascript:alert(1)",
		"https://user:pass@host.example.com/api/v1", // no userinfo
		"ftp://host.example.com/api",
		"://host.example.com",
		"http://",
	}
	for _, c := range cases {
		if out, err := validateAPIURL(c); err == nil {
			t.Fatalf("want rejection for %q, got %q", c, out)
		}
		if err := checkCredsPolicy(context.Background(), wakaCreds{APIURL: c}); err == nil {
			t.Fatalf("want policy rejection for %q", c)
		}
	}
}

func TestN1_ValidShapesAccepted(t *testing.T) {
	cases := []string{
		"https://wakatime.com/api/v1",
		"https://wakatime.com/api/v1/", // trailing slash normalized
		"http://wakapi.local:5001",     // self-hosted Wakapi (default policy allows)
		"http://172.18.0.5:5001",       // docker-network Wakapi (default policy allows)
	}
	for _, c := range cases {
		out, err := validateAPIURL(c)
		if err != nil {
			t.Fatalf("want accept for %q: %v", c, err)
		}
		if out != c && out != c[:len(c)-1] {
			t.Fatalf("normalization changed %q to %q", c, out)
		}
		if err := checkCredsPolicy(context.Background(), wakaCreds{APIURL: c}); err != nil {
			t.Fatalf("default policy must allow self-hosted Wakapi %q: %v", c, err)
		}
	}
}

func TestN1_HostLockdown(t *testing.T) {
	t.Setenv("WAKATIME_ALLOWED_API_HOSTS", "*.wakatime.com, wakapi.example.org")
	ok := wakaCreds{APIURL: "https://wakatime.com/api/v1"}
	if err := checkCredsPolicy(context.Background(), ok); err != nil {
		t.Fatalf("wakatime.com must pass *.wakatime.com lockdown: %v", err)
	}
	bad := wakaCreds{APIURL: "http://192.168.1.9:5001"}
	err := checkCredsPolicy(context.Background(), bad)
	if err != errHostBlocked {
		t.Fatalf("want errHostBlocked under lockdown, got %v", err)
	}
}

func TestN1_PrivateIPRejectionOptIn(t *testing.T) {
	t.Setenv("WAKATIME_REJECT_PRIVATE_IPS", "1")
	// loopback must be rejected when the policy is on
	err := checkCredsPolicy(context.Background(), wakaCreds{APIURL: "http://127.0.0.1:5001"})
	if err != errHostBlocked {
		t.Fatalf("want loopback rejected, got %v", err)
	}
	err = checkCredsPolicy(context.Background(), wakaCreds{APIURL: "http://169.254.169.254/latest/meta-data/"})
	if err != errHostBlocked {
		t.Fatalf("want link-local rejected, got %v", err)
	}
	// public IP must pass
	if err := checkCredsPolicy(context.Background(), wakaCreds{APIURL: "https://93.184.216.34/api/v1"}); err != nil {
		t.Fatalf("public IP must pass: %v", err)
	}
}

func TestN1_LegacyStoredCredentialBlockedWhenPolicyTightens(t *testing.T) {
	// a credential stored before lockdown tightening must be rejected at
	// the client dial boundary (defense in depth).
	t.Setenv("WAKATIME_ALLOWED_API_HOSTS", "*.wakatime.com")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := newWakaClient()
	err := c.verifyCredentials(context.Background(), wakaCreds{APIURL: srv.URL + "/api/v1", APIKey: "secret"})
	if err == nil {
		t.Fatal("want the stored private endpoint to be refused under lockdown")
	}
}

// audit 034-N4: the in-process sentinels stay distinct even though both
// map to the Node-parity 500 boundary.
func TestN4_SentinelDistinction(t *testing.T) {
	if errBadAPI == errUpstream {
		t.Fatal("sentinels must stay distinct for logging/tests")
	}
	if errBadAPI.Error() == errUpstream.Error() {
		t.Fatal("sentinel messages must differ")
	}
	var e *ErrWaka
	if !asErrWaka(errBadAPI, &e) || e.Status != http.StatusInternalServerError {
		t.Fatal("errBadAPI must keep the 500 parity boundary")
	}
}

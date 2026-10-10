package wakatime

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"
)


// TestProvisionAccountAgainstLocal — live E2E (skipped unless
// WAKAPI_TEST_BASE is set, e.g. https://psintern…/wakapi): signup →
// login → reset_apikey → key scraped. Owner G, 2026-10-09.
func TestProvisionAccountAgainstLocal(t *testing.T) {
	base := os.Getenv("WAKAPI_TEST_BASE")
	if base == "" {
		t.Skip("set WAKAPI_TEST_BASE to run against a live wakapi")
	}
	un := fmt.Sprintf("gprobe%06d", time.Now().UnixNano()%1_000_000)
	p := ProvisionRequest{
		ServerBase: base,
		Username:   un,
		Email:      un + "@dev.local",
	}
	res, err := provisionAccount(context.Background(), p)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(res.Key) {
		t.Fatalf("key shape: %q", res.Key)
	}
}

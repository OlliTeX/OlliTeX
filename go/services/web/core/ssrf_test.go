package core

import (
	"net/netip"
	"testing"
)

func mustAddr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return a
}

func TestSSRFBlockedRanges(t *testing.T) {
	for _, s := range []string{
		// IPv4 blocked
		"0.0.0.0", "10.1.2.3", "172.16.0.5", "172.20.0.11", "172.31.255.255",
		"192.168.1.1", "169.254.169.254", "100.64.0.1", "198.18.1.1", "198.19.9.9",
		"224.0.0.1", "255.255.255.255", "127.0.0.53",
		// IPv6 blocked
		"::", "::1", "fe80::9", "fc00::1", "fd12:3456:789a::1",
		// mapped
		"::ffff:10.0.0.1", "::ffff:192.168.7.7", "::ffff:127.0.0.1",
	} {
		if !SSRFBlocked(mustAddr(t, s)) {
			t.Errorf("expected BLOCKED: %s", s)
		}
	}
	for _, s := range []string{
		// IPv4 allowed
		"1.1.1.1", "8.8.8.8", "93.184.216.34", "193.57.139.244",
		"172.15.255.255", "172.32.0.1", "100.127.255.255", "101.64.0.1",
		"169.255.255.255", "198.17.0.1",
		// IPv6 allowed
		"2606:4700:4400::1111", "2001:4860:4860::8888",
	} {
		if SSRFBlocked(mustAddr(t, s)) {
			t.Errorf("expected ALLOWED: %s", s)
		}
	}
}

func TestCheckSSRFHost(t *testing.T) {
	if err := CheckSSRFHost("172.20.0.11"); err == nil {
		t.Error("expected block for 172.20.0.11")
	}
	if err := CheckSSRFHost("127.0.0.1"); err != ErrSSRFBlocked {
		t.Errorf("127.0.0.1 err = %v, want ErrSSRFBlocked", err)
	}
	if err := CheckSSRFHost("193.57.139.244"); err != nil {
		t.Errorf("public IP should pass, got %v", err)
	}
	if err := CheckSSRFHost("example.com"); err != nil {
		t.Errorf("name should pass the parse-time check, got %v", err)
	}
	if err := CheckSSRFHost(""); err == nil {
		t.Error("empty host must fail")
	}
}

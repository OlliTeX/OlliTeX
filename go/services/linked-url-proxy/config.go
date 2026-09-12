// Package linkedurlproxy is the Go 1:1 rewrite of the Node linked-url-proxy
// service (services/linked-url-proxy/app/src/*). It was split from the single
// top-level services file into logical files mirroring the Node module layout.
package linkedurlproxy

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// LinkedURLProxyConfig holds every runtime setting of the linked-url-proxy
// service. The defaults and environment wiring mirror
// services/linked-url-proxy/config/settings.defaults.cjs exactly.
type LinkedURLProxyConfig struct {
	// MaxRedirects is the maximum number of redirects followed (default 5).
	MaxRedirects int
	// FetchTimeoutMs is the upstream fetch timeout in milliseconds (default 30000).
	FetchTimeoutMs int
	// BlockedNetworks is the raw list of CIDR strings to block. They are parsed
	// and matched per request so that an invalid entry yields a 500 at request
	// time (1:1 with the Node.js implementation).
	BlockedNetworks []string
	// AllowedResources, when non-nil, exempts matching URLs from the blocked-IP
	// check (1:1 with OVERLEAF_LINKED_URL_ALLOWED_RESOURCES).
	AllowedResources *regexp.Regexp
	// UserAgent is the User-Agent header sent to upstream (1:1 default string).
	UserAgent string
	// MaxUploadSize is the maximum upstream body size in bytes (default 50 MiB).
	MaxUploadSize int64
	// Host and Port are the local bind address (default 127.0.0.1:3066).
	Host string
	Port int

	// ResolveHost is the DNS backend used to resolve the target host. It
	// defaults to the system resolver (pbhttp.ResolveHostDNS) and can be
	// overridden in tests. 1:1 with `dns.lookup(hostname, { all: true })`.
	ResolveHost func(hostname string) ([]netip.Addr, error)
}

// DefaultLinkedURLProxyConfig returns a config with the same defaults as the
// Node.js settings.defaults.cjs (env-agnostic base values). Env overrides are
// applied by NewLinkedURLProxyConfigFromEnv.
func DefaultLinkedURLProxyConfig() *LinkedURLProxyConfig {
	return &LinkedURLProxyConfig{
		MaxRedirects:   5,
		FetchTimeoutMs: 30000,
		UserAgent:      "Overleaf Extended CE - LinkedURLProxy (https://github.com/yu-i-i/overleaf-cep)",
		MaxUploadSize:  50 * 1024 * 1024,
		Host:           "127.0.0.1",
		Port:           3066,
	}
}

// NewLinkedURLProxyConfigFromEnv builds a config from the environment,
// mirroring the exact parsing in config/settings.defaults.cjs.
func NewLinkedURLProxyConfigFromEnv(getEnv func(string) string) *LinkedURLProxyConfig {
	cfg := DefaultLinkedURLProxyConfig()

	if raw := getEnv("OVERLEAF_LINKED_URL_BLOCKED_NETWORKS"); raw != "" {
		cfg.BlockedNetworks = splitCIDRList(raw)
	}
	if raw := getEnv("OVERLEAF_LINKED_URL_ALLOWED_RESOURCES"); raw != "" {
		if re, err := regexp.Compile(raw); err == nil {
			cfg.AllowedResources = re
		}
	}
	if raw := getEnv("MAX_UPLOAD_SIZE"); raw != "" {
		if mb, err := strconv.Atoi(raw); err == nil {
			cfg.MaxUploadSize = int64(mb) * 1024 * 1024
		}
	}
	cfg.Host = getEnv("LINKED_URL_PROXY_HOST")
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	return cfg
}

// splitCIDRList splits a comma/whitespace-delimited CIDR list (1:1 with the
// Node `.split(/[,\s]+/).filter(Boolean).map(trim)` pipeline).
func splitCIDRList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

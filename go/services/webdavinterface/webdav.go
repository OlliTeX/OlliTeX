// Package webdavinterface is the Go 1:1 rewrite of the Node webdavinterface
// service (services/webdavinterface/app/src/*). Split from the single top-level
// services file into logical files mirroring the Node module layout
// (WebDAVClient.mjs, server.mjs). Shared HTTP helpers are imported from
// ollitex/go/pbhttp.
package webdavinterface

import (
	"net/http"
	"time"
)

// WebDAVError carries a provider status code (1:1 with the Node client errors
// tagged with `error.status`).
type WebDAVError struct {
	Status int
	Msg    string
	Err    error
}

func (e *WebDAVError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "webdav error"
}

func (e *WebDAVError) Unwrap() error { return e.Err }

func webdavStatus(status int, msg string) *WebDAVError { return &WebDAVError{Status: status, Msg: msg} }

// WebDAVEntry is the normalized directory entry returned by List/Check.
// Field-for-field with the Node WebDAVClient.list() mapping.
type WebDAVEntry struct {
	Href        string `json:"href"`
	Path        string `json:"path"`
	IsDirectory bool   `json:"isDirectory"`
	Etag        string `json:"etag"`
	ModifiedAt  string `json:"modifiedAt"` // ISO-8601 UTC or ""
	Size        int64  `json:"size"`
}

// WebDAVConfig holds the service/client runtime settings.
type WebDAVConfig struct {
	ServiceToken string       // SHARED_SERVICE_TOKEN ("" = legacy permissive, warn once)
	MaxRetries   int          // default 2
	RetryDelayMs int          // default 100
	HTTPClient   *http.Client // upstream transport (tests inject a fake)

	Sleep func(time.Duration) // backoff sleeper (tests make it instant); nil sleeps real
}

func (c *WebDAVConfig) withDefaults() {
	if c.MaxRetries == 0 {
		c.MaxRetries = 2
	}
	if c.RetryDelayMs == 0 {
		c.RetryDelayMs = 100
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if c.Sleep == nil {
		c.Sleep = time.Sleep
	}
}

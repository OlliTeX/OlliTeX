// Package dropboxinterface is the Go 1:1 rewrite of the Node dropboxinterface
// service (services/dropboxinterface/app/src/*). Split from the single
// top-level services file into logical files mirroring the Node module layout
// (DropboxClient.mjs, auth helpers, server.mjs). Shared HTTP helpers are
// imported from ollitex/go/pbhttp.
package dropboxinterface

import (
	"errors"
	"net/http"
)

// DropboxError carries a mapped HTTP status + message (1:1 with the Node
// customError { statusCode, message, dropboxErrorCode }).
type DropboxError struct {
	StatusCode       int
	Message          string
	DropboxErrorCode string
	Err              error
}

func (e *DropboxError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "dropbox error"
}
func (e *DropboxError) Unwrap() error { return e.Err }

func dropboxStatus(code int, msg, dbxEcode string) *DropboxError {
	return &DropboxError{StatusCode: code, Message: msg, DropboxErrorCode: dbxEcode}
}

func dropboxStatusOf(err error) int {
	var de *DropboxError
	if errors.As(err, &de) {
		return de.StatusCode
	}
	return 0
}

// DropboxEntry is a normalized list entry (1:1 with Node list()).
type DropboxEntry struct {
	RelativePath string  `json:"relative_path"`
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Size         int64   `json:"size"`
	Binary       bool    `json:"binary"`
	Checksum     *string `json:"checksum"`
	Hash         *string `json:"hash"`
	ContentHash  *string `json:"content_hash"`
	Mtime        *string `json:"mtime"`
	DropboxID    string  `json:"dropbox_id"`
	Rev          *string `json:"rev"`
}

// DropboxConfig holds the client runtime settings.
type DropboxConfig struct {
	ServiceToken string       // SHARED_SERVICE_TOKEN ("" = legacy permissive)
	APIBase      string       // default https://api.dropboxapi.com/2
	ContentBase  string       // default https://content.dropboxapi.com/2
	HTTPClient   *http.Client // upstream transport (tests inject a fake)
}

func (c *DropboxConfig) withDefaults() {
	if c.APIBase == "" {
		c.APIBase = "https://api.dropboxapi.com/2" // dropbox SDK default
	}
	if c.ContentBase == "" {
		c.ContentBase = "https://content.dropboxapi.com/2"
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{}
	}
}

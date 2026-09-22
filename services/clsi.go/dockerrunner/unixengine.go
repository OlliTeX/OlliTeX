package dockerrunner

// unixengine.go: the production Engine implementation — raw Docker
// Engine v2 API over /var/run/docker.sock (HTTP/1.1, plain, no TLS).
//
// It mirrors dockerode's wire behavior (probe-confirmed against the live
// engine: Docker Engine 29.5.3, ApiVersion 1.54):
//
//   Routes (OLD-style; the engine's /containers/{id}/inspect is 404):
//     Inspect   GET    /containers/{id}/json
//     Create    POST   /containers/create?name=<name>    body=<opts>
//     Attach    POST   /containers/{id}/attach?stdout=1&stderr=1&stream=1
//                 (streaming; NOT hijacked -> raw 8-byte-header demux)
//     Start     POST   /containers/{id}/start
//     Wait      POST   /containers/{id}/wait
//                 (blocking; 200 body {"StatusCode":N})
//     Kill      POST   /containers/{id}/kill  (no options -> engine SIGKILL)
//     Remove    DELETE /containers/{id}?force=<bool>&v=true
//     List      GET    /containers/json?all=true
//
//   Errors: dockerode shape (see APIError.Error), reason from the
//     per-operation statusCodes table via reason(code), cause = body
//     `message` || `cause` || raw body.
//
//   Wire divergences (documented; all benign):
//     - create body: Go omits `name` (the engine takes it from the query,
//       probe-confirmed) and `LogConfig.Config` (omitempty empty map).
//     - 200 responses: dockerode accepts them (unofficial, proxies), the
//       engine uses 201 for create/attach and 200 elsewhere; Go treats 2xx
//       uniformly.
//     - list: dockerode passes engine JSON through; Go unmarshals the
//       fields the monitor uses (Id, Name, Names, Created — epoch seconds).
//     - attach: Go returns the raw response body for 2xx; the demuxer
//       (pipeline.go) parses the 8-byte-header frames.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// UnixEngine is a Docker Engine client over a unix socket.
type UnixEngine struct {
	SocketPath string
	client     *http.Client
}

// client builds (and caches) a client that dials only the unix socket.
func (e *UnixEngine) httpClient() *http.Client {
	if e.client != nil {
		return e.client
	}
	var dialer net.Dialer
	e.client = &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, "unix", e.SocketPath)
			},
		},
	}
	return e.client
}

// req performs one request over the unix socket. The Host placeholder is
// ignored by the transport (it dials the socket directly).
func (e *UnixEngine) req(method, urlStr string, body []byte) (*http.Response, error) {
	if urlStr == "" {
		urlStr = "http://docker/"
	}
	req, err := http.NewRequest(method, urlStr, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(len(body))
	}
	resp, err := e.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// errShape builds a dockerode-style *APIError from a non-2xx response
// (message || cause || raw body).
func errShape(resp *http.Response) error {
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	reason := reason(resp.StatusCode)
	cause := ""
	var m map[string]any
	if json.Unmarshal(b, &m) == nil {
		if s, ok := m["message"].(string); ok {
			cause = s
		} else if s, ok := m["cause"].(string); ok {
			cause = s
		}
	}
	if cause == "" {
		cause = string(b)
	}
	return &APIError{
		StatusCode: resp.StatusCode,
		Reason:     reason,
		Message:    cause,
	}
}

// readOk drains and discards a 2xx body, returning the status error for
// non-2xx.
func readOk(resp *http.Response) error {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errShape(resp)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

// Inspect: GET /containers/{id}/json (the engine's old route).
func (e *UnixEngine) Inspect(id string) (*ContainerInfo, error) {
	resp, err := e.req("GET", "http://docker/containers/"+url.PathEscape(id)+"/json", nil)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errShape(resp)
	}
	b, _ := io.ReadAll(resp.Body)
	var info ContainerInfo
	if err := json.Unmarshal(b, &info); err != nil {
		return nil, fmt.Errorf("inspect %s: invalid JSON: %w", id, err)
	}
	return &info, nil
}

// Create: POST /containers/create?name=<name> body=<opts>.
func (e *UnixEngine) Create(id string, opts CreateOpts) error {
	b, err := json.Marshal(opts)
	if err != nil {
		return err
	}
	resp, err := e.req("POST", "http://docker/containers/create?name="+url.QueryEscape(id), b)
	if err != nil {
		return fmt.Errorf("create %s: %w", id, err)
	}
	return readOk(resp)
}

// Attach: POST /containers/{id}/attach?stdout=1&stderr=1&stream=1.
// Returns the raw 2xx response body (the 8-byte-header demux stream).
func (e *UnixEngine) Attach(id string) (io.ReadCloser, error) {
	resp, err := e.req("POST",
		"http://docker/containers/"+url.PathEscape(id)+"/attach?stdout=1&stderr=1&stream=1", nil)
	if err != nil {
		return nil, fmt.Errorf("attach %s: %w", id, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errShape(resp)
	}
	// Body stays open; the caller (drainStream) closes it.
	return resp.Body, nil
}

// Start: POST /containers/{id}/start. 304 -> APIError (already started).
func (e *UnixEngine) Start(id string) error {
	resp, err := e.req("POST", "http://docker/containers/"+url.PathEscape(id)+"/start", nil)
	if err != nil {
		return fmt.Errorf("start %s: %w", id, err)
	}
	return readOk(resp)
}

// Wait: POST /containers/{id}/wait (blocking). 2xx body {"StatusCode":N}.
func (e *UnixEngine) Wait(id string) (int, error) {
	resp, err := e.req("POST", "http://docker/containers/"+url.PathEscape(id)+"/wait", nil)
	if err != nil {
		return 0, fmt.Errorf("wait %s: %w", id, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, errShape(resp)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if len(b) == 0 {
		return 0, nil
	}
	var w struct {
		StatusCode int `json:"StatusCode"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return 0, fmt.Errorf("wait %s: invalid JSON: %s", id, b)
	}
	return w.StatusCode, nil
}

// Kill: POST /containers/{id}/kill (dockerode no-arg default -> SIGKILL).
func (e *UnixEngine) Kill(id string) error {
	resp, err := e.req("POST", "http://docker/containers/"+url.PathEscape(id)+"/kill", nil)
	if err != nil {
		return fmt.Errorf("kill %s: %w", id, err)
	}
	return readOk(resp)
}

// Remove: DELETE /containers/{id}?force=<bool>&v=true.
func (e *UnixEngine) Remove(id string, force bool) error {
	q := "force=" + strconv.FormatBool(force) + "&v=true"
	resp, err := e.req("DELETE", "http://docker/containers/"+url.PathEscape(id)+"?"+q, nil)
	if err != nil {
		return fmt.Errorf("remove %s: %w", id, err)
	}
	return readOk(resp)
}

// List: GET /containers/json?all=true.
func (e *UnixEngine) List() ([]ListedContainer, error) {
	resp, err := e.req("GET", "http://docker/containers/json?all=true", nil)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errShape(resp)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var containers []ListedContainer
	if err := json.Unmarshal(b, &containers); err != nil {
		return nil, fmt.Errorf("list: invalid JSON: %w", err)
	}
	return containers, nil
}

var _ = time.Second // kept for future per-op timeouts

package services

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// HTTPStatusError carries an HTTP status code alongside a message. It mirrors
// the convention used across the Overleaf Node.js microservices where an error
// is tagged with `err.info = { status: N }` and the handler writes that status
// back to the caller. Shared by all Go service conversions in this package so
// they can reuse one idiom without colliding.
type HTTPStatusError struct {
	Status int    // HTTP status to return (0 -> caller defaults to 500)
	Msg    string // human-readable message (without any prefix)
	Err    error  // optional underlying cause
}

// Error implements the error interface.
func (e *HTTPStatusError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "http status error"
}

// Unwrap supports errors.Is / errors.As unwrapping of the cause.
func (e *HTTPStatusError) Unwrap() error { return e.Err }

// HTTPStatus builds an *HTTPStatusError with the given code and message.
func HTTPStatus(status int, msg string) *HTTPStatusError {
	return &HTTPStatusError{Status: status, Msg: msg}
}

// HTTPStatusErr wraps a cause with an HTTP status.
func HTTPStatusErr(status int, msg string, err error) *HTTPStatusError {
	return &HTTPStatusError{Status: status, Msg: msg, Err: err}
}

// Status returns the HTTP status of err if it is (or wraps) an
// *HTTPStatusError, otherwise 0.
func StatusOf(err error) int {
	for err != nil {
		if se, ok := err.(*HTTPStatusError); ok {
			return se.Status
		}
		err = func() error {
			if u, ok := err.(interface{ Unwrap() error }); ok {
				return u.Unwrap()
			}
			return nil
		}()
	}
	return 0
}

// Message returns the message of err if it is (or wraps) an *HTTPStatusError,
// otherwise the raw Error() text.
func MessageOf(err error) string {
	for err != nil {
		if se, ok := err.(*HTTPStatusError); ok && se.Msg != "" {
			return se.Msg
		}
		err = func() error {
			if u, ok := err.(interface{ Unwrap() error }); ok {
				return u.Unwrap()
			}
			return nil
		}()
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// WriteStatus writes a plain-text "Error: <msg>" response with the given code,
// matching the Node.js proxy handlers that answer failures with
// `Content-Type: text/plain` and an `Error: ...` body.
func WriteStatus(w http.ResponseWriter, code int, msg string) {
	if code == 0 {
		code = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte("Error: " + msg))
}

// WritePlainText writes a plain-text body (no "Error:" prefix) with the code,
// used for the `Missing ?url parameter` style responses.
func WritePlainText(w http.ResponseWriter, code int, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(text))
}

// resolveHostDNS looks up all addresses for hostname using the system
// resolver. It is the default DNS backend for the proxy services and is
// overrideable in tests via the per-service config.
func resolveHostDNS(hostname string) ([]netip.Addr, error) {
	ips, err := net.LookupHost(hostname)
	if err != nil {
		return nil, err
	}
	out := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		a, perr := netip.ParseAddr(ip)
		if perr != nil {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// firstAddr returns the first address or a zero value plus a not-found flag.
func firstAddr(addrs []netip.Addr) (netip.Addr, bool) {
	if len(addrs) == 0 {
		return netip.Addr{}, false
	}
	return addrs[0], true
}

// containsAny reports whether any of the strings in list equals or is a
// substring prefix of s (helper for small allow-list checks).
func containsAny(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// fmtStatusErrorf is a convenience for building a *HTTPStatusError from a
// format string (kept here so the services do not each re-implement it).
func fmtStatusError(status int, format string, a ...any) *HTTPStatusError {
	return &HTTPStatusError{Status: status, Msg: fmt.Sprintf(format, a...)}
}

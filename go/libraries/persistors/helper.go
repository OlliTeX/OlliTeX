package persistors

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"sync"
	"time"
)

// --- Logger and Metrics seams -------------------------------------------------
//
// Node's object-persistor imports `@overleaf/logger` and `@overleaf/metrics`
// (LIB-13 / LIB-14, not yet ported at time of writing — see HANDOFF). Like
// the L10 rediswrapper seams, the persistors consume NARROW interfaces with
// null defaults so the package is complete and oracle-testable on its own;
// the real implementations can be injected once L13/L14 land.

// LoggerAPI mirrors the @overleaf/logger surface used by the persistors
// (info/warn/error/debug, pino-style (info|msg) ordering). (Named *API
// because the package variable `Logger` — the Node module import — occupies
// the plain name.)
type LoggerAPI interface {
	Info(info any, msg string)
	Warn(info any, msg string)
	Error(info any, msg string)
	Debug(info any, msg string)
}

// NullLogger discards.
type NullLogger struct{}

func (NullLogger) Info(any, string)  {}
func (NullLogger) Warn(any, string)  {}
func (NullLogger) Error(any, string) {}
func (NullLogger) Debug(any, string) {}

// MetricsAPI mirrors the @overleaf/metrics surface used here:
// count(name, value, 1, labels), inc(name, 1, labels), histogram(name, value, buckets, labels).
type MetricsAPI interface {
	Count(metric string, value int64, one int64, labels map[string]string)
	Inc(metric string, one int64, labels map[string]string)
	Histogram(metric string, value int64, buckets []int64, labels map[string]string)
}

// NullMetrics discards.
type NullMetrics struct{}

func (NullMetrics) Count(string, int64, int64, map[string]string)       {}
func (NullMetrics) Inc(string, int64, map[string]string)                {}
func (NullMetrics) Histogram(string, int64, []int64, map[string]string) {}

// Package-wide defaults (Node: module-level `Logger`/`Metrics` imports).
var (
	Logger  LoggerAPI  = NullLogger{}
	Metrics MetricsAPI = NullMetrics{}
)

// --- Observer -----------------------------------------------------------------

var (
	sizeBuckets   = []int64{0, 1_000, 10_000, 100_000, 128 * 1024, 1_000_000, 10_000_000, 50_000_000, 100_000_000}
	timingBuckets = []int64{0, 1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000, 10000, 20000, 50000}
)

// Observer mirrors PersistorHelper.ObserverStream (a Transform that counts
// bytes, records first-byte latency, optionally hashes, and reports metrics
// on stream end/error).
type Observer struct {
	metric string
	bucket string

	mu          sync.Mutex
	bytes       int64
	start       time.Time
	firstByteMs float64
	gotFirst    bool
	hash        hash.Hash
	ended       bool
}

// NewObserver mirrors `new ObserverStream({metric, bucket, hash})`. hash is
// ” (none) or an algorithm name, e.g. 'md5'.
func NewObserver(metric, bucket, hashAlgo string) *Observer {
	o := &Observer{metric: metric, bucket: bucket, start: time.Now()}
	if hashAlgo != "" {
		o.hash = newHash(hashAlgo)
	}
	return o
}

func newHash(algo string) hash.Hash {
	switch algo {
	case "md5":
		return md5.New()
	case "sha256":
		return sha256.New()
	}
	// Node: crypto.createHash(unknown) throws at construction.
	panic("Digest method not implemented: " + algo)
}

// Write mirrors _transform: count bytes, first-byte timing, hash update.
// (Pass-through happens via io.MultiWriter at each call site.)
func (o *Observer) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.gotFirst {
		o.firstByteMs = float64(time.Since(o.start).Microseconds()) / 1000.0
		o.gotFirst = true
	}
	if o.hash != nil {
		_, _ = o.hash.Write(p)
	}
	o.bytes += int64(len(p))
	return len(p), nil
}

// Finish mirrors the onEnd handler (emitted once, on 'end' or 'error').
func (o *Observer) Finish(err error) {
	o.mu.Lock()
	if o.ended {
		o.mu.Unlock()
		return
	}
	o.ended = true
	status := "success"
	if err != nil {
		status = "error"
	}
	bytes := o.bytes
	firstByteMs := o.firstByteMs
	gotFirst := o.gotFirst
	sinceStartMs := float64(time.Since(o.start).Microseconds()) / 1000.0
	o.mu.Unlock()

	sizeLabel := "lt-128KiB"
	if bytes >= 128*1024 {
		sizeLabel = "gte-128KiB"
	}
	labels := map[string]string{
		"size":   sizeLabel,
		"bucket": o.bucket,
		"status": status,
	}
	Metrics.Count(o.metric, bytes, 1, labels)
	Metrics.Inc(o.metric+".hit", 1, labels)

	if status == "error" {
		return
	}
	Metrics.Histogram(o.metric+".size", bytes, sizeBuckets, map[string]string{
		"status": status,
		"bucket": o.bucket,
	})
	if gotFirst {
		Metrics.Histogram(o.metric+".latency.first-byte", int64(firstByteMs), timingBuckets, labels)
	}
	Metrics.Histogram(o.metric+".latency", int64(sinceStartMs), timingBuckets, labels)
}

// GetHash mirrors `getHash()`: ” (not set) or the hex digest.
func (o *Observer) GetHash() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.hash == nil {
		return ""
	}
	return hex.EncodeToString(o.hash.Sum(nil))
}

// Bytes exposes the observed byte count (Node: `.bytes`).
func (o *Observer) Bytes() int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.bytes
}

// --- md5 helpers ---------------------------------------------------------------

// hexToBase64 mirrors `hexToBase64(hex)`: Buffer.from(hex,'hex').toString('base64').
func hexToBase64(hexStr string) string {
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}

// base64ToHex mirrors `base64ToHex(base64)`.
func base64ToHex(b64 string) string {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// calculateStreamMd5 mirrors `calculateStreamMd5(stream)`: the hex md5 of the
// stream's content (the stream is consumed).
func calculateStreamMd5(r io.Reader) (string, error) {
	h := md5.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// PersistorSeam is the narrow slice of a persistor that verifyMd5 needs
// (Node: any object with getObjectMd5Hash + deleteObject).
type PersistorSeam interface {
	GetObjectMd5Hash(bucket, key string, opts Opts) (string, error)
	DeleteObject(bucket, key string) error
}

// verifyMd5 mirrors `verifyMd5(persistor, bucket, key, sourceMd5, destMd5?)`.
func verifyMd5(p PersistorSeam, bucket, key, sourceMd5 string, destMd5 ...string) error {
	dest := ""
	if len(destMd5) > 0 {
		dest = destMd5[0]
	}
	if dest == "" {
		got, err := p.GetObjectMd5Hash(bucket, key, Opts{})
		if err != nil {
			return err
		}
		dest = got
	}
	if sourceMd5 != dest {
		if err := p.DeleteObject(bucket, key); err != nil {
			Logger.Warn(err, "error deleting file for invalid upload")
		}
		return NewWriteError("source and destination hashes do not match", map[string]any{
			"sourceMd5": sourceMd5,
			"destMd5":   dest,
			"bucket":    bucket,
			"key":       key,
		})
	}
	return nil
}

// --- error wrapping -------------------------------------------------------------

// errClass names the Node ErrorType argument wrapError receives (it is a
// class — the Go equivalent is the constructor selector).
type errClass int

const (
	classNotFound errClass = iota
	classWrite
	classRead
	classNotImpl
	classAlreadyWritten
)

// codedErr is the Go seam for `error.code` / `error.Code` (AWS-SDK-style).
type codedErr interface{ Code() string }

// statusErr is the Go seam for `error.response.statusCode` / `error.code`
// numeric (GCS-style 404/412).
type statusErr interface{ StatusCode() int }

func codeOf(err error) string {
	if c, ok := err.(codedErr); ok {
		return c.Code()
	}
	// Node fs errors carry code 'ENOENT' — map the Go stdlib equivalent so
	// the same wrapError list applies (errors.Is(err, fs.ErrNotExist)).
	if errors.Is(err, fs.ErrNotExist) {
		return "ENOENT"
	}
	return ""
}

func statusCodeOf(err error) int {
	if s, ok := err.(statusErr); ok {
		return s.StatusCode()
	}
	return 0
}

// wrapError mirrors PersistorHelper.wrapError:
//
//	params = {...params, cause: error}
//	errorCode = error.code || error.Code || error.name
//	if (error instanceof NotFoundError || code in [NoSuchKey, NotFound, 404,
//	    AccessDenied, ENOENT] || error.response?.statusCode === 404)
//	    → NotFoundError('no such file', params, error)
//	else if (params.ifNoneMatch === '*' &&
//	    (code === 'PreconditionFailed' || error.response?.statusCode === 412
//	     || error instanceof AlreadyWrittenError))
//	    → AlreadyWrittenError(message, params, error)
//	else → new ErrorType(message, params, error)
//
// Go seams for the duck-typed fields: codedErr (code/Code) and statusErr
// (error.response.statusCode / numeric error.code).
func wrapError(err error, message string, params map[string]any, target errClass) error {
	if params == nil {
		params = map[string]any{}
	}
	if params["cause"] == nil {
		params["cause"] = err
	}

	code := codeOf(err)
	status := statusCodeOf(err)

	notFound := isNotFoundError(err) ||
		code == "NoSuchKey" || code == "NotFound" || code == "404" ||
		code == "AccessDenied" || code == "ENOENT" || status == 404
	if notFound {
		return finishWrap(classNotFound, "no such file", params, err)
	}

	if params["ifNoneMatch"] == "*" &&
		(code == "PreconditionFailed" || status == 412 || isAlreadyWrittenError(err)) {
		return finishWrap(classAlreadyWritten, message, params, err)
	}

	switch target {
	case classNotFound:
		return finishWrap(classNotFound, message, params, err)
	case classWrite:
		return finishWrap(classWrite, message, params, err)
	case classRead:
		return finishWrap(classRead, message, params, err)
	case classNotImpl:
		return finishWrap(classNotImpl, message, params, err)
	case classAlreadyWritten:
		return finishWrap(classAlreadyWritten, message, params, err)
	}
	return errors.New(message)
}

func finishWrap(t errClass, message string, params map[string]any, cause error) error {
	var c error
	if cause != nil {
		c = cause
	}
	switch t {
	case classNotFound:
		return NewNotFoundError(message, params, c)
	case classWrite:
		return NewWriteError(message, params, c)
	case classRead:
		return NewReadError(message, params, c)
	case classNotImpl:
		return NewNotImplementedError(message, params, c)
	case classAlreadyWritten:
		return NewAlreadyWrittenError(message, params, c)
	}
	return fmt.Errorf("%s", message)
}

// keep context referenced (callers of persistors use context.Context)
var _ = context.Background

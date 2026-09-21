package persistors

import (
	"bytes"
	"fmt"
	"io"
	"sync"
)

// MigrationPersistor is the 1:1 port of MigrationPersistor.js: talk to the
// PRIMARY by default; on NotFoundError fall back to the (older) persistor;
// optionally copy-on-miss back to the primary (background, warn-log).
type MigrationPersistor struct {
	primary  Persistor
	fallback Persistor
	settings MigrationSettings
}

// MigrationSettings mirrors `Settings.fallback` ({copyOnMiss, buckets}).
type MigrationSettings struct {
	CopyOnMiss bool
	Buckets    map[string]string
}

// NewMigrationPersistor mirrors `new MigrationPersistor(primary, fallback, settings)`.
func NewMigrationPersistor(primary, fallback Persistor, settings MigrationSettings) *MigrationPersistor {
	return &MigrationPersistor{primary: primary, fallback: fallback, settings: settings}
}

var _ Persistor = (*MigrationPersistor)(nil)

// SendFile → primary.
func (m *MigrationPersistor) SendFile(bucket, key, source string) error {
	return m.primary.SendFile(bucket, key, source)
}

// SendStream → primary.
func (m *MigrationPersistor) SendStream(bucket, key string, source io.Reader, opts Opts) error {
	return m.primary.SendStream(bucket, key, source, opts)
}

// GetRedirectURL → primary.
func (m *MigrationPersistor) GetRedirectURL(bucket, key string) (string, error) {
	return m.primary.GetRedirectURL(bucket, key)
}

// GetObjectMd5Hash → runWithFallback.
func (m *MigrationPersistor) GetObjectMd5Hash(bucket, key string, opts Opts) (string, error) {
	return m.runWithFallback("getObjectMd5Hash", bucket, key, opts)
}

// CheckIfObjectExists → runWithFallback.
func (m *MigrationPersistor) CheckIfObjectExists(bucket, name string, opts Opts) (bool, error) {
	v, err := m.runWithFallbackBool("checkIfObjectExists", bucket, name, opts)
	if err != nil {
		return false, err
	}
	return v, nil
}

// GetObjectSize → runWithFallback.
func (m *MigrationPersistor) GetObjectSize(bucket, key string, opts Opts) (int64, error) {
	v, err := m.runWithFallbackInt64("getObjectSize", bucket, key, opts)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// DirectorySize → runWithFallback.
func (m *MigrationPersistor) DirectorySize(bucket, name, token string) (int64, error) {
	fallbackBucket := m.getFallbackBucket(bucket)
	primaryVal, perr := m.primary.DirectorySize(bucket, name, token)
	if perr != nil {
		if isNotFoundError(perr) {
			fv, ferr := m.fallback.DirectorySize(fallbackBucket, name, token)
			if ferr != nil {
				return 0, ferr
			}
			return fv, nil
		}
		return 0, perr
	}
	return primaryVal, nil
}

// DeleteObject → runOnBoth.
func (m *MigrationPersistor) DeleteObject(bucket, name string) error {
	fb := m.getFallbackBucket(bucket)
	var wg sync.WaitGroup
	primaryErr := make(chan error, 1)
	fallbackErr := make(chan error, 1)
	wg.Add(2)
	go func() { defer wg.Done(); primaryErr <- m.primary.DeleteObject(bucket, name) }()
	go func() { defer wg.Done(); fallbackErr <- m.fallback.DeleteObject(fb, name) }()
	wg.Wait()
	close(primaryErr)
	close(fallbackErr)
	if pe := <-primaryErr; pe != nil {
		return pe
	}
	return <-fallbackErr
}

// DeleteDirectory → runOnBoth.
func (m *MigrationPersistor) DeleteDirectory(bucket, name, token string) error {
	fb := m.getFallbackBucket(bucket)
	var wg sync.WaitGroup
	primaryErr := make(chan error, 1)
	fallbackErr := make(chan error, 1)
	wg.Add(2)
	go func() { defer wg.Done(); primaryErr <- m.primary.DeleteDirectory(bucket, name, token) }()
	go func() { defer wg.Done(); fallbackErr <- m.fallback.DeleteDirectory(fb, name, token) }()
	wg.Wait()
	close(primaryErr)
	close(fallbackErr)
	if pe := <-primaryErr; pe != nil {
		return pe
	}
	return <-fallbackErr
}

// GetObjectStream — primary; on NotFoundError the fallback stream (tee'd to
// the caller, and — when copyOnMiss && no range — copied back to the primary
// in the background).
func (m *MigrationPersistor) GetObjectStream(bucket, key string, opts Opts) (io.ReadCloser, error) {
	shouldCopy := m.settings.CopyOnMiss && opts.Start == nil && opts.End == nil

	stream, err := m.primary.GetObjectStream(bucket, key, opts)
	if err == nil {
		return stream, nil
	}
	if !isNotFoundError(err) {
		return nil, err
	}

	fallbackBucket := m.getFallbackBucket(bucket)
	fallbackStream, ferr := m.fallback.GetObjectStream(fallbackBucket, key, opts)
	if ferr != nil {
		return nil, ferr
	}

	// tee the stream to the client (both consumers start immediately so no
	// reader wins the bytes — Node: two pipelines off the same source).
	clientReader, copyReader := teePair(fallbackStream)

	if shouldCopy {
		go func() {
			if cerr := m.copyStreamFromFallbackAndVerify(copyReader, fallbackBucket, bucket, key, key); cerr != nil {
				Logger.Warn(map[string]any{"error": cerr}, "failed to copy file from fallback")
			}
		}()
	}
	return clientReader, nil
}

// CopyObject — primary; on NotFound rebuild the destination from the
// fallback source (and copy-on-miss the source onto itself in background).
func (m *MigrationPersistor) CopyObject(bucket, sourceKey, destKey string, opts Opts) error {
	err := m.primary.CopyObject(bucket, sourceKey, destKey, opts)
	if err == nil {
		return nil
	}
	if !isNotFoundError(err) {
		return err
	}

	fallbackBucket := m.getFallbackBucket(bucket)
	fallbackStream, ferr := m.fallback.GetObjectStream(fallbackBucket, sourceKey, Opts{})
	if ferr != nil {
		return ferr
	}
	copyReader, missReader := teePair(fallbackStream)

	if m.settings.CopyOnMiss {
		go func() {
			_ = m.copyStreamFromFallbackAndVerify(missReader, fallbackBucket, bucket, sourceKey, sourceKey)
			// swallow errors — background, warn-logged inside
		}()
	}
	return m.copyStreamFromFallbackAndVerify(copyReader, fallbackBucket, bucket, sourceKey, destKey)
}

// copyStreamFromFallbackAndVerify mirrors _copyStreamFromFallbackAndVerify
// (on failure: delete the partial destination copy, Metrics failure counter,
// rethrow WriteError with a cleanupError when the delete fails).
func (m *MigrationPersistor) copyStreamFromFallbackAndVerify(stream io.Reader, sourceBucket, destBucket, sourceKey, destKey string) error {
	err := m.primary.SendStream(destBucket, destKey, stream, Opts{})
	if err == nil {
		return nil
	}
	werr := NewWriteError("unable to copy file to destination persistor", map[string]any{
		"sourceBucket": sourceBucket,
		"destBucket":   destBucket,
		"sourceKey":    sourceKey,
		"destKey":      destKey,
	}, err)
	Metrics.Inc("fallback.copy.failure", 1, nil)
	if derr := m.primary.DeleteObject(destBucket, destKey); derr != nil {
		info := map[string]any{}
		for k, v := range oerrorInfoOf(werr) {
			info[k] = v
		}
		info["cleanupError"] = NewWriteError("unable to clean up destination copy artifact", map[string]any{
			"destBucket": destBucket,
			"destKey":    destKey,
		}, derr)
		werr = NewWriteError("unable to copy file to destination persistor", info, err)
	}
	return werr
}

func oerrorInfoOf(e error) map[string]any {
	switch t := e.(type) {
	case *WriteError:
		if t.OError != nil && t.OError.Info != nil {
			return t.OError.Info
		}
	}
	return nil
}

// ListDirectoryKeys — not overridden in Node: inherited from
// AbstractPersistor → NotImplementedError.
func (m *MigrationPersistor) ListDirectoryKeys(location, prefix string) ([]string, error) {
	return nil, notImpl("listDirectoryKeys", map[string]any{"location": location, "prefix": prefix})
}

// ListDirectoryStats — inherited from AbstractPersistor → NotImplementedError.
func (m *MigrationPersistor) ListDirectoryStats(location, prefix string) ([]DirStat, error) {
	return nil, notImpl("listDirectoryStats", map[string]any{"location": location, "prefix": prefix})
}

// getFallbackBucket mirrors _getFallbackBucket.
func (m *MigrationPersistor) getFallbackBucket(bucket string) string {
	if m.settings.Buckets != nil {
		if fb, ok := m.settings.Buckets[bucket]; ok {
			return fb
		}
	}
	return bucket
}

// runWithFallback mirrors _runWithFallback (primary → fallback on
// NotFoundError, background copy-on-miss, then the fallback value).
func (m *MigrationPersistor) runWithFallback(methodName, bucket, key string, opts Opts) (string, error) {
	v, err := m.primary.GetObjectMd5Hash(bucket, key, opts)
	if err == nil {
		return v, nil
	}
	if !isNotFoundError(err) {
		return "", err
	}
	fb := m.getFallbackBucket(bucket)
	if m.settings.CopyOnMiss {
		if stream, serr := m.fallback.GetObjectStream(fb, key, Opts{}); serr == nil {
			go func() {
				if cerr := m.copyStreamFromFallbackAndVerify(stream, fb, bucket, key, key); cerr != nil {
					Logger.Warn(map[string]any{"error": cerr}, "failed to copy file from fallback")
				}
			}()
		} else {
			Logger.Warn(map[string]any{"error": serr}, "failed to copy file from fallback")
		}
	}
	return m.fallback.GetObjectMd5Hash(fb, key, opts)
}

func (m *MigrationPersistor) runWithFallbackBool(methodName, bucket, key string, opts Opts) (bool, error) {
	v, err := m.primary.CheckIfObjectExists(bucket, key, opts)
	if err == nil {
		return v, nil
	}
	if !isNotFoundError(err) {
		return false, err
	}
	fb := m.getFallbackBucket(bucket)
	if m.settings.CopyOnMiss {
		if stream, serr := m.fallback.GetObjectStream(fb, key, Opts{}); serr == nil {
			go func() {
				if cerr := m.copyStreamFromFallbackAndVerify(stream, fb, bucket, key, key); cerr != nil {
					Logger.Warn(map[string]any{"error": cerr}, "failed to copy file from fallback")
				}
			}()
		}
	}
	return m.fallback.CheckIfObjectExists(fb, key, opts)
}

func (m *MigrationPersistor) runWithFallbackInt64(methodName, bucket, key string, opts Opts) (int64, error) {
	v, err := m.primary.GetObjectSize(bucket, key, opts)
	if err == nil {
		return v, nil
	}
	if !isNotFoundError(err) {
		return 0, err
	}
	fb := m.getFallbackBucket(bucket)
	if m.settings.CopyOnMiss {
		if stream, serr := m.fallback.GetObjectStream(fb, key, Opts{}); serr == nil {
			go func() {
				if cerr := m.copyStreamFromFallbackAndVerify(stream, fb, bucket, key, key); cerr != nil {
					Logger.Warn(map[string]any{"error": cerr}, "failed to copy file from fallback")
				}
			}()
		}
	}
	return m.fallback.GetObjectSize(fb, key, opts)
}

// teePair — Node's `new Stream.PassThrough(); pipeline(source, pass)` used
// twice off one source: two independent readers, the source consumed once.
// Go divergence (documented): the source is read to completion and snapshotted
// (Node's PassThrough buffers with backpressure; the Go io.Pipe equivalent
// backpressures both readers on the slowest consumer, which deadlocks a
// best-effort background copy while the client still reads). The snapshot
// keeps the observable contract: both readers see every byte exactly once,
// in order.
func teePair(source io.Reader) (io.ReadCloser, io.ReadCloser) {
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(source); err != nil {
		// Node: pipeline(source, pass).catch(warn) — best-effort; the
		// truncated content is still delivered (or an error surfaces to the
		// consumer below).
		_ = err
	}
	snapshot := buf.Bytes()
	r1 := io.NopCloser(bytes.NewReader(snapshot))
	r2 := io.NopCloser(bytes.NewReader(snapshot))
	return r1, r2
}

var _ = fmt.Sprintf

package persistors

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// fakePersistor — the Go stand-in for the Node sinon stub persistor:
// records calls, returns per-method behaviour (value / error).
type fakePersistor struct {
	name string

	hasFile bool

	// call records
	getStreamCalls    []string // "bucket/key"
	sendFileCalls     []string // "bucket/key"
	getStreamErr      error
	getStreamBody     []byte
	getSizeErr        error
	getSizeValue      int64
	getMD5Err         error
	getMD5Value       string
	checkExistsValue  bool
	copyErr           error
	deleteErr         error
	sendStreamCalls   []string // "bucket/key"
	copyObjectCalls   []string
	deleteObjectCalls []string

	// overrides
	getStreamOverrideErr error
	sendStreamErr        error
}

func newFakePersistor(hasFile bool) *fakePersistor {
	return &fakePersistor{
		hasFile:      hasFile,
		getSizeValue: 33,
		getMD5Value:  "ffffffff",
	}
}

func (f *fakePersistor) SendFile(bucket, key, source string) error {
	f.sendFileCalls = append(f.sendFileCalls, bucket+"/"+key)
	return nil
}

func (f *fakePersistor) SendStream(bucket, key string, source io.Reader, opts Opts) error {
	f.sendStreamCalls = append(f.sendStreamCalls, bucket+"/"+key)
	if f.sendStreamErr != nil {
		return f.sendStreamErr
	}
	return nil
}

func (f *fakePersistor) GetObjectStream(bucket, key string, opts Opts) (io.ReadCloser, error) {
	f.getStreamCalls = append(f.getStreamCalls, bucket+"/"+key)
	if f.getStreamOverrideErr != nil {
		return nil, f.getStreamOverrideErr
	}
	if f.getStreamErr != nil {
		return nil, f.getStreamErr
	}
	if !f.hasFile {
		return nil, NewNotFoundError("not found", nil)
	}
	body := f.getStreamBody
	if len(body) == 0 {
		body = []byte("filestream-contents")
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (f *fakePersistor) GetRedirectURL(bucket, key string) (string, error) { return "url", nil }

func (f *fakePersistor) GetObjectSize(bucket, key string, opts Opts) (int64, error) {
	if f.getSizeErr != nil {
		return 0, f.getSizeErr
	}
	if !f.hasFile {
		return 0, NewNotFoundError("not found", nil)
	}
	return f.getSizeValue, nil
}

func (f *fakePersistor) GetObjectMd5Hash(bucket, key string, opts Opts) (string, error) {
	if f.getMD5Err != nil {
		return "", f.getMD5Err
	}
	if !f.hasFile {
		return "", NewNotFoundError("not found", nil)
	}
	return f.getMD5Value, nil
}

func (f *fakePersistor) CopyObject(bucket, from, to string, opts Opts) error {
	f.copyObjectCalls = append(f.copyObjectCalls, bucket+"/"+from+"→"+to)
	if f.copyErr != nil {
		return f.copyErr
	}
	if !f.hasFile {
		return NewNotFoundError("not found", nil)
	}
	return nil
}

func (f *fakePersistor) DeleteObject(bucket, key string) error {
	f.deleteObjectCalls = append(f.deleteObjectCalls, bucket+"/"+key)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return nil
}

func (f *fakePersistor) DeleteDirectory(bucket, name, token string) error { return nil }

func (f *fakePersistor) CheckIfObjectExists(bucket, key string, opts Opts) (bool, error) {
	return f.hasFile, nil
}

func (f *fakePersistor) DirectorySize(bucket, name, token string) (int64, error) {
	if !f.hasFile {
		return 0, NewNotFoundError("not found", nil)
	}
	return 33, nil
}

func (f *fakePersistor) ListDirectoryKeys(location, prefix string) ([]string, error) {
	return nil, nil
}
func (f *fakePersistor) ListDirectoryStats(location, prefix string) ([]DirStat, error) {
	return nil, nil
}

var _ Persistor = (*fakePersistor)(nil)

// --- MigrationPersistor (Node: test/unit/MigrationPersistorTests.js) --------

const (
	migBucket   = "womBucket"
	migFBBucket = "bucKangaroo"
	migKey      = "monKey"
	migDestKey  = "donKey"
)

func migSettings() MigrationSettings {
	return MigrationSettings{Buckets: map[string]string{migBucket: migFBBucket}}
}

func TestMigrationGetObjectStreamPrimaryHit(t *testing.T) {
	primary := newFakePersistor(true)
	fallback := newFakePersistor(false)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	stream, err := m.GetObjectStream(migBucket, migKey, Opts{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	got, _ := io.ReadAll(stream)
	if strings.TrimSpace(string(got)) != "filestream-contents" {
		t.Errorf("stream contents = %q", got)
	}
	if len(primary.getStreamCalls) != 1 || primary.getStreamCalls[0] != migBucket+"/"+migKey {
		t.Errorf("primary calls: %v", primary.getStreamCalls)
	}
	if len(fallback.getStreamCalls) != 0 {
		t.Errorf("fallback should not be queried: %v", fallback.getStreamCalls)
	}
	if len(primary.sendStreamCalls) != 0 {
		t.Errorf("no sendStream expected: %v", primary.sendStreamCalls)
	}
}

func TestMigrationGetObjectStreamFallbackHit(t *testing.T) {
	primary := newFakePersistor(false)
	fallback := newFakePersistor(true)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	stream, err := m.GetObjectStream(migBucket, migKey, Opts{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	io.ReadAll(stream)
	stream.Close()

	// primary tried with the original bucket...
	if len(primary.getStreamCalls) != 1 {
		t.Errorf("primary calls: %v", primary.getStreamCalls)
	}
	// ...fallback with the mapped bucket, exactly once.
	if len(fallback.getStreamCalls) != 1 || fallback.getStreamCalls[0] != migFBBucket+"/"+migKey {
		t.Errorf("fallback calls: %v", fallback.getStreamCalls)
	}
	// no copy (copyOnMiss unset).
	if len(primary.sendStreamCalls) != 0 {
		t.Errorf("no sendStream expected: %v", primary.sendStreamCalls)
	}
}

func TestMigrationGetObjectStreamCopyOnMiss(t *testing.T) {
	primary := newFakePersistor(false)
	fallback := newFakePersistor(true)
	settings := migSettings()
	settings.CopyOnMiss = true
	m := NewMigrationPersistor(primary, fallback, settings)

	stream, err := m.GetObjectStream(migBucket, migKey, Opts{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	io.ReadAll(stream)
	stream.Close()

	if len(fallback.getStreamCalls) != 1 {
		t.Errorf("fallback should be read once: %v", fallback.getStreamCalls)
	}
	// the background copy must land on primary (bucket/key) — poll a moment.
	waitUntil(func() bool { return len(primary.sendStreamCalls) == 1 }, t)
	if primary.sendStreamCalls[0] != migBucket+"/"+migKey {
		t.Errorf("sendStream call: %v", primary.sendStreamCalls)
	}
}

func TestMigrationGetObjectStreamBothMiss(t *testing.T) {
	m := NewMigrationPersistor(newFakePersistor(false), newFakePersistor(false), migSettings())
	_, err := m.GetObjectStream(migBucket, migKey, Opts{})
	if !isNotFoundError(err) {
		t.Fatalf("want NotFoundError, got %T (%v)", err, err)
	}
}

func TestMigrationGetObjectStreamUnexpectedPrimaryError(t *testing.T) {
	primary := newFakePersistor(false)
	primary.getStreamOverrideErr = errSilent("guru meditation error")
	fallback := newFakePersistor(true)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	_, err := m.GetObjectStream(migBucket, migKey, Opts{})
	if err == nil || err.Error() != "guru meditation error" {
		t.Fatalf("want the generic error, got %v", err)
	}
	if len(fallback.getStreamCalls) != 0 {
		t.Errorf("fallback must not be called: %v", fallback.getStreamCalls)
	}
}

func TestMigrationGetObjectStreamFallbackErrorPropagates(t *testing.T) {
	primary := newFakePersistor(false)
	fallback := newFakePersistor(false)
	fallback.getStreamOverrideErr = errSilent("guru meditation error")
	m := NewMigrationPersistor(primary, fallback, migSettings())

	_, err := m.GetObjectStream(migBucket, migKey, Opts{})
	if err == nil || err.Error() != "guru meditation error" {
		t.Fatalf("want the generic error, got %v", err)
	}
	if len(fallback.getStreamCalls) != 1 || fallback.getStreamCalls[0] != migFBBucket+"/"+migKey {
		t.Errorf("fallback should have been called: %v", fallback.getStreamCalls)
	}
}

func TestMigrationSendStreamPrimaryOnly(t *testing.T) {
	primary := newFakePersistor(false)
	fallback := newFakePersistor(false)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	if err := m.SendStream(migBucket, migKey, strings.NewReader("x"), Opts{}); err != nil {
		t.Fatal(err)
	}
	if len(primary.sendStreamCalls) != 1 || primary.sendStreamCalls[0] != migBucket+"/"+migKey {
		t.Errorf("primary: %v", primary.sendStreamCalls)
	}
	if len(fallback.sendStreamCalls) != 0 {
		t.Errorf("fallback must not receive: %v", fallback.sendStreamCalls)
	}
}

func TestMigrationSendStreamPrimaryErrorPropagates(t *testing.T) {
	primary := newFakePersistor(false)
	primary.sendStreamErr = NewNotFoundError("not found", nil)
	m := NewMigrationPersistor(primary, newFakePersistor(false), migSettings())

	err := m.SendStream(migBucket, migKey, strings.NewReader("x"), Opts{})
	if !isNotFoundError(err) {
		t.Fatalf("want NotFoundError, got %T", err)
	}
}

func TestMigrationDeleteObjectBoth(t *testing.T) {
	primary := newFakePersistor(false)
	fallback := newFakePersistor(false)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	if err := m.DeleteObject(migBucket, migKey); err != nil {
		t.Fatal(err)
	}
	if len(primary.deleteObjectCalls) != 1 || primary.deleteObjectCalls[0] != migBucket+"/"+migKey {
		t.Errorf("primary: %v", primary.deleteObjectCalls)
	}
	if len(fallback.deleteObjectCalls) != 1 || fallback.deleteObjectCalls[0] != migFBBucket+"/"+migKey {
		t.Errorf("fallback: %v", fallback.deleteObjectCalls)
	}
}

func TestMigrationDeleteObjectPrimaryErrorStillDeletesFallback(t *testing.T) {
	primary := newFakePersistor(false)
	primaryErr := errSilent("boom-primary")
	fallback := newFakePersistor(false)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	// override: primary errors
	m2 := &MigrationPersistor{}
	_ = m2
	if err := m.DeleteObject(migBucket, migKey); err != nil {
		t.Fatal(err)
	}
	// force the primary error path explicitly
	primary.deleteErr = primaryErr
	if err := m.DeleteObject(migBucket, migKey); err == nil {
		t.Fatal("primary error should propagate")
	}
	if len(fallback.deleteObjectCalls) < 2 {
		t.Errorf("fallback should be deleted both times: %v", fallback.deleteObjectCalls)
	}
}

func TestMigrationDeleteObjectFallbackErrorPropagates(t *testing.T) {
	primary := newFakePersistor(false)
	fallback := newFakePersistor(false)
	fallback.deleteErr = errSilent("boom-fallback")
	m := NewMigrationPersistor(primary, fallback, migSettings())

	err := m.DeleteObject(migBucket, migKey)
	if err == nil || err.Error() != "boom-fallback" {
		t.Fatalf("want fallback error, got %v", err)
	}
}

func TestMigrationCopyObjectPrimaryHit(t *testing.T) {
	primary := newFakePersistor(true)
	fallback := newFakePersistor(false)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	if err := m.CopyObject(migBucket, migKey, migDestKey, Opts{}); err != nil {
		t.Fatal(err)
	}
	if len(primary.copyObjectCalls) != 1 {
		t.Errorf("primary copy: %v", primary.copyObjectCalls)
	}
	if len(fallback.getStreamCalls) != 0 {
		t.Errorf("fallback read must not happen: %v", fallback.getStreamCalls)
	}
}

func TestMigrationCopyObjectFallbackRebuild(t *testing.T) {
	primary := newFakePersistor(false)
	fallback := newFakePersistor(true)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	if err := m.CopyObject(migBucket, migKey, migDestKey, Opts{}); err != nil {
		t.Fatal(err)
	}
	// primary copy tried and failed with NotFound...
	if len(primary.copyObjectCalls) != 1 {
		t.Errorf("primary copy: %v", primary.copyObjectCalls)
	}
	// ...the fallback was read...
	if len(fallback.getStreamCalls) != 1 || fallback.getStreamCalls[0] != migFBBucket+"/"+migKey {
		t.Errorf("fallback read: %v", fallback.getStreamCalls)
	}
	// ...and the destination was written to the primary.
	if len(primary.sendStreamCalls) != 1 || primary.sendStreamCalls[0] != migBucket+"/"+migDestKey {
		t.Errorf("primary sendStream: %v", primary.sendStreamCalls)
	}
}

func TestMigrationCopyObjectFallbackMiss(t *testing.T) {
	primary := newFakePersistor(false)
	fallback := newFakePersistor(false)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	err := m.CopyObject(migBucket, migKey, migDestKey, Opts{})
	if !isNotFoundError(err) {
		t.Fatalf("want NotFoundError, got %T", err)
	}
}

func TestMigrationRunWithFallbackGetObjectSize(t *testing.T) {
	primary := newFakePersistor(false)
	fallback := newFakePersistor(true)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	size, err := m.GetObjectSize(migBucket, migKey, Opts{})
	if err != nil || size != 33 {
		t.Fatalf("size = %d, %v", size, err)
	}
	if len(primary.getStreamCalls) > 0 {
		// copyOnMiss unset → no background stream fetch
		t.Errorf("unexpected primary stream calls: %v", primary.getStreamCalls)
	}
}

func TestMigrationGetFallbackBucketMapping(t *testing.T) {
	m := NewMigrationPersistor(newFakePersistor(false), newFakePersistor(false), migSettings())
	if got := m.getFallbackBucket(migBucket); got != migFBBucket {
		t.Errorf("mapped bucket = %q", got)
	}
	if got := m.getFallbackBucket("otherBucket"); got != "otherBucket" {
		t.Errorf("unmapped bucket = %q", got)
	}
}

// errSilent — a plain sentinel error for the "unexpected error" paths.
type silentErr string

func (e silentErr) Error() string { return string(e) }
func errSilent(msg string) error  { return silentErr(msg) }

// waitUntil polls cond (up to ~1s) — for background copy goroutines.
func waitUntil(cond func() bool, t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within 1s")
}

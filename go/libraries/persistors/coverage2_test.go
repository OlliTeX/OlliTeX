package persistors

import (
	"io"
	"os"
	"strings"
	"testing"

	"ollitex/go/libraries/oerror"
)

// Second coverage supplement: the remaining defensive branches and a few
// Node-parity pins that the first pass did not reach.

// --- factory -----------------------------------------------------------------

func TestFactoryFallbackUnknownBackend(t *testing.T) {
	_, err := Create(Settings{
		Backend:  "fs",
		Fallback: &FallbackSettings{Backend: "magic"},
	}, Adapters{})
	if err == nil || !strings.Contains(err.Error(), "unknown backend") {
		t.Fatalf("want unknown backend, got %v", err)
	}
}

// --- S3 branches ---------------------------------------------------------------

func newErringS3Factory(t *testing.T) func(bucket string) (S3Client, error) {
	return func(bucket string) (S3Client, error) {
		if bucket == "bad-bucket" {
			return nil, plainErr("no S3 client factory configured for bucket bad-bucket")
		}
		return newFakeS3(t), nil
	}
}

func TestS3FactoryBucketErrorSurfaces(t *testing.T) {
	p := NewS3Persistor(S3Settings{Key: "frog", Secret: "prince"}, newErringS3Factory(t))

	if _, err := p.GetObjectStream("bad-bucket", "k", Opts{}); err == nil {
		t.Fatal("want client-factory error")
	}
	if _, err := p.GetRedirectURL("bad-bucket", "k"); err == nil {
		t.Fatal("want client-factory error")
	}
	if _, err := p.GetObjectSize("bad-bucket", "k", Opts{}); err == nil {
		t.Fatal("want client-factory error")
	}
	if err := p.DeleteObject("bad-bucket", "k"); err == nil {
		t.Fatal("want client-factory error")
	}
	if err := p.DeleteDirectory("bad-bucket", "k", ""); err == nil {
		t.Fatal("want client-factory error")
	}
	if err := p.SendStream("bad-bucket", "k", strings.NewReader("x"), Opts{}); err == nil {
		t.Fatal("want client-factory error")
	}
	if _, err := p.ListDirectoryKeys("bad-bucket", "k"); err == nil {
		t.Fatal("want client-factory error")
	}
	if _, err := p.ListDirectoryStats("bad-bucket", "k"); err == nil {
		t.Fatal("want client-factory error")
	}
	if _, err := p.CheckIfObjectExists("bad-bucket", "k", Opts{}); err == nil {
		t.Fatal("want client-factory error")
	}
}

func TestS3SendFileMissingSource(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)
	err := p.SendFile(s3Bucket, s3Key, "/no/such/source")
	if err == nil {
		t.Fatal("want error")
	}
}

func TestS3GetObjectMd5HashHeadError(t *testing.T) {
	f := newFakeS3(t)
	f.heads = append(f.heads, fakeS3Outcome{err: plainErr("head boom")})
	p := newTestS3Persistor(t, S3Settings{}, f)

	_, err := p.GetObjectMd5Hash(s3Bucket, s3Key, Opts{})
	if !strings.Contains(err.Error(), "error getting hash of s3 object") {
		t.Fatalf("message: %v", err)
	}
}

func TestS3GetObjectMd5HashDownloadError(t *testing.T) {
	f := newFakeS3(t)
	f.heads = append(f.heads, fakeS3Outcome{head: &HeadObjectResult{ETag: "multiparty"}})
	f.gets = append(f.gets, fakeS3Outcome{get: &GetObjectResult{Body: &gcsBoomBody{}}})
	p := newTestS3Persistor(t, S3Settings{}, f)

	_, err := p.GetObjectMd5Hash(s3Bucket, s3Key, Opts{})
	if err == nil {
		t.Fatal("want error from failing stream")
	}
}

func TestS3ListErrorMidPagination(t *testing.T) {
	// a fresh fake per consumer: the call queue is per-fake
	newPaged := func() *S3Persistor {
		fq := newFakeS3(t)
		fq.lists = append(fq.lists,
			fakeS3Outcome{list: &ListObjectsV2Result{
				Contents: []S3Object{{Key: "a"}}, IsTruncated: true, NextContinuationToken: "T",
			}},
			fakeS3Outcome{err: plainErr("boom list")},
		)
		return newTestS3Persistor(t, S3Settings{}, fq)
	}

	if _, err := newPaged().ListDirectoryKeys(s3Bucket, s3Key); err == nil {
		t.Fatal("want pagination error on keys")
	}
	if _, err := newPaged().ListDirectoryStats(s3Bucket, s3Key); err == nil {
		t.Fatal("want pagination error on stats")
	}
}

func TestApplyDeleteObjectsMd5FallbackNoBody(t *testing.T) {
	out := applyDeleteObjectsMd5Fallback(map[string]string{"Accept": "*/*"}, nil)
	if _, present := out["Content-MD5"]; present {
		t.Fatal("no body → no Content-MD5")
	}
	if out["Accept"] != "*/*" {
		t.Fatalf("headers: %v", out)
	}
}

// --- GCS branches ---------------------------------------------------------------

func TestGCSNilFactory(t *testing.T) {
	_, err := NewGcsPersistor(GCSSettings{}, nil)
	if err == nil || !strings.Contains(err.Error(), "no GCS storage factory configured") {
		t.Fatalf("got %v", err)
	}
}

func TestGCSIdempotencyIntAccepted(t *testing.T) {
	_, err := NewGcsPersistor(GCSSettings{
		RetryOptions: map[string]any{"idempotencyStrategy": 3},
	}, func(map[string]any) (GCSStorage, error) { return newFakeGCS(), nil })
	if err != nil {
		t.Fatalf("int strategy must be accepted: %v", err)
	}
}

func TestGCSSendFileMissing(t *testing.T) {
	gcs := newFakeGCS()
	p := newGcsTest(t, gcsSettings(), gcs)
	err := p.SendFile(gcsBucket, gcsKey, "/no/such/source")
	if err == nil {
		t.Fatal("want error")
	}
	if !isNotFoundError(err) {
		t.Fatalf("ENOENT source must map to NotFoundError, got %T (%v)", err, err)
	}
}

func TestGCSSendStreamCloseError(t *testing.T) {
	gcs := newFakeGCS()
	file := gcs.put(gcsBucket, gcsKey, "x")
	file.writeCloseErr = plainErr("close failed")
	p := newGcsTest(t, gcsSettings(), gcs)

	err := p.SendStream(gcsBucket, gcsKey, strings.NewReader("x"), Opts{})
	if !strings.Contains(err.Error(), "upload to GCS failed") {
		t.Fatalf("want upload failure, got %v", err)
	}
}

func TestGCSSendStreamSourceError(t *testing.T) {
	gcs := newFakeGCS()
	p := newGcsTest(t, gcsSettings(), gcs)

	err := p.SendStream(gcsBucket, gcsKey, errReader{}, Opts{})
	if !strings.Contains(err.Error(), "upload to GCS failed") {
		t.Fatalf("want upload failure, got %v", err)
	}
}

func TestGCSSendStreamMD5VerifyFailure(t *testing.T) {
	gcs := newFakeGCS()
	file := gcs.put(gcsBucket, gcsKey, "contents")
	// force a mismatch: the fake GetMetadata returns the REAL md5, so write
	// different content than what the metadata will report.
	file.gunzipBody = nil
	file.metaMD5Override = "aa"
	p := newGcsTest(t, gcsSettings(), gcs)

	err := p.SendStream(gcsBucket, gcsKey, strings.NewReader("other contents"), Opts{})
	if err == nil || !strings.Contains(err.Error(), "upload to GCS failed") {
		t.Fatalf("want md5 verify failure, got %v", err)
	}
	// the partial object must be deleted (Node: verifyMd5 deletes on mismatch)
	if len(file.contents) > 0 {
		t.Fatalf("mismatching upload must be deleted, %q remains", file.contents)
	}
	_ = file
}

func TestGCSGunzipCorrupt(t *testing.T) {
	gcs := newFakeGCS()
	file := gcs.put(gcsBucket, gcsKey, "x")
	file.gunzipBody = []byte("not a gzip stream")
	p := newGcsTest(t, gcsSettings(), gcs)

	_, err := p.GetObjectStream(gcsBucket, gcsKey, Opts{AutoGunzip: true})
	if _, isRead := asPersistorError(err).(*ReadError); !isRead {
		t.Fatalf("want ReadError, got %T (%v)", err, err)
	}
}

type unsignedFileAdapter struct {
	key string
}

// unsignedFileAdapter deliberately does NOT implement the optional
// GetSignedURL seam — adapters that can't sign (Node: real Storage does;
// some third-party gateways don't) exercise signedReadURL's fallback.
func (f *unsignedFileAdapter) CreateWriteStream(o GCSWriteOptions) (io.WriteCloser, error) {
	return nil, plainErr("no create")
}
func (f *unsignedFileAdapter) CreateReadStream(o GCSReadOptions) (*GCSReadStream, error) {
	return nil, plainErr("no read")
}
func (f *unsignedFileAdapter) GetMetadata() (*GCSFileMetadata, error) {
	return nil, plainErr("no metadata")
}
func (f *unsignedFileAdapter) Delete() error                    { return nil }
func (f *unsignedFileAdapter) Copy(*GCSFile) error              { return nil }
func (f *unsignedFileAdapter) SetMetadata(map[string]any) error { return nil }
func (f *unsignedFileAdapter) Exists() (bool, error)            { return false, nil }

type unsignedStorage struct{ bucketName string }

func (s *unsignedStorage) Bucket(name string) GCSBucket { return &unsignedBucket{name: name} }

type unsignedBucket struct{ name string }

func (b *unsignedBucket) File(string, ...int64) GCSFile { return &unsignedFileAdapter{} }
func (b *unsignedBucket) GetFiles(GCSQuery) ([]GCSListedFile, *GCSQuery, error) {
	return nil, nil, nil
}

func TestGCSSignedURLUnsupportedAdapter(t *testing.T) {
	p, err := NewGcsPersistor(gcsSettings(), func(map[string]any) (GCSStorage, error) {
		return &unsignedStorage{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.GetRedirectURL(gcsBucket, gcsKey)
	if err == nil {
		t.Fatal("want error")
	}
	// outer wrapper is the read-error; the adapter's "not supported" is the
	// cause (Node: signedReadURL rejects, GetRedirectUrl wraps).
	if _, isRead := asPersistorError(err).(*ReadError); !isRead {
		t.Fatalf("want ReadError, got %T", err)
	}
}

func TestGCSDeleteObjectUnlockError(t *testing.T) {
	gcs := newFakeGCS()
	file := gcs.put(gcsBucket, gcsKey, "held")
	file.setMetaErr = plainErr("setMeta failed")
	settings := gcsSettings()
	settings.UnlockBeforeDelete = true
	p := newGcsTest(t, settings, gcs)

	err := p.DeleteObject(gcsBucket, gcsKey)
	if !strings.Contains(err.Error(), "error deleting GCS object") {
		t.Fatalf("want delete error, got %v", err)
	}
}

func TestGCSDeleteObjectSuffixCopyMissing(t *testing.T) {
	gcs := newFakeGCS()
	// no object exists: the suffix copy fails with 404
	settings := gcsSettings()
	settings.DeletedBucketSuffix = "_deleted"
	p := newGcsTest(t, settings, gcs)

	// Node: 404s are ignored on the whole delete flow ("it's fine if the
	// file doesn't exist") → no-op
	if err := p.DeleteObject(gcsBucket, gcsKey); err != nil {
		t.Fatalf("404 delete flow must be a no-op: %v", err)
	}
}

func TestGCSDeleteDirectoryMemberError(t *testing.T) {
	gcs := newFakeGCS()
	file := gcs.put(gcsBucket, "dir/a", "1")
	file.deleteErr = &GCSStatusError{ErrorCode: 500, Message: "delete failed"}
	settings := gcsSettings()
	settings.DeleteConcurrency = 3
	p := newGcsTest(t, settings, gcs)

	err := p.DeleteDirectory(gcsBucket, "dir", "")
	if !strings.Contains(err.Error(), "failed to delete directory in GCS") {
		t.Fatalf("want directory delete error, got %v", err)
	}
}

func TestGCSListEmptyPrefix(t *testing.T) {
	gcs := newFakeGCS()
	gcs.put(gcsBucket, "a", "x")
	gcs.put(gcsBucket, "b", "yy")
	p := newGcsTest(t, gcsSettings(), gcs)

	keys, err := p.ListDirectoryKeys(gcsBucket, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("keys: %v", keys)
	}
}

func TestGCSStreamReadError(t *testing.T) {
	gcs := newFakeGCS()
	file := gcs.put(gcsBucket, gcsKey, "x")
	file.readBody = &gcsBoomBody{}
	p := newGcsTest(t, gcsSettings(), gcs)

	stream, err := p.GetObjectStream(gcsBucket, gcsKey, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	boom := false
	for !boom {
		if _, err := stream.Read(make([]byte, 64)); err != nil {
			boom = true
			if !strings.Contains(err.Error(), "mid-stream boom") {
				t.Fatalf("want mid-stream boom, got %v", err)
			}
		}
	}
	stream.Close()
}

// --- FS branches -----------------------------------------------------------------

func TestFSSendStreamTempWriteFailure(t *testing.T) {
	tmpDir, _, p := setupFsScenario(t, false)
	notADir := tmpDir + "/not-a-dir"

	err := p.SendStream(notADir, "a/b.tex", strings.NewReader("x"), Opts{})
	if err == nil {
		t.Fatal("want error writing under a file path")
	}
	if !strings.Contains(err.Error(), "failed to write stream") {
		t.Fatalf("message: %v", err)
	}
}

func TestFSGetObjectStreamOpenError(t *testing.T) {
	_, _, p := setupFsScenario(t, false)
	_, err := p.GetObjectStream("/loc", "missing-key", Opts{})
	if err == nil {
		t.Fatal("want error")
	}
}

// --- migration copy branches -------------------------------------------------------

func TestMigrationCopyVerifyFailure(t *testing.T) {
	primary := newFakePersistor(false)
	primary.sendStreamErr = plainErr("rebuild send failed")
	fallback := newFakePersistor(true)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	err := m.CopyObject(migBucket, migKey, migDestKey, Opts{})
	if err == nil {
		t.Fatal("want the copy failure")
	}
	// the partial destination copy must have been cleaned up
	found := false
	for _, d := range primary.deleteObjectCalls {
		if strings.Contains(d, migBucket+"/"+migDestKey) {
			found = true
		}
	}
	if !found {
		t.Fatalf("partial dest not deleted: %v", primary.deleteObjectCalls)
	}
}

func TestMigrationCopyFailureWithDeleteError(t *testing.T) {
	primary := newFakePersistor(false)
	primary.sendStreamErr = plainErr("send failed")
	primary.deleteErr = plainErr("cleanup failed")
	fallback := newFakePersistor(true)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	err := m.CopyObject(migBucket, migKey, migDestKey, Opts{})
	if err == nil {
		t.Fatal("want failure")
	}
	if _, isWrite := asPersistorError(err).(*WriteError); !isWrite {
		t.Fatalf("want WriteError, got %T", err)
	}
	if !strings.Contains(err.Error(), "unable to copy file to destination persistor") {
		t.Fatalf("message: %v", err)
	}
	full := oerror.GetFullInfo(err)
	if _, ok := full["cleanupError"]; !ok {
		t.Fatalf("cleanupError missing: %v", full)
	}
}

func TestMigrationCopyOnMissFallbackStreamError(t *testing.T) {
	primary := newFakePersistor(false)
	fallback := newFakePersistor(false)
	fallback.getStreamOverrideErr = plainErr("fallback stream broken")
	settings := migSettings()
	settings.CopyOnMiss = true
	m := NewMigrationPersistor(primary, fallback, settings)

	_, err := m.GetObjectStream(migBucket, migKey, Opts{})
	if err == nil || !strings.Contains(err.Error(), "fallback stream broken") {
		t.Fatalf("want the generic fallback error, got %v", err)
	}
}

// --- perproject copy + cached ------------------------------------------------------------

func TestPerProjectCopyObject(t *testing.T) {
	f := newFakeS3(t)
	// both DEK reads succeed (source + destination)
	f.gets = append(f.gets,
		fakeS3Outcome{get: &GetObjectResult{Body: nopCloser(make([]byte, 32))}},
		fakeS3Outcome{get: &GetObjectResult{Body: nopCloser(make([]byte, 32))}},
	)
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))

	if err := p.CopyObject("bkt", "src/f.tex", "dst/f.tex", Opts{}); err != nil {
		t.Fatal(err)
	}
	f.assertSent("CopyObject")
}

func TestPerProjectCachedSendFile(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets, fakeS3Outcome{get: &GetObjectResult{Body: nopCloser(make([]byte, 32))}})
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))
	cached, err := p.ForProject("bkt", "p1")
	if err != nil {
		t.Fatal(err)
	}
	src := mustTempFile(t, "cached file")
	if err := cached.SendFile("bkt", "p1/f.tex", src); err != nil {
		t.Fatal(err)
	}
	f.assertSent("PutObject", "PutSSEC=map")
}

func TestPerProjectGenerateAlreadyWrittenRetry(t *testing.T) {
	f := newFakeS3(t)
	// read → missing; generate → put rejected 412 (concurrent writer);
	// retry read → now present
	f.gets = append(f.gets,
		fakeS3Outcome{err: &S3Error{ErrorCode: "NoSuchKey"}},
		fakeS3Outcome{get: &GetObjectResult{Body: nopCloser(make([]byte, 32))}})
	f.puts = append(f.puts, fakeS3Outcome{err: &S3Error{ErrorCode: "PreconditionFailed", Status: 412}})
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))

	if _, err := p.ForProject("bkt", "p1"); err != nil {
		t.Fatalf("the concurrent-write race must resolve via re-read: %v", err)
	}
}

func TestPerProjectExistingCopyError(t *testing.T) {
	f := newFakeS3(t)
	// DEK read returns a body that fails mid-copy
	f.gets = append(f.gets, fakeS3Outcome{get: &GetObjectResult{Body: &gcsBoomBody{}}})
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))

	if _, err := p.ForProjectRO("bkt", "p1"); err == nil {
		t.Fatal("want the stream error to surface")
	}
}

// --- helpers / errors / seams ----------------------------------------------------------

func TestObserverNoHashAndEmpty(t *testing.T) {
	o := NewObserver("m", "b", "")
	if o.GetHash() != "" {
		t.Fatal("no hash algorithm → empty digest")
	}
	o.Finish(nil) // must not panic with zero bytes
}

func TestCodeOfErrNotExist(t *testing.T) {
	err := plainFSNotExist()
	if codeOf(err) != "ENOENT" {
		t.Fatalf("codeOf = %q, want ENOENT", codeOf(err))
	}
	if codeOf(plainErr("x")) != "" {
		t.Fatal("plain error has no code")
	}
}

func TestWrapErrorENOENTIsNotFound(t *testing.T) {
	err := wrapError(plainFSNotExist(), "whatever", map[string]any{}, classRead)
	if !isNotFoundError(err) {
		t.Fatalf("ENOENT must map to NotFoundError, got %T", err)
	}
}

func TestWrapErrorAlreadyWrittenInstance(t *testing.T) {
	base := NewAlreadyWrittenError("already written", nil)
	w := wrapError(base, "upload to S3 failed", map[string]any{"ifNoneMatch": "*"}, classWrite)
	if _, isAE := asPersistorError(w).(*AlreadyWrittenError); !isAE {
		t.Fatalf("want AlreadyWrittenError, got %T", w)
	}
}

func TestErrorCauseVariants(t *testing.T) {
	cause := plainErr("root reason")
	check := func(e interface{ Unwrap() error }) {
		if e.Unwrap() != cause {
			t.Error("cause not attached")
		}
	}
	check(NewSettingsError("s", nil, cause))
	check(NewNotImplementedError("n", nil, cause))
	check(NewNoKEKMatchedError("k", nil, cause))
	check(NewAlreadyWrittenError("a", nil, cause))
	check(NewReadError("r", nil, cause))
	check(NewWriteError("w", nil, cause))
	check(NewNotFoundError("nf", nil, cause))
	_ = oerror.GetFullInfo
}

func TestS3ErrorSurface(t *testing.T) {
	e := &S3Error{}
	if e.Error() == "" {
		t.Fatal("S3Error default message")
	}
	if e.Code() != "" {
		t.Fatal("empty ErrorCode → empty Code")
	}
	e2 := &S3Error{Err: plainErr("inner")}
	if e2.Unwrap() == nil {
		t.Fatal("unwrap")
	}
}

func TestGCSStatusErrorSurface(t *testing.T) {
	e := &GCSStatusError{}
	if e.Error() == "" {
		t.Fatal("default message")
	}
	if e.Code() != "" {
		t.Fatal("zero ErrorCode → empty code string")
	}
}

func TestSSECGetKey(t *testing.T) {
	kef := make([]byte, 32)
	s := NewSSECOptions(kef)
	if len(s.GetKey()) != 32 {
		t.Fatal("GetKey")
	}
}

func TestFactoryErrorCause(t *testing.T) {
	_, err := Create(Settings{}, Adapters{})
	if oerror.GetFullInfo(err)["cause"] != nil {
		// node: new Error('no backend specified') has no cause
		t.Fatalf("unexpected cause: %v", oerror.GetFullInfo(err))
	}
}

// --- small helpers for the seams ------------------------------------------------------

func plainFSNotExist() error {
	_, err := os.Stat("/no/such/path/for/persistors-test")
	if err == nil {
		panic("expected stat error")
	}
	return err
}

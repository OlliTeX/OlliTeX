package persistors

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file supplements the Node oracle with Go-side determinations for the
// code paths Node's unit suite does not pin (documented in HANDOFF):
// stub defaults, helpers, gunzip paths, and the error branches.

// --- BasePersistor (Node AbstractPersistor defaults) -----------------------

func TestBasePersistorDefaults(t *testing.T) {
	b := &BasePersistor{}

	check := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: want NotImplementedError", name)
		}
		if !isNotImplErr(err) {
			t.Fatalf("%s: got %T", name, err)
		}
		if !strings.Contains(err.Error(), "method not implemented in persistor") {
			t.Fatalf("%s: message %q", name, err.Error())
		}
	}

	check("sendFile", b.SendFile("loc", "t", "s"))
	check("sendStream", b.SendStream("loc", "t", strings.NewReader("x"), Opts{}))
	_, err := b.GetObjectStream("loc", "n", Opts{})
	check("getObjectStream", err)
	_, err = b.GetRedirectURL("loc", "n")
	check("getRedirectUrl", err)
	_, err = b.GetObjectSize("loc", "n", Opts{})
	check("getObjectSize", err)
	_, err = b.GetObjectMd5Hash("loc", "n", Opts{})
	check("getObjectMd5Hash", err)
	check("copyObject", b.CopyObject("loc", "a", "b", Opts{}))
	check("deleteObject", b.DeleteObject("loc", "n"))
	check("deleteDirectory", b.DeleteDirectory("loc", "n", ""))
	_, err = b.CheckIfObjectExists("loc", "n", Opts{})
	check("checkIfObjectExists", err)
	_, err = b.DirectorySize("loc", "n", "")
	check("directorySize", err)
	_, err = b.ListDirectoryKeys("loc", "n")
	check("listDirectoryKeys", err)
	_, err = b.ListDirectoryStats("loc", "n")
	check("listDirectoryStats", err)

	// the not-implemented info carries the method name
	full := oerrorFullInfo(b.SendFile("loc", "t", "s"))
	if full["method"] != "sendFile" {
		t.Fatalf("info.method = %v", full)
	}
}

// --- helpers ----------------------------------------------------------------

func TestNewHashVariants(t *testing.T) {
	if h := newHash("md5"); h == nil {
		t.Fatal("md5 hash")
	}
	if h := newHash("sha256"); h == nil {
		t.Fatal("sha256 hash")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("unknown algorithm must panic (Node: crypto.createHash throws)")
		}
	}()
	newHash("blowfish")
}

func TestHexBase64Helpers(t *testing.T) {
	want := base64.StdEncoding.EncodeToString([]byte{0xde, 0xad, 0xbe, 0xef})
	if got := hexToBase64("deadbeef"); got != want {
		t.Fatalf("hexToBase64 = %q, want %q", got, want)
	}
	if got := base64ToHex(want); got != "deadbeef" {
		t.Fatalf("base64ToHex = %q", got)
	}
	if hexToBase64("zz") != "" {
		t.Fatal("invalid hex must give empty string")
	}
	if base64ToHex("!!") != "" {
		t.Fatal("invalid base64 must give empty string")
	}
}

type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errors.New("read fail") }

func TestCalculateStreamMd5(t *testing.T) {
	got, err := calculateStreamMd5(strings.NewReader("hash me"))
	if err != nil || got != md5HexOfString("hash me") {
		t.Fatalf("md5 = %q (%v)", got, err)
	}
	_, err = calculateStreamMd5(errReader{})
	if err == nil {
		t.Fatal("want error")
	}
}

type verifySeam struct {
	hash    string
	hashErr error
	deleted []string
	delErr  error
}

func (v *verifySeam) GetObjectMd5Hash(bucket, key string, opts Opts) (string, error) {
	return v.hash, v.hashErr
}
func (v *verifySeam) DeleteObject(bucket, key string) error {
	v.deleted = append(v.deleted, bucket+"/"+key)
	return v.delErr
}

func TestVerifyMd5Match(t *testing.T) {
	v := &verifySeam{hash: "abc"}
	if err := verifyMd5(v, "bkt", "key", "abc"); err != nil {
		t.Fatalf("matching hashes must pass: %v", err)
	}
}

func TestVerifyMd5MismatchDeletes(t *testing.T) {
	v := &verifySeam{hash: "abc"}
	err := verifyMd5(v, "bkt", "key", "zzz")
	if err == nil {
		t.Fatal("mismatch must fail")
	}
	// Node: WriteError('source and destination hashes do not match')
	if !strings.Contains(err.Error(), "source and destination hashes do not match") {
		t.Fatalf("message: %v", err)
	}
	if len(v.deleted) != 1 || v.deleted[0] != "bkt/key" {
		t.Fatalf("deleted: %v", v.deleted)
	}
}

func TestVerifyMd5ExplicitDest(t *testing.T) {
	v := &verifySeam{hash: "abc"}
	if err := verifyMd5(v, "bkt", "key", "abc", "abc"); err != nil {
		t.Fatalf("explicit dest must pass: %v", err)
	}
}

func TestVerifyMd5HashErrorPropagates(t *testing.T) {
	v := &verifySeam{hashErr: errSilent("boom-hash")}
	if err := verifyMd5(v, "bkt", "key", "abc"); err == nil {
		t.Fatal("want error")
	}
}

// --- Null logger / metrics ---------------------------------------------------

func TestNullLoggerMetrics(t *testing.T) {
	NullLogger{}.Info(nil, "i")
	NullLogger{}.Warn(nil, "w")
	NullLogger{}.Error(nil, "e")
	NullLogger{}.Debug(nil, "d")
	NullMetrics{}.Count("m", 1, 1, map[string]string{})
	NullMetrics{}.Inc("m", 1, map[string]string{})
	NullMetrics{}.Histogram("m", 1, []int64{1}, map[string]string{})
}

// --- gzip paths (S3 + GCS) ----------------------------------------------------

func gzipBytes(t *testing.T, s string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := gzip.NewWriter(buf)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestS3GetObjectStreamAutoGunzip(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets, fakeS3Outcome{get: &GetObjectResult{
		Body:            io.NopCloser(bytes.NewReader(gzipBytes(t, "gunzipped body"))),
		ContentEncoding: "gzip",
	}})
	p := newTestS3Persistor(t, S3Settings{}, f)

	stream, err := p.GetObjectStream(s3Bucket, s3Key, Opts{AutoGunzip: true})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(stream)
	stream.Close()
	if string(got) != "gunzipped body" {
		t.Fatalf("gunzip: %q", got)
	}
}

func TestS3GetObjectStreamAutoGunzipCorrupt(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets, fakeS3Outcome{get: &GetObjectResult{
		Body:            io.NopCloser(bytes.NewReader([]byte("not gzip"))),
		ContentEncoding: "gzip",
	}})
	p := newTestS3Persistor(t, S3Settings{}, f)

	_, err := p.GetObjectStream(s3Bucket, s3Key, Opts{AutoGunzip: true})
	if _, isRead := asPersistorError(err).(*ReadError); !isRead {
		t.Fatalf("want ReadError, got %T (%v)", err, err)
	}
}

func TestGCSGetObjectStreamAutoGunzip(t *testing.T) {
	gcs := newFakeGCS()
	file := gcs.put(gcsBucket, gcsKey, "x")
	file.gunzipBody = gzipBytes(t, "contents-out")
	p := newGcsTest(t, gcsSettings(), gcs)

	stream, err := p.GetObjectStream(gcsBucket, gcsKey, Opts{AutoGunzip: true})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(stream)
	stream.Close()
	if string(got) != "contents-out" {
		t.Fatalf("gunzip: %q", got)
	}
}

type gcsBoomBody struct{ done bool }

func (b *gcsBoomBody) Read(p []byte) (int, error) {
	if !b.done {
		b.done = true
		n := copy(p, []byte("ab"))
		return n, nil
	}
	return 0, plainErr("mid-stream boom")
}
func (b *gcsBoomBody) Close() error { return nil }

func TestS3GetObjectStreamMidStreamError(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets, fakeS3Outcome{get: &GetObjectResult{Body: &gcsBoomBody{}}})
	p := newTestS3Persistor(t, S3Settings{}, f)

	stream, err := p.GetObjectStream(s3Bucket, s3Key, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	var buf []byte
	var lastErr error
	for lastErr == nil {
		chunk := make([]byte, 64)
		n, err := stream.Read(chunk)
		buf = append(buf, chunk[:n]...)
		lastErr = err
	}
	if !strings.Contains(lastErr.Error(), "mid-stream boom") {
		t.Fatalf("want mid-stream error, got %v", lastErr)
	}
	if string(buf) != "ab" {
		t.Fatalf("partial content = %q", buf)
	}
	stream.Close()
}

// --- S3 error branches -------------------------------------------------------

func TestS3DeleteObjectError(t *testing.T) {
	f := newFakeS3(t)
	f.deletes = append(f.deletes, fakeS3Outcome{err: errSilent("delete failed")})
	p := newTestS3Persistor(t, S3Settings{}, f)

	err := p.DeleteObject(s3Bucket, s3Key)
	if _, isWrite := asPersistorError(err).(*WriteError); !isWrite {
		t.Fatalf("want WriteError, got %T (%v)", err, err)
	}
	if !strings.Contains(err.Error(), "failed to delete file in S3") {
		t.Fatalf("message: %v", err)
	}
}

func TestS3GetRedirectUrlError(t *testing.T) {
	f := newFakeS3(t)
	f.presigns = append(f.presigns, fakeS3Outcome{err: errSilent("presign failed")})
	p := newTestS3Persistor(t, S3Settings{}, f)

	_, err := p.GetRedirectURL(s3Bucket, s3Key)
	if _, isRead := asPersistorError(err).(*ReadError); !isRead {
		t.Fatalf("want ReadError, got %T (%v)", err, err)
	}
	if !strings.Contains(err.Error(), "error generating signed url for S3 file") {
		t.Fatalf("message: %v", err)
	}
}

func TestS3GetObjectStorageClassError(t *testing.T) {
	f := newFakeS3(t)
	f.heads = append(f.heads, fakeS3Outcome{err: errSilent("head failed")})
	p := newTestS3Persistor(t, S3Settings{}, f)

	_, err := p.GetObjectStorageClass(s3Bucket, s3Key, Opts{})
	if err == nil {
		t.Fatal("want error")
	}
}

func TestS3SendStreamAlreadyWritten(t *testing.T) {
	f := newFakeS3(t)
	f.puts = append(f.puts, fakeS3Outcome{err: &S3Error{ErrorCode: "PreconditionFailed", Status: 412}})
	p := newTestS3Persistor(t, S3Settings{}, f)

	err := p.SendStream(s3Bucket, s3Key, strings.NewReader("x"), Opts{IfNoneMatch: "*"})
	ae, isAE := asPersistorError(err).(*AlreadyWrittenError)
	if !isAE {
		t.Fatalf("want AlreadyWrittenError, got %T (%v)", err, err)
	}
	_ = ae
}

func TestS3SendStreamAlreadyWritten412Status(t *testing.T) {
	f := newFakeS3(t)
	f.puts = append(f.puts, fakeS3Outcome{err: &S3Error{Status: 412}})
	p := newTestS3Persistor(t, S3Settings{}, f)

	err := p.SendStream(s3Bucket, s3Key, strings.NewReader("x"), Opts{IfNoneMatch: "*"})
	if _, isAE := asPersistorError(err).(*AlreadyWrittenError); !isAE {
		t.Fatalf("want AlreadyWrittenError, got %T (%v)", err, err)
	}
}

func TestS3CopyObjectError(t *testing.T) {
	f := newFakeS3(t)
	f.copies = append(f.copies, fakeS3Outcome{err: errSilent("copy failed")})
	p := newTestS3Persistor(t, S3Settings{}, f)

	err := p.CopyObject(s3Bucket, s3Key, s3DestKey, Opts{})
	if _, isWrite := asPersistorError(err).(*WriteError); !isWrite {
		t.Fatalf("want WriteError, got %T (%v)", err, err)
	}
	if !strings.Contains(err.Error(), "failed to copy file in S3") {
		t.Fatalf("message: %v", err)
	}
}

func TestS3CreateBucket(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)
	if err := p.CreateBucket("new-bucket"); err != nil {
		t.Fatal(err)
	}
	f.assertSent("CreateBucket", "Bucket=new-bucket")
}

func TestS3GetClientNoFactory(t *testing.T) {
	s3s := S3Settings{Key: "frog", Secret: "prince"}
	p := NewS3Persistor(s3s, nil)
	_, err := p.GetObjectStream("b", "k", Opts{})
	if err == nil {
		t.Fatal("want client-factory error")
	}
}

func TestApplyDeleteObjectsMd5Fallback(t *testing.T) {
	out := applyDeleteObjectsMd5Fallback(map[string]string{
		"X-Amz-Checksum-Sha256":    "AAA", // dropped
		"X-Amz-Sdk-Checksum-Crc32": "BBB", // dropped
		"Content-Type":             "application/xml",
	}, []byte("body"))
	if _, present := out["X-Amz-Checksum-Sha256"]; present {
		t.Fatal("x-amz-checksum-* must be dropped")
	}
	if _, present := out["X-Amz-Sdk-Checksum-Crc32"]; present {
		t.Fatal("x-amz-sdk-checksum-* must be dropped")
	}
	if out["Content-Type"] != "application/xml" {
		t.Fatalf("other headers must be kept: %v", out)
	}
	if out["Content-MD5"] == "" {
		t.Fatal("Content-MD5 must be set")
	}
}

// --- FS branches ---------------------------------------------------------------

func TestFSListSubdirectories(t *testing.T) {
	tmpDir := t.TempDir()
	location := tmpDir + "/bucket"
	layout := map[string]string{
		"a/b.txt":   "one",
		"a/c/d.txt": "two two",
		"a/.hidden": "secret",
		"top.txt":   "top",
	}
	for rel, contents := range layout {
		p := location + "/" + rel
		if err := mkdirAllParent(p); err != nil {
			t.Fatal(err)
		}
		if err := writeFile(p, contents); err != nil {
			t.Fatal(err)
		}
	}

	f, err := NewFSPersistor(FSSettings{UseSubdirectories: true})
	if err != nil {
		t.Fatal(err)
	}

	keys, err := f.ListDirectoryKeys(location, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("keys = %v (dotfile must be excluded)", keys)
	}

	stats, err := f.ListDirectoryStats(location, "a")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Fatalf("stats = %v", stats)
	}

	size, err := f.DirectorySize(location, "a", "")
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len("one")+len("two two")) {
		t.Fatalf("size = %d", size)
	}

	// missing directory → no files (Node glob semantics)
	keys, err = f.ListDirectoryKeys(location, "missing")
	if err != nil || len(keys) != 0 {
		t.Fatalf("missing dir: %v, %v", keys, err)
	}
}

func TestFSCopyObjectMissingSource(t *testing.T) {
	_, _, p := setupFsScenario(t, false)
	err := p.CopyObject("loc", "does/not/exist", "target", Opts{})
	if err == nil {
		t.Fatal("want error copying a missing file")
	}
}

func TestFSSendFileMissingSource(t *testing.T) {
	_, location, p := setupFsScenario(t, false)
	err := p.SendFile(location, "a/b.tex", "/no/such/source")
	if err == nil {
		t.Fatal("want error for missing source file")
	}
}

// --- GCS branches --------------------------------------------------------------

func TestGCSSendFile(t *testing.T) {
	gcs := newFakeGCS()
	p := newGcsTest(t, gcsSettings(), gcs)
	src := mustTempFile(t, "gcs file bytes")
	if err := p.SendFile(gcsBucket, gcsKey, src); err != nil {
		t.Fatal(err)
	}
	if got := gcs.bucket(gcsBucket).objects[gcsKey]; got == nil || string(got.contents) != "gcs file bytes" {
		t.Fatalf("stored: %v", gcs.bucket(gcsBucket).objects[gcsKey])
	}
}

func TestGCSDeleteDirectoryNotFoundIsNoop(t *testing.T) {
	gcs := newFakeGCS()
	gcs.bucket(gcsBucket).getFilesErr = &GCSStatusError{ErrorCode: 404}
	p := newGcsTest(t, gcsSettings(), gcs)

	if err := p.DeleteDirectory(gcsBucket, "missing-dir", ""); err != nil {
		t.Fatalf("404 directory delete must be a no-op: %v", err)
	}
}

func TestGCSDeleteDirectoryError(t *testing.T) {
	gcs := newFakeGCS()
	gcs.bucket(gcsBucket).getFilesErr = &GCSStatusError{ErrorCode: 500, Message: "server error"}
	p := newGcsTest(t, gcsSettings(), gcs)

	err := p.DeleteDirectory(gcsBucket, "dir", "")
	if _, isWrite := asPersistorError(err).(*WriteError); !isWrite {
		t.Fatalf("want WriteError, got %T (%v)", err, err)
	}
	if !strings.Contains(err.Error(), "failed to delete directory in GCS") {
		t.Fatalf("message: %v", err)
	}
}

func TestGCSListErrors(t *testing.T) {
	gcs := newFakeGCS()
	gcs.bucket(gcsBucket).getFilesErr = &GCSStatusError{ErrorCode: 500}
	p := newGcsTest(t, gcsSettings(), gcs)

	_, err := p.ListDirectoryKeys(gcsBucket, "p")
	if _, isR := asPersistorError(err).(*ReadError); !isR {
		t.Fatalf("want ReadError, got %v", err)
	}
	_, err = p.ListDirectoryStats(gcsBucket, "p")
	if _, isR := asPersistorError(err).(*ReadError); !isR {
		t.Fatalf("want ReadError, got %v", err)
	}
	_, err = p.DirectorySize(gcsBucket, "p", "")
	if _, isR := asPersistorError(err).(*ReadError); !isR {
		t.Fatalf("want ReadError, got %v", err)
	}
}

func TestGCSGetObjectStreamErrorReading(t *testing.T) {
	gcs := newFakeGCS()
	file := gcs.put(gcsBucket, gcsKey, "x")
	file.readErr = &GCSStatusError{ErrorCode: 500}
	p := newGcsTest(t, gcsSettings(), gcs)

	_, err := p.GetObjectStream(gcsBucket, gcsKey, Opts{})
	if _, isRead := asPersistorError(err).(*ReadError); !isRead {
		t.Fatalf("want ReadError, got %T (%v)", err, err)
	}
	if !strings.Contains(err.Error(), "error reading file from GCS") {
		t.Fatalf("message: %v", err)
	}
}

// --- migration pass-throughs ----------------------------------------------------

func TestMigrationPassThroughs(t *testing.T) {
	primary := newFakePersistor(true)
	fallback := newFakePersistor(true)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	src := mustTempFile(t, "x")
	if err := m.SendFile(migBucket, migKey, src); err != nil {
		t.Fatal(err)
	}
	if len(primary.sendFileCalls) != 1 || primary.sendFileCalls[0] != migBucket+"/"+migKey {
		t.Errorf("sendFile → primary sendFile: %v", primary.sendFileCalls)
	}

	url, err := m.GetRedirectURL(migBucket, migKey)
	if err != nil || url != "url" {
		t.Fatalf("redirect: %q (%v)", url, err)
	}

	hash, err := m.GetObjectMd5Hash(migBucket, migKey, Opts{})
	if err != nil || hash != "ffffffff" {
		t.Fatalf("md5: %q (%v)", hash, err)
	}

	// Node: listDirectoryKeys/Stats are NOT overridden on the migration
	// persistor → inherit the AbstractPersistor NotImplementedError.
	if _, err := m.ListDirectoryKeys(migBucket, migKey); !isNotImplErr(err) {
		t.Fatalf("want NotImplementedError, got %T (%v)", err, err)
	}
	if _, err := m.ListDirectoryStats(migBucket, migKey); !isNotImplErr(err) {
		t.Fatalf("want NotImplementedError, got %T (%v)", err, err)
	}

	if err := m.DeleteDirectory(migBucket, migKey, ""); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationCheckIfObjectExistsPrimaryFalse(t *testing.T) {
	// Node: the stub RESOLVES hasFile (no NotFound rejection), so the
	// migration returns the primary's answer as-is — no fallback consult.
	primary := newFakePersistor(false)
	fallback := newFakePersistor(true)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	ok, err := m.CheckIfObjectExists(migBucket, migKey, Opts{})
	if err != nil || ok {
		t.Fatalf("exists = %v (%v)", ok, err)
	}
}

func TestMigrationCheckIfObjectExistsMissing(t *testing.T) {
	m := NewMigrationPersistor(newFakePersistor(false), newFakePersistor(false), migSettings())
	ok, err := m.CheckIfObjectExists(migBucket, migKey, Opts{})
	if err != nil || ok {
		t.Fatalf("exists = %v (%v)", ok, err)
	}
}

func TestMigrationDirectorySizeFallback(t *testing.T) {
	primary := newFakePersistor(false)
	fallback := newFakePersistor(true)
	m := NewMigrationPersistor(primary, fallback, migSettings())

	size, err := m.DirectorySize(migBucket, migKey, "")
	if err != nil || size != 33 {
		t.Fatalf("size = %d (%v)", size, err)
	}
}

// --- perproject branches ---------------------------------------------------------

func TestPerProjectDataEncryptionKeySize(t *testing.T) {
	f := newFakeS3(t)
	// DEK object present (HeadObject 200 → size 5555)
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))

	size, err := p.DataEncryptionKeySize("bkt", "p1")
	if err != nil || size != s3ObjectSize {
		t.Fatalf("size = %d (%v)", size, err)
	}
}

func TestPerProjectDataEncryptionKeySizeForbiddenFallback(t *testing.T) {
	f := newFakeS3(t)
	// first KEK → 403, second KEK → hit
	f.heads = append(f.heads,
		fakeS3Outcome{err: &S3Error{ErrorCode: "InvalidArgument", Status: 403}},
		fakeS3Outcome{head: &HeadObjectResult{}})
	keys := []*RootKeyEncryptionKey{mustKEK(t, 1), mustKEK(t, 2)}
	p := newEncryptedS3(t, f, encryptedSettings(keys...))

	if _, err := p.DataEncryptionKeySize("bkt", "p1"); err != nil {
		t.Fatalf("fallback KEK should succeed: %v", err)
	}
}

func TestPerProjectDataEncryptionKeySizeNoMatch(t *testing.T) {
	f := newFakeS3(t)
	keys := []*RootKeyEncryptionKey{mustKEK(t, 1), mustKEK(t, 2)}
	for range keys {
		f.heads = append(f.heads, fakeS3Outcome{err: &S3Error{ErrorCode: "InvalidArgument", Status: 403}})
	}
	p := newEncryptedS3(t, f, encryptedSettings(keys...))

	_, err := p.DataEncryptionKeySize("bkt", "p1")
	if !isNoKEKMatchedErr(err) {
		t.Fatalf("want NoKEKMatchedError, got %T (%v)", err, err)
	}
	if !strings.Contains(err.Error(), "no kek matched") {
		t.Fatalf("message: %v", err)
	}
}

func isNoKEKMatchedErr(err error) bool {
	_, ok := asPersistorError(err).(*NoKEKMatchedError)
	return ok
}

func TestPerProjectForProjectROExisting(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets, fakeS3Outcome{get: &GetObjectResult{Body: nopCloser(make([]byte, 32))}})
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))

	cached, err := p.ForProjectRO("bkt", "p1")
	if err != nil {
		t.Fatal(err)
	}
	f.assertNotSent("PutObject")
	_ = cached
}

func TestPerProjectForProjectROMissing(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets, fakeS3Outcome{err: &S3Error{ErrorCode: "NoSuchKey"}})
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))

	_, err := p.ForProjectRO("bkt", "p1")
	if !isNotFoundError(err) {
		t.Fatalf("want NotFoundError (RO never generates), got %T (%v)", err, err)
	}
	f.assertNotSent("PutObject")
}

func TestPerProjectGenerateDataEncryptionKey(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets, fakeS3Outcome{err: &S3Error{ErrorCode: "NoSuchKey"}})
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))

	if _, err := p.GenerateDataEncryptionKey("bkt", "p1"); err != nil {
		t.Fatal(err)
	}
	f.assertSent("PutObject", "Bucket=dek-bucket", "IfNoneMatch=*")
}

func TestPerProjectRotationSendErrorHonoured(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets,
		fakeS3Outcome{err: &S3Error{ErrorCode: "InvalidArgument", Status: 403}},
		fakeS3Outcome{get: &GetObjectResult{Body: nopCloser(make([]byte, 32))}})
	f.puts = append(f.puts, fakeS3Outcome{err: errSilent("rotation put failed")})
	settings := encryptedSettings(mustKEK(t, 1), mustKEK(t, 2))
	settings.AutomaticallyRotateDEKEncryption = true
	p := newEncryptedS3(t, f, settings)

	_, err := p.ForProject("bkt", "p1")
	if err == nil {
		t.Fatal("rotation failure must surface (ignore flag off)")
	}
}

func TestPerProjectRotationSendErrorIgnored(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets,
		fakeS3Outcome{err: &S3Error{ErrorCode: "InvalidArgument", Status: 403}},
		fakeS3Outcome{get: &GetObjectResult{Body: nopCloser(make([]byte, 32))}})
	f.puts = append(f.puts, fakeS3Outcome{err: errSilent("rotation put failed")})
	settings := encryptedSettings(mustKEK(t, 1), mustKEK(t, 2))
	settings.AutomaticallyRotateDEKEncryption = true
	settings.IgnoreErrorsFromDEKReEncryption = true
	p := newEncryptedS3(t, f, settings)

	if _, err := p.ForProject("bkt", "p1"); err != nil {
		t.Fatalf("rotation error must be ignored: %v", err)
	}
}

func TestCachedPerProjectSendStreamCarriesSSEC(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets, fakeS3Outcome{get: &GetObjectResult{Body: nopCloser(make([]byte, 32))}})
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))
	cached, err := p.ForProject("bkt", "p1")
	if err != nil {
		t.Fatal(err)
	}

	if err := cached.SendStream("bkt", "p1/f.tex", strings.NewReader("x"), Opts{}); err != nil {
		t.Fatal(err)
	}
	f.assertSent("PutObject", "PutSSEC=map")

	if _, err := cached.GetObjectStream("bkt", "p1/f.tex", Opts{}); err == nil {
		// the fake returns a default body — fine, the seam call is what matters
	}
}

// tiny fs helpers

func mkdirAllParent(p string) error {
	return os.MkdirAll(filepath.Dir(p), 0o755)
}

func writeFile(p, contents string) error {
	return os.WriteFile(p, []byte(contents), 0o644)
}

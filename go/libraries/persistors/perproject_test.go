package persistors

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// --- PerProjectEncryptedS3Persistor (port of the Node S3SSEC semantics; the
// Node repo ships no unit suite for this class — these Go tests pin the
// documented behaviour: KEK validation, DEK lifecycle, multi-KEK fallback,
// and the signed-URL refusal) -----------------------------------------------

const dekBucket = "dek-bucket"

func kekBytes(seed byte) []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = seed + byte(i)
	}
	return k
}

func mustKEK(t *testing.T, seed byte) *RootKeyEncryptionKey {
	t.Helper()
	k, err := NewRootKeyEncryptionKey(kekBytes(seed), kekBytes(seed+100))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func encryptedSettings(keks ...*RootKeyEncryptionKey) EncryptedS3Settings {
	return EncryptedS3Settings{
		S3Settings:                  S3Settings{Key: "frog", Secret: "prince"},
		DataEncryptionKeyBucketName: dekBucket,
		PathToProjectFolder:         func(bucket, p string) string { return "proj/" + p },
		GetRootKeyEncryptionKeys:    func() ([]*RootKeyEncryptionKey, error) { return keks, nil },
	}
}

func newEncryptedS3(t *testing.T, f *fakeS3, settings EncryptedS3Settings) *PerProjectEncryptedS3Persistor {
	t.Helper()
	p, err := NewPerProjectEncryptedS3Persistor(settings,
		func(bucket string) (S3Client, error) { return f, nil })
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestKEKLengthValidation(t *testing.T) {
	_, err := NewRootKeyEncryptionKey(make([]byte, 31), nil)
	if err == nil || !strings.Contains(err.Error(), "kek is not 32 bytes long") {
		t.Fatalf("want 32-byte error, got %v", err)
	}
	if _, err := NewRootKeyEncryptionKey(make([]byte, 32), nil); err != nil {
		t.Fatalf("32-byte kek must be accepted: %v", err)
	}
}

func TestForProjectDeterministic(t *testing.T) {
	k := mustKEK(t, 1)
	a := k.ForProject("project/1")
	b := k.ForProject("project/1")
	if a.GetMD5() != b.GetMD5() {
		t.Fatal("same prefix must derive the same SSEC key")
	}
	c := k.ForProject("project/2")
	if a.GetMD5() == c.GetMD5() {
		t.Fatal("different prefixes must derive different keys")
	}
}

func TestEncryptedConstructorMissingDEKBucket(t *testing.T) {
	settings := encryptedSettings(mustKEK(t, 1))
	settings.DataEncryptionKeyBucketName = ""
	_, err := NewPerProjectEncryptedS3Persistor(settings,
		func(string) (S3Client, error) { return newFakeS3(t), nil })
	if err == nil || !strings.Contains(err.Error(), "settings.dataEncryptionKeyBucketName is missing") {
		t.Fatalf("want missing-bucket error, got %v", err)
	}
}

func TestEncryptedNoRootKEK(t *testing.T) {
	f := newFakeS3(t)
	p := newEncryptedS3(t, f, encryptedSettings()) // empty keys

	err := p.SendStream("bkt", "p/file.tex", strings.NewReader("x"), Opts{})
	if err == nil || !strings.Contains(err.Error(), "no root kek provided") {
		t.Fatalf("want no-kek error, got %v", err)
	}
}

func TestEncryptedSendStreamNotFoundWithoutDEK(t *testing.T) {
	f := newFakeS3(t)
	// DEK read → NoSuchKey
	f.gets = append(f.gets, fakeS3Outcome{err: &S3Error{ErrorCode: "NoSuchKey"}})
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))

	stream, err := p.GetObjectStream("bkt", "p1/file.tex", Opts{})
	if !isNotFoundError(err) {
		t.Fatalf("want NotFoundError (no DEK), got %T (%v)", err, stream)
	}
}

func TestEncryptedForProjectGeneratesDEK(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets, fakeS3Outcome{err: &S3Error{ErrorCode: "NoSuchKey"}})
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))

	if _, err := p.ForProject("bkt", "p1"); err != nil {
		t.Fatal(err)
	}
	f.assertSent("PutObject", "Bucket=dek-bucket", "Key=proj/p1/dek", "IfNoneMatch=*", "PutSSEC=map")
}

func TestEncryptedForProjectReusesDEK(t *testing.T) {
	f := newFakeS3(t)
	// first DEK read succeeds: 32 bytes
	dek := make([]byte, 32)
	for i := range dek {
		dek[i] = byte(i)
	}
	f.gets = append(f.gets, fakeS3Outcome{get: &GetObjectResult{Body: nopCloser(dek)}})
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))

	if _, err := p.ForProject("bkt", "p1"); err != nil {
		t.Fatal(err)
	}
	f.assertNotSent("PutObject")

	cached, err := p.ForProject("bkt", "p1")
	if err != nil {
		t.Fatal(err)
	}
	f.assertNotSent("PutObject")
	_ = cached
}

func TestEncryptedMultiKEKForbiddenFallback(t *testing.T) {
	f := newFakeS3(t)
	// KEK #1 forbidden (403), KEK #2 succeeds
	f.gets = append(f.gets,
		fakeS3Outcome{err: &S3Error{ErrorCode: "InvalidArgument", Status: 403, Message: "forbidden"}},
		fakeS3Outcome{get: &GetObjectResult{Body: nopCloser(make([]byte, 32))}})
	keys := []*RootKeyEncryptionKey{mustKEK(t, 1), mustKEK(t, 2)}
	p := newEncryptedS3(t, f, encryptedSettings(keys...))

	if _, err := p.ForProject("bkt", "p1"); err != nil {
		t.Fatalf("fallback KEK should succeed: %v", err)
	}
	f.assertSent("GetObject", "GetSSEC=map")
	// exactly two GetObject attempts (KEK #1 → 403, KEK #2 → hit)
	f.mu.Lock()
	count := 0
	for _, c := range f.calls {
		if c.name == "GetObject" {
			count++
		}
	}
	f.mu.Unlock()
	if count != 2 {
		t.Fatalf("expected 2 DEK reads, got %d", count)
	}
}

func TestEncryptedDEKRotationOnRead(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets,
		fakeS3Outcome{err: &S3Error{ErrorCode: "InvalidArgument", Status: 403}},
		fakeS3Outcome{get: &GetObjectResult{Body: nopCloser(make([]byte, 32))}})
	settings := encryptedSettings(mustKEK(t, 1), mustKEK(t, 2))
	settings.AutomaticallyRotateDEKEncryption = true
	p := newEncryptedS3(t, f, settings)

	if _, err := p.ForProject("bkt", "p1"); err != nil {
		t.Fatal(err)
	}
	// the re-encrypted DEK must be written back (PutObject)
	f.assertSent("PutObject", "Bucket=dek-bucket", "Key=proj/p1/dek")
}

func TestEncryptedGetObjectMd5ForcesDownload(t *testing.T) {
	f := newFakeS3(t)
	// Node: the per-project md5 is delegated to the S3 base with
	// etagIsNotMD5 (no DEK resolution in that method) — even a perfectly
	// valid MD5 ETag must force the download-and-hash path.
	f.heads = append(f.heads, fakeS3Outcome{head: &HeadObjectResult{ETag: `"ffffffff00000000ffffffff00000000"`}})
	f.gets = append(f.gets,
		fakeS3Outcome{get: &GetObjectResult{Body: nopCloser([]byte("payload"))}})
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))

	md5h, err := p.GetObjectMd5Hash("bkt", "p1/f.tex", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if md5h != md5HexOfString("payload") {
		t.Fatalf("md5 = %q, want %q", md5h, md5HexOfString("payload"))
	}
	// the ETag fast path was skipped: a GetObject download happened
	f.mu.Lock()
	gets := 0
	for _, c := range f.calls {
		if c.name == "GetObject" {
			gets++
		}
	}
	f.mu.Unlock()
	if gets != 1 {
		t.Fatalf("expected exactly 1 GetObject download, got %d", gets)
	}
}

func TestEncryptedGetRedirectURLRefused(t *testing.T) {
	f := newFakeS3(t)
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))
	_, err := p.GetRedirectURL("bkt", "p1/f.tex")
	if !isNotImplErr(err) {
		t.Fatalf("want NotImplementedError, got %T", err)
	}
	if !strings.Contains(err.Error(), "signed links are not supported with SSE-C") {
		t.Fatalf("message: %v", err)
	}
}

func TestEncryptedDeleteDirectoryDropsDEK(t *testing.T) {
	f := newFakeS3(t)
	settings := encryptedSettings(mustKEK(t, 1))
	// identity folder mapping: projectFolder === path → the DEK is dropped
	settings.PathToProjectFolder = func(bucket, p string) string { return p }
	p := newEncryptedS3(t, f, settings)

	if err := p.DeleteDirectory("bkt", "p1", ""); err != nil {
		t.Fatal(err)
	}
	f.assertSent("DeleteObject", "Bucket=dek-bucket", "Key=p1/dek")
}

func TestEncryptedDeleteDirectoryKeepsDEK(t *testing.T) {
	f := newFakeS3(t)
	p := newEncryptedS3(t, f, encryptedSettings(mustKEK(t, 1)))

	// "files" under project folder "proj/files" — projectFolder ≠ path →
	// the DEK must remain (the prefix sweep itself still happens)
	if err := p.DeleteDirectory("bkt", "proj/files", ""); err != nil {
		t.Fatal(err)
	}
	f.assertSent("DeleteObjects", "Bucket=bkt")
	f.assertSent("ListObjectsV2", "Prefix=proj/files")
	f.mu.Lock()
	dekDeleted := false
	for _, c := range f.calls {
		if c.name == "DeleteObject" && strings.Contains(c.params, "Bucket=dek-bucket") {
			dekDeleted = true
		}
	}
	f.mu.Unlock()
	if dekDeleted {
		t.Fatalf("DEK must not be deleted: %v", f.calls)
	}
}

func nopCloser(b []byte) io.ReadCloser {
	return io.NopCloser(bytes.NewReader(b))
}

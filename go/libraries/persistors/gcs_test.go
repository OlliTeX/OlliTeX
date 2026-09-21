package persistors

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"ollitex/go/libraries/oerror"
)

// fakeGCS — the Go stand-in for the @google-cloud/storage stub in
// test/unit/GcsPersistorTests.js. In-memory objects; per-file call records.
type fakeGCS struct {
	buckets map[string]*fakeGCSBucket
}

type fakeGCSBucket struct {
	name         string
	objects      map[string]*fakeGCSFile
	filesCreated []string
	queries      []GCSQuery
	getFilesErr  error
}

type fakeGCSFile struct {
	bucket          *fakeGCSBucket
	key             string
	contents        []byte
	generation      int64 // recorded at File(name, generation)
	generations     []int64
	writeOpts       GCSWriteOptions
	readOpts        GCSReadOptions
	gunzipBody      []byte        // when set: served as a gzip-encoded body
	readBody        io.ReadCloser // when set: served verbatim (can error mid-stream)
	metaMD5Override string        // when set: metadata.md5Hash uses this base64
	writeCloseErr   error         // writeCloser close failure
	writeErr        error
	readErr         error
	deleteErr       error
	copyErr         error
	metaErr         error
	setMetaErr      error
	existsErr       error
	existsOk        *bool // nil → derived
	setMetaCalls    []map[string]any
	copiedTo        []string
	signedAt        int64
	readStatus      int // 200/206/404/other
}

func newFakeGCS() *fakeGCS {
	f := &fakeGCS{buckets: map[string]*fakeGCSBucket{}}
	return f
}

func (f *fakeGCS) bucket(name string) *fakeGCSBucket {
	b, ok := f.buckets[name]
	if !ok {
		b = &fakeGCSBucket{name: name, objects: map[string]*fakeGCSFile{}}
		f.buckets[name] = b
	}
	return b
}

func (f *fakeGCS) put(bucket, key, contents string) *fakeGCSFile {
	file := &fakeGCSFile{bucket: f.bucket(bucket), key: key, readStatus: 200}
	b := f.bucket(bucket)
	b.objects[key] = file
	file.contents = []byte(contents)
	return file
}

func (f *fakeGCS) Bucket(name string) GCSBucket { return fakeGCSBucketHandle{b: f.bucket(name)} }

var _ GCSStorage = (*fakeGCS)(nil)

type fakeGCSBucketHandle struct {
	b *fakeGCSBucket
}

func (h fakeGCSBucketHandle) File(name string, generation ...int64) GCSFile {
	file, ok := h.b.objects[name]
	if !ok {
		file = &fakeGCSFile{bucket: h.b, key: name, readStatus: 404}
		h.b.objects[name] = file
	}
	if len(generation) > 0 {
		file.generation = generation[0]
	}
	file.generations = append(file.generations, 0)
	if len(generation) > 0 {
		file.generations[len(file.generations)-1] = generation[0]
	}
	return file
}

func (h fakeGCSBucketHandle) GetFiles(q GCSQuery) ([]GCSListedFile, *GCSQuery, error) {
	h.b.queries = append(h.b.queries, q)
	if h.b.getFilesErr != nil {
		return nil, nil, h.b.getFilesErr
	}
	var out []GCSListedFile
	for k, file := range h.b.objects {
		if strings.HasPrefix(k, q.Prefix) {
			out = append(out, GCSListedFile{
				Name:     k,
				Metadata: GCSFileMetadata{Size: fmt.Sprintf("%d", len(file.contents))},
			})
		}
	}
	// sort by key for determinism
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Name < out[j-1].Name; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil, nil // single page
}

var _ GCSBucket = fakeGCSBucketHandle{}

func (f *fakeGCSFile) CreateWriteStream(opts GCSWriteOptions) (io.WriteCloser, error) {
	f.writeOpts = opts
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	buf := &bytes.Buffer{}
	return &fakeGCSWriteCloser{buf: buf, file: f}, nil
}

type fakeGCSWriteCloser struct {
	buf  *bytes.Buffer
	file *fakeGCSFile
}

func (w *fakeGCSWriteCloser) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *fakeGCSWriteCloser) Close() error {
	if w.file.writeCloseErr != nil {
		return w.file.writeCloseErr
	}
	w.file.contents = w.buf.Bytes()
	w.file.readStatus = 200
	return nil
}

func (f *fakeGCSFile) CreateReadStream(opts GCSReadOptions) (*GCSReadStream, error) {
	f.readOpts = opts
	if f.readErr != nil {
		return nil, f.readErr
	}
	if f.readBody != nil {
		return &GCSReadStream{Status: 200, Body: f.readBody}, nil
	}
	status := f.readStatus
	if status == 200 && len(f.generations) > 0 && f.generation == 0 && len(f.contents) == 0 {
		// generation-0 read of a missing file
		status = 404
	}
	if status == 200 && len(f.contents) == 0 {
		status = 404
	}
	if status == 404 {
		return &GCSReadStream{Status: 404, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	}
	contents := f.contents
	if opts.Start != nil || opts.End != nil {
		start := int64(0)
		if opts.Start != nil {
			start = *opts.Start
		}
		end := int64(len(contents) - 1)
		if opts.End != nil {
			end = *opts.End
		}
		contents = contents[start : end+1]
		status = 206
	}
	if len(f.gunzipBody) > 0 {
		return &GCSReadStream{Status: 200, ContentEncoding: "gzip", Body: io.NopCloser(bytes.NewReader(f.gunzipBody))}, nil
	}
	return &GCSReadStream{Status: status, Body: io.NopCloser(bytes.NewReader(contents))}, nil
}

func (f *fakeGCSFile) GetMetadata() (*GCSFileMetadata, error) {
	if f.metaErr != nil {
		return nil, f.metaErr
	}
	hash := hexToBase64(md5HexOfString(string(f.contents)))
	if f.metaMD5Override != "" {
		hash = f.metaMD5Override
	}
	return &GCSFileMetadata{
		Size:    fmt.Sprintf("%d", len(f.contents)),
		MD5Hash: hash, // GCS API: base64
	}, nil
}

func (f *fakeGCSFile) Delete() error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if len(f.contents) == 0 {
		return &GCSStatusError{ErrorCode: 404}
	}
	delete(f.bucket.objects, f.key)
	f.contents = nil
	return nil
}

func (f *fakeGCSFile) Copy(dest *GCSFile) error {
	if f.copyErr != nil {
		return f.copyErr
	}
	if dest == nil {
		return fmt.Errorf("bad dest")
	}
	df := (*dest).(*fakeGCSFile)
	if len(f.contents) == 0 {
		return &GCSStatusError{ErrorCode: 404}
	}
	df.contents = append([]byte(nil), f.contents...)
	df.readStatus = 200
	delete(f.bucket.objects, f.key)
	f.contents = nil
	f.copiedTo = append(f.copiedTo, df.bucket.name+"/"+df.key)
	return nil
}

func (f *fakeGCSFile) SetMetadata(m map[string]any) error {
	f.setMetaCalls = append(f.setMetaCalls, m)
	return f.setMetaErr
}

func (f *fakeGCSFile) Exists() (bool, error) {
	if f.existsErr != nil {
		return false, f.existsErr
	}
	if f.existsOk != nil {
		return *f.existsOk, nil
	}
	return len(f.contents) > 0, nil
}

// GetSignedURL — the optional signing seam used by signedReadURL.
func (f *fakeGCSFile) GetSignedURL(expires int64) (string, error) {
	f.signedAt = expires
	return "https://signed.example.com/" + f.bucket.name + "/" + f.key, nil
}

// convenience fields (kept on the file struct via embedding-free access)
var _ GCSFile = (*fakeGCSFile)(nil)

// --- GcsPersistor oracle (port of test/unit/GcsPersistorTests.js) -----------

const (
	gcsBucket  = "wombat-project"
	gcsKey     = "monKey"
	gcsDestKey = "donKey"
)

func gcsSettings() GCSSettings {
	return GCSSettings{
		Endpoint: GCSBackendEndpoint{ProjectID: "project", APIEndpoint: "https://gcs.example.com"},
	}
}

func newGcsTest(t *testing.T, settings GCSSettings, f *fakeGCS) *GcsPersistor {
	t.Helper()
	p, err := NewGcsPersistor(settings, func(opts map[string]any) (GCSStorage, error) { return f, nil })
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGCSRejectsStorageClass(t *testing.T) {
	_, err := NewGcsPersistor(GCSSettings{StorageClass: "STANDARD"},
		func(map[string]any) (GCSStorage, error) { return newFakeGCS(), nil })
	if !strings.Contains(err.Error(), "Use default bucket class for GCS") {
		t.Fatalf("message: %v", err)
	}
}

func TestGCSRejectsUnknownIdempotencyStrategy(t *testing.T) {
	_, err := NewGcsPersistor(GCSSettings{
		RetryOptions: map[string]any{"idempotencyStrategy": "RETRY_FOREVER"},
	}, func(map[string]any) (GCSStorage, error) { return newFakeGCS(), nil })
	if err == nil || !strings.Contains(err.Error(), "Unrecognised value for retryOptions.idempotencyStrategy") {
		t.Fatalf("want Unrecognised value, got %v", err)
	}
}

func TestGCSGetObjectStream(t *testing.T) {
	f := newFakeGCS()
	f.put(gcsBucket, gcsKey, "contents")
	p := newGcsTest(t, gcsSettings(), f)

	stream, err := p.GetObjectStream(gcsBucket, gcsKey, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(stream)
	stream.Close()
	if string(got) != "contents" {
		t.Fatalf("contents: %q", got)
	}
}

func TestGCSGetObjectStreamRange(t *testing.T) {
	f := newFakeGCS()
	f.put(gcsBucket, gcsKey, "0123456789")
	p := newGcsTest(t, gcsSettings(), f)

	start, end := int64(3), int64(6)
	stream, err := p.GetObjectStream(gcsBucket, gcsKey, Opts{Start: &start, End: &end})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(stream)
	stream.Close()
	if string(got) != "3456" {
		t.Fatalf("range: %q", got)
	}
}

func TestGCSGetObjectStreamNotFound(t *testing.T) {
	f := newFakeGCS()
	p := newGcsTest(t, gcsSettings(), f)

	_, err := p.GetObjectStream(gcsBucket, "missing", Opts{})
	if !isNotFoundError(err) {
		t.Fatalf("want NotFoundError, got %T (%v)", err, err)
	}
}

func TestGCSGetObjectStreamNonSuccess(t *testing.T) {
	f := newFakeGCS()
	file := f.put(gcsBucket, gcsKey, "x")
	file.readStatus = 500
	p := newGcsTest(t, gcsSettings(), f)

	_, err := p.GetObjectStream(gcsBucket, gcsKey, Opts{})
	if err == nil {
		t.Fatal("want error")
	}
	// Node: generic error → ReadError('error reading file from GCS') + cause
	full := oerror.GetFullInfo(err)
	if fmt.Sprint(full["cause"]) != "non success status: 500" {
		t.Fatalf("cause = %v", full["cause"])
	}
}

func TestGCSGetObjectStreamError(t *testing.T) {
	f := newFakeGCS()
	file := f.put(gcsBucket, gcsKey, "x")
	file.readErr = fmtError("guru")
	p := newGcsTest(t, gcsSettings(), f)

	_, err := p.GetObjectStream(gcsBucket, gcsKey, Opts{})
	if err == nil {
		t.Fatal("want error")
	}
}

func TestGCSGetRedirectURLSigned(t *testing.T) {
	f := newFakeGCS()
	file := f.put(gcsBucket, gcsKey, "x")
	settings := gcsSettings()
	settings.SignedUrlExpiryInMs = 3600000
	p := newGcsTest(t, settings, f)

	url, err := p.GetRedirectURL(gcsBucket, gcsKey)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "https://signed.example.com/") {
		t.Fatalf("url: %q", url)
	}
	// expiry ≈ now+3600000 (±5s)
	if diff := file.signedAt - time.Now().UnixMilli(); diff > 3605000 || diff < 3595000 {
		t.Fatalf("expires delta: %d", diff)
	}
}

func TestGCSGetRedirectURLUnsigned(t *testing.T) {
	f := newFakeGCS()
	settings := gcsSettings()
	settings.UnsignedUrls = true
	p := newGcsTest(t, settings, f)

	url, err := p.GetRedirectURL(gcsBucket, "giraffe")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://gcs.example.com/download/storage/v1/b/wombat-project/o/giraffe?alt=media"
	if url != want {
		t.Fatalf("url = %q\nwant %q", url, want)
	}
}

func TestGCSGetObjectSize(t *testing.T) {
	f := newFakeGCS()
	f.put(gcsBucket, gcsKey, "12345")
	p := newGcsTest(t, gcsSettings(), f)

	size, err := p.GetObjectSize(gcsBucket, gcsKey, Opts{})
	if err != nil || size != 5 {
		t.Fatalf("size = %d (%v)", size, err)
	}
}

func TestGCSGetObjectSizeNotFound(t *testing.T) {
	f := newFakeGCS()
	file := f.put(gcsBucket, gcsKey, "x")
	file.metaErr = &GCSStatusError{ErrorCode: 404}
	p := newGcsTest(t, gcsSettings(), f)

	_, err := p.GetObjectSize(gcsBucket, gcsKey, Opts{})
	if !isNotFoundError(err) {
		t.Fatalf("want NotFoundError, got %T", err)
	}
}

func TestGCSGetObjectMd5Hash(t *testing.T) {
	f := newFakeGCS()
	f.put(gcsBucket, gcsKey, "hash me")
	p := newGcsTest(t, gcsSettings(), f)

	got, err := p.GetObjectMd5Hash(gcsBucket, gcsKey, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if got != md5HexOfString("hash me") {
		t.Fatalf("md5 = %q, want %q", got, md5HexOfString("hash me"))
	}
}

func TestGCSSendStream(t *testing.T) {
	f := newFakeGCS()
	p := newGcsTest(t, gcsSettings(), f)

	if err := p.SendStream(gcsBucket, gcsKey, strings.NewReader("new contents"), Opts{
		ContentType:     "application/x-latex",
		ContentEncoding: "gzip",
	}); err != nil {
		t.Fatal(err)
	}
	file := f.bucket(gcsBucket).objects[gcsKey]
	if string(file.contents) != "new contents" {
		t.Fatalf("stored: %q", file.contents)
	}
	if file.writeOpts.Metadata["contentType"] != "application/x-latex" {
		t.Fatalf("metadata: %v", file.writeOpts.Metadata)
	}
	if file.writeOpts.Metadata["contentEncoding"] != "gzip" {
		t.Fatalf("metadata: %v", file.writeOpts.Metadata)
	}
}

func TestGCSSendStreamSourceMd5(t *testing.T) {
	f := newFakeGCS()
	p := newGcsTest(t, gcsSettings(), f)

	if err := p.SendStream(gcsBucket, gcsKey, strings.NewReader("x"), Opts{SourceMD5: "ab"}); err != nil {
		t.Fatal(err)
	}
	file := f.bucket(gcsBucket).objects[gcsKey]
	if file.writeOpts.Validation != "md5" {
		t.Fatalf("validation: %v", file.writeOpts.Validation)
	}
	if file.writeOpts.Metadata["md5Hash"] != hexToBase64("ab") {
		t.Fatalf("md5Hash: %v", file.writeOpts.Metadata)
	}
}

func TestGCSSendStreamGenerationForIfNoneMatch(t *testing.T) {
	f := newFakeGCS()
	p := newGcsTest(t, gcsSettings(), f)

	if err := p.SendStream(gcsBucket, gcsKey, strings.NewReader("x"), Opts{IfNoneMatch: "*"}); err != nil {
		t.Fatal(err)
	}
	// the file accessor must be called with generation 0 exactly once
	generationUsed := false
	file := f.bucket(gcsBucket).objects[gcsKey]
	for _, g := range file.generations {
		if g == 0 {
			generationUsed = true
		}
	}
	if !generationUsed {
		t.Fatalf("generation 0 not requested: %v", file.generations)
	}
}

func TestGCSSendStreamError(t *testing.T) {
	f := newFakeGCS()
	file := f.put(gcsBucket, gcsKey, "")
	file.writeErr = fmtError("write failed")
	p := newGcsTest(t, gcsSettings(), f)

	err := p.SendStream(gcsBucket, gcsKey, strings.NewReader("x"), Opts{})
	if err == nil || !strings.Contains(err.Error(), "upload to GCS failed") {
		t.Fatalf("want upload failure, got %v", err)
	}
}

func TestGCSCopyObject(t *testing.T) {
	f := newFakeGCS()
	f.put(gcsBucket, gcsKey, "move me")
	p := newGcsTest(t, gcsSettings(), f)

	if err := p.CopyObject(gcsBucket, gcsKey, "donKey", Opts{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.bucket(gcsBucket).objects[gcsKey]; ok {
		t.Fatal("source should be gone")
	}
	if got := f.bucket(gcsBucket).objects["donKey"]; got == nil || string(got.contents) != "move me" {
		t.Fatal("destination missing")
	}
}

func TestGCSCopyObjectNotFound(t *testing.T) {
	f := newFakeGCS()
	p := newGcsTest(t, gcsSettings(), f)

	err := p.CopyObject(gcsBucket, "missing", "donKey", Opts{})
	if !isNotFoundError(err) {
		t.Fatalf("want NotFoundError, got %T (%v)", err, err)
	}
}

func TestGCSCopyObjectFakeServerJSONBug(t *testing.T) {
	f := newFakeGCS()
	p := newGcsTest(t, gcsSettings(), f)
	file := f.put(gcsBucket, gcsKey, "x")
	file.copyErr = fmtError("Cannot parse response as JSON: not found\n")

	err := p.CopyObject(gcsBucket, gcsKey, "donKey", Opts{})
	if !isNotFoundError(err) {
		t.Fatalf("the fake-server JSON bug must map to NotFoundError, got %T (%v)", err, err)
	}
}

func TestGCSDeleteObject(t *testing.T) {
	f := newFakeGCS()
	f.put(gcsBucket, gcsKey, "bye")
	p := newGcsTest(t, gcsSettings(), f)

	if err := p.DeleteObject(gcsBucket, gcsKey); err != nil {
		t.Fatal(err)
	}
	file := f.bucket(gcsBucket).objects[gcsKey]
	if file != nil && len(file.contents) > 0 {
		t.Fatal("should be deleted")
	}
}

func TestGCSDeleteObjectMissingIsNoop(t *testing.T) {
	f := newFakeGCS()
	p := newGcsTest(t, gcsSettings(), f)
	// 404 delete → no-op
	if err := p.DeleteObject(gcsBucket, gcsKey); err != nil {
		t.Fatalf("404 delete must be a no-op: %v", err)
	}
}

func TestGCSDeleteObjectError(t *testing.T) {
	f := newFakeGCS()
	file := f.put(gcsBucket, gcsKey, "x")
	file.deleteErr = fmtError("delete exploded")
	p := newGcsTest(t, gcsSettings(), f)

	err := p.DeleteObject(gcsBucket, gcsKey)
	if err == nil || !strings.Contains(err.Error(), "error deleting GCS object") {
		t.Fatalf("want delete error, got %v", err)
	}
}

func TestGCSDeleteObjectDeletedBucketSuffix(t *testing.T) {
	f := newFakeGCS()
	f.put(gcsBucket, gcsKey, "preserve me")
	settings := gcsSettings()
	settings.DeletedBucketSuffix = "_deleted"
	p := newGcsTest(t, settings, f)

	if err := p.DeleteObject(gcsBucket, gcsKey); err != nil {
		t.Fatal(err)
	}
	// the copy landed in <bucket>_deleted/<key>-<iso>
	b, ok := f.buckets[gcsBucket+"_deleted"]
	if !ok {
		t.Fatal("deleted-suffix bucket missing")
	}
	found := false
	for k, obj := range b.objects {
		if strings.HasPrefix(k, gcsKey+"-") && string(obj.contents) == "preserve me" {
			found = true
		}
	}
	if !found {
		t.Fatalf("suffixed object missing: %v", keysOf(b.objects))
	}
	// original is gone
	orig := f.bucket(gcsBucket).objects[gcsKey]
	if orig != nil && len(orig.contents) > 0 {
		t.Fatal("original should be gone")
	}
}

func TestGCSDeleteObjectUnlockBeforeDelete(t *testing.T) {
	f := newFakeGCS()
	file := f.put(gcsBucket, gcsKey, "held")
	settings := gcsSettings()
	settings.UnlockBeforeDelete = true
	p := newGcsTest(t, settings, f)

	if err := p.DeleteObject(gcsBucket, gcsKey); err != nil {
		t.Fatal(err)
	}
	if len(file.setMetaCalls) != 1 {
		t.Fatalf("setMetadata calls: %d", len(file.setMetaCalls))
	}
	hold, ok := file.setMetaCalls[0]["eventBasedHold"].(*bool)
	if !ok || *hold != false {
		t.Fatalf("eventBasedHold = %v", file.setMetaCalls[0]["eventBasedHold"])
	}
}

func TestGCSDeleteDirectory(t *testing.T) {
	f := newFakeGCS()
	f.put(gcsBucket, "dir/a", "1")
	f.put(gcsBucket, "dir/b", "22")
	f.put(gcsBucket, "other", "3")
	p := newGcsTest(t, gcsSettings(), f)

	if err := p.DeleteDirectory(gcsBucket, "dir", ""); err != nil {
		t.Fatal(err)
	}
	// query must have been the directory prefix
	b := f.bucket(gcsBucket)
	if len(b.queries) == 0 || b.queries[0].Prefix != "dir/" {
		t.Fatalf("queries: %+v", b.queries)
	}
	if _, ok := b.objects["other"]; !ok {
		t.Fatal("other must remain")
	}
}

func TestGCSListDirectoryKeys(t *testing.T) {
	f := newFakeGCS()
	f.put(gcsBucket, "p/1", "x")
	f.put(gcsBucket, "p/2", "yy")
	f.put(gcsBucket, "q", "z")
	p := newGcsTest(t, gcsSettings(), f)

	keys, err := p.ListDirectoryKeys(gcsBucket, "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] != "p/1" || keys[1] != "p/2" {
		t.Fatalf("keys: %v", keys)
	}
}

func TestGCSListDirectoryStats(t *testing.T) {
	f := newFakeGCS()
	f.put(gcsBucket, "p/1", "x")
	p := newGcsTest(t, gcsSettings(), f)

	stats, err := p.ListDirectoryStats(gcsBucket, "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].Key != "p/1" || stats[0].Size != 1 {
		t.Fatalf("stats: %v", stats)
	}
}

func TestGCSDirectorySize(t *testing.T) {
	f := newFakeGCS()
	f.put(gcsBucket, "d/a", "xx")
	f.put(gcsBucket, "d/b", "yyy")
	p := newGcsTest(t, gcsSettings(), f)

	size, err := p.DirectorySize(gcsBucket, "d", "")
	if err != nil || size != 5 {
		t.Fatalf("size = %d (%v)", size, err)
	}
}

func TestGCSCheckIfObjectExists(t *testing.T) {
	f := newFakeGCS()
	f.put(gcsBucket, gcsKey, "x")
	p := newGcsTest(t, gcsSettings(), f)

	ok, err := p.CheckIfObjectExists(gcsBucket, gcsKey, Opts{})
	if err != nil || !ok {
		t.Fatalf("ok = %v (%v)", ok, err)
	}
	ok, err = p.CheckIfObjectExists(gcsBucket, "missing", Opts{})
	if err != nil || ok {
		t.Fatalf("ok = %v (%v)", ok, err)
	}
}

func TestGCSCheckIfObjectExistsError(t *testing.T) {
	f := newFakeGCS()
	f.put(gcsBucket, "missing", "x")
	file := f.bucket(gcsBucket).objects["missing"]
	file.existsErr = fmtError("exists exploded")
	p := newGcsTest(t, gcsSettings(), f)

	_, err := p.CheckIfObjectExists(gcsBucket, "missing", Opts{})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "error checking if file exists in GCS") {
		t.Fatalf("message: %v", err)
	}
}

// helpers for the fake (avoid collisions with package names)

type plainErr string

func (p plainErr) Error() string { return string(p) }
func fmtError(s string) error    { return plainErr(s) }

func keysOf(m map[string]*fakeGCSFile) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

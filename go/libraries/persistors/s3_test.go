package persistors

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"ollitex/go/libraries/oerror"
)

// fakeS3 — the Go stand-in for test/unit/S3ClientMock.js: per-command
// response queues + call recording (name, params), assertable afterwards.
type fakeS3 struct {
	t     *testing.T
	mu    sync.Mutex
	calls []fakeS3Call

	gets     []fakeS3Outcome
	puts     []fakeS3Outcome
	heads    []fakeS3Outcome
	deletes  []fakeS3Outcome
	batchDel []fakeS3Outcome
	lists    []fakeS3Outcome
	copies   []fakeS3Outcome
	buckets  []fakeS3Outcome
	presigns []fakeS3Outcome
	counts   map[string]int
}

type fakeS3Call struct {
	name   string
	params string // stable rendering of the payload
}

type fakeS3Outcome struct {
	err  error
	get  *GetObjectResult
	head *HeadObjectResult
	list *ListObjectsV2Result
	url  string
}

func newFakeS3(t *testing.T) *fakeS3 { return &fakeS3{t: t, counts: map[string]int{}} }

func (f *fakeS3) record(name, params string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeS3Call{name: name, params: params})
}

// next returns the queued outcome for `kind` (index auto-advances) or a
// default success — mirroring sinon's mockSend queue.
func (f *fakeS3) next(kind string) (fakeS3Outcome, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q, i := f.queue(kind), f.counts[kind]
	f.counts[kind] = i + 1
	if i < len(q) {
		return q[i], i
	}
	return fakeS3Outcome{}, i
}

var _ = fakeS3Call{}

// NOTE: queue accessors below keep the bookkeeping simple (per-kind counters).
func (f *fakeS3) queue(kind string) []fakeS3Outcome {
	switch kind {
	case "get":
		return f.gets
	case "put":
		return f.puts
	case "head":
		return f.heads
	case "delete":
		return f.deletes
	case "batchdelete":
		return f.batchDel
	case "list":
		return f.lists
	case "copy":
		return f.copies
	case "bucket":
		return f.buckets
	case "presign":
		return f.presigns
	}
	return nil
}

func (f *fakeS3) PutObject(ctx context.Context, p PutObjectParams) error {
	body, _ := io.ReadAll(p.Body)
	f.record("PutObject", s3param{
		"Bucket": p.Bucket, "Key": p.Key, "StorageClass": p.StorageClass,
		"ContentType": p.ContentType, "ContentEncoding": p.ContentEncoding,
		"IfNoneMatch": p.IfNoneMatch, "Body": body, "PutSSEC": p.PutSSEC,
	}.String())
	out, _ := f.next("put")
	return out.err
}

func (f *fakeS3) GetObject(ctx context.Context, p GetObjectParams) (*GetObjectResult, error) {
	f.record("GetObject", s3param{
		"Bucket": p.Bucket, "Key": p.Key, "Range": p.Range, "GetSSEC": p.GetSSEC,
	}.String())
	out, _ := f.next("get")
	if out.err != nil {
		return nil, out.err
	}
	if out.get != nil {
		return out.get, nil
	}
	return &GetObjectResult{Body: io.NopCloser(bytes.NewReader([]byte("s3-body")))}, nil
}

func (f *fakeS3) HeadObject(ctx context.Context, bucket, key string, getSSEC map[string]any) (*HeadObjectResult, error) {
	f.record("HeadObject", s3param{"Bucket": bucket, "Key": key, "GetSSEC": getSSEC}.String())
	out, _ := f.next("head")
	if out.err != nil {
		return nil, out.err
	}
	if out.head != nil {
		return out.head, nil
	}
	n := int64(5555)
	return &HeadObjectResult{ContentLength: &n, ETag: `"ffffffff00000000ffffffff00000000"`}, nil
}

func (f *fakeS3) DeleteObject(ctx context.Context, bucket, key string) error {
	f.record("DeleteObject", s3param{"Bucket": bucket, "Key": key}.String())
	out, _ := f.next("delete")
	return out.err
}

func (f *fakeS3) DeleteObjects(ctx context.Context, p DeleteObjectsParams) error {
	keys := make([]string, 0, len(p.Objects))
	for _, o := range p.Objects {
		keys = append(keys, o.Key)
	}
	f.record("DeleteObjects", s3param{"Bucket": p.Bucket, "Keys": keys, "Quiet": p.Quiet}.String())
	out, _ := f.next("batchdelete")
	return out.err
}

func (f *fakeS3) ListObjectsV2(ctx context.Context, p ListObjectsV2Params) (*ListObjectsV2Result, error) {
	f.record("ListObjectsV2", s3param{"Bucket": p.Bucket, "Prefix": p.Prefix, "Token": p.ContinuationToken}.String())
	out, _ := f.next("list")
	if out.err != nil {
		return nil, out.err
	}
	if out.list != nil {
		return out.list, nil
	}
	// default: the Node 'files' fixture
	return &ListObjectsV2Result{
		Contents: []S3Object{{Key: "llama", Size: 11}, {Key: "hippo", Size: 22}},
	}, nil
}

func (f *fakeS3) CopyObject(ctx context.Context, p CopyObjectParams) error {
	f.record("CopyObject", s3param{"Bucket": p.Bucket, "Key": p.Key, "CopySource": p.CopySource}.String())
	out, _ := f.next("copy")
	return out.err
}

func (f *fakeS3) CreateBucket(ctx context.Context, bucket string) error {
	f.record("CreateBucket", s3param{"Bucket": bucket}.String())
	out, _ := f.next("bucket")
	return out.err
}

func (f *fakeS3) PresignGetObject(ctx context.Context, bucket, key string, expiry time.Duration) (string, error) {
	f.record("PresignGetObject", s3param{"Bucket": bucket, "Key": key, "ExpiryMs": expiry.Milliseconds()}.String())
	out, _ := f.next("presign")
	if out.err != nil {
		return "", out.err
	}
	if out.url != "" {
		return out.url, nil
	}
	return "https://wombat.potato/giraffe?X-Amz-Expires=3600", nil
}

// s3param — a stable, order-independent rendering of command params for
// call assertions (Node: chai deep.equal on the payload object).
type s3param map[string]any

func (p s3param) String() string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sortStrings(keys)
	parts := make([]string, 0, len(p))
	for _, k := range keys {
		parts = append(parts, k+"="+renderAny(p[k]))
	}
	return strings.Join(parts, " ")
}

func renderAny(v any) string {
	switch t := v.(type) {
	case nil:
		return "nil"
	case string:
		return t
	case []byte:
		return string(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int64:
		return itoa64(t)
	case map[string]any:
		return "map"
	case []string:
		return "[" + strings.Join(t, ",") + "]"
	default:
		return "v"
	}
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := ""
	for n > 0 {
		digits = digit(n%10) + digits
		n /= 10
	}
	if neg {
		return "-" + digits
	}
	return digits
}

func digit(d int64) string { return string("0123456789"[d%10]) }

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

// assertSent — the Go assertSendCalledWith: a call with the name exists and
// (optionally) its params include the given fragments.
func (f *fakeS3) assertSent(name string, fragments ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.name == name {
			all := true
			for _, frag := range fragments {
				if !strings.Contains(c.params, frag) {
					all = false
					break
				}
			}
			if all {
				return
			}
		}
	}
	f.t.Fatalf("expected a %s call%s, got: %v", name, fragments, f.calls)
}

func (f *fakeS3) assertNotSent(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.name == name {
			f.t.Fatalf("expected no %s call, got %v", name, f.calls)
		}
	}
}

func (f *fakeS3) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls, f.gets, f.puts, f.heads, f.deletes = nil, nil, nil, nil, nil
	f.batchDel, f.lists, f.copies, f.buckets, f.presigns = nil, nil, nil, nil, nil
	f.counts = map[string]int{}
}

// --- S3Persistor oracle (port of test/unit/S3PersistorTests.js) -------------

const (
	s3Bucket      = "womBucket"
	s3Key         = "monKey"
	s3DestKey     = "donKey"
	s3ObjectSize  = 5555
	s3MD5         = "ffffffff00000000ffffffff00000000"
	s3RedirectURL = "https://wombat.potato/giraffe"
)

func newTestS3Persistor(t *testing.T, settings S3Settings, f *fakeS3) *S3Persistor {
	if settings.Key == "" && settings.Secret == "" {
		settings.Key, settings.Secret = "frog", "prince"
	}
	return NewS3Persistor(settings, func(bucket string) (S3Client, error) { return f, nil })
}

func TestS3GetObjectStreamValid(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)

	stream, err := p.GetObjectStream(s3Bucket, s3Key, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(stream)
	stream.Close()
	if string(got) != "s3-body" {
		t.Fatalf("contents: %q", got)
	}
	f.assertSent("GetObject", "Bucket="+s3Bucket, "Key="+s3Key)
}

func TestS3GetObjectStreamByteRange(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{Key: "frog", Secret: "prince"}, f)

	start, end := int64(5), int64(16)
	if _, err := p.GetObjectStream(s3Bucket, s3Key, Opts{Start: &start, End: &end}); err != nil {
		t.Fatal(err)
	}
	f.assertSent("GetObject", "Range=bytes=5-16")
}

func TestS3GetObjectStreamNotFound(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets, fakeS3Outcome{err: &S3Error{ErrorCode: "NoSuchKey", Message: "nf"}})
	p := newTestS3Persistor(t, S3Settings{}, f)

	_, err := p.GetObjectStream(s3Bucket, s3Key, Opts{})
	if !isNotFoundError(err) {
		t.Fatalf("want NotFoundError, got %T (%v)", err, err)
	}
	// Node: NoSuchKey → NotFoundError('no such file') via the wrapError not-found
	// mapping (the "error reading file from S3" message is swallowed for 404s).
	full := oerrorFullInfo(err)
	if full["bucketName"] != s3Bucket || full["key"] != s3Key {
		t.Fatalf("info: %v", full)
	}
	if full["cause"] == nil {
		t.Fatalf("cause missing: %v", full)
	}
}

func TestS3GetObjectStreamAccessDeniedIsNotFound(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets, fakeS3Outcome{err: &S3Error{ErrorCode: "AccessDenied"}})
	p := newTestS3Persistor(t, S3Settings{}, f)

	_, err := p.GetObjectStream(s3Bucket, s3Key, Opts{})
	if !isNotFoundError(err) {
		t.Fatalf("AccessDenied must map to NotFoundError, got %T", err)
	}
}

func TestS3GetObjectStreamUnknownErrorReadError(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets, fakeS3Outcome{err: errors.New("guru meditation error")})
	p := newTestS3Persistor(t, S3Settings{}, f)

	_, err := p.GetObjectStream(s3Bucket, s3Key, Opts{})
	if _, isRead := asPersistorError(err).(*ReadError); !isRead {
		t.Fatalf("want ReadError, got %T", err)
	}
	if !strings.Contains(err.Error(), "error reading file from S3") {
		t.Fatalf("message: %v", err)
	}
}

func TestS3GetObjectStreamSSECNotfound(t *testing.T) {
	f := newFakeS3(t)
	f.gets = append(f.gets, fakeS3Outcome{err: &S3Error{ErrorCode: "NoSuchKey"}})
	p := newTestS3Persistor(t, S3Settings{}, f)

	ssec := NewSSECOptions(make([]byte, 32))
	_, err := p.GetObjectStream(s3Bucket, s3Key, Opts{SSEC: ssec})
	if !isNotFoundError(err) {
		t.Fatalf("want NotFoundError, got %v", err)
	}
	f.assertSent("GetObject", "GetSSEC=map")
}

func TestS3GetRedirectUrl(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{SignedUrlExpiryInMs: 3600000}, f)

	url, err := p.GetRedirectURL(s3Bucket, s3Key)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, s3RedirectURL) {
		t.Fatalf("url: %q", url)
	}
	f.assertSent("PresignGetObject", "Bucket="+s3Bucket, "Key="+s3Key, "ExpiryMs=3600000")
}

func TestS3GetObjectSize(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)

	size, err := p.GetObjectSize(s3Bucket, s3Key, Opts{})
	if err != nil || size != s3ObjectSize {
		t.Fatalf("size = %d (%v)", size, err)
	}
	f.assertSent("HeadObject", "Bucket="+s3Bucket, "Key="+s3Key)
}

func TestS3GetObjectSizeNotFound(t *testing.T) {
	f := newFakeS3(t)
	f.heads = append(f.heads, fakeS3Outcome{err: &S3Error{ErrorCode: "NoSuchKey"}})
	p := newTestS3Persistor(t, S3Settings{}, f)

	_, err := p.GetObjectSize(s3Bucket, s3Key, Opts{})
	if !isNotFoundError(err) {
		t.Fatalf("want NotFoundError, got %T", err)
	}
	// Node: NoSuchKey maps to NotFoundError('no such file'); the cause is the S3 error.
	if oerror.GetFullInfo(err)["cause"] == nil {
		t.Fatalf("cause missing: %v", oerror.GetFullInfo(err))
	}
}

func TestS3GetObjectSizeErrorRead(t *testing.T) {
	f := newFakeS3(t)
	f.heads = append(f.heads, fakeS3Outcome{err: errors.New("boom")})
	p := newTestS3Persistor(t, S3Settings{}, f)

	_, err := p.GetObjectSize(s3Bucket, s3Key, Opts{})
	if _, isRead := asPersistorError(err).(*ReadError); !isRead {
		t.Fatalf("want ReadError, got %T", err)
	}
}

func TestS3SendStreamBasic(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)
	body := "hello s3"

	if err := p.SendStream(s3Bucket, s3Key, strings.NewReader(body), Opts{}); err != nil {
		t.Fatal(err)
	}
	f.assertSent("PutObject", "Bucket="+s3Bucket, "Key="+s3Key, "Body=hello s3")
}

func TestS3SendStreamMetadata(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)

	start := int64(5)
	opts := Opts{
		ContentType:     "application/x-latex",
		ContentEncoding: "gzip",
		ContentLength:   111,
		IfNoneMatch:     "*",
	}
	if err := p.SendStream(s3Bucket, s3Key, strings.NewReader("x"), opts); err != nil {
		t.Fatal(err)
	}
	f.assertSent("PutObject", "ContentType=application/x-latex", "ContentEncoding=gzip", "IfNoneMatch=*")
	_ = start
}

func TestS3SendStreamSourceMd5Rejected(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)

	err := p.SendStream(s3Bucket, s3Key, strings.NewReader("x"), Opts{SourceMD5: "deadbeef"})
	if err == nil {
		t.Fatal("sourceMd5 must be rejected")
	}
	if !strings.Contains(err.Error(), "upload to S3 failed") {
		t.Fatalf("outer message: %v", err)
	}
	// Node: the sourceMd5 message lives in the cause chain (WriteError → WriteError).
	if oerror.GetFullInfo(err)["cause"] == nil {
		t.Fatalf("cause missing: %v", oerror.GetFullInfo(err))
	}
}

func TestS3SendStreamUploadError(t *testing.T) {
	f := newFakeS3(t)
	f.puts = append(f.puts, fakeS3Outcome{err: errors.New("upload blew")})
	p := newTestS3Persistor(t, S3Settings{}, f)

	err := p.SendStream(s3Bucket, s3Key, strings.NewReader("x"), Opts{})
	if _, isWrite := asPersistorError(err).(*WriteError); !isWrite {
		t.Fatalf("want WriteError, got %T", err)
	}
	if !strings.Contains(err.Error(), "upload to S3 failed") {
		t.Fatalf("message: %v", err)
	}
}

func TestS3SendFile(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)
	src := mustTempFile(t, "file bytes here")

	if err := p.SendFile(s3Bucket, s3Key, src); err != nil {
		t.Fatal(err)
	}
	f.assertSent("PutObject", "Body=file bytes here")
}

func TestS3GetObjectMd5HashFromETag(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)

	md5h, err := p.GetObjectMd5Hash(s3Bucket, s3Key, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if md5h != s3MD5 {
		t.Fatalf("md5 = %q", md5h)
	}
	f.assertSent("HeadObject")
	f.assertNotSent("GetObject")
}

func TestS3GetObjectMd5HashDownloadPath(t *testing.T) {
	f := newFakeS3(t)
	f.heads = append(f.heads, fakeS3Outcome{head: &HeadObjectResult{ETag: "multipart-a-1"}})
	f.gets = append(f.gets, fakeS3Outcome{get: &GetObjectResult{
		Body: io.NopCloser(bytes.NewReader([]byte("somebytes"))),
	}})
	ml := &metricsRecorder{}
	oldM := Metrics
	Metrics = ml
	defer func() { Metrics = oldM }()
	p := newTestS3Persistor(t, S3Settings{}, f)

	md5h, err := p.GetObjectMd5Hash(s3Bucket, s3Key, Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if md5h != md5HexOfString("somebytes") {
		t.Fatalf("md5 = %q, want %q", md5h, md5HexOfString("somebytes"))
	}
	found := false
	for _, m := range ml.incs {
		if m == "s3.md5Download" {
			found = true
		}
	}
	if !found {
		t.Fatalf("s3.md5Download not incremented: %+v", ml.incs)
	}
}

func TestS3CopyObject(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)

	if err := p.CopyObject(s3Bucket, s3Key, s3DestKey, Opts{}); err != nil {
		t.Fatal(err)
	}
	f.assertSent("CopyObject", "Bucket="+s3Bucket, "Key="+s3DestKey, "CopySource=/womBucket/monKey")
}

func TestS3CopyObjectSSEC(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)
	ssec := NewSSECOptions(make([]byte, 32))
	ssecSrc := NewSSECOptions(make([]byte, 32))

	if err := p.CopyObject(s3Bucket, s3Key, s3DestKey, Opts{SSEC: ssec, SSECSrc: ssecSrc}); err != nil {
		t.Fatal(err)
	}
	f.assertSent("CopyObject")
}

func TestS3DeleteObject(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)

	if err := p.DeleteObject(s3Bucket, s3Key); err != nil {
		t.Fatal(err)
	}
	f.assertSent("DeleteObject", "Bucket="+s3Bucket, "Key="+s3Key)
}

func TestS3DeleteDirectoryValid(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)

	if err := p.DeleteDirectory(s3Bucket, s3Key, ""); err != nil {
		t.Fatal(err)
	}
	f.assertSent("ListObjectsV2", "Bucket="+s3Bucket, "Prefix="+s3Key)
	f.assertSent("DeleteObjects", "Bucket="+s3Bucket, "Keys=[llama,hippo]", "Quiet=true")
}

func TestS3DeleteDirectoryEmpty(t *testing.T) {
	f := newFakeS3(t)
	f.lists = append(f.lists, fakeS3Outcome{list: &ListObjectsV2Result{Contents: nil}})
	p := newTestS3Persistor(t, S3Settings{}, f)

	if err := p.DeleteDirectory(s3Bucket, s3Key, ""); err != nil {
		t.Fatal(err)
	}
	f.assertSent("ListObjectsV2")
	f.assertNotSent("DeleteObjects")
}

func TestS3DeleteDirectoryContinuation(t *testing.T) {
	f := newFakeS3(t)
	f.lists = append(
		f.lists,
		fakeS3Outcome{list: &ListObjectsV2Result{
			Contents: []S3Object{{Key: "a"}}, IsTruncated: true, NextContinuationToken: "TOK1",
		}},
		fakeS3Outcome{list: &ListObjectsV2Result{Contents: []S3Object{{Key: "b"}}}},
	)
	p := newTestS3Persistor(t, S3Settings{}, f)

	if err := p.DeleteDirectory(s3Bucket, s3Key, ""); err != nil {
		t.Fatal(err)
	}
	// second list with the continuation token
	found := false
	f.mu.Lock()
	for _, c := range f.calls {
		if c.name == "ListObjectsV2" && strings.Contains(c.params, "Token=TOK1") {
			found = true
		}
	}
	f.mu.Unlock()
	if !found {
		t.Fatalf("second list with continuation token missing: %+v", f.calls)
	}
}

func TestS3DeleteDirectoryListError(t *testing.T) {
	f := newFakeS3(t)
	f.lists = append(f.lists, fakeS3Outcome{err: errors.New("list failed")})
	p := newTestS3Persistor(t, S3Settings{}, f)

	err := p.DeleteDirectory(s3Bucket, s3Key, "")
	if _, isRead := asPersistorError(err).(*ReadError); !isRead {
		t.Fatalf("want ReadError, got %T", err)
	}
	if !strings.Contains(err.Error(), "failed to list objects in S3") {
		t.Fatalf("message: %v", err)
	}
	f.assertNotSent("DeleteObjects")
}

func TestS3DeleteDirectoryDeleteError(t *testing.T) {
	f := newFakeS3(t)
	f.batchDel = append(f.batchDel, fakeS3Outcome{err: errors.New("batch failed")})
	p := newTestS3Persistor(t, S3Settings{}, f)

	err := p.DeleteDirectory(s3Bucket, s3Key, "")
	if _, isWrite := asPersistorError(err).(*WriteError); !isWrite {
		t.Fatalf("want WriteError, got %T", err)
	}
	if !strings.Contains(err.Error(), "failed to delete objects in S3") {
		t.Fatalf("message: %v", err)
	}
}

func TestS3DirectorySize(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)

	size, err := p.DirectorySize(s3Bucket, s3Key, "")
	if err != nil || size != 33 {
		t.Fatalf("size = %d (%v)", size, err)
	}
	f.assertSent("ListObjectsV2")
}

func TestS3DirectorySizeEmpty(t *testing.T) {
	f := newFakeS3(t)
	f.lists = append(f.lists, fakeS3Outcome{list: &ListObjectsV2Result{Contents: nil}})
	p := newTestS3Persistor(t, S3Settings{}, f)

	size, err := p.DirectorySize(s3Bucket, s3Key, "")
	if err != nil || size != 0 {
		t.Fatalf("size = %d (%v)", size, err)
	}
}

func TestS3DirectorySizeContinuation(t *testing.T) {
	f := newFakeS3(t)
	f.lists = append(
		f.lists,
		fakeS3Outcome{list: &ListObjectsV2Result{
			Contents: []S3Object{{Key: "a", Size: 11}}, IsTruncated: true, NextContinuationToken: "TOK2",
		}},
		fakeS3Outcome{list: &ListObjectsV2Result{Contents: []S3Object{{Key: "b", Size: 22}}}},
	)
	p := newTestS3Persistor(t, S3Settings{}, f)

	size, err := p.DirectorySize(s3Bucket, s3Key, "")
	if err != nil || size != 33 {
		t.Fatalf("size = %d (%v)", size, err)
	}
}

func TestS3DirectorySizeListError(t *testing.T) {
	f := newFakeS3(t)
	f.lists = append(f.lists, fakeS3Outcome{err: errors.New("nope")})
	p := newTestS3Persistor(t, S3Settings{}, f)

	_, err := p.DirectorySize(s3Bucket, s3Key, "")
	if _, isRead := asPersistorError(err).(*ReadError); !isRead {
		t.Fatalf("want ReadError, got %T", err)
	}
}

func TestS3CheckIfObjectExistsTrue(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)
	ok, err := p.CheckIfObjectExists(s3Bucket, s3Key, Opts{})
	if err != nil || !ok {
		t.Fatalf("ok = %v (%v)", ok, err)
	}
	f.assertSent("HeadObject")
}

func TestS3CheckIfObjectExistsFalse(t *testing.T) {
	f := newFakeS3(t)
	f.heads = append(f.heads, fakeS3Outcome{err: &S3Error{ErrorCode: "NoSuchKey"}})
	p := newTestS3Persistor(t, S3Settings{}, f)
	ok, err := p.CheckIfObjectExists(s3Bucket, s3Key, Opts{})
	if err != nil || ok {
		t.Fatalf("ok = %v (%v)", ok, err)
	}
}

func TestS3CheckIfObjectExistsError(t *testing.T) {
	f := newFakeS3(t)
	f.heads = append(f.heads, fakeS3Outcome{err: errors.New("exploded")})
	p := newTestS3Persistor(t, S3Settings{}, f)
	_, err := p.CheckIfObjectExists(s3Bucket, s3Key, Opts{})
	if _, isRead := asPersistorError(err).(*ReadError); !isRead {
		t.Fatalf("want ReadError, got %T", err)
	}
	if !strings.Contains(err.Error(), "error checking whether S3 object exists") {
		t.Fatalf("message: %v", err)
	}
}

func TestS3ListDirectoryKeys(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)
	keys, err := p.ListDirectoryKeys(s3Bucket, s3Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] != "llama" || keys[1] != "hippo" {
		t.Fatalf("keys = %v", keys)
	}
}

func TestS3ListDirectoryKeysContinuation(t *testing.T) {
	f := newFakeS3(t)
	f.lists = append(
		f.lists,
		fakeS3Outcome{list: &ListObjectsV2Result{
			Contents: []S3Object{{Key: "a"}}, IsTruncated: true, NextContinuationToken: "TOK9",
		}},
		fakeS3Outcome{list: &ListObjectsV2Result{Contents: []S3Object{{Key: "b"}}}},
	)
	p := newTestS3Persistor(t, S3Settings{}, f)
	keys, err := p.ListDirectoryKeys(s3Bucket, s3Key)
	if err != nil || len(keys) != 2 {
		t.Fatalf("keys = %v (%v)", keys, err)
	}
}

func TestS3ListDirectoryKeysError(t *testing.T) {
	f := newFakeS3(t)
	f.lists = append(f.lists, fakeS3Outcome{err: errors.New("bad list")})
	p := newTestS3Persistor(t, S3Settings{}, f)
	_, err := p.ListDirectoryKeys(s3Bucket, s3Key)
	if _, isRead := asPersistorError(err).(*ReadError); !isRead {
		t.Fatalf("want ReadError, got %T", err)
	}
}

func TestS3ListDirectoryStats(t *testing.T) {
	f := newFakeS3(t)
	p := newTestS3Persistor(t, S3Settings{}, f)
	stats, err := p.ListDirectoryStats(s3Bucket, s3Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 || stats[0].Key != "llama" || stats[0].Size != 11 {
		t.Fatalf("stats = %v", stats)
	}
}

func TestS3ListDirectoryStatsError(t *testing.T) {
	f := newFakeS3(t)
	f.lists = append(f.lists, fakeS3Outcome{err: errors.New("stats no")})
	p := newTestS3Persistor(t, S3Settings{}, f)
	_, err := p.ListDirectoryStats(s3Bucket, s3Key)
	if _, isRead := asPersistorError(err).(*ReadError); !isRead {
		t.Fatalf("want ReadError, got %T", err)
	}
}

func TestS3GetObjectStorageClass(t *testing.T) {
	f := newFakeS3(t)
	f.heads = append(f.heads, fakeS3Outcome{head: &HeadObjectResult{StorageClass: "GLACIER"}})
	p := newTestS3Persistor(t, S3Settings{}, f)

	sc, err := p.GetObjectStorageClass(s3Bucket, s3Key, Opts{})
	if err != nil || sc != "GLACIER" {
		t.Fatalf("sc = %q (%v)", sc, err)
	}
}

func TestS3MD5FromResponse(t *testing.T) {
	cases := []struct {
		etag string
		want *string
	}{
		{`"ffffffff00000000ffffffff00000000"`, ptr(s3MD5)},
		{`ffffffff00000000ffffffff00000000`, ptr(s3MD5)},
		{"multipart-a-1", nil},
		{"", nil},
		{"ffffffff00000000ffffffff000o000g", nil},
	}
	for _, c := range cases {
		res := &HeadObjectResult{ETag: c.etag}
		got := md5FromResponse(res)
		if (got == nil) != (c.want == nil) {
			t.Errorf("etag %q: got %v, want %v", c.etag, got, c.want)
		}
		if got != nil && c.want != nil && *got != *c.want {
			t.Errorf("etag %q: %q vs %q", c.etag, *got, *c.want)
		}
	}
}

func ptr(s string) *string { return &s }

// --- client options (Node pins _buildClientOptions via the constructed
// client's config)

func TestS3BuildClientOptionsCredentials(t *testing.T) {
	p := NewS3Persistor(S3Settings{
		Key:    "frog",
		Secret: "prince",
		Region: "us-west-2",
	}, nil)
	opts := p.buildClientOptions(nil, nil)
	creds := opts["credentials"].(map[string]string)
	if creds["accessKeyId"] != "frog" || creds["secretAccessKey"] != "prince" {
		t.Fatalf("creds: %v", creds)
	}
	if opts["region"] != "us-west-2" {
		t.Fatalf("region: %v", opts["region"])
	}
}

func TestS3BuildClientOptionsBucketCredsWin(t *testing.T) {
	p := NewS3Persistor(S3Settings{Key: "frog", Secret: "prince"}, nil)
	bucketCreds := &BucketCreds{AuthKey: "alt-key", AuthSecret: "alt-secret"}
	opts := p.buildClientOptions(bucketCreds, nil)
	creds := opts["credentials"].(map[string]string)
	if creds["accessKeyId"] != "alt-key" || creds["secretAccessKey"] != "alt-secret" {
		t.Fatalf("bucket creds must win: %v", creds)
	}
}

func TestS3BuildClientOptionsDefaultProvider(t *testing.T) {
	p := NewS3Persistor(S3Settings{}, nil)
	opts := p.buildClientOptions(nil, nil)
	if _, present := opts["credentials"]; present {
		t.Fatalf("no credentials expected for the default provider chain: %v", opts)
	}
	if opts["region"] != "us-east-1" {
		t.Fatalf("default region: %v", opts["region"])
	}
}

func TestS3BuildClientOptionsEndpointSSLOptions(t *testing.T) {
	p := NewS3Persistor(S3Settings{
		Endpoint:  "https://minio.example.com",
		PathStyle: true,
	}, nil)
	opts := p.buildClientOptions(nil, nil)
	if opts["endpoint"] != "https://minio.example.com" {
		t.Fatalf("endpoint: %v", opts["endpoint"])
	}
	if opts["sslEnabled"] != true {
		t.Fatalf("sslEnabled: %v", opts)
	}
	if opts["forcePathStyle"] != true {
		t.Fatalf("forcePathStyle: %v", opts)
	}

	// http options + ca
	p2 := NewS3Persistor(S3Settings{
		Endpoint:    "https://https.example.com",
		HTTPOptions: map[string]any{"timeout": 5000},
		Ca:          "CERTDATA",
	}, nil)
	opts2 := p2.buildClientOptions(nil, nil)
	handler, ok := opts2["requestHandler"].(map[string]any)
	if !ok {
		t.Fatalf("requestHandler missing: %v", opts2)
	}
	if handler["connectionTimeout"] != 5000 {
		t.Fatalf("timeout → connectionTimeout: %v", handler)
	}
	if handler["httpsAgent"] == nil || handler["httpsAgent"].(map[string]any)["ca"] != "CERTDATA" {
		t.Fatalf("ca agent missing: %v", handler)
	}
}

func TestS3BuildClientOptionsMaxAttempts(t *testing.T) {
	p := NewS3Persistor(S3Settings{MaxRetries: 3}, nil)
	opts := p.buildClientOptions(nil, nil)
	if opts["maxAttempts"] != 4 {
		t.Fatalf("maxAttempts = %v (wants maxRetries+1)", opts["maxAttempts"])
	}
}

// oerrorFullInfo — small helper to avoid the import name in many tests.
func oerrorFullInfo(err error) map[string]any {
	return oerror.GetFullInfo(err)
}

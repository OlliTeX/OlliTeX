package persistors

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"testing"

	"ollitex/go/libraries/oerror"
)

// --- ProjectKey (Node: test/unit/ProjectKeyTests.js) -----------------------

func TestProjectKeyReversesPaddedKeys(t *testing.T) {
	if got := Format(1); got != "100/000/000" {
		t.Errorf("format(1) = %q", got)
	}
	if got := Format(12); got != "210/000/000" {
		t.Errorf("format(12) = %q", got)
	}
	if got := Format(123456789); got != "987/654/321" {
		t.Errorf("format(123456789) = %q", got)
	}
	if got := Format(9123456789); got != "987/654/3219" {
		t.Errorf("format(9123456789) = %q", got)
	}
}

func TestProjectKeyPad(t *testing.T) {
	// Node: pad(undefined) / pad(null) → both 0 → '000000000'
	if got := Pad(0); got != "000000000" {
		t.Errorf("pad(0) = %q", got)
	}
	if got := Pad(1); got != "000000001" {
		t.Errorf("pad(1) = %q", got)
	}
	if got := Pad(10); got != "000000010" {
		t.Errorf("pad(10) = %q", got)
	}
	if got := Pad(100000000); got != "100000000" {
		t.Errorf("pad(100000000) = %q", got)
	}
	if got := Pad(1000000000); got != "1000000000" {
		t.Errorf("pad(1000000000) = %q", got)
	}
}

// --- Errors (Node: class hierarchy extends OError) --------------------------

func TestErrorTypesRenderNodeNames(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{NewNotFoundError("no such file", nil), "NotFoundError: no such file"},
		{NewWriteError("boom", nil), "WriteError: boom"},
		{NewReadError("boop", nil), "ReadError: boop"},
		{NewSettingsError("no backend specified - config incomplete", nil),
			"SettingsError: no backend specified - config incomplete"},
		{NewNotImplementedError("method not implemented in persistor", nil),
			"NotImplementedError: method not implemented in persistor"},
		{NewAlreadyWrittenError("already", nil), "AlreadyWrittenError: already"},
		{NewNoKEKMatchedError("no kek matched", nil), "NoKEKMatchedError: no kek matched"},
	}
	for _, c := range cases {
		if c.err.Error() != c.want {
			t.Errorf("%v, want %q", c.err, c.want)
		}
	}
}

func TestErrorCarriesInfoAndCause(t *testing.T) {
	cause := errors.New("the underlying failure")
	err := NewReadError("error reading file from S3", map[string]any{
		"bucketName": "womBucket",
		"key":        "monKey",
	}, cause)

	full := oerror.GetFullInfo(error(err))
	if full["bucketName"] != "womBucket" || full["key"] != "monKey" {
		t.Fatalf("info not carried: %v", full)
	}
	// Node: `expect(error.cause).to.equal(originalError)` — the cause is a
	// property of the error (OError.Cause), not a key in the info map.
	if err.Cause != cause {
		t.Fatalf("cause not carried: %v", err.Cause)
	}
}

// --- wrapError (Node: PersistorHelper.wrapError) ----------------------------

func TestWrapErrorNotFoundVariants(t *testing.T) {
	// S3-style codes
	for _, code := range []string{"NoSuchKey", "NotFound", "404", "AccessDenied", "ENOENT"} {
		err := wrapError(&S3Error{ErrorCode: code, Message: "m"}, "msg", map[string]any{}, classRead)
		if !isNotFoundError(err) {
			t.Errorf("code %q: want NotFoundError, got %T (%v)", code, err, err)
		}
	}
	// numeric / status 404 (GCS-style err.code = 404, or response.statusCode)
	for _, err := range []error{
		&S3Error{ErrorCode: "", Status: 404, Message: "nf"},
		&GCSStatusError{ErrorCode: 404},
	} {
		w := wrapError(err, "msg", map[string]any{}, classRead)
		if !isNotFoundError(w) {
			t.Errorf("%T: want NotFoundError, got %T", err, w)
		}
	}
	// a persistor NotFoundError passing through stays a NotFoundError
	nf := NewNotFoundError("upstream not found", nil)
	w := wrapError(nf, "msg", map[string]any{}, classRead)
	if !isNotFoundError(w) {
		t.Errorf("persistor NotFoundError must map to NotFoundError, got %T", w)
	}
}

func TestWrapErrorAlreadyWrittenVariant(t *testing.T) {
	// PreconditionFailed with ifNoneMatch '*' → AlreadyWrittenError
	err := wrapError(
		&S3Error{ErrorCode: "PreconditionFailed", Message: "pc"},
		"upload to S3 failed",
		map[string]any{"ifNoneMatch": "*", "bucketName": "b", "key": "k"},
		classWrite)
	if !isAlreadyWrittenError(err) {
		t.Fatalf("want AlreadyWrittenError, got %T (%v)", err, err)
	}
	// an upstream AlreadyWrittenError counts too
	aw := NewAlreadyWrittenError("written", nil)
	w := wrapError(aw, "upload", map[string]any{"ifNoneMatch": "*"}, classWrite)
	if !isAlreadyWrittenError(w) {
		t.Fatalf("want AlreadyWrittenError, got %T", w)
	}
	// WITHOUT ifNoneMatch '*' a plain WriteError
	if w2 := wrapError(&S3Error{ErrorCode: "PreconditionFailed"}, "upload", map[string]any{}, classWrite); isAlreadyWrittenError(w2) {
		t.Fatalf("unexpected AlreadyWrittenError without ifNoneMatch: %T", w2)
	}
}

func TestWrapErrorDefaultClass(t *testing.T) {
	err := wrapError(errors.New("guru meditation error"), "upload to S3 failed",
		map[string]any{"bucketName": "womBucket", "key": "monKey"}, classWrite)
	w, ok := err.(*WriteError)
	if !ok {
		t.Fatalf("want *WriteError, got %T", err)
	}
	if w.OError == nil || w.OError.Message != "upload to S3 failed" {
		t.Fatalf("message: %v", w)
	}
	// params + cause preserved
	full := oerror.GetFullInfo(error(err))
	if full["bucketName"] != "womBucket" {
		t.Fatalf("params lost: %v", full)
	}
	if full["cause"] == nil {
		t.Fatal("cause missing")
	}
}

// --- base64/hex (Node: base64ToHex / hexToBase64) ----------------------------

func TestHexBase64RoundTrip(t *testing.T) {
	hexStr := "d41d8cd98f00b204e9800998ecf8427e" // md5 of ''
	b64 := hexToBase64(hexStr)
	if b64 != "1B2M2Y8AsgTpgAmY7PhCfg==" {
		t.Fatalf("hexToBase64: %q", b64)
	}
	if back := base64ToHex(b64); back != hexStr {
		t.Fatalf("base64ToHex: %q", back)
	}
}

// --- Observer (Node: ObserverStream) ----------------------------------------

func TestObserverCountsBytesAndEmitsMetrics(t *testing.T) {
	ml := &metricsRecorder{}
	oldM, oldL := Metrics, Logger
	Metrics, Logger = ml, NullLogger{}
	defer func() { Metrics, Logger = oldM, oldL }()

	o := NewObserver("s3.egress", "womBucket", "")
	if _, err := o.Write([]byte("hello world")); err != nil {
		t.Fatal(err)
	}
	if o.Bytes() != 11 {
		t.Fatalf("bytes = %d", o.Bytes())
	}
	o.Finish(nil)

	// Metrics.count(metric, bytes, 1, {size, bucket, status})
	var count *countRec
	for i := range ml.counts {
		c := &ml.counts[i]
		if c.metric == "s3.egress" {
			count = c
		}
	}
	if count == nil {
		t.Fatalf("no count for s3.egress: %+v", ml.counts)
	}
	if count.value != 11 {
		t.Fatalf("count value = %d, want 11", count.value)
	}
	if count.labels["size"] != "lt-128KiB" || count.labels["bucket"] != "womBucket" || count.labels["status"] != "success" {
		t.Fatalf("labels = %v", count.labels)
	}
	// Metrics.inc(metric + '.hit', 1, labels)
	foundHit := false
	for _, m := range ml.incs {
		if m == "s3.egress.hit" {
			foundHit = true
		}
	}
	if !foundHit {
		t.Fatalf("no s3.egress.hit inc: %+v", ml.incs)
	}
	// histogram .size / .latency
	foundSizeHist := false
	for _, h := range ml.hist {
		if h == "s3.egress.size" {
			foundSizeHist = true
		}
	}
	if !foundSizeHist {
		t.Fatalf("no s3.egress.size histogram: %+v", ml.hist)
	}
}

func TestObserverErrorStatusEmitsNoHistograms(t *testing.T) {
	ml := &metricsRecorder{}
	oldM := Metrics
	Metrics = ml
	defer func() { Metrics = oldM }()

	o := NewObserver("s3.ingress", "b", "")
	o.Finish(errors.New("boom"))

	for _, h := range ml.hist {
		t.Fatalf("no histograms expected on error, got %q", h)
	}
	statusOK := false
	for _, c := range ml.counts {
		if c.metric == "s3.ingress" && c.labels["status"] == "error" {
			statusOK = true
		}
	}
	if !statusOK {
		t.Fatalf("count label status should be 'error': %+v", ml.counts)
	}
}

type countRec struct {
	metric string
	value  int64
	labels map[string]string
}

type metricsRecorder struct {
	counts []countRec
	incs   []string
	hist   []string
}

func (m *metricsRecorder) Count(metric string, value int64, one int64, labels map[string]string) {
	m.counts = append(m.counts, countRec{metric, value, labels})
}
func (m *metricsRecorder) Inc(metric string, one int64, labels map[string]string) {
	m.incs = append(m.incs, metric)
}
func (m *metricsRecorder) Histogram(metric string, value int64, buckets []int64, labels map[string]string) {
	m.hist = append(m.hist, metric)
}

// --- SSECOptions (Node: getPutOptions/getGetOptions/getCopyOptions) ----------

func TestSSECOptionsOptionMaps(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	s := NewSSECOptions(key)

	put := s.Put()
	if put["SSECustomerAlgorithm"] != "AES256" {
		t.Fatalf("put algorithm: %v", put)
	}
	if put["SSECustomerKey"] == nil || put["SSECustomerKeyMD5"] == "" {
		t.Fatalf("put keys missing: %v", put)
	}
	get := s.Get()
	getKey, ok1 := get["SSECustomerKey"].([]byte)
	putKey, ok2 := put["SSECustomerKey"].([]byte)
	if !ok1 || !ok2 || bytes.Equal(getKey, putKey) == false {
		t.Fatal("get key should match put key")
	}
	copyOpts := s.Copy()
	if copyOpts["CopySourceSSECustomerAlgorithm"] != "AES256" {
		t.Fatalf("copy algorithm: %v", copyOpts)
	}
	// MD5 correctness (Node: base64 md5 of the raw key)
	sum := md5.Sum(key)
	want := base64.StdEncoding.EncodeToString(sum[:])
	if s.GetMD5() != want {
		t.Fatalf("md5: %q, want %q", s.GetMD5(), want)
	}
}

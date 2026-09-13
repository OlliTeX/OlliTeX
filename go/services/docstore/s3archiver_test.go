package docstore

// s3archiver_test.go — unit test for the S3/SeaweedFS archive backend
// against a minimal in-memory gateway stub (the same contract the s3x
// client pins against its fake: PUT/GET/DELETE object, LIST prefix).

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type stubS3 struct {
	mu      sync.Mutex
	buckets map[string]map[string][]byte
}

func newStubS3() *stubS3 {
	return &stubS3{buckets: map[string]map[string][]byte{}}
}

func errXML(code string) string {
	return `<Error><Code>` + code + `</Code></Error>`
}

func (s *stubS3) serve(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := parts[0]
	b, ok := s.buckets[bucket]

	// bucket-scoped: PUT /bucket (create), GET /bucket?prefix= (list)
	if len(parts) == 1 || (len(parts) > 1 && r.URL.Query().Get("prefix") != "") {
		if !ok {
			w.WriteHeader(404)
			fmt.Fprint(w, errXML("NoSuchBucket"))
			return
		}
		switch r.Method {
		case http.MethodPut:
			w.WriteHeader(200)
		case http.MethodGet:
			prefix := r.URL.Query().Get("prefix")
			items := ""
			for k, v := range b {
				if strings.HasPrefix(k, prefix) {
					items += `<Contents><Key>` + k + `</Key><Size>` +
						fmt.Sprintf("%d", len(v)) + `</Size></Contents>`
				}
			}
			fmt.Fprint(w, `<ListBucketResult>`+items+`<IsTruncated>false</IsTruncated></ListBucketResult>`)
		default:
			w.WriteHeader(405)
		}
		return
	}
	if len(parts) < 2 {
		w.WriteHeader(404)
		return
	}
	key := strings.Join(parts[1:], "/")
	switch r.Method {
	case http.MethodPut:
		if !ok {
			w.WriteHeader(403)
			fmt.Fprint(w, errXML("NoSuchBucket"))
			return
		}
		body, _ := io.ReadAll(r.Body)
		b[key] = body
		w.Header().Set("ETag", `"`+md5hex(body)+`"`)
		w.WriteHeader(200)
	case http.MethodGet, http.MethodHead:
		if !ok {
			w.WriteHeader(404)
			fmt.Fprint(w, errXML("NoSuchBucket"))
			return
		}
		data, ok := b[key]
		if !ok {
			w.WriteHeader(404)
			fmt.Fprint(w, errXML("NoSuchKey"))
			return
		}
		w.Header().Set("ETag", `"`+md5hex(data)+`"`)
		if r.Method == http.MethodGet {
			w.Write(data)
		}
	case http.MethodDelete:
		if !ok {
			w.WriteHeader(404)
			fmt.Fprint(w, errXML("NoSuchBucket"))
			return
		}
		delete(b, key)
		w.WriteHeader(204)
	}
}

func TestS3ArchiverRoundtrip(t *testing.T) {
	stub := newStubS3()
	stub.buckets["archive"] = map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(stub.serve))
	defer srv.Close()
	ctx := context.Background()

	a := NewS3Archiver(srv.URL, "", "")
	ini, ok := a.(BucketInitializer)
	if !ok {
		t.Fatal("NewS3Archiver must implement BucketInitializer")
	} else if err := ini.EnsureBucket(ctx, "archive"); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}

	// slash keys must be stored VERBATIM (Node S3Persistor semantics)
	data := []byte(`{"docId":"d1"}`)
	if err := a.Send(ctx, "archive", "p1/d1", data, md5hex(data)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	saved, ok := stub.buckets["archive"]["p1/d1"]
	if !ok || string(saved) != string(data) {
		t.Fatalf("key not stored verbatim (have: %v)", keys(stub.buckets["archive"]))
	}

	got, md5, err := a.Get(ctx, "archive", "p1/d1")
	if err != nil || string(got) != string(data) || md5 != md5hex(data) {
		t.Fatalf("Get = %q md5 %q err %v", got, md5, err)
	}

	// missing object → error (the route maps it to the generic 500 like the
	// fs backend's ENOENT)
	if _, _, err := a.Get(ctx, "archive", "p1/missing"); err == nil {
		t.Fatal("Get missing object must error")
	}

	// DeleteDirectory sweeps the project prefix
	if err := a.DeleteDirectory(ctx, "archive", "p1"); err != nil {
		t.Fatalf("DeleteDirectory: %v", err)
	}
	if n := len(stub.buckets["archive"]); n != 0 {
		t.Fatalf("after DeleteDirectory: %d objects left: %v", n, keys(stub.buckets["archive"]))
	}
}

func TestS3ArchiverMissingBucket(t *testing.T) {
	stub := newStubS3()
	srv := httptest.NewServer(http.HandlerFunc(stub.serve))
	defer srv.Close()
	a := NewS3Archiver(srv.URL, "", "")
	if _, _, err := a.Get(context.Background(), "nope", "p/d"); err == nil {
		t.Fatal("Get from a missing bucket must error")
	}
	// DeleteDirectory on a missing bucket is tolerated as a no-op (the
	// archive-all sweep must not hard-fail on "nothing archived"), matching
	// the fs backend's empty-directory result.
	if err := a.DeleteDirectory(context.Background(), "nope", "p"); err != nil {
		t.Fatalf("DeleteDirectory from a missing bucket must be a no-op, got %v", err)
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

package s3x

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeS3 is a minimal in-memory S3-gateway stand-in (path-style bucket/key)
// implementing just enough for the client under test: PUT/GET/HEAD/DELETE
// object, CREATE/HEAD bucket, LIST with prefix + marker pagination.
type fakeS3 struct {
	mu      sync.Mutex
	buckets map[string]map[string][]byte // bucket -> key -> bytes
}

func newFakeS3() *fakeS3 {
	return &fakeS3{buckets: map[string]map[string][]byte{}}
}

func (f *fakeS3) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		w.WriteHeader(200)
		fmt.Fprint(w, "<ListAllMyBucketsResult/>")
		return
	}
	bucket := parts[0]
	objects := f.buckets[bucket]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodPut: // create bucket
			if _, exists := f.buckets[bucket]; !exists {
				f.buckets[bucket] = map[string][]byte{}
			}
			w.WriteHeader(200)
		case http.MethodHead:
			if objects == nil {
				w.WriteHeader(404)
				return
			}
			w.WriteHeader(200)
		case http.MethodGet: // list
			prefix := r.URL.Query().Get("prefix")
			marker := r.URL.Query().Get("marker")
			pageSize, _ := strconv.Atoi(r.URL.Query().Get("_pageSize"))
			var keys []string
			for k := range objects {
				if objects != nil && strings.HasPrefix(k, prefix) && k > marker {
					keys = append(keys, k)
				}
			}
			sortStrings(keys)
			var xmlb bytes.Buffer
			xmlb.WriteString(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
			fmt.Fprintf(&xmlb, "<Name>%s</Name>", bucket)
			if pageSize > 0 && len(keys) > pageSize {
				page := keys[:pageSize]
				fmt.Fprintf(&xmlb, "<IsTruncated>true</IsTruncated><NextMarker>%s</NextMarker>", page[len(page)-1])
				for _, k := range page {
					writeListObj(&xmlb, k, objects[k])
				}
			} else {
				fmt.Fprint(&xmlb, "<IsTruncated>false</IsTruncated>")
				for _, k := range keys {
					writeListObj(&xmlb, k, objects[k])
				}
			}
			xmlb.WriteString("</ListBucketResult>")
			w.Header().Set("Content-Type", "application/xml")
			w.Write(xmlb.Bytes())
		default:
			w.WriteHeader(405)
		}
		return
	}
	key := strings.Join(parts[1:], "/")
	if objects == nil {
		w.WriteHeader(404)
		fmt.Fprint(w, `<Error><Code>NoSuchBucket</Code></Error>`)
		return
	}
	data, ok := objects[key]
	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Content-MD5") != "" {
			sum := md5.Sum(body)
			want, derr := base64.StdEncoding.DecodeString(r.Header.Get("Content-MD5"))
			if derr != nil || hex.EncodeToString(sum[:]) != hex.EncodeToString(want) {
				w.WriteHeader(400)
				fmt.Fprint(w, `<Error><Code>BadDigest</Code></Error>`)
				return
			}
		}
		objects[key] = body
		sum := md5.Sum(body)
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
		w.WriteHeader(200)
	case http.MethodGet, http.MethodHead:
		if !ok {
			w.WriteHeader(404)
			fmt.Fprint(w, `<Error><Code>NoSuchKey</Code></Error>`)
			return
		}
		sum := md5.Sum(data)
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			return
		}
		w.Write(data)
	case http.MethodDelete:
		delete(objects, key)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func writeListObj(b *bytes.Buffer, key string, data []byte) {
	sum := md5.Sum(data)
	fmt.Fprintf(b, "<Contents><Key>%s</Key><Size>%d</Size><ETag>%q</ETag>"+
		"<LastModified>%s</LastModified></Contents>",
		key, len(data), hex.EncodeToString(sum[:]), time.Now().UTC().Format(time.RFC3339))
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

func newClient(t *testing.T) *Client {
	t.Helper()
	fake := newFakeS3()
	ts := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(ts.Close)
	return New(ts.URL, "", "")
}

func hexMD5(b []byte) string {
	s := md5.Sum(b)
	return hex.EncodeToString(s[:])
}

func TestPutGetRoundTrip(t *testing.T) {
	c := newClient(t)
	if err := c.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	payload := []byte("hello seaweed")
	want := hexMD5(payload)
	etag, err := c.PutObject("b", "p1/p2.txt", bytes.NewReader(payload), "txt", "")
	if err != nil {
		t.Fatal(err)
	}
	if etag != want {
		t.Fatalf("etag = %q, want %q", etag, want)
	}
	gr, err := c.GetObject("b", "p1/p2.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Body.Close()
	got, _ := io.ReadAll(gr.Body)
	if string(got) != "hello seaweed" {
		t.Fatalf("body = %q", got)
	}
	if gr.ETag != want {
		t.Fatalf("get etag = %q", gr.ETag)
	}
}

func TestPutWithBadMD5Rejected(t *testing.T) {
	c := newClient(t)
	_ = c.CreateBucket("b")
	// "deadbeef" is not 16 bytes hex-decoded → the client must refuse before
	// sending (bad md5 guard) — and the fake gateway would also reject a
	// mismatched digest.
	_, err := c.PutObject("b", "k", strings.NewReader("abc"), "", "deadbeef00112233")
	if err == nil {
		t.Fatal("expected rejection for a non-16-byte md5, got nil")
	}
	// well-formed but wrong digest → gateway BadDigest
	_, err = c.PutObject("b", "k", strings.NewReader("abc"), "", hexMD5([]byte("different")))
	if err == nil {
		t.Fatal("expected BadDigest for a wrong but valid md5, got nil")
	}
}

func TestNotFounds(t *testing.T) {
	c := newClient(t)
	if _, err := c.GetObject("missing-bucket", "k"); !isNoSuch(err) {
		t.Fatalf("want no-such-bucket err, got %v", err)
	}
	_ = c.CreateBucket("b")
	if _, err := c.GetObject("b", "nope"); !isNoSuch(err) {
		t.Fatalf("want no-such-key err, got %v", err)
	}
	if _, _, err := c.HeadObject("b", "nope"); !isNoSuch(err) {
		t.Fatalf("head: want no-such-key, got %v", err)
	}
}

func TestDeleteMissingIsNoop(t *testing.T) {
	c := newClient(t)
	_ = c.CreateBucket("b")
	if err := c.DeleteObject("b", "not-there"); err != nil {
		t.Fatalf("DELETE of missing key must be a no-op, got %v", err)
	}
}

func TestListPrefix(t *testing.T) {
	c := newClient(t)
	_ = c.CreateBucket("b")
	for _, k := range []string{"pid1/a", "pid1/b", "pid2/a"} {
		_, _ = c.PutObject("b", k, strings.NewReader("x"), "", "")
	}
	objs, err := c.ListObjects("b", "pid1/")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 || objs[0].Key != "pid1/a" || objs[1].Key != "pid1/b" {
		t.Fatalf("list = %+v", objs)
	}
}

func TestListPagination(t *testing.T) {
	// The fake paginates at 2 objects per page (page marker semantics), so
	// a 5-key prefix exercises the client's IsTruncated/NextMarker loop.
	globalFake.mu.Lock()
	globalFake.buckets["b"] = map[string][]byte{}
	for _, k := range []string{"p/00", "p/01", "p/02", "p/03", "p/04"} {
		globalFake.buckets["b"][k] = []byte("x")
	}
	globalFake.mu.Unlock()
	ts := httptest.NewServer(http.HandlerFunc(paginatedServe))
	defer ts.Close()
	c := New(ts.URL, "", "")
	objs, err := c.ListObjects("b", "p/")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 5 || objs[0].Key != "p/00" || objs[4].Key != "p/04" {
		t.Fatalf("list = %+v", objs)
	}
	for i, k := range []string{"p/00", "p/01", "p/02", "p/03", "p/04"} {
		if objs[i].Key != k {
			t.Fatalf("order[%d] = %q", i, objs[i].Key)
		}
	}
}

func paginatedServe(w http.ResponseWriter, r *http.Request) {
	fake := globalFake
	fake.mu.Lock()
	defer fake.mu.Unlock()
	objects := fake.buckets["b"]
	prefix := r.URL.Query().Get("prefix")
	marker := r.URL.Query().Get("marker")
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("_pageSize"))
	if pageSize <= 0 {
		pageSize = 2
	}
	var keys []string
	for k := range objects {
		if strings.HasPrefix(k, prefix) && k > marker {
			keys = append(keys, k)
		}
	}
	sortStrings(keys)
	var xmlb bytes.Buffer
	xmlb.WriteString(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	if len(keys) > pageSize {
		page := keys[:pageSize]
		xmlb.WriteString("<IsTruncated>true</IsTruncated>")
		fmt.Fprintf(&xmlb, "<NextMarker>%s</NextMarker>", page[len(page)-1])
		for _, k := range page {
			writeListObj(&xmlb, k, objects[k])
		}
	} else {
		xmlb.WriteString("<IsTruncated>false</IsTruncated>")
		for _, k := range keys {
			writeListObj(&xmlb, k, objects[k])
		}
	}
	xmlb.WriteString("</ListBucketResult>")
	w.Header().Set("Content-Type", "application/xml")
	w.Write(xmlb.Bytes())
}

var globalFake = newFakeS3()

func TestCreateBucketIdempotent(t *testing.T) {
	c := newClient(t)
	if err := c.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateBucket("b"); err != nil {
		t.Fatalf("second create must succeed (409/200), got %v", err)
	}
	ok, err := c.BucketExists("b")
	if err != nil || !ok {
		t.Fatalf("exists = %v, %v", ok, err)
	}
}

func isNoSuch(err error) bool {
	return errors.Is(err, ErrNoSuchBucket) || errors.Is(err, ErrNoSuchKey) ||
		strings.Contains(err.Error(), "no such bucket") || strings.Contains(err.Error(), "no such key")
}

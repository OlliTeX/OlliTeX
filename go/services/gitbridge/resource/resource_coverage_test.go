// Coverage tests for package resource, complementing resource_test.go (the
// mirrored UrlResourceCacheTest cases).
//
// Targets: DefaultByteGetter against a real httptest server (transport,
// status, header-check, body read) and the remaining Get/fetch branch
// combinations that the mirror tests did not touch.
package resource

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"ollitex/go/services/gitbridge/filestore"
	"ollitex/go/services/gitbridge/giterrors"
)

func TestDefaultByteGetter200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "hello")
	}))
	defer srv.Close()
	body, err := DefaultByteGetter(&http.Client{})(srv.URL, nil)
	if err != nil {
		t.Fatalf("defaultByteGetter 200: %v", err)
	}
	if len(body) != 5 || string(body) != "hello" {
		t.Fatalf("body = %q, want hello", body)
	}
}

func TestDefaultByteGetter302FollowsByDefault(t *testing.T) {
	// default http.Client follows redirects: a 302 must still return 200.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "end")
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()
	body, err := DefaultByteGetter(&http.Client{})(srv.URL, nil)
	if err != nil {
		t.Fatalf("defaultByteGetter redirect: %v", err)
	}
	if string(body) != "end" {
		t.Fatalf("body = %q, want end", body)
	}
}

func TestDefaultByteGetter404IsFailedConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "nope")
	}))
	defer srv.Close()
	_, err := DefaultByteGetter(&http.Client{})(srv.URL, nil)
	if err == nil {
		t.Fatalf("404 must fail, got nil")
	}
	if _, ok := err.(*giterrors.FailedConnectionException); !ok {
		t.Fatalf("expected *FailedConnectionException, got %v", err)
	}
}

func TestDefaultByteGetterHeaderCheckErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	checkErr := &giterrors.SizeLimitExceededException{Path: "p", MaxSize: 1, ActualSize: 5}
	_, err := DefaultByteGetter(&http.Client{})(srv.URL, func(string) error { return checkErr })
	if err == nil {
		t.Fatalf("header error must propagate, got nil")
	}
	if got, ok := err.(*giterrors.SizeLimitExceededException); !ok || got != checkErr {
		t.Fatalf("expected propagated *SizeLimitExceededException pointer, got %v", err)
	}
}

func TestDefaultByteGetterTransportErrorIsFailedConnection(t *testing.T) {
	// Close the server, then the client.Get against its port fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "x")
	}))
	url := srv.URL
	srv.Close()
	_, err := DefaultByteGetter(&http.Client{})(url, nil)
	if err == nil {
		t.Fatalf("transport error expected, got nil")
	}
	if _, ok := err.(*giterrors.FailedConnectionException); !ok {
		t.Fatalf("expected *FailedConnectionException, got %v", err)
	}
}

// NewUrlResourceCache (uses DefaultByteGetter) fetches a 2xx URL end-to-end.
func TestNewUrlResourceCacheRealFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2")
		io.WriteString(w, "ok")
	}))
	defer srv.Close()
	db := &fakeDB{pathForURL: map[string]string{}}
	cache := NewUrlResourceCache(db)
	fetchedUrls := map[string][]byte{}
	f, err := cache.Get("p", srv.URL, "o.bin", map[string]filestore.RawFile{}, fetchedUrls, nil)
	if err != nil {
		t.Fatalf("real fetch: %v", err)
	}
	if f.GetPath() != "o.bin" {
		t.Fatalf("Get path = %q, want o.bin", f.GetPath())
	}
	// The fetch is remembered in fetchedUrls under the raw url with the full
	// body (both observables Java asserts).
	if got, _ := fetchedUrls[srv.URL]; len(got) != 2 || string(got) != "ok" {
		t.Fatalf("fetchedUrls contents = %d bytes (\"%s\"), want 2 bytes \"ok\"", len(got), got)
	}
}

func TestNewUrlResourceCacheFetchErrorIsFailedConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	url := srv.URL
	// keep the server alive for the request then closed for reuse
	db := &fakeDB{pathForURL: map[string]string{}}
	cache := NewUrlResourceCache(db)
	if _, err := cache.Get("p", url, "o.bin",
		map[string]filestore.RawFile{}, map[string][]byte{}, nil); err == nil {
		t.Fatalf("500 fetch must error")
	}
}

func TestGetKnownURLInFetchedUrlsShortCircuits(t *testing.T) {
	db := &fakeDB{pathForURL: map[string]string{}}
	cache := &UrlResourceCache{
		dbStore: db,
		byteGet: func(url string, _ func(string) error) ([]byte, error) {
			return nil, errors.New("byteGet must not be called")
		},
	}
	// url known with cache key = url (no token), stored in fetchedUrls.
	fu := map[string][]byte{fileURL: []byte("cached-bytes")}
	db.pathForURL[proj+":"+fileURL] = "stored.bin"
	if f, err := cache.Get(proj, fileURL, "n.bin", map[string]filestore.RawFile{}, fu, nil); err != nil {
		t.Fatalf("err: %v", err)
	} else {
		if string(f.GetContents()) != "cached-bytes" {
			t.Fatalf("short-circuit miss: %q", f.GetContents())
		}
	}
}

func TestGetKnownPathMissingInBothFilesAndFetchedFetches(t *testing.T) {
	// known path (in DB), NOT in fetchedUrls, NOT in fileTable -> fetch
	// branch with `path` (not `newPath`) as the destination.
	db := &fakeDB{pathForURL: map[string]string{proj + ":" + fileURL: "stored.bin"}}
	http := newMockHTTP("3", 3)
	cache := NewUrlResourceCacheWithGetter(db, http.byteGet)
	ft, fu := emptyTables()
	if _, err := cache.Get(proj, fileURL, "new.bin", ft, fu, nil); err != nil {
		t.Fatalf("fetch-on-known: %v", err)
	}
	// the URL-index write must use path "stored.bin" (the KNOWN path, not
	// the new path) — Java: addURLIndexForProject(PROJ, key, path).
	if len(db.addedURLIndex) != 1 || db.addedURLIndex[0] != [3]string{proj, fileURL, "stored.bin"} {
		t.Fatalf("URL-index write = %v", db.addedURLIndex)
	}
}

func TestFetchByteGetGenericErrorIsFailedConnection(t *testing.T) {
	db := &fakeDB{}
	cache := &UrlResourceCache{
		dbStore: db,
		byteGet: func(url string, _ func(string) error) ([]byte, error) {
			return nil, errors.New("not a SizeLimit")
		},
	}
	_, err := cache.Get(proj, fileURL, "n.bin", map[string]filestore.RawFile{}, map[string][]byte{}, nil)
	if err == nil {
		t.Fatalf("expected err, got nil")
	}
	if _, ok := err.(*giterrors.FailedConnectionException); !ok {
		t.Fatalf("expected *FailedConnectionException, got %v", err)
	}
}

func TestFetchPostCheckSizeLimitKnownPath(t *testing.T) {
	db := &fakeDB{pathForURL: map[string]string{proj + ":" + fileURL: "stored.bin"}}
	http := newMockHTTP("0", 4) // no CL header, body 4 bytes
	cache := NewUrlResourceCacheWithGetter(db, http.byteGet)
	if _, err := cache.Get(proj, fileURL, "n.bin", map[string]filestore.RawFile{}, map[string][]byte{}, ptr64(3)); err == nil {
		t.Fatalf("expected SizeLimit error, got nil")
	}
}

func TestFetchHeaderCheckNilAndNoLimit(t *testing.T) {
	// headerCheck != nil + maxFileSize == nil (headerCheck is always
	// non-nil inside fetch; when limit is nil the first `return nil` runs).
	// This is the `maxFileSize == nil` branch inside headerCheck.
	cache, _ := newCache(t, "2", 2)
	if _, err := cache.Get(proj, fileURL+"?token=abc", "n.bin",
		map[string]filestore.RawFile{}, map[string][]byte{}, nil); err != nil {
		t.Fatalf("no-limit headerCheck: %v", err)
	}
}

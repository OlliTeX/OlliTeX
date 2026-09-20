package resource

import (
	"testing"

	"ollitex/go/services/gitbridge/filestore"
	"ollitex/go/services/gitbridge/giterrors"
)

const (
	proj    = "proj"
	fileURL = "http://localhost/file.jpg"
	newPath = "file1.jpg"
)

// fakeDB is a minimal DBStore (records URL-index writes, returns path on
// demand). Satisfied by *db.SqliteDBStore too, so the production store is a
// drop-in.
type fakeDB struct {
	pathForURL    map[string]string
	addedURLIndex [][3]string
	lookupKeys    []string // (projectName, urlCacheKey) pairs
}

func (d *fakeDB) GetPathForURLInProject(projectName, url string) (string, bool) {
	d.lookupKeys = append(d.lookupKeys, projectName+":"+url)
	if p, ok := d.pathForURL[projectName+":"+url]; ok {
		return p, true
	}
	return "", false
}

func (d *fakeDB) AddURLIndexForProject(projectName, url, path string) {
	d.addedURLIndex = append(d.addedURLIndex, [3]string{projectName, url, path})
}

// mockHTTP ports the mocked NingHttpClientFacade:
//
//	respondWithContentLength(cl)   -> body of cl bytes
//	respondWithContentLength(cl, n) -> body of n bytes (the "misdeclared" length)
type mockHTTP struct {
	contentLength string
	bodyLen       int
	// when contentLength is set to "" (no header), the fetch sees no CL header
	clPresent bool
}

func (m *mockHTTP) byteGet(url string, headerCheck func(string) error) ([]byte, error) {
	if m.clPresent {
		if headerCheck != nil {
			if err := headerCheck(m.contentLength); err != nil {
				return nil, err
			}
		}
	}
	out := make([]byte, m.bodyLen)
	return out, nil
}

func newMockHTTP(cl string, bodyLen int) *mockHTTP {
	return &mockHTTP{contentLength: cl, bodyLen: bodyLen, clPresent: cl != ""}
}

func emptyTables() (map[string]filestore.RawFile, map[string][]byte) {
	return map[string]filestore.RawFile{}, map[string][]byte{}
}

func newCache(t *testing.T, cl string, bodyLen int) (*UrlResourceCache, *fakeDB) {
	db := &fakeDB{pathForURL: map[string]string{}}
	http := newMockHTTP(cl, bodyLen)
	cache := NewUrlResourceCacheWithGetter(db, http.byteGet)
	return cache, db
}

func testSizeLimit(t *testing.T, got error) {
	t.Helper()
	_, ok := got.(*giterrors.SizeLimitExceededException)
	if !ok {
		t.Fatalf("expected *SizeLimitExceededException, got %v", got)
	}
}

// Java: getDoesNotThrowWhenContentLengthLT — CL 1 < max 2.
func TestGetDoesNotThrowWhenContentLengthLT(t *testing.T) {
	cache, _ := newCache(t, "1", 1)
	ft, fu := emptyTables()
	_, err := cache.Get(proj, fileURL, newPath, ft, fu, ptr64(2))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

// Java: getDoesNotThrowWhenContentLengthEQ — CL 2 = max 2.
func TestGetDoesNotThrowWhenContentLengthEQ(t *testing.T) {
	cache, _ := newCache(t, "2", 2)
	ft, fu := emptyTables()
	_, err := cache.Get(proj, fileURL, newPath, ft, fu, ptr64(2))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

// Java: getThrowsSizeLimitExceededWhenContentLengthGT — CL 3 > max 2.
func TestGetThrowsSizeLimitExceededWhenContentLengthGT(t *testing.T) {
	cache, _ := newCache(t, "3", 3)
	ft, fu := emptyTables()
	_, got := cache.Get(proj, fileURL, newPath, ft, fu, ptr64(2))
	if got == nil || got.Error() == "" {
		t.Fatalf("expected SizeLimitExceededException, got nil")
	}
	testSizeLimit(t, got)
}

// Java: getWithEmptyContentIsValid — CL 0 = max 0 (zero-byte content is valid).
func TestGetWithEmptyContentIsValid(t *testing.T) {
	cache, _ := newCache(t, "0", 0)
	ft, fu := emptyTables()
	_, err := cache.Get(proj, fileURL, newPath, ft, fu, ptr64(0))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

// Java: getWithoutLimitDoesNotThrow — Integer.MAX_VALUE CL with no limit.
func TestGetWithoutLimitDoesNotThrow(t *testing.T) {
	cache, _ := newCache(t, "2147483647", 0)
	ft, fu := emptyTables()
	_, err := cache.Get(proj, fileURL, newPath, ft, fu, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
}

// Java: getThrowsIfActualContentTooBig — CL 0 is under the limit, but the
// real body is 10 bytes > max 5.
func TestGetThrowsIfActualContentTooBig(t *testing.T) {
	cache, _ := newCache(t, "0", 10)
	ft, fu := emptyTables()
	_, got := cache.Get(proj, fileURL, newPath, ft, fu, ptr64(5))
	if got == nil || got.Error() == "" {
		t.Fatalf("expected SizeLimitExceededException, got nil")
	}
	testSizeLimit(t, got)
}

// Java: tokenIsRemovedFromCacheKey — token in the URL is stripped on the
// lookup and the URL-index write use the cache key (with the token REMOVED).
func TestTokenIsRemovedFromCacheKey(t *testing.T) {
	url := "http://history.overleaf.com/projects/1234/blobs/abdef?token=secretencryptedstuff&_path=test.tex"
	expectedCacheKey := "http://history.overleaf.com/projects/1234/blobs/abdef?token=REMOVED&_path=test.tex"
	db := &fakeDB{pathForURL: map[string]string{}}
	http := &mockHTTP{contentLength: "123", bodyLen: 123, clPresent: true}
	cache := NewUrlResourceCacheWithGetter(db, http.byteGet)
	ft, fu := emptyTables()
	if _, err := cache.Get(proj, url, newPath, ft, fu, nil); err != nil {
		t.Fatalf("err: %v", err)
	}
	// Java: verify(dbStore).getPathForURLInProject(PROJ, cacheKey) — the lookup
	// is against the cache key, not the raw URL.
	if len(db.lookupKeys) != 1 || db.lookupKeys[0] != proj+":"+expectedCacheKey {
		t.Fatalf("DB lookup key = %v, want [%s]", db.lookupKeys, proj+":"+expectedCacheKey)
	}
	// Java: verify(dbStore).addURLIndexForProject(PROJ, cacheKey, NEW_PATH).
	if len(db.addedURLIndex) != 1 || db.addedURLIndex[0] != [3]string{proj, expectedCacheKey, newPath} {
		t.Fatalf("URL-index write = %+v, want one [%s] %s %s", db.addedURLIndex, proj, expectedCacheKey, newPath)
	}
	// A second Get with the cache-key path known AND the file present in the
	// current commit (fileTable hit) must not fetch and must not re-add a
	// URL-index row (already indexed) — mirrors Java's `rawFile` path.
	before := len(db.addedURLIndex)
	db.pathForURL[proj+":"+expectedCacheKey] = newPath
	ft2, fu2 := emptyTables()
	ft2[newPath] = filestore.NewRepositoryFile(newPath, []byte{'h', 'i'})
	if f, err := cache.Get(proj, url, newPath, ft2, fu2, nil); err != nil {
		t.Fatalf("err: %v", err)
	} else {
		if len(f.GetContents()) != 2 {
			t.Fatal("hit path should have reused the fileTable contents")
		}
	}
	if len(db.addedURLIndex) != before {
		t.Fatalf("hit path should not add another URL-index row, got %d", len(db.addedURLIndex))
	}
}

// getCacheKeyFromUrl direct port assertions (the Java test's "tokenIsRemoved"
// uses only this through the cache; add a direct assertion to be explicit).
func TestGetCacheKeyFromUrlStripsToken(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://h/api/x?token=abc&_path=t.tex", "http://h/api/x?token=REMOVED&_path=t.tex"},
		{"http://h/api/x?token=123&other=y", "http://h/api/x?token=REMOVED&other=y"},
		{"http://h/api/x?noToken", "http://h/api/x?noToken"},
		{"http://h/api/x", "http://h/api/x"},
	}
	for _, c := range cases {
		if got := getCacheKeyFromUrl(c.in); got != c.want {
			t.Errorf("getCacheKeyFromUrl(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func ptr64(v int64) *int64 { return &v }

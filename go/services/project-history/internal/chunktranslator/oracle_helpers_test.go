// Oracle test helpers for the chunktranslator port (B10).
package chunktranslator

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"
)

// jsNulls — vendor `_.map(authors, id => id == null ? null : id)`: JSON-
// decoded `null` author entries must be Go nil (not ""/0). Applied to raw
// fixtures so the ported decode path mirrors vendor.
func jsNullsInChanges(raw map[string]any) map[string]any {
	out := cloneAnyMap(raw)
	if cs, ok := out["chunk"].(map[string]any); ok {
		csClone := cloneAnyMap(cs)
		hist, _ := csClone["history"].(map[string]any)
		if hist != nil {
			for _, rc := range hist["changes"].([]any) {
				cm, _ := rc.(map[string]any)
				if cm == nil {
					continue
				}
				if arr, ok := cm["authors"].([]any); ok {
					cm["authors"] = jsNullArray(arr)
				}
				if arr, ok := cm["v2Authors"].([]any); ok {
					cm["v2Authors"] = jsNullArray(arr)
				}
			}
		}
		out["chunk"] = csClone
	}
	return out
}

func jsNullArray(a []any) []any {
	out := make([]any, len(a))
	for i, v := range a {
		out[i] = v
	}
	return out
}

func cloneAnyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// expectJSONEqual — deep-compare via canonical JSON (deterministic key order;
// numeric types unified by Go marshalling float64/int — same idiom as B9).
func expectJSONEqual(t *testing.T, got, want any) {
	t.Helper()
	gj, wj := mustMarshal(t, got), mustMarshal(t, want)
	if gj != wj {
		t.Errorf("mismatch:\n got: %s\nwant: %s", gj, wj)
	}
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// blobStore — fake HistoryStoreManager (getProjectBlob) + WebApiManager
// (getHistoryID) pair, seeded per the vendor fixtures (historyId=12345).
type blobStore struct {
	blobs    map[string]string // "histID:hash" -> raw text
	blobErrs map[string]error
}

func newBlobStore() *blobStore {
	return &blobStore{blobs: map[string]string{}, blobErrs: map[string]error{}}
}

func (s *blobStore) seed(hash, content string) {
	s.blobs["12345:"+hash] = content
}

func (s *blobStore) setBlobErr(hash string, err error) {
	s.blobErrs["12345:"+hash] = err
}

func (s *blobStore) deps() Deps {
	return Deps{
		GetHistoryID:   func(projectID string) (int, error) { return 12345, nil },
		GetProjectBlob: s.getBlob,
	}
}

func (s *blobStore) getBlob(histID int, hash string) (string, error) {
	key := strconv.Itoa(histID) + ":" + hash
	if err, has := s.blobErrs[key]; has {
		return "", err
	}
	return s.blobs[key], nil
}

var errNotFound = errors.New("blob not found")

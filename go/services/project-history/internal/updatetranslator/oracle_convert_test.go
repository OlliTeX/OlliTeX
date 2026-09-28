// Oracle tests mirroring vendor
// test/unit/js/UpdateTranslator/UpdateTranslatorTests.js (27 cases).
//
// Vendor source: services/project-history/app/js/UpdateTranslator.js.
// Wire output is compared byte-for-byte (JSON marshal) after
// ConvertToChanges -> Change.ToRaw, mirroring vendor
// `changes.map(c => c.toRaw())`.
package updatetranslator

import (
	"encoding/json"
	"fmt"
	"testing"

	"ollitex/go/services/project-history/internal/historyot"
)

// Deterministic fixtures mirroring the vendor beforeEach.
const (
	fProjectID = "59bfd450e3028c4d40a1e9aa"
	fDocID     = "59bfd450e3028c4d40a1e9ab"
	fFileID    = "59bfd450e3028c4d40a1easd"
	fUser      = "59bb9051abf6e8682a269b64"
	fTSISO     = "2000-01-01T00:00:00.000Z"
	fHash      = "12345abc12345abc12345abc12345abc12345abc"
)

// fTSEpoch — `new Date('2000-01-01T00:00:00.000Z').getTime()`.
const fTSEpoch = float64(946684800000)

func textUpdate(op []any, meta map[string]any) map[string]any {
	return map[string]any{"doc": fDocID, "op": op, "v": float64(0), "meta": meta}
}

func baseMeta(ts any) map[string]any {
	return map[string]any{"user_id": fUser, "ts": ts, "pathname": "/main.tex"}
}

func convertUpdates(t *testing.T, updates []UpdateWithBlob) []*historyot.Change {
	t.Helper()
	changes, err := ConvertToChanges(fProjectID, updates)
	if err != nil {
		t.Fatalf("ConvertToChanges: %v", err)
	}
	return changes
}

func assertChanges(t *testing.T, name string, got []*historyot.Change, want []map[string]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d changes, want %d", name, len(got), len(want))
	}
	for i := range got {
		assertChange(t, fmt.Sprintf("%s[%d]", name, i), got[i].ToRaw(), want[i])
	}
}

func assertChange(t *testing.T, name string, got, want map[string]any) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if fmt.Sprintf("%s", g) != fmt.Sprintf("%s", w) {
		t.Fatalf("%s mismatch:\n got: %s\nwant: %s", name, g, w)
	}
}

func uwb(update map[string]any, blob ...map[string]any) UpdateWithBlob {
	b := map[string]any{}
	if len(blob) > 0 {
		b = blob[0]
	}
	return UpdateWithBlob{Update: update, BlobHashes: b}
}

func wantTextOp(t *testing.T, ops ...any) map[string]any {
	return map[string]any{"pathname": "main.tex", "textOperation": toAnySlice(ops...)}
}

func toAnySlice(vals ...any) []any {
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = v
	}
	return out
}

func wantBase(tsKey, tsVal any) map[string]any {
	return map[string]any{"authors": []any{}, "v2Authors": []any{fUser}, "timestamp": tsVal}
}

// --- vendor 'convertToChanges' cases ---

func TestDocAddition(t *testing.T) {
	// 'can translate doc additions'
	changes := convertUpdates(t, []UpdateWithBlob{uwb(
		map[string]any{
			"doc":      fDocID,
			"pathname": "/main.tex",
			"docLines": "a\nb",
			"meta":     map[string]any{"user_id": fUser, "ts": fTSISO},
		},
		map[string]any{"file": fHash},
	)})
	assertChanges(t, "doc-addition", changes, []map[string]any{
		{
			"authors":    []any{},
			"operations": []any{map[string]any{"pathname": "main.tex", "file": map[string]any{"hash": fHash}}},
			"v2Authors":  []any{fUser},
			"timestamp":  fTSISO,
		},
	})
}

func TestFileAddition(t *testing.T) {
	// 'can translate file additions'
	changes := convertUpdates(t, []UpdateWithBlob{uwb(
		map[string]any{
			"file":     fFileID,
			"pathname": "/test.png",
			"url":      "filestore.example.com/test.png",
			"meta":     map[string]any{"user_id": fUser, "ts": fTSISO},
		},
		map[string]any{"file": fHash},
	)})
	assertChanges(t, "file-addition", changes, []map[string]any{
		{
			"authors":    []any{},
			"operations": []any{map[string]any{"pathname": "test.png", "file": map[string]any{"hash": fHash}}},
			"v2Authors":  []any{fUser},
			"timestamp":  fTSISO,
		},
	})
}

func TestDocRename(t *testing.T) {
	// 'can translate doc renames'
	changes := convertUpdates(t, []UpdateWithBlob{uwb(
		map[string]any{
			"doc":          fDocID,
			"pathname":     "/main.tex",
			"new_pathname": "/new_main.tex",
			"meta":         map[string]any{"user_id": fUser, "ts": fTSISO},
		},
	)})
	assertChanges(t, "doc-rename", changes, []map[string]any{
		{
			"authors":    []any{},
			"operations": []any{map[string]any{"pathname": "main.tex", "newPathname": "new_main.tex"}},
			"v2Authors":  []any{fUser},
			"timestamp":  fTSISO,
		},
	})
}

func TestFileRename(t *testing.T) {
	// 'can translate file renames'
	changes := convertUpdates(t, []UpdateWithBlob{uwb(
		map[string]any{
			"file":         fFileID,
			"pathname":     "/test.png",
			"new_pathname": "/new_test.png",
			"meta":         map[string]any{"user_id": fUser, "ts": fTSISO},
		},
	)})
	assertChanges(t, "file-rename", changes, []map[string]any{
		{
			"authors":    []any{},
			"operations": []any{map[string]any{"pathname": "test.png", "newPathname": "new_test.png"}},
			"v2Authors":  []any{fUser},
			"timestamp":  fTSISO,
		},
	})
}

func TestMultipleUpdates(t *testing.T) {
	// 'can translate multiple updates with the correct versions'
	changes := convertUpdates(t, []UpdateWithBlob{
		uwb(map[string]any{
			"doc":      fDocID,
			"pathname": "/main.tex",
			"docLines": "a\nb",
			"meta":     map[string]any{"user_id": fUser, "ts": fTSISO},
		}, map[string]any{"file": fHash}),
		uwb(map[string]any{
			"file":     fFileID,
			"pathname": "/test.png",
			"url":      "filestore.example.com/test.png",
			"meta":     map[string]any{"user_id": fUser, "ts": fTSISO},
		}, map[string]any{"file": fHash}),
	})
	assertChanges(t, "multiple-updates", changes, []map[string]any{
		{
			"authors":    []any{},
			"operations": []any{map[string]any{"pathname": "main.tex", "file": map[string]any{"hash": fHash}}},
			"v2Authors":  []any{fUser},
			"timestamp":  fTSISO,
		},
		{
			"authors":    []any{},
			"operations": []any{map[string]any{"pathname": "test.png", "file": map[string]any{"hash": fHash}}},
			"v2Authors":  []any{fUser},
			"timestamp":  fTSISO,
		},
	})
}

func TestUnknownFormat(t *testing.T) {
	// 'returns an error if the update has an unknown format'
	_, err := ConvertToChanges(fProjectID, []UpdateWithBlob{
		uwb(map[string]any{"foo": "bar"}),
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if got := err.Error(); got != "update with unknown format" {
		t.Fatalf("want 'update with unknown format', got %q", got)
	}
}

func TestBackslashPathname(t *testing.T) {
	// 'replaces backslashes with underscores in pathnames'
	changes := convertUpdates(t, []UpdateWithBlob{uwb(
		map[string]any{
			"doc":          fDocID,
			"pathname":     "/\\main\\foo.tex",
			"new_pathname": "/\\new_main\\foo\\bar.tex",
			"meta":         map[string]any{"user_id": fUser, "ts": fTSISO},
		},
	)})
	assertChanges(t, "backslash", changes, []map[string]any{
		{
			"authors":    []any{},
			"operations": []any{map[string]any{"pathname": "_main_foo.tex", "newPathname": "_new_main_foo_bar.tex"}},
			"v2Authors":  []any{fUser},
			"timestamp":  fTSISO,
		},
	})
}

func TestAsteriskPathname(t *testing.T) {
	// 'replaces leading asterisks with __ASTERISK__ in pathnames'
	changes := convertUpdates(t, []UpdateWithBlob{uwb(
		map[string]any{
			"file":     fFileID,
			"pathname": "/test*test.png",
			"url":      "filestore.example.com/test*test.png",
			"meta":     map[string]any{"user_id": fUser, "ts": fTSISO},
		},
		map[string]any{"file": fHash},
	)})
	assertChanges(t, "asterisk", changes, []map[string]any{
		{
			"authors":    []any{},
			"operations": []any{map[string]any{"pathname": "test__ASTERISK__test.png", "file": map[string]any{"hash": fHash}}},
			"v2Authors":  []any{fUser},
			"timestamp":  fTSISO,
		},
	})
}

func TestLeadingSpaceTopLevel(t *testing.T) {
	// 'replaces a leading space for top-level files with __SPACE__'
	changes := convertUpdates(t, []UpdateWithBlob{uwb(
		map[string]any{
			"file":     fFileID,
			"pathname": "/ test.png",
			"url":      "filestore.example.com/test.png",
			"meta":     map[string]any{"user_id": fUser, "ts": fTSISO},
		},
		map[string]any{"file": fHash},
	)})
	assertChanges(t, "leading-space", changes, []map[string]any{
		{
			"authors":    []any{},
			"operations": []any{map[string]any{"pathname": "__SPACE__test.png", "file": map[string]any{"hash": fHash}}},
			"v2Authors":  []any{fUser},
			"timestamp":  fTSISO,
		},
	})
}

func TestLeadingSpaceSubfolder(t *testing.T) {
	// 'replaces leading spaces of files in subfolders with __SPACE__'
	changes := convertUpdates(t, []UpdateWithBlob{uwb(
		map[string]any{
			"file":     fFileID,
			"pathname": "/folder/ test.png",
			"url":      "filestore.example.com/folder/test.png",
			"meta":     map[string]any{"user_id": fUser, "ts": fTSISO},
		},
		map[string]any{"file": fHash},
	)})
	assertChanges(t, "subfolder-space", changes, []map[string]any{
		{
			"authors":    []any{},
			"operations": []any{map[string]any{"pathname": "folder/__SPACE__test.png", "file": map[string]any{"hash": fHash}}},
			"v2Authors":  []any{fUser},
			"timestamp":  fTSISO,
		},
	})
}

func TestAnonymousAuthor(t *testing.T) {
	// 'sets a null author when user_id is "anonymous-user"'
	changes := convertUpdates(t, []UpdateWithBlob{uwb(
		map[string]any{
			"doc":      fDocID,
			"pathname": "/main.tex",
			"docLines": "a\nb",
			"meta":     map[string]any{"user_id": "anonymous-user", "ts": fTSISO},
		},
		map[string]any{"file": fHash},
	)})
	assertChanges(t, "anonymous", changes, []map[string]any{
		{
			"authors":    []any{},
			"operations": []any{map[string]any{"pathname": "main.tex", "file": map[string]any{"hash": fHash}}},
			"v2Authors":  []any{nil},
			"timestamp":  fTSISO,
		},
	})
}

func TestNoUserAuthor(t *testing.T) {
	// 'sets an empty array as author when there is no meta.user_id'
	changes := convertUpdates(t, []UpdateWithBlob{uwb(
		map[string]any{
			"doc":      fDocID,
			"pathname": "/main.tex",
			"docLines": "a\nb",
			"meta":     map[string]any{"ts": fTSISO},
		},
		map[string]any{"file": fHash},
	)})
	assertChanges(t, "no-user", changes, []map[string]any{
		{
			"authors":    []any{},
			"operations": []any{map[string]any{"pathname": "main.tex", "file": map[string]any{"hash": fHash}}},
			"v2Authors":  []any{},
			"timestamp":  fTSISO,
		},
	})
}

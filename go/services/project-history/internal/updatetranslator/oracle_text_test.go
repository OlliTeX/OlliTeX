// Oracle tests for text-update translation, mirroring vendor
// test/unit/js/UpdateTranslator/UpdateTranslatorTests.js:
//
//	'can translate insertions'
//	'can translate deletions'
//	'should translate retains without tracking data'
//	'can translate retains with tracking data'
//	'drops zero-length retains carrying tracking data'
//	'can translate insertions at the start and end (with zero retained)'
//	'can handle operations in non-linear offset order'
//	'handles comment ops'
//	'handles insertions after the end of the document'
//	'translates external source metadata into an origin'
//	'errors on unexpected ops'
//	'handles deletes over tracked deletes'
//	'handles tracked delete rejections specially'
//	'handles tracked changes'
//	'handles a delete over a mix of tracked inserts and tracked deletes'
//
// Expected wire values are the vendor test expectations (the node rig ran
// the vendored source end-to-end and reproduced every vendor
// expectation: PASS 27). Expected textOperation lists are POST
// Composition (the vendored `compressOperations` runs on the raw ops
// before `toRaw`).
package updatetranslator

import (
	"encoding/json"
	"testing"

	"ollitex/go/services/project-history/internal/errors"
)

// fTextMeta — vendor text-update `meta` (epoch ts:
// `new Date(fTSISO).getTime()`).
func textMeta(extra map[string]any) map[string]any {
	m := map[string]any{
		"user_id":    fUser,
		"ts":         fTSEpoch,
		"pathname":   "/main.tex",
		"doc_length": float64(20),
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func wantTracked(typ string) map[string]any {
	return map[string]any{"type": typ, "userId": fUser, "ts": fTSISO}
}

func assertTextChange(t *testing.T, name string, op []any, meta map[string]any, want map[string]any) {
	t.Helper()
	changes := convertUpdates(t, []UpdateWithBlob{
		{Update: map[string]any{"doc": fDocID, "op": op, "v": float64(0), "meta": meta}},
	})
	assertChange(t, name, changes[0].ToRaw(), want)
}

func v2DocVersionsWant() map[string]any {
	return map[string]any{fDocID: map[string]any{"pathname": "main.tex", "v": float64(0)}}
}

// --- text updates ---

func TestInsertions(t *testing.T) {
	// 'can translate insertions'
	assertTextChange(t, "insertions", []any{
		map[string]any{"p": float64(3), "i": "foo"},
		map[string]any{"p": float64(15), "i": "bar", "commentIds": []any{"comment1"}},
	}, map[string]any{
		"user_id":    fUser,
		"ts":         fTSEpoch,
		"pathname":   "/main.tex",
		"doc_length": float64(20),
		"source":     "some-editor-id",
	}, map[string]any{
		"authors": []any{},
		"operations": []any{
			map[string]any{"pathname": "main.tex", "textOperation": []any{
				float64(3), "foo", float64(9),
				map[string]any{"i": "bar", "commentIds": []any{"comment1"}},
				float64(8),
			}},
		},
		"v2Authors":     []any{fUser},
		"timestamp":     fTSISO,
		"v2DocVersions": v2DocVersionsWant(),
	})
}

func TestDeletions(t *testing.T) {
	// 'can translate deletions'
	assertTextChange(t, "deletions", []any{
		map[string]any{"p": float64(3), "d": "lo"},
		map[string]any{"p": float64(10), "d": "bar"},
	}, textMeta(nil), map[string]any{
		"authors": []any{},
		"operations": []any{
			map[string]any{"pathname": "main.tex", "textOperation": []any{
				float64(3), float64(-2), float64(7), float64(-3), float64(5),
			}},
		},
		"v2Authors":     []any{fUser},
		"timestamp":     fTSISO,
		"v2DocVersions": v2DocVersionsWant(),
	})
}

func TestRetainsNoTracking(t *testing.T) {
	// 'should translate retains without tracking data'
	assertTextChange(t, "retains-no-tracking", []any{
		map[string]any{"p": float64(3), "r": "lo"},
	}, textMeta(nil), map[string]any{
		"authors": []any{},
		"operations": []any{
			map[string]any{"pathname": "main.tex", "textOperation": []any{float64(20)}},
		},
		"v2Authors":     []any{fUser},
		"timestamp":     fTSISO,
		"v2DocVersions": v2DocVersionsWant(),
	})
}

func TestRetainsWithTracking(t *testing.T) {
	// 'can translate retains with tracking data'
	assertTextChange(t, "retains-with-tracking", []any{
		map[string]any{"p": float64(3), "r": "lo", "tracking": map[string]any{"type": "none"}},
	}, textMeta(nil), map[string]any{
		"authors": []any{},
		"operations": []any{
			map[string]any{"pathname": "main.tex", "textOperation": []any{
				float64(3),
				map[string]any{"r": float64(2), "tracking": map[string]any{"type": "none"}},
				float64(15),
			}},
		},
		"v2Authors":     []any{fUser},
		"timestamp":     fTSISO,
		"v2DocVersions": v2DocVersionsWant(),
	})
}

func TestDropZeroLengthTrackingRetain(t *testing.T) {
	// 'drops zero-length retains carrying tracking data'
	assertTextChange(t, "drop-zero-tracking", []any{
		map[string]any{"p": float64(3), "r": "",
			"tracking": map[string]any{"type": "delete", "userId": fUser, "ts": float64(0)}},
	}, textMeta(nil), map[string]any{
		"authors": []any{},
		"operations": []any{
			map[string]any{"pathname": "main.tex", "textOperation": []any{float64(20)}},
		},
		"v2Authors":     []any{fUser},
		"timestamp":     fTSISO,
		"v2DocVersions": v2DocVersionsWant(),
	})
}

func TestZeroLengthStartEnd(t *testing.T) {
	// 'can translate insertions at the start and end (with zero retained)'
	assertTextChange(t, "zero-start-end", []any{
		map[string]any{"p": float64(0), "i": "foo"},
		map[string]any{"p": float64(23), "i": "bar"},
		map[string]any{"p": float64(0), "d": "foo"},
		map[string]any{"p": float64(20), "d": "bar"},
	}, textMeta(nil), map[string]any{
		"authors": []any{},
		"operations": []any{
			map[string]any{"pathname": "main.tex", "textOperation": []any{float64(20)}},
		},
		"v2Authors":     []any{fUser},
		"timestamp":     fTSISO,
		"v2DocVersions": v2DocVersionsWant(),
	})
}

func TestNonLinearOffsets(t *testing.T) {
	// 'can handle operations in non-linear offset order'
	assertTextChange(t, "non-linear", []any{
		map[string]any{"p": float64(15), "i": "foo"},
		map[string]any{"p": float64(3), "i": "bar"},
	}, map[string]any{
		"user_id":    fUser,
		"ts":         fTSISO, // vendor: ts is the ISO string here.
		"pathname":   "/main.tex",
		"doc_length": float64(20),
	}, map[string]any{
		"authors": []any{},
		"operations": []any{
			map[string]any{"pathname": "main.tex", "textOperation": []any{
				float64(3), "bar", float64(12), "foo", float64(5),
			}},
		},
		"v2Authors":     []any{fUser},
		"timestamp":     fTSISO,
		"v2DocVersions": v2DocVersionsWant(),
	})
}

func TestCommentOps(t *testing.T) {
	// 'handles comment ops'
	assertTextChange(t, "comment-ops", []any{
		map[string]any{"p": float64(0), "i": "foo"},
		map[string]any{"p": float64(3), "d": "bar"},
		map[string]any{"p": float64(5), "c": "comment this", "t": "comment-id-1"},
		map[string]any{"p": float64(7), "c": "another comment", "t": "comment-id-2"},
		map[string]any{"p": float64(9), "c": "", "t": "comment-id-3"},
		map[string]any{"p": float64(10), "i": "baz"},
	}, textMeta(nil), map[string]any{
		"authors": []any{},
		"operations": []any{
			map[string]any{"pathname": "main.tex", "textOperation": []any{"foo", float64(-3), float64(17)}},
			map[string]any{"pathname": "main.tex", "commentId": "comment-id-1", "ranges": []any{
				map[string]any{"pos": float64(5), "length": float64(12)},
			}},
			map[string]any{"pathname": "main.tex", "commentId": "comment-id-2", "ranges": []any{
				map[string]any{"pos": float64(7), "length": float64(15)},
			}},
			map[string]any{"pathname": "main.tex", "commentId": "comment-id-3", "ranges": []any{}},
			map[string]any{"pathname": "main.tex", "textOperation": []any{float64(10), "baz", float64(10)}},
		},
		"v2Authors":     []any{fUser},
		"timestamp":     fTSISO,
		"v2DocVersions": v2DocVersionsWant(),
	})
}

func TestInsertionAfterEnd(t *testing.T) {
	// 'handles insertions after the end of the document'
	assertTextChange(t, "after-end", []any{
		map[string]any{"p": float64(3), "i": "\\"},
	}, map[string]any{
		"user_id":    fUser,
		"ts":         fTSEpoch,
		"pathname":   "/main.tex",
		"doc_length": float64(2),
	}, map[string]any{
		"authors": []any{},
		"operations": []any{
			map[string]any{"pathname": "main.tex", "textOperation": []any{float64(2), "\\"}},
		},
		"v2Authors":     []any{fUser},
		"timestamp":     fTSISO,
		"v2DocVersions": v2DocVersionsWant(),
	})
}

func TestExternalOrigin(t *testing.T) {
	// 'translates external source metadata into an origin'
	assertTextChange(t, "external-origin", []any{
		map[string]any{"p": float64(3), "i": "foo"},
	}, map[string]any{
		"user_id":    fUser,
		"ts":         fTSEpoch,
		"pathname":   "/main.tex",
		"doc_length": float64(20),
		"type":       "external",
		"source":     "dropbox",
	}, map[string]any{
		"authors":       []any{},
		"operations":    []any{map[string]any{"pathname": "main.tex", "textOperation": []any{float64(3), "foo", float64(17)}}},
		"v2Authors":     []any{fUser},
		"timestamp":     fTSISO,
		"v2DocVersions": v2DocVersionsWant(),
		"origin":        map[string]any{"kind": "dropbox"},
	})
}

func TestUnexpectedOp(t *testing.T) {
	// 'errors on unexpected ops'
	_, err := ConvertToChanges(fProjectID, []UpdateWithBlob{
		{Update: map[string]any{"doc": fDocID, "op": []any{
			map[string]any{"p": float64(5), "z": "bar"},
		}, "v": float64(0), "meta": textMeta(nil)}},
	})
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if e, ok := err.(*errors.Error); ok {
		if e.Kind != errors.KindUnexpectedOpType {
			t.Fatalf("want KindUnexpectedOpType, got %v", e.Kind)
		}
	} else {
		t.Fatalf("want *errors.Error, got %T", err)
	}
}

// --- text updates with history metadata ---

func TestDeletesOverTrackedDeletes(t *testing.T) {
	// 'handles deletes over tracked deletes'
	assertTextChange(t, "deletes-over-tracked", []any{
		map[string]any{"i": "foo", "p": float64(3), "hpos": float64(5)},
		map[string]any{
			"d": "quux", "p": float64(10), "hpos": float64(15),
			"trackedChanges": []any{
				map[string]any{"type": "delete", "offset": float64(2), "length": float64(3)},
				map[string]any{"type": "delete", "offset": float64(3), "length": float64(1)},
			},
		},
		map[string]any{"c": "noteworthy", "p": float64(8), "t": "comment-id", "hpos": float64(11), "hlen": float64(14)},
	}, map[string]any{
		"user_id":            fUser,
		"ts":                 fTSEpoch,
		"pathname":           "/main.tex",
		"doc_length":         float64(20),
		"history_doc_length": float64(30),
		"source":             "some-editor-id",
	}, map[string]any{
		"authors": []any{},
		"operations": []any{
			map[string]any{"pathname": "main.tex", "textOperation": []any{
				float64(5), "foo", float64(7),
				float64(-2), float64(3), float64(-1), float64(1), float64(-1), float64(10),
			}},
			map[string]any{"pathname": "main.tex", "commentId": "comment-id", "ranges": []any{
				map[string]any{"pos": float64(11), "length": float64(14)},
			}},
		},
		"v2Authors":     []any{fUser},
		"timestamp":     fTSISO,
		"v2DocVersions": v2DocVersionsWant(),
	})
}

func TestTrackedDeleteRejection(t *testing.T) {
	// 'handles tracked delete rejections specially'
	assertTextChange(t, "tracked-rejection", []any{
		map[string]any{"i": "foo", "p": float64(3), "trackedDeleteRejection": true},
	}, map[string]any{
		"user_id":    fUser,
		"ts":         fTSEpoch,
		"pathname":   "/main.tex",
		"doc_length": float64(20),
		"source":     "some-editor-id",
	}, map[string]any{
		"authors": []any{},
		"operations": []any{
			map[string]any{"pathname": "main.tex", "textOperation": []any{
				float64(3),
				map[string]any{"r": float64(3), "tracking": map[string]any{"type": "none"}},
				float64(14),
			}},
		},
		"v2Authors":     []any{fUser},
		"timestamp":     fTSISO,
		"v2DocVersions": v2DocVersionsWant(),
	})
}

func TestTrackedChanges(t *testing.T) {
	// 'handles tracked changes'
	assertTextChange(t, "tracked-changes", []any{
		map[string]any{"i": "inserted", "p": float64(5)},
		map[string]any{"d": "deleted", "p": float64(20)},
		map[string]any{"i": "rejected deletion", "p": float64(30), "trackedDeleteRejection": true},
		map[string]any{
			"d": "rejected insertion", "p": float64(50),
			"trackedChanges": []any{
				map[string]any{"type": "insert", "offset": float64(0), "length": float64(18)},
			},
		},
	}, map[string]any{
		"tc":         "tracked-change-id",
		"user_id":    fUser,
		"ts":         fTSEpoch,
		"pathname":   "/main.tex",
		"doc_length": float64(70),
		"source":     "some-editor-id",
	}, map[string]any{
		"authors": []any{},
		"operations": []any{
			map[string]any{"pathname": "main.tex", "textOperation": []any{
				float64(5),
				map[string]any{"i": "inserted", "tracking": wantTracked("insert")},
				float64(7),
				map[string]any{"r": float64(7), "tracking": wantTracked("delete")},
				float64(3),
				map[string]any{"r": float64(17), "tracking": map[string]any{"type": "none"}},
				float64(3), float64(-18), float64(10),
			}},
		},
		"v2Authors":     []any{fUser},
		"timestamp":     fTSISO,
		"v2DocVersions": v2DocVersionsWant(),
	})
}

func TestDeleteOverMixedTracked(t *testing.T) {
	// 'handles a delete over a mix of tracked inserts and tracked deletes'
	assertTextChange(t, "delete-over-mixed", []any{
		map[string]any{
			"d": "abcdef", "p": float64(10),
			"trackedChanges": []any{
				map[string]any{"type": "insert", "offset": float64(0), "length": float64(3)},
				map[string]any{"type": "delete", "offset": float64(2), "length": float64(10)},
				map[string]any{"type": "insert", "offset": float64(2), "length": float64(2)},
			},
		},
	}, map[string]any{
		"tc":                 "tracking-id",
		"user_id":            fUser,
		"ts":                 fTSEpoch,
		"pathname":           "/main.tex",
		"doc_length":         float64(20),
		"history_doc_length": float64(30),
		"source":             "some-editor-id",
	}, map[string]any{
		"authors": []any{},
		"operations": []any{
			map[string]any{"pathname": "main.tex", "textOperation": []any{
				float64(10), float64(-3), float64(10), float64(-2),
				map[string]any{"r": float64(1), "tracking": wantTracked("delete")},
				float64(4),
			}},
		},
		"v2Authors":     []any{fUser},
		"timestamp":     fTSISO,
		"v2DocVersions": v2DocVersionsWant(),
	})
}

var _ = json.Marshal

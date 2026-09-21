package otc

// schemas_test.go — mirrors the Node `schemas.test.js` oracle (the 4 top-level
// schemas' `safeParse` accept/reject cases) 1:1, plus behaviour-pins for the
// remaining exported schemas (those have no Node oracle) so the local Val
// shorthands (intVal / literalVal / recordVal / nullableVal / regexVal /
// uuidVal / kindVal / isoDatetimeVal) are covered.

import (
	"strings"
	"testing"

	vt "ollitex/go/libraries/validtools"
)

// str40hex returns a valid 40-hex-char blob hash (satisfies rawBlobHash).
func str40hex() string { return strings.Repeat("a", 40) }

func schemaOK(schema vt.Val, data any) bool {
	_, iss := schema.Validate(true, data)
	return len(iss) == 0
}

// ---- oracle: rawTextOperation ----

func TestSchema_RawTextOperation(t *testing.T) {
	if !schemaOK(rawTextOperation, map[string]any{"textOperation": []any{}}) {
		t.Fatal("rawTextOperation should accept a no-op (empty textOperation)")
	}
	if schemaOK(rawTextOperation, map[string]any{"textOperation": []any{map[string]any{"i": "a", "bogus": 1}}}) {
		t.Fatal("rawTextOperation should reject an unrecognised op key (strict)")
	}
}

// ---- oracle: rawRetainOp ----

func TestSchema_RawRetainOp(t *testing.T) {
	// accepts a bare retain length
	if !schemaOK(rawRetainOp, 5) {
		t.Fatal("rawRetainOp should accept a bare length (5)")
	}
	// accepts a retain with tracked-insert props
	if !schemaOK(rawRetainOp, map[string]any{
		"r": 5,
		"tracking": map[string]any{
			"type":   "insert",
			"userId": "507f1f77bcf86cd799439011",
			"ts":     "2024-01-01T00:00:00.000Z",
		},
	}) {
		t.Fatal("rawRetainOp should accept tracked-insert props")
	}
	// accepts a retain with clear-tracking props
	if !schemaOK(rawRetainOp, map[string]any{"r": 5, "tracking": map[string]any{"type": "none"}}) {
		t.Fatal("rawRetainOp should accept clear-tracking props (type none)")
	}
	// rejects a retain with an unrecognized tracking type
	if schemaOK(rawRetainOp, map[string]any{"r": 5, "tracking": map[string]any{"type": "not-a-tracking-type"}}) {
		t.Fatal("rawRetainOp should reject an unrecognized tracking type")
	}
	// rejects a retain with a malformed tracking userId
	if schemaOK(rawRetainOp, map[string]any{
		"r": 5,
		"tracking": map[string]any{
			"type":   "insert",
			"userId": "not-an-object-id",
			"ts":     "2024-01-01T00:00:00.000Z",
		},
	}) {
		t.Fatal("rawRetainOp should reject a malformed tracking userId")
	}
}

// ---- oracle: rawLinkedFileData ----

func TestSchema_RawLinkedFileData(t *testing.T) {
	t.Run("reject_empty", func(t *testing.T) {
		if schemaOK(rawLinkedFileData, map[string]any{}) {
			t.Fatal("should reject an empty object")
		}
	})
	t.Run("reject_unknown_provider", func(t *testing.T) {
		if schemaOK(rawLinkedFileData, map[string]any{"provider": "not-a-provider"}) {
			t.Fatal("should reject an unrecognized provider")
		}
	})
	t.Run("reject_project_file_extra_key", func(t *testing.T) {
		if schemaOK(rawLinkedFileData, map[string]any{
			"provider": "project_file", "source_entity_path": "/main.tex", "extraUnrecognizedKey": "abcd",
		}) {
			t.Fatal("should reject an unrecognized extra key (strict)")
		}
	})
	t.Run("accept_project_file_valid_id", func(t *testing.T) {
		if !schemaOK(rawLinkedFileData, map[string]any{
			"provider": "project_file", "source_project_id": "507f1f77bcf86cd799439011", "source_entity_path": "/main.tex",
		}) {
			t.Fatal("should accept a project_file with a valid source_project_id")
		}
	})
	t.Run("accept_project_file_missing_id", func(t *testing.T) {
		if !schemaOK(rawLinkedFileData, map[string]any{"provider": "project_file", "source_entity_path": "/main.tex"}) {
			t.Fatal("should accept a project_file with a missing source_project_id")
		}
	})
	t.Run("accept_project_file_v1_id", func(t *testing.T) {
		if !schemaOK(rawLinkedFileData, map[string]any{
			"provider": "project_file", "v1_source_doc_id": 1234, "source_entity_path": "/main.tex",
		}) {
			t.Fatal("should accept a project_file with a v1 source doc id")
		}
	})
	t.Run("accept_project_file_legacy", func(t *testing.T) {
		if !schemaOK(rawLinkedFileData, map[string]any{
			"provider": "project_file", "v1_source_doc_id": 1234, "source_entity_path": "/main.tex",
			"source_project_display_name": "My linked project", "importedAt": "2017-05-04T00:00:00.000Z",
		}) {
			t.Fatal("should accept a project_file with the legacy display name")
		}
	})
	t.Run("reject_project_file_bad_id", func(t *testing.T) {
		if schemaOK(rawLinkedFileData, map[string]any{
			"provider": "project_file", "source_project_id": "not-an-object-id", "source_entity_path": "/main.tex",
		}) {
			t.Fatal("should reject a malformed source_project_id")
		}
	})
	t.Run("accept_output_file_valid_id", func(t *testing.T) {
		if !schemaOK(rawLinkedFileData, map[string]any{
			"provider": "project_output_file", "source_project_id": "507f1f77bcf86cd799439011", "source_output_file_path": "output.pdf",
		}) {
			t.Fatal("should accept a project_output_file with a valid source_project_id")
		}
	})
	t.Run("reject_output_file_bad_id", func(t *testing.T) {
		if schemaOK(rawLinkedFileData, map[string]any{
			"provider": "project_output_file", "source_project_id": "not-an-object-id", "source_output_file_path": "output.pdf",
		}) {
			t.Fatal("should reject a malformed source_project_id (output)")
		}
	})
	t.Run("accept_output_file_v1_id", func(t *testing.T) {
		if !schemaOK(rawLinkedFileData, map[string]any{
			"provider": "project_output_file", "v1_source_doc_id": 1234, "source_output_file_path": "output.pdf",
		}) {
			t.Fatal("should accept a project_output_file with a v1 source doc id")
		}
	})
	t.Run("accept_output_file_build_id", func(t *testing.T) {
		if !schemaOK(rawLinkedFileData, map[string]any{
			"provider": "project_output_file", "source_output_file_path": "output.pdf", "build_id": "1234-abcd",
		}) {
			t.Fatal("should accept a project_output_file with a valid build_id")
		}
	})
	t.Run("accept_output_file_verbose", func(t *testing.T) {
		if !schemaOK(rawLinkedFileData, map[string]any{
			"provider": "project_output_file", "source_project_id": "507f1f77bcf86cd799439011",
			"source_output_file_path": "output.pdf", "compileGroup": "standard",
			"clsiServerId": "clsi-pre-emp-e2-f-tqnd", "build_id": "1234-abcd", "importedAt": "2026-08-14T00:00:00.000Z",
		}) {
			t.Fatal("should accept a project_output_file with verbose details")
		}
	})
	t.Run("reject_output_file_bad_build_id", func(t *testing.T) {
		if schemaOK(rawLinkedFileData, map[string]any{
			"provider": "project_output_file", "source_output_file_path": "output.pdf", "build_id": "not-a-valid-build-id",
		}) {
			t.Fatal("should reject a malformed build_id")
		}
	})
	// mendeley / zotero / papers — group_id semantics.
	for _, provider := range []string{"mendeley", "zotero", "papers"} {
		provider := provider
		t.Run("accept_"+provider+"_group_id", func(t *testing.T) {
			if !schemaOK(rawLinkedFileData, map[string]any{"provider": provider, "group_id": "abcd"}) {
				t.Fatalf("should accept a %s provider with an opaque group_id", provider)
			}
		})
		t.Run("accept_"+provider+"_missing_group_id", func(t *testing.T) {
			if !schemaOK(rawLinkedFileData, map[string]any{"provider": provider}) {
				t.Fatalf("should accept a %s provider with a missing group_id", provider)
			}
		})
		t.Run("accept_"+provider+"_null_group_id", func(t *testing.T) {
			if !schemaOK(rawLinkedFileData, map[string]any{"provider": provider, "group_id": nil}) {
				t.Fatalf("should accept a %s provider with a null group_id", provider)
			}
		})
		t.Run("reject_"+provider+"_path_group_id", func(t *testing.T) {
			if schemaOK(rawLinkedFileData, map[string]any{"provider": provider, "group_id": "abcd/../../etc/passwd"}) {
				t.Fatalf("should reject a %s group_id containing a path separator", provider)
			}
		})
		t.Run("reject_"+provider+"_dotdot_group_id", func(t *testing.T) {
			if schemaOK(rawLinkedFileData, map[string]any{"provider": provider, "group_id": ".."}) {
				t.Fatalf("should reject a %s group_id of ..", provider)
			}
		})
	}
}

// ---- oracle: rawFileMetadata ----

func TestSchema_RawFileMetadata(t *testing.T) {
	cases := []struct {
		name   string
		data   any
		expect bool
	}{
		{"legacy_v1_main_flag", map[string]any{"main": true}, true},
		{"legacy_v1_url_import", map[string]any{"agent": "url", "agentDataId": 42}, true},
		{"legacy_v1_wlfile_import", map[string]any{"agent": "wlfile", "agentDataId": 1337}, true},
		{"main_flag_importedAt", map[string]any{"main": true, "importedAt": "2024-01-01T00:00:00.000Z"}, true},
		{"linked_project_file", map[string]any{"importedAt": "2024-01-01T00:00:00.000Z", "provider": "project_file", "v1_source_doc_id": 1234, "source_entity_path": "/main.tex"}, true},
		{"importedAt_only", map[string]any{"importedAt": "2024-01-01T00:00:00.000Z"}, true},
		{"reject_bad_importedAt", map[string]any{"importedAt": "not-a-date"}, false},
		{"main_bibliography", map[string]any{"mainBibliography": true}, true},
		{"both_doc_flags", map[string]any{"main": true, "mainBibliography": true}, true},
		{"reject_nonbool_flag", map[string]any{"mainBibliography": "yes"}, false},
		{"empty_object", map[string]any{}, true},
	}
	for _, c := range cases {
		if got := schemaOK(rawFileMetadata, c.data); got != c.expect {
			t.Errorf("%s: got %v, want %v", c.name, got, c.expect)
		}
	}
}

// ---- behaviour-pins: the remaining exported schemas (no Node oracle) ----

func TestSchema_RawRangeAndFileData(t *testing.T) {
	if !schemaOK(rawRange, map[string]any{"pos": 0, "length": 10}) {
		t.Fatal("rawRange should accept a valid range")
	}
	if schemaOK(rawRange, map[string]any{"pos": -1, "length": 10}) {
		t.Fatal("rawRange should reject a negative pos")
	}
	if !schemaOK(rawHashFileData, map[string]any{"hash": str40hex()}) {
		t.Fatal("rawHashFileData should accept a 40-hex hash")
	}
	if schemaOK(rawHashFileData, map[string]any{"hash": "nope"}) {
		t.Fatal("rawHashFileData should reject a malformed hash (regex)")
	}
	if !schemaOK(rawBinaryFileData, map[string]any{"hash": str40hex(), "byteLength": 0}) {
		t.Fatal("rawBinaryFileData should accept a hash + byteLength")
	}
	if !schemaOK(rawHollowStringFileData, map[string]any{"stringLength": 5}) {
		t.Fatal("rawHollowStringFileData should accept a stringLength")
	}
	// rawLazyStringFileData with an ops array (exercises ArrayVal + rawEditOperation).
	// Note: rawEditOperation = textOperation | addComment | deleteComment |
	// setCommentState | noOp — NOT a bare scan op, so the op is a textOperation.
	if !schemaOK(rawLazyStringFileData, map[string]any{
		"hash": str40hex(), "stringLength": 5,
		"operations": []any{map[string]any{"textOperation": []any{map[string]any{"r": 2}}}},
	}) {
		t.Fatal("rawLazyStringFileData should accept a hash + stringLength + ops")
	}
	if schemaOK(rawLazyStringFileData, map[string]any{
		"hash": str40hex(), "stringLength": 5,
		"operations": []any{map[string]any{"r": 2}}, // not a valid editOperation
	}) {
		t.Fatal("rawLazyStringFileData should reject a bare scan op as an editOperation")
	}
}

func TestSchema_RawFileAndMap(t *testing.T) {
	// rawFile: a hash file with metadata (the metadata-union arm).
	if !schemaOK(rawFile, map[string]any{"hash": str40hex(), "metadata": map[string]any{"main": true}}) {
		t.Fatal("rawFile should accept a hash file + doc-flag metadata")
	}
	if schemaOK(rawFile, map[string]any{"hash": str40hex(), "metadata": map[string]any{"bogusKey": 1}}) {
		t.Fatal("rawFile should reject unknown metadata keys (strict via the union)")
	}
	// rawFileMap: a record of path -> rawFile (exercises recordVal).
	if !schemaOK(rawFileMap, map[string]any{
		"main.tex": map[string]any{"hash": str40hex()},
	}) {
		t.Fatal("rawFileMap should accept a map of path->file")
	}
	if schemaOK(rawFileMap, map[string]any{"main.tex": map[string]any{"bogus": 1}}) {
		t.Fatal("rawFileMap should reject a value that is not a file")
	}
	// rawV2DocVersions: a record of name -> {pathname, v} (recordVal + intVal).
	if !schemaOK(rawV2DocVersions, map[string]any{
		"a:1": map[string]any{"pathname": "a", "v": 1},
	}) {
		t.Fatal("rawV2DocVersions should accept {pathname, v}")
	}
	if schemaOK(rawV2DocVersions, map[string]any{"a:1": map[string]any{"pathname": "a", "v": "one"}}) {
		t.Fatal("rawV2DocVersions should reject a non-int v")
	}
}

func TestSchema_RawSnapshotAndOrigin(t *testing.T) {
	if !schemaOK(rawSnapshotSchema, map[string]any{
		"files":     map[string]any{},
		"timestamp": "2024-01-01T00:00:00.000Z",
	}) {
		t.Fatal("rawSnapshot should accept {files, timestamp}")
	}
	if schemaOK(rawSnapshotSchema, map[string]any{"files": "nope"}) {
		t.Fatal("rawSnapshot should reject a non-record files")
	}

	// rawOrigin: the named kinds discriminate; the base refine rejects a named kind.
	okRestore := map[string]any{"kind": "restore", "version": 3, "timestamp": "2024-01-01T00:00:00.000Z", "historyClientId": "123e4567-e89b-12d3-a456-426614174000"}
	if !schemaOK(rawOrigin, okRestore) {
		t.Fatal("rawOrigin should accept a restore origin")
	}
	if !schemaOK(rawOrigin, map[string]any{"kind": "file-restore", "version": 3, "path": "p", "timestamp": "2024-01-01T00:00:00.000Z"}) {
		t.Fatal("rawOrigin should accept a file-restore origin")
	}
	if !schemaOK(rawOrigin, map[string]any{"kind": "project-restore", "version": 3, "timestamp": "2024-01-01T00:00:00.000Z"}) {
		t.Fatal("rawOrigin should accept a project-restore origin")
	}
	if !schemaOK(rawOrigin, map[string]any{"kind": "some-generic-kind"}) {
		t.Fatal("rawOrigin should accept a generic (non-named) kind")
	}
	// base kind refine: a named kind on the catch-all must be flagged by the refine
	// (so the union only accepts it via its own shaped arm, which still passes).
	if !schemaOK(rawBaseOrigin, map[string]any{"kind": "something-else"}) {
		t.Fatal("rawBaseOrigin should accept a non-named kind")
	}
}

func TestSchema_RawOperationAndChange(t *testing.T) {
	// rawOperation: add-file arm.
	if !schemaOK(rawOperation, map[string]any{
		"pathname": "main.tex",
		"file":     map[string]any{"hash": str40hex()},
	}) {
		t.Fatal("rawOperation should accept an add-file operation")
	}
	// move arm
	if !schemaOK(rawOperation, map[string]any{"pathname": "a", "newPathname": "b"}) {
		t.Fatal("rawOperation should accept a move operation")
	}
	// no-op arm
	if !schemaOK(rawOperation, map[string]any{}) {
		t.Fatal("rawOperation should accept a no-op ({})")
	}

	// rawChange: operations + timestamp + nullable author/v2Author arms.
	changeOK := map[string]any{
		"operations": []any{
			map[string]any{"pathname": "main.tex", "file": map[string]any{"hash": str40hex()}},
		},
		"timestamp": "2024-01-01T00:00:00.000Z",
		"authors":   []any{1, nil},
		"v2Authors": []any{"507f1f77bcf86cd799439011", nil},
	}
	if !schemaOK(rawChange, changeOK) {
		t.Fatal("rawChange should accept a valid change (incl. nullable authors)")
	}
	if schemaOK(rawChange, map[string]any{"operations": "nope", "timestamp": "2024-01-01T00:00:00.000Z"}) {
		t.Fatal("rawChange should reject a non-array operations")
	}
	if schemaOK(rawChange, map[string]any{
		"operations": []any{}, "timestamp": "2024-01-01T00:00:00.000Z", "v2Authors": []any{"not-an-object-id"},
	}) {
		t.Fatal("rawChange should reject a malformed v2 author")
	}
}

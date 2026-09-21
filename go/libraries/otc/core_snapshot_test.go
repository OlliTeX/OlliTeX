package otc

import (
	"testing"
	"time"
)

func fileForTest(t *testing.T, content string, metadata map[string]any) *File {
	t.Helper()
	f, err := FileFromString(content, metadata)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSnapshotFindBlobHashes(t *testing.T) {
	s := NewSnapshot(nil, nil, nil, nil)
	hashes := map[string]bool{}
	s.FindBlobHashes(hashes)
	if len(hashes) != 0 {
		t.Fatalf("expected 0 hashes, got %d", len(hashes))
	}
	if err := s.AddFile("foo", fileForTest(t, "", nil)); err != nil {
		t.Fatal(err)
	}
	s.FindBlobHashes(hashes)
	if len(hashes) != 0 {
		t.Fatalf("expected 0 hashes, got %d", len(hashes))
	}
	f, err := FileFromHash(FileEmptyHash, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddFile("bar", f); err != nil {
		t.Fatal(err)
	}
	s.FindBlobHashes(hashes)
	if !hashes[FileEmptyHash] || len(hashes) != 1 {
		t.Fatalf("expected empty hash found, got %v", hashes)
	}
}

func TestSnapshotEditFile(t *testing.T) {
	s := NewSnapshot(nil, nil, nil, nil)
	if err := s.AddFile("hello.txt", fileForTest(t, "hello", nil)); err != nil {
		t.Fatal(err)
	}
	op := NewTextOperation()
	if err := op.Retain(5, RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := op.Insert(" world!", InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	editOp := NewTextEdit(op)

	// applies
	if err := s.EditFile("hello.txt", editOp); err != nil {
		t.Fatal(err)
	}
	content := *s.GetFile("hello.txt").GetContent(false)
	if content != "hello world!" {
		t.Fatalf("content = %q", content)
	}

	// missing file -> EditMissingFileError
	if err := s.EditFile("does-not-exist.txt", NewTextEdit(NewTextOperation())); err == nil {
		t.Fatal("expected EditMissingFileError")
	} else if !isErrType[*EditMissingFileError](err) {
		t.Fatalf("got %T", err)
	}
}

func TestSnapshotGetPathnameWithDocFlag(t *testing.T) {
	cases := []struct {
		name string
		adds []struct {
			path     string
			content  bool // use fromHash instead of string
			metadata map[string]any
		}
		key  string
		want *string
	}{
		{"finds the doc carrying the flag",
			[]struct {
				path     string
				content  bool
				metadata map[string]any
			}{
				{path: "a.tex", content: true /*string*/, metadata: map[string]any{"main": true}},
			}, "main", strPtr("a.tex")},
		{"nothing when no flag",
			[]struct {
				path     string
				content  bool
				metadata map[string]any
			}{
				{path: "a.tex", content: true, metadata: nil},
			}, "main", nil},
		{"not confused by a different flag",
			[]struct {
				path     string
				content  bool
				metadata map[string]any
			}{
				{path: "a.tex", content: true, metadata: map[string]any{"mainBibliography": true}},
			}, "main", nil},
		{"picks the flagged file out of several",
			[]struct {
				path     string
				content  bool
				metadata map[string]any
			}{
				{path: "a.tex", content: true, metadata: nil},
				{path: "b.tex", content: true, metadata: map[string]any{"main": true}},
				{path: "c.tex", content: true, metadata: nil},
			}, "main", strPtr("b.tex")},
		{"ignores flag on non-doc (importedAt present)",
			[]struct {
				path     string
				content  bool
				metadata map[string]any
			}{
				{path: "a.bin", content: false /*hash*/, metadata: map[string]any{
					"main":       true,
					"importedAt": "2026-01-01T00:00:00.000Z",
				}},
			}, "main", nil},
		{"finds flag on a not-yet-loaded doc (hash only)",
			[]struct {
				path     string
				content  bool
				metadata map[string]any
			}{
				{path: "a.tex", content: false, metadata: map[string]any{"main": true}},
			}, "main", strPtr("a.tex")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewSnapshot(nil, nil, nil, nil)
			for _, a := range c.adds {
				var f *File
				var err error
				if a.content {
					f, err = FileFromString("", a.metadata)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					f, err = FileFromHash("a5675307b61ec2517330622a6e649b4ca1ee5612", nil, a.metadata)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := s.AddFile(a.path, f); err != nil {
					t.Fatal(err)
				}
			}
			got := s.GetPathnameWithDocFlag(c.key)
			if (got == nil) != (c.want == nil) {
				t.Fatalf("want %v got %v", c.want, got)
			}
			if c.want != nil && *got != *c.want {
				t.Fatalf("want %q got %q", *c.want, *got)
			}
		})
	}
}

func TestSnapshotApplyAll(t *testing.T) {
	badOp := NewTextOperation()
	if err := badOp.Insert("FAIL!", InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	goodOp := NewTextOperation()
	if err := goodOp.Insert("SUCCESS!", InsertBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	change := NewChange(
		[]Operation{
			OperationEditFile("missing.txt", NewTextEdit(badOp)),
			OperationEditFile("empty.txt", NewTextEdit(goodOp)),
		},
		nowTime(),
		nil, nil, nil, nil, nil,
	)

	// ignores recoverable errors
	s1 := NewSnapshot(nil, nil, nil, nil)
	if err := s1.AddFile("empty.txt", fileForTest(t, "", nil)); err != nil {
		t.Fatal(err)
	}
	if err := s1.ApplyAll([]*Change{change}, false); err != nil {
		t.Fatalf("non-strict should not error: %v", err)
	}
	if got := *s1.GetFile("empty.txt").GetContent(false); got != "SUCCESS!" {
		t.Fatalf("content = %q", got)
	}

	// strict: stops on recoverable error
	s2 := NewSnapshot(nil, nil, nil, nil)
	if err := s2.AddFile("empty.txt", fileForTest(t, "", nil)); err != nil {
		t.Fatal(err)
	}
	err := s2.ApplyAll([]*Change{change}, true)
	if err == nil || !isErrType[*EditMissingFileError](err) {
		t.Fatalf("expected strict EditMissingFileError, got %v", err)
	}
	if got := *s2.GetFile("empty.txt").GetContent(false); got != "" {
		t.Fatalf("content should be empty, got %q", got)
	}
}

func strPtr(s string) *string { return &s }

func nowTime() time.Time { return time.Now().UTC() }

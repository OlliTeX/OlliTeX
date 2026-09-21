package otc

import (
	"testing"
)

func makeTestFileMap(t *testing.T, pathnames []string) *FileMap {
	t.Helper()
	files := make(map[string]*File, len(pathnames))
	for _, p := range pathnames {
		f, err := FileFromString(p, nil)
		if err != nil {
			t.Fatal(err)
		}
		files[p] = f
	}
	fm, err := NewFileMap(files)
	if err != nil {
		t.Fatalf("NewFileMap(%v): %v", pathnames, err)
	}
	return fm
}

func TestFileMapSingleAndCaseFolders(t *testing.T) {
	makeTestFileMap(t, []string{"a"})
	makeTestFileMap(t, []string{"a/b", "A/c"})
	makeTestFileMap(t, []string{"a/b/c", "A/b/d"})
	makeTestFileMap(t, []string{"a/b/c", "a/B/d"})
}

func TestFileMapConflictOnConstruct(t *testing.T) {
	files := map[string]*File{}
	for _, p := range []string{"a", "a/b"} {
		f, _ := FileFromString(p, nil)
		files[p] = f
	}
	if _, err := NewFileMap(files); err == nil {
		t.Fatal("expected PathnameConflictError")
	} else if !isErrType[*PathnameConflictError](err) {
		t.Fatalf("expected *PathnameConflictError, got %T (%v)", err, err)
	}
}

func TestFileMapWouldConflict(t *testing.T) {
	// case that sorts before '/'
	fm := makeTestFileMap(t, []string{"a", "a!"})
	if !fm.WouldConflict("a/b") {
		t.Fatal("wouldConflict('a/b') should be true")
	}

	fm2 := makeTestFileMap(t, []string{"a/b/c"})
	expect := map[string]bool{
		"a/b/c/d": true,
		"a":       true,
		"b":       false,
		"a/b":     true,
		"a/c":     false,
		"a/b/c":   false,
		"a/b/d":   false,
		"d/b/c":   false,
		"a/b/C":   false,
		"A":       false,
		"A/b":     false,
		"a/B":     false,
		"A/B":     false,
	}
	for p, want := range expect {
		if got := fm2.WouldConflict(p); got != want {
			t.Fatalf("wouldConflict(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestFileMapAddConflict(t *testing.T) {
	fm := makeTestFileMap(t, []string{"a/b"})
	f, _ := FileFromString("a/b/c", nil)
	if err := fm.AddFile("a/b/c", f); err == nil {
		t.Fatal("expected PathnameConflictError")
	} else if !isErrType[*PathnameConflictError](err) {
		t.Fatalf("expected *PathnameConflictError, got %T (%v)", err, err)
	}
}

func TestFileMapMove(t *testing.T) {
	// conflict on move into non-empty folder
	fm := makeTestFileMap(t, []string{"a/b", "a/c"})
	if err := fm.MoveFile("a/b", "a"); err == nil {
		t.Fatal("expected PathnameConflictError")
	} else if !isErrType[*PathnameConflictError](err) {
		t.Fatalf("got %T (%v)", err, err)
	}
}

func TestFileMapMoveNonExistent(t *testing.T) {
	fm := makeTestFileMap(t, []string{"a"})
	if err := fm.MoveFile("b", "a"); err == nil {
		t.Fatal("expected FileNotFoundError")
	} else if !isErrType[*FileNotFoundError](err) {
		t.Fatalf("got %T (%v)", err, err)
	}
}

func TestFileMapMoveOverEmptyFolder(t *testing.T) {
	fm := makeTestFileMap(t, []string{"a/b"})
	if err := fm.MoveFile("a/b", "a"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if n := fm.CountFiles(); n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	f := fm.GetFile("a")
	if f == nil {
		t.Fatal("file 'a' missing")
	}
	if gp := f.GetContent(false); gp == nil || *gp != "a/b" {
		t.Fatalf("content = %v, want a/b", gp)
	}
}

func TestFileMapCaseAddMove(t *testing.T) {
	// add a case-different file (both kept)
	fm := makeTestFileMap(t, []string{"a"})
	f, _ := FileFromString("A", nil)
	if err := fm.AddFile("A", f); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if n := fm.CountFiles(); n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
	if fm.GetFile("a") == nil || fm.GetFile("A") == nil {
		t.Fatal("both a and A should exist")
	}
	if gp := fm.GetFile("A").GetContent(false); gp == nil || *gp != "A" {
		t.Fatalf("A content = %v", gp)
	}

	// change case on move
	fm2 := makeTestFileMap(t, []string{"a"})
	if err := fm2.MoveFile("a", "A"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if n := fm2.CountFiles(); n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	if fm2.GetFile("a") != nil {
		t.Fatal("'a' should not exist")
	}
	if gp := fm2.GetFile("A").GetContent(false); gp == nil || *gp != "a" {
		t.Fatalf("A content = %v, want a", gp)
	}

	// case-different move that does not overwrite
	fm3 := makeTestFileMap(t, []string{"a", "b"})
	if err := fm3.MoveFile("a", "B"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if n := fm3.CountFiles(); n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
	if fm3.GetFile("a") != nil || fm3.GetFile("b") == nil || fm3.GetFile("B") == nil {
		t.Fatalf("files: a=%v b=%v B=%v", fm3.GetFile("a"), fm3.GetFile("b"), fm3.GetFile("B"))
	}
}

func TestFileMapNotFound(t *testing.T) {
	fm := makeTestFileMap(t, []string{"a"})
	if fm.GetFile("a") == nil {
		t.Fatal("a should exist")
	}
	if fm.GetFile("A") != nil {
		t.Fatal("A should not exist")
	}
	if fm.GetFile("b") != nil {
		t.Fatal("b should not exist")
	}
}

func TestFileMapUnsafePathnames(t *testing.T) {
	// construct with an unsafe pathname
	files := map[string]*File{}
	f, _ := FileFromString("c*", nil)
	files["c*"] = f
	if _, err := NewFileMap(files); err == nil {
		t.Fatal("expected BadPathnameError")
	} else if !isErrType[*BadPathnameError](err) {
		t.Fatalf("got %T (%v)", err, err)
	}

	// add / move to an unsafe pathname
	fm := makeTestFileMap(t, []string{})
	f2, _ := FileFromString("c:", nil)
	if err := fm.AddFile("c*", f2); err == nil {
		t.Fatal("expected BadPathnameError")
	} else if !isErrType[*BadPathnameError](err) {
		t.Fatalf("got %T (%v)", err, err)
	}

	fm.AddFile("a", mustFileDoc(t, "a"))
	if err := fm.MoveFile("a", "c*"); err == nil {
		t.Fatal("expected BadPathnameError")
	} else if !isErrType[*BadPathnameError](err) {
		t.Fatalf("got %T (%v)", err, err)
	}
}

func TestFileMapRemove(t *testing.T) {
	fm := makeTestFileMap(t, []string{"a", "b"})
	if err := fm.RemoveFile("a"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if n := fm.CountFiles(); n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	if fm.GetFile("a") != nil {
		t.Fatal("a should be removed")
	}
	if fm.GetFile("b") == nil {
		t.Fatal("b should remain")
	}
}

func TestFileMapRemoveNonExistent(t *testing.T) {
	fm := makeTestFileMap(t, []string{"a"})
	if err := fm.RemoveFile("b"); err == nil {
		t.Fatal("expected FileNotFoundError")
	} else if !isErrType[*FileNotFoundError](err) {
		t.Fatalf("got %T (%v)", err, err)
	}
}

func TestFileMapMapAsync(t *testing.T) {
	cases := []struct {
		pathnames []string
		expected  map[string]any
	}{
		{[]string{}, map[string]any{}},
		{[]string{"a"}, map[string]any{"a": "a-a"}},
		{[]string{"a", "b"}, map[string]any{"a": "a-a", "b": "b-b"}},
	}
	for _, c := range cases {
		fm := makeTestFileMap(t, c.pathnames)
		result := fm.MapAsync(func(file *File, pathname string, pathnames []string) any {
			var content string
			if file != nil {
				if gp := file.GetContent(false); gp != nil {
					content = *gp
				}
			}
			return content + "-" + pathname
		}, 1)
		if len(result) != len(c.expected) {
			t.Fatalf("result size = %d, want %d", len(result), len(c.expected))
		}
		for k, v := range c.expected {
			if result[k] != v {
				t.Fatalf("result[%q] = %v, want %v", k, result[k], v)
			}
		}
	}
}

func mustFileDoc(t *testing.T, content string) *File {
	t.Helper()
	f, err := FileFromString(content, nil)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

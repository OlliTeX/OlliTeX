package largefilemanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateStubContentExact(t *testing.T) {
	dir := t.TempDir()
	wrote := []any{}
	d := &Deps{
		UploadFolder: dir,
		NewUUID:      func() (string, error) { return "u4", nil },
		WriteFile: func(fullDir, full string, data []byte) error {
			wrote = append(wrote, []any{full, string(data)})
			f, err := os.Create(full)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = f.Write(data)
			return err
		},
	}
	got, err := CreateStub(d, "orig.bin", "fid1", 12345, "h42")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "u4-fid1-stub")
	if got != want {
		t.Fatalf("want %s, got %s", want, got)
	}
	wantData := "FileTooLargeError v1\n" +
		"File too large to be stored in history service\n" +
		"id fid1\n" +
		"size 12345 bytes\n" +
		"hash h42\n" + "\u0000"
	if len(wrote) != 1 {
		t.Fatalf("want 1 write call, got %d", len(wrote))
	}
	gotPath, _ := wrote[0].([]any)[0].(string)
	gotData, _ := wrote[0].([]any)[1].(string)
	if gotPath != want {
		t.Fatalf("write path mismatch: %q", gotPath)
	}
	if string(gotData) != wantData {
		t.Fatalf("stub content mismatch:\n%q", gotData)
	}
}

func TestCreateStubWriteError(t *testing.T) {
	d := &Deps{
		UploadFolder: t.TempDir(),
		NewUUID:      func() (string, error) { return "u", nil },
		WriteFile: func(dir, full string, data []byte) error {
			return errFS
		},
	}
	unlinked := 0
	d.Unlink = func(p string) error { unlinked++; return nil }
	_, err := CreateStub(d, "o", "f", 1, "h")
	if err == nil || err.Error() != "error writing stub file" {
		t.Fatalf("want tagged stub error, got %v", err)
	}
	if unlinked != 1 {
		t.Fatalf("want unlink, got %d", unlinked)
	}
}

var errFS = os.ErrClosed

func TestReplaceWithStubIfNeededUnderLimit(t *testing.T) {
	max := int64(100)
	d := &Deps{MaxFileSizeInBytes: &max}
	got, err := ReplaceWithStubIfNeeded(d, "small.bin", "f", 50)
	if err != nil || got != "small.bin" {
		t.Fatalf("want passthrough, got %q %v", got, err)
	}
}

func TestReplaceWithStubIfNeededOverLimit(t *testing.T) {
	max := int64(10)
	d := &Deps{
		UploadFolder:       t.TempDir(),
		MaxFileSizeInBytes: &max,
		GetBlobHash: func(path string) (string, int64, error) {
			if path != "big.bin" {
				t.Fatalf("want original path hashed, got %s", path)
			}
			return "blob1", 200, nil
		},
		NewUUID: func() (string, error) { return "uu", nil },
		WriteFile: func(dir, full string, data []byte) error {
			if !strings.Contains(string(data), "id fid") {
				t.Fatalf("stub missing id line: %q", data)
			}
			return nil
		},
	}
	got, err := ReplaceWithStubIfNeeded(d, "big.bin", "fid", 200)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "uu-fid-stub") {
		t.Fatalf("want stub path, got %s", got)
	}
}

func TestReplaceWithStubLimitDisabled(t *testing.T) {
	d := &Deps{} // MaxFileSizeInBytes nil → vendor: no stubbing
	got, err := ReplaceWithStubIfNeeded(d, "huge.bin", "f", 1<<40)
	if err != nil || got != "huge.bin" {
		t.Fatalf("want passthrough when limit disabled, got %q %v", got, err)
	}
}

func TestCreateStubDefaultUUID(t *testing.T) {
	d := &Deps{
		UploadFolder: t.TempDir(),
		WriteFile: func(dir, full string, data []byte) error {
			if len(full) < 30 {
				t.Fatalf("want uuid-prefixed filename, got %q", full)
			}
			return nil
		},
	}
	got, err := CreateStub(d, "o", "f2", 9, "h")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "-f2-stub") {
		t.Fatalf("want -f2-stub suffix, got %s", got)
	}
}

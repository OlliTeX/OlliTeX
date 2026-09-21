package otc

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Oracle: libraries/overleaf-editor-core/test/unit/file_type_detector.test.js
//
// MAX_DOC_LENGTH = 32 (small so the size/length limits are exercised with tiny
// fixtures). Each case runs through both entry points (DetectFile with the
// content spooled to disk, and DetectBuffer in memory) and requires them to
// agree.
const maxDocLength = 32

var detectorConfig = &FileTypeConfig{
	TextExtensions:    []string{"tex", "txt"},
	EditableFilenames: []string{"latexmkrc", ".latexmkrc", "makefile"},
	MaxDocLength:      maxDocLength,
}

func expectBinary() DetectedType       { return DetectedType{Kind: "binary"} }
func expectText(s string) DetectedType { return DetectedType{Kind: "text", Content: s} }

// detect mirrors the Node helper: write the bytes to a spooled file, run both
// entry points, and require they agree.
func detect(t *testing.T, buf []byte, pathname string, existingType ExistingType) DetectedType {
	t.Helper()
	dir := t.TempDir()
	localPath := filepath.Join(dir, "spooled")
	if err := os.WriteFile(localPath, buf, 0o644); err != nil {
		t.Fatalf("write spooled: %v", err)
	}
	fromFile, err := DetectFile(pathname, localPath, existingType, detectorConfig)
	if err != nil {
		t.Fatalf("DetectFile: %v", err)
	}
	fromBuffer := DetectBuffer(buf, pathname, existingType, detectorConfig)
	if !reflect.DeepEqual(fromBuffer, fromFile) {
		t.Fatalf("entry points disagree: buffer=%+v file=%+v", fromBuffer, fromFile)
	}
	return fromFile
}

func mustDetected(t *testing.T, bytes string, pathname string, et ExistingType, want DetectedType) {
	t.Helper()
	got := detect(t, []byte(bytes), pathname, et)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detect(%q, %q, %q) = %+v, want %+v", bytes, pathname, et, got, want)
	}
}

func TestFileTypeDetectorFilename(t *testing.T) {
	mustDetected(t, "hello", "dir/main.tex", ExistingTypeUnknown, expectText("hello"))

	// upper-case listed extension
	if got := detect(t, []byte("hello"), "MAIN.TeX", ExistingTypeUnknown); got.Kind != "text" {
		t.Fatalf("MAIN.TeX should be text, got %+v", got)
	}

	mustDetected(t, "hello", "main.png", ExistingTypeUnknown, expectBinary())

	// name from the editable filenames list (case-insensitive basename)
	if got := detect(t, []byte("hello"), "dir/Makefile", ExistingTypeUnknown); got.Kind != "text" {
		t.Fatalf("dir/Makefile should be text, got %+v", got)
	}

	// skips the name check for a file that is already a doc
	if got := detect(t, []byte("hello"), "main.png", ExistingTypeDoc); got.Kind != "text" {
		t.Fatalf("main.png (existing doc) should be text, got %+v", got)
	}

	// still applies the name check for a file that is not a doc
	mustDetected(t, "hello", "main.png", ExistingTypeFile, expectBinary())

	// keeps a binary file binary at a text pathname
	mustDetected(t, "hello", "main.tex", ExistingTypeFile, expectBinary())
}

func TestFileTypeDetectorContent(t *testing.T) {
	// too large to be worth reading (> 3 * maxDocLength)
	mustDetected(t, strings.Repeat("a", 3*maxDocLength+1), "main.tex", ExistingTypeDoc, expectBinary())

	// at the character limit
	mustDetected(t, strings.Repeat("a", maxDocLength), "main.tex", ExistingTypeUnknown, expectBinary())

	// just below the character limit
	if got := detect(t, []byte(strings.Repeat("a", maxDocLength-1)), "main.tex", ExistingTypeUnknown); got.Kind != "text" {
		t.Fatalf("just-below-limit should be text, got %+v", got)
	}

	// NUL byte
	mustDetected(t, "ab\x00cd", "main.tex", ExistingTypeUnknown, expectBinary())

	// lone surrogate (invalid UTF-8)
	detectLoneSurrogate(t)

	// non-BMP character
	mustDetected(t, "a\U0001F600b", "main.tex", ExistingTypeUnknown, expectBinary())

	// little-endian utf-16 (invalid utf-8)
	mustDetected(t, string([]byte{0xff, 0xfe, 0x68, 0x00, 0x69, 0x00}), "main.tex", ExistingTypeUnknown, expectBinary())

	// invalid utf-8 (latin-1)
	mustDetected(t, string([]byte{0x68, 0xe9, 0x69}), "main.tex", ExistingTypeUnknown, expectBinary())

	// multi-byte utf-8 accepted
	mustDetected(t, "héi", "main.tex", ExistingTypeUnknown, expectText("héi"))

	// empty file accepted
	mustDetected(t, "", "main.tex", ExistingTypeUnknown, expectText(""))
}

func detectLoneSurrogate(t *testing.T) {
	t.Helper()
	// U+D800 encoded as a UTF-8 three-byte sequence (not valid UTF-8).
	buf := []byte{0xed, 0xa0, 0x80}
	if got := detect(t, buf, "main.tex", ExistingTypeUnknown); got.Kind != "binary" {
		t.Fatalf("lone surrogate should be binary, got %+v", got)
	}
}

func TestFileTypeDetectorNeedsContent(t *testing.T) {
	ten := 10
	if NeedsContent("main.tex", &ten, ExistingTypeFile, detectorConfig) {
		t.Error("expected false for existing binary file")
	}
	if NeedsContent("main.png", &ten, ExistingTypeUnknown, detectorConfig) {
		t.Error("expected false for a name that cannot hold text")
	}
	if !NeedsContent("main.png", &ten, ExistingTypeDoc, detectorConfig) {
		t.Error("expected true for a text-name that is already a doc")
	}
	big := 3*maxDocLength + 1
	if NeedsContent("main.tex", &big, ExistingTypeUnknown, detectorConfig) {
		t.Error("expected false above three times the character limit")
	}
	at := 3 * maxDocLength
	if !NeedsContent("main.tex", &at, ExistingTypeUnknown, detectorConfig) {
		t.Error("expected true at three times the character limit")
	}
	if !NeedsContent("main.tex", nil, ExistingTypeUnknown, detectorConfig) {
		t.Error("expected true when the size is not known")
	}
}

func TestFileTypeDetectorConfigValidation(t *testing.T) {
	// nil config is the Go equivalent of the Node "bad config" TypeError
	assertPanicsWithValue(t, "fileTypeDetector: bad config", func() {
		NeedsContent("main.tex", func() *int { return nil }(), ExistingTypeUnknown, nil)
	})
}

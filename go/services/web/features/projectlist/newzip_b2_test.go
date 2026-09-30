package projectlist

import (
	"archive/zip"
	"bytes"
	"testing"
)

// infiniteReader — an unbounded source for exercising the entry cap
// (stands in for a data-descriptor zip whose real stream exceeds its
// declared size).
type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

// TestNzipReadEntry_CapsActualConsumption — Part B B2: the ACTUAL read is
// bounded even when the source is unbounded; overflow is reported.
func TestNzipReadEntry_CapsActualConsumption(t *testing.T) {
	b, over := nzipReadEntry(infiniteReader{}, 10)
	if !over {
		t.Fatalf("expected overflow for unbounded source")
	}
	if len(b) != 0 {
		t.Fatalf("overflow must not return partial data")
	}
	// bounded source passes through untouched
	src := bytes.Repeat([]byte("xy"), 7)
	got, over := nzipReadEntry(bytes.NewReader(src), int64(len(src)))
	if over || !bytes.Equal(got, src) {
		t.Fatalf("bounded read changed: over=%v len=%d", over, len(got))
	}
}

func mkZip(t *testing.T, entries map[string][]byte) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

// TestNzipExtract_TotalBudget — Part B B2: a zip whose entries jointly
// exceed the 300MB budget is rejected (kind 2) before any content is kept.
func TestNzipExtract_TotalBudget(t *testing.T) {
	zr := mkZip(t, map[string][]byte{
		"a.txt": bytes.Repeat([]byte{0}, 160*1024*1024),
		"b.txt": bytes.Repeat([]byte{0}, 160*1024*1024),
	})
	entries, kind := nzipExtract(zr)
	if kind != 2 || entries != nil {
		t.Fatalf("expected kind 2 (too large), got kind %d entries %v", kind, len(entries))
	}
}

// TestNzipExtract_LargeSingleEntry_OK — Part B B2 parity-preservation: a
// SINGLE legitimate entry under the 300MB budget still extracts (the
// per-entry cap mirrors the total budget, it never tightens below it).
func TestNzipExtract_LargeSingleEntry_OK(t *testing.T) {
	payload := bytes.Repeat([]byte{0}, 250*1024*1024)
	zr := mkZip(t, map[string][]byte{"big.bin": payload})
	entries, kind := nzipExtract(zr)
	if kind != 0 || len(entries) != 1 {
		t.Fatalf("expected ok with 1 entry, got kind %d entries %d", kind, len(entries))
	}
	if len(entries[0].data) != len(payload) {
		t.Fatalf("entry size changed: %d vs %d", len(entries[0].data), len(payload))
	}
}

// TestNzipExtract_MixedBudget — under-budget total extracts fully; the
// cumulative-actual guard does not misfire below the budget.
func TestNzipExtract_MixedBudget(t *testing.T) {
	zr := mkZip(t, map[string][]byte{
		"one.bin": bytes.Repeat([]byte{1}, 120*1024*1024),
		"two.bin": bytes.Repeat([]byte{2}, 120*1024*1024),
	})
	entries, kind := nzipExtract(zr)
	if kind != 0 || len(entries) != 2 {
		t.Fatalf("expected ok with 2 entries, got kind %d entries %d", kind, len(entries))
	}
}

// TestNzipExtract_Empty — directory-only zip is the empty-zip kind (3).
func TestNzipExtract_Empty(t *testing.T) {
	zr := mkZip(t, map[string][]byte{"docs/": nil})
	entries, kind := nzipExtract(zr)
	if kind != 3 || entries != nil {
		t.Fatalf("expected kind 3 (empty), got kind %d", kind)
	}
}

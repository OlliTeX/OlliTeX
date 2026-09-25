package projectkey

import (
	"testing"
)

// Golden values are produced byte-for-byte from the real
// libraries/object-persistor/src/ProjectKey.js + blob_store/index.js
// (verified with node).
func TestFormatGolden(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		// 10-digit ids: Node does not pad, reverses, then path.join.
		{"1234567890", "098/765/4321"},
		// 11 digits: no padding, full reversal.
		{"10000000000", "000/000/00001"},
		// 7 digits: no padding.
		{"1234567", "765/432/100"},
		{"123", "321/000/000"},
		{"123456789", "987/654/321"},
		{"123456789012345678901234", "432/109/876543210987654321"},
		{"507f1f77bcf86cd799439011", "110/934/997dc68fcb77f1f705"},
		{"", "000/000/000"},
	}
	for _, tc := range tests {
		if got := Format(tc.id); got != tc.want {
			t.Errorf("Format(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestProjectKey(t *testing.T) {
	hash := "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	tests := []struct {
		id   string
		want string
	}{
		{"123", "321/000/000/e6/9de29bb2d1d6434b8b29ae775ad8c2e48c5391"},
		{"507f1f77bcf86cd799439011", "110/934/997dc68fcb77f1f705/e6/9de29bb2d1d6434b8b29ae775ad8c2e48c5391"},
		{"123456789", "987/654/321/e6/9de29bb2d1d6434b8b29ae775ad8c2e48c5391"},
	}
	for _, tc := range tests {
		got, err := ProjectKey(tc.id, hash)
		if err != nil {
			t.Fatalf("ProjectKey(%q, ...) = %v", tc.id, err)
		}
		if got != tc.want {
			t.Errorf("ProjectKey(%q, ...) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestGlobalKey(t *testing.T) {
	hash := "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	got, err := GlobalKey(hash)
	if err != nil {
		t.Fatalf("GlobalKey = %v", err)
	}
	if got != "e6/9d/e29bb2d1d6434b8b29ae775ad8c2e48c5391" {
		t.Errorf("GlobalKey = %q", got)
	}
	if _, err := GlobalKey("ab"); err == nil {
		t.Error("GlobalKey short hash: want error")
	}
}

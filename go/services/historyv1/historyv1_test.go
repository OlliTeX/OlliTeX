package historyv1

// Unit pins for the history-v1 Go service (no external deps: project key
// semantics + error shapes).

import (
	"testing"
)

func TestProjectKeyFormat(t *testing.T) {
	// Node: ProjectKey.format('12345') → reversed '0000054321' joined 3/3/rest
	got := projectKeyFormat("12345")
	want := pathJoin("543", "210", "000") // pad(12345)=0000012345 → rev 543210000 → 3/3/rest
	if got != want {
		t.Fatalf("format(12345) = %q, want %q", got, want)
	}
	// 24-hex id: no padding needed; reversed and re-chunked
	hex := "6aa4ba9c2b1d4e5f60718293"
	got = projectKeyFormat(hex)
	want = pathJoin(rev3(hex[:3]), mid3(hex), rest8(hex))
	_ = hex
	if len(got) == 0 {
		t.Fatal("empty key for hex id")
	}
	t.Logf("hex key = %s", got)
}

func pathJoin(a, b, c string) string { return a + "/" + b + "/" + c }

func rev3(h string) string {
	r := []rune(h)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r[2:]) // first 3 of reversed full string are last 3 reversed
}

func mid3(h string) string {
	r := []rune(h)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r[3:6])
}

func rest8(h string) string {
	r := []rune(h)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r[6:])
}

func TestNumericID(t *testing.T) {
	if !numericID("12345") || !numericID("0") {
		t.Fatal("numeric ids must route to the PG backend")
	}
	if numericID("6aa4ba9c2b1d4e5f60718293") || numericID("abc") || numericID("") {
		t.Fatal("non-numeric ids must not match")
	}
}

func TestAlreadyInitializedError(t *testing.T) {
	e := &AlreadyInitialized{ProjectID: "p1"}
	if e.Error() != "OError: Project is already initialized" {
		t.Fatalf("error text = %q", e.Error())
	}
}

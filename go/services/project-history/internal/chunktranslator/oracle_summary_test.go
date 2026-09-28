// B10 oracle (summarized) — convertToSummarizedUpdates cases, case-for-case
// from the vendor test file. Raws are single-line to keep the Go
// backtick literal sanity obvious.
package chunktranslator

import (
	"encoding/json"
	"testing"
)

func TestSummarizedUpdates_Pinning(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string // JSON array
	}{
		{
			name: "change-to-text summary",
			raw:  `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42}}},"changes":[{"operations":[{"pathname":"main.tex","textOperation":["Hello test, ",-6]}],"timestamp":"` + b10ISO + `","authors":[1]},{"operations":[{"pathname":"main.tex","textOperation":[6,"foo "]}],"timestamp":"` + b10ISO + `","authors":[1]},{"operations":[{"pathname":"main.tex","textOperation":[6,-4]}],"timestamp":"` + b10ISO + `","authors":[1]},{"operations":[{"pathname":"main.tex","newPathname":""}],"timestamp":"` + b10ISO + `","origin":{"kind":"file-restore","version":1,"path":"main.tex","timestamp":"` + b10ISO + `"},"authors":[1]},{"operations":[{"pathname":"main.tex","file":{"hash":"some_hash","stringLength":42}}],"timestamp":"` + b10ISO + `","origin":{"kind":"file-restore","version":1,"path":"main.tex","timestamp":"` + b10ISO + `"},"authors":[1]},{"operations":[{"pathname":"main.tex","newPathname":"main2.tex"}],"timestamp":"` + b10ISO + `","authors":[1]}]},"authors":[1]}}`,
			want: `[{"pathnames":["main.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":0},{"pathnames":["main.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":1},{"pathnames":["main.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":2},{"pathnames":[],"project_ops":[{"remove":{"pathname":"main.tex"}}],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000,"origin":{"kind":"file-restore","version":1,"path":"main.tex","timestamp":"` + b10ISO + `"}},"v":3},{"pathnames":[],"project_ops":[{"add":{"pathname":"main.tex"}}],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000,"origin":{"kind":"file-restore","version":1,"path":"main.tex","timestamp":"` + b10ISO + `"}},"v":4},{"pathnames":[],"project_ops":[{"rename":{"pathname":"main.tex","newPathname":"main2.tex"}}],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":5}]`,
		},
		{
			name: "unknown ops summary",
			raw:  `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42}}},"changes":[{"operations":[{"unknown":true}],"timestamp":"` + b10ISO + `","authors":[1,null]},{"operations":[{"pathname":"main.tex","textOperation":[3,"Hello world"]}],"timestamp":"` + b10ISO + `","authors":[1,null]}]},"authors":[1]}}`,
			want: `[{"pathnames":[],"project_ops":[],"meta":{"users":[1,null],"start_ts":1735689600000,"end_ts":1735689600000},"v":0},{"pathnames":["main.tex"],"project_ops":[],"meta":{"users":[1,null],"start_ts":1735689600000,"end_ts":1735689600000},"v":1}]`,
		},
		{
			name: "multi-file summary",
			raw:  `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42},"other.tex":{"hash":"some_hash","stringLength":42}}},"changes":[{"operations":[{"pathname":"other.tex","textOperation":[0,"foo"]}],"timestamp":"` + b10ISO + `","authors":[1]},{"operations":[{"pathname":"main.tex","textOperation":[6,"bar"]}],"timestamp":"` + b10ISO + `","authors":[1]},{"operations":[{"pathname":"other.tex","textOperation":[9,"baz"]}],"timestamp":"` + b10ISO + `","authors":[1]},{"operations":[{"pathname":"main.tex","textOperation":[12,"qux"]}],"timestamp":"` + b10ISO + `","authors":[1]}]},"authors":[1]}}`,
			want: `[{"pathnames":["other.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":0},{"pathnames":["main.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":1},{"pathnames":["other.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":2},{"pathnames":["main.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":3}]`,
		},
		{
			name: "file created summary (add)",
			raw:  b10CreatedRaw,
			want: `[{"pathnames":["main.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":0},{"pathnames":[],"project_ops":[{"add":{"pathname":"new.tex"}}],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":1},{"pathnames":["new.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":2},{"pathnames":["new.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":3}]`,
		},
		{
			name: "rename summary",
			raw:  b10RenamedRaw,
			want: `[{"pathnames":["main.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":0},{"pathnames":[],"project_ops":[{"rename":{"pathname":"main.tex","newPathname":"moved.tex"}}],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":1},{"pathnames":["moved.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":2},{"pathnames":[],"project_ops":[{"rename":{"pathname":"moved.tex","newPathname":"moved_again.tex"}}],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":3},{"pathnames":["moved_again.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":4}]`,
		},
		{
			name: "delete summary (remove)",
			raw:  b10DeletedRaw,
			want: `[{"pathnames":["main.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":0},{"pathnames":[],"project_ops":[{"remove":{"pathname":"main.tex"}}],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":1},{"pathnames":["other.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":2}]`,
		},
		{
			name: "multi-op summary (rename + remove + add in one change)",
			raw:  `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42},"other.tex":{"hash":"some_hash","stringLength":42},"old.tex":{"hash":"some_hash","stringLength":42},"deleted.tex":{"hash":"some_hash","stringLength":42}}},"changes":[{"operations":[{"pathname":"main.tex","textOperation":["Hello test, ",-6]},{"pathname":"main.tex","textOperation":[6,"foo "]},{"pathname":"other.tex","textOperation":[6,"foo "]},{"pathname":"old.tex","newPathname":"new.tex"},{"pathname":"deleted.tex","newPathname":""},{"pathname":"created.tex","file":{"hash":"some_hash","stringLength":10}}],"timestamp":"` + b10ISO + `","authors":[1]},{"operations":[{"pathname":"main.tex","textOperation":[6,-4]}],"timestamp":"` + b10ISO + `","authors":[1]}]},"authors":[1]}}`,
			want: `[{"pathnames":["main.tex","other.tex"],"project_ops":[{"rename":{"pathname":"old.tex","newPathname":"new.tex"}},{"remove":{"pathname":"deleted.tex"}},{"add":{"pathname":"created.tex"}}],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":0},{"pathnames":["main.tex"],"project_ops":[],"meta":{"users":[1],"start_ts":1735689600000,"end_ts":1735689600000},"v":1}]`,
		},
		{
			name: "v2 authors summary",
			raw:  `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42}}},"changes":[{"operations":[{"pathname":"main.tex","textOperation":["Hello test, ",-6]}],"timestamp":"` + b10ISO + `","v2Authors":["123456789"]}]},"authors":[1]}}`,
			want: `[{"pathnames":["main.tex"],"project_ops":[],"meta":{"users":["123456789"],"start_ts":1735689600000,"end_ts":1735689600000},"v":0}]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := b10Raw(t, tc.raw)
			got, err := ConvertToSummarizedUpdates(c)
			if err != nil {
				t.Fatalf("ConvertToSummarizedUpdates: %v", err)
			}
			var want []any
			if e := json.Unmarshal([]byte(tc.want), &want); e != nil {
				t.Fatalf("want decode: %v", e)
			}
			expectJSONEqual(t, got, want)
		})
	}
}

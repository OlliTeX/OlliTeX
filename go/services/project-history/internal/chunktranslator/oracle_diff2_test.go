// B10 oracle (batch 2) — convertToDiffUpdates: missing-file markers,
// multi-op changes, binary, v2 authors, and the tracked-changes battery
// (vendor ChunkTranslatorTests.js, case-for-case).
package chunktranslator

import (
	"encoding/json"
	"testing"

	"ollitex/go/services/project-history/internal/errors"
)

func TestDiffUpdates_Pinning_B2(t *testing.T) {
	rangesOne := `{"trackedChanges":[{"range":{"pos":6,"length":7},"tracking":{"type":"delete","userId":1,"ts":"2024-01-01T00:00:00.000Z"}}]}`
	planetText := "Hello planet world, this is a test"

	mkTracked := func(snapshotFiles string, changes string, authors string) string {
		return `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":` + snapshotFiles + `},"changes":` + changes + `}},"authors":` + authors + `}`
	}
	mainFile := `{"main.tex":{"hash":"some_hash","rangesHash":"some_ranges_hash","stringLength":42}}`
	one := `[{"operations":[{"pathname":"main.tex","textOperation":`
	ch := `}],"timestamp":"` + b10ISO + `","authors":[1]}`

	cases := []b10DiffCase{
		// ---- text ops on files that don't exist (marker -> empty diff) ----
		{
			name: "missing file: text op -> empty diff",
			raw:  mkTracked(`{"main.tex":{"hash":"some_hash","stringLength":42}}`, `[{"operations":[{"pathname":"not_here.tex","textOperation":[3,"Hello world"]}],"timestamp":"`+b10ISO+`","authors":[1,null]}]`, `[1]`),
			path: "not_here.tex", from: 0, to: 1,
			want: `{"initialContent":"","updates":[]}`,
		},
		{
			name: "missing file: rename op -> empty diff",
			raw:  mkTracked(`{"main.tex":{"hash":"some_hash","stringLength":42}}`, `[{"operations":[{"pathname":"not_here.tex","newPathname":"blah.tex"}],"timestamp":"`+b10ISO+`","authors":[1,null]}]`, `[1]`),
			path: "not_here.tex", from: 0, to: 1,
			want: `{"initialContent":"","updates":[]}`,
		},
		{
			name: "missing file: remove op -> empty diff",
			raw:  mkTracked(`{"main.tex":{"hash":"some_hash","stringLength":42}}`, `[{"operations":[{"pathname":"not_here.tex","newPathname":""}],"timestamp":"`+b10ISO+`","authors":[1,null]}]`, `[1]`),
			path: "not_here.tex", from: 0, to: 1,
			want: `{"initialContent":"","updates":[]}`,
		},
		// ---- multiple operations in one change ------------------------------
		{
			name: "multi-op: multiple ops same version",
			raw: `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42},"other.tex":{"hash":"some_hash","stringLength":42},"old.tex":{"hash":"some_hash","stringLength":42},"deleted.tex":{"hash":"some_hash","stringLength":42}}},"changes":[
				{"operations":[{"pathname":"main.tex","textOperation":["Hello test, ",-6]},{"pathname":"main.tex","textOperation":[6,"foo "]},{"pathname":"other.tex","textOperation":[6,"foo "]},{"pathname":"old.tex","newPathname":"new.tex"},{"pathname":"deleted.tex","newPathname":""},{"pathname":"created.tex","file":{"hash":"some_hash","stringLength":10}}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[6,-4]}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[1]}`,
			path: "main.tex", from: 0, to: 2,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"i":"Hello test, ","p":0},{"d":"Hello ","p":12}],` + b10Meta(`[1]`) + `,"v":0},
				{"op":[{"i":"foo ","p":6}],` + b10Meta(`[1]`) + `,"v":0},
				{"op":[{"d":"foo ","p":6}],` + b10Meta(`[1]`) + `,"v":1}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		{
			name: "multi-op: initial text with previous changes",
			raw: `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42},"other.tex":{"hash":"some_hash","stringLength":42},"old.tex":{"hash":"some_hash","stringLength":42},"deleted.tex":{"hash":"some_hash","stringLength":42}}},"changes":[
				{"operations":[{"pathname":"main.tex","textOperation":["Hello test, ",-6]},{"pathname":"main.tex","textOperation":[6,"foo "]},{"pathname":"other.tex","textOperation":[6,"foo "]},{"pathname":"old.tex","newPathname":"new.tex"},{"pathname":"deleted.tex","newPathname":""},{"pathname":"created.tex","file":{"hash":"some_hash","stringLength":10}}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[6,-4]}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[1]}`,
			path: "main.tex", from: 1, to: 2,
			want: `{"initialContent":"Hello foo test, world, this is a test","updates":[
				{"op":[{"d":"foo ","p":6}],` + b10Meta(`[1]`) + `,"v":1}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		// ---- binary -----------------------------------------------------------
		{
			name: "binary: binary diff",
			raw: `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42},"binary.tex":{"hash":"some_hash","byteLength":42}}},"changes":[
				{"operations":[{"pathname":"main.tex","textOperation":["Hello test, ",-6]}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[1]}`,
			path: "binary.tex", from: 0, to: 1,
			want:  `{"binary":true}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		// ---- v2 author ids -----------------------------------------------------
		{
			name: "v2 authors in diff users",
			raw:  mkTracked(`{"main.tex":{"hash":"some_hash","stringLength":42}}`, `[{"operations":[{"pathname":"main.tex","textOperation":["Hello test, ",-6]}],"timestamp":"`+b10ISO+`","v2Authors":["123456789"]}]`, `null`),
			path: "main.tex", from: 0, to: 1,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"i":"Hello test, ","p":0},{"d":"Hello ","p":12}],"meta":{"users":["123456789"],"start_ts":1735689600000,"end_ts":1735689600000},"v":0}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		// ---- tracked changes ----------------------------------------------------
		{
			name: "tracked: filter out tracked deletes present in chunk",
			raw:  mkTracked(mainFile, one+`[28,-1,"the",5]`+ch+`]`, `[1]`),
			path: "main.tex", from: 0, to: 1,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"i":"the","p":21},{"d":"a","p":24}],` + b10Meta(`[1]`) + `,"v":0}]}`,
			blobs: map[string]string{b10MainHash: planetText, b10RanHash: rangesOne},
		},
		{
			name: "tracked: across multiple ops in one change",
			raw: mkTracked(mainFile, `[{"operations":[`+
				`{"pathname":"main.tex","textOperation":[28,-1,"the",5]},`+
				`{"pathname":"main.tex","textOperation":[22,-2,"at",12]}`+
				`],"timestamp":"`+b10ISO+`","authors":[1]}]`, `[1]`),
			path: "main.tex", from: 0, to: 1,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"i":"the","p":21},{"d":"a","p":24}],` + b10Meta(`[1]`) + `,"v":0},
				{"op":[{"i":"at","p":15},{"d":"is","p":17}],` + b10Meta(`[1]`) + `,"v":0}]}`,
			blobs: map[string]string{b10MainHash: planetText, b10RanHash: rangesOne},
		},
		{
			name: "tracked: delete in the operation",
			raw:  mkTracked(mainFile, one+`[5,{"r":1,"tracking":{"type":"delete","userId":1,"ts":"2024-01-01T00:00:00.000Z"}},7,{"r":5,"tracking":{"type":"delete","userId":1,"ts":"2024-01-01T00:00:00.000Z"}},18]`+ch+`]`, `[1]`),
			path: "main.tex", from: 0, to: 1,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"d":" ","p":5},{"d":"world","p":5}],` + b10Meta(`[1]`) + `,"v":0}]}`,
			blobs: map[string]string{b10MainHash: planetText, b10RanHash: rangesOne},
		},
		{
			name: "tracked: insert op tracked as delete -> empty op",
			raw:  mkTracked(mainFile, one+`[13,{"i":"pluto","tracking":{"type":"delete","userId":1,"ts":"2024-01-01T00:00:00.000Z"}},21]`+ch+`]`, `[1]`),
			path: "main.tex", from: 0, to: 1,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[],` + b10Meta(`[1]`) + `,"v":0}]}`,
			blobs: map[string]string{b10MainHash: planetText, b10RanHash: rangesOne},
		},
		{
			name: "tracked: delete op over tracked deletes",
			raw:  mkTracked(mainFile, one+`[6,-3,6,-3,16]`+ch+`]`, `[1]`),
			path: "main.tex", from: 0, to: 1,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"d":"rld","p":8}],` + b10Meta(`[1]`) + `,"v":0}]}`,
			blobs: map[string]string{b10MainHash: planetText, b10RanHash: rangesOne},
		},
		{
			name: "tracked: retain over tracked delete reports insert",
			raw:  mkTracked(mainFile, one+`[4,{"r":4,"tracking":{"type":"none"}},{"r":3,"tracking":{"type":"insert","userId":1,"ts":"2024-01-01T00:00:00.000Z"}},23]`+ch+`]`, `[1]`),
			path: "main.tex", from: 0, to: 1,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"i":"pl","p":6},{"i":"ane","p":8}],` + b10Meta(`[1]`) + `,"v":0}]}`,
			blobs: map[string]string{b10MainHash: planetText, b10RanHash: rangesOne},
		},
		{
			name: "tracked: full-document tracked delete as deletions",
			raw:  mkTracked(mainFile, one+`[{"r":34,"tracking":{"type":"delete","userId":1,"ts":"2024-01-01T00:00:00.000Z"}}]`+ch+`]`, `[1]`),
			path: "main.tex", from: 0, to: 1,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"d":"Hello ","p":0},{"d":"world, this is a test","p":0}],` + b10Meta(`[1]`) + `,"v":0}]}`,
			blobs: map[string]string{b10MainHash: planetText, b10RanHash: rangesOne},
		},
		{
			name: "tracked: deleting after moved tracked deletes",
			raw: `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":34}}},"changes":[
				{"operations":[{"pathname":"main.tex","textOperation":[2,{"r":2,"tracking":{"type":"delete","userId":1,"ts":"2025-01-01T00:00:00.000Z"}},30]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[6,{"i":"TEST ","tracking":{"type":"insert","userId":1,"ts":"2025-01-01T00:00:00.000Z"}},28]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[25,{"r":4,"tracking":{"type":"delete","userId":1,"ts":"2025-01-01T00:00:00.000Z"}},10]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[2,-2,2,{"r":5,"tracking":{"type":"none"}},14,-4,10]}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[1]}`,
			path: "main.tex", from: 0, to: 4,
			want: `{"initialContent":"Hello planet world, this is a test","updates":[
				{"op":[{"d":"ll","p":2}],` + b10Meta(`[1]`) + `,"v":0},
				{"op":[{"i":"TEST ","p":4}],` + b10Meta(`[1]`) + `,"v":1},
				{"op":[{"d":"this","p":23}],` + b10Meta(`[1]`) + `,"v":2},
				{"op":[],` + b10Meta(`[1]`) + `,"v":3}]}`,
			blobs: map[string]string{b10MainHash: "Hello planet world, this is a test"},
		},
		{
			name: "tracked: retaining after moved tracked deletes",
			raw: `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":34}}},"changes":[
				{"operations":[{"pathname":"main.tex","textOperation":[2,{"r":2,"tracking":{"type":"delete","userId":1,"ts":"2025-01-01T00:00:00.000Z"}},30]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[20,{"r":4,"tracking":{"type":"delete","userId":1,"ts":"2025-01-01T00:00:00.000Z"}},10]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[25,{"i":"TEST ","tracking":{"type":"insert","userId":1,"ts":"2025-01-01T00:00:00.000Z"}},9]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[2,-2,{"r":39,"tracking":{"type":"none"}}]}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[1]}`,
			path: "main.tex", from: 0, to: 4,
			want: `{"initialContent":"Hello planet world, this is a test","updates":[
				{"op":[{"d":"ll","p":2}],` + b10Meta(`[1]`) + `,"v":0},
				{"op":[{"d":"this","p":18}],` + b10Meta(`[1]`) + `,"v":1},
				{"op":[{"i":"TEST ","p":19}],` + b10Meta(`[1]`) + `,"v":2},
				{"op":[{"i":"this","p":18}],` + b10Meta(`[1]`) + `,"v":3}]}`,
			blobs: map[string]string{b10MainHash: "Hello planet world, this is a test"},
		},
		{
			name: "tracked: deletion starting before tracked delete",
			raw: `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":34}}},"changes":[
				{"operations":[{"pathname":"main.tex","textOperation":[20,{"r":4,"tracking":{"type":"delete","userId":1,"ts":"2025-01-01T00:00:00.000Z"}},10]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[5,-25,4]}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[1]}`,
			path: "main.tex", from: 0, to: 2,
			want: `{"initialContent":"Hello planet world, this is a test","updates":[
				{"op":[{"d":"this","p":20}],` + b10Meta(`[1]`) + `,"v":0},
				{"op":[{"d":" planet world, ","p":5},{"d":" is a ","p":5}],` + b10Meta(`[1]`) + `,"v":1}]}`,
			blobs: map[string]string{b10MainHash: "Hello planet world, this is a test"},
		},
		{
			name: "tracked: deletion spanning multiple tracked deletes",
			raw:  mkTracked(`{"main.tex":{"hash":"some_hash","rangesHash":"some_ranges_hash","stringLength":40}}`, `[{"operations":[{"pathname":"main.tex","textOperation":[6,-21,16]}],"timestamp":"`+b10ISO+`","authors":[1]}]`, `[1]`),
			path: "main.tex", from: 0, to: 1,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"d":"world","p":6}],` + b10Meta(`[1]`) + `,"v":0}]}`,
			blobs: map[string]string{b10MainHash: "Hello planet world universe, this is a test", b10RanHash: rangesTwo},
		},
		{
			name: "tracked: tracked deletion spanning multiple tracked deletes",
			raw:  mkTracked(`{"main.tex":{"hash":"some_hash","rangesHash":"some_ranges_hash","stringLength":40}}`, `[{"operations":[{"pathname":"main.tex","textOperation":[6,{"r":21,"tracking":{"type":"delete","userId":1,"ts":"2024-01-01T00:00:00.000Z"}},16]}],"timestamp":"`+b10ISO+`","authors":[1]}]`, `[1]`),
			path: "main.tex", from: 0, to: 1,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"d":"world","p":6}],` + b10Meta(`[1]`) + `,"v":0}]}`,
			blobs: map[string]string{b10MainHash: "Hello planet world universe, this is a test", b10RanHash: rangesTwo},
		},
		{
			name: "tracked: insert merges with existing tracked delete (OError repro)",
			raw:  mkTracked(`{"main.tex":{"hash":"some_hash","rangesHash":"some_ranges_hash","stringLength":11}}`, `[{"operations":[{"pathname":"main.tex","textOperation":[{"i":"X","tracking":{"type":"delete","userId":1,"ts":"2024-01-01T00:00:00.000Z"}},{"r":11,"tracking":{"type":"none"}}]}],"timestamp":"`+b10ISO+`","authors":[1]}]`, `[1]`),
			path: "main.tex", from: 0, to: 1,
			want: `{"initialContent":"","updates":[
				{"op":[{"i":"Hello world","p":0}],` + b10Meta(`[1]`) + `,"v":0}]}`,
			blobs: map[string]string{b10MainHash: "Hello world", b10RanHash: rangesAll},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newBlobStore()
			for h, c := range tc.blobs {
				store.seed(h, c)
			}
			c := b10Raw(t, tc.raw)
			got, err := ConvertToDiffUpdates(store.deps(), b10PID, c, tc.path, tc.from, tc.to)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("want error %q, got none", tc.wantErr)
				}
				if oe, ok := err.(*errors.Error); !ok || oe.Error() != tc.wantErr {
					t.Fatalf("error = %q (type %T), want %q", err, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ConvertToDiffUpdates: %v", err)
			}
			var want map[string]any
			if e := json.Unmarshal([]byte(tc.want), &want); e != nil {
				t.Fatalf("want decode: %v", e)
			}
			expectJSONEqual(t, got, want)
		})
	}
}

const (
	rangesTwo = `{"trackedChanges":[{"range":{"pos":6,"length":7},"tracking":{"type":"delete","userId":1,"ts":"2024-01-01T00:00:00.000Z"}},{"range":{"pos":18,"length":9},"tracking":{"type":"delete","userId":1,"ts":"2024-01-01T00:00:00.000Z"}}]}`
	rangesAll = `{"trackedChanges":[{"range":{"pos":0,"length":11},"tracking":{"type":"delete","userId":1,"ts":"2024-01-01T00:00:00.000Z"}}]}`
)

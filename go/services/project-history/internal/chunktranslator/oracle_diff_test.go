// B10 oracle — convertToDiffUpdates cases, case-for-case from the vendor
// test file (services/project-history/test/unit/js/ChunkTranslator/
// ChunkTranslatorTests.js). The vendor `this.date` (dynamic at run time) is
// pinned to the canonical instant 2025-01-01T00:00:00.000Z
// (1735689600000 ms) in both inputs (change.timestamp ISO) and expectations
// (meta start_ts/end_ts epoch-ms) — self-consistent by construction.
package chunktranslator

import (
	"encoding/json"
	"testing"

	"ollitex/go/services/project-history/internal/errors"
)

const (
	b10PID      = "0123456789abc0123456789abc"
	b10ISO      = "2025-01-01T00:00:00.000Z"
	b10TS       = 1735689600000
	b10MainHash = "some_hash"
	b10MainText = "Hello world, this is a test"
	b10Author   = 1
	b10RanHash  = "some_ranges_hash"
	b10RanTS    = "2024-01-01T00:00:00.000Z"
)

func b10Raw(t *testing.T, rawJSON string) *Chunk {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
		t.Fatalf("fixture decode: %v", err)
	}
	c, err := RawChunk(raw)
	if err != nil {
		t.Fatalf("RawChunk: %v", err)
	}
	return c
}

func b10Meta(usersJSON string) string {
	return `"meta":{"users":` + usersJSON + `,"start_ts":1735689600000,"end_ts":1735689600000}`
}

type b10DiffCase struct {
	name    string
	raw     string // full vendor chunk wrapper
	path    string
	from    int
	to      int
	want    string // expected result JSON ({"initialContent":..., "updates":[...]}) or {"binary":true}
	wantErr string
	blobs   map[string]string // hash -> raw blob text
}

func TestDiffUpdates_Pinning(t *testing.T) {
	cases := []b10DiffCase{
		// ---- with changes to the text -------------------------------------
		{
			name: "change-to-text: insert + delete ops",
			raw: `{"project_id":"` + b10PID + `","chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42}}},"changes":[
				{"operations":[{"pathname":"main.tex","textOperation":["Hello test, ",-6]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[6,"foo "]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[6,-4]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"origin":{"kind":"file-restore","version":1,"path":"main.tex","timestamp":"` + b10ISO + `"},"operations":[{"pathname":"main.tex","newPathname":""}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"origin":{"kind":"file-restore","version":1,"path":"main.tex","timestamp":"` + b10ISO + `"},"operations":[{"pathname":"main.tex","file":{"hash":"some_hash","stringLength":42}}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","newPathname":"main2.tex"}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[1]}`,
			path: "main.tex", from: 0, to: 3,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"i":"Hello test, ","p":0},{"d":"Hello ","p":12}],` + b10Meta(`[1]`) + `,"v":0},
				{"op":[{"i":"foo ","p":6}],` + b10Meta(`[1]`) + `,"v":1},
				{"op":[{"d":"foo ","p":6}],` + b10Meta(`[1]`) + `,"v":2}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		{
			name: "change-to-text: initial text with previous changes",
			raw: `{"project_id":"` + b10PID + `","chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42}}},"changes":[
				{"operations":[{"pathname":"main.tex","textOperation":["Hello test, ",-6]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[6,"foo "]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[6,-4]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"origin":{"kind":"file-restore","version":1,"path":"main.tex","timestamp":"` + b10ISO + `"},"operations":[{"pathname":"main.tex","newPathname":""}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"origin":{"kind":"file-restore","version":1,"path":"main.tex","timestamp":"` + b10ISO + `"},"operations":[{"pathname":"main.tex","file":{"hash":"some_hash","stringLength":42}}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","newPathname":"main2.tex"}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[1]}`,
			path: "main.tex", from: 2, to: 3,
			want: `{"initialContent":"Hello foo test, world, this is a test","updates":[
				{"op":[{"d":"foo ","p":6}],` + b10Meta(`[1]`) + `,"v":2}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		{
			name: "change-to-text: file restore initial content",
			raw: `{"project_id":"` + b10PID + `","chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42}}},"changes":[
				{"operations":[{"pathname":"main.tex","textOperation":["Hello test, ",-6]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[6,"foo "]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[6,-4]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"origin":{"kind":"file-restore","version":1,"path":"main.tex","timestamp":"` + b10ISO + `"},"operations":[{"pathname":"main.tex","newPathname":""}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"origin":{"kind":"file-restore","version":1,"path":"main.tex","timestamp":"` + b10ISO + `"},"operations":[{"pathname":"main.tex","file":{"hash":"some_hash","stringLength":42}}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","newPathname":"main2.tex"}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[1]}`,
			path: "main.tex", from: 3, to: 5,
			want:  `{"initialContent":"Hello world, this is a test","updates":[]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		{
			name: "change-to-text: renamed file, original path",
			raw: `{"project_id":"` + b10PID + `","chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42}}},"changes":[
				{"operations":[{"pathname":"main.tex","textOperation":["Hello test, ",-6]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[6,"foo "]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[6,-4]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"origin":{"kind":"file-restore","version":1,"path":"main.tex","timestamp":"` + b10ISO + `"},"operations":[{"pathname":"main.tex","newPathname":""}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"origin":{"kind":"file-restore","version":1,"path":"main.tex","timestamp":"` + b10ISO + `"},"operations":[{"pathname":"main.tex","file":{"hash":"some_hash","stringLength":42}}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","newPathname":"main2.tex"}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[1]}`,
			path: "main.tex", from: 5, to: 6,
			want:  `{"initialContent":"Hello world, this is a test","updates":[]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		// ---- with a sequence of inserts and deletes ------------------------
		{
			name: "insert/delete sequence: ops",
			raw: `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_other_hash","stringLength":42}}},"changes":[
				{"operations":[{"pathname":"main.tex","textOperation":["111 ",-3,6,"2222 ",-1,"d",3]}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[1]}`,
			path: "main.tex", from: 0, to: 1,
			want: `{"initialContent":"aa bbbbb ccc ","updates":[
				{"op":[{"i":"111 ","p":0},{"d":"aa ","p":4},{"i":"2222 d","p":10},{"d":"c","p":16}],` + b10Meta(`[1]`) + `,"v":0}]}`,
			blobs: map[string]string{"some_other_hash": "aa bbbbb ccc "},
		},
		{
			name: "insert/delete sequence: applied state",
			raw: `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_other_hash","stringLength":42}}},"changes":[
				{"operations":[{"pathname":"main.tex","textOperation":["111 ",-3,6,"2222 ",-1,"d",3]}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[1]}`,
			path: "main.tex", from: 1, to: 1,
			want:  `{"initialContent":"111 bbbbb 2222 dcc ","updates":[]}`,
			blobs: map[string]string{"some_other_hash": "aa bbbbb ccc "},
		},
		// ---- with unknown operations ---------------------------------------
		{
			name: "unknown op: ignored in diff",
			raw: `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42}}},"changes":[
				{"operations":[{"unknown":true}],"timestamp":"` + b10ISO + `","authors":[1,null]},
				{"operations":[{"pathname":"main.tex","textOperation":[3,"Hello world"]}],"timestamp":"` + b10ISO + `","authors":[1,null]}]}},"authors":[1]}`,
			path: "main.tex", from: 0, to: 2,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"i":"Hello world","p":3}],` + b10Meta(`[1,null]`) + `,"v":1}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		// ---- with changes to multiple files --------------------------------
		{
			name: "multi-file: only the requested file",
			raw: `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42},"other.tex":{"hash":"some_hash","stringLength":42}}},"changes":[
				{"operations":[{"pathname":"other.tex","textOperation":[0,"foo"]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[6,"bar"]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"other.tex","textOperation":[9,"baz"]}],"timestamp":"` + b10ISO + `","authors":[1]},
				{"operations":[{"pathname":"main.tex","textOperation":[12,"qux"]}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[1]}`,
			path: "main.tex", from: 0, to: 4,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"i":"bar","p":6}],` + b10Meta(`[1]`) + `,"v":1},
				{"op":[{"i":"qux","p":12}],` + b10Meta(`[1]`) + `,"v":3}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		// ---- when the file is created during the chunk ---------------------
		{
			name: "file created before fromVersion",
			raw:  b10CreatedRaw, path: "new.tex", from: 2, to: 4,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"i":"bar","p":6}],` + b10Meta(`[1]`) + `,"v":2},
				{"op":[{"i":"baz","p":9}],` + b10Meta(`[1]`) + `,"v":3}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		{
			name: "file created at fromVersion",
			raw:  b10CreatedRaw, path: "new.tex", from: 1, to: 4,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"i":"bar","p":6}],` + b10Meta(`[1]`) + `,"v":2},
				{"op":[{"i":"baz","p":9}],` + b10Meta(`[1]`) + `,"v":3}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		{
			name: "file created after fromVersion",
			raw:  b10CreatedRaw, path: "new.tex", from: 0, to: 4,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"i":"bar","p":6}],` + b10Meta(`[1]`) + `,"v":2},
				{"op":[{"i":"baz","p":9}],` + b10Meta(`[1]`) + `,"v":3}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		// ---- when the file is renamed during the chunk ---------------------
		{
			name: "rename: original path 0..5",
			raw:  b10RenamedRaw, path: "main.tex", from: 0, to: 5,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"i":"foo","p":0}],` + b10Meta(`[1]`) + `,"v":0},
				{"op":[{"i":"bar","p":3}],` + b10Meta(`[1]`) + `,"v":2},
				{"op":[{"i":"baz","p":6}],` + b10Meta(`[1]`) + `,"v":4}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		{
			name: "rename: original path before move",
			raw:  b10RenamedRaw, path: "main.tex", from: 1, to: 5,
			want: `{"initialContent":"fooHello world, this is a test","updates":[
				{"op":[{"i":"bar","p":3}],` + b10Meta(`[1]`) + `,"v":2},
				{"op":[{"i":"baz","p":6}],` + b10Meta(`[1]`) + `,"v":4}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		{
			name: "rename: new path after move",
			raw:  b10RenamedRaw, path: "moved.tex", from: 2, to: 5,
			want: `{"initialContent":"fooHello world, this is a test","updates":[
				{"op":[{"i":"bar","p":3}],` + b10Meta(`[1]`) + `,"v":2},
				{"op":[{"i":"baz","p":6}],` + b10Meta(`[1]`) + `,"v":4}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		{
			name: "rename: tracks multiple renames",
			raw:  b10RenamedRaw, path: "moved_again.tex", from: 4, to: 5,
			want: `{"initialContent":"foobarHello world, this is a test","updates":[
				{"op":[{"i":"baz","p":6}],` + b10Meta(`[1]`) + `,"v":4}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		{
			name: "rename: moved file errors",
			raw:  b10RenamedRaw, path: "main.tex", from: 4, to: 5,
			wantErr: "pathname 'main.tex' not found in range",
			blobs:   map[string]string{b10MainHash: b10MainText},
		},
		// ---- when the file is deleted during the chunk ---------------------
		{
			name: "deleted: updates up to delete",
			raw:  b10DeletedRaw, path: "main.tex", from: 0, to: 3,
			want: `{"initialContent":"Hello world, this is a test","updates":[
				{"op":[{"i":"foo","p":0}],` + b10Meta(`[1]`) + `,"v":0}]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		{
			name: "deleted: fromVersion at delete",
			raw:  b10DeletedRaw, path: "main.tex", from: 1, to: 3,
			want:  `{"initialContent":"fooHello world, this is a test","updates":[]}`,
			blobs: map[string]string{b10MainHash: b10MainText},
		},
		{
			name: "deleted: after delete errors",
			raw:  b10DeletedRaw, path: "main.tex", from: 2, to: 3,
			wantErr: "pathname 'main.tex' not found in range",
			blobs:   map[string]string{b10MainHash: b10MainText},
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

var _ = errors.New

// shared vendor fixtures (raw chunk wrappers) --------------------------------
const (
	b10CreatedRaw = `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42}}},"changes":[
		{"operations":[{"pathname":"main.tex","textOperation":[6,"bar"]}],"timestamp":"` + b10ISO + `","authors":[1]},
		{"operations":[{"pathname":"new.tex","file":{"hash":"some_hash","stringLength":10}}],"timestamp":"` + b10ISO + `","authors":[1]},
		{"operations":[{"pathname":"new.tex","textOperation":[6,"bar"]}],"timestamp":"` + b10ISO + `","authors":[1]},
		{"operations":[{"pathname":"new.tex","textOperation":[9,"baz"]}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[{"id":1,"email":"james.allen@overleaf.com","name":"James Allen"}]}`

	b10RenamedRaw = `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42}}},"changes":[
		{"operations":[{"pathname":"main.tex","textOperation":["foo"]}],"timestamp":"` + b10ISO + `","authors":[1]},
		{"operations":[{"pathname":"main.tex","newPathname":"moved.tex"}],"timestamp":"` + b10ISO + `","authors":[1]},
		{"operations":[{"pathname":"moved.tex","textOperation":[3,"bar"]}],"timestamp":"` + b10ISO + `","authors":[1]},
		{"operations":[{"pathname":"moved.tex","newPathname":"moved_again.tex"}],"timestamp":"` + b10ISO + `","authors":[1]},
		{"operations":[{"pathname":"moved_again.tex","textOperation":[6,"baz"]}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[{"id":1,"email":"james.allen@overleaf.com","name":"James Allen"}]}`

	b10DeletedRaw = `{"chunk":{"startVersion":0,"history":{"snapshot":{"files":{"main.tex":{"hash":"some_hash","stringLength":42},"other.tex":{"hash":"some_hash","stringLength":42}}},"changes":[
		{"operations":[{"pathname":"main.tex","textOperation":["foo"]}],"timestamp":"` + b10ISO + `","authors":[1]},
		{"operations":[{"pathname":"main.tex","newPathname":""}],"timestamp":"` + b10ISO + `","authors":[1]},
		{"operations":[{"pathname":"other.tex","textOperation":["foo"]}],"timestamp":"` + b10ISO + `","authors":[1]}]}},"authors":[{"id":1,"email":"james.allen@overleaf.com","name":"James Allen"}]}`
)

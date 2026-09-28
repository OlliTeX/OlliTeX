// C14 oracle — SummarizedUpdatesManager S0–S7.
package summarizedupdatesmanager

import (
	"context"
	"errors"
	"testing"
)

var cctx = context.Background()

func upd(v int, pathnames []string, ops []map[string]any, startTS, endTS float64, origin ...map[string]any) map[string]any {
	meta := map[string]any{"users": []any{map[string]any{"id": "u1"}}, "start_ts": startTS, "end_ts": endTS}
	if len(origin) > 0 {
		meta["origin"] = origin[0]
	}
	return map[string]any{"v": v, "meta": meta, "pathnames": pathnames, "project_ops": ops}
}

func chunkWith(start int, updates ...map[string]any) map[string]any {
	return map[string]any{"chunk": map[string]any{"startVersion": start, "history": map[string]any{"changes": []any{}}}}
}

func newDeps(updates []map[string]any) *Deps {
	return &Deps{
		ProcessUpdates:          func(ctx context.Context, p string) error { return nil },
		GetLabels:               func(ctx context.Context, p string) ([]map[string]any, error) { return nil, nil },
		GetHistoryId:            func(ctx context.Context, p string) (string, error) { return "h", nil },
		ShouldUseProjectHistory: func(ctx context.Context, p string) (bool, error) { return true, nil },
		MostRecentChunk: func(ctx context.Context, p, h string) (map[string]any, error) {
			return chunkWith(1), nil
		},
		ChunkAtVersion: func(ctx context.Context, p, h string, v int) (map[string]any, error) {
			if v <= 1 {
				return chunkWith(0), nil // an earlier chunk starts at 0 (startVersions descend to 0)
			}
			return chunkWith(v - 2), nil
		},
		ConvertToSummarizedUpdates: func(chunk map[string]any) ([]map[string]any, error) {
			// simulate a chunk of 3 updates (ascending v) — the manager reverses
			return []map[string]any{
				upd(1, []string{"a.tex"}, nil, 1000, 2000),
				upd(2, []string{"a.tex"}, nil, 2100, 3000),
				upd(3, []string{"a.tex"}, nil, 3100, 4000),
			}, nil
		},
	}
}

func TestC14_BasicMerging(t *testing.T) {
	d := newDeps(nil)
	got, next, err := d.GetSummarizedProjectUpdates(cctx, "p", Options{})
	if err != nil {
		t.Fatal(err)
	}
	// 3 consecutive updates within 5 min, same pathname, no labels → ONE summary
	if len(got) != 1 {
		t.Fatalf("summary count: %d", len(got))
	}
	s := got[0]
	if s["fromV"] != 1 || s["toV"] != 4 {
		t.Fatalf("fromV=%v toV=%v (want 1..4)", s["fromV"], s["toV"])
	}
	meta := s["meta"].(map[string]any)
	// ts values arrive as JSON numbers (float64 in Go) — compare numerically
	if floatAny(meta["start_ts"]) != 1000 || floatAny(meta["end_ts"]) != 4000 {
		t.Fatalf("ts: %v %v", meta["start_ts"], meta["end_ts"])
	}
	// vendor: after the loop the request version reached startVersion 0 →
	// `0 > 0 ? 0 : undefined` → undefined (the port: 0)
	if next != 0 {
		t.Fatalf("nextVersion: %d (want 0/undefined after the walk reaches v0)", next)
	}
}

func TestC14_NoHistoryEmpty(t *testing.T) {
	d := newDeps(nil)
	d.ShouldUseProjectHistory = func(ctx context.Context, p string) (bool, error) { return false, nil }
	got, next, err := d.GetSummarizedProjectUpdates(cctx, "p", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || next != 0 {
		t.Fatalf("vendor: no history → [] and no next (%d, %d)", len(got), next)
	}
}

func TestC14_BeforeFiltersAndNext(t *testing.T) {
	d := newDeps(nil)
	before := 3
	got, next, err := d.GetSummarizedProjectUpdates(cctx, "p", Options{Before: &before})
	if err != nil {
		t.Fatal(err)
	}
	// v=3 discarded (u.v < before only) → v1, v2 merged
	if len(got) != 1 {
		t.Fatalf("count: %d", len(got))
	}
	if got[0]["fromV"] != 1 || got[0]["toV"] != 3 {
		t.Fatalf("range %v..%v", got[0]["fromV"], got[0]["toV"])
	}
	if next != 0 {
		t.Fatalf("next: %d", next)
	}
}

func TestC14_MinCountCapsLoop(t *testing.T) {
	d := newDeps(nil)
	d.MostRecentChunk = func(ctx context.Context, p, h string) (map[string]any, error) { return chunkWith(0), nil }
	n := 1
	d.ConvertToSummarizedUpdates = func(chunk map[string]any) ([]map[string]any, error) {
		n++
		return []map[string]any{upd(9, []string{"x"}, nil, 1, 1)}, nil
	}
	got, _, err := d.GetSummarizedProjectUpdates(cctx, "p", Options{MinCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("min_count cap: %d", len(got))
	}
}

func TestC14_MaxChunkCap(t *testing.T) {
	d := newDeps(nil)
	d.MostRecentChunk = func(ctx context.Context, p, h string) (map[string]any, error) { return chunkWith(1), nil }
	d.ChunkAtVersion = func(ctx context.Context, p, h string, v int) (map[string]any, error) {
		return chunkWith(v + 1), nil // startVersion never reaches 0 → loop runs to the cap
	}
	n := 1
	d.ConvertToSummarizedUpdates = func(chunk map[string]any) ([]map[string]any, error) {
		// a NEW summary each time (huge time gap) → summarized.length grows
		v := n * 10
		n++
		return []map[string]any{upd(v, []string{"x"}, nil, float64(v)*1000, float64(v)*1000+10)}, nil
	}
	_, _, err := d.GetSummarizedProjectUpdates(cctx, "p", Options{MinCount: 100})
	if err != nil {
		t.Fatal(err)
	}
	if n-1 != MaxChunkRequests {
		t.Fatalf("chunk requests: %d (want cap %d)", n-1, MaxChunkRequests)
	}
}

func TestC14_OriginSplitAndMerge(t *testing.T) {
	rest := map[string]any{"kind": "file-restore", "timestamp": "2025-01-01T00:00:00Z", "path": "a.tex"}
	d := newDeps(nil)
	d.MostRecentChunk = func(ctx context.Context, p, h string) (map[string]any, error) { return chunkWith(1), nil }
	d.ConvertToSummarizedUpdates = func(chunk map[string]any) ([]map[string]any, error) {
		if toIntOf(chunk) == 0 {
			return []map[string]any{}, nil
		}
		return []map[string]any{
			upd(1, []string{"a.tex"}, nil, 1000, 2000, rest),
			upd(2, []string{"a.tex"}, nil, 2100, 3000, map[string]any{"kind": "file-restore", "timestamp": "OTHER", "path": "a.tex"}),
		}, nil
	}
	got, _, err := d.GetSummarizedProjectUpdates(cctx, "p", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("restore timestamp mismatch must split: %d", len(got))
	}
}

func TestC14_TimeGapSplit(t *testing.T) {
	d := newDeps(nil)
	d.MostRecentChunk = func(ctx context.Context, p, h string) (map[string]any, error) { return chunkWith(1), nil }
	d.ConvertToSummarizedUpdates = func(chunk map[string]any) ([]map[string]any, error) {
		if toIntOf(chunk) == 0 {
			return []map[string]any{}, nil
		}
		return []map[string]any{
			upd(1, []string{"a.tex"}, nil, 1000, 2000),
			upd(2, []string{"a.tex"}, nil, 1000+300001, 1000+400001), // gap >= 5 min from the previous end_ts
		}, nil
	}
	got, _, err := d.GetSummarizedProjectUpdates(cctx, "p", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("5-min gap must split: %d", len(got))
	}
}

func TestC14_TextFileMixSplitUnlessResync(t *testing.T) {
	d := newDeps(nil)
	d.MostRecentChunk = func(ctx context.Context, p, h string) (map[string]any, error) { return chunkWith(1), nil }
	ops := []map[string]any{{"add": map[string]any{"pathname": "fig.png"}}}
	d.ConvertToSummarizedUpdates = func(chunk map[string]any) ([]map[string]any, error) {
		if toIntOf(chunk) == 0 {
			return []map[string]any{}, nil
		}
		return []map[string]any{
			upd(1, []string{"a.tex"}, nil, 1000, 2000),
			upd(2, nil, ops, 2100, 3000),
		}, nil
	}
	got, _, err := d.GetSummarizedProjectUpdates(cctx, "p", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("text+file mix must split: %d", len(got))
	}

	// history-resync origin: both merge
	d2 := newDeps(nil)
	d2.MostRecentChunk = func(ctx context.Context, p, h string) (map[string]any, error) { return chunkWith(1), nil }
	resync := map[string]any{"kind": "history-resync"}
	d2.ConvertToSummarizedUpdates = func(chunk map[string]any) ([]map[string]any, error) {
		return []map[string]any{
			upd(1, []string{"a.tex"}, nil, 1000, 2000, resync),
			upd(2, nil, ops, 2100, 3000, resync),
		}, nil
	}
	got2, _, err := d2.GetSummarizedProjectUpdates(cctx, "p", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got2) != 1 {
		t.Fatalf("resync text+file must merge: %d", len(got2))
	}
}

func TestC14_LabelsSplit(t *testing.T) {
	d := newDeps(nil)
	d.GetLabels = func(ctx context.Context, p string) ([]map[string]any, error) {
		return []map[string]any{{"version": 2, "comment": "milestone"}}, nil
	}
	d.MostRecentChunk = func(ctx context.Context, p, h string) (map[string]any, error) { return chunkWith(1), nil }
	d.ConvertToSummarizedUpdates = func(chunk map[string]any) ([]map[string]any, error) {
		if toIntOf(chunk) == 0 {
			return []map[string]any{}, nil
		}
		return []map[string]any{
			upd(1, []string{"a.tex"}, nil, 1000, 2000),
			upd(2, []string{"a.tex"}, nil, 2100, 3000),
		}, nil
	}
	got, _, err := d.GetSummarizedProjectUpdates(cctx, "p", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("labels at v+1=3 must split: %d", len(got))
	}
	// the label at version 2 applies to the update that CREATED version 2 (v=1)
	found := false
	for _, e := range got {
		if len(e["labels"].([]map[string]any)) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the v=1 summary must carry the version-2 label: %#v", got)
	}
	// the unlabelled (v=2) summary must NOT carry it
	if len(got[0]["labels"].([]map[string]any)) != 0 {
		t.Fatalf("v=2 summary must be label-free: %#v", got[0])
	}
}

func TestC14_UserUnionUniqueBy(t *testing.T) {
	a := upd(1, []string{"a"}, nil, 1, 2)
	s := map[string]any{
		"fromV":       2,
		"toV":         3,
		"meta":        map[string]any{"users": []any{map[string]any{"id": "u1"}}, "start_ts": 5, "end_ts": 6},
		"pathnames":   []string{"b"},
		"project_ops": []map[string]any{},
	}
	mergeUpdate(a, &s)
	users := s["meta"].(map[string]any)["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("uniqBy user.id: %#v", users)
	}
	if s["fromV"] != 1 || s["toV"] != 3 {
		t.Fatalf("range: %v..%v", s["fromV"], s["toV"])
	}
	mmeta := s["meta"].(map[string]any)
	if floatAny(mmeta["start_ts"]) != 1 || floatAny(mmeta["end_ts"]) != 6 {
		t.Fatalf("ts merge: %v %v", mmeta["start_ts"], mmeta["end_ts"])
	}
}

func TestC14_AddOpRemovesEditPathname(t *testing.T) {
	// the adding update is a PURE add (it has no edit pathnames of its own)
	u := upd(5, nil, nil, 1, 2)
	u["project_ops"] = []map[string]any{{"add": map[string]any{"pathname": "fig.png"}}}
	s := map[string]any{
		"fromV": 6, "toV": 7,
		"meta":        map[string]any{"users": []any{}, "start_ts": 3, "end_ts": 4},
		"pathnames":   []string{"fig.png", "a.tex"},
		"project_ops": []map[string]any{},
	}
	mergeUpdate(u, &s)
	paths := s["pathnames"].([]string)
	if len(paths) != 1 || paths[0] != "a.tex" {
		t.Fatalf("add must remove its pathname: %v", paths)
	}
	if len(s["project_ops"].([]map[string]any)) != 1 {
		t.Fatalf("ops: %#v", s["project_ops"])
	}
}

func TestC14_EmptyUpdateAdvancesVersionOnly(t *testing.T) {
	d := newDeps(nil)
	d.MostRecentChunk = func(ctx context.Context, p, h string) (map[string]any, error) { return chunkWith(1), nil }
	d.ConvertToSummarizedUpdates = func(chunk map[string]any) ([]map[string]any, error) {
		return []map[string]any{
			upd(1, []string{"a.tex"}, nil, 1000, 2000),
			map[string]any{"v": 2, "meta": map[string]any{"users": []any{}, "start_ts": 2100, "end_ts": 3000}, "pathnames": []any{}, "project_ops": []any{}},
			upd(3, []string{"a.tex"}, nil, 3100, 4000),
		}, nil
	}
	got, _, err := d.GetSummarizedProjectUpdates(cctx, "p", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("empty update must not make its own summary: %d", len(got))
	}
	if got[0]["toV"] != 4 {
		t.Fatalf("toV must still advance past empty updates: %v", got[0]["toV"])
	}
}

func TestC14_StoreErrors(t *testing.T) {
	boom := errors.New("down")
	d := newDeps(nil)
	d.ProcessUpdates = func(ctx context.Context, p string) error { return boom }
	if _, _, err := d.GetSummarizedProjectUpdates(cctx, "p", Options{}); err != boom {
		t.Fatal("want process error")
	}
	d2 := newDeps(nil)
	d2.GetHistoryId = func(ctx context.Context, p string) (string, error) { return "", boom }
	if _, _, err := d2.GetSummarizedProjectUpdates(cctx, "p", Options{}); err != boom {
		t.Fatal("want hid error")
	}
	d3 := newDeps(nil)
	d3.MostRecentChunk = func(ctx context.Context, p, h string) (map[string]any, error) { return nil, boom }
	if _, _, err := d3.GetSummarizedProjectUpdates(cctx, "p", Options{}); err != boom {
		t.Fatal("want chunk error")
	}
	d4 := newDeps(nil)
	d4.ConvertToSummarizedUpdates = func(chunk map[string]any) ([]map[string]any, error) { return nil, boom }
	if _, _, err := d4.GetSummarizedProjectUpdates(cctx, "p", Options{}); err != boom {
		t.Fatal("want convert error")
	}
}

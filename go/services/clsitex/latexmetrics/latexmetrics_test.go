package latexmetrics

import (
	"encoding/json"
	"os"
	"testing"
)

type caseIn struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

func loadJSON(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("load oracle %s: %v", name, err)
	}
	return data
}

func deepDiff(got, want any, path string) []string {
	var diffs []string
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return []string{path + " want map got " + jlabel(got)}
		}
		for k, wk := range w {
			gk, exists := g[k]
			if !exists {
				diffs = append(diffs, path+k+" missing in go")
				continue
			}
			diffs = append(diffs, deepDiff(gk, wk, path+k+".")...)
		}
		for k := range g {
			if _, exists := w[k]; !exists {
				diffs = append(diffs, path+k+" extra in go")
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok {
			return []string{path + " want array got " + jlabel(got)}
		}
		if len(g) != len(w) {
			return []string{path + " len go=" + itoa(len(g)) + " want=" + itoa(len(w))}
		}
		for i := range w {
			diffs = append(diffs, deepDiff(g[i], w[i], path+itoa(i)+".")...)
		}
	case float64:
		gf, gok := got.(float64)
		if !gok || gf != w {
			diffs = append(diffs, path+" got "+jlabel(got)+" want "+jlabel(w))
		}
	case string:
		gs, gok := got.(string)
		if !gok || gs != w {
			diffs = append(diffs, path+" got "+jlabel(got)+" want "+jlabel(w))
		}
	case bool:
		gb, gok := got.(bool)
		if !gok || gb != w {
			diffs = append(diffs, path+" got "+jlabel(got)+" want "+jlabel(w))
		}
	case nil:
	}
	return diffs
}

func jlabel(v any) string { s, _ := json.Marshal(v); return string(s) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + byte(n%10))
		n /= 10
	}
	if neg {
		return "-" + string(b[i:])
	}
	return string(b[i:])
}

func joinAll(ss []string, sep string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}

func TestMatchesNodeOracleStdout(t *testing.T) {
	var cs []caseIn
	if err := json.Unmarshal(loadJSON(t, "stdcases.json"), &cs); err != nil {
		t.Fatalf("stdcases: %v", err)
	}
	var want []map[string]any
	if err := json.Unmarshal(loadJSON(t, "oracle_lmj_stdout.json"), &want); err != nil {
		t.Fatalf("oracle: %v", err)
	}
	if len(cs) != len(want) {
		t.Fatalf("len cases=%d want oracle=%d", len(cs), len(want))
	}
	for i, c := range cs {
		stats := map[string]any{}
		EnableLatexMkMetrics(stats)
		AddLatexMkMetrics(c.Stdout, c.Stderr, stats, map[string]any{})
		detail := LatexMk(stats)
		got := jsonRoundTrip(t, detail)
		// oracle dump is wrapped {detail, top, timings}
		wantv, ok := want[i]["detail"].(map[string]any)
		if !ok {
			t.Fatalf("oracle %d has no detail", i)
		}
		diffs := deepDiff(got, wantv, "stdout."+itoa(i))
		if len(diffs) > 0 {
			gjson, _ := json.Marshal(got)
			t.Fatalf("stdout oracle case %d mismatch:\n  got:   %s\n  want: %s\n%s",
				i, string(gjson), wantRaw(want[i]), joinAll(diffs, "\n"))
		}
	}
}

func TestMatchesNodeOracleFdb(t *testing.T) {
	var fdb []string
	if err := json.Unmarshal(loadJSON(t, "cases_fdb.json"), &fdb); err != nil {
		t.Fatalf("cases_fdb: %v", err)
	}
	var want []map[string]any
	if err := json.Unmarshal(loadJSON(t, "oracle_fdb.json"), &want); err != nil {
		t.Fatalf("oracle: %v", err)
	}
	if len(fdb) != len(want) {
		t.Fatalf("len fdb=%d want oracle=%d", len(fdb), len(want))
	}
	for i, f := range fdb {
		stats := map[string]any{}
		EnableLatexMkMetrics(stats)
		AddLatexFdbMetrics(f, stats)
		detail := LatexMk(stats)
		got := jsonRoundTrip(t, detail)
		diffs := deepDiff(got, want[i], "fdb."+itoa(i))
		if len(diffs) > 0 {
			gjson, _ := json.Marshal(got)
			t.Fatalf("fdb oracle case %d mismatch:\n  got:   %s\n  want: %s\n%s",
				i, string(gjson), wantRaw(want[i]), joinAll(diffs, "\n"))
		}
	}
}

func wantRaw(v any) string {
	s, _ := json.Marshal(v)
	return string(s)
}

func jsonRoundTrip(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	var got any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	return got
}

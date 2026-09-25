package sharejstptwo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGolden(t *testing.T) {
	for _, r := range goldenRows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			wantErr := ""
			if strings.HasPrefix(r.expected, "ERR:") {
				wantErr = strings.TrimPrefix(r.expected, "ERR:")
			}

			var got any
			var gotErr error
			arr := decArgs(r.input)
			switch r.kind {
			case "create":
				got = docMap(Create())
			case "deserialize":
				res, _ := Deserialize(anySlice(arr[0]))
				got = docMap(res)
			case "serialize":
				if arr[0] == nil {
					_, err := Serialize(nil)
					if err == nil || err.Error() != wantErr {
						t.Fatalf("got err %v, want %q", err, wantErr)
					}
					return
				}
				data := anySlice(arr[0])
				d, _ := Deserialize(data)
				got, _ = Serialize(&d)
			case "apply":
				got, gotErr = applyRow(arr)
			case "normalize":
				got = opList(Normalize(anySlice(arr[0])))
			case "appendop":
				op := anySlice(arr[0])
				AppendOp(&op, arr[1])
				got = opList(op)
			case "transform":
				got, gotErr = transformRow(arr)
			case "prune":
				got, gotErr = pruneRow(arr)
			case "compose":
				got, gotErr = composeRow(arr)
			default:
				t.Fatalf("unknown kind %s", r.kind)
			}

			if gotErr != nil {
				if gotErr.Error() != wantErr {
					t.Fatalf("error = %q, want %q", gotErr.Error(), wantErr)
				}
				return
			}
			if wantErr == "" && r.expected == "null" {
				if got != nil {
					t.Fatalf("got %v, want nil", got)
				}
				return
			}
			if !isErrRow(r.expected) {
				var want any
				_ = json.Unmarshal([]byte(r.expected), &want)
				if !deepEq(got, want) {
					gb, _ := json.Marshal(got)
					t.Fatalf("got %s, want %s", gb, r.expected)
				}
			}
		})
	}
}

func isErrRow(s string) bool {
	return strings.HasPrefix(s, "ERR:")
}

func decArgs(s string) []any {
	if s == "null" {
		return []any{nil}
	}
	var raw []any
	_ = json.Unmarshal([]byte(s), &raw)
	out := make([]any, len(raw))
	for i, v := range raw {
		out[i] = normNum(v)
	}
	return out
}

func normNum(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = normNum(val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = normNum(t[i])
		}
		return t
	case float64:
		if t == float64(int(t)) {
			return int(t)
		}
		return t
	default:
		return v
	}
}

func anySlice(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}

func applyRow(arr []any) (any, error) {
	var d *Doc
	if arr[0] == nil {
		d = nil
	} else {
		doc, _ := Deserialize(anySlice(arr[0]))
		d = &doc
	}
	res, err := Apply(d, arr[1])
	if err != nil {
		return nil, err
	}
	return docMap(res), nil
}

func transformRow(arr []any) (any, error) {
	op := anySlice(arr[0])
	other := anySlice(arr[1])
	side := arr[2].(string)
	res, err := Transform(op, other, side)
	if err != nil {
		return nil, err
	}
	return opList(res), nil
}

func pruneRow(arr []any) (any, error) {
	res, err := Prune(anySlice(arr[0]), anySlice(arr[1]))
	if err != nil {
		return nil, err
	}
	return opList(res), nil
}

func composeRow(arr []any) (any, error) {
	var op1 []any
	if arr[0] != nil {
		op1 = anySlice(arr[0])
	}
	op2 := anySlice(arr[1])
	res, err := Compose(op1, op2)
	if err != nil {
		return nil, err
	}
	return opList(res), nil
}

func opList(op []any) []any {
	return op
}

func docMap(d Doc) any {
	return map[string]any{
		"charLength":    d.CharLength,
		"totalLength":   d.TotalLength,
		"positionCache": []any{},
		"data":          d.Data,
	}
}

func deepEq(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if !deepEq(v, y[k]) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !deepEq(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	if n, ok := b.(float64); ok {
		b = int(n)
	}
	return a == b
}

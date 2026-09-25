package sharejstxtcomp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestGolden(t *testing.T) {
	for _, row := range goldenRows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			var got any

			var in []any
			_ = json.Unmarshal([]byte(row.input), &in)
			isErr := strings.HasPrefix(row.expected, "ERR:")
			wantErr := ""
			if isErr {
				wantErr = strings.TrimPrefix(row.expected, "ERR:")
			}

			var err error
			switch row.kind {
			case "create":
				got = Create()
			case "normalize":
				got = Normalize(norm(in[0]))
			case "apply":
				got, err = applyFrom(in)
			case "transform":
				got, err = Transform(norm(in[0]), norm(in[1]), in[2].(string))
			case "compose":
				got, err = Compose(norm(in[0]), norm(in[1]))
			case "invert":
				got = Invert(norm(in[0]))
			default:
				t.Fatalf("unknown kind %q", row.kind)
			}

			if err != nil {
				if wantErr == "" {
					t.Fatalf("kind=%s name=%s: unexpected error: %v", row.kind, row.name, err)
				}
				if err.Error() != wantErr {
					t.Fatalf("kind=%s name=%s: error = %q, want %q", row.kind, row.name, err.Error(), wantErr)
				}
				return
			}
			if isErr {
				t.Fatalf("kind=%s name=%s: got value %+v, want error %q", row.kind, row.name, got, wantErr)
			}

			if s, ok := got.(string); ok {
				var wantStr any
				_ = json.Unmarshal([]byte(row.expected), &wantStr)
				if s != wantStr.(string) {
					t.Fatalf("kind=%s name=%s: got %q, want %q", row.kind, row.name, s, wantStr)
				}
				return
			}

			gb, _ := json.Marshal(got)
			if string(gb) != row.expected {
				t.Fatalf("kind=%s name=%s: got %s, want %s", row.kind, row.name, gb, row.expected)
			}
		})
	}
}

func norm(v any) []any {
	if v == nil {
		return nil
	}
	return normAny(v).([]any)
}

func normAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range t {
			out[k] = normAny(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = normAny(item)
		}
		return out
	case float64:
		return int(t)
	default:
		return v
	}
}

func applyFrom(in []any) (any, error) {
	if len(in) < 2 {
		return nil, errors.New("bad apply row")
	}
	// vendored apply accepts any op value; a non-array op fails checkOp
	// before any component is inspected.
	opRaw := in[1]
	if _, ok := opRaw.([]any); !ok {
		return "", CheckOpInput(nil)
	}
	return Apply(in[0], norm(opRaw))
}

package toolkit

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// hubUserEmail must read the legacy `emails` fallback through EVERY driver
// shape (the Q/C bug class: bson.A / bson.D are nominal types the old
// `.([]any)`/`. (bson.M)` chain silently dropped).
func TestHubUserEmailDriverShapes(t *testing.T) {
	cases := map[string]struct {
		d    bson.M
		want string
	}{
		"plain email":            {bson.M{"email": "a@b.c"}, "a@b.c"},
		"emails bson.A/bson.D":   {bson.M{"emails": bson.A{bson.D{{"emailAddress", "u@v.w"}}}}, "u@v.w"},
		"emails []any/map":       {bson.M{"emails": []any{map[string]any{"emailAddress": "x@y.z"}}}, "x@y.z"},
		"emails string fallback": {bson.M{"emails": bson.A{"s@t.u"}}, "s@t.u"},
		"no email at all":        {bson.M{"name": "ghost"}, ""},
	}
	for name, c := range cases {
		if got := hubUserEmail(c.d); got != c.want {
			t.Errorf("%s: got %q want %q", name, got, c.want)
		}
	}
}

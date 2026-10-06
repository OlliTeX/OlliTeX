package sso

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestUserIDHex(t *testing.T) {
	oid := bson.ObjectID{0x6a, 0x46, 0x60, 0x03, 0x2d, 0xfd, 0x6a, 0x03, 0xd8, 0x16, 0xaa, 0x29}
	hex := oid.Hex()
	if hex != "6a4660032dfd6a03d816aa29" {
		t.Fatalf("fixture: %s", hex)
	}
	cases := []struct {
		in   map[string]any
		want string
	}{
		{map[string]any{"_id": "6a4660032dfd6a03d816aa29"}, "6a4660032dfd6a03d816aa29"},
		{map[string]any{"_id": oid}, hex},
		{map[string]any{}, ""},
		{nil, ""},
		{map[string]any{"_id": nil}, ""},
	}
	for i, c := range cases {
		if got := userIDHex(c.in); got != c.want {
			t.Fatalf("case %d: got %q want %q", i, got, c.want)
		}
	}
}

package assert

import (
	"fmt"
	"testing"
)

func TestProjectID(t *testing.T) {
	// Valid Mongo and Postgres ids.
	for _, id := range []string{"507f1f77bcf86cd799439011", "123456789", "1"} {
		if err := ProjectID(id, "should be a project id"); err != nil {
			t.Errorf("ProjectID(%q) = %v, want nil", id, err)
		}
	}
	// Invalid values: each must return *Assertion with message and arg.
	for _, id := range []string{"invalid-id", "12345x", "0123456", "", "0", "0123456789012345"} {
		err := ProjectID(id, "should be a project id")
		if err == nil {
			t.Errorf("ProjectID(%q) = nil, want assertion", id)
			continue
		}
		if err.Error() != "should be a project id" {
			t.Errorf("ProjectID(%q).message = %q", id, err.Error())
		}
		info := err.Info()
		if info["arg"] != id {
			t.Errorf("ProjectID(%q).info.arg = %v, want %q", id, info["arg"], id)
		}
	}
}

func TestChunkID(t *testing.T) {
	if err := ChunkID("507f1f77bcf86cd799439011", "x"); err != nil {
		t.Errorf("ChunkID(mongo) = %v", err)
	}
	if err := ChunkID("123456789", "x"); err != nil {
		t.Errorf("ChunkID(postgres) = %v", err)
	}
	if err := ChunkID("12345", "x"); err != nil {
		t.Errorf("ChunkID(\"12345\")= %v, want nil (5-digit string is a valid postgres id)", err)
	}
	// The node test chunkId(12345) failed because the number, not a string,
	// failed check-types' type check — a string "12345" is valid here.
	if err := ChunkID("invalid-id", "x"); err == nil {
		t.Error("ChunkID(invalid) = nil, want assertion")
	}
}

func TestMongoID(t *testing.T) {
	if err := MongoID("507f1f77bcf86cd799439011", "x"); err != nil {
		t.Errorf("MongoID(valid) = %v", err)
	}
	for _, id := range []string{"invalid-id", "12345", "507f1f77bcf86cd79943901", "507f1f77bcf86cd7994390111", "507F1F77BCF86CD799439011"} {
		if err := MongoID(id, "x"); err == nil {
			t.Errorf("MongoID(%q) = nil, want assertion", id)
		}
	}
}

func TestPostgresID(t *testing.T) {
	for _, id := range []string{"123456789", "1"} {
		if err := PostgresID(id, "x"); err != nil {
			t.Errorf("PostgresID(%q) = %v", id, err)
		}
	}
	for _, id := range []string{"invalid-id", "0123456", "12345678901", "0"} {
		if err := PostgresID(id, "x"); err == nil {
			t.Errorf("PostgresID(%q) = nil, want assertion", id)
		}
	}
}

func TestBlobHash(t *testing.T) {
	if err := BlobHash("e69de29bb2d1d6434b8b29ae775ad8c2e48c5391", "x"); err != nil {
		t.Errorf("BlobHash(valid) = %v", err)
	}
	for _, h := range []string{"invalid-hash", "123", "", "E69DE29BB2D1D6434B8B29AE775AD8C2E48C5391", "e69de29bb2d1d6434b8b29ae775ad8c2e48c53"} {
		if err := BlobHash(h, "x"); err == nil {
			t.Errorf("BlobHash(%q) = nil, want assertion", h)
		}
	}
}

func TestIsPostgres(t *testing.T) {
	ok, n := IsPostgres("12345")
	if !ok || n != 12345 {
		t.Errorf("IsPostgres(12345) = %v %d", ok, n)
	}
	if ok, _ := IsPostgres("abc"); ok {
		t.Error("IsPostgres(abc) unexpected true")
	}
}

func TestAssertionErrorString(t *testing.T) {
	err := ProjectID("bad", "should be a project id")
	if err == nil {
		t.Fatal("want assertion")
	}
	if got := fmt.Sprintf("%v", err); got != "should be a project id" {
		t.Errorf("error string = %q", got)
	}
}

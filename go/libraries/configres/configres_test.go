package configres

import (
	"os"
	"path/filepath"
	"testing"

	"ollitex/go/libraries/configstore"
)

// newTempStore — a fresh plaintext store in a temp dir (explicit nil key:
// no CONFIG_DB_ENCRYPTION_KEY dependency for this suite).
func newTempStore(t *testing.T) (*configstore.ConfigStore, string) {
	t.Helper()
	dbFile := filepath.Join(t.TempDir(), "configdb.sqlite3")
	s, err := configstore.NewWithKey(dbFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dbFile
}

func set(t *testing.T, s *configstore.ConfigStore, k, v string) {
	t.Helper()
	if err := s.Set(k, v, "test"); err != nil {
		t.Fatal(err)
	}
}

// clearEnv removes the two boot vars a test must not inherit.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"CONFIG_DB_PATH", "OVERLEAF_HOME"} {
		if old, ok := os.LookupEnv(k); ok {
			t.Cleanup(func() { os.Setenv(k, old) })
		}
		os.Unsetenv(k)
	}
}

func TestPathContract(t *testing.T) {
	t.Run("explicit path wins", func(t *testing.T) {
		t.Setenv("CONFIG_DB_PATH", "/x/y/db.sqlite3")
		os.Unsetenv("OVERLEAF_HOME")
		if got := Path(); got != "/x/y/db.sqlite3" {
			t.Fatalf("Path() = %q", got)
		}
	})
	t.Run("OVERLEAF_HOME", func(t *testing.T) {
		os.Unsetenv("CONFIG_DB_PATH")
		t.Setenv("OVERLEAF_HOME", "/home/o")
		if got := Path(); got != "/home/o/configdb/configdb.sqlite3" {
			t.Fatalf("Path() = %q", got)
		}
	})
	t.Run("default", func(t *testing.T) {
		clearEnv(t)
		if got := Path(); got != "configdb/configdb.sqlite3" {
			t.Fatalf("Path() = %q", got)
		}
	})
}

func TestOpenAbsentReturnsNilAndCreatesNothing(t *testing.T) {
	clearEnv(t)
	missing := filepath.Join(t.TempDir(), "nope.sqlite3")
	t.Setenv("CONFIG_DB_PATH", missing)
	if s := Open(); s != nil {
		t.Fatal("Open() over an absent file must be nil")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("Open() must not create the file; stat err=%v", err)
	}
}

func TestOpenPresentReads(t *testing.T) {
	s, dbFile := newTempStore(t)
	clearEnv(t)
	t.Setenv("CONFIG_DB_PATH", dbFile)
	set(t, s, "K1", "v1")
	got := Open()
	if got == nil {
		t.Fatal("Open() = nil over an existing store")
	}
	defer got.Close()
	if v, err := got.Get("K1"); err != nil || v != "v1" {
		t.Fatalf("Get(K1) = %q, %v", v, err)
	}
}

func TestIntPrecedence(t *testing.T) {
	// (name, envVal, dbVal, want) — dbVal "-" = key absent, envVal "-" = unset.
	t.Setenv("COLLAB_KEEP_VERSIONS", "") // ensure absent unless a test sets it
	setEnv := func(v string) {
		if v == "-" {
			os.Unsetenv("COLLAB_KEEP_VERSIONS")
		} else {
			t.Setenv("COLLAB_KEEP_VERSIONS", v)
		}
	}
	cases := []struct {
		name, envVal, dbVal string
		want                int
	}{
		{"no store no env → default", "-", "-", 7},
		{"no store env set → env", "42", "-", 42},
		{"no store env garbage → default", "xx", "-", 7},
		{"store val beats no env", "-", "13", 13},
		{"store val beats env", "42", "13", 13},
		{"store garbage falls to env", "42", "junk", 42},
		{"store garbage no env → default", "-", "junk", 7},
		{"explicit zero is a VALUE, not fallback", "-", "0", 0},
		{"store empty string falls to env", "9", "", 9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var st *configstore.ConfigStore
			if c.dbVal != "-" {
				s, _ := newTempStore(t)
				if c.dbVal != "" {
					set(t, s, "COLLAB_KEEP_VERSIONS", c.dbVal)
				}
				st = s
			}
			setEnv(c.envVal)
			if got := Int(st, "COLLAB_KEEP_VERSIONS", "COLLAB_KEEP_VERSIONS", 7); got != c.want {
				t.Fatalf("Int = %d, want %d", got, c.want)
			}
		})
	}
}

func TestStringPrecedence(t *testing.T) {
	os.Unsetenv("APP_NAME")
	if got := String(nil, "APP_NAME", "APP_NAME", "OlliTeX"); got != "OlliTeX" {
		t.Fatalf("no tiers → default, got %q", got)
	}
	t.Setenv("APP_NAME", "FromEnv")
	if got := String(nil, "APP_NAME", "APP_NAME", "OlliTeX"); got != "FromEnv" {
		t.Fatalf("env tier, got %q", got)
	}
	s, _ := newTempStore(t)
	set(t, s, "APP_NAME", "FromDB")
	t.Setenv("APP_NAME", "FromEnv")
	if got := String(s, "APP_NAME", "APP_NAME", "OlliTeX"); got != "FromDB" {
		t.Fatalf("db tier must win, got %q", got)
	}
	// DB empty string falls through to env.
	if err := s.Set("APP_NAME", "", "test"); err != nil {
		t.Fatal(err)
	}
	if got := String(s, "APP_NAME", "APP_NAME", "OlliTeX"); got != "FromEnv" {
		t.Fatalf("empty db value must fall to env, got %q", got)
	}
}

func TestBoolPrecedence(t *testing.T) {
	os.Unsetenv("OVERLEAF_SITE_OPEN")
	if got := Bool(nil, "OVERLEAF_SITE_OPEN", "OVERLEAF_SITE_OPEN", false); got {
		t.Fatal("default must apply")
	}
	t.Setenv("OVERLEAF_SITE_OPEN", "true")
	if got := Bool(nil, "OVERLEAF_SITE_OPEN", "OVERLEAF_SITE_OPEN", false); !got {
		t.Fatal("env must apply")
	}
	s, _ := newTempStore(t)
	set(t, s, "OVERLEAF_SITE_OPEN", "false")
	if got := Bool(s, "OVERLEAF_SITE_OPEN", "OVERLEAF_SITE_OPEN", true); got {
		t.Fatal("db must win over env/default")
	}
}

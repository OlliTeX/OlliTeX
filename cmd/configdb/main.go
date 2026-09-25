// Command configdb is the `go run` CLI for the overleaf SQLite config DB
// (P7-post item 1: *"a CLI tool as backup"*). It operates on the same
// go/libraries/configstore that the /hub admin endpoints (a later slice) use,
// so an operator can inspect, edit, back up, and restore the runtime config
// without touching the web service:
//
//	go run ./cmd/configdb list
//	go run ./cmd/configdb get SiteTitle
//	go run ./cmd/configdb set SiteTitle "My OlliTeX"
//	go run ./cmd/configdb export
//	go run ./cmd/configdb backup ./backup.json
//	go run ./cmd/configdb restore ./backup.json
//
// It is deliberately dependency-light (stdlib + the configstore library) and
// exits non-zero with a one-line message on any failure.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"ollitex/go/libraries/configschema"
	"ollitex/go/libraries/configstore"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "configdb: "+err.Error())
		os.Exit(1)
	}
}

// dbPath resolves the config DB file: $CONFIG_DB_PATH, else
// $OVERLEAF_HOME/configdb/configdb.sqlite3, else ./configdb/configdb.sqlite3.
func dbPath() string {
	if p := os.Getenv("CONFIG_DB_PATH"); p != "" {
		return p
	}
	if h := os.Getenv("OVERLEAF_HOME"); h != "" {
		return h + "/configdb/configdb.sqlite3"
	}
	return "configdb/configdb.sqlite3"
}

// run parses args and executes one command, writing all normal output to out
// (os.Stdout in main; a buffer in tests) — so the CLI is testable without
// capturing the process stdout.
func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		_ = usage(os.Stderr)
		return errors.New("no command (see: configdb help)")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "-h", "--help":
		return usage(out)
	case "list", "get", "set", "delete", "export", "backup", "restore",
		"import-env", "import-defaults", "defaults", "init", "doctor":
		return dispatch(cmd, rest, out)
	default:
		return fmt.Errorf("unknown command %q (see: configdb help)", cmd)
	}
}

func dispatch(cmd string, args []string, out io.Writer) error {
	s, err := configstore.New(dbPath())
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	switch cmd {
	case "list":
		if len(args) > 0 && (args[0] == "--all" || args[0] == "--registry") {
			return listRegistry(s, out)
		}
		keys, err := s.Keys()
		if err != nil {
			return err
		}
		for _, k := range keys {
			fmt.Fprintln(out, k)
		}
		return nil

	case "get":
		key, reveal := "false", ""
		if len(args) >= 1 && !strings.HasPrefix(args[0], "-") {
			key = args[0]
		}
		if len(args) == 0 || key == "" {
			return fmt.Errorf("get requires KEY")
		}
		for _, a := range args[1:] {
			if a == "--reveal" || a == "--unmask" {
				reveal = "true"
			}
		}
		secret := configschema.IsSecret(key)
		v, err := s.Get(key)
		if err != nil {
			if errors.Is(err, configstore.ErrMissing) {
				return fmt.Errorf("key %q not present", key)
			}
			return err
		}
		if secret && !strings.EqualFold(reveal, "true") {
			if v == "" {
				fmt.Fprintln(out, "<empty>")
			} else {
				fmt.Fprintln(out, strings.Repeat("•", 8))
			}
			return nil
		}
		fmt.Fprintln(out, v)
		return nil

	case "set":
		if len(args) < 2 {
			return fmt.Errorf("set requires KEY VALUE [SOURCE]")
		}
		if err := validateKind(args[0], args[1]); err != nil {
			return err
		}
		src := "cli:configdb"
		if len(args) >= 3 {
			src = args[2]
		}
		if err := s.Set(args[0], args[1], src); err != nil {
			return err
		}
		fmt.Fprintf(out, "set %s\n", args[0])
		return nil

	case "delete":
		if len(args) != 1 {
			return fmt.Errorf("delete requires KEY")
		}
		if err := s.Delete(args[0]); err != nil {
			return err
		}
		fmt.Fprintf(out, "deleted %s\n", args[0])
		return nil

	case "export":
		m, err := s.All()
		if err != nil {
			return err
		}
		buf, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(buf))
		return nil

	case "backup":
		dest := "configdb-backup-" + time.Now().UTC().Format("20060102-150405") + ".json"
		if len(args) >= 1 {
			dest = args[0]
		}
		m, err := s.Dump(dest)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "backed up %d keys to %s\n", len(m), dest)
		return nil

	case "restore":
		if len(args) != 1 {
			return fmt.Errorf("restore requires SRC")
		}
		n, err := s.Restore(args[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "restored %d keys\n", n)
		return nil

	case "import-env":
		if len(args) < 1 {
			return fmt.Errorf("import-env requires FILE (a KEY=VALUE env file)")
		}
		return importEnvFile(s, args[0], out)

	case "import-defaults", "defaults":
		if cmd == "defaults" {
			if len(args) >= 1 {
				b, err := os.ReadFile(args[0])
				if err != nil {
					return err
				}
				fmt.Fprintln(out, string(b))
				return nil
			}
			fmt.Fprintln(out, configschema.EmbeddedDefaults)
			return nil
		}
		return importDefaults(s, out)

	case "init":
		return initStore(s, out)

	case "doctor":
		return doctor(s, out)
	}
	return nil
}

// validateKind type-checks VALUE against the registry kind of KEY (unknown
// keys pass — the store accepts arbitrary keys for forward-compat).
func validateKind(key, value string) error {
	p, ok := configschema.Find(key)
	if !ok {
		return nil
	}
	switch p.Kind {
	case configschema.KBool:
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s is a bool and %q does not parse (use true/false/1/0)", key, value)
		}
	case configschema.KInt:
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("%s is an int and %q does not parse", key, value)
		}
	}
	return nil
}

func listRegistry(s *configstore.ConfigStore, out io.Writer) error {
	fmt.Fprintln(out, "GROUP           KEY                                     KIND    STATUS")
	for _, p := range configschema.Registry {
		status := "default"
		masked := ""
		if v, err := s.Get(p.Key); err == nil {
			status = "set"
			if p.Secret && v != "" {
				masked = " (masked)"
			}
		}
		sec := ""
		if p.Secret {
			sec = "  [secret]"
		}
		fmt.Fprintf(out, "%-14s %-38s %-6s %s%s%s\n", p.Group, p.Key, string(p.Kind), status, masked, sec)
	}
	return nil
}

func parseEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("import-env read %s: %w", path, err)
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		v = strings.Trim(v, `"'`)
		if k != "" {
			m[k] = v
		}
	}
	return m, nil
}

func importEnvFile(s *configstore.ConfigStore, path string, out io.Writer) error {
	m, err := parseEnvFile(path)
	if err != nil {
		return err
	}
	imported, skipped := 0, 0
	for k, v := range m {
		if !configschema.Known(k) {
			skipped++
			continue
		}
		if err := s.Set(k, v, "cli:import-env"); err != nil {
			return err
		}
		imported++
	}
	fmt.Fprintf(out, "imported %d of %d keys from %s (%d unknown/skipped)\n", imported, len(m), path, skipped)
	return nil
}

// importDefaults seeds the store from the defaults JSONC (embedded, or
// $CONFIGDB_DEFAULTS_FILE when set) — first-boot population. NEVER clobbers a
// key already present in the store; null entries (no static default) are
// skipped; unknown keys are skipped + counted.
func importDefaults(s *configstore.ConfigStore, out io.Writer) error {
	var (
		def map[string]any
		err error
		src = "embedded"
	)
	if f := os.Getenv("CONFIGDB_DEFAULTS_FILE"); f != "" {
		b, rerr := os.ReadFile(f)
		if rerr != nil {
			return rerr
		}
		def, err = configschema.ParseDefaultsFile(string(b))
		src = f
	} else {
		def, err = configschema.Defaults()
	}
	if err != nil {
		return err
	}
	seeded, existing, unknown, nulls := 0, 0, 0, 0
	for k, v := range def {
		if v == nil {
			nulls++
			continue
		}
		if !configschema.Known(k) {
			unknown++
			continue
		}
		if _, gerr := s.Get(k); gerr == nil {
			existing++
			continue
		}
		sv := rawValueString(v)
		if err := s.Set(k, sv, "cli:import-defaults:"+src); err != nil {
			return err
		}
		seeded++
	}
	keys, _ := s.Keys()
	fmt.Fprintf(out, "defaults(%s): seeded %d, kept %d already-set, %d no-default, %d unknown -> store now has %d keys\n", src, seeded, existing, nulls, unknown, len(keys))
	return nil
}

func rawValueString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return fmt.Sprintf("%v", int64(x))
	case bool:
		if x {
			return "true"
		}
		return "false"
	}
	return ""
}

func initStore(s *configstore.ConfigStore, out io.Writer) error {
	if raw := os.Getenv(configstore.EncryptionKeyEnv); raw != "" {
		if _, err := configstore.ParseKey(raw); err != nil {
			return fmt.Errorf("encryption key present but invalid: %w", err)
		}
		fmt.Fprintln(out, "encryption: CONFIG_DB_ENCRYPTION_KEY present and valid")
	} else {
		key, err := configstore.GenerateKey()
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "encryption: NO key configured. Generated a new one — put it in the container environment as:")
		fmt.Fprintf(out, "  %s=%s\n", configstore.EncryptionKeyEnv, key)
		fmt.Fprintln(out, "(the key is NEVER stored in the DB; values written while it is active are field-encrypted)")
	}
	imported := 0
	for _, p := range configschema.Registry {
		v, ok := os.LookupEnv(p.Key)
		if !ok || v == "" {
			continue
		}
		if _, err := s.Get(p.Key); err == nil {
			continue // never clobber an existing DB value
		}
		if err := s.Set(p.Key, v, "cli:init-env"); err != nil {
			return err
		}
		imported++
	}
	// Step 3 (initial setup JSONC seed): populate the remaining keys from the
	// defaults file (embedded or $CONFIGDB_DEFAULTS_FILE). Never clobbers.
	if err := importDefaults(s, out); err != nil {
		return err
	}
	keys, err := s.Keys()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "db ready: %s (now %d keys; env-seeded %d, defaults-seeded per line above, existing values untouched)\n", dbPath(), len(keys), imported)
	return nil
}

func doctor(s *configstore.ConfigStore, out io.Writer) error {
	if raw := os.Getenv(configstore.EncryptionKeyEnv); raw == "" {
		fmt.Fprintln(out, "encryption: NOT configured (plain-text store)")
	} else if _, err := configstore.ParseKey(raw); err != nil {
		fmt.Fprintf(out, "encryption: CONFIG_DB_ENCRYPTION_KEY present but INVALID (%v)\n", err)
	} else {
		fmt.Fprintln(out, "encryption: configured (AES-256-GCM field encryption active)")
	}
	keys, err := s.Keys()
	if err != nil {
		fmt.Fprintf(out, "db: %s (UNREADABLE: %v — encrypted values need the key)\n", dbPath(), err)
		return nil
	}
	fmt.Fprintf(out, "db: %s (%d keys)\n", dbPath(), len(keys))
	if m, err := s.All(); err == nil {
		fmt.Fprintf(out, "read-back: OK (%d values readable)\n", len(m))
	} else {
		fmt.Fprintf(out, "read-back: FAILED (%v)\n", err)
	}
	return nil
}

func usage(w io.Writer) error {
	_, err := fmt.Fprint(w, `configdb — CLI for the overleaf SQLite config DB (P7-post item 1).

Usage:
  configdb list                      list configured keys
  configdb list --all                the FULL registry (groups, kinds, status, [secret])
  configdb get KEY [--reveal]        print the value of KEY (secrets masked without --reveal)
  configdb set KEY VALUE [SOURCE]    upsert KEY (type-checked against the registry)
  configdb delete KEY                remove KEY (idempotent)
  configdb export                    print the whole store as JSON (secrets decrypted, NOT masked)
  configdb backup [DEST]             write a JSON backup (default ./configdb-backup-<ts>.json)
  configdb restore SRC               load a JSON backup into the store
  configdb import-env FILE           import known keys from a KEY=VALUE env file (bootstrap)
  configdb import-defaults [FILE]    seed from the defaults JSONC (embedded/FILE; never clobbers)
  configdb defaults [FILE]           print the defaults JSONC (embedded/FILE)
  configdb init                      (un)configure the encryption key + seed env + seed defaults
  configdb doctor                    key/db/read-back health
  configdb help                      this help

DB path: $CONFIG_DB_PATH, or $OVERLEAF_HOME/configdb/configdb.sqlite3, or ./configdb/configdb.sqlite3.
Encryption: $CONFIG_DB_ENCRYPTION_KEY (hex-64 or base64-44, 32 bytes) — field encryption of stored
values (AES-256-GCM); the key itself is never stored in the DB.
`)
	return err
}

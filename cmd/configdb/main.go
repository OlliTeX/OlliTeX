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
	"time"

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
	case "list", "get", "set", "delete", "export", "backup", "restore":
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
		keys, err := s.Keys()
		if err != nil {
			return err
		}
		for _, k := range keys {
			fmt.Fprintln(out, k)
		}
		return nil

	case "get":
		if len(args) != 1 {
			return fmt.Errorf("get requires KEY")
		}
		v, err := s.Get(args[0])
		if err != nil {
			if errors.Is(err, configstore.ErrMissing) {
				return fmt.Errorf("key %q not present", args[0])
			}
			return err
		}
		fmt.Fprintln(out, v)
		return nil

	case "set":
		if len(args) < 2 {
			return fmt.Errorf("set requires KEY VALUE [SOURCE]")
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
	}
	return nil
}

func usage(w io.Writer) error {
	_, err := fmt.Fprint(w, `configdb — CLI for the overleaf SQLite config DB (P7-post item 1).

Usage:
  configdb list                      list configured keys
  configdb get KEY                   print the value of KEY
  configdb set KEY VALUE [SOURCE]    upsert KEY
  configdb delete KEY                remove KEY (idempotent)
  configdb export                    print the whole store as JSON
  configdb backup [DEST]             write a JSON backup (default ./configdb-backup-<ts>.json)
  configdb restore SRC               load a JSON backup into the store
  configdb help                      this help

DB path: $CONFIG_DB_PATH, or $OVERLEAF_HOME/configdb/configdb.sqlite3, or ./configdb/configdb.sqlite3.
`)
	return err
}

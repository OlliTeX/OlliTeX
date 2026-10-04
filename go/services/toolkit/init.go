package toolkit

import (
	"fmt"
	"io"
	"os"

	"ollitex/go/libraries/configschema"
	"ollitex/go/libraries/configstore"
)

// InitStore runs the first-boot setup (owner plane: the config store is the
// one true source — init seeds it without clobbering):
//
//  1. Encryption key: CONFIG_DB_ENCRYPTION_KEY validated, or a fresh key
//     GENERATED + printed (the key lives outside the DB by design).
//  2. Env seed: known registry keys present in the process env are imported
//     (never clobbering).
//  3. Defaults seed: every remaining registry key gets its registered default
//     (never clobbering).
//
// Mirrors `configdb init` semantics (same libraries), so TUI and CLI agree.
func InitStore(t *Toolkit, out io.Writer) error {
	if t.Store == nil {
		return fmt.Errorf("no config store open")
	}

	// 1. encryption key
	if raw := os.Getenv(configstore.EncryptionKeyEnv); raw != "" {
		if _, err := configstore.ParseKey(raw); err != nil {
			return fmt.Errorf("encryption key present but invalid: %w", err)
		}
		fmt.Fprintln(out, "encryption: "+configstore.EncryptionKeyEnv+" present and valid")
	} else {
		key, err := configstore.GenerateKey()
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "encryption: NO key configured. Generated a new one — export it as:")
		fmt.Fprintf(out, "  %s=%s\n", configstore.EncryptionKeyEnv, key)
		fmt.Fprintln(out, "(the key is NEVER stored in the DB; values written while it is active are field-encrypted)")
	}

	// 2. env seed (known keys present in the environment)
	envImported := 0
	for _, p := range configschema.Registry {
		v, ok := os.LookupEnv(p.Key)
		if !ok || v == "" {
			continue
		}
		if _, gerr := t.Store.Get(p.Key); gerr == nil {
			continue // never clobber an existing store value
		}
		if err := t.Store.Set(p.Key, v, "toolkit:init-env"); err != nil {
			return err
		}
		envImported++
	}

	// 3. defaults seed (registry default per key; never clobbers)
	defaultsSeeded := 0
	for _, p := range configschema.Registry {
		if p.Default == "" {
			continue
		}
		if _, gerr := t.Store.Get(p.Key); gerr == nil {
			continue
		}
		if err := t.Store.Set(p.Key, p.Default, "toolkit:init"); err != nil {
			return err
		}
		defaultsSeeded++
	}

	keys, err := t.Store.Keys()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "db ready: %s (%d keys; env-seeded %d, defaults-seeded %d, existing values untouched)\n",
		t.StoreDescribe, len(keys), envImported, defaultsSeeded)
	return nil
}

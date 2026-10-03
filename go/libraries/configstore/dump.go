package configstore

import (
	"encoding/json"
	"fmt"
	"os"
)

// writeDump serializes a store's map to dest as JSON (shared by both backends).
func writeDump(m map[string]string, dest string) (map[string]string, error) {
	buf, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("configstore: dump marshal: %w", err)
	}
	if err := os.WriteFile(dest, buf, 0o644); err != nil {
		return nil, fmt.Errorf("configstore: dump write %s: %w", dest, err)
	}
	return m, nil
}

// restoreInto upserts every entry of a JSON backup (key -> value) into st
// (source "restore"); returns the restored key count. Shared by both backends,
// which makes `configdb export` (SQLite) → `configdb restore` (Postgres) the
// one-shot SQLite→Postgres migration path.
func restoreInto(st Store, src string) (int, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return 0, fmt.Errorf("configstore: restore read %s: %w", src, err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return 0, fmt.Errorf("configstore: restore parse %s: %w", src, err)
	}
	for k, v := range m {
		if err := st.Set(k, v, "restore"); err != nil {
			return 0, err
		}
	}
	return len(m), nil
}

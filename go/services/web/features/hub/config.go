package hub

import (
	"encoding/json"
	"os"
	"sort"
	"strconv"

	"ollitex/go/libraries/configschema"
	"ollitex/go/libraries/configstore"
	"ollitex/go/services/web/core"
)

// /api/hub/config — manage the SQLite config DB (the single source of truth,
// D6) from the /hub admin (P7-post item 1: "managed by the admin part of
// /hub").
//
// Follows the /api/hub-theme pattern: core mustCsrf (middleware) → site-admin
// gate → store op → JSON.
//
// Slice B (2026-09-25): the surface is now the FULL registry
// (go/libraries/configschema — 149 typed keys), not the 3-key curated set:
//
//	GET → per registry key: {value|null, masked, source(db|env|default),
//	      secret, group}. Registered secrets are masked (value = "••••")
//	      unless they hold no value; the operator CLI can reveal
//	      (`configdb get KEY --reveal`).
//	PUT → flat {KEY: "value"}: registered keys only; "" = reset that key
//	      (delete → env/default takes over, same semantics as the e-mail
//	      templates); type-checked (bool/int) against the registry.
//
// The same DB file the CLI/toolkit reads (core.ConfigDBPath), so /hub admin
// and the operator CLI manage one store.

// hubConfigAllowed — true when key is in the registry (the single source of
// truth for what the admin surface may touch).
func hubConfigAllowed(key string) bool {
	return configschema.Known(key)
}

// openHubConfigStore — opens the shared config DB (creating it if needed, so
// a first /hub write persists), at core's canonical path.
func openHubConfigStore() (*configstore.ConfigStore, error) {
	return configstore.New(core.ConfigDBPath())
}

// hubConfigEntry is one row of the GET listing.
type hubConfigEntry struct {
	Value  *string `json:"value"`
	Masked bool    `json:"masked"`
	Source string  `json:"source"` // db | env | default
	Secret bool    `json:"secret"`
	Group  string  `json:"group"`
}

// buildHubConfigGet assembles the GET payload for the full registry.
func buildHubConfigGet(store *configstore.ConfigStore) (map[string]hubConfigEntry, error) {
	out := map[string]hubConfigEntry{}
	for _, p := range configschema.Registry {
		entry := hubConfigEntry{
			Value:  nil,
			Masked: false,
			Source: "default",
			Secret: p.Secret,
			Group:  p.Group,
		}
		v, err := store.Get(p.Key)
		if err == nil {
			entry.Source = "db"
			if p.Secret && v != "" {
				m := "••••"
				entry.Value = &m
				entry.Masked = true
			} else {
				entry.Value = &v
			}
		} else if env := processEnv(p.Key); env != "" {
			entry.Source = "env"
		}
		out[p.Key] = entry
	}
	return out, nil
}

// validateHubConfigPut checks a PUT body {KEY: value} against the registry.
// Empty values are "reset" (delete) entries — the admin's per-key reset
// (same semantics as the e-mail template slots). Returns the set/reset maps
// or the offending key + message.
func validateHubConfigPut(body map[string]any) (set, reset map[string]string, badKey, badMsg string) {
	set = map[string]string{}
	reset = map[string]string{}
	for k, v := range body {
		if !configschema.Known(k) {
			return nil, nil, k, k + " is not a known config key"
		}
		sv, _ := v.(string)
		if sv == "" {
			reset[k] = ""
			continue
		}
		if p, _ := configschema.Find(k); p.Kind != configschema.KString {
			if p.Kind == configschema.KBool {
				if _, err := strconv.ParseBool(sv); err != nil {
					return nil, nil, k, k + ": value must be a boolean (true/false/1/0)"
				}
			} else if _, err := strconv.Atoi(sv); err != nil {
				return nil, nil, k, k + ": value must be an integer"
			}
		}
		set[k] = sv
	}
	return set, reset, "", ""
}

func getHubConfig(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		store, err := openHubConfigStore()
		if err != nil {
			res.JSON(500, []byte(`{"error":"config store unavailable"}`))
			return
		}
		defer store.Close()

		out, err := buildHubConfigGet(store)
		if err != nil {
			res.BareWrite(500, []byte("{}"))
			return
		}
		b, err := json.Marshal(out)
		if err != nil {
			res.BareWrite(500, []byte("{}"))
			return
		}
		res.JSON(200, b)
	}
}

func putHubConfig(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		body, has, syntaxErr := parseBody(cxt.Req)
		if syntaxErr {
			res.BareWrite(400, []byte("{}")) // express.json syntax (pinned P6.1)
			return
		}
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		set, reset, badKey, badMsg := validateHubConfigPut(body)
		if badKey != "" {
			res.JSON(400, []byte(`{"error":"`+jsEscapeErr(badMsg)+`"}`))
			return
		}
		if !has {
			res.JSON(200, []byte(`{"ok":true,"set":[],"reset":[]}`))
			return
		}
		store, err := openHubConfigStore()
		if err != nil {
			res.JSON(500, []byte(`{"error":"config store unavailable"}`))
			return
		}
		defer store.Close()
		for k, sv := range set {
			if err := store.Set(k, sv, "hub:/api/hub/config"); err != nil {
				res.JSON(500, []byte(`{"error":"`+jsEscapeErr(k)+`: set failed"}`))
				return
			}
		}
		for k := range reset {
			if err := store.Delete(k); err != nil {
				res.JSON(500, []byte(`{"error":"`+jsEscapeErr(k)+`: reset failed"}`))
				return
			}
		}
		b, _ := json.Marshal(map[string]any{"ok": true, "set": sortedKeys(set), "reset": sortedKeys(reset)})
		res.JSON(200, b)
	}
}

// sortedKeys — deterministic JSON arrays (map iteration is random).
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// processEnv reads the process environment for GET's source attribution.
func processEnv(key string) string { return os.Getenv(key) }

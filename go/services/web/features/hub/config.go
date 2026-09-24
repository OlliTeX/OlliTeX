package hub

import (
	"encoding/json"

	"ollitex/go/libraries/configstore"
	"ollitex/go/services/web/core"
)

// /api/hub/config — manage the SQLite config DB (the env→override values —
// core/configdb_override.go) from the /hub admin (P7-post item 1: "managed
// by the admin part of /hub").
//
// Follows the /api/hub-theme pattern: core mustCsrf (middleware) → site-admin
// gate → store op → JSON. Only the curated non-secret keys may be read or
// written. The same DB file the CLI reads (core.ConfigDBPath), so /hub admin
// and the operator CLI manage one store.

// hubConfigAllowed — true when key is in the curated admin-editable set.
func hubConfigAllowed(key string) bool {
	for _, k := range core.ConfigDBOverridableKeys() {
		if k == key {
			return true
		}
	}
	return false
}

// openHubConfigStore — opens the shared config DB (creating it if needed, so
// a first /hub write persists), at core's canonical path.
func openHubConfigStore() (*configstore.ConfigStore, error) {
	return configstore.New(core.ConfigDBPath())
}

// getHubConfig — GET /api/hub/config (site admin):
// JSON of the curated keys → value in the config DB (null = not set in the
// DB, i.e. the env/defaults supply it).
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

		out := map[string]any{}
		for _, k := range core.ConfigDBOverridableKeys() {
			v, err := store.Get(k)
			if err != nil { // ErrMissing → not set in the DB
				out[k] = nil
				continue
			}
			out[k] = v
		}
		b, err := json.Marshal(out)
		if err != nil {
			res.BareWrite(500, []byte("{}"))
			return
		}
		res.JSON(200, b)
	}
}

// putHubConfig — PUT /api/hub/config (site admin):
// body {"AppName":"…","SiteURL":"…",…} (curated keys only) → set each →
// {"ok":true}. Unknown key → 400 {"error":"…"}; non-string/empty value → 400;
// malformed JSON → 400 {}.
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
		set := map[string]string{}
		if has {
			for k, v := range body {
				if !hubConfigAllowed(k) {
					res.JSON(400, []byte(`{"error":"`+jsEscapeErr(k)+` is not an admin-editable config key"}`))
					return
				}
				sv, _ := v.(string)
				if sv == "" {
					res.JSON(400, []byte(`{"error":"`+jsEscapeErr(k)+`: value must be a non-empty string"}`))
					return
				}
				set[k] = sv
			}
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
		res.JSON(200, []byte(`{"ok":true}`))
	}
}

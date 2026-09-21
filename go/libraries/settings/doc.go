// Package settings is the 1:1 Go port of `libraries/settings`
// (npm `@overleaf/settings` v3.0.0): merge.js + Settings.js.
//
// Node source (spec; pinned byte-for-byte in tests):
//
//	// merge.js
//	function merge(settings, defaults) {
//	  for (const [key, value] of Object.entries(settings)) {
//	   	if (typeof value === 'object' && !(value instanceof Array)) {
//	     	defaults[key] = merge(value, defaults[key] || {})
//	    } else {
//	     	defaults[key] = value
//	    }
//	  }
//	  return defaults
//	}
//
//	// Settings.js (per-module load, run once at require):
//	//   env: OVERLEAF_CONFIG || SHARELATEX_CONFIG (mismatch -> throw),
//	//   NODE_ENV (default "development", lowercased)
//	//   defaults: <cwd>/config/settings.defaults.{cjs,js} then
//	//              <entry-point>/config/settings.defaults.{cjs,js}
//	//   overrides: OVERLEAF_CONFIG path, else <cwd>/config/settings.<NODE_ENV>{.cjs,.js}
//	//   dispatch:  typeof settings.mergeWith === 'function' ?
//	//                  settings.mergeWith(overrides) : merge(overrides, settings)
//
// Go adaptations (documented; see HANDOFF §Divergences (LIB-05)):
//
//  1. Node loads config files via PnP-anchored `require(absPath)(absPath)`. Go
//     cannot require JS modules, so the seam is explicit: a `Reader` func maps
//     an absolute config path to its export value.
//  2. The `mergeWith` dispatch (web's lazy/stamp module, R11) is likewise an
//     injected seam: `Reducer` (nil = plain module -> `Merge`).
//  3. JS `Object.entries` ordering (ascending integer keys, then insertion
//     order) becomes `orderedKeys` (ascending numeric, then lexicographic):
//     Go maps have no insertion order. Success output is identical; only the
//     observed order of the (Go-erroneous) partial application before a null
//     override error can differ.
//  4. Node's TypeError "Cannot convert undefined or null to object" (a null
//     override value recurses into `Object.entries(null)`) returns as Go
//     `ErrNullOverride` wrapped with the dotted key path.
//
// Consumers in the Go tree mirror `require('@overleaf/settings')` (the plain
// object export) and `require('@overleaf/settings/merge')` ({ merge })
// respectively.
package settings

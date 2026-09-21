package settings

import "strconv"

// Merge mirrors Node `merge.js`, the recursive deep-merge used by
// `@overleaf/settings` to fold overrides into defaults:
//
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
// Go signature: Merge(overrides, defaults). It FOLDS `overrides` into
// `defaults`, MUTATES `defaults`, and returns the same map (identity
// contract: Node returns `defaults`; pinned in TestMergeReturnsDefaults).
//
// Faithful semantics (Node-oracle-pinned in settings_test.go):
//
//   - override value a plain object (map)   -> recurse into the (falsy->fresh)
//     defaults slot; override's nested keys win (3DEEP/RECURSE).
//   - override value `nil` (JS `null`)       -> error: Node runs
//     `Object.entries(null)` -> TypeError "Cannot convert undefined or null to
//     object". Returned as *NullOverrideError with the dotted key path.
//   - override value a primitive/array      -> wholesale replace. Arrays are
//     `instanceof Array` in Node, so they are replaced, never recursed; a Go
//     `[]any` value is not a `map[string]any`, so it takes this arm too.
func Merge(overrides, defaults map[string]any) (map[string]any, error) {
	if defaults == nil {
		return nil, ErrNilDefaults
	}
	if ov := overrides; ov == nil {
		// JS: merge(undefined, x) -> Object.entries(undefined) -> TypeError.
		return nil, ErrNilOverrides
	}
	return foldLayer(overrides, defaults, "")
}

// foldLayer folds one object level of `overrides` into `defaults` (mutating
// `defaults`) and returns it. `path` is the dotted prefix of the keys at this
// depth, carried for null-override error context.
func foldLayer(overrides, defaults map[string]any, path string) (map[string]any, error) {
	for _, k := range orderedKeys(overrides) {
		p := path + k
		v := overrides[k]

		if m, ok := v.(map[string]any); ok {
			// typeof 'object' && !(instanceof Array): plain nested object.
			slot, present := defaults[k]
			switch {
			case !present || jsFalsy(slot):
				// absent OR falsy slot (0 / "" / false / null): `|| {}` is a
				// fresh object -> fold into it and store (oracle SLOT-0 /
				// E-STR / FALSE all end up {"o":{"a":1}}).
				fresh := map[string]any{}
				if _, err := foldLayer(m, fresh, p+"."); err != nil {
					return nil, err
				}
				defaults[k] = fresh
			case isMap(slot):
				// slot is an existing map: mutate it in place (Node's
				// `defaults[key] || {}` yields the same object reference).
				if _, err := foldLayer(m, slot.(map[string]any), p+"."); err != nil {
					return nil, err
				}
			default:
				// TRUTHY PRIMITIVE slot (e.g. 5, "str"). Node does
				// `merge(obj, <prim>)`: `<prim>[k] = ...` writes are no-ops on a
				// primitive and it returns the primitive unchanged, so the
				// override is silently dropped and the slot survives (oracle
				// SLOT-5). Faithful: leave the primitive untouched.
				_ = m
			}
			continue
		}

		if v == nil {
			// JS `null` -> typeof 'object' (not Array) -> recurse into
			// Object.entries(null) -> TypeError. Reported with dotted path.
			return nil, &NullOverrideError{Path: p}
		}

		// primitive or array: wholesale replace.
		defaults[k] = v
	}
	return defaults, nil
}

func isMap(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

// jsFalsy mirrors JS truthiness for the primitive slot values the `|| {}`
// branch sees. nil/0/""/false/NaN are falsy; maps and arrays are truthy.
func jsFalsy(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case bool:
		return !x
	case string:
		return x == ""
	case float64:
		return x == 0 || x != x // 0 or NaN
	case int:
		return x == 0
	case int8:
		return x == 0
	case int16:
		return x == 0
	case int32:
		return x == 0
	case int64:
		return x == 0
	case uint:
		return x == 0
	case uint8:
		return x == 0
	case uint16:
		return x == 0
	case uint32:
		return x == 0
	case uint64:
		return x == 0
	default:
		return false // maps, arrays, unknown values are truthy
	}
}

// orderedKeys returns keys in V8 `Object.entries` order: ascending integer-like
// keys numerically first, then the remaining keys lexicographically. Go maps
// have no insertion order, so this canonical ordering pins the observable
// sequence (an implementation note; not a pinned oracle behavior).
func orderedKeys(m map[string]any) []string {
	nums, strs := []string{}, []string{}
	for k := range m {
		if isNonNegativeInt(k) {
			nums = append(nums, k)
		} else {
			strs = append(strs, k)
		}
	}
	sortNumericPrefix(nums)
	sortLex(strs)
	keys := make([]string, 0, len(nums)+len(strs))
	keys = append(keys, nums...)
	keys = append(keys, strs...)
	return keys
}

func sortNumericPrefix(keys []string) {
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && parseIntish(keys[j-1]) > parseIntish(keys[j]); j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
}

func sortLex(keys []string) {
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
}

func isNonNegativeInt(k string) bool {
	if k == "" {
		return false
	}
	for _, c := range k {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func parseIntish(k string) int {
	n, _ := strconv.Atoi(k)
	return n
}

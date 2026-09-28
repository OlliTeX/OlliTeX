// Package utils mirrors app/js/Utils.js: Op discriminators.
//
// Op is a generic map (parsed JSON) carrying its discriminator key. We
// mirror the JS "is X" guards rather than strict struct types, because ops
// traverse many layers as map[string]any and the guards are the only place
// the code narrows.
package utils

// IsInsert: op has a non-nil "i" key.
func IsInsert(op map[string]any) bool {
	v, ok := op["i"]
	return ok && v != nil
}

// IsRetain: op has a non-nil "r" key.
func IsRetain(op map[string]any) bool {
	v, ok := op["r"]
	return ok && v != nil
}

// IsDelete: op has a non-nil "d" key.
func IsDelete(op map[string]any) bool {
	v, ok := op["d"]
	return ok && v != nil
}

// IsComment: op has non-nil "c" and "t" keys.
func IsComment(op map[string]any) bool {
	c, ok := op["c"]
	if !ok || c == nil {
		return false
	}
	t, ok := op["t"]
	return ok && t != nil
}

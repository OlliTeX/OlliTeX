package otc

// ContainsNonBmpChars mirrors util.containsNonBmpChars: true when the string
// contains any character outside the BMP (Node: a leading high surrogate,
// i.e. a UTF-16 code unit in the D800–DBFF range). Go strings are UTF-8, so the
// equivalent is: some rune strictly greater than 0xFFFF.
func ContainsNonBmpChars(str string) bool {
	for _, r := range str {
		if r > 0xFFFF {
			return true
		}
	}
	return false
}

package otpure

import "unicode/utf16"

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

// UTF16Units is the Node-JS `content.length`: the number of UTF-16 code
// units (moved from otc/file_type_detector.go — pure, otc-free).
func UTF16Units(s string) int {
	return len(utf16.Encode([]rune(s)))
}

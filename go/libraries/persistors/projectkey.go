package persistors

import "strconv"

// ProjectKey mirrors libraries/object-persistor/src/ProjectKey.js — the AWS
// request-rate prefix scheme (avoid sequential key prefixes by reversing the
// zero-padded project id). Node:
//
//	format(projectId) = path.join(a, b, c) where
//	  prefix = naiveReverse(pad(projectId)), a=prefix[0:3], b=prefix[3:6], c=prefix[6:]
//
// e.g. format(1234) === '432/100/00000'  (pad(1234)="000001234" → reversed "432100000").

// Format mirrors `format(projectId)`.
func Format(projectId int) string {
	prefix := NaiveReverse(Pad(projectId))
	return prefix[0:3] + "/" + prefix[3:6] + "/" + prefix[6:]
}

// Pad mirrors `pad(number)`: `(number || 0).toString().padStart(9, '0')`.
func Pad(number int) string {
	s := strconv.Itoa(number)
	for len(s) < 9 {
		s = "0" + s
	}
	return s
}

// NaiveReverse mirrors `naiveReverse(string)`.
func NaiveReverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

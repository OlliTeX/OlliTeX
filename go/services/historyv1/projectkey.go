package historyv1

import (
	"path"
)

// projectKey 1:1 with @overleaf/object-persistor/src/ProjectKey.js.
//
// The advice in the AWS request-rate docs is to avoid sequential key
// prefixes, so the project ID part of the key is reversed.

// naiveReverse reverses a string rune-by-rune (JS split(”).reverse().join(”)).
func naiveReverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// pad — (number || 0).toString().padStart(9, '0').
func pad(number int) string {
	if number == 0 {
		number = 0 // (0 || 0)
	}
	s := itoa9(number)
	for len(s) < 9 {
		s = "0" + s
	}
	return s
}

func itoa9(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// format — path.join(prefix.slice(0,3), prefix.slice(3,6), prefix.slice(6)).
func projectKeyFormat(projectID string) string {
	prefix := naiveReverse(pad9(projectID))
	three := func(s string, n int) string {
		if len(s) > n {
			return s[:n]
		}
		return s
	}
	return path.Join(three(prefix, 3), prefix[3:6], prefix[6:])
}

// pad9 pads the numeric project id used with the persistor key.
func pad9(projectID string) string {
	// JS: pad(number) with number = projectId (numeric). 24-hex ids: the
	// Node historyStore is only exercised with numeric (PG-plane) ids for
	// the key — ObjectIds still round `number` through toString (string).
	if len(projectID) >= 9 {
		return projectID
	}
	out := leftPad(projectID, 9)
	return out
}

func leftPad(s string, n int) string {
	for len(s) < n {
		s = "0" + s
	}
	return s
}

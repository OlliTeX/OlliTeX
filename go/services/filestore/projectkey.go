package filestore

// --- ProjectKey (1:1 with object-persistor/src/ProjectKey.js) --------------

func fseProjectKeyFormat(id string) string {
	if id == "" {
		id = "0"
	}
	for len(id) < 9 {
		id = "0" + id
	}
	r := []byte(id)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	s := string(r)
	if len(s) < 6 {
		return s
	}
	return s[:3] + "/" + s[3:6] + "/" + s[6:]
}

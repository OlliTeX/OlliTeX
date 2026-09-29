// Package safereader ports services/clsi/app/js/SafeReader.js.
//
// Node semantics: readFile(file, size, encoding, cb) opens the file and
// reads UP TO size bytes from offset 0.
//   - ENOENT -> (null, ”, 0): "safe" no-op so the caller doesn't have to
//     special-case missing cache files.
//   - other os errors -> (err, "", -1) shape: the caller sees the error and
//     the result is empty.
//   - success -> (nil, buffer[0:bytesRead] as a string, bytesRead).
//
// Go port: the two-value (string, err) pattern of os.ReadFile is not enough
// because we only want the first N bytes and we must swallow ENOENT.
package safereader

import "os"

// ReadFile ports SafeReader.promises.readFile(file, size, encoding).
// Returns the first upTo bytes of `file` as a string. A missing file is
// NOT an error — it yields ("", nil, 0). Other errors propagate.
func ReadFile(file string, upTo int) (string, int, error) {
	f, err := os.Open(file)
	switch {
	case err == nil:
		defer f.Close()
	case os.IsNotExist(err):
		return "", 0, nil // ENOENT -> safe no-op
	default:
		return "", 0, err
	}
	buf := make([]byte, upTo)
	n, err := f.ReadAt(buf, 0)
	if n > 0 {
		// Node: buffer.toString(encoding, 0, bytesRead) — the slice of what
		// was actually read, not the full buffer.
		return string(buf[:n]), n, nil
	}
	if err != nil {
		// Node: the read callback receives (err, ...) -> callbackWithClose(err)
		// => the error path propagates with an empty result.
		return "", 0, err
	}
	return "", 0, nil
}

// ReadFileAtOffset ports fs.read(fd, buf, 0, len, position) semantics for
// callers that need the exact byte position (Node's SafeReader always reads
// from 0; kept minimal for parity with the public API).
// (Provided as a seam for test harnesses; not exported by the Node module.)
func ReadFileAtOffset(file string, upTo, offset int64) (string, int, error) {
	f, err := os.Open(file)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, nil
		}
		return "", 0, err
	}
	defer f.Close()
	buf := make([]byte, upTo)
	n, err := f.ReadAt(buf, offset)
	if err != nil {
		return "", n, err
	}
	return string(buf[:n]), n, nil
}

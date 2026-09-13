package filestore

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// --- FSPersistor (1:1 with libraries/object-persistor/src/FSPersistor.js) ---

// Store is the persistor abstraction the filestore handlers program against.
// Two backends implement it (1:1 with libraries/object-persistor):
//
//   - fseStore  — FSPersistor (BACKEND='fs', the CE default)
//   - s3xStore  — S3Persistor on a SeaweedFS/S3 gateway (BACKEND='s3')
//
// Semantics both honour:
//   - "location" is the bucket directory (fs) or the bucket NAME (s3) — the
//     same env vars carry values of one kind in fs mode and of the other in
//     s3 mode (CE: OVERLEAF_FILESTORE_TEMPLATE_FILES_BUCKET_NAME, the
//     OVERLEAF_HISTORY_*_BLOBS buckets).
//   - keys are handler-level keys (may contain '/'); fs flattens '/'→'_' when
//     useSubdirectories is off, S3 stores the key verbatim (Node S3Persistor
//     does the same — only FSPersistor flattens).
//   - missing key → fseNotFound (404); deleting a missing key is a no-op.
type Store interface {
	open(location, key string, reqUseSub bool) (io.ReadCloser, error)
	objectSize(location, key string, reqUseSub bool) (int64, error)
	objectMd5(location, key string, reqUseSub bool) (string, error)
	exists(location, key string, reqUseSub bool) bool
	sendStream(location, key string, r io.Reader, reqUseSub bool, sourceMd5 string) error
	sendFile(location, key, source string, reqUseSub bool) error
	copyObject(location, from, to string, reqUseSub bool) error
	deleteObject(location, key string, reqUseSub bool) error
	// deleteDirectory sweeps everything under the key prefix — fs does
	// `<flattened key>_*`, S3 lists with Prefix=key (1:1 with Node
	// S3Persistor.#listDirectory: `Prefix: key`, no trailing slash).
	deleteDirectory(location, key string, reqUseSub bool) error
	listFiles(location, key string, reqUseSub bool) []string
}

type fseStore struct {
	useSubdirectories bool
}

func (s *fseStore) fsPath(location, key string, reqUseSub bool) string {
	key = strings.TrimSuffix(key, "/")
	if !s.useSubdirectories && !reqUseSub {
		key = strings.ReplaceAll(key, "/", "_")
	}
	return filepath.Join(location, key)
}

func (s *fseStore) open(location, key string, reqUseSub bool) (io.ReadCloser, error) {
	f, err := os.Open(s.fsPath(location, key, reqUseSub))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fseNotFound()
		}
		return nil, fseRead("failed to open file for streaming")
	}
	return f, nil
}

func (s *fseStore) objectSize(location, key string, reqUseSub bool) (int64, error) {
	st, err := os.Stat(s.fsPath(location, key, reqUseSub))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fseNotFound()
		}
		return 0, fseRead("failed to stat file")
	}
	return st.Size(), nil
}

func (s *fseStore) objectMd5(location, key string, reqUseSub bool) (string, error) {
	f, err := s.open(location, key, reqUseSub)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fseRead("unable to hash file")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *fseStore) exists(location, key string, reqUseSub bool) bool {
	_, err := os.Stat(s.fsPath(location, key, reqUseSub))
	return err == nil
}

// sendStream writes r to the target key (via a temp file under location/tmp-*/).
func (s *fseStore) sendStream(location, key string, r io.Reader, reqUseSub bool, sourceMd5 string) error {
	target := s.fsPath(location, key, reqUseSub)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fseWrite("failed to write stream")
	}
	tempDir, err := os.MkdirTemp(location, "tmp-")
	if err != nil {
		return fseWrite("failed to create temp dir")
	}
	tempFile := filepath.Join(tempDir, "uploaded-file")
	cleanup := func() { _ = os.RemoveAll(tempDir) }
	f, err := os.Create(tempFile)
	if err != nil {
		cleanup()
		return fseWrite("problem writing temp file locally")
	}
	h := md5.New()
	var dst io.Writer = f
	if sourceMd5 != "" {
		dst = io.MultiWriter(f, h)
	}
	if _, err := io.Copy(dst, r); err != nil {
		_ = f.Close()
		cleanup()
		return fseWrite("problem writing temp file locally")
	}
	if err := f.Close(); err != nil {
		cleanup()
		return fseWrite("problem writing temp file locally")
	}
	if sourceMd5 != "" {
		if got := hex.EncodeToString(h.Sum(nil)); got != sourceMd5 {
			cleanup()
			return fseWrite("md5 hash mismatch")
		}
	}
	if err := os.Rename(tempFile, target); err != nil {
		cleanup()
		return fseWrite("failed to write stream")
	}
	cleanup()
	return nil
}

func (s *fseStore) sendFile(location, key, source string, reqUseSub bool) error {
	in, err := os.Open(source)
	if err != nil {
		return fseWrite("failed to copy the specified file")
	}
	defer in.Close()
	return s.sendStream(location, key, in, reqUseSub, "")
}

func (s *fseStore) copyObject(location, from, to string, reqUseSub bool) error {
	src := s.fsPath(location, from, reqUseSub)
	dst := s.fsPath(location, to, reqUseSub)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fseWrite("failed to copy file")
	}
	in, err := os.Open(src)
	if err != nil {
		return fseRead("failed to copy file")
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return fseWrite("failed to copy file")
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fseWrite("failed to copy file")
	}
	return nil
}

// deleteObject removes a key; a missing key is a no-op (1:1 with S3 semantics).
func (s *fseStore) deleteObject(location, key string, reqUseSub bool) error {
	p := s.fsPath(location, key, reqUseSub)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fseWrite("failed to delete file")
	}
	return nil
}

// deleteDirectory removes a converted-cache "directory" (flattened key prefix + '_').
func (s *fseStore) deleteDirectory(location, key string, reqUseSub bool) error {
	fsPath := s.fsPath(location, key, reqUseSub)
	if s.useSubdirectories || reqUseSub {
		if err := os.RemoveAll(fsPath); err != nil {
			return fseWrite("failed to delete directory")
		}
		return nil
	}
	pattern := fsPath + "_*"
	matches, _ := filepath.Glob(pattern)
	for _, m := range matches {
		if err := os.Remove(m); err != nil && !os.IsNotExist(err) {
			return fseWrite("failed to delete directory")
		}
	}
	return nil
}

func (s *fseStore) listFiles(location, key string, reqUseSub bool) []string {
	var out []string
	if s.useSubdirectories || reqUseSub {
		_ = filepath.WalkDir(s.fsPath(location, key, reqUseSub), func(p string, d os.DirEntry, err error) error {
			if err == nil && d != nil && d.Type().IsRegular() {
				out = append(out, p)
			}
			return nil
		})
		return out
	}
	base := s.fsPath(location, key, reqUseSub)
	matches, _ := filepath.Glob(base + "_*")
	return append(out, matches...)
}

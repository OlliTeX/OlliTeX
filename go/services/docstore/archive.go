package docstore

// archive.go — the object-persistor FS backend 1:1 (FSPersistor with
// useSubdirectories=false, as docstore's PersistorFactory selects it):
//
//   - keys are FLATTENED: `bucket/<key with '/' → '_'>` (e.g.
//     bucket/<projectId>_<docId>),
//   - writes go through a temp file in the bucket root (mkdtemp) then
//     rename, with an optional source-md5 verification,
//   - reads compute the md5 of the stored file and compare it to the
//     digest computed by the caller,
//   - deleteDirectory sweeps the flat `bucket/<key>_*` files.

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

type fsArchiver struct{}

func NewFSArchiver() Archiver { return fsArchiver{} }

func (fsArchiver) fsPath(bucket, key string) string {
	if len(key) > 0 && key[len(key)-1] == '/' {
		key = key[:len(key)-1]
	}
	// useSubdirectories=false ⇒ every '/' becomes '_' (FSPersistor._getFsPath)
	flat := make([]byte, 0, len(key))
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c == '/' {
			flat = append(flat, '_')
		} else {
			flat = append(flat, c)
		}
	}
	return filepath.Join(bucket, string(flat))
}

// Send = FSPersistor.sendStream 1:1 (temp dir in the bucket, atomic-ish
// rename, optional source-md5 check).
func (fsArchiver) Send(ctx context.Context, bucket, key string, data []byte, sourceMD5 string) error {
	target := (fsArchiver{}).fsPath(bucket, key)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp(bucket, "tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	tmpFile := filepath.Join(tmpDir, "uploaded-file")
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return err
	}
	if sourceMD5 != "" {
		h := md5.Sum(data)
		if hex.EncodeToString(h[:]) != sourceMD5 {
			return errors.New("md5 hash mismatch")
		}
	}
	if err := os.Rename(tmpFile, target); err != nil {
		// cross-device fallback (tmp dir and target share the bucket, but
		// keep the copy path for mounts that split them)
		if cerr := copyFile(tmpFile, target); cerr != nil {
			return err
		}
	}
	return nil
}

// Get = FSPersistor.getObjectStream + getObjectMd5Hash 1:1 (bytes + live
// md5 of the stored file).
func (fsArchiver) Get(ctx context.Context, bucket, key string) ([]byte, string, error) {
	data, err := os.ReadFile((fsArchiver{}).fsPath(bucket, key))
	if err != nil {
		return nil, "", err
	}
	h := md5.Sum(data)
	return data, hex.EncodeToString(h[:]), nil
}

// DeleteDirectory = FSPersistor.deleteDirectory 1:1 (useSubdirectories=false
// ⇒ sweep the flat `bucket/<key>_*` files, missing OK).
func (fsArchiver) DeleteDirectory(ctx context.Context, bucket, key string) error {
	if len(key) > 0 && key[len(key)-1] == '/' {
		key = key[:len(key)-1]
	}
	dir := filepath.Join(bucket, key)
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil
		}
		return err
	}
	prefix := filepath.Base(dir) + "_"
	for _, e := range entries {
		if e.Type().IsDir() || !hasPrefix(e.Name(), prefix) {
			continue
		}
		p := filepath.Join(filepath.Dir(dir), e.Name())
		if err := os.Remove(p); err != nil && !errors.Is(err, syscall.ENOENT) {
			return err
		}
	}
	return nil
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

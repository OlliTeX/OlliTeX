package filestore

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// --- LocalFileWriter (1:1 with app/js/LocalFileWriter.js) ------------------

type fseWriter struct {
	uploadFolder string
}

func fseUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (w *fseWriter) path(key string) string {
	k := key
	if k == "" {
		k = fseUUID()
	}
	k = strings.ReplaceAll(k, "/", "-")
	return filepath.Join(w.uploadFolder, k)
}

func (w *fseWriter) writeStream(r io.Reader, key string) (string, error) {
	p := w.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fseWrite("problem writing file locally")
	}
	in, err := os.Create(p)
	if err != nil {
		return "", fseWrite("problem writing file locally")
	}
	defer in.Close()
	if _, err := io.Copy(in, r); err != nil {
		_ = os.Remove(p)
		return "", fseWrite("problem writing file locally")
	}
	return p, nil
}

func (w *fseWriter) deleteFile(fsPath string) error {
	if fsPath == "" {
		return nil
	}
	if err := os.Remove(fsPath); err != nil && !os.IsNotExist(err) {
		return fseWrite("failed to delete file")
	}
	return nil
}

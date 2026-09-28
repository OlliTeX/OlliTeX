// Package largefilemanager is a 1:1 port of
// services/project-history/app/js/LargeFileManager.js (88 L).
package largefilemanager

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"

	pherrs "ollitex/go/services/project-history/internal/errors"
)

// Deps injects the seams the module talks to.
type Deps struct {
	// Settings.path.uploadFolder
	UploadFolder string `json:"upload_folder"`
	// Settings.maxFileSizeInBytes (null → stubbing disabled)
	MaxFileSizeInBytes *int64 `json:"max_file_size_in_bytes,omitempty"`
	// HashManager._getBlobHash
	GetBlobHash func(path string) (string, int64, error)
	// fs.createWriteStream / fs.unlink
	WriteFile func(dir, full string, data []byte) error
	Unlink    func(path string) error
	// logger.error { fsPath, fileId, maxFileSizeInBytes } 'file too large, will use stub'
	LogErr func(info map[string]any, msg string)
	// logger.debug { fsPath, fileId, fileSize, fileHash } 'replaced large file with stub'
	LogDebug func(info map[string]any, msg string)
	// crypto.randomUUID (optional; default = crypto/rand UUIDv4)
	NewUUID func() (string, error)
}

// CreateStub writes the vendor stub file into uploadFolder and returns the new
// path. On write error the vendor tags 'error writing stub file' with
// { fsPath, newFsPath }, unlinks, and continues; that error then propagates.
//
// The stub content is byte-exact (vendor `stubLines.join('\n')`):
//
//	FileTooLargeError v1\nFile too large to be stored in history service\n
//	id <fileId>\nsize <fileSize> bytes\nhash <fileHash>\n\0
func CreateStub(d *Deps, fsPath, fileId string, fileSize int64, fileHash string) (string, error) {
	uuid := ""
	if d.NewUUID != nil {
		var err error
		uuid, err = d.NewUUID()
		if err != nil {
			return "", err
		}
	} else {
		var err error
		uuid, err = genUUID()
		if err != nil {
			return "", err
		}
	}
	newFsPath := filepath.Join(d.UploadFolder, fmt.Sprintf("%s-%s-stub", uuid, fileId))
	stub := "FileTooLargeError v1\n" +
		"File too large to be stored in history service\n" +
		fmt.Sprintf("id %s", fileId) + "\n" +
		fmt.Sprintf("size %d bytes", fileSize) + "\n" +
		fmt.Sprintf("hash %s", fileHash) + "\n\u0000"
	if err := d.WriteFile(d.UploadFolder, newFsPath, []byte(stub)); err != nil {
		// vendor: OError.tag(error, 'error writing stub file', { fsPath, newFsPath })
		if d.Unlink != nil {
			_ = d.Unlink(newFsPath)
		}
		return "", pherrs.New("Error", "error writing stub file")
	}
	if d.LogDebug != nil {
		d.LogDebug(map[string]any{
			"fsPath": fsPath, "fileId": fileId,
			"fileSize": fileSize, "fileHash": fileHash,
		}, "replaced large file with stub")
	}
	return newFsPath, nil
}

// ReplaceWithStubIfNeeded mirrors the vendor: when maxFileSizeInBytes is set
// and fileSize exceeds it, log the error, hash the file, replace with stub →
// the stub path; otherwise pass the original path through.
func ReplaceWithStubIfNeeded(d *Deps, fsPath, fileId string, fileSize int64) (string, error) {
	if d.MaxFileSizeInBytes != nil && fileSize > *d.MaxFileSizeInBytes {
		if d.LogErr != nil {
			d.LogErr(map[string]any{
				"fsPath": fsPath, "fileId": fileId,
				"maxFileSizeInBytes": *d.MaxFileSizeInBytes,
			}, "file too large, will use stub")
		}
		fileHash, _, err := d.GetBlobHash(fsPath)
		if err != nil {
			return "", err
		}
		return CreateStub(d, fsPath, fileId, fileSize, fileHash)
	}
	return fsPath, nil
}

func genUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" +
		hex.EncodeToString(b[10:16]), nil
}

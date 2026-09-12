package datamanipulator

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// File type constants (1:1 with fileUtils.FileTypes).
const (
	DMText   = "text"
	DMBinary = "binary"
)

var dmSyncTransientRe = regexp.MustCompile(`\.(aux|log|out|toc|fls|idx|vrb)$`)
var dmSyncTeXGZRe = regexp.MustCompile(`\.synctex\.gz$`)

// dmSyncExcluded mirrors isSyncExcluded (RF.5: hidden in ANY segment + LaTeX transients).
func dmSyncExcluded(name string) bool {
	if name == "" {
		return true
	}
	parts := []string{}
	for _, seg := range strings.Split(name, "/") {
		if seg != "" {
			parts = append(parts, seg)
		}
	}
	if len(parts) == 0 {
		return true
	}
	for _, part := range parts {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	base := parts[len(parts)-1]
	return dmSyncTransientRe.MatchString(base) || dmSyncTeXGZRe.MatchString(base)
}

var dmBinaryExtSet = map[string]bool{
	"pdf": true, "jpg": true, "jpeg": true, "png": true, "gif": true, "zip": true,
	"ttf": true, "woff": true, "woff2": true, "eot": true, "ico": true,
	"exe": true, "msi": true, "bin": true, "tar": true, "gz": true,
	"rar": true, "7z": true, "img": true, "iso": true, "dmg": true,
}

func dmExtOf(p string) string {
	idx := strings.LastIndex(p, ".")
	if idx < 0 {
		base := p
		if slash := strings.LastIndex(p, "/"); slash >= 0 {
			base = p[slash+1:]
		}
		return strings.ToLower(base)
	}
	return strings.ToLower(p[idx+1:])
}

// dmDetectFileType mirrors detectFileType (extension fast path + null-byte + UTF-8 check).
func dmDetectFileType(filePath string, buf []byte) (typ, encoding string) {
	if dmBinaryExtSet[dmExtOf(filePath)] {
		return DMBinary, ""
	}
	if len(buf) == 0 {
		return DMText, "utf8"
	}
	sample := buf
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	nullCount := bytes.Count(sample, []byte{0})
	if float64(nullCount) > float64(len(sample))*0.05 {
		return DMBinary, ""
	}
	if !utf8.Valid(sample) {
		if nullCount == 0 {
			return DMText, "latin1"
		}
		return DMBinary, ""
	}
	return DMText, "utf8"
}

// dmChecksum mirrors calculateChecksum (sha256:hex, empty-hash fallback).
func dmChecksum(buf []byte) string {
	if len(buf) == 0 {
		return "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	}
	h := sha256.Sum256(buf)
	return "sha256:" + hex.EncodeToString(h[:])
}

const dmMute = "2006-01-02T15:04:05.000Z"

// dmFileMetadata mirrors getFileMetadata.
func dmFileMetadata(filePath string, buf []byte) map[string]interface{} {
	typ, encoding := dmDetectFileType(filePath, buf)
	name := filePath
	if slash := strings.LastIndex(filePath, "/"); slash >= 0 {
		name = filePath[slash+1:]
	}
	if name == "" {
		name = filePath
	}
	m := map[string]interface{}{
		"relative_path": filePath,
		"name":          name,
		"type":          "file",
		"size":          len(buf),
		"binary":        typ == DMBinary,
		"checksum":      dmChecksum(buf),
		"mtime":         time.Now().UTC().Format(dmMute),
	}
	if encoding != "" {
		m["encoding"] = encoding
	}
	return m
}

// resolveProjectPath mirrors resolveProjectPath (traversal guard).
func dmResolveProjectPath(projectDir, relativePath string) (string, error) {
	root, _ := filepath.Abs(projectDir)
	resolved, _ := filepath.Abs(filepath.Join(root, relativePath))
	if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
		return "", errors.New("Path must stay within the project directory")
	}
	return resolved, nil
}

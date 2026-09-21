package persistors

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FSSettings mirrors the FSPersistor settings object.
type FSSettings struct {
	UseSubdirectories bool
	StorageClass      any // Node: presence → NotImplementedError
}

// FSPersistor is the 1:1 port of FSPersistor.js.
type FSPersistor struct {
	useSubdirectories bool
}

// NewFSPersistor mirrors `new FSPersistor(settings)`:
// settings.storageClass present → NotImplementedError
// ('FS backend does not support storage classes').
func NewFSPersistor(settings FSSettings) (*FSPersistor, error) {
	if settings.StorageClass != nil {
		return nil, NewNotImplementedError("FS backend does not support storage classes", nil)
	}
	return &FSPersistor{useSubdirectories: settings.UseSubdirectories}, nil
}

var _ Persistor = (*FSPersistor)(nil)

// SendFile mirrors sendFile (copy — not move — via a read stream).
func (f *FSPersistor) SendFile(location, target, source string) error {
	src, err := os.Open(source)
	if err != nil {
		return wrapError(err, "failed to copy the specified file",
			map[string]any{"location": location, "target": target, "source": source}, classWrite)
	}
	defer src.Close()
	if err := f.SendStream(location, target, src, Opts{}); err != nil {
		return wrapError(err, "failed to copy the specified file",
			map[string]any{"location": location, "target": target, "source": source}, classWrite)
	}
	return nil
}

// SendStream mirrors sendStream:
//
//	ifNoneMatch '*'  → NotImplementedError (FS has no exclusive flag)
//	temp file in <location>/tmp-*/uploaded-file (observer + optional md5
//	  observer, verified against opts.sourceMd5), then rename onto the target,
//	then temp cleanup.
func (f *FSPersistor) SendStream(location, target string, source io.Reader, opts Opts) error {
	if opts.IfNoneMatch == "*" {
		return NewNotImplementedError(
			"Overwrite protection required by caller, but it is not available is FS backend. Configure GCS or S3 backend instead, get in touch with support for further information.",
			nil)
	}

	targetPath := f.getFsPath(location, target, false)

	info := map[string]any{
		"location":    location,
		"target":      target,
		"ifNoneMatch": opts.IfNoneMatch,
	}
	if err := f.ensureDirectoryExists(targetPath); err != nil {
		return wrapError(err, "failed to write stream", info, classWrite)
	}
	tempFilePath, err := f.writeStreamToTempFile(location, source, opts)
	var writeErr error
	if err != nil {
		writeErr = err
	} else {
		// rename temp → target (atomic on the same filesystem: both live
		// under `location`), then always clean the temp dir (Node: finally).
		if rerr := os.Rename(tempFilePath, targetPath); rerr != nil {
			writeErr = rerr
		}
		if cerr := f.cleanupTempFile(tempFilePath); cerr != nil && writeErr == nil {
			writeErr = cerr
		}
	}
	if writeErr != nil {
		return wrapError(writeErr, "failed to write stream", info, classWrite)
	}
	return nil
}

// GetObjectStream mirrors getObjectStream (range-capable file stream).
func (f *FSPersistor) GetObjectStream(location, name string, opts Opts) (io.ReadCloser, error) {
	if opts.AutoGunzip {
		return nil, NewNotImplementedError(
			"opts.autoGunzip is not supported by FS backend. Configure GCS or S3 backend instead, get in touch with support for further information.",
			nil)
	}
	observer := NewObserver("fs.ingress", location, "") // ingress to us from disk
	fsPath := f.getFsPath(location, name, opts.UseSubdirectories)

	fh, err := os.Open(fsPath)
	if err != nil {
		return nil, wrapError(err, "failed to open file for streaming", map[string]any{
			"location": location,
			"name":     name,
			"fsPath":   fsPath,
			"opts":     opts,
		}, classRead)
	}
	start := int64(0)
	if opts.Start != nil {
		start = *opts.Start
	}
	if start > 0 {
		if _, serr := fh.Seek(start, io.SeekStart); serr != nil {
			fh.Close()
			return nil, wrapError(serr, "failed to open file for streaming", map[string]any{
				"location": location,
				"name":     name,
				"fsPath":   fsPath,
				"opts":     opts,
			}, classRead)
		}
	}
	if opts.End != nil {
		// Node fs.createReadStream: {start, end} — end INCLUSIVE.
		limit := *opts.End - start + 1
		if limit <= 0 {
			limit = 0
		}
		body := io.LimitReader(fh, limit)
		return &fsStream{body: body, fh: fh, observer: observer}, nil
	}
	return &fsStream{body: fh, fh: fh, observer: observer}, nil
}

// fsStream surfaces source errors on Read and reports the observer on close
// (Node: pipeline(stream, observer, pass).catch(() => {})).
type fsStream struct {
	body     io.Reader
	fh       *os.File
	observer *Observer
}

func (s *fsStream) Read(p []byte) (int, error) {
	n, err := s.body.Read(p)
	if err != nil && err != io.EOF {
		s.observer.Finish(err)
	}
	return n, err
}

func (s *fsStream) Close() error {
	s.observer.Finish(nil)
	return s.fh.Close()
}

// GetRedirectURL mirrors getRedirectUrl: null.
func (f *FSPersistor) GetRedirectURL(location, name string) (string, error) {
	return "", nil
}

// GetObjectSize mirrors getObjectSize.
func (f *FSPersistor) GetObjectSize(location, filename string, _ Opts) (int64, error) {
	fsPath := f.getFsPath(location, filename)
	st, err := os.Stat(fsPath)
	if err != nil {
		return 0, wrapError(err, "failed to stat file",
			map[string]any{"location": location, "filename": filename}, classRead)
	}
	return st.Size(), nil
}

// GetObjectMd5Hash mirrors getObjectMd5Hash. Note the Node code throws a
// direct ReadError here (NOT wrapError) — an ENOENT therefore stays a
// ReadError('unable to get md5 hash from file'), unlike other fs paths.
func (f *FSPersistor) GetObjectMd5Hash(location, filename string, _ Opts) (string, error) {
	fsPath := f.getFsPath(location, filename)
	fh, err := os.Open(fsPath)
	if err != nil {
		return "", NewReadError("unable to get md5 hash from file",
			map[string]any{"location": location, "filename": filename}, err)
	}
	defer fh.Close()
	h := md5.New()
	if _, err := io.Copy(h, fh); err != nil {
		return "", NewReadError("unable to get md5 hash from file",
			map[string]any{"location": location, "filename": filename}, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CopyObject mirrors copyObject.
func (f *FSPersistor) CopyObject(location, source, target string, _ Opts) error {
	sourceFsPath := f.getFsPath(location, source)
	targetFsPath := f.getFsPath(location, target)

	if err := f.ensureDirectoryExists(targetFsPath); err != nil {
		return wrapError(err, "failed to copy file", map[string]any{
			"location": location, "source": source, "target": target,
			"sourceFsPath": sourceFsPath, "targetFsPath": targetFsPath,
		}, classWrite)
	}
	if err := copyFile(sourceFsPath, targetFsPath); err != nil {
		return wrapError(err, "failed to copy file", map[string]any{
			"location": location, "source": source, "target": target,
			"sourceFsPath": sourceFsPath, "targetFsPath": targetFsPath,
		}, classWrite)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// DeleteObject mirrors deleteObject (force — missing file is a no-op; S3
// parity: no NotFoundError on delete).
func (f *FSPersistor) DeleteObject(location, name string) error {
	fsPath := f.getFsPath(location, name)
	if err := os.RemoveAll(fsPath); err != nil { // rm { force: true }
		return wrapError(err, "failed to delete file",
			map[string]any{"location": location, "name": name, "fsPath": fsPath}, classWrite)
	}
	return nil
}

// DeleteDirectory mirrors deleteDirectory.
func (f *FSPersistor) DeleteDirectory(location, name string, _ string) error {
	fsPath := f.getFsPath(location, name)

	var err error
	if f.useSubdirectories {
		err = os.RemoveAll(fsPath) // rm { recursive: true, force: true }
	} else {
		files, lerr := f.listDirectory(fsPath)
		if lerr != nil {
			err = lerr
		}
		for _, file := range files {
			if rerr := os.RemoveAll(file); rerr != nil {
				err = rerr
			}
		}
	}
	if err != nil {
		return wrapError(err, "failed to delete directory",
			map[string]any{"location": location, "name": name, "fsPath": fsPath}, classWrite)
	}
	return nil
}

// ListDirectoryKeys mirrors listDirectoryKeys (files only).
func (f *FSPersistor) ListDirectoryKeys(location, name string) ([]string, error) {
	fsPath := f.getFsPath(location, name)
	paths, err := f.listDirectory(fsPath)
	if err != nil {
		return nil, err
	}
	files := []string{}
	for _, p := range paths {
		st, serr := os.Stat(p)
		if serr != nil {
			if !errorsFSNotExist(serr) {
				return nil, serr
			}
			continue // ignore files that may have just been deleted
		}
		if st.Mode().IsRegular() {
			files = append(files, p)
		}
	}
	return files, nil
}

// ListDirectoryStats mirrors listDirectoryStats (files only, with sizes).
func (f *FSPersistor) ListDirectoryStats(location, name string) ([]DirStat, error) {
	fsPath := f.getFsPath(location, name)
	paths, err := f.listDirectory(fsPath)
	if err != nil {
		return nil, err
	}
	stats := []DirStat{}
	for _, p := range paths {
		st, serr := os.Stat(p)
		if serr != nil {
			if !errorsFSNotExist(serr) {
				return nil, serr
			}
			continue
		}
		if st.Mode().IsRegular() {
			stats = append(stats, DirStat{Key: p, Size: st.Size()})
		}
	}
	return stats, nil
}

// CheckIfObjectExists mirrors checkIfObjectExists.
func (f *FSPersistor) CheckIfObjectExists(location, name string, _ Opts) (bool, error) {
	fsPath := f.getFsPath(location, name)
	if _, err := os.Stat(fsPath); err != nil {
		if errorsFSNotExist(err) {
			return false, nil
		}
		return false, wrapError(err, "failed to stat file",
			map[string]any{"location": location, "name": name, "fsPath": fsPath}, classRead)
	}
	return true, nil
}

// DirectorySize mirrors directorySize (flat only — the fs layout is
// flattened unless useSubdirectories).
func (f *FSPersistor) DirectorySize(location, name string, _ string) (int64, error) {
	fsPath := f.getFsPath(location, name)
	var size int64
	files, err := f.listDirectory(fsPath)
	if err != nil {
		return 0, wrapError(err, "failed to get directory size",
			map[string]any{"location": location, "name": name}, classRead)
	}
	for _, file := range files {
		st, serr := os.Stat(file)
		if serr != nil {
			if !errorsFSNotExist(serr) {
				return 0, serr
			}
			continue
		}
		if st.Mode().IsRegular() {
			size += st.Size()
		}
	}
	return size, nil
}

// ---------------------------------------------------------------------------

// writeStreamToTempFile mirrors _writeStreamToTempFile.
func (f *FSPersistor) writeStreamToTempFile(location string, stream io.Reader, opts Opts) (string, error) {
	tempDirPath, err := os.MkdirTemp(location, "tmp-")
	if err != nil {
		return "", NewWriteError("problem writing temp file locally", map[string]any{}, err)
	}
	tempFilePath := filepath.Join(tempDirPath, "uploaded-file")

	observer := NewObserver("fs.egress", location, "") // egress from us to disk
	md5h := md5.New()
	var needMD5 bool
	if opts.SourceMD5 != "" {
		needMD5 = true
	}

	dst, err := os.Create(tempFilePath)
	if err != nil {
		f.cleanupTempFile(tempFilePath)
		return "", NewWriteError("problem writing temp file locally", map[string]any{"tempFilePath": tempFilePath}, err)
	}
	w := multiWriter(dst, observer)
	if needMD5 {
		w = multiWriter(w, md5h)
	}
	written, err := io.Copy(w, stream)
	closeErr := dst.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		f.cleanupTempFile(tempFilePath)
		return "", NewWriteError("problem writing temp file locally", map[string]any{"tempFilePath": tempFilePath}, err)
	}

	if needMD5 {
		actualMD5 := hex.EncodeToString(md5h.Sum(nil))
		if actualMD5 != opts.SourceMD5 {
			f.cleanupTempFile(tempFilePath)
			return "", NewWriteError("md5 hash mismatch", map[string]any{
				"expectedMd5": opts.SourceMD5,
				"actualMd5":   actualMD5,
			})
		}
	}
	_ = written
	return tempFilePath, nil
}

func multiWriter(writers ...io.Writer) io.Writer {
	return &multiWriterT{writers: writers}
}

type multiWriterT struct{ writers []io.Writer }

func (m *multiWriterT) Write(p []byte) (int, error) {
	var firstErr error
	total := 0
	for _, w := range m.writers {
		n, err := w.Write(p)
		total = n
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return total, firstErr
}

// cleanupTempFile mirrors _cleanupTempFile (rm the temp DIR, force+recursive).
func (f *FSPersistor) cleanupTempFile(tempFilePath string) error {
	dirPath := filepath.Dir(tempFilePath)
	return os.RemoveAll(dirPath)
}

// getFsPath mirrors _getFsPath(location, key, useSubdirectories = false).
func (f *FSPersistor) getFsPath(location, key string, useSubdirectories ...bool) string {
	key = strings.TrimRight(key, "/")
	us := false
	if len(useSubdirectories) > 0 {
		us = useSubdirectories[0]
	}
	if !f.useSubdirectories && !us {
		key = strings.ReplaceAll(key, "/", "_")
	}
	return filepath.Join(location, key)
}

// listDirectory mirrors _listDirectory: subdirs → recursive walk (glob
// `path/**`), flat → `<path>_*` siblings (the flattened-key scheme).
func (f *FSPersistor) listDirectory(path string) ([]string, error) {
	if f.useSubdirectories {
		return walkRecursive(path)
	}
	matches, err := filepath.Glob(path + "_*")
	if err != nil {
		return nil, err
	}
	return matches, nil
}

// walkRecursive — Go equivalent of glob(`path/**`): all entries under path,
// dotfiles/dotdirs excluded (Node glob defaults: dot = false).
func walkRecursive(root string) ([]string, error) {
	var out []string
	if _, err := os.Stat(root); err != nil {
		if errorsFSNotExist(err) {
			return out, nil // Node: glob of a missing path → []
		}
		return nil, err
	}
	err := fs.WalkDir(os.DirFS(root), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			if errorsFSNotExist(err) {
				return nil // ignore files deleted mid-walk
			}
			return err
		}
		if d == nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if rel != "." && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		st, serr := d.Info()
		if serr != nil {
			if errorsFSNotExist(serr) {
				return nil
			}
			return serr
		}
		if st.Mode().IsRegular() {
			out = append(out, filepath.Join(root, rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ensureDirectoryExists mirrors _ensureDirectoryExists (mkdir recursive).
func (f *FSPersistor) ensureDirectoryExists(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

// errorsFSNotExist — the Go `error.code === 'ENOENT'` check.
func errorsFSNotExist(err error) bool {
	return os.IsNotExist(err) || err == fs.ErrNotExist
}

var _ = fmt.Sprintf

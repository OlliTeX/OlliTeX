package repo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	bzip2 "github.com/dsnet/compress/bzip2"
)

// compressDir ports Java Tar.{gzip,bz2}.zip(dir): archive dir with entry
// names relative to its PARENT (so the top-level entry is ".git"),
// gzip- or bzip2-compressed. Returns the full compressed byte stream.
func compressDir(dir, parentPath, kind string) ([]byte, error) {
	var buf bytes.Buffer
	var stream io.WriteCloser
	switch kind {
	case "gzip":
		var e error
		stream, e = gzip.NewWriterLevel(&buf, 9)
		if e != nil {
			return nil, e
		}
	case "bzip2":
		var e error
		stream, e = bzip2.NewWriter(&buf, nil)
		if e != nil {
			return nil, e
		}
	default:
		return nil, fmt.Errorf("unknown stream kind: %s", kind)
	}
	tw := tar.NewWriter(stream)
	if err := addTarEntry(tw, parentPath, dir); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := stream.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// addTarEntry mirrors Java Tar.addTarEntry(tout, base, fileOrDir): base is
// the PARENT of the path, so entry names are relative to it (".git",
// ".git/HEAD", ...). Directories are emitted with a trailing slash, then
// recursively.
func addTarEntry(tw *tar.Writer, base, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return &sizeLimitError{path: path, size: 0}
	}
	if st.IsDir() {
		name := relToBase(abs, base)
		hdr := &tar.Header{
			Name:     name + "/",
			Mode:     int64(st.Mode().Perm()),
			Typeflag: tar.TypeDir,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			return err
		}
		for _, e := range entries {
			full := filepath.Join(abs, e.Name())
			if e.IsDir() {
				if err := addTarEntry(tw, base, full); err != nil {
					return err
				}
			} else {
				if err := addTarFile(tw, base, full); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return addTarFile(tw, base, abs)
}

// addTarFile mirrors Java Tar.addTarFile: checks size ≤ Integer.MAX_VALUE
// (1<<31-1) before writing the file bytes.
func addTarFile(tw *tar.Writer, base, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return &sizeLimitError{path: path, size: 0}
	}
	if st.Size() > int64(1<<31-1) {
		return &sizeLimitError{path: path, size: st.Size()}
	}
	body, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	name := relToBase(abs, base)
	hdr := &tar.Header{
		Name: name,
		Mode: int64(st.Mode().Perm()),
		Size: st.Size(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = tw.Write(body)
	return err
}

// relToBase returns the /-separated relative path of abs under base
// (Java base.relativize(abs)). base being the parent → ".git", ".git/HEAD".
func relToBase(abs, base string) string {
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(rel)
}

// sizeLimitError mirrors Java FileTooLargeException.
type sizeLimitError struct {
	path string
	size int64
}

func (e *sizeLimitError) Error() string {
	return fmt.Sprintf("file too big (%d B): %s", e.size, e.path)
}

// untarStream ports Java Tar.untar: extract the gzip/bzip2'd tar into
// target (parentDir). Java calls setLastModified BEFORE the file exists
// (no-op on missing files), then creates/mkdirs and copies; size is checked
// ≤ Integer.MAX_VALUE. Directory names in the archive end with "/".
func untarStream(data []byte, target, kind string) error {
	var reader io.Reader
	switch kind {
	case "gzip":
		gr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return err
		}
		defer gr.Close()
		reader = gr
	case "bzip2":
		br, err := bzip2.NewReader(bytes.NewReader(data), nil)
		if err != nil {
			return err
		}
		reader = br
	default:
		reader = bytes.NewReader(data)
	}
	tr := tar.NewReader(reader)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := hdr.Name
		if len(name) > 0 && name[0] == '/' {
			name = name[1:]
		}
		if name == "" || name == "." {
			continue
		}
		if hdr.Size < 0 || hdr.Size > int64(1<<31-1) {
			return fmt.Errorf("file too big (%d B)", hdr.Size)
		}
		dest := filepath.Join(target, filepath.FromSlash(name))
		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		f, err := os.Create(dest)
		if err != nil {
			return err
		}
		if _, cerr := io.Copy(f, tr); cerr != nil {
			f.Close()
			return cerr
		}
		if cerr := f.Close(); cerr != nil {
			return cerr
		}
	}
}

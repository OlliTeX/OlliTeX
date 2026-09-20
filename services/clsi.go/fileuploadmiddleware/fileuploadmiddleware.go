// Package fileuploadmiddleware ports services/clsi/app/js/FileUploadMiddleware.js
// (31L).
//
// Node:
//
//	const upload = multer({
//	  dest: Settings.path.uploadFolder,
//	  limits: { fileSize: Settings.maxUploadSize, parts: 2 },
//	})
//
//	function multerMiddleware(req, res, next) {
//	  return upload.single('qqfile')(req, res, (err) => {
//	    if (err instanceof multer.MulterError && err.code === 'LIMIT_FILE_SIZE') {
//	      return res.status(422).json({success:false, error:'file_too_large'})
//	    }
//	    if (err) return next(err)
//	    if (!req.file?.path) {
//	      logger.info({req}, 'missing req.file.path on upload')
//	      return res.status(400).json({success:false, error:'invalid_upload_request'})
//	    }
//	    next()
//	  })
//	}
//
// Go mapping (service-level transport leaf, not a library):
//
//   - `upload.single('qqfile')` => extract the single multipart part whose
//     FieldName is "qqfile" and stream it to `M.UploadDir` under a random
//     name (multer `getRandomFilename` style).
//   - `fileSize` limit => 422 `file_too_large` (partial file is removed).
//   - `parts: 2` limit => more than two parts => forwarded error (500).
//   - no file part / empty `path` => 400 `invalid_upload_request`.
//   - any other error => forwarded (500), mirroring express `next(err)`.
//
// The next handler receives the extracted *UploadedFile (a faithful stand-in
// for Node's `req.file`), so the Go route is wired as
// `next(w, r, file)`.
package fileuploadmiddleware

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ErrTooManyParts mirrors multer `LIMIT_PART_COUNT` (non-file error: forwarded).
var ErrTooManyParts = errors.New("too many parts")

// ErrUploadFileTooLarge mirrors multer code `LIMIT_FILE_SIZE`.
type ErrUploadFileTooLarge struct{ Limit int64 }

func (e ErrUploadFileTooLarge) Error() string {
	return fmt.Sprintf("file too large (limit %d)", e.Limit)
}

// UploadedFile mirrors `req.file` (multer single).
type UploadedFile struct {
	Fieldname   string `json:"fieldname"`   // "qqfile"
	Original    string `json:"originalname"` // client-supplied filename
	Filename    string `json:"filename"`     // random temp name inside UploadDir
	Path        string `json:"path"`         // Node `req.file.path` (destination)
	Size        int64  `json:"size"`
	ContentType string `json:"-"`
}

// Middleware is the configured `upload` object (dest + limits).
type Middleware struct {
	uploadDir   string
	maxFileSize int64
	maxParts    int // Node: 2
	logInfo     func(map[string]any, string)
	logWarn     func(map[string]any, string)
	// openFile is a test seam so savePart's error branches are reachable.
	openFile func(name string) (*os.File, error)
	// writeFile is a test seam so savePart's write-error branch is reachable.
	writeFile func(f *os.File, buf []byte) (int, error)
	// partRead is a test seam mirroring *multipart.Part.Read (error branch).
	partRead func(p *multipart.Part, buf []byte) (int, error)
	// randBytes is a test seam mirroring crypto/rand.Read (fallback branch).
	randBytes func(n int) ([]byte, error)
	// nextParts is a test seam for parts that exceed the limit (drain path).
	drainParts func(r *multipart.Reader) int
}

// New wires the middleware (mirrors the module-level `upload`).
func New(uploadDir string, maxFileSize int64, logInfo, logWarn func(map[string]any, string)) *Middleware {
	if logInfo == nil {
		logInfo = func(map[string]any, string) {}
	}
	if logWarn == nil {
		logWarn = func(map[string]any, string) {}
	}
	return &Middleware{
		uploadDir:   uploadDir,
		maxFileSize: maxFileSize,
		maxParts:    2,
		logInfo:     logInfo,
		logWarn:     logWarn,
		openFile:    defaultOpenFile,
		writeFile:   osFileWrite,
		partRead:    partReadDefault,
		randBytes:   defaultRandBytes,
		drainParts:  defaultDrain,
	}
}

func defaultOpenFile(name string) (*os.File, error) {
	return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
}

func osFileWrite(f *os.File, buf []byte) (int, error) { return f.Write(buf) }

func partReadDefault(p *multipart.Part, buf []byte) (int, error) { return p.Read(buf) }

func defaultRandBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// defaultDrain discards the rest of the multipart stream and reports how
// many extra parts remained (for observability only).
func defaultDrain(r *multipart.Reader) int {
	extra := 0
	for {
		_, err := r.NextPart()
		if err == io.EOF {
			return extra
		}
		extra++
	}
}

// UploadDir / MaxFileSize accessors (for tests and composition).
func (m *Middleware) UploadDir() string   { return m.uploadDir }
func (m *Middleware) MaxFileSize() int64  { return m.maxFileSize }
func (m *Middleware) MaxParts() int       { return m.maxParts }

// Handle mirrors `multerMiddleware`: uploads, enforces limits, then calls
// `next(w, r, file)`. On failure it writes the express JSON envelope and
// does not forward.
func (m *Middleware) Handle(next func(w http.ResponseWriter, r *http.Request, file *UploadedFile)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file, err := m.single(r)
		if err != nil {
			if varErr, ok := err.(ErrUploadFileTooLarge); ok {
				_ = varErr
				m.respond(w, http.StatusUnprocessableEntity, "file_too_large")
				m.logWarn(map[string]any{"err": err.Error()}, "file too large")
				return
			}
			m.respond(w, http.StatusInternalServerError, "upload_failed")
			m.logWarn(map[string]any{"err": err.Error()}, "upload failed")
			return
		}
		if file == nil || file.Path == "" {
			m.logInfo(map[string]any{"url": r.URL.String()}, "missing req.file.path on upload")
			m.respond(w, http.StatusBadRequest, "invalid_upload_request")
			return
		}
		next(w, r, file)
	}
}

// single mirrors `upload.single('qqfile')`: returns the saved UploadedFile,
// or nil if the "qqfile" field has no file part (Node: `req.file` unset).
func (m *Middleware) single(r *http.Request) (*UploadedFile, error) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		// Not multipart: no file field, no error (Node: req.file undefined).
		return nil, nil
	}
	if r.Body == nil {
		return nil, nil
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, fmt.Errorf("parse multipart: %w", err)
	}
	var saved *UploadedFile
	partCount := 0
	for {
		part, perr := mr.NextPart()
		if perr == io.EOF {
			break
		}
		if perr != nil {
			return nil, perr
		}
		partCount++
		if partCount > m.maxParts {
			// Node: more parts than `limits.parts` -> error.
			_ = part.Close()
			_ = m.drainParts(mr)
			return nil, ErrTooManyParts
		}
		isFile := part.FileName() != ""
		if isFile {
			if part.FormName() != "qqfile" || saved != nil {
				// upload.single: only the first 'qqfile' file part is extracted.
				_, _ = io.Copy(io.Discard, part)
				_ = part.Close()
				continue
			}
			u, ferr := m.savePart(part, part.FileName())
			if ferr != nil {
				return nil, ferr
			}
			saved = u
			continue
		}
		// field (non-file) part: drained; counted against `parts` limit.
		_, _ = io.Copy(io.Discard, part)
		_ = part.Close()
	}
	return saved, nil
}

// savePart mirrors multer file storage (write to UploadDir; LIMIT_FILE_SIZE
// removes the partial file and is returned).
func (m *Middleware) savePart(part *multipart.Part, original string) (*UploadedFile, error) {
	name := m.randomName(original)
	dst := filepath.Join(m.uploadDir, name)
	f, err := m.openFile(dst)
	if err != nil {
		_ = os.Remove(dst)
		return nil, fmt.Errorf("open %q: %w", dst, err)
	}
	buf := make([]byte, 32 * 1024)
	var total int64
	for {
		n, rerr := m.partRead(part, buf)
		if n > 0 {
			total += int64(n)
			if m.maxFileSize > 0 && total > m.maxFileSize {
				_ = f.Close()
				_ = os.Remove(dst)
				return nil, ErrUploadFileTooLarge{Limit: m.maxFileSize}
			}
			if _, werr := m.writeFile(f, buf[:n]); werr != nil {
				_ = os.Remove(dst)
				return nil, werr
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = os.Remove(dst)
			return nil, rerr
		}
	}
	_ = f.Close()
	return &UploadedFile{
		Fieldname:   part.FormName(),
		Original:    original,
		Filename:    name,
		Path:        dst,
		Size:        total,
		ContentType: part.Header.Get("Content-Type"),
	}, nil
}

func (m *Middleware) respond(w http.ResponseWriter, status int, errMsg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"error":   errMsg,
	})
}

// randomName mirrors multer `getRandomFilename` (16 alphanumeric chars +
// the original name's extension). Uses the `randBytes` seam for testability.
func (m *Middleware) randomName(original string) string {
	b, _ := m.randBytes(16)
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	nb := make([]byte, 16)
	if len(b) == 16 {
		for i := range b {
			nb[i] = alphabet[b[i]%26]
		}
	} else {
		for i := range nb {
			nb[i] = 'a'
		}
	}
	return string(nb) + fileExt(original)
}

func fileExt(name string) string {
	i := strings.LastIndex(name, ".")
	if i <= 0 || i == len(name)-1 {
		return ""
	}
	return name[i:]
}

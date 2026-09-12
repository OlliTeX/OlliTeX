package filestore

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// --- HTTP server (1:1 with app/src/server.mjs + FileController.js) ----------

type byteRange struct {
	start int64
	end   int64 // inclusive
}

var (
	fseBucketNameRe = regexp.MustCompile(`^[a-z0-9-]+$`)
	fseObjectIdRe   = regexp.MustCompile(`^[0-9a-f]{24}$`)
	fseFilepathRe   = regexp.MustCompile(`^[\w\-.]+$`)
)

func fseParseRange(size int64, header string) *byteRange {
	if !strings.HasPrefix(header, "bytes=") {
		return nil
	}
	spec := strings.TrimSpace(header[len("bytes="):])
	if spec == "" || strings.Contains(spec, ",") {
		return nil
	}
	pos := strings.Index(spec, "-")
	if pos < 0 {
		return nil
	}
	l := spec[:pos]
	rr := spec[pos+1:]
	var start, end int64
	if l == "" {
		n, err := strconv.ParseInt(rr, 10, 64)
		if err != nil || n <= 0 {
			return nil
		}
		start = size - n
		if start < 0 {
			start = 0
		}
		end = size - 1
	} else {
		s, err := strconv.ParseInt(l, 10, 64)
		if err != nil {
			return nil
		}
		start = s
		if rr == "" {
			end = size - 1
		} else {
			e, err := strconv.ParseInt(rr, 10, 64)
			if err != nil {
				return nil
			}
			end = e
		}
	}
	if size <= 0 || start < 0 || end < start || start >= size {
		return nil
	}
	if end >= size {
		end = size - 1
	}
	return &byteRange{start: start, end: end}
}

type FSTConfig struct {
	TemplateFiles string
	ProjectBlobs  string
	GlobalBlobs   string
	UploadFolder  string

	EnableConversions bool
	Converter         string
	ConvertPrefix     []string

	UseSubdirectories bool
}

func (c *FSTConfig) withDefaults() {
	if c.TemplateFiles == "" {
		c.TemplateFiles = "/var/lib/overleaf/data/template_files"
	}
	if c.ProjectBlobs == "" {
		c.ProjectBlobs = "/var/lib/overleaf/data/history/overleaf-project-blobs"
	}
	if c.GlobalBlobs == "" {
		c.GlobalBlobs = "/var/lib/overleaf/data/history/overleaf-global-blobs"
	}
	if c.UploadFolder == "" {
		// Node (server-ce/config/settings.js): settings.path.uploadFolder =
		// Path.join(TMP_DIR, 'uploads') with TMP_DIR = /var/lib/overleaf/tmp.
		c.UploadFolder = "/var/lib/overleaf/tmp/uploads"
	}
	if c.Converter == "" {
		// Node (server-ce/config/settings.js): CONVERTER || 'pdftocairo'.
		c.Converter = "pdftocairo"
	}
}

type FSTHandlers struct {
	Cfg       FSTConfig
	Store     *fseStore
	Writer    *fseWriter
	Converter *fseConverter
	Handler   *fseHandler
}

func NewFSTHandlers(cfg FSTConfig) *FSTHandlers {
	cfg.withDefaults()
	store := &fseStore{useSubdirectories: cfg.UseSubdirectories}
	writer := &fseWriter{uploadFolder: cfg.UploadFolder}
	conv := &fseConverter{converter: cfg.Converter, prefix: cfg.ConvertPrefix, enable: cfg.EnableConversions}
	return &FSTHandlers{
		Cfg:       cfg,
		Store:     store,
		Writer:    writer,
		Converter: conv,
		Handler:   &fseHandler{Store: store, Writer: writer, Converter: conv, TemplateB: cfg.TemplateFiles, EnableConv: cfg.EnableConversions},
	}
}

func (h *FSTHandlers) Mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health_check", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("filestore is up"))
	})
	mux.HandleFunc("/bucket/", h.bucketFile)
	mux.HandleFunc("/history/global/hash/", h.globalBlobFile)
	mux.HandleFunc("/history/project/", h.projectBlobFile)
	mux.HandleFunc("/template/", h.templateFile)
	return mux
}

func (h *FSTHandlers) bucketFile(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/bucket/")
	// shape: /bucket/<bucket>/key/<key> -- <key> may be a nested path
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	bucket := parts[0]
	if !fseBucketNameRe.MatchString(bucket) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	tail := parts[1]
	if !strings.HasPrefix(tail, "key/") {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	key := tail[len("key/"):]
	if key == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	h.runFile(w, r, bucket, key, false)
}

func (h *FSTHandlers) globalBlobFile(w http.ResponseWriter, r *http.Request) {
	hash := strings.TrimPrefix(r.URL.Path, "/history/global/hash/")
	if hash == "" || strings.Contains(hash, "/") {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	key := hash[:2] + "/" + hash[2:4] + "/" + hash[4:]
	h.runFile(w, r, h.Cfg.GlobalBlobs, key, true)
}

func (h *FSTHandlers) projectBlobFile(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/history/project/")
	hid, sep, hash := cutOnce(rest, "/hash/")
	if sep == "" || hid == "" || hash == "" || strings.Contains(hash, "/") {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	key := fseProjectKeyFormat(hid) + "/" + hash[:2] + "/" + hash[2:]
	h.runFile(w, r, h.Cfg.ProjectBlobs, key, true)
}

func (h *FSTHandlers) templateFile(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/template/")
	parts := strings.Split(rest, "/")
	if len(parts) != 4 && len(parts) != 5 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	templateId, version, format := parts[0], parts[2], parts[3]
	subType := ""
	if len(parts) == 5 {
		subType = parts[4]
	}
	if !fseObjectIdRe.MatchString(templateId) || !fseFilepathRe.MatchString(format) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if _, err := strconv.Atoi(version); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if subType != "" && !fseFilepathRe.MatchString(subType) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	key := templateId + "/v/" + version + "/" + format
	if subType != "" {
		key += "/" + subType
	}
	bucket := h.Cfg.TemplateFiles
	switch r.Method {
	case http.MethodGet:
		h.runFile(w, r, bucket, key, false)
	case http.MethodHead:
		h.runHead(w, r, bucket, key, false)
	case http.MethodPost:
		h.runInsert(w, r, bucket, key, false)
	case http.MethodDelete:
		h.runDelete(w, r, bucket, key, false)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func cutOnce(s, sep string) (before, found, after string) {
	i := strings.Index(s, sep)
	if i < 0 {
		return s, "", ""
	}
	return s[:i], s[i : i+len(sep)], s[i+len(sep):]
}

func (h *FSTHandlers) runFile(w http.ResponseWriter, r *http.Request, bucket, key string, useSub bool) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	format := r.URL.Query().Get("format")
	style := r.URL.Query().Get("style")
	cacheWarm := r.URL.Query().Get("cacheWarm") == "true"

	var rng *byteRange
	if rh := r.Header.Get("Range"); rh != "" {
		if size, serr := h.Store.objectSize(bucket, key, useSub); serr == nil {
			rng = fseParseRange(size, rh)
			if rng != nil {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rng.start, rng.end, size))
			}
		}
	}

	rc, err := h.Handler.getFile(bucket, key, fseGetOpts{format: format, style: style, useSub: useSub})
	if err != nil {
		if fseErrCode(err) == 404 {
			w.WriteHeader(http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	defer rc.Close()
	if cacheWarm {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	if rng != nil {
		if rr, ok := rc.(io.ReadSeeker); ok {
			if _, serr := rr.Seek(rng.start, io.SeekStart); serr == nil {
				_, _ = io.CopyN(w, rr, rng.end-rng.start+1)
				return
			}
		}
	}
	_, _ = io.Copy(w, rc)
}

func (h *FSTHandlers) runHead(w http.ResponseWriter, r *http.Request, bucket, key string, useSub bool) {
	if r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	size, err := h.Handler.getFileSize(bucket, key, useSub)
	if err != nil {
		if fseErrCode(err) == 404 {
			w.WriteHeader(http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
}

func (h *FSTHandlers) runInsert(w http.ResponseWriter, r *http.Request, bucket, key string, useSub bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := h.Handler.insertFile(bucket, key, r.Body, useSub); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *FSTHandlers) runDelete(w http.ResponseWriter, r *http.Request, bucket, key string, useSub bool) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := h.Handler.deleteFile(bucket, key, useSub); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

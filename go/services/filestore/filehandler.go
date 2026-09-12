package filestore

import (
	"io"
	"os"
	"regexp"
	"strings"
)

// --- FileHandler (1:1 with app/js/FileHandler.js) ---------------------------

var (
	fseInsertKeyRe = regexp.MustCompile(`^[0-9a-f]{24}/([0-9a-f]{24}|v/[0-9]+/[a-z0-9]+)`)
	fseDeleteKeyRe = regexp.MustCompile(`^[0-9a-f]{24}/([0-9a-f]{24}|v/[0-9]+/[a-z]+)`)
)

type fseHandler struct {
	Store      *fseStore
	Writer     *fseWriter
	Converter  *fseConverter
	TemplateB  string
	EnableConv bool
}

func fseConvertedFolderKey(key string) string { return key + "-converted-cache/" }

func fseAddCachingToKey(key, format, style string) string {
	key = fseConvertedFolderKey(key)
	if format != "" && style == "" {
		key += "format-" + format
	} else if style != "" && format == "" {
		key += "style-" + style
	} else if style != "" && format != "" {
		key += "format-" + format + "-style-" + style
	}
	return key
}

func (h *fseHandler) insertFile(bucket, key string, r io.Reader, reqUseSub bool) error {
	ck := fseConvertedFolderKey(key)
	if !fseInsertKeyRe.MatchString(ck) {
		return fseInvalidParams("key does not match validation regex")
	}
	return h.Store.sendStream(bucket, key, r, reqUseSub, "")
}

func (h *fseHandler) deleteFile(bucket, key string, reqUseSub bool) error {
	ck := fseConvertedFolderKey(key)
	if !fseDeleteKeyRe.MatchString(ck) {
		return fseInvalidParams("key does not match validation regex")
	}
	if err := h.Store.deleteObject(bucket, key, reqUseSub); err != nil {
		return err
	}
	if h.EnableConv && bucket == h.TemplateB {
		return h.Store.deleteDirectory(bucket, ck, reqUseSub)
	}
	return nil
}

func (h *fseHandler) getFile(bucket, key string, opts fseGetOpts) (io.ReadCloser, error) {
	if opts.format == "" && opts.style == "" {
		return h.Store.open(bucket, key, opts.useSub)
	}
	return h.getConvertedFile(bucket, key, opts)
}

type fseGetOpts struct {
	format string
	style  string
	useSub bool
}

func (h *fseHandler) getFileSize(bucket, key string, reqUseSub bool) (int64, error) {
	return h.Store.objectSize(bucket, key, reqUseSub)
}

func (h *fseHandler) getConvertedFile(bucket, key string, opts fseGetOpts) (io.ReadCloser, error) {
	convertedKey := fseAddCachingToKey(key, opts.format, opts.style)
	if h.Store.exists(bucket, convertedKey, opts.useSub) {
		f, err := h.Store.open(bucket, convertedKey, opts.useSub)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	original, err := h.Store.open(bucket, key, opts.useSub)
	if err != nil {
		return nil, err
	}
	localPath, werr := h.Writer.writeStream(original, key)
	original.Close()
	if werr != nil {
		return nil, werr
	}
	var (
		dest string
		cerr error
	)
	if opts.format != "" {
		dest, cerr = h.Converter.convert(localPath, opts.format)
	} else if opts.style == "thumbnail" {
		dest, cerr = h.Converter.thumbnail(localPath)
	} else if opts.style == "preview" {
		dest, cerr = h.Converter.preview(localPath)
	} else {
		return nil, fseConversion("invalid file conversion options")
	}
	if dest == "" {
		h.Writer.deleteFile(localPath)
		if cerr != nil {
			return nil, cerr
		}
		return nil, fseConversion("failed to convert file")
	}
	if strings.HasSuffix(strings.ToLower(dest), ".png") {
		fseCompressPng(dest)
	}
	h.Store.sendFile(bucket, convertedKey, dest, opts.useSub)
	h.Writer.deleteFile(localPath)
	out, oerr := os.Open(dest)
	if oerr != nil {
		return nil, fseRead("failed to read converted file")
	}
	_ = os.Remove(dest)
	return out, nil
}

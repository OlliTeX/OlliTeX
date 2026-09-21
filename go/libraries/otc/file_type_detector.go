package otc

import (
	"os"
	"path"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Mirrors libraries/overleaf-editor-core/lib/file_type_detector.js.
//
// Whether a file is an editable doc or a binary file, at a given pathname.
// The rules mirror web's Uploads/FileTypeManager and the type decision in web's
// UpdateMerger. This answers whether a file *should be* a doc: it caps at web's
// max_doc_length and rejects non-BMP content. (lib/blob_utils answers a
// different question - whether history *can* hold content as text.)

// ExistingType is the type of what is at the pathname now, if anything
// ('doc', 'file', or unknown/null).
type ExistingType string

const (
	ExistingTypeDoc     ExistingType = "doc"
	ExistingTypeFile    ExistingType = "file"
	ExistingTypeUnknown ExistingType = "" // null | undefined
)

// FileTypeConfig is the injected rule: web's max_doc_length in characters plus
// the shared extension / filename lists.
type FileTypeConfig struct {
	TextExtensions    []string
	EditableFilenames []string
	MaxDocLength      int
}

// DetectedType is the verdict: `{kind:'text', content}` or `{kind:'binary'}`.
type DetectedType struct {
	Kind    string // "text" or "binary"
	Content string // set only when Kind=="text"
}

func detectedBinary() DetectedType { return DetectedType{Kind: "binary"} }
func detectedText(s string) DetectedType {
	return DetectedType{Kind: "text", Content: s}
}

func validateFileTypeConfig(config *FileTypeConfig) {
	if config == nil {
		panic(newTypeError("fileTypeDetector: bad config"))
	}
}

// IsTextFilename mirrors isTextFilename: whether the name alone allows the file
// to be a doc.
func IsTextFilename(pathname string, config *FileTypeConfig) bool {
	validateFileTypeConfig(config)
	basename := strings.ToLower(path.Base(pathname))
	extension := strings.ToLower(path.Ext(pathname))
	for _, ext := range config.TextExtensions {
		if extension == "."+ext {
			return true
		}
	}
	for _, name := range config.EditableFilenames {
		if basename == name {
			return true
		}
	}
	return false
}

// NeedsContent mirrors needsContent: false is a verdict (a binary file), true
// means the content must be read to classify.
func NeedsContent(pathname string, byteLength *int, existingType ExistingType, config *FileTypeConfig) bool {
	validateFileTypeConfig(config)

	if existingType == ExistingTypeFile {
		return false
	}
	if existingType != ExistingTypeDoc && !IsTextFilename(pathname, config) {
		return false
	}
	if byteLength != nil && *byteLength > 3*config.MaxDocLength {
		return false
	}
	return true
}

// IsEditableString mirrors isEditableString: whether a decoded string can be
// stored as a doc.
func IsEditableString(content string, config *FileTypeConfig) bool {
	validateFileTypeConfig(config)
	if utf16Units(content) >= config.MaxDocLength {
		return false
	}
	if strings.ContainsRune(content, 0) {
		return false
	}
	if ContainsNonBmpChars(content) {
		return false
	}
	return true
}

// DetectBuffer mirrors detectBuffer: classify content already held in memory.
func DetectBuffer(buf []byte, pathname string, existingType ExistingType, config *FileTypeConfig) DetectedType {
	byteLength := len(buf)
	if !NeedsContent(pathname, &byteLength, existingType, config) {
		return detectedBinary()
	}
	if !utf8.Valid(buf) {
		return detectedBinary()
	}
	content := string(buf)
	if !IsEditableString(content, config) {
		return detectedBinary()
	}
	return detectedText(content)
}

// DetectFile mirrors detectFile: classify the file spooled at localPath as it
// would be classified at pathname.
func DetectFile(pathname, localPath string, existingType ExistingType, config *FileTypeConfig) (DetectedType, error) {
	if !NeedsContent(pathname, nil, existingType, config) {
		return detectedBinary(), nil
	}
	stat, err := os.Stat(localPath)
	if err != nil {
		return DetectedType{}, err
	}
	byteLength := int(stat.Size())
	if !NeedsContent(pathname, &byteLength, existingType, config) {
		return detectedBinary(), nil
	}
	buf, err := os.ReadFile(localPath)
	if err != nil {
		return DetectedType{}, err
	}
	return DetectBuffer(buf, pathname, existingType, config), nil
}

// utf16Units is the Node-JS `content.length`: the number of UTF-16 code units.
func utf16Units(s string) int {
	return len(utf16.Encode([]rune(s)))
}

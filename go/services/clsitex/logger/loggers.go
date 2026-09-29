// Package logger ports services/clsi/app/js/LoggerSerializers.js.
//
// Node parity notes:
//
// LoggerSerializers exports a single serializer, clsiRequest, which builds a
// shallow logging summary from a parsed CLSI request.
//   - The allow-list CLSI_REQUEST_SERIALIZED_PROPERTIES is a fixed list of 13
//     keys; only keys that are present (JS `!== undefined`) on the incoming
//     object are copied.
//   - `imageName` gets `Path.basename(imageName)` applied before inclusion
//     (JS truthiness: falsy values — empty string — are skipped, so the
//     basename is never reached for "").
//   - Key order is preserved from the allow-list (JS objects iterate in
//     insertion order, which is the order of CLSI_REQUEST_SERIALIZED_PROPERTIES).
package logger

import "path"

// CLSIRequestSerializedProperties is the port of CLSI_REQUEST_SERIALIZED_PROPERTIES.
// Order matters: it is the iteration order of the produced summary.
var CLSIRequestSerializedProperties = []string{
	"compiler",
	"compileFromClsiCache",
	"populateClsiCache",
	"enablePdfCaching",
	"pdfCachingMinChunkSize",
	"timeout",
	"imageName",
	"draft",
	"stopOnFirstError",
	"check",
	"flags",
	"compileGroup",
	"syncType",
}

// SerializeClsiRequest ports LoggerSerializers.clsiRequest.
//
// In Go the parsed request is a map[string]interface{} (as decoded by
// encoding/json); "present" is approximated by key existence with a non-nil
// value, matching JS `!== undefined` for JSON-decoded objects (undefined
// simply does not exist in Go's map).
func SerializeClsiRequest(clsiRequest map[string]interface{}) map[string]interface{} {
	summary := map[string]interface{}{}
	for _, key := range CLSIRequestSerializedProperties {
		if key == "imageName" {
			if s, ok := clsiRequest["imageName"].(string); ok && s != "" {
				summary["imageName"] = baseUrlname(s)
				continue
			}
		} else if v, present := clsiRequest[key]; present && v != nil {
			summary[key] = v
		}
	}
	return summary
}

// baseUrlname mirrors Path.basename on a POSIX-style image name
// (e.g. "/some/images/xelatex:latest" -> "xelatex:latest"). The Node service
// uses node:path which, with its POSIX default, never treats ':' specially.
func baseUrlname(p string) string { return path.Base(p) }

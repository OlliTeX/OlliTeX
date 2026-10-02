// wire.go renders the CompileController response envelopes (Node:
// res.status(code).send / .sendStatus).
//
// Node's express res.status(code).send(object) renders the object with
// JSON.stringify (which omits undefined values but emits null where JS null
// exists) with Content-Type application/json + charset. The Go builders
// mirror the envelope keys in Node's literal insertion order; Go omits the
// keys Node omits via undefined (buildId / baseHistoryVersion when unset),
// matching a Node success/error 200 body.
package compilecontroller

import (
	"encoding/json"
	"fmt"
	"net/http"

	off "ollitex/go/services/clsitypst/outputfilefinder"
)

// sendPlain mirrors res.send(string) / res.sendStatus(code) (html text body,
// or 204 with no body).
func sendPlain(res http.ResponseWriter, code int, text string) (int, error) {
	if code >= 200 && code < 300 && text != "" {
		res.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	res.WriteHeader(code)
	if code >= 200 && code < 300 && text != "" {
		if _, werr := res.Write([]byte(text)); werr != nil {
			return code, werr
		}
	}
	return code, nil
}

// buildCompileBody assembles the `{ compile: {...} }` envelope
// (CompileController.js L148-176).
//
// errorRender mirrors Node's `error?.message || error`:
//
//	""            -> omitted (undefined) on the wire
//	"a string"    -> rendered as a JSON string
//	"object"      -> rendered as {} (Node sends the Error object; JSON.stringify
//	                emits {} for a primitive Error)
type compileWire struct {
	status             string
	code               int // 0 => express default 200
	errorRender        string
	baseHistoryVersion *int
	buildID            *string
	stats              map[string]any
	timings            map[string]any
	outputFiles        []off.OutputFile
	instanceType       string
	zone               string
	isSpotInstance     bool
	outputURLPrefix    string
	downloadHost       string
	projectID          string
	userID             string
}

func buildCompileBody(c compileWire) map[string]any {
	compile := map[string]any{"status": c.status}
	switch c.errorRender {
	case "":
		// undefined -> omitted (the success tail has no `error` key).
	case "object":
		compile["error"] = map[string]any{}
	default:
		compile["error"] = c.errorRender
	}
	// Node D9: baseHistoryVersion is destructured from result; in v1
	// typst (no HRW) it is the parsed baseHistoryVersion echo, and it is
	// present on the wire whenever the value is defined (not null).
	if c.baseHistoryVersion != nil {
		compile["baseHistoryVersion"] = *c.baseHistoryVersion
	}
	compile["stats"] = c.stats
	compile["timings"] = c.timings
	if c.buildID != nil {
		compile["buildId"] = *c.buildID
	}
	// Node: clsiCacheShard is always `undefined` here (typst baseline D3 —
	// the D6 bootstrap downloads from the clsi cache; the notify is a
	// clsi-side concern) and omitted by JSON.stringify.
	compile["instanceType"] = c.instanceType
	compile["zone"] = c.zone
	compile["isSpotInstance"] = c.isSpotInstance
	compile["outputUrlPrefix"] = c.outputURLPrefix
	files := make([]any, len(c.outputFiles))
	for i, f := range c.outputFiles {
		files[i] = wireFile(c, f)
	}
	compile["outputFiles"] = files
	return map[string]any{"compile": compile}
}

// wireFile mirrors `outputFiles.map(file => ({url, ...file}))`: url first,
// then the file fields, in the off.OutputFile JSON order (documented: Go key
// order differs from the Node spread order but the key SET is identical).
func wireFile(c compileWire, f off.OutputFile) map[string]any {
	url := fmt.Sprintf("%s/project/%s%s/build/%s/output/%s",
		c.downloadHost, c.projectID,
		userSeg(c.userID), f.Build, f.Path)
	out := map[string]any{"url": url, "path": f.Path, "type": f.Type}
	if f.Build != "" {
		out["build"] = f.Build
	}
	if f.Size != nil {
		out["size"] = *f.Size
	}
	if f.ContentID != nil {
		// off.OutputFile JSON key is "contentId"; Node spreads the same key.
		out["contentId"] = *f.ContentID
	}
	if f.Ranges != nil {
		out["ranges"] = f.Ranges
	}
	if f.StartXRefTable != nil {
		out["startXRefTable"] = *f.StartXRefTable
	}
	return out
}

// userSeg mirrors `(user_id != null ? "/user/" + user_id : "")`.
func userSeg(userID string) string {
	if userID != "" {
		return "/user/" + userID
	}
	return ""
}

// writeJSON writes a JSON body with the express charset header.
func writeJSON(res http.ResponseWriter, code int, body any) (int, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	res.Header().Set("Content-Type", "application/json; charset=utf-8")
	res.WriteHeader(code)
	if _, werr := res.Write(data); werr != nil {
		return code, werr
	}
	return code, nil
}

// sendCompileWire renders the `{compile: {...}}` envelope with the
// res.status(code || 200) default (Node express: falsy code => 200).
func sendCompileWire(res http.ResponseWriter, c compileWire) (int, error) {
	code := c.code
	if code == 0 {
		code = http.StatusOK
	}
	return writeJSON(res, code, buildCompileBody(c))
}

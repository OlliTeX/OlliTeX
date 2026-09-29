// wire.go renders the CompileController response envelopes (Node:
// res.status(code).send / .json / .sendStatus).
//
// Node's express res.status(code).send(object) renders the object with JSON
// .stringify (which omits undefined values but emits null where JS null
// exists) with Content-Type application/json + charset. The Go builders
// mirror the envelope keys in Node's literal insertion order; Go omits the
// keys Node omits via undefined (buildId / clsiCacheShard /
// baseHistoryVersion when unset), matching a Node success/error 200 body.
package compilecontroller

import (
	"encoding/json"
	"fmt"
	"net/http"

	off "ollitex/go/services/clsitex/outputfilefinder"
)

// sendJSONObject renders body under `object` in the order Node's literal
// emits it. The server layer (and tests) compare the unmarshalled map, so
// key order is cosmetic (documented divergence from Node key order).
func sendJSONObject(res http.ResponseWriter, code int, body map[string]any) (int, error) {
	return writeJSON(res, code, body)
}

// writeJSON writes a JSON body with the express charset header.
func writeJSON(res http.ResponseWriter, code int, body map[string]any) (int, error) {
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

// buildCompileBody assembles the `{ compile: {...} }` envelope.
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
	clsiCacheShard     string
	hasShard           bool
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
	case "object":
		compile["error"] = map[string]any{}
	default:
		compile["error"] = c.errorRender
	}
	if c.baseHistoryVersion != nil {
		compile["baseHistoryVersion"] = *c.baseHistoryVersion
	}
	if c.stats != nil {
		compile["stats"] = c.stats
	} else {
		compile["stats"] = map[string]any{}
	}
	if c.timings != nil {
		compile["timings"] = c.timings
	} else {
		compile["timings"] = map[string]any{}
	}
	if c.buildID != nil {
		compile["buildId"] = *c.buildID
	}
	if c.hasShard {
		compile["clsiCacheShard"] = c.clsiCacheShard
	}
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
// then the file fields (path/type/build/size/contentId/ranges/...), in the
// off.OutputFile JSON order (documented: Go key order differs from the
// Node spread order but the key SET is identical).
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

// sendCompileWire renders the `{compile: {...}}` envelope with the
// res.status(code || 200) default (Node express: falsy code => 200).
func sendCompileWire(res http.ResponseWriter, c compileWire) (int, error) {
	code := c.code
	if code == 0 {
		code = http.StatusOK
	}
	return writeJSON(res, code, buildCompileBody(c))
}
